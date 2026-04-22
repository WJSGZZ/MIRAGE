// Package uri implements the mirage:// URI scheme.
package uri

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"

	"miraged/internal/protocol"
)

// Server holds the connection parameters encoded in a mirage:// URI.
//
// The current repo understands both:
// 1. Spec format: mirage://<credentials>@host:port?sni=...&cert_pin=...
// 2. Legacy prototype format: mirage://host:port?pubkey=...&shortId=...
type Server struct {
	Name     string
	UserName string
	Addr     string
	SNI      string

	// Spec fields.
	PSKBase64       string
	CertPinBase64   string // standard base64 with padding for config persistence
	PaddingSeedBase64 string // standard base64 with padding for config persistence

	// Legacy fields.
	PubKeyBase64       string
	ShortID            string
	InsecureSkipVerify bool
}

// Encode converts a Server to a mirage:// URI string.
func Encode(s Server) string {
	if strings.TrimSpace(s.PSKBase64) != "" || strings.TrimSpace(s.CertPinBase64) != "" {
		return encodeSpec(s)
	}
	return encodeLegacy(s)
}

// Decode parses a mirage:// URI string into a Server.
func Decode(raw string) (Server, error) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "mirage://") {
		return Server{}, fmt.Errorf("uri: must start with mirage://")
	}

	u, err := url.Parse(raw)
	if err != nil {
		return Server{}, fmt.Errorf("uri: parse: %w", err)
	}
	if strings.TrimSpace(u.Host) == "" {
		return Server{}, fmt.Errorf("uri: missing host:port")
	}

	// Spec links carry credentials in userinfo and cert_pin in query.
	if ui := strings.TrimSpace(u.User.String()); ui != "" || u.Query().Get("cert_pin") != "" {
		return decodeSpec(u)
	}
	return decodeLegacy(u)
}

func encodeSpec(s Server) string {
	name := strings.TrimSpace(s.UserName)
	if name == "" {
		name = strings.TrimSpace(s.Name)
	}
	cred := protocol.Base64URLNoPad([]byte(name + ":" + s.PSKBase64))

	q := url.Values{}
	q.Set("sni", s.SNI)
	q.Set("cert_pin", base64StdToURL(s.CertPinBase64))
	if strings.TrimSpace(s.PaddingSeedBase64) != "" {
		q.Set("seed", base64StdToURL(s.PaddingSeedBase64))
	}
	if strings.TrimSpace(s.Name) != "" {
		q.Set("name", s.Name)
	}
	return "mirage://" + cred + "@" + s.Addr + "?" + q.Encode()
}

func encodeLegacy(s Server) string {
	q := url.Values{}
	q.Set("pubkey", base64StdToURL(s.PubKeyBase64))
	q.Set("sni", s.SNI)
	q.Set("shortId", s.ShortID)
	if s.Name != "" {
		q.Set("name", s.Name)
	}
	if s.InsecureSkipVerify {
		q.Set("skip", "1")
	}
	return "mirage://" + s.Addr + "?" + q.Encode()
}

func decodeSpec(u *url.URL) (Server, error) {
	credRaw, err := protocol.ParseBase64URLNoPad(u.User.String())
	if err != nil {
		return Server{}, fmt.Errorf("uri: credentials: %w", err)
	}
	nameAndPSK := strings.SplitN(string(credRaw), ":", 2)
	if len(nameAndPSK) != 2 {
		return Server{}, fmt.Errorf("uri: credentials must be <name>:<base64(psk)>")
	}

	pskBytes, err := base64.StdEncoding.DecodeString(nameAndPSK[1])
	if err != nil {
		return Server{}, fmt.Errorf("uri: psk base64: %w", err)
	}
	if len(pskBytes) != 32 {
		return Server{}, fmt.Errorf("uri: psk must be 32 bytes")
	}

	q := u.Query()
	sni := q.Get("sni")
	if strings.TrimSpace(sni) == "" {
		return Server{}, fmt.Errorf("uri: missing sni")
	}
	certPinURL := q.Get("cert_pin")
	if strings.TrimSpace(certPinURL) == "" {
		return Server{}, fmt.Errorf("uri: missing cert_pin")
	}
	certPinBytes, err := protocol.ParseBase64URLNoPad(certPinURL)
	if err != nil {
		return Server{}, fmt.Errorf("uri: cert_pin: %w", err)
	}
	if len(certPinBytes) != 32 {
		return Server{}, fmt.Errorf("uri: cert_pin must be 32 bytes")
	}

	var seedStd string
	if seedURL := q.Get("seed"); strings.TrimSpace(seedURL) != "" {
		seedBytes, err := protocol.ParseBase64URLNoPad(seedURL)
		if err != nil {
			return Server{}, fmt.Errorf("uri: seed: %w", err)
		}
		if len(seedBytes) != 16 {
			return Server{}, fmt.Errorf("uri: seed must be 16 bytes")
		}
		seedStd = base64.StdEncoding.EncodeToString(seedBytes)
	}

	name := q.Get("name")
	if strings.TrimSpace(name) == "" {
		name = u.Host
	}

	return Server{
		Name:              name,
		UserName:          nameAndPSK[0],
		Addr:              u.Host,
		SNI:               sni,
		PSKBase64:         base64.StdEncoding.EncodeToString(pskBytes),
		CertPinBase64:     base64.StdEncoding.EncodeToString(certPinBytes),
		PaddingSeedBase64: seedStd,
	}, nil
}

func decodeLegacy(u *url.URL) (Server, error) {
	q := u.Query()
	pubKeyURL := q.Get("pubkey")
	if pubKeyURL == "" {
		return Server{}, fmt.Errorf("uri: missing pubkey")
	}
	shortID := q.Get("shortId")
	if shortID == "" {
		return Server{}, fmt.Errorf("uri: missing shortId")
	}

	pubKeyStd := base64URLToStd(pubKeyURL)
	if _, err := base64.StdEncoding.DecodeString(pubKeyStd); err != nil {
		return Server{}, fmt.Errorf("uri: invalid pubkey: %w", err)
	}

	return Server{
		Name:               q.Get("name"),
		Addr:               u.Host,
		PubKeyBase64:       pubKeyStd,
		SNI:                q.Get("sni"),
		ShortID:            shortID,
		InsecureSkipVerify: q.Get("skip") == "1",
	}, nil
}

func base64StdToURL(s string) string {
	s = strings.TrimRight(s, "=")
	s = strings.ReplaceAll(s, "+", "-")
	s = strings.ReplaceAll(s, "/", "_")
	return s
}

func base64URLToStd(s string) string {
	s = strings.ReplaceAll(s, "-", "+")
	s = strings.ReplaceAll(s, "_", "/")
	switch len(s) % 4 {
	case 2:
		s += "=="
	case 3:
		s += "="
	}
	return s
}
