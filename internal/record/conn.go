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
	"time"

	"miraged/internal/protocol"
)

const (
	TypeData      byte = 0x01
	TypePadding   byte = 0x02
	TypeHeartbeat byte = 0x03

	maxDataPayload = 16383
	frameHeaderLen = 3
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

	closeOnce sync.Once
	closeCh   chan struct{}
}

func NewConn(raw net.Conn, seed []byte) (*Conn, error) {
	if raw == nil {
		return nil, fmt.Errorf("record: nil conn")
	}
	if len(seed) != 16 {
		return nil, fmt.Errorf("record: seed must be 16 bytes")
	}

	var seedArr [16]byte
	copy(seedArr[:], seed)
	params, err := protocol.DerivePaddingParams(seed[:], seed[:])
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
		raw:     raw,
		rng:     mathrand.New(mathrand.NewSource(rngSeed)),
		params:  params,
		seed:    seedArr,
		closeCh: make(chan struct{}),
	}
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
		err = c.raw.Close()
	})
	return err
}

func (c *Conn) LocalAddr() net.Addr                { return c.raw.LocalAddr() }
func (c *Conn) RemoteAddr() net.Addr               { return c.raw.RemoteAddr() }
func (c *Conn) SetDeadline(t time.Time) error      { return c.raw.SetDeadline(t) }
func (c *Conn) SetReadDeadline(t time.Time) error  { return c.raw.SetReadDeadline(t) }
func (c *Conn) SetWriteDeadline(t time.Time) error { return c.raw.SetWriteDeadline(t) }

func (c *Conn) heartbeatLoop() {
	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.writeMu.Lock()
			_ = c.writeFrame(TypeHeartbeat, nil)
			c.writeMu.Unlock()
		case <-c.closeCh:
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
	if length < 0 || length > maxDataPayload {
		return 0, nil, fmt.Errorf("record: invalid frame length %d", length)
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(c.raw, payload); err != nil {
		return 0, nil, err
	}
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

	type span struct{ min, max int }
	regions := []span{
		{min: 256, max: 1200},
		{min: 1201, max: 4096},
		{min: 4097, max: 12000},
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
