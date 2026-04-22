// Package mux wraps hashicorp/yamux behind the repository's existing stream API.
package mux

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"

	"github.com/hashicorp/yamux"
)

type Session struct {
	sess *yamux.Session
}

type Stream struct {
	net.Conn
	dest string
}

func NewClientSession(conn net.Conn) *Session {
	cfg := yamux.DefaultConfig()
	cfg.EnableKeepAlive = false
	sess, err := yamux.Client(conn, cfg)
	if err != nil {
		return &Session{}
	}
	return &Session{sess: sess}
}

func NewServerSession(conn net.Conn) *Session {
	cfg := yamux.DefaultConfig()
	cfg.EnableKeepAlive = false
	sess, err := yamux.Server(conn, cfg)
	if err != nil {
		return &Session{}
	}
	return &Session{sess: sess}
}

func (s *Session) OpenStream(dest string) (*Stream, error) {
	if s == nil || s.sess == nil {
		return nil, fmt.Errorf("mux: session unavailable")
	}
	st, err := s.sess.Open()
	if err != nil {
		return nil, fmt.Errorf("mux: open stream: %w", err)
	}
	if err := writeDest(st, dest); err != nil {
		_ = st.Close()
		return nil, err
	}
	return &Stream{Conn: st}, nil
}

func (s *Session) Accept() (*Stream, error) {
	if s == nil || s.sess == nil {
		return nil, fmt.Errorf("mux: session unavailable")
	}
	st, err := s.sess.Accept()
	if err != nil {
		return nil, err
	}
	dest, err := readDest(st)
	if err != nil {
		_ = st.Close()
		return nil, err
	}
	return &Stream{Conn: st, dest: dest}, nil
}

func (s *Session) Done() <-chan struct{} {
	if s == nil || s.sess == nil {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	return s.sess.CloseChan()
}

func (s *Session) Close() error {
	if s == nil || s.sess == nil {
		return nil
	}
	return s.sess.Close()
}

func (st *Stream) Dest() string {
	if st == nil {
		return ""
	}
	return st.dest
}

func writeDest(w io.Writer, dest string) error {
	if len(dest) == 0 {
		return fmt.Errorf("mux: empty destination")
	}
	if len(dest) > 65535 {
		return fmt.Errorf("mux: destination too long")
	}
	var hdr [2]byte
	binary.BigEndian.PutUint16(hdr[:], uint16(len(dest)))
	if _, err := w.Write(hdr[:]); err != nil {
		return fmt.Errorf("mux: write destination length: %w", err)
	}
	if _, err := io.WriteString(w, dest); err != nil {
		return fmt.Errorf("mux: write destination: %w", err)
	}
	return nil
}

func readDest(r io.Reader) (string, error) {
	var hdr [2]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return "", fmt.Errorf("mux: read destination length: %w", err)
	}
	n := binary.BigEndian.Uint16(hdr[:])
	if n == 0 {
		return "", fmt.Errorf("mux: empty destination")
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", fmt.Errorf("mux: read destination: %w", err)
	}
	return string(buf), nil
}
