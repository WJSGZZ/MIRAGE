// Package record implements the MIRAGE record layer that sits between TLS and Yamux.
package record

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	mathrand "math/rand"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"miraged/internal/protocol"
)

const (
	TypeData      byte = 0x01
	TypePadding   byte = 0x02
	TypeHeartbeat byte = 0x03

	maxDataPayload = 16383
	frameHeaderLen = 3

	deadPeerTimeout = 90 * time.Second
	deadPeerCheck   = 10 * time.Second
)

type Conn struct {
	raw net.Conn

	readMu  sync.Mutex
	readBuf []byte

	writeMu sync.Mutex
	rngMu   sync.Mutex
	rng     *mathrand.Rand
	params  protocol.PadParams
	seed    [16]byte

	frameCount uint32

	// lastSeen tracks the unix timestamp of the most recent received frame.
	// Updated by readFrame; checked by heartbeatLoop for dead-peer detection.
	lastSeen atomic.Int64

	closeOnce     sync.Once
	closeCh       chan struct{}
	heartbeatDone chan struct{}
}

// NewConn wraps raw in a MIRAGE record-layer connection. psk is the user's
// pre-shared key used to derive padding parameters (may be nil for legacy
// connections, which will use default params). seed must be exactly 16 bytes.
func NewConn(raw net.Conn, psk, seed []byte) (*Conn, error) {
	if raw == nil {
		return nil, fmt.Errorf("record: nil conn")
	}
	if len(seed) != 16 {
		return nil, fmt.Errorf("record: seed must be 16 bytes")
	}

	var seedArr [16]byte
	copy(seedArr[:], seed)
	params, err := protocol.DerivePaddingParams(psk, seed)
	if err != nil {
		params = protocol.PadParams{
			PaddingMin: 0,
			PaddingMax: 64,
			TriggerN:   4,
			InsertProb: 20,
		}
	}

	rngSeed := int64(binary.BigEndian.Uint64(seedArr[:8]) ^ binary.BigEndian.Uint64(seedArr[8:]))
	if rngSeed == 0 {
		rngSeed = 1
	}

	c := &Conn{
		raw:           raw,
		rng:           mathrand.New(mathrand.NewSource(rngSeed)),
		params:        params,
		seed:          seedArr,
		closeCh:       make(chan struct{}),
		heartbeatDone: make(chan struct{}),
	}
	c.lastSeen.Store(time.Now().Unix())
	go c.heartbeatLoop()
	return c, nil
}

func (c *Conn) Read(p []byte) (int, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()

	for len(c.readBuf) == 0 {
		typ, payload, err := c.readFrame()
		if err != nil {
			return 0, err
		}
		switch typ {
		case TypeData:
			c.readBuf = append(c.readBuf, payload...)
		case TypePadding, TypeHeartbeat:
			// Discard silently.
		default:
			// Unknown types are ignored to preserve stream continuity.
		}
	}

	n := copy(p, c.readBuf)
	c.readBuf = c.readBuf[n:]
	return n, nil
}

func (c *Conn) Write(p []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	total := 0
	remaining := p
	for len(remaining) > 0 {
		chunkLen := c.nextChunkSize(len(remaining))
		chunk := remaining[:chunkLen]
		if err := c.writeFrame(TypeData, chunk); err != nil {
			return total, err
		}
		total += len(chunk)
		remaining = remaining[chunkLen:]

		c.frameCount++
		if c.shouldInsertPadding() {
			if err := c.writePaddingFrame(); err != nil {
				return total, err
			}
		}
	}
	return total, nil
}

func (c *Conn) Close() error {
	var err error
	c.closeOnce.Do(func() {
		close(c.closeCh)
		// Close raw conn first so any in-progress heartbeat write unblocks with
		// an error; otherwise <-heartbeatDone would deadlock waiting for a write
		// that can never complete.
		err = c.raw.Close()
		<-c.heartbeatDone // wait for goroutine to confirm exit
	})
	return err
}

func (c *Conn) LocalAddr() net.Addr                { return c.raw.LocalAddr() }
func (c *Conn) RemoteAddr() net.Addr               { return c.raw.RemoteAddr() }
func (c *Conn) SetDeadline(t time.Time) error      { return c.raw.SetDeadline(t) }
func (c *Conn) SetReadDeadline(t time.Time) error  { return c.raw.SetReadDeadline(t) }
func (c *Conn) SetWriteDeadline(t time.Time) error { return c.raw.SetWriteDeadline(t) }

func (c *Conn) heartbeatLoop() {
	defer close(c.heartbeatDone)
	deadCheck := time.NewTicker(deadPeerCheck)
	defer deadCheck.Stop()
	for {
		// Random interval 25~35 s (spec §8.5)
		interval := 25*time.Second + time.Duration(c.randInt(10001))*time.Millisecond
		t := time.NewTimer(interval)
		select {
		case <-t.C:
			// Payload: 16~128 crypto/rand bytes (length itself is part of obfuscation)
			size := 16 + c.randInt(113)
			payload := make([]byte, size)
			if _, err := rand.Read(payload); err != nil {
				return
			}
			c.writeMu.Lock()
			_ = c.writeFrame(TypeHeartbeat, payload)
			c.writeMu.Unlock()

		case <-deadCheck.C:
			t.Stop()
			// Dead peer detection (spec §8.5): close if no frame received for 90 s.
			if time.Duration(time.Now().Unix()-c.lastSeen.Load())*time.Second > deadPeerTimeout {
				_ = c.raw.Close()
				return
			}

		case <-c.closeCh:
			t.Stop()
			return
		}
	}
}

func (c *Conn) readFrame() (byte, []byte, error) {
	var hdr [frameHeaderLen]byte
	if _, err := io.ReadFull(c.raw, hdr[:]); err != nil {
		return 0, nil, err
	}
	length := int(binary.BigEndian.Uint16(hdr[0:2]))
	if length > maxDataPayload {
		return 0, nil, fmt.Errorf("record: invalid frame length %d", length)
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(c.raw, payload); err != nil {
		return 0, nil, err
	}
	// Any valid frame updates the dead-peer timestamp (spec §8.5).
	c.lastSeen.Store(time.Now().Unix())
	return hdr[2], payload, nil
}

func (c *Conn) writeFrame(typ byte, payload []byte) error {
	if len(payload) > maxDataPayload {
		return fmt.Errorf("record: payload too large: %d", len(payload))
	}
	var hdr [frameHeaderLen]byte
	binary.BigEndian.PutUint16(hdr[0:2], uint16(len(payload)))
	hdr[2] = typ
	if _, err := c.raw.Write(hdr[:]); err != nil {
		return err
	}
	if len(payload) == 0 {
		return nil
	}
	_, err := c.raw.Write(payload)
	return err
}

func (c *Conn) writePaddingFrame() error {
	min := int(c.params.PaddingMin)
	max := int(c.params.PaddingMax)
	if max < min {
		max = min
	}
	if max > 255 {
		max = 255
	}
	size := min
	if max > min {
		size += c.randInt(max-min+1)
	}
	if size == 0 {
		return c.writeFrame(TypePadding, nil)
	}
	padding := make([]byte, size)
	if _, err := rand.Read(padding); err != nil {
		return err
	}
	return c.writeFrame(TypePadding, padding)
}

func (c *Conn) shouldInsertPadding() bool {
	if c.frameCount < c.params.TriggerN {
		return true
	}
	if c.params.InsertProb == 0 {
		return false
	}
	return uint32(c.randInt(100)) < c.params.InsertProb
}

func (c *Conn) nextChunkSize(available int) int {
	if available <= 0 {
		return 0
	}
	if available == 1 {
		return 1
	}
	limit := available
	if limit > maxDataPayload {
		limit = maxDataPayload
	}

	// Spec §8.3: Chrome-reference distribution (weights 35/40/25).
	type span struct{ min, max int }
	regions := []span{
		{min: 100, max: 1400},
		{min: 1400, max: 8192},
		{min: 8192, max: 16383},
	}
	weights := []int{35, 40, 25}

	pick := c.randInt(100)
	region := regions[0]
	switch {
	case pick < weights[0]:
		region = regions[0]
	case pick < weights[0]+weights[1]:
		region = regions[1]
	default:
		region = regions[2]
	}

	if limit < region.min {
		return limit
	}
	baseMax := region.max
	if baseMax > limit {
		baseMax = limit
	}
	base := region.min
	if baseMax > region.min {
		base += c.randInt(baseMax-region.min+1)
	}
	jitter := (float64(c.randInt(2001)) / 10000.0) - 0.10
	size := int(math.Floor(float64(base) * (1 + jitter)))
	if size < 1 {
		size = 1
	}
	if size > limit {
		size = limit
	}
	return size
}

func (c *Conn) randInt(n int) int {
	if n <= 1 {
		return 0
	}
	c.rngMu.Lock()
	defer c.rngMu.Unlock()
	return c.rng.Intn(n)
}
