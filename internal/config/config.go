// Package config loads and validates MIRAGE server and client configuration.
package config

import (
	"crypto/ecdh"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
)

// ServerUser is one authorised client account on the server.
type ServerUser struct {
	Name    string `json:"name"`    // human label, not used on the wire
	ShortID string `json:"shortId"` // hex, 1–16 chars (1–8 bytes); unique per user

	ShortIDBytes []byte // parsed
}

// ServerConfig is loaded from the server's config.json.
type ServerConfig struct {
	Listen string `json:"listen"` // e.g. "0.0.0.0:443"

	// ServerKey: base64-encoded X25519 private key (32 bytes).
	// Used by clients to authenticate to this server.
	// Generate with: miraged -genkey
	ServerKey string `json:"serverKey"`

	// TLS certificate files. If both are empty a self-signed cert is
	// auto-generated and saved next to the config file.
	CertFile string `json:"certFile"`
	KeyFile  string `json:"keyFile"`

	// MaxTimeDiff: clock-skew tolerance in seconds (default 60).
	MaxTimeDiff int `json:"maxTimeDiff"`

	Users []ServerUser `json:"users"`

	// Parsed
	PrivKey *ecdh.PrivateKey
	PubKey  *ecdh.PublicKey
}

// ClientConfig is loaded from the client's client.json.
type ClientConfig struct {
	Listen string `json:"listen"` // local SOCKS5 address, e.g. "127.0.0.1:1080"

	// Server is the MIRAGE server address, e.g. "1.2.3.4:443".
	Server string `json:"server"`

	// ServerPubKey: base64-encoded X25519 public key of the server.
	// Printed by: miraged -genkey
	ServerPubKey string `json:"serverPubKey"`

	// SNI sent in the TLS ClientHello.
	SNI string `json:"sni"`

	// ShortID: hex string matching one entry in the server's user list.
	ShortID string `json:"shortId"`

	// InsecureSkipVerify skips TLS certificate verification.
	// Set true when the server uses a self-signed certificate.
	InsecureSkipVerify bool `json:"insecureSkipVerify"`

	// Parsed
	ServerPub    *ecdh.PublicKey
	ShortIDBytes []byte
}

func LoadServer(path string) (*ServerConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var cfg ServerConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if cfg.Listen == "" {
		return nil, fmt.Errorf("config: listen required")
	}
	if cfg.MaxTimeDiff == 0 {
		cfg.MaxTimeDiff = 60
	}

	kb, err := base64.StdEncoding.DecodeString(cfg.ServerKey)
	if err != nil {
		return nil, fmt.Errorf("config: serverKey base64: %w", err)
	}
	cfg.PrivKey, err = ecdh.X25519().NewPrivateKey(kb)
	if err != nil {
		return nil, fmt.Errorf("config: serverKey X25519: %w", err)
	}
	cfg.PubKey = cfg.PrivKey.PublicKey()

	for i := range cfg.Users {
		u := &cfg.Users[i]
		if u.ShortID == "" {
			return nil, fmt.Errorf("user %d: shortId required", i)
		}
		u.ShortIDBytes, err = hex.DecodeString(u.ShortID)
		if err != nil {
			return nil, fmt.Errorf("user %d shortId: %w", i, err)
		}
		if len(u.ShortIDBytes) < 1 || len(u.ShortIDBytes) > 8 {
			return nil, fmt.Errorf("user %d shortId: must be 1–8 bytes (2–16 hex chars)", i)
		}
	}
	return &cfg, nil
}

func LoadClient(path string) (*ClientConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var cfg ClientConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if cfg.Listen == "" {
		cfg.Listen = "127.0.0.1:1080"
	}
	if cfg.Server == "" {
		return nil, fmt.Errorf("config: server required")
	}
	if cfg.ServerPubKey == "" {
		return nil, fmt.Errorf("config: serverPubKey required")
	}
	if cfg.ShortID == "" {
		return nil, fmt.Errorf("config: shortId required")
	}

	pb, err := base64.StdEncoding.DecodeString(cfg.ServerPubKey)
	if err != nil {
		return nil, fmt.Errorf("config: serverPubKey base64: %w", err)
	}
	cfg.ServerPub, err = ecdh.X25519().NewPublicKey(pb)
	if err != nil {
		return nil, fmt.Errorf("config: serverPubKey X25519: %w", err)
	}

	cfg.ShortIDBytes, err = hex.DecodeString(cfg.ShortID)
	if err != nil {
		return nil, fmt.Errorf("config: shortId hex: %w", err)
	}
	if len(cfg.ShortIDBytes) < 1 || len(cfg.ShortIDBytes) > 8 {
		return nil, fmt.Errorf("config: shortId must be 1–8 bytes")
	}
	return &cfg, nil
}

// FindUser returns the ServerUser whose ShortIDBytes exactly match, or nil.
func (c *ServerConfig) FindUser(shortID []byte) *ServerUser {
	for i := range c.Users {
		u := &c.Users[i]
		if len(u.ShortIDBytes) != len(shortID) {
			continue
		}
		ok := true
		for j := range u.ShortIDBytes {
			if u.ShortIDBytes[j] != shortID[j] {
				ok = false
				break
			}
		}
		if ok {
			return u
		}
	}
	return nil
}
