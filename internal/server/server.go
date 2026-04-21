// Package server implements the MIRAGE server.
//
// Connection lifecycle:
//  1. Accept TCP
//  2. TLS 1.3 handshake (server presents its own cert)
//  3. Read 80-byte MIRAGE auth message, verify with BLAKE3
//     • Fail → send HTTP 400, close. Looks like a normal web server.
//     • Pass → send 0x00 ACK, enter mux server mode.
//  4. Loop: Accept mux streams, dial destination, relay bidirectionally.
package server

import (
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"time"

	"miraged/internal/auth"
	"miraged/internal/config"
	"miraged/internal/mux"
)

// Run starts the server and blocks until it returns an error.
func Run(cfg *config.ServerConfig, tlsCfg *tls.Config) error {
	ln, err := tls.Listen("tcp", cfg.Listen, tlsCfg)
	if err != nil {
		return fmt.Errorf("listen %s: %w", cfg.Listen, err)
	}
	log.Printf("miraged: listening on %s", cfg.Listen)

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("miraged: accept: %v", err)
			continue
		}
		go handleConn(conn, cfg)
	}
}

func handleConn(conn net.Conn, cfg *config.ServerConfig) {
	defer conn.Close()

	// ── 1. TLS handshake deadline ────────────────────────────────────────────
	conn.SetDeadline(time.Now().Add(30 * time.Second))

	// Force the TLS handshake so we can measure its duration separately.
	if tc, ok := conn.(*tls.Conn); ok {
		if err := tc.Handshake(); err != nil {
			return // TLS failure — normal for scanners
		}
	}

	// ── 2. Read & verify MIRAGE auth ─────────────────────────────────────────
	conn.SetDeadline(time.Now().Add(20 * time.Second))

	shortIDs := make([][]byte, len(cfg.Users))
	for i, u := range cfg.Users {
		shortIDs[i] = u.ShortIDBytes
	}

	_, err := auth.ReadAndVerify(conn, cfg.PrivKey, shortIDs, cfg.MaxTimeDiff)
	if err != nil {
		// Respond like a real web server to confuse active probers.
		rejectHTTP(conn)
		return
	}

	// ── 3. Send ACK ──────────────────────────────────────────────────────────
	if _, err := conn.Write([]byte{0x00}); err != nil {
		return
	}

	conn.SetDeadline(time.Time{}) // clear deadline for proxy operation

	// ── 4. Serve mux streams ─────────────────────────────────────────────────
	sess := mux.NewServerSession(conn)
	defer sess.Close()

	for {
		st, err := sess.Accept()
		if err != nil || st == nil {
			return
		}
		go serveStream(st)
	}
}

func serveStream(st *mux.Stream) {
	defer st.Close()

	dst, err := net.DialTimeout("tcp", st.Dest(), 15*time.Second)
	if err != nil {
		log.Printf("miraged: dial %s: %v", st.Dest(), err)
		return
	}
	defer dst.Close()

	relay(st, dst)
}

// relay copies bidirectionally between a and b until either side closes.
func relay(a, b io.ReadWriter) {
	done := make(chan struct{}, 2)
	go func() {
		io.Copy(b, a)
		if tc, ok := b.(interface{ CloseWrite() error }); ok {
			tc.CloseWrite()
		}
		done <- struct{}{}
	}()
	go func() {
		io.Copy(a, b)
		done <- struct{}{}
	}()
	<-done
}

// rejectHTTP sends a minimal HTTP 400 response to make the server look like
// a normal web server that rejects bad requests.
func rejectHTTP(w io.Writer) {
	const resp = "HTTP/1.1 400 Bad Request\r\n" +
		"Content-Type: text/plain\r\n" +
		"Content-Length: 11\r\n" +
		"Connection: close\r\n\r\n" +
		"Bad Request"
	w.Write([]byte(resp)) //nolint
}
