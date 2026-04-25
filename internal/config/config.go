// Package config loads and validates MIRAGE server and client configuration.
package config

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"miraged/internal/protocol"
)

// ServerUser is one authorised client account on the server.
//
// v0 prototype uses ShortID. The spec-aligned model uses PSK -> rolling UID.
// Both are kept here temporarily so the repo can migrate incrementally.
type ServerUser struct {
	Name    string `json:"name"`
	PSK     string `json:"psk,omitempty"`
	ShortID string `json:"shortId,omitempty"`

	PSKBytes     []byte
	ShortIDBytes []byte
}

// ServerConfig is loaded from the server's config.json.
type ServerConfig struct {
	Listen string `json:"listen"`

	Users []ServerUser `json:"users"`

	// Spec-aligned fields.
	Fallback          string `json:"fallback,omitempty"`
	Cert              string `json:"cert,omitempty"`
	Key               string `json:"key,omitempty"`
	ServerPaddingSeed string `json:"server_padding_seed,omitempty"`
	ReplayCacheTTL    int    `json:"replay_cache_ttl,omitempty"`
	ReplayCacheCap    int    `json:"replay_cache_cap,omitempty"`
	StatsAPI          string `json:"stats_api,omitempty"`
	ControlToken      string `json:"control_token,omitempty"`

	// Legacy prototype fields.
	ServerKey   string `json:"serverKey,omitempty"`
	CertFile    string `json:"certFile,omitempty"`
	KeyFile     string `json:"keyFile,omitempty"`
	MaxTimeDiff int    `json:"maxTimeDiff,omitempty"`

	// Parsed legacy auth material.
	PrivKey *ecdh.PrivateKey
	PubKey  *ecdh.PublicKey

	// Parsed spec-aligned fields.
	ServerPaddingSeedBytes [16]byte
	hasPSKUsers            bool

	// Rolling UID maps (T-1 / T / T+1 hour windows), rebuilt hourly.
	// Protected by mapsMu; never access the maps directly outside this file.
	mapsMu  sync.RWMutex
	uidPrev map[[4]byte]*ServerUser
	uidCurr map[[4]byte]*ServerUser
	uidNext map[[4]byte]*ServerUser
}

// ClientConfig is loaded from the client's client.json.
type ClientConfig struct {
	// Spec-aligned fields.
	Name              string   `json:"name,omitempty"`
	Server            string   `json:"server"`
	PSK               string   `json:"psk,omitempty"`
	SNI               string   `json:"sni"`
	CertPin           string   `json:"cert_pin,omitempty"`
	CertPins          []string `json:"cert_pins,omitempty"`
	ClientPaddingSeed string   `json:"client_padding_seed,omitempty"`
	LocalSocks5       string   `json:"local_socks5,omitempty"`
	LocalHTTP         string   `json:"local_http,omitempty"`
	StatsAPI          string   `json:"stats_api,omitempty"`
	UTLSProfile       string   `json:"utls_profile,omitempty"`
	ProxyMode         string   `json:"proxy_mode,omitempty"`

	// Legacy prototype fields.
	Listen             string `json:"listen,omitempty"`
	ServerPubKey       string `json:"serverPubKey,omitempty"`
	ShortID            string `json:"shortId,omitempty"`
	InsecureSkipVerify bool   `json:"insecureSkipVerify,omitempty"`

	// Parsed legacy fields.
	ServerPub    *ecdh.PublicKey
	ShortIDBytes []byte

	// Parsed spec-aligned fields.
	PSKBytes               []byte
	CertPinBytes           [][]byte
	ClientPaddingSeedBytes [16]byte
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
	if err := cfg.normalizeServer(); err != nil {
		return nil, err
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
	if err := ParseClientFields(&cfg); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	return &cfg, nil
}

// ParseClientFields parses the base64/hex fields of a ClientConfig that was
// built programmatically (not via LoadClient). Call after setting the string
// fields directly.
func ParseClientFields(cfg *ClientConfig) error {
	if cfg == nil {
		return fmt.Errorf("nil client config")
	}
	cfg.applyClientDefaults()
	if strings.TrimSpace(cfg.Server) == "" {
		return fmt.Errorf("server required")
	}

	if err := parseLegacyClientFields(cfg); err != nil {
		return err
	}
	if err := parseSpecClientFields(cfg); err != nil {
		return err
	}
	return nil
}

// FindUser returns the legacy user whose ShortIDBytes exactly match, or nil.
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

// HasPSKUsers reports whether any user has a PSK configured.
func (c *ServerConfig) HasPSKUsers() bool {
	return c != nil && c.hasPSKUsers
}

// FindUserByUID looks up the user across T-1/T/T+1 hour windows (curr first).
func (c *ServerConfig) FindUserByUID(uid [4]byte) *ServerUser {
	if c == nil {
		return nil
	}
	c.mapsMu.RLock()
	defer c.mapsMu.RUnlock()
	if u := c.uidCurr[uid]; u != nil {
		return u
	}
	if u := c.uidPrev[uid]; u != nil {
		return u
	}
	return c.uidNext[uid]
}

// RebuildUserMaps rebuilds the T-1/T/T+1 UID lookup maps for the given time.
// Safe to call concurrently; uses a write-lock for the swap.
func (c *ServerConfig) RebuildUserMaps(now time.Time) error {
	tCurr := protocol.UIDHourWindow(now.Unix())
	prev, err := buildWindowMap(c.Users, tCurr-1)
	if err != nil {
		return fmt.Errorf("uid map T-1: %w", err)
	}
	curr, err := buildWindowMap(c.Users, tCurr)
	if err != nil {
		return fmt.Errorf("uid map T: %w", err)
	}
	next, err := buildWindowMap(c.Users, tCurr+1)
	if err != nil {
		return fmt.Errorf("uid map T+1: %w", err)
	}
	c.mapsMu.Lock()
	c.uidPrev = prev
	c.uidCurr = curr
	c.uidNext = next
	c.mapsMu.Unlock()
	return nil
}

// buildWindowMap derives UIDs for all PSK users at a specific hour window and
// returns the resulting map. Fails fast on any UID collision within the window.
func buildWindowMap(users []ServerUser, tUID int64) (map[[4]byte]*ServerUser, error) {
	m := make(map[[4]byte]*ServerUser, len(users))
	for i := range users {
		u := &users[i]
		if len(u.PSKBytes) == 0 {
			continue
		}
		uid, err := protocol.DeriveUID(u.PSKBytes, tUID)
		if err != nil {
			return nil, fmt.Errorf("user %q: %w", u.Name, err)
		}
		if prev, exists := m[uid]; exists {
			return nil, fmt.Errorf("uid collision at window %d between %q and %q — change a PSK and redeploy", tUID, prev.Name, u.Name)
		}
		m[uid] = u
	}
	return m, nil
}

func (cfg *ServerConfig) normalizeServer() error {
	if strings.TrimSpace(cfg.Listen) == "" {
		return fmt.Errorf("config: listen required")
	}

	// Mirror spec names into legacy runtime fields so existing code keeps working.
	if cfg.CertFile == "" {
		cfg.CertFile = cfg.Cert
	}
	if cfg.KeyFile == "" {
		cfg.KeyFile = cfg.Key
	}
	if cfg.Cert == "" {
		cfg.Cert = cfg.CertFile
	}
	if cfg.Key == "" {
		cfg.Key = cfg.KeyFile
	}
	if cfg.ReplayCacheTTL == 0 {
		cfg.ReplayCacheTTL = int(protocol.ReplayTTLSeconds)
	}
	if cfg.ReplayCacheCap == 0 {
		cfg.ReplayCacheCap = 10000
	}
	if cfg.StatsAPI == "" {
		cfg.StatsAPI = "127.0.0.1:9999"
	}
	if cfg.MaxTimeDiff == 0 {
		cfg.MaxTimeDiff = 60
	}
	if err := parseLegacyServerKey(cfg); err != nil {
		return err
	}
	if err := parseServerPaddingSeed(cfg); err != nil {
		return err
	}
	if err := parseServerUsers(cfg); err != nil {
		return err
	}
	return nil
}

func parseLegacyServerKey(cfg *ServerConfig) error {
	if strings.TrimSpace(cfg.ServerKey) == "" {
		return nil
	}
	kb, err := base64.StdEncoding.DecodeString(cfg.ServerKey)
	if err != nil {
		return fmt.Errorf("config: serverKey base64: %w", err)
	}
	cfg.PrivKey, err = ecdh.X25519().NewPrivateKey(kb)
	if err != nil {
		return fmt.Errorf("config: serverKey X25519: %w", err)
	}
	cfg.PubKey = cfg.PrivKey.PublicKey()
	return nil
}

func parseServerPaddingSeed(cfg *ServerConfig) error {
	if strings.TrimSpace(cfg.ServerPaddingSeed) == "" {
		var seed [16]byte
		if _, err := rand.Read(seed[:]); err != nil {
			return fmt.Errorf("config: generate server_padding_seed: %w", err)
		}
		cfg.ServerPaddingSeedBytes = seed
		cfg.ServerPaddingSeed = base64.StdEncoding.EncodeToString(seed[:])
		return nil
	}

	seed, err := base64.StdEncoding.DecodeString(cfg.ServerPaddingSeed)
	if err != nil {
		return fmt.Errorf("config: server_padding_seed base64: %w", err)
	}
	if len(seed) != 16 {
		return fmt.Errorf("config: server_padding_seed must be 16 bytes")
	}
	copy(cfg.ServerPaddingSeedBytes[:], seed)
	return nil
}

func parseServerUsers(cfg *ServerConfig) error {
	for i := range cfg.Users {
		u := &cfg.Users[i]
		if strings.TrimSpace(u.Name) == "" {
			u.Name = fmt.Sprintf("user-%d", i+1)
		}
		if strings.TrimSpace(u.PSK) != "" {
			psk, err := base64.StdEncoding.DecodeString(u.PSK)
			if err != nil {
				return fmt.Errorf("user %d psk base64: %w", i, err)
			}
			if len(psk) != 32 {
				return fmt.Errorf("user %d psk: must be 32 bytes", i)
			}
			u.PSKBytes = psk
			cfg.hasPSKUsers = true
		}

		if strings.TrimSpace(u.ShortID) == "" {
			continue
		}
		shortIDBytes, err := hex.DecodeString(u.ShortID)
		if err != nil {
			return fmt.Errorf("user %d shortId: %w", i, err)
		}
		if len(shortIDBytes) < 1 || len(shortIDBytes) > 8 {
			return fmt.Errorf("user %d shortId: must be 1-8 bytes", i)
		}
		u.ShortIDBytes = shortIDBytes
	}
	if cfg.hasPSKUsers {
		if err := cfg.RebuildUserMaps(time.Now()); err != nil {
			return fmt.Errorf("initial uid map build: %w", err)
		}
	}
	return nil
}

func (cfg *ClientConfig) applyClientDefaults() {
	if strings.TrimSpace(cfg.LocalSocks5) == "" {
		if strings.TrimSpace(cfg.Listen) != "" {
			cfg.LocalSocks5 = cfg.Listen
		} else {
			cfg.LocalSocks5 = "127.0.0.1:1080"
		}
	}
	if strings.TrimSpace(cfg.Listen) == "" {
		cfg.Listen = cfg.LocalSocks5
	}
	if strings.TrimSpace(cfg.LocalHTTP) == "" {
		cfg.LocalHTTP = nextPortAddrOrEmpty(cfg.LocalSocks5)
	}
	if strings.TrimSpace(cfg.StatsAPI) == "" {
		cfg.StatsAPI = "127.0.0.1:9999"
	}
	if strings.TrimSpace(cfg.UTLSProfile) == "" {
		cfg.UTLSProfile = "chrome_auto"
	}
	if strings.TrimSpace(cfg.ProxyMode) == "" {
		cfg.ProxyMode = "system"
	}
	if len(cfg.CertPins) == 0 && strings.TrimSpace(cfg.CertPin) != "" {
		cfg.CertPins = []string{cfg.CertPin}
	}
	if strings.TrimSpace(cfg.CertPin) == "" && len(cfg.CertPins) > 0 {
		cfg.CertPin = cfg.CertPins[0]
	}
}

func parseLegacyClientFields(cfg *ClientConfig) error {
	if strings.TrimSpace(cfg.ServerPubKey) == "" && strings.TrimSpace(cfg.ShortID) == "" {
		return nil
	}
	if strings.TrimSpace(cfg.ServerPubKey) == "" {
		return fmt.Errorf("serverPubKey required when using legacy shortId auth")
	}
	if strings.TrimSpace(cfg.ShortID) == "" {
		return fmt.Errorf("shortId required when using legacy shortId auth")
	}

	pb, err := base64.StdEncoding.DecodeString(cfg.ServerPubKey)
	if err != nil {
		return fmt.Errorf("serverPubKey base64: %w", err)
	}
	cfg.ServerPub, err = ecdh.X25519().NewPublicKey(pb)
	if err != nil {
		return fmt.Errorf("serverPubKey X25519: %w", err)
	}
	cfg.ShortIDBytes, err = hex.DecodeString(cfg.ShortID)
	if err != nil {
		return fmt.Errorf("shortId hex: %w", err)
	}
	if len(cfg.ShortIDBytes) < 1 || len(cfg.ShortIDBytes) > 8 {
		return fmt.Errorf("shortId must be 1-8 bytes")
	}
	return nil
}

func parseSpecClientFields(cfg *ClientConfig) error {
	if strings.TrimSpace(cfg.PSK) == "" && len(cfg.CertPins) == 0 && strings.TrimSpace(cfg.ClientPaddingSeed) == "" {
		return nil
	}

	if strings.TrimSpace(cfg.PSK) == "" {
		return fmt.Errorf("psk required for spec client config")
	}
	psk, err := base64.StdEncoding.DecodeString(cfg.PSK)
	if err != nil {
		return fmt.Errorf("psk base64: %w", err)
	}
	if len(psk) != 32 {
		return fmt.Errorf("psk must be 32 bytes")
	}
	cfg.PSKBytes = psk

	if len(cfg.CertPins) == 0 {
		return fmt.Errorf("cert_pin required for spec client config")
	}
	cfg.CertPinBytes = cfg.CertPinBytes[:0]
	for i, pin := range cfg.CertPins {
		raw, err := parseCertPin(pin)
		if err != nil {
			return fmt.Errorf("cert_pin[%d]: %w", i, err)
		}
		if len(raw) != 32 {
			return fmt.Errorf("cert_pin[%d]: must be 32 bytes", i)
		}
		cfg.CertPinBytes = append(cfg.CertPinBytes, raw)
	}

	if strings.TrimSpace(cfg.ClientPaddingSeed) == "" {
		if _, err := rand.Read(cfg.ClientPaddingSeedBytes[:]); err != nil {
			return fmt.Errorf("generate client_padding_seed: %w", err)
		}
		cfg.ClientPaddingSeed = base64.StdEncoding.EncodeToString(cfg.ClientPaddingSeedBytes[:])
		return nil
	}

	seed, err := base64.StdEncoding.DecodeString(cfg.ClientPaddingSeed)
	if err != nil {
		return fmt.Errorf("client_padding_seed base64: %w", err)
	}
	if len(seed) != 16 {
		return fmt.Errorf("client_padding_seed must be 16 bytes")
	}
	copy(cfg.ClientPaddingSeedBytes[:], seed)
	return nil
}

func parseCertPin(pin string) ([]byte, error) {
	pin = strings.TrimSpace(pin)
	raw, err := protocol.ParseBase64URLNoPad(pin)
	if err == nil {
		return raw, nil
	}
	std, stdErr := base64.StdEncoding.DecodeString(pin)
	if stdErr == nil {
		return std, nil
	}
	return nil, err
}

func nextPortAddrOrEmpty(addr string) string {
	host, port, ok := strings.Cut(addr, ":")
	if !ok || host == "" || port == "" {
		return ""
	}
	// Leave exact increment logic to the daemon; this is only a UI-facing default.
	switch port {
	case "1080":
		return host + ":1081"
	default:
		return ""
	}
}
