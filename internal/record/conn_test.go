package record

import (
	"bytes"
	"io"
	"net"
	"testing"
	"time"
)

func TestConnRoundTripStripsPaddingAndHeartbeat(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()

	seed := bytes.Repeat([]byte{0x11}, 16)
	client, err := NewConn(left, nil, seed)
	if err != nil {
		t.Fatalf("NewConn(client): %v", err)
	}
	server, err := NewConn(right, nil, seed)
	if err != nil {
		t.Fatalf("NewConn(server): %v", err)
	}
	defer client.Close()
	defer server.Close()

	want := bytes.Repeat([]byte("mirage-record-layer-"), 2000)
	errCh := make(chan error, 1)
	go func() {
		_, err := client.Write(want)
		errCh <- err
	}()

	got := make([]byte, len(want))
	if _, err := io.ReadFull(server, got); err != nil {
		t.Fatalf("ReadFull(server): %v", err)
	}
	// Drain any trailing padding frame that client.Write may still be sending.
	// Without this, client.Write blocks on the last padding frame write after
	// io.ReadFull has already collected all data bytes and stopped reading.
	server.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	io.Copy(io.Discard, server)
	server.SetReadDeadline(time.Time{})

	if err := <-errCh; err != nil {
		t.Fatalf("Write(client): %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("round-trip mismatch: got %d bytes", len(got))
	}
}

func TestConnDiscardsUnknownAndPaddingFrames(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()

	seed := bytes.Repeat([]byte{0x22}, 16)
	reader, err := NewConn(left, nil, seed)
	if err != nil {
		t.Fatalf("NewConn: %v", err)
	}
	defer reader.Close()

	go func() {
		_ = writeRawFrame(right, TypePadding, []byte("noise"))
		_ = writeRawFrame(right, 0x99, []byte("ignored"))
		_ = writeRawFrame(right, TypeHeartbeat, nil)
		_ = writeRawFrame(right, TypeData, []byte("hello"))
	}()

	buf := make([]byte, 5)
	_ = reader.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := io.ReadFull(reader, buf)
	if err != nil {
		t.Fatalf("ReadFull: %v", err)
	}
	if n != 5 || string(buf) != "hello" {
		t.Fatalf("unexpected payload %q", string(buf[:n]))
	}
}

func writeRawFrame(w io.Writer, typ byte, payload []byte) error {
	hdr := []byte{byte(len(payload) >> 8), byte(len(payload)), typ}
	if _, err := w.Write(hdr); err != nil {
		return err
	}
	if len(payload) == 0 {
		return nil
	}
	_, err := w.Write(payload)
	return err
}
