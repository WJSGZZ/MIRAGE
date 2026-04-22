// Package replayconn exposes a net.Conn that replays already-consumed prefix bytes first.
package replayconn

import (
	"bytes"
	"net"
	"sync"
	"time"
)

type Conn struct {
	net.Conn

	mu  sync.Mutex
	buf *bytes.Reader
}

func New(conn net.Conn, prefix []byte) *Conn {
	cp := append([]byte(nil), prefix...)
	return &Conn{
		Conn: conn,
		buf:  bytes.NewReader(cp),
	}
}

func (c *Conn) Read(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.buf != nil && c.buf.Len() > 0 {
		return c.buf.Read(p)
	}
	return c.Conn.Read(p)
}

func (c *Conn) SetDeadline(t time.Time) error {
	return c.Conn.SetDeadline(t)
}

func (c *Conn) SetReadDeadline(t time.Time) error {
	return c.Conn.SetReadDeadline(t)
}

func (c *Conn) SetWriteDeadline(t time.Time) error {
	return c.Conn.SetWriteDeadline(t)
}
