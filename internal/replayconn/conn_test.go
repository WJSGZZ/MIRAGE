package replayconn

import (
	"io"
	"net"
	"testing"
)

func TestReplayPrefixThenUnderlying(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()

	rc := New(left, []byte("hello "))
	defer rc.Close()

	go func() {
		_, _ = io.WriteString(right, "world")
	}()

	buf := make([]byte, 11)
	if _, err := io.ReadFull(rc, buf); err != nil {
		t.Fatalf("ReadFull: %v", err)
	}
	if string(buf) != "hello world" {
		t.Fatalf("unexpected payload %q", string(buf))
	}
}
