// Package client implements the MIRAGE client.
//
// The client keeps one persistent TLS+mux session to the server.
// When the session dies it is re-established on the next request.
// Multiple SOCKS5 requests share the same session via mux streams.
package client

import (
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"miraged/internal/auth"
	"miraged/internal/config"
	"miraged/internal/mux"
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

// Dial opens a mux stream to the given destination (e.g. "example.com:443").
// It establishes (or reuses) the underlying TLS session to the MIRAGE server.
func (c *Client) Dial(dest string) (*mux.Stream, error) {
	sess, err := c.getSession()
	if err != nil {
		return nil, err
	}
	st, err := sess.OpenStream(dest)
	if err != nil {
		// Session likely dead — drop it and let next call reconnect.
		c.dropSession()
		return nil, fmt.Errorf("client: open stream: %w", err)
	}
	return st, nil
}

// getSession returns the current live session or establishes a new one.
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
	// When the session's underlying connection dies, clear the cached session
	// so the next Dial will reconnect.
	go func() {
		// Wait for session to close by trying to drain the accept channel,
		// which is only closed when the session dies.
		// We detect death via a background dial that will fail.
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

// connect creates a new TLS connection to the server, performs MIRAGE auth,
// and returns an initialised mux.Session.
func (c *Client) connect() (*mux.Session, error) {
	cfg := c.cfg

	tlsCfg := &tls.Config{
		ServerName:         cfg.SNI,
		InsecureSkipVerify: cfg.InsecureSkipVerify,
		MinVersion:         tls.VersionTLS13,
	}

	log.Printf("miragec: connecting to %s (sni=%s)", cfg.Server, cfg.SNI)

	rawConn, err := net.DialTimeout("tcp", cfg.Server, 15*time.Second)
	if err != nil {
		return nil, fmt.Errorf("client: tcp dial: %w", err)
	}

	tlsConn := tls.Client(rawConn, tlsCfg)
	tlsConn.SetDeadline(time.Now().Add(30 * time.Second))
	if err := tlsConn.Handshake(); err != nil {
		rawConn.Close()
		return nil, fmt.Errorf("client: tls handshake: %w", err)
	}

	// Send MIRAGE auth message and wait for ACK.
	if err := auth.SendAndVerify(tlsConn, cfg.ServerPub, cfg.ShortIDBytes); err != nil {
		tlsConn.Close()
		return nil, fmt.Errorf("client: auth: %w", err)
	}

	tlsConn.SetDeadline(time.Time{}) // clear deadline
	log.Printf("miragec: session established")

	return mux.NewClientSession(tlsConn), nil
}

// waitForDeath blocks until the session is closed.
// It probes by reading one byte from a dummy stream open — any error means dead.
func waitForDeath(sess *mux.Session) {
	// Open a "ping" stream that will immediately get RST from the server
	// (unknown destination "0.0.0.0:1") — we just use it to detect death.
	// Actually: just wait for a real Open to fail by blocking forever until
	// the session's closeCh is signalled, which we can't access externally.
	// Simple heuristic: try to open a stream every 30 seconds. If it fails,
	// the session is dead.
	for {
		time.Sleep(30 * time.Second)
		_, err := sess.OpenStream("127.0.0.1:1")
		if err != nil {
			return
		}
	}
}

// Relay copies bidirectionally between a SOCKS conn and a mux stream.
func Relay(socks net.Conn, st *mux.Stream) {
	defer socks.Close()
	defer st.Close()

	done := make(chan struct{}, 2)
	go func() {
		io.Copy(st, socks)
		done <- struct{}{}
	}()
	go func() {
		io.Copy(socks, st)
		done <- struct{}{}
	}()
	<-done
}
