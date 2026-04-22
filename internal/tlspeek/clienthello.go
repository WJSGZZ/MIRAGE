// Package tlspeek parses the first TLS ClientHello from a raw TCP connection.
package tlspeek

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"
)

const (
	recordTypeHandshake = 0x16
	handshakeTypeHello  = 0x01
	maxRecordLen        = 1<<14 + 2048
	maxBufferedHello    = 64 * 1024
)

type ClientHello struct {
	Raw        []byte
	Random     [32]byte
	SessionID  []byte
	ServerName string
}

func ReadClientHello(conn net.Conn, deadline time.Duration) (*ClientHello, error) {
	if deadline > 0 {
		_ = conn.SetReadDeadline(time.Now().Add(deadline))
		defer conn.SetReadDeadline(time.Time{})
	}

	var raw []byte
	var handshake []byte
	for len(handshake) < 4 {
		rec, payload, err := readRecord(conn)
		if err != nil {
			return nil, err
		}
		raw = append(raw, rec...)
		if payload != nil {
			handshake = append(handshake, payload...)
		}
		if len(raw) > maxBufferedHello {
			return nil, fmt.Errorf("tlspeek: clienthello exceeds %d bytes", maxBufferedHello)
		}
	}

	if handshake[0] != handshakeTypeHello {
		return nil, fmt.Errorf("tlspeek: first handshake message is 0x%02x, not client_hello", handshake[0])
	}
	bodyLen := int(handshake[1])<<16 | int(handshake[2])<<8 | int(handshake[3])
	totalLen := 4 + bodyLen
	for len(handshake) < totalLen {
		rec, payload, err := readRecord(conn)
		if err != nil {
			return nil, err
		}
		raw = append(raw, rec...)
		if payload != nil {
			handshake = append(handshake, payload...)
		}
		if len(raw) > maxBufferedHello {
			return nil, fmt.Errorf("tlspeek: clienthello exceeds %d bytes", maxBufferedHello)
		}
	}

	ch, err := parseClientHelloBody(handshake[4:totalLen])
	if err != nil {
		return nil, err
	}
	ch.Raw = append([]byte(nil), raw...)
	return ch, nil
}

func readRecord(r io.Reader) ([]byte, []byte, error) {
	var hdr [5]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, nil, err
	}
	length := int(binary.BigEndian.Uint16(hdr[3:5]))
	if length <= 0 || length > maxRecordLen {
		return nil, nil, fmt.Errorf("tlspeek: invalid record length %d", length)
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, nil, err
	}
	record := append(hdr[:], payload...)
	if hdr[0] != recordTypeHandshake {
		return record, nil, fmt.Errorf("tlspeek: first record type 0x%02x is not handshake", hdr[0])
	}
	return record, payload, nil
}

func parseClientHelloBody(body []byte) (*ClientHello, error) {
	// legacy_version(2) + random(32) + session_id_len(1)
	if len(body) < 35 {
		return nil, fmt.Errorf("tlspeek: clienthello too short")
	}
	ch := &ClientHello{}
	copy(ch.Random[:], body[2:34])

	pos := 34
	sidLen := int(body[pos])
	pos++
	if pos+sidLen > len(body) {
		return nil, fmt.Errorf("tlspeek: session_id truncated")
	}
	ch.SessionID = append([]byte(nil), body[pos:pos+sidLen]...)
	pos += sidLen

	if pos+2 > len(body) {
		return nil, fmt.Errorf("tlspeek: cipher suites length truncated")
	}
	csLen := int(binary.BigEndian.Uint16(body[pos : pos+2]))
	pos += 2 + csLen
	if pos > len(body) {
		return nil, fmt.Errorf("tlspeek: cipher suites truncated")
	}

	if pos+1 > len(body) {
		return nil, fmt.Errorf("tlspeek: compression methods truncated")
	}
	compLen := int(body[pos])
	pos += 1 + compLen
	if pos > len(body) {
		return nil, fmt.Errorf("tlspeek: compression methods truncated")
	}

	if pos == len(body) {
		return ch, nil
	}
	if pos+2 > len(body) {
		return nil, fmt.Errorf("tlspeek: extensions length truncated")
	}
	extLen := int(binary.BigEndian.Uint16(body[pos : pos+2]))
	pos += 2
	if pos+extLen > len(body) {
		return nil, fmt.Errorf("tlspeek: extensions truncated")
	}
	exts := body[pos : pos+extLen]
	ch.ServerName = parseSNI(exts)
	return ch, nil
}

func parseSNI(exts []byte) string {
	for pos := 0; pos+4 <= len(exts); {
		extType := binary.BigEndian.Uint16(exts[pos : pos+2])
		extLen := int(binary.BigEndian.Uint16(exts[pos+2 : pos+4]))
		pos += 4
		if pos+extLen > len(exts) {
			return ""
		}
		if extType == 0x0000 {
			data := exts[pos : pos+extLen]
			if len(data) < 5 {
				return ""
			}
			listLen := int(binary.BigEndian.Uint16(data[0:2]))
			if 2+listLen > len(data) || data[2] != 0x00 || len(data) < 5 {
				return ""
			}
			nameLen := int(binary.BigEndian.Uint16(data[3:5]))
			if 5+nameLen > len(data) {
				return ""
			}
			return string(data[5 : 5+nameLen])
		}
		pos += extLen
	}
	return ""
}
