package protocol

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/zeebo/blake3"
)

const (
	ProtocolVersion byte = 0x01

	UIDContext          = "MIRAGE v1 user identity 2026"
	PaddingContext      = "MIRAGE v1 padding params 2026"
	SessionWindowSecs   = int64(30)
	ReplayTTLSeconds    = int64(90)
)

type PadParams struct {
	PaddingMin uint32
	PaddingMax uint32
	TriggerN   uint32
	InsertProb uint32
}

func DeriveUID(psk []byte) ([4]byte, error) {
	var uid [4]byte
	if len(psk) == 0 {
		return uid, fmt.Errorf("derive uid: empty psk")
	}
	h := blake3.NewDeriveKey(UIDContext)
	_, _ = h.Write(psk)
	copy(uid[:], h.Sum(nil))
	return uid, nil
}

func DerivePaddingParams(psk, seed []byte) (PadParams, error) {
	var params PadParams
	if len(psk) == 0 {
		return params, fmt.Errorf("derive padding params: empty psk")
	}
	if len(seed) != 16 {
		return params, fmt.Errorf("derive padding params: seed must be 16 bytes")
	}

	h := blake3.NewDeriveKey(PaddingContext)
	_, _ = h.Write(psk)
	_, _ = h.Write(seed)
	sum := h.Sum(nil)
	if len(sum) < 16 {
		return params, fmt.Errorf("derive padding params: short output")
	}

	params.PaddingMin = binary.BigEndian.Uint32(sum[0:4]) % 128
	params.PaddingMax = params.PaddingMin + (binary.BigEndian.Uint32(sum[4:8]) % (256 - params.PaddingMin))
	params.TriggerN = 3 + (binary.BigEndian.Uint32(sum[8:12]) % 5)
	params.InsertProb = binary.BigEndian.Uint32(sum[12:16]) % 101
	return params, nil
}

func TimeWindow(unixSeconds int64) int64 {
	if unixSeconds < 0 {
		return 0
	}
	return unixSeconds / SessionWindowSecs
}

func DeriveHMACToken(psk []byte, timeWindow int64, clientRandom [32]byte) ([28]byte, error) {
	var out [28]byte
	if len(psk) == 0 {
		return out, fmt.Errorf("derive hmac token: empty psk")
	}
	var msg [40]byte
	binary.BigEndian.PutUint64(msg[0:8], uint64(timeWindow))
	copy(msg[8:], clientRandom[:])
	mac := hmacSHA256(psk, msg[:])
	copy(out[:], mac[:28])
	return out, nil
}

func BuildSessionID(uid [4]byte, token [28]byte) [32]byte {
	var sid [32]byte
	copy(sid[0:4], uid[:])
	copy(sid[4:], token[:])
	return sid
}

func SplitSessionID(sessionID []byte) (uid [4]byte, token [28]byte, err error) {
	if len(sessionID) != 32 {
		return uid, token, fmt.Errorf("split session_id: expected 32 bytes, got %d", len(sessionID))
	}
	copy(uid[:], sessionID[0:4])
	copy(token[:], sessionID[4:])
	return uid, token, nil
}

func ReplayKey(uid [4]byte, matchedWindow int64, clientRandom [32]byte) [32]byte {
	var input [45]byte
	input[0] = ProtocolVersion
	copy(input[1:5], uid[:])
	binary.BigEndian.PutUint64(input[5:13], uint64(matchedWindow))
	copy(input[13:], clientRandom[:])
	return sha256.Sum256(input[:])
}

func SPKIPinFromDER(spkiDER []byte) [32]byte {
	return sha256.Sum256(spkiDER)
}

func Base64URLNoPad(data []byte) string {
	return strings.TrimRight(base64.URLEncoding.EncodeToString(data), "=")
}

func ParseBase64URLNoPad(s string) ([]byte, error) {
	if strings.TrimSpace(s) == "" {
		return nil, fmt.Errorf("base64url: empty input")
	}
	switch len(s) % 4 {
	case 2:
		s += "=="
	case 3:
		s += "="
	}
	return base64.URLEncoding.DecodeString(s)
}

func hmacSHA256(key, msg []byte) [32]byte {
	const blockSize = 64
	if len(key) > blockSize {
		sum := sha256.Sum256(key)
		key = sum[:]
	}
	k := make([]byte, blockSize)
	copy(k, key)

	ipad := make([]byte, blockSize)
	opad := make([]byte, blockSize)
	for i := 0; i < blockSize; i++ {
		ipad[i] = k[i] ^ 0x36
		opad[i] = k[i] ^ 0x5c
	}

	inner := sha256.New()
	_, _ = inner.Write(ipad)
	_, _ = inner.Write(msg)
	innerSum := inner.Sum(nil)

	outer := sha256.New()
	_, _ = outer.Write(opad)
	_, _ = outer.Write(innerSum)

	var out [32]byte
	copy(out[:], outer.Sum(nil))
	return out
}
