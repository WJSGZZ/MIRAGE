package dashboard

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"miraged/internal/sysproxy"
	tunmode "miraged/internal/tun"
)

const (
	proxyModeOff    = "off"
	proxyModeSystem = "system"
	proxyModeManual = "manual"
	proxyModePAC    = "pac"
	proxyModeTUN    = "tun"
)

type proxyConfig struct {
	Mode         string `json:"mode"`
	ApplyWinHTTP bool   `json:"applyWinHttp"`
	ExportEnv    bool   `json:"exportEnv"`
}

type proxyConfigResp struct {
	Mode           string            `json:"mode"`
	ApplyWinHTTP   bool              `json:"applyWinHttp"`
	ExportEnv      bool              `json:"exportEnv"`
	PacURL         string            `json:"pacUrl"`
	PacRunning     bool              `json:"pacRunning"`
	Applied        bool              `json:"applied"`
	LastApplyAt    string            `json:"lastApplyAt"`
	LastApplyError string            `json:"lastApplyError"`
	TunActive      bool              `json:"tunActive"`
	TunTarget      string            `json:"tunTarget"`
	System         sysproxy.Snapshot `json:"system"`
}

func defaultProxyConfig() proxyConfig {
	return proxyConfig{
		Mode:         proxyModeManual,
		ApplyWinHTTP: false,
		ExportEnv:    false,
	}
}

func normalizeProxyConfig(cfg proxyConfig) proxyConfig {
	switch cfg.Mode {
	case proxyModeOff, proxyModeSystem, proxyModeManual, proxyModePAC, proxyModeTUN:
	default:
		cfg.Mode = proxyModeManual
	}
	return cfg
}

func (dash *Dashboard) loadProxyConfig() {
	// Bridge mode is the only mode miragec supports; never load a non-manual
	// config from disk regardless of what proxy.json says.  This ensures that
	// even a hand-edited or leftover proxy.json with mode="system"/"tun"/etc.
	// cannot cause MIRAGE to touch Windows system proxy, WinHTTP, environment
	// variables, or TUN on startup.
	dash.proxyCfg = defaultProxyConfig() // always "manual"
	if dash.bridgeMode {
		return
	}
	data, err := os.ReadFile(dash.proxyFile)
	if err != nil {
		return
	}
	var cfg proxyConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return
	}
	dash.proxyCfg = normalizeProxyConfig(cfg)
}

func (dash *Dashboard) SetBridgeMode() {
	dash.mu.Lock()
	dash.bridgeMode = true
	dash.proxyCfg = proxyConfig{
		Mode:         proxyModeManual,
		ApplyWinHTTP: false,
		ExportEnv:    false,
	}
	dash.saveProxyConfig()
	dash.mu.Unlock()
	dash.recordProxyApply(nil)
}

func (dash *Dashboard) isBridgeMode() bool {
	dash.mu.Lock()
	defer dash.mu.Unlock()
	return dash.bridgeMode
}

func (dash *Dashboard) saveProxyConfig() {
	data, err := json.MarshalIndent(dash.proxyCfg, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(dash.proxyFile, data, 0600)
}

func (dash *Dashboard) proxyPACURL() string {
	dash.mu.Lock()
	addr := dash.probeAddr
	dash.mu.Unlock()
	if strings.TrimSpace(addr) == "" {
		addr = "127.0.0.1:9099"
	}
	return "http://" + addr + "/pac.js"
}

func (dash *Dashboard) currentProxyConfig() proxyConfigResp {
	dash.mu.Lock()
	cfg := dash.proxyCfg
	lastAt := dash.lastProxyApplyAt
	lastErr := dash.lastProxyApplyError
	tunActive := dash.tunActive
	tunTarget := dash.tunTarget
	dash.mu.Unlock()
	system := sysproxy.SnapshotState()
	return proxyConfigResp{
		Mode:           cfg.Mode,
		ApplyWinHTTP:   cfg.ApplyWinHTTP,
		ExportEnv:      cfg.ExportEnv,
		PacURL:         dash.proxyPACURL(),
		PacRunning:     cfg.Mode == proxyModePAC,
		Applied:        proxyModeApplied(cfg.Mode, system),
		LastApplyAt:    lastAt,
		LastApplyError: lastErr,
		TunActive:      tunActive,
		TunTarget:      tunTarget,
		System:         system,
	}
}

func proxyModeApplied(mode string, snapshot sysproxy.Snapshot) bool {
	switch mode {
	case proxyModeOff:
		return snapshot.ProxyEnable == "0x0" && snapshot.ProxyServer == "" && snapshot.AutoConfigURL == ""
	case proxyModeSystem:
		return snapshot.ProxyEnable == "0x1" && snapshot.ProxyServer != "" && snapshot.AutoConfigURL == ""
	case proxyModeManual:
		return true
	case proxyModePAC:
		return snapshot.AutoConfigURL != ""
	case proxyModeTUN:
		return true
	default:
		return false
	}
}

func (dash *Dashboard) applyProxyPolicy() error {
	if dash.isBridgeMode() {
		dash.recordProxyApply(nil)
		return nil
	}

	dash.mu.Lock()
	cfg := dash.proxyCfg
	dash.mu.Unlock()

	if !dash.d.Running() {
		dash.stopTUNIfActive()
		if cfg.Mode == proxyModeOff {
			err := sysproxy.ClearAll(sysproxy.ApplyOptions{ApplyWinHTTP: true, ExportEnv: true})
			dash.recordProxyApply(err)
			return err
		}
		dash.recordProxyApply(nil)
		return nil
	}

	httpAddr := dash.d.HTTPListen()
	socksAddr := dash.d.SocksListen()
	opts := sysproxy.ApplyOptions{
		ApplyWinHTTP:   cfg.ApplyWinHTTP,
		ExportEnv:      cfg.ExportEnv,
		HTTPProxyAddr:  httpAddr,
		SocksProxyAddr: socksAddr,
	}

	var err error
	switch cfg.Mode {
	case proxyModeManual:
		dash.stopTUNIfActive()
	case proxyModeOff:
		dash.stopTUNIfActive()
		err = sysproxy.ClearAll(sysproxy.ApplyOptions{ApplyWinHTTP: true, ExportEnv: true})
	case proxyModeSystem:
		dash.stopTUNIfActive()
		err = sysproxy.ApplySystem(httpAddr, socksAddr, opts)
	case proxyModePAC:
		dash.stopTUNIfActive()
		err = sysproxy.ApplyPAC(dash.proxyPACURL(), opts)
	case proxyModeTUN:
		err = dash.startTUNForActiveProfile(socksAddr)
		if err == nil {
			err = sysproxy.ClearAll(sysproxy.ApplyOptions{ApplyWinHTTP: true, ExportEnv: true})
		}
	default:
		err = fmt.Errorf("unsupported proxy mode: %s", cfg.Mode)
	}
	dash.recordProxyApply(err)
	return err
}

func (dash *Dashboard) clearProxyAfterDisconnect() error {
	if dash.isBridgeMode() {
		dash.recordProxyApply(nil)
		return nil
	}

	dash.mu.Lock()
	cfg := dash.proxyCfg
	dash.mu.Unlock()
	if cfg.Mode == proxyModeManual {
		dash.recordProxyApply(nil)
		return nil
	}
	dash.stopTUNIfActive()
	err := sysproxy.ClearAll(sysproxy.ApplyOptions{ApplyWinHTTP: true, ExportEnv: true})
	dash.recordProxyApply(err)
	return err
}

func (dash *Dashboard) startTUNForActiveProfile(socksAddr string) error {
	if strings.TrimSpace(socksAddr) == "" {
		return fmt.Errorf("tun: socks proxy is not running")
	}

	dash.mu.Lock()
	activeID := dash.activeID
	var serverAddr string
	for _, srv := range dash.servers {
		if srv.ID == activeID {
			serverAddr = srv.Addr
			break
		}
	}
	if dash.tunActive && dash.tunTarget == serverAddr {
		dash.mu.Unlock()
		return nil
	}
	oldTarget := dash.tunTarget
	wasActive := dash.tunActive
	dash.tunActive = false
	dash.tunTarget = ""
	dash.mu.Unlock()

	if wasActive && strings.TrimSpace(oldTarget) != "" {
		if host, _, err := net.SplitHostPort(oldTarget); err == nil {
			if vpsIP, err := resolveIPv4(host); err == nil {
				tunmode.Stop(vpsIP)
			}
		}
	}
	if strings.TrimSpace(serverAddr) == "" {
		return fmt.Errorf("tun: active profile not found")
	}
	host, _, err := net.SplitHostPort(serverAddr)
	if err != nil {
		return fmt.Errorf("tun: server address %q: %w", serverAddr, err)
	}
	vpsIP, err := resolveIPv4(host)
	if err != nil {
		return err
	}
	if err := tunmode.Start(socksAddr, vpsIP); err != nil {
		return err
	}

	dash.mu.Lock()
	dash.tunActive = true
	dash.tunTarget = serverAddr
	dash.mu.Unlock()
	return nil
}

func (dash *Dashboard) stopTUNIfActive() {
	dash.mu.Lock()
	active := dash.tunActive
	target := dash.tunTarget
	dash.tunActive = false
	dash.tunTarget = ""
	dash.mu.Unlock()

	if !active || strings.TrimSpace(target) == "" {
		return
	}
	if host, _, err := net.SplitHostPort(target); err == nil {
		if vpsIP, err := resolveIPv4(host); err == nil {
			tunmode.Stop(vpsIP)
		}
	}
}

func resolveIPv4(host string) (string, error) {
	if ip := net.ParseIP(host); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			return v4.String(), nil
		}
		return "", fmt.Errorf("tun: IPv6 server addresses are not supported yet: %s", host)
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return "", fmt.Errorf("tun: resolve server %s: %w", host, err)
	}
	for _, ip := range ips {
		if v4 := ip.To4(); v4 != nil {
			return v4.String(), nil
		}
	}
	return "", fmt.Errorf("tun: no IPv4 address found for %s", host)
}

func (dash *Dashboard) recordProxyApply(err error) {
	dash.mu.Lock()
	defer dash.mu.Unlock()
	dash.lastProxyApplyAt = time.Now().Format(time.RFC3339)
	if err != nil {
		dash.lastProxyApplyError = err.Error()
		return
	}
	dash.lastProxyApplyError = ""
}

func (dash *Dashboard) apiProxyConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		jsonOK(w, dash.currentProxyConfig())
	case http.MethodPost:
		if dash.isBridgeMode() {
			jsonErr(w, "bridge mode is read-only; MIRAGE will not change system proxy settings", http.StatusForbidden)
			return
		}
		var req proxyConfig
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonErr(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		req = normalizeProxyConfig(req)
		dash.mu.Lock()
		dash.proxyCfg = req
		dash.saveProxyConfig()
		dash.mu.Unlock()
		if err := dash.applyProxyPolicy(); err != nil {
			jsonErr(w, err.Error(), http.StatusInternalServerError)
			return
		}
		jsonOK(w, dash.currentProxyConfig())
	default:
		http.Error(w, "GET or POST required", http.StatusMethodNotAllowed)
	}
}

func (dash *Dashboard) apiProxyReapply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	if dash.isBridgeMode() {
		dash.recordProxyApply(nil)
		jsonOK(w, dash.currentProxyConfig())
		return
	}
	if err := dash.applyProxyPolicy(); err != nil {
		jsonErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, dash.currentProxyConfig())
}

func (dash *Dashboard) apiProxyPAC(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, map[string]interface{}{
		"url":     dash.proxyPACURL(),
		"running": dash.d.Running(),
	})
}

func (dash *Dashboard) servePAC(w http.ResponseWriter, r *http.Request) {
	state := dash.currentState()
	httpAddr := state.HTTP
	socksAddr := state.Socks5
	if httpAddr == "" {
		httpAddr = "127.0.0.1:1081"
	}
	if socksAddr == "" {
		socksAddr = "127.0.0.1:1080"
	}
	w.Header().Set("Content-Type", "application/x-ns-proxy-autoconfig")
	_, _ = w.Write([]byte(renderPAC(httpAddr, socksAddr)))
}

func renderPAC(httpAddr, socksAddr string) string {
	return fmt.Sprintf(`function FindProxyForURL(url, host) {
  if (isPlainHostName(host) ||
      host === "localhost" ||
      host === "127.0.0.1" ||
      host === "::1" ||
      dnsDomainIs(host, ".local") ||
      isInNet(host, "10.0.0.0", "255.0.0.0") ||
      isInNet(host, "172.16.0.0", "255.240.0.0") ||
      isInNet(host, "192.168.0.0", "255.255.0.0") ||
      isInNet(host, "127.0.0.0", "255.0.0.0")) {
    return "DIRECT";
  }
  return "PROXY %s; SOCKS5 %s; DIRECT";
}
`, httpAddr, socksAddr)
}

func (dash *Dashboard) LaunchPACURL() string {
	return dash.proxyPACURL()
}
