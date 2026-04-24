package config

import (
	"encoding/base64"
	"testing"
)

func TestParseClientFieldsAcceptsCertPinEncodings(t *testing.T) {
	pin := []byte{
		0xfb, 0x37, 0xd2, 0x98, 0x31, 0x6f, 0xce, 0xda,
		0x26, 0x75, 0x35, 0x20, 0xba, 0x54, 0x14, 0xb0,
		0xa9, 0xd7, 0xe4, 0x8f, 0x50, 0xc0, 0x19, 0x90,
		0xf2, 0x76, 0xf5, 0xce, 0x26, 0x7c, 0x64, 0xb9,
	}
	psk := make([]byte, 32)
	seed := make([]byte, 16)

	for _, certPin := range []string{
		base64.RawURLEncoding.EncodeToString(pin),
		base64.StdEncoding.EncodeToString(pin),
	} {
		cfg := &ClientConfig{
			Server:            "127.0.0.1:8443",
			PSK:               base64.StdEncoding.EncodeToString(psk),
			SNI:               "www.microsoft.com",
			CertPin:           certPin,
			ClientPaddingSeed: base64.StdEncoding.EncodeToString(seed),
		}
		if err := ParseClientFields(cfg); err != nil {
			t.Fatalf("ParseClientFields(%q): %v", certPin, err)
		}
		if got := len(cfg.CertPinBytes); got != 1 {
			t.Fatalf("CertPinBytes len = %d, want 1", got)
		}
	}
}
