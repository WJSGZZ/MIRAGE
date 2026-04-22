package client

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
)

// ListenSocks5 starts a local proxy listener on addr and blocks until it fails.
// Despite the name, the listener accepts both SOCKS5 and SOCKS4/4a clients so
// Windows system-proxy users still work.
func ListenSocks5(addr string, c *Client) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("socks5 listen %s: %w", addr, err)
	}
	log.Printf("miragec: SOCKS proxy listening on %s", addr)
	Serve(ln, c)
	return nil
}

// Serve accepts connections from an existing listener and proxies them through c.
// Returns when ln is closed.
func Serve(ln net.Listener, c *Client) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go handleProxyConn(conn, c)
	}
}

// handleProxyConn handles one SOCKS client connection end-to-end.
func handleProxyConn(conn net.Conn, c *Client) {
	defer conn.Close()

	dest, reply, err := proxyHandshake(conn)
	if err != nil {
		log.Printf("miragec: proxy [%s]: %v", conn.RemoteAddr(), err)
		return
	}

	st, err := c.Dial(dest)
	if err != nil {
		reply(0x04)
		log.Printf("miragec: dial %s: %v", dest, err)
		return
	}

	if err := reply(0x00); err != nil {
		st.Close()
		return
	}

	Relay(conn, st)
}

func proxyHandshake(conn net.Conn) (dest string, reply func(byte) error, err error) {
	var ver [1]byte
	if _, err := io.ReadFull(conn, ver[:]); err != nil {
		return "", nil, fmt.Errorf("read version: %w", err)
	}

	switch ver[0] {
	case 0x05:
		dest, err := socks5Handshake(conn, ver[0])
		return dest, socks5Reply(conn), err
	case 0x04:
		dest, err := socks4Handshake(conn)
		return dest, socks4Reply(conn), err
	default:
		return "", nil, fmt.Errorf("unsupported proxy version %d", ver[0])
	}
}

// socks5Handshake performs the SOCKS5 negotiation and returns the destination.
//
// SOCKS5 RFC 1928:
//  1. Greeting:   VER(1=5) NMETHODS(1) METHODS(N)
//  2. Choice:     VER(1=5) METHOD(1=0 no-auth)
//  3. Request:    VER(1=5) CMD(1) RSV(1=0) ATYP(1) DST.ADDR DST.PORT(2)
//  4. Reply:      VER(1=5) REP(1) RSV(1=0) ATYP(1=1) BND.ADDR(4=0) BND.PORT(2=0)
func socks5Handshake(conn net.Conn, ver byte) (dest string, err error) {
	nMethods := make([]byte, 1)
	if _, err = io.ReadFull(conn, nMethods); err != nil {
		return "", fmt.Errorf("read nmethods: %w", err)
	}
	methods := make([]byte, int(nMethods[0]))
	if _, err = io.ReadFull(conn, methods); err != nil {
		return "", fmt.Errorf("read methods: %w", err)
	}

	hasNoAuth := false
	for _, m := range methods {
		if m == 0x00 {
			hasNoAuth = true
			break
		}
	}
	if !hasNoAuth {
		conn.Write([]byte{ver, 0xFF}) //nolint:errcheck
		return "", fmt.Errorf("no acceptable auth method")
	}

	if _, err = conn.Write([]byte{ver, 0x00}); err != nil {
		return "", fmt.Errorf("write method selection: %w", err)
	}

	reqHdr := make([]byte, 4)
	if _, err = io.ReadFull(conn, reqHdr); err != nil {
		return "", fmt.Errorf("read request: %w", err)
	}
	if reqHdr[0] != ver {
		return "", fmt.Errorf("request ver %d", reqHdr[0])
	}
	if reqHdr[1] != 0x01 {
		conn.Write([]byte{ver, 0x07, 0, 1, 0, 0, 0, 0, 0, 0}) //nolint:errcheck
		return "", fmt.Errorf("unsupported command 0x%02x", reqHdr[1])
	}

	var host string
	switch reqHdr[3] {
	case 0x01:
		addr := make([]byte, 4)
		if _, err = io.ReadFull(conn, addr); err != nil {
			return "", err
		}
		host = net.IP(addr).String()
	case 0x04:
		addr := make([]byte, 16)
		if _, err = io.ReadFull(conn, addr); err != nil {
			return "", err
		}
		host = "[" + net.IP(addr).String() + "]"
	case 0x03:
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

// socks4Handshake performs a SOCKS4/SOCKS4a CONNECT negotiation.
func socks4Handshake(conn net.Conn) (dest string, err error) {
	reqHdr := make([]byte, 7)
	if _, err = io.ReadFull(conn, reqHdr); err != nil {
		return "", fmt.Errorf("read SOCKS4 request: %w", err)
	}
	if reqHdr[0] != 0x01 {
		return "", fmt.Errorf("unsupported SOCKS4 command 0x%02x", reqHdr[0])
	}

	port := binary.BigEndian.Uint16(reqHdr[1:3])
	host := net.IP(reqHdr[3:7]).String()

	if _, err := readNullTerminated(conn); err != nil {
		return "", fmt.Errorf("read SOCKS4 user id: %w", err)
	}

	if reqHdr[3] == 0x00 && reqHdr[4] == 0x00 && reqHdr[5] == 0x00 && reqHdr[6] != 0x00 {
		name, err := readNullTerminated(conn)
		if err != nil {
			return "", fmt.Errorf("read SOCKS4a host: %w", err)
		}
		if len(name) == 0 {
			return "", fmt.Errorf("empty SOCKS4a host")
		}
		host = string(name)
	}

	return fmt.Sprintf("%s:%d", host, port), nil
}

func readNullTerminated(r io.Reader) ([]byte, error) {
	var buf bytes.Buffer
	var b [1]byte
	for {
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return nil, err
		}
		if b[0] == 0x00 {
			return buf.Bytes(), nil
		}
		buf.WriteByte(b[0])
	}
}

// socks5Reply sends the SOCKS5 response with the given REP code.
func socks5Reply(conn net.Conn) func(byte) error {
	return func(rep byte) error {
		reply := []byte{5, rep, 0, 1, 0, 0, 0, 0, 0, 0}
		_, err := conn.Write(reply)
		return err
	}
}

// socks4Reply sends a SOCKS4 response.
func socks4Reply(conn net.Conn) func(byte) error {
	return func(rep byte) error {
		status := byte(0x5A)
		if rep != 0x00 {
			status = 0x5B
		}
		_, err := conn.Write([]byte{0x00, status, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
		return err
	}
}
