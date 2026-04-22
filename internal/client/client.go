// Package client implements the MIRAGE client.
package client

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	utls "github.com/refraction-networking/utls"

	"miraged/internal/auth"
	"miraged/internal/config"
	"miraged/internal/mux"
	"miraged/internal/protocol"
	"miraged/internal/record"
)

// Client holds the state of one MIRAGE client instance.
type Client struct {
	cfg *config.ClientConfig

	mu      sync.Mutex
	session *mux.Session
}

// New creates a Client from cfg. Does not connect until the first request.
func New(cfg *config.ClientConfig) *Client {
	return &Client{cfg: cfg}
}

// Dial opens a mux stream to the given destination.
func (c *Client) Dial(dest string) (*mux.Stream, error) {
	sess, err := c.getSession()
	if err != nil {
		return nil, err
	}
	st, err := sess.OpenStream(dest)
	if err != nil {
		c.dropSession()
		return nil, fmt.Errorf("client: open stream: %w", err)
	}
	return st, nil
}

func (c *Client) getSession() (*mux.Session, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.session != nil {
		return c.session, nil
	}
	sess, err := c.connect()
	if err != nil {
		return nil, err
	}
	c.session = sess
	go func() {
		waitForDeath(sess)
		c.dropSession()
	}()
	return sess, nil
}

func (c *Client) dropSession() {
	c.mu.Lock()
	c.session = nil
	c.mu.Unlock()
}

func (c *Client) connect() (*mux.Session, error) {
	cfg := c.cfg
	log.Printf("miragec: connecting to %s (sni=%s)", cfg.Server, cfg.SNI)

	rawConn, err := net.DialTimeout("tcp", cfg.Server, 15*time.Second)
	if err != nil {
		return nil, fmt.Errorf("client: tcp dial: %w", err)
	}

	var transport net.Conn
	if len(cfg.PSKBytes) == 32 {
		transport, err = c.connectSpec(rawConn)
	} else {
		transport, err = c.connectLegacy(rawConn)
	}
	if err != nil {
		rawConn.Close()
		return nil, err
	}

	log.Printf("miragec: session established")
	if cfg.ClientPaddingSeedBytes != ([16]byte{}) {
		recordConn, err := record.NewConn(transport, cfg.ClientPaddingSeedBytes[:])
		if err != nil {
			transport.Close()
			return nil, fmt.Errorf("client: record conn: %w", err)
		}
		transport = recordConn
	}

	return mux.NewClientSession(transport), nil
}

func (c *Client) connectLegacy(rawConn net.Conn) (net.Conn, error) {
	cfg := c.cfg
	tlsCfg := &tls.Config{
		ServerName:         cfg.SNI,
		MinVersion:         tls.VersionTLS13,
		InsecureSkipVerify: cfg.InsecureSkipVerify,
	}
	if len(cfg.CertPinBytes) > 0 {
		tlsCfg.InsecureSkipVerify = true
		tlsCfg.VerifyPeerCertificate = buildPinVerifier(cfg.CertPinBytes)
	}

	tlsConn := tls.Client(rawConn, tlsCfg)
	_ = tlsConn.SetDeadline(time.Now().Add(30 * time.Second))
	if err := tlsConn.Handshake(); err != nil {
		return nil, fmt.Errorf("client: tls handshake: %w", err)
	}
	if err := auth.SendAndVerify(tlsConn, cfg.ServerPub, cfg.ShortIDBytes); err != nil {
		tlsConn.Close()
		return nil, fmt.Errorf("client: auth: %w", err)
	}
	_ = tlsConn.SetDeadline(time.Time{})
	return tlsConn, nil
}

func (c *Client) connectSpec(rawConn net.Conn) (net.Conn, error) {
	cfg := c.cfg
	utlsCfg := &utls.Config{
		ServerName: cfg.SNI,
		MinVersion: tls.VersionTLS13,
	}
	if len(cfg.CertPinBytes) > 0 {
		utlsCfg.InsecureSkipVerify = true
		utlsCfg.VerifyPeerCertificate = buildPinVerifier(cfg.CertPinBytes)
	}

	uconn := utls.UClient(rawConn, utlsCfg, utls.HelloChrome_Auto)
	_ = uconn.SetDeadline(time.Now().Add(30 * time.Second))
	if err := uconn.BuildHandshakeState(); err != nil {
		return nil, fmt.Errorf("client: build utls state: %w", err)
	}

	var clientRandom [32]byte
	if _, err := rand.Read(clientRandom[:]); err != nil {
		return nil, fmt.Errorf("client: random: %w", err)
	}
	if err := uconn.SetClientRandom(clientRandom[:]); err != nil {
		return nil, fmt.Errorf("client: set client random: %w", err)
	}

	uid, err := protocol.DeriveUID(cfg.PSKBytes)
	if err != nil {
		return nil, fmt.Errorf("client: derive uid: %w", err)
	}
	token, err := protocol.DeriveHMACToken(cfg.PSKBytes, protocol.TimeWindow(time.Now().Unix()), clientRandom)
	if err != nil {
		return nil, fmt.Errorf("client: derive token: %w", err)
	}
	sessionID := protocol.BuildSessionID(uid, token)
	uconn.HandshakeState.Hello.SessionId = append([]byte(nil), sessionID[:]...)
	if err := uconn.MarshalClientHello(); err != nil {
		return nil, fmt.Errorf("client: marshal client hello: %w", err)
	}
	if err := uconn.Handshake(); err != nil {
		return nil, fmt.Errorf("client: utls handshake: %w", err)
	}
	_ = uconn.SetDeadline(time.Time{})
	return uconn, nil
}

func buildPinVerifier(pins [][]byte) func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
	return func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
		if len(rawCerts) == 0 {
			return fmt.Errorf("pin verify: no server certificates")
		}
		cert, err := x509.ParseCertificate(rawCerts[0])
		if err != nil {
			return fmt.Errorf("pin verify: parse leaf: %w", err)
		}
		sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
		for _, pin := range pins {
			if len(pin) != len(sum) {
				continue
			}
			match := true
			for i := range pin {
				if pin[i] != sum[i] {
					match = false
					break
				}
			}
			if match {
				return nil
			}
		}
		return fmt.Errorf("pin verify: SPKI pin mismatch")
	}
}

func waitForDeath(sess *mux.Session) {
	<-sess.Done()
}

// Relay copies bidirectionally between a SOCKS conn and a mux stream.
func Relay(socks net.Conn, st *mux.Stream) {
	defer socks.Close()
	defer st.Close()

	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(st, socks)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(socks, st)
		done <- struct{}{}
	}()
	<-done
}
