package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"miraged/internal/sysproxy"
)

const (
	proxyModeOff    = "off"
	proxyModeSystem = "system"
	proxyModeManual = "manual"
	proxyModePAC    = "pac"
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
	System         sysproxy.Snapshot `json:"system"`
}

func defaultProxyConfig() proxyConfig {
	return proxyConfig{
		Mode:         proxyModeSystem,
		ApplyWinHTTP: true,
		ExportEnv:    true,
	}
}

func normalizeProxyConfig(cfg proxyConfig) proxyConfig {
	switch cfg.Mode {
	case proxyModeOff, proxyModeSystem, proxyModeManual, proxyModePAC:
	default:
		cfg.Mode = proxyModeSystem
	}
	return cfg
}

func (dash *Dashboard) loadProxyConfig() {
	dash.proxyCfg = defaultProxyConfig()
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
	default:
		return false
	}
}

func (dash *Dashboard) applyProxyPolicy() error {
	dash.mu.Lock()
	cfg := dash.proxyCfg
	dash.mu.Unlock()

	if !dash.d.Running() {
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
		ApplyWinHTTP: cfg.ApplyWinHTTP,
		ExportEnv:    cfg.ExportEnv,
		HTTPProxyAddr: httpAddr,
		SocksProxyAddr: socksAddr,
	}

	var err error
	switch cfg.Mode {
	case proxyModeOff, proxyModeManual:
		err = sysproxy.ClearAll(sysproxy.ApplyOptions{ApplyWinHTTP: true, ExportEnv: true})
	case proxyModeSystem:
		err = sysproxy.ApplySystem(httpAddr, socksAddr, opts)
	case proxyModePAC:
		err = sysproxy.ApplyPAC(dash.proxyPACURL(), opts)
	default:
		err = fmt.Errorf("unsupported proxy mode: %s", cfg.Mode)
	}
	dash.recordProxyApply(err)
	return err
}

func (dash *Dashboard) clearProxyAfterDisconnect() error {
	dash.mu.Lock()
	cfg := dash.proxyCfg
	dash.mu.Unlock()
	if cfg.Mode == proxyModeManual {
		dash.recordProxyApply(nil)
		return nil
	}
	err := sysproxy.ClearAll(sysproxy.ApplyOptions{ApplyWinHTTP: true, ExportEnv: true})
	dash.recordProxyApply(err)
	return err
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
