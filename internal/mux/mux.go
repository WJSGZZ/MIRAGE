// Package mux implements the MIRAGE inner multiplexing protocol.
//
// # Why a custom mux instead of VLESS
//
// VLESS sends one TCP connection per proxied stream. Each connection goes
// through the full TLS handshake, creating a JA3-fingerprintable burst
// pattern. A custom mux runs multiple proxied streams over a single TLS
// connection: only one TLS handshake is ever visible to the network.
//
// # Frame format
//
// Every message is a frame:
//
//	[0:4]  StreamID — uint32 big-endian
//	[4:5]  Type     — 1 byte (see constants below)
//	[5:7]  Length   — uint16 big-endian, payload size (max 32 KiB)
//	[7:]   Payload  — Length bytes
//
// # OPEN payload (address encoding)
//
//	[0]    AddrType — 0x01=IPv4, 0x02=domain, 0x03=IPv6
//	IPv4:  [1:5] 4-byte IP, [5:7] port
//	domain:[1]   name_len, [2:2+len] name, [2+len:4+len] port
//	IPv6:  [1:17] 16-byte IP, [17:19] port
//
// # Stream lifecycle
//
//	Client                     Server
//	  OPEN ──────────────────▶
//	  DATA ──────────────────▶ (dials destination, buffers)
//	       ◀────────────────── DATA
//	  FIN  ──────────────────▶ (client done writing)
//	       ◀────────────────── FIN (server done writing)
//	  (stream removed from table on both sides)
//
// RST aborts the stream immediately from either side.
package mux

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
)

// Frame type constants.
const (
	TypeOpen = byte(0x01) // new stream; payload = address
	TypeData = byte(0x02) // data; payload = raw bytes
	TypeFIN  = byte(0x03) // half-close (write side done)
	TypeRST  = byte(0x04) // hard reset
)

const (
	hdrLen          = 7    // StreamID(4) + Type(1) + Length(2)
	maxPayload      = 32 * 1024
	streamRecvBuf   = 256 * 1024 // per-stream receive buffer (bytes)
)

// ─── Session ────────────────────────────────────────────────────────────────

// Session multiplexes many streams over a single net.Conn.
// One goroutine reads frames and dispatches; one goroutine serialises writes.
type Session struct {
	conn      net.Conn
	streams   sync.Map    // uint32 → *Stream
	acceptCh  chan *Stream // incoming streams (server side)
	sendCh    chan []byte  // serialised outgoing frames
	nextID    atomic.Uint32
	closeOnce sync.Once
	closeCh   chan struct{}
}

// NewClientSession wraps conn as a client-side mux session.
// Client streams use odd IDs starting at 1.
func NewClientSession(conn net.Conn) *Session {
	s := newSession(conn, true)
	return s
}

// NewServerSession wraps conn as a server-side mux session.
func NewServerSession(conn net.Conn) *Session {
	s := newSession(conn, false)
	return s
}

func newSession(conn net.Conn, isClient bool) *Session {
	s := &Session{
		conn:     conn,
		acceptCh: make(chan *Stream, 64),
		sendCh:   make(chan []byte, 512),
		closeCh:  make(chan struct{}),
	}
	if isClient {
		s.nextID.Store(1) // odd IDs: 1, 3, 5, …
	} else {
		s.nextID.Store(0) // nextID not used server-side for opens
	}
	go s.readLoop()
	go s.writeLoop()
	return s
}

// OpenStream opens a new stream to dest (e.g. "example.com:443").
// Returns immediately; data can be written before the server confirms.
func (s *Session) OpenStream(dest string) (*Stream, error) {
	// Claim the next odd ID.
	id := s.nextID.Add(2) - 2

	st := newStream(id, s)
	s.streams.Store(id, st)

	addrBytes, err := encodeAddr(dest)
	if err != nil {
		s.streams.Delete(id)
		return nil, fmt.Errorf("mux: encode addr %q: %w", dest, err)
	}

	if err := s.queueFrame(id, TypeOpen, addrBytes); err != nil {
		s.streams.Delete(id)
		return nil, err
	}
	return st, nil
}

// Accept blocks until a new incoming stream arrives (server side).
func (s *Session) Accept() (*Stream, error) {
	select {
	case st := <-s.acceptCh:
		return st, nil
	case <-s.closeCh:
		return nil, fmt.Errorf("mux: session closed")
	}
}

// Close tears down the session and all streams.
func (s *Session) Close() error {
	s.closeOnce.Do(func() {
		close(s.closeCh)
		s.conn.Close()
		// Wake up any blocked Accept callers.
		select {
		case s.acceptCh <- nil:
		default:
		}
	})
	return nil
}

func (s *Session) readLoop() {
	defer s.Close()
	hdr := make([]byte, hdrLen)
	for {
		if _, err := io.ReadFull(s.conn, hdr); err != nil {
			return
		}
		id := binary.BigEndian.Uint32(hdr[0:4])
		typ := hdr[4]
		plen := int(binary.BigEndian.Uint16(hdr[5:7]))

		var payload []byte
		if plen > 0 {
			if plen > maxPayload {
				return // protocol violation
			}
			payload = make([]byte, plen)
			if _, err := io.ReadFull(s.conn, payload); err != nil {
				return
			}
		}

		switch typ {
		case TypeOpen:
			// Server receives OPEN from client: create stream + push to accept queue.
			dest, err := decodeAddr(payload)
			if err != nil {
				s.queueFrame(id, TypeRST, nil) //nolint
				continue
			}
			st := newStream(id, s)
			st.dest = dest
			s.streams.Store(id, st)
			select {
			case s.acceptCh <- st:
			default:
				// Accept queue full — reject stream.
				s.streams.Delete(id)
				s.queueFrame(id, TypeRST, nil) //nolint
			}

		case TypeData:
			if v, ok := s.streams.Load(id); ok {
				v.(*Stream).feedData(payload)
			}

		case TypeFIN:
			if v, ok := s.streams.Load(id); ok {
				v.(*Stream).signalEOF()
			}

		case TypeRST:
			if v, ok := s.streams.Load(id); ok {
				st := v.(*Stream)
				s.streams.Delete(id)
				st.abort()
			}
		}
	}
}

func (s *Session) writeLoop() {
	for {
		select {
		case frame := <-s.sendCh:
			if _, err := s.conn.Write(frame); err != nil {
				s.Close()
				return
			}
		case <-s.closeCh:
			return
		}
	}
}

// queueFrame serialises a frame and puts it on the send queue.
func (s *Session) queueFrame(id uint32, typ byte, payload []byte) error {
	frame := make([]byte, hdrLen+len(payload))
	binary.BigEndian.PutUint32(frame[0:4], id)
	frame[4] = typ
	binary.BigEndian.PutUint16(frame[5:7], uint16(len(payload)))
	copy(frame[hdrLen:], payload)

	select {
	case s.sendCh <- frame:
		return nil
	case <-s.closeCh:
		return fmt.Errorf("mux: session closed")
	}
}

// ─── Stream ──────────────────────────────────────────────────────────────────

// Stream is one multiplexed connection inside a Session.
// It implements net.Conn (partially — LocalAddr/RemoteAddr return nil stubs).
type Stream struct {
	id   uint32
	sess *Session
	dest string // destination address (server side only)

	recvMu   sync.Mutex
	recvBuf  []byte
	recvEOF  bool
	recvAbrt bool
	recvCond *sync.Cond

	closeOnce sync.Once
	closeCh   chan struct{}
}

func newStream(id uint32, sess *Session) *Stream {
	st := &Stream{
		id:      id,
		sess:    sess,
		closeCh: make(chan struct{}),
	}
	st.recvCond = sync.NewCond(&st.recvMu)
	return st
}

// Dest returns the destination address parsed from the OPEN frame (server side).
func (st *Stream) Dest() string { return st.dest }

// Write sends data to the remote side via DATA frames.
// Frames are at most maxPayload bytes each.
func (st *Stream) Write(p []byte) (int, error) {
	total := 0
	for len(p) > 0 {
		chunk := p
		if len(chunk) > maxPayload {
			chunk = p[:maxPayload]
		}
		if err := st.sess.queueFrame(st.id, TypeData, chunk); err != nil {
			return total, err
		}
		total += len(chunk)
		p = p[len(chunk):]
	}
	return total, nil
}

// Read receives data from the remote side.
// Blocks until data is available, EOF (FIN), or the stream is aborted (RST).
func (st *Stream) Read(p []byte) (int, error) {
	st.recvMu.Lock()
	defer st.recvMu.Unlock()
	for len(st.recvBuf) == 0 && !st.recvEOF && !st.recvAbrt {
		st.recvCond.Wait()
	}
	if st.recvAbrt {
		return 0, fmt.Errorf("mux: stream reset")
	}
	if len(st.recvBuf) == 0 {
		return 0, io.EOF
	}
	n := copy(p, st.recvBuf)
	st.recvBuf = st.recvBuf[n:]
	return n, nil
}

// Close sends FIN and removes the stream from the session table.
func (st *Stream) Close() error {
	st.closeOnce.Do(func() {
		close(st.closeCh)
		st.sess.queueFrame(st.id, TypeFIN, nil) //nolint
		st.sess.streams.Delete(st.id)
		st.signalEOF()
	})
	return nil
}

// feedData is called by the session reader goroutine.
func (st *Stream) feedData(data []byte) {
	if len(data) == 0 {
		return
	}
	st.recvMu.Lock()
	st.recvBuf = append(st.recvBuf, data...)
	st.recvMu.Unlock()
	st.recvCond.Signal()
}

// signalEOF is called when a FIN frame arrives.
func (st *Stream) signalEOF() {
	st.recvMu.Lock()
	st.recvEOF = true
	st.recvMu.Unlock()
	st.recvCond.Broadcast()
}

// abort is called when an RST frame arrives.
func (st *Stream) abort() {
	st.recvMu.Lock()
	st.recvAbrt = true
	st.recvMu.Unlock()
	st.recvCond.Broadcast()
	st.closeOnce.Do(func() { close(st.closeCh) })
}

// Stub net.Conn methods — LocalAddr / RemoteAddr not meaningful for mux streams.
func (st *Stream) LocalAddr() net.Addr              { return stubAddr{} }
func (st *Stream) RemoteAddr() net.Addr             { return stubAddr{} }
func (st *Stream) SetDeadline(_ interface{}) error  { return nil }
func (st *Stream) SetReadDeadline(_ interface{}) error  { return nil }
func (st *Stream) SetWriteDeadline(_ interface{}) error { return nil }

type stubAddr struct{}

func (stubAddr) Network() string { return "mux" }
func (stubAddr) String() string  { return "mux-stream" }

// ─── Address encoding ────────────────────────────────────────────────────────

// encodeAddr converts "host:port" to the OPEN payload byte format.
func encodeAddr(hostport string) ([]byte, error) {
	host, portStr, err := net.SplitHostPort(hostport)
	if err != nil {
		return nil, err
	}
	port, err := net.LookupPort("tcp", portStr)
	if err != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("bad port %q", portStr)
	}
	portBytes := []byte{byte(port >> 8), byte(port)}

	if ip := net.ParseIP(host); ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			return append(append([]byte{0x01}, ip4...), portBytes...), nil
		}
		return append(append([]byte{0x03}, ip.To16()...), portBytes...), nil
	}
	// Domain name.
	if len(host) > 255 {
		return nil, fmt.Errorf("domain too long")
	}
	b := []byte{0x02, byte(len(host))}
	b = append(b, []byte(host)...)
	b = append(b, portBytes...)
	return b, nil
}

// decodeAddr parses an OPEN payload back to "host:port".
func decodeAddr(b []byte) (string, error) {
	if len(b) < 3 {
		return "", fmt.Errorf("addr too short")
	}
	switch b[0] {
	case 0x01: // IPv4
		if len(b) < 7 {
			return "", fmt.Errorf("ipv4 addr short")
		}
		ip := net.IP(b[1:5])
		port := int(b[5])<<8 | int(b[6])
		return fmt.Sprintf("%s:%d", ip, port), nil
	case 0x03: // IPv6
		if len(b) < 19 {
			return "", fmt.Errorf("ipv6 addr short")
		}
		ip := net.IP(b[1:17])
		port := int(b[17])<<8 | int(b[18])
		return fmt.Sprintf("[%s]:%d", ip, port), nil
	case 0x02: // domain
		if len(b) < 2 {
			return "", fmt.Errorf("domain len missing")
		}
		nameLen := int(b[1])
		if 2+nameLen+2 > len(b) {
			return "", fmt.Errorf("domain addr short")
		}
		name := string(b[2 : 2+nameLen])
		port := int(b[2+nameLen])<<8 | int(b[2+nameLen+1])
		return fmt.Sprintf("%s:%d", name, port), nil
	}
	return "", fmt.Errorf("unknown atype 0x%02x", b[0])
}
