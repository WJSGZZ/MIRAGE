// Package auth implements MIRAGE's post-handshake authentication.
//
// # Why post-handshake instead of session_id injection
//
// Xray Reality embeds the auth token in the TLS ClientHello session_id field,
// which requires the client to control the TLS ephemeral key pair — only
// possible through uTLS internals. MIRAGE uses a simpler, equally secure
// mechanism: exchange auth bytes over the already-encrypted TLS channel.
// GFW cannot see the auth bytes (they're inside TLS); an active prober who
// completes the TLS handshake only sees the server close the connection (or
// return an HTTP response), indistinguishable from any HTTPS server that
// rejects unrecognised requests.
//
// # Wire format
//
// Client → Server, exactly 80 bytes immediately after TLS handshake:
//
//	[0:32]  auth_pub   — client's ephemeral X25519 public key
//	[32:40] timestamp  — big-endian int64 Unix seconds
//	[40:48] short_id   — user ShortID bytes, zero-padded to 8 bytes
//	[48:80] mac        — 32-byte BLAKE3 derive_key output (see below)
//
// mac = BLAKE3.derive_key(
//
//	context  = "MIRAGE-v1-client-auth",
//	material = ecdhe_secret || timestamp_bytes || short_id_padded,
//
// )
//
// where ecdhe_secret = X25519(client_auth_priv, server_long_pub)
//
// Server → Client, 1 byte:
//
//	0x00 = auth OK, enter mux mode
//
// On failure the server closes the connection (optionally after an HTTP 400).
package auth

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/zeebo/blake3"
)

const (
	MsgLen      = 80 // total bytes the client sends
	authContext = "MIRAGE-v1-client-auth"
)

// ClientMsg is the 80-byte authentication message sent by the client.
type ClientMsg [MsgLen]byte

// BuildClientMsg constructs the authentication message.
//
//   - serverPub: server's long-term X25519 public key (from config)
//   - shortID:   the user's ShortID bytes (1–8 bytes)
func BuildClientMsg(serverPub *ecdh.PublicKey, shortID []byte) (ClientMsg, error) {
	var msg ClientMsg

	// Generate a fresh ephemeral X25519 key pair for auth ECDHE.
	authPriv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return msg, fmt.Errorf("auth: keygen: %w", err)
	}
	authPub := authPriv.PublicKey().Bytes() // 32 bytes

	// ECDHE shared secret.
	ecdheSecret, err := authPriv.ECDH(serverPub)
	if err != nil {
		return msg, fmt.Errorf("auth: ecdh: %w", err)
	}

	// Timestamp (8 bytes, big-endian int64).
	ts := time.Now().Unix()
	var tsBytes [8]byte
	binary.BigEndian.PutUint64(tsBytes[:], uint64(ts))

	// ShortID zero-padded to 8 bytes.
	var shortIDPad [8]byte
	copy(shortIDPad[:], shortID)

	// MAC = BLAKE3 derive_key over (ecdhe_secret || timestamp || shortId_padded).
	mac := deriveMAC(ecdheSecret, tsBytes[:], shortIDPad[:])

	// Pack into wire message.
	copy(msg[0:32], authPub)
	copy(msg[32:40], tsBytes[:])
	copy(msg[40:48], shortIDPad[:])
	copy(msg[48:80], mac[:])

	return msg, nil
}

// VerifyClientMsg verifies the 80-byte auth message from the client.
// Returns the matching ShortID bytes on success, or an error.
//
//   - serverPriv:  server's long-term X25519 private key
//   - candidates:  list of (shortIDBytes, maxTimeDiff) pairs to try
func VerifyClientMsg(
	msg ClientMsg,
	serverPriv *ecdh.PrivateKey,
	shortIDs [][]byte,
	maxTimeDiff int,
) ([]byte, error) {
	// Extract fields.
	authPubBytes := msg[0:32]
	tsBytes := msg[32:40]
	shortIDPad := msg[40:48]
	receivedMAC := msg[48:80]

	// Parse client's ephemeral public key.
	clientPub, err := ecdh.X25519().NewPublicKey(authPubBytes)
	if err != nil {
		return nil, fmt.Errorf("auth: bad auth_pub: %w", err)
	}

	// Server-side ECDHE.
	ecdheSecret, err := serverPriv.ECDH(clientPub)
	if err != nil {
		return nil, fmt.Errorf("auth: ecdh: %w", err)
	}

	// Verify timestamp.
	ts := int64(binary.BigEndian.Uint64(tsBytes))
	now := time.Now().Unix()
	if math.Abs(float64(now-ts)) > float64(maxTimeDiff) {
		return nil, fmt.Errorf("auth: timestamp skew %ds exceeds limit %ds", now-ts, maxTimeDiff)
	}

	// Recompute expected MAC.
	expectedMAC := deriveMAC(ecdheSecret, tsBytes, shortIDPad[:])

	// Constant-time comparison to prevent timing oracle.
	if !equal32(receivedMAC, expectedMAC[:]) {
		return nil, fmt.Errorf("auth: MAC mismatch")
	}

	// Identify which user by matching shortIDPad against known users.
	for _, sid := range shortIDs {
		var padded [8]byte
		copy(padded[:], sid)
		if equal8(shortIDPad, padded[:]) {
			return sid, nil
		}
	}
	return nil, fmt.Errorf("auth: unknown shortId")
}

// ReadAndVerify reads exactly MsgLen bytes from r, then calls VerifyClientMsg.
func ReadAndVerify(
	r io.Reader,
	serverPriv *ecdh.PrivateKey,
	shortIDs [][]byte,
	maxTimeDiff int,
) ([]byte, error) {
	var msg ClientMsg
	if _, err := io.ReadFull(r, msg[:]); err != nil {
		return nil, fmt.Errorf("auth: read msg: %w", err)
	}
	return VerifyClientMsg(msg, serverPriv, shortIDs, maxTimeDiff)
}

// SendAndVerify sends the client auth message to w, then reads the 1-byte ACK.
func SendAndVerify(w io.ReadWriter, serverPub *ecdh.PublicKey, shortID []byte) error {
	msg, err := BuildClientMsg(serverPub, shortID)
	if err != nil {
		return err
	}
	if _, err := w.Write(msg[:]); err != nil {
		return fmt.Errorf("auth: send msg: %w", err)
	}
	var ack [1]byte
	if _, err := io.ReadFull(w, ack[:]); err != nil {
		return fmt.Errorf("auth: read ack: %w", err)
	}
	if ack[0] != 0x00 {
		return fmt.Errorf("auth: server rejected (ack=0x%02x)", ack[0])
	}
	return nil
}

// deriveMAC computes BLAKE3.derive_key("MIRAGE-v1-client-auth", material...).
func deriveMAC(ecdheSecret, tsBytes, shortIDPad []byte) [32]byte {
	h := blake3.NewDeriveKey(authContext)
	h.Write(ecdheSecret)
	h.Write(tsBytes)
	h.Write(shortIDPad)
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// equal32 is a constant-time 32-byte comparison.
func equal32(a, b []byte) bool {
	if len(a) != 32 || len(b) != 32 {
		return false
	}
	var diff byte
	for i := 0; i < 32; i++ {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

// equal8 is a constant-time 8-byte comparison.
func equal8(a, b []byte) bool {
	if len(a) != 8 || len(b) != 8 {
		return false
	}
	var diff byte
	for i := 0; i < 8; i++ {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}
