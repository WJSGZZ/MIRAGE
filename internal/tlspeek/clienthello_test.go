package tlspeek

import (
	"crypto/tls"
	"io"
	"net"
	"testing"
	"time"
)

func TestReadClientHelloParsesSessionIDAndSNI(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	errCh := make(chan error, 1)
	go func() {
		cfg := &tls.Config{
			ServerName: "example.com",
			MinVersion: tls.VersionTLS13,
			MaxVersion: tls.VersionTLS13,
			SessionTicketsDisabled: true,
		}
		c := tls.Client(clientConn, cfg)
		_ = c.SetDeadline(time.Now().Add(2 * time.Second))
		errCh <- c.Handshake()
	}()

	hello, err := ReadClientHello(serverConn, 2*time.Second)
	if err != nil {
		t.Fatalf("ReadClientHello: %v", err)
	}
	if len(hello.Raw) == 0 {
		t.Fatalf("raw clienthello is empty")
	}
	if got := len(hello.SessionID); got == 0 {
		t.Fatalf("session id not parsed")
	}
	// Standard library chooses a random session_id; overwrite check is only for shape here.
	if got := hello.ServerName; got != "example.com" {
		t.Fatalf("server name = %q", got)
	}

	// Complete closure so the writer goroutine exits.
	_ = serverConn.Close()
	select {
	case <-errCh:
	case <-time.After(3 * time.Second):
		t.Fatal("handshake goroutine did not exit")
	}
}

func TestReadClientHelloRejectsNonTLS(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	go func() {
		_, _ = io.WriteString(clientConn, "GET / HTTP/1.1\r\n\r\n")
	}()

	if _, err := ReadClientHello(serverConn, 2*time.Second); err == nil {
		t.Fatal("expected non-TLS parse error")
	}
}
