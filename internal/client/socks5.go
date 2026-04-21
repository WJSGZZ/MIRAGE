package client

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
)

// ListenSocks5 starts a SOCKS5 proxy on addr.
// For each accepted connection it calls handleFunc(conn, dest) where dest is
// the "host:port" the client wants to reach.
// This function blocks until the listener fails.
func ListenSocks5(addr string, c *Client) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("socks5 listen %s: %w", addr, err)
	}
	log.Printf("miragec: SOCKS5 listening on %s", addr)
	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("miragec: socks5 accept: %v", err)
			continue
		}
		go handleSocks5(conn, c)
	}
}

// handleSocks5 handles one SOCKS5 client connection end-to-end.
func handleSocks5(conn net.Conn, c *Client) {
	defer conn.Close()

	dest, err := socks5Handshake(conn)
	if err != nil {
		log.Printf("miragec: socks5 [%s]: %v", conn.RemoteAddr(), err)
		return
	}

	st, err := c.Dial(dest)
	if err != nil {
		// Tell the SOCKS5 client: Host Unreachable (0x04).
		socks5Reply(conn, 0x04)
		log.Printf("miragec: dial %s: %v", dest, err)
		return
	}

	// Tell the SOCKS5 client: success (0x00).
	if err := socks5Reply(conn, 0x00); err != nil {
		st.Close()
		return
	}

	Relay(conn, st)
}

// socks5Handshake performs the SOCKS5 negotiation and returns the destination.
//
// SOCKS5 RFC 1928:
//  1. Greeting:   VER(1=5) NMETHODS(1) METHODS(N)
//  2. Choice:     VER(1=5) METHOD(1=0 no-auth)
//  3. Request:    VER(1=5) CMD(1) RSV(1=0) ATYP(1) DST.ADDR DST.PORT(2)
//  4. Reply:      VER(1=5) REP(1) RSV(1=0) ATYP(1=1) BND.ADDR(4=0) BND.PORT(2=0)
func socks5Handshake(conn net.Conn) (dest string, err error) {
	// ── 1. Greeting ──────────────────────────────────────────────────────────
	hdr := make([]byte, 2)
	if _, err = io.ReadFull(conn, hdr); err != nil {
		return "", fmt.Errorf("read greeting: %w", err)
	}
	if hdr[0] != 5 {
		return "", fmt.Errorf("not SOCKS5 (ver=%d)", hdr[0])
	}
	methods := make([]byte, int(hdr[1]))
	if _, err = io.ReadFull(conn, methods); err != nil {
		return "", fmt.Errorf("read methods: %w", err)
	}
	// We only support no-auth (0x00).
	hasNoAuth := false
	for _, m := range methods {
		if m == 0x00 {
			hasNoAuth = true
		}
	}
	if !hasNoAuth {
		conn.Write([]byte{5, 0xFF}) //nolint — tell client: no acceptable method
		return "", fmt.Errorf("no acceptable auth method")
	}

	// ── 2. No-auth selection ──────────────────────────────────────────────────
	if _, err = conn.Write([]byte{5, 0x00}); err != nil {
		return "", fmt.Errorf("write method selection: %w", err)
	}

	// ── 3. Request ───────────────────────────────────────────────────────────
	reqHdr := make([]byte, 4)
	if _, err = io.ReadFull(conn, reqHdr); err != nil {
		return "", fmt.Errorf("read request: %w", err)
	}
	if reqHdr[0] != 5 {
		return "", fmt.Errorf("request ver %d", reqHdr[0])
	}
	if reqHdr[1] != 0x01 { // only CONNECT
		conn.Write([]byte{5, 0x07, 0, 1, 0, 0, 0, 0, 0, 0}) //nolint — Command not supported
		return "", fmt.Errorf("unsupported command 0x%02x", reqHdr[1])
	}

	var host string
	switch reqHdr[3] { // ATYP
	case 0x01: // IPv4
		addr := make([]byte, 4)
		if _, err = io.ReadFull(conn, addr); err != nil {
			return "", err
		}
		host = net.IP(addr).String()
	case 0x04: // IPv6
		addr := make([]byte, 16)
		if _, err = io.ReadFull(conn, addr); err != nil {
			return "", err
		}
		host = "[" + net.IP(addr).String() + "]"
	case 0x03: // domain
		lenB := make([]byte, 1)
		if _, err = io.ReadFull(conn, lenB); err != nil {
			return "", err
		}
		dom := make([]byte, int(lenB[0]))
		if _, err = io.ReadFull(conn, dom); err != nil {
			return "", err
		}
		host = string(dom)
	default:
		return "", fmt.Errorf("unknown atyp 0x%02x", reqHdr[3])
	}

	portB := make([]byte, 2)
	if _, err = io.ReadFull(conn, portB); err != nil {
		return "", err
	}
	port := binary.BigEndian.Uint16(portB)

	return fmt.Sprintf("%s:%d", host, port), nil
}

// socks5Reply sends the SOCKS5 response with the given REP code.
// On success (rep=0x00) it also sets the bound address/port to 0.
func socks5Reply(conn net.Conn, rep byte) error {
	// VER REP RSV ATYP BND.ADDR(4) BND.PORT(2)
	reply := []byte{5, rep, 0, 1, 0, 0, 0, 0, 0, 0}
	_, err := conn.Write(reply)
	return err
}
