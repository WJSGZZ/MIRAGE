package mux

import (
	"io"
	"net"
	"testing"
)

func TestSessionOpenAcceptAndData(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()

	client := NewClientSession(left)
	server := NewServerSession(right)
	defer client.Close()
	defer server.Close()

	acceptCh := make(chan *Stream, 1)
	errCh := make(chan error, 1)
	go func() {
		st, err := server.Accept()
		if err != nil {
			errCh <- err
			return
		}
		acceptCh <- st
	}()

	clientStream, err := client.OpenStream("example.com:443")
	if err != nil {
		t.Fatalf("OpenStream: %v", err)
	}
	defer clientStream.Close()

	var serverStream *Stream
	select {
	case serverStream = <-acceptCh:
	case err := <-errCh:
		t.Fatalf("Accept: %v", err)
	}
	defer serverStream.Close()

	if got := serverStream.Dest(); got != "example.com:443" {
		t.Fatalf("Dest mismatch: %q", got)
	}

	const payload = "hello over yamux"
	go func() {
		_, _ = io.WriteString(clientStream, payload)
	}()

	buf := make([]byte, len(payload))
	if _, err := io.ReadFull(serverStream, buf); err != nil {
		t.Fatalf("ReadFull(serverStream): %v", err)
	}
	if string(buf) != payload {
		t.Fatalf("payload mismatch: %q", string(buf))
	}
}
