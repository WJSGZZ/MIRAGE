// Package dashboard serves the local control API for miragec.
package dashboard

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"miraged/internal/config"
	"miraged/internal/daemon"
	"miraged/internal/sysproxy"
	"miraged/internal/uri"
)

// SavedServer is one entry in servers.json.
type SavedServer struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	UserName           string `json:"userName,omitempty"`
	Addr               string `json:"addr"`
	PSK                string `json:"psk,omitempty"`
	CertPin            string `json:"certPin,omitempty"`
	ClientPaddingSeed  string `json:"clientPaddingSeed,omitempty"`
	PubKeyBase64       string `json:"pubKeyBase64"`
	SNI                string `json:"sni"`
	ShortID            string `json:"shortId"`
	InsecureSkipVerify bool   `json:"insecureSkipVerify"`
	Listen             string `json:"listen"` // local SOCKS5 addr, default 127.0.0.1:1080
}

type State = stateResp
type Diagnostics = diagnosticsResp
type Conclusion = diagConclusion
type ListenerStatus = listenerStatus
type TestResult = testResult

const (
	coreVersion     = "1.2.1"
	protocolVersion = "MIRAGE-SPEC-001 1.0.6-draft"
)

// Dashboard owns the HTTP mux, daemon, and persisted server list.
type Dashboard struct {
	mu                  sync.Mutex
	d                   *daemon.Daemon
	servers             []SavedServer
	activeID            string
	file                string // path to servers.json, or a profiles directory
	proxyFile           string
	proxyCfg            proxyConfig
	bridgeMode          bool
	probeAddr           string
	startedAt           time.Time
	logs                *memoryLog
	lastProxyApplyAt    string
	lastProxyApplyError string
	tunActive           bool
	tunTarget           string
}

// New loads servers.json from file (creating it if absent) and returns a Dashboard.
func New(file string) *Dashboard {
	dash := &Dashboard{
		d:          &daemon.Daemon{},
		file:       file,
		bridgeMode: true,
		probeAddr:  "127.0.0.1:9099",
		startedAt:  time.Now(),
		logs:       installMemoryLog(),
	}
	if dash.usesProfileDir() {
		dash.proxyFile = filepath.Join(file, "proxy.json")
	} else {
		dash.proxyFile = file + ".proxy.json"
	}
	dash.load()
	dash.loadProxyConfig()
	return dash
}

func (dash *Dashboard) SetProbeAddr(addr string) {
	dash.mu.Lock()
	dash.probeAddr = addr
	dash.mu.Unlock()
}

// Handler returns the HTTP handler for the control API.
func (dash *Dashboard) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", dash.serveRoot)
	mux.HandleFunc("/health", dash.apiHealth)
	mux.HandleFunc("/version", dash.apiVersion)
	mux.HandleFunc("/state", dash.apiState)
	mux.HandleFunc("/stats", dash.apiStats)
	mux.HandleFunc("/profiles", dash.apiProfiles)
	mux.HandleFunc("/connect", dash.apiConnectCompat)
	mux.HandleFunc("/disconnect", dash.apiDisconnect)
	mux.HandleFunc("/reload-config", dash.apiReloadConfig)
	mux.HandleFunc("/logs", dash.apiLogs)
	mux.HandleFunc("/proxy/config", dash.apiProxyConfig)
	mux.HandleFunc("/proxy/reapply", dash.apiProxyReapply)
	mux.HandleFunc("/proxy/pac", dash.apiProxyPAC)
	mux.HandleFunc("/pac.js", dash.servePAC)
	mux.HandleFunc("/compat/mihomo.yaml", dash.serveMihomoCompat)
	mux.HandleFunc("/compat/v2rayn.json", dash.serveV2RayNCompat)
	mux.HandleFunc("/api/state", dash.apiState)
	mux.HandleFunc("/api/servers", dash.apiServers)
	mux.HandleFunc("/api/import", dash.apiImport)
	mux.HandleFunc("/api/connect", dash.apiConnect)
	mux.HandleFunc("/api/disconnect", dash.apiDisconnect)
	mux.HandleFunc("/api/diagnostics", dash.apiDiagnostics)
	mux.HandleFunc("/api/diagnostics/text", dash.apiDiagnosticsText)
	mux.HandleFunc("/api/launch", dash.apiLaunch)
	mux.HandleFunc("/api/winhttp/apply", dash.apiApplyWinHTTP)
	mux.HandleFunc("/api/servers/", dash.apiDeleteServer)
	return withCORS(mux)
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (dash *Dashboard) serveRoot(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, map[string]interface{}{
		"ok":       true,
		"service":  "miragec-core",
		"version":  coreVersion,
		"protocol": protocolVersion,
		"endpoints": []string{
			"/health",
			"/state",
			"/profiles",
			"/api/import",
			"/api/connect",
			"/api/disconnect",
			"/compat/mihomo.yaml",
			"/compat/v2rayn.json",
		},
	})
}

type stateResp struct {
	Running             bool   `json:"running"`
	Status              string `json:"status"`
	Socks5              string `json:"socks5"`     // empty when not running
	HTTP                string `json:"http"`       // empty when not running
	EnvHTTP             string `json:"envHttp"`    // recommended HTTP(S)_PROXY value
	EnvALL              string `json:"envAll"`     // recommended ALL_PROXY value
	EnvNote             string `json:"envNote"`    // note for apps that read env at startup
	ProxyScope          string `json:"proxyScope"` // summary of what was applied on connect
	CaptureGap          string `json:"captureGap"` // why some apps can still bypass the proxy
	ActiveID            string `json:"activeId"`   // empty when not running
	ProxyMode           string `json:"proxyMode"`
	ProxyApplied        bool   `json:"proxyApplied"`
	ApplyWinHTTP        bool   `json:"applyWinHttp"`
	ExportEnv           bool   `json:"exportEnv"`
	PACURL              string `json:"pacUrl"`
	TUNActive           bool   `json:"tunActive"`
	TUNTarget           string `json:"tunTarget"`
	LastProxyApplyAt    string `json:"lastProxyApplyAt"`
	LastProxyApplyError string `json:"lastProxyApplyError"`
}

type versionResp struct {
	Core     string `json:"core"`
	Protocol string `json:"protocol"`
}

type statsResp struct {
	Running         bool   `json:"running"`
	ActiveProfile   string `json:"activeProfile"`
	UptimeSeconds   int64  `json:"uptimeSeconds"`
	Socks5Listen    string `json:"socks5Listen"`
	HTTPListen      string `json:"httpListen"`
	UploadBytes     int64  `json:"uploadBytes"`
	DownloadBytes   int64  `json:"downloadBytes"`
	UploadRateBps   int64  `json:"uploadRateBps"`
	DownloadRateBps int64  `json:"downloadRateBps"`
}

type profileResp struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Server    string `json:"server"`
	SNI       string `json:"sni,omitempty"`
	ProxyMode string `json:"proxyMode"`
	Active    bool   `json:"active"`
}

func (dash *Dashboard) apiState(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, dash.currentState())
}

func (dash *Dashboard) apiHealth(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, map[string]interface{}{
		"ok":      true,
		"ready":   true,
		"running": dash.d.Running(),
	})
}

func (dash *Dashboard) apiVersion(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, versionResp{
		Core:     coreVersion,
		Protocol: protocolVersion,
	})
}

func (dash *Dashboard) State() State {
	return dash.currentState()
}

func (dash *Dashboard) apiProfiles(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, dash.Profiles())
}

func (dash *Dashboard) apiServers(w http.ResponseWriter, r *http.Request) {
	dash.mu.Lock()
	list := make([]SavedServer, len(dash.servers))
	copy(list, dash.servers)
	dash.mu.Unlock()
	jsonOK(w, list)
}

func (dash *Dashboard) Servers() []SavedServer {
	dash.mu.Lock()
	defer dash.mu.Unlock()
	list := make([]SavedServer, len(dash.servers))
	copy(list, dash.servers)
	return list
}

func (dash *Dashboard) Profiles() []profileResp {
	dash.mu.Lock()
	defer dash.mu.Unlock()
	out := make([]profileResp, 0, len(dash.servers))
	mode := dash.proxyCfg.Mode
	for _, s := range dash.servers {
		out = append(out, profileResp{
			ID:        s.ID,
			Name:      s.Name,
			Server:    s.Addr,
			SNI:       s.SNI,
			ProxyMode: mode,
			Active:    dash.activeID == s.ID && dash.d.Running(),
		})
	}
	return out
}

type importReq struct {
	URI    string `json:"uri"`
	Listen string `json:"listen"` // optional override
}

func (dash *Dashboard) apiImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	var req importReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	saved, err := dash.ImportURI(req.URI, req.Listen)
	if err != nil {
		jsonErr(w, err.Error(), http.StatusBadRequest)
		return
	}
	jsonOK(w, saved)
}

func (dash *Dashboard) ImportURI(rawURI, listen string) (SavedServer, error) {
	srv, err := uri.Decode(rawURI)
	if err != nil {
		return SavedServer{}, err
	}
	if listen == "" {
		listen = "127.0.0.1:1080"
	}
	id, _ := randomHex(4)
	saved := SavedServer{
		ID:                 id,
		Name:               srv.Name,
		UserName:           srv.UserName,
		Addr:               srv.Addr,
		PSK:                srv.PSKBase64,
		CertPin:            srv.CertPinBase64,
		ClientPaddingSeed:  srv.PaddingSeedBase64,
		PubKeyBase64:       srv.PubKeyBase64,
		SNI:                srv.SNI,
		ShortID:            srv.ShortID,
		InsecureSkipVerify: srv.InsecureSkipVerify,
		Listen:             listen,
	}
	if saved.Name == "" {
		saved.Name = srv.Addr
	}

	dash.mu.Lock()
	for i := range dash.servers {
		if dash.servers[i].Addr == saved.Addr && dash.servers[i].UserName == saved.UserName {
			saved.ID = dash.servers[i].ID
			dash.servers[i] = saved
			dash.save()
			dash.mu.Unlock()
			return saved, nil
		}
	}
	dash.servers = append(dash.servers, saved)
	dash.save()
	dash.mu.Unlock()
	return saved, nil
}

type connectReq struct {
	ID string `json:"id"`
}

func (dash *Dashboard) apiConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	var req connectReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	dash.mu.Lock()
	var found *SavedServer
	for i := range dash.servers {
		if dash.servers[i].ID == req.ID {
			found = &dash.servers[i]
			break
		}
	}
	dash.mu.Unlock()

	if found == nil {
		jsonErr(w, "server not found", http.StatusNotFound)
		return
	}

	cfg := &config.ClientConfig{
		Name:               found.Name,
		Listen:             found.Listen,
		LocalSocks5:        found.Listen,
		Server:             found.Addr,
		PSK:                found.PSK,
		ServerPubKey:       found.PubKeyBase64,
		SNI:                found.SNI,
		CertPin:            found.CertPin,
		ClientPaddingSeed:  found.ClientPaddingSeed,
		ShortID:            found.ShortID,
		InsecureSkipVerify: found.InsecureSkipVerify,
	}
	if err := config.ParseClientFields(cfg); err != nil {
		jsonErr(w, "config error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := dash.d.Start(cfg); err != nil {
		jsonErr(w, "start failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	dash.mu.Lock()
	dash.activeID = found.ID
	dash.mu.Unlock()

	if err := dash.applyProxyPolicy(); err != nil {
		jsonErr(w, "proxy apply failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("dashboard: connected to %s via HTTP %s and SOCKS5 %s", found.Addr, dash.d.HTTPListen(), dash.d.SocksListen())
	jsonOK(w, dash.currentState())
}

func (dash *Dashboard) apiConnectCompat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Profile string `json:"profile"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Profile == "" {
		jsonErr(w, "profile is required", http.StatusBadRequest)
		return
	}
	state, err := dash.Connect(req.Profile)
	if err != nil {
		jsonErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, state)
}

func (dash *Dashboard) Connect(id string) (State, error) {
	dash.mu.Lock()
	var found *SavedServer
	for i := range dash.servers {
		if dash.servers[i].ID == id {
			found = &dash.servers[i]
			break
		}
	}
	dash.mu.Unlock()
	if found == nil {
		return State{}, fmt.Errorf("server not found")
	}

	cfg := &config.ClientConfig{
		Name:               found.Name,
		Listen:             found.Listen,
		LocalSocks5:        found.Listen,
		Server:             found.Addr,
		PSK:                found.PSK,
		ServerPubKey:       found.PubKeyBase64,
		SNI:                found.SNI,
		CertPin:            found.CertPin,
		ClientPaddingSeed:  found.ClientPaddingSeed,
		ShortID:            found.ShortID,
		InsecureSkipVerify: found.InsecureSkipVerify,
	}
	if err := config.ParseClientFields(cfg); err != nil {
		return State{}, fmt.Errorf("config error: %w", err)
	}
	if err := dash.d.Start(cfg); err != nil {
		return State{}, fmt.Errorf("start failed: %w", err)
	}

	dash.mu.Lock()
	dash.activeID = found.ID
	dash.mu.Unlock()
	if err := dash.applyProxyPolicy(); err != nil {
		return State{}, fmt.Errorf("proxy apply failed: %w", err)
	}
	log.Printf("dashboard: connected to %s via HTTP %s and SOCKS5 %s", found.Addr, dash.d.HTTPListen(), dash.d.SocksListen())
	return dash.currentState(), nil
}

func (dash *Dashboard) apiDisconnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	dash.d.Stop()
	dash.mu.Lock()
	dash.activeID = ""
	dash.mu.Unlock()
	_ = dash.clearProxyAfterDisconnect()
	log.Printf("dashboard: disconnected")
	jsonOK(w, stateResp{})
}

func (dash *Dashboard) Disconnect() {
	dash.d.Stop()
	dash.mu.Lock()
	dash.activeID = ""
	dash.mu.Unlock()
	_ = dash.clearProxyAfterDisconnect()
	log.Printf("dashboard: disconnected")
}

func (dash *Dashboard) apiApplyWinHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	if dash.isBridgeMode() {
		jsonErr(w, "bridge mode is read-only; MIRAGE will not change WinHTTP settings", http.StatusForbidden)
		return
	}
	httpAddr := dash.d.HTTPListen()
	if httpAddr == "" {
		jsonErr(w, "proxy not running", http.StatusBadRequest)
		return
	}
	if err := sysproxy.ApplyWinHTTPElevated(httpAddr); err != nil {
		jsonErr(w, "elevated WinHTTP apply failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	time.Sleep(250 * time.Millisecond)
	jsonOK(w, map[string]interface{}{
		"ok":     true,
		"http":   httpAddr,
		"system": sysproxy.SnapshotState(),
	})
}

func (dash *Dashboard) ApplyWinHTTP() (Diagnostics, error) {
	if dash.isBridgeMode() {
		return Diagnostics{}, fmt.Errorf("bridge mode is read-only; MIRAGE will not change WinHTTP settings")
	}
	httpAddr := dash.d.HTTPListen()
	if httpAddr == "" {
		return Diagnostics{}, fmt.Errorf("proxy not running")
	}
	if err := sysproxy.ApplyWinHTTPElevated(httpAddr); err != nil {
		return Diagnostics{}, fmt.Errorf("elevated WinHTTP apply failed: %w", err)
	}
	time.Sleep(250 * time.Millisecond)
	return dash.Diagnostics("https://api.openai.com"), nil
}

func (dash *Dashboard) apiStats(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, dash.Stats())
}

func (dash *Dashboard) Stats() statsResp {
	state := dash.currentState()
	dash.mu.Lock()
	activeID := dash.activeID
	dash.mu.Unlock()
	snap := dash.d.StatsSnapshot()
	return statsResp{
		Running:         state.Running,
		ActiveProfile:   activeID,
		UptimeSeconds:   snap.UptimeSeconds,
		Socks5Listen:    state.Socks5,
		HTTPListen:      state.HTTP,
		UploadBytes:     snap.UploadBytes,
		DownloadBytes:   snap.DownloadBytes,
		UploadRateBps:   snap.UploadRateBps,
		DownloadRateBps: snap.DownloadRateBps,
	}
}

type diagnosticsResp struct {
	Timestamp   string            `json:"timestamp"`
	State       stateResp         `json:"state"`
	System      sysproxy.Snapshot `json:"system"`
	Listeners   []listenerStatus  `json:"listeners"`
	Tests       []testResult      `json:"tests"`
	Conclusions []diagConclusion  `json:"conclusions"`
}

type launchReq struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

type diagConclusion struct {
	Level   string `json:"level"`
	Title   string `json:"title"`
	Detail  string `json:"detail"`
	Channel string `json:"channel,omitempty"`
}

type listenerStatus struct {
	Name      string `json:"name"`
	Address   string `json:"address"`
	Reachable bool   `json:"reachable"`
	Error     string `json:"error,omitempty"`
}

type testResult struct {
	Name     string `json:"name"`
	Target   string `json:"target"`
	Via      string `json:"via"`
	Success  bool   `json:"success"`
	Detail   string `json:"detail"`
	Duration string `json:"duration"`
}

func (dash *Dashboard) apiDiagnostics(w http.ResponseWriter, r *http.Request) {
	target := strings.TrimSpace(r.URL.Query().Get("target"))
	jsonOK(w, dash.Diagnostics(target))
}

func (dash *Dashboard) apiDiagnosticsText(w http.ResponseWriter, r *http.Request) {
	target := strings.TrimSpace(r.URL.Query().Get("target"))
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, FormatDiagnosticsText(dash.Diagnostics(target)))
}

func (dash *Dashboard) Diagnostics(target string) Diagnostics {
	state := dash.currentState()
	dash.mu.Lock()
	probeAddr := dash.probeAddr
	dash.mu.Unlock()
	resp := diagnosticsResp{
		Timestamp: time.Now().Format(time.RFC3339),
		State:     state,
		System:    sysproxy.SnapshotState(),
	}
	if probeAddr != "" {
		resp.Listeners = append(resp.Listeners, probeListener("dashboard", probeAddr))
	}
	if state.Socks5 != "" {
		resp.Listeners = append(resp.Listeners, probeListener("socks5", state.Socks5))
	}
	if state.HTTP != "" {
		resp.Listeners = append(resp.Listeners, probeListener("http", state.HTTP))
	}
	if strings.TrimSpace(target) == "" {
		target = "https://example.com"
	}
	resp.Tests = []testResult{
		httpProxyTest("http-proxy", state.HTTP, target),
		socksProxyTest("socks5-proxy", state.Socks5, target),
	}
	resp.Conclusions = diagnose(resp)
	return resp
}

func (dash *Dashboard) apiDeleteServer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "DELETE required", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/servers/")
	if id == "" {
		jsonErr(w, "missing id", http.StatusBadRequest)
		return
	}

	dash.mu.Lock()
	idx := -1
	for i, s := range dash.servers {
		if s.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		jsonErr(w, "not found", http.StatusNotFound)
		return
	}
	removedID := dash.servers[idx].ID
	dash.servers = append(dash.servers[:idx], dash.servers[idx+1:]...)
	dash.save()
	dash.deleteProfileFile(removedID)
	needReapply := false
	if dash.activeID == id {
		dash.d.Stop()
		dash.activeID = ""
		needReapply = true
	}
	dash.mu.Unlock()
	if needReapply {
		_ = dash.clearProxyAfterDisconnect()
	}
	jsonOK(w, map[string]bool{"ok": true})
}

func (dash *Dashboard) apiReloadConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	reloaded, err := dash.ReloadConfig()
	if err != nil {
		jsonErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]interface{}{
		"ok":       true,
		"profiles": reloaded,
	})
}

func (dash *Dashboard) apiLaunch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	var req launchReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Command) == "" {
		jsonErr(w, "command is required", http.StatusBadRequest)
		return
	}
	info, err := dash.LaunchWithProxy(req.Command, req.Args)
	if err != nil {
		jsonErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, info)
}

func (dash *Dashboard) DeleteServer(id string) error {
	if id == "" {
		return fmt.Errorf("missing id")
	}
	dash.mu.Lock()
	idx := -1
	for i, s := range dash.servers {
		if s.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("not found")
	}
	removedID := dash.servers[idx].ID
	dash.servers = append(dash.servers[:idx], dash.servers[idx+1:]...)
	dash.save()
	dash.deleteProfileFile(removedID)
	needReapply := false
	if dash.activeID == id {
		dash.d.Stop()
		dash.activeID = ""
		needReapply = true
	}
	dash.mu.Unlock()
	if needReapply {
		_ = dash.clearProxyAfterDisconnect()
	}
	return nil
}

func (dash *Dashboard) ReloadConfig() (int, error) {
	if dash.usesProfileDir() {
		list, err := dash.readProfileDir()
		if err != nil {
			return 0, fmt.Errorf("reload config: %w", err)
		}
		dash.mu.Lock()
		dash.servers = list
		dash.mu.Unlock()
		log.Printf("dashboard: reloaded %d profiles from %s", len(list), dash.file)
		return len(list), nil
	}

	data, err := os.ReadFile(dash.file)
	if err != nil {
		if errorsIs(err, fs.ErrNotExist) {
			dash.mu.Lock()
			dash.servers = nil
			dash.mu.Unlock()
			return 0, nil
		}
		return 0, fmt.Errorf("reload config: %w", err)
	}
	var list []SavedServer
	if len(strings.TrimSpace(string(data))) != 0 {
		if err := json.Unmarshal(data, &list); err != nil {
			return 0, fmt.Errorf("reload config: %w", err)
		}
	}
	dash.mu.Lock()
	dash.servers = list
	dash.mu.Unlock()
	log.Printf("dashboard: reloaded %d profiles from %s", len(list), dash.file)
	return len(list), nil
}

func (dash *Dashboard) LaunchWithProxy(command string, args []string) (map[string]interface{}, error) {
	state := dash.currentState()
	if !state.Running || state.HTTP == "" || state.Socks5 == "" {
		return nil, fmt.Errorf("mirage is not connected")
	}

	cmd := exec.Command(command, args...)
	cmd.Env = append(os.Environ(),
		"HTTP_PROXY="+state.EnvHTTP,
		"HTTPS_PROXY="+state.EnvHTTP,
		"ALL_PROXY="+state.EnvALL,
		"NO_PROXY=127.0.0.1,localhost,::1",
	)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("launch failed: %w", err)
	}

	log.Printf("dashboard: launched %s with MIRAGE proxy env", command)
	return map[string]interface{}{
		"ok":        true,
		"command":   command,
		"args":      args,
		"pid":       cmd.Process.Pid,
		"httpProxy": state.EnvHTTP,
		"allProxy":  state.EnvALL,
	}, nil
}

func (dash *Dashboard) load() {
	if dash.usesProfileDir() {
		list, err := dash.readProfileDir()
		if err == nil && len(list) > 0 {
			dash.servers = list
			return
		}
		legacy := filepath.Join(filepath.Dir(dash.file), "servers.json")
		if data, err := os.ReadFile(legacy); err == nil {
			var legacyList []SavedServer
			if json.Unmarshal(data, &legacyList) == nil && len(legacyList) > 0 {
				dash.servers = legacyList
				dash.save()
			}
		}
		return
	}

	data, err := os.ReadFile(dash.file)
	if err != nil {
		return
	}
	_ = json.Unmarshal(data, &dash.servers)
}

func (dash *Dashboard) save() {
	if dash.usesProfileDir() {
		_ = dash.saveProfileDir()
		return
	}
	data, err := json.MarshalIndent(dash.servers, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(dash.file, data, 0600)
}

func (dash *Dashboard) usesProfileDir() bool {
	if strings.TrimSpace(dash.file) == "" {
		return false
	}
	if st, err := os.Stat(dash.file); err == nil {
		return st.IsDir()
	}
	return filepath.Ext(dash.file) == ""
}

func (dash *Dashboard) readProfileDir() ([]SavedServer, error) {
	dir := dash.file
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var list []SavedServer
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if filepath.Ext(name) != ".json" || name == "proxy.json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		var saved SavedServer
		if err := json.Unmarshal(data, &saved); err != nil {
			continue
		}
		if strings.TrimSpace(saved.ID) == "" || strings.TrimSpace(saved.Addr) == "" {
			continue
		}
		list = append(list, saved)
	}
	return list, nil
}

func (dash *Dashboard) saveProfileDir() error {
	if err := os.MkdirAll(dash.file, 0700); err != nil {
		return err
	}
	for _, srv := range dash.servers {
		if strings.TrimSpace(srv.ID) == "" {
			continue
		}
		dash.deleteProfileFile(srv.ID)
		name := srv.ID + "-" + safeProfileFilePart(srv.Name)
		if name == srv.ID+"-" {
			name = srv.ID + "-" + safeProfileFilePart(srv.Addr)
		}
		if name == srv.ID+"-" {
			name = srv.ID
		}
		path := filepath.Join(dash.file, name+".json")
		data, err := json.MarshalIndent(srv, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			return err
		}
	}
	return nil
}

func (dash *Dashboard) deleteProfileFile(id string) {
	if !dash.usesProfileDir() || strings.TrimSpace(id) == "" {
		return
	}
	matches, _ := filepath.Glob(filepath.Join(dash.file, id+"-*.json"))
	if exact := filepath.Join(dash.file, id+".json"); exact != "" {
		matches = append(matches, exact)
	}
	for _, path := range matches {
		_ = os.Remove(path)
	}
}

func safeProfileFilePart(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune('-')
		}
		if b.Len() >= 48 {
			break
		}
	}
	return strings.Trim(b.String(), "-_.")
}

func (dash *Dashboard) apiLogs(w http.ResponseWriter, r *http.Request) {
	since := strings.TrimSpace(r.URL.Query().Get("since"))
	var sinceTime time.Time
	if since != "" {
		t, err := time.Parse(time.RFC3339, since)
		if err != nil {
			jsonErr(w, "invalid since timestamp", http.StatusBadRequest)
			return
		}
		sinceTime = t
	}
	jsonOK(w, map[string]interface{}{
		"items": dash.logs.Since(sinceTime),
	})
}

func jsonOK(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func jsonErr(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	_, err := rand.Read(b)
	return hex.EncodeToString(b), err
}

func (dash *Dashboard) currentState() stateResp {
	dash.mu.Lock()
	activeID := dash.activeID
	proxyCfg := dash.proxyCfg
	bridgeMode := dash.bridgeMode
	lastAt := dash.lastProxyApplyAt
	lastErr := dash.lastProxyApplyError
	tunActive := dash.tunActive
	tunTarget := dash.tunTarget
	dash.mu.Unlock()
	system := sysproxy.SnapshotState()

	resp := stateResp{
		Running:             dash.d.Running(),
		Status:              "idle",
		Socks5:              dash.d.SocksListen(),
		HTTP:                dash.d.HTTPListen(),
		EnvHTTP:             "http://" + dash.d.HTTPListen(),
		EnvALL:              "socks5://" + dash.d.SocksListen(),
		EnvNote:             "Apps already open may need a restart to pick up proxy environment variables.",
		ProxyScope:          "MIRAGE applies Windows system proxy settings and user proxy environment variables, then checks whether WinHTTP also picked them up.",
		CaptureGap:          "Apps that ignore WinINet, WinHTTP, and proxy environment variables can still bypass MIRAGE until TUN or service mode is added.",
		ActiveID:            activeID,
		ProxyMode:           proxyCfg.Mode,
		ProxyApplied:        proxyModeApplied(proxyCfg.Mode, system),
		ApplyWinHTTP:        proxyCfg.ApplyWinHTTP,
		ExportEnv:           proxyCfg.ExportEnv,
		PACURL:              dash.proxyPACURL(),
		TUNActive:           tunActive,
		TUNTarget:           tunTarget,
		LastProxyApplyAt:    lastAt,
		LastProxyApplyError: lastErr,
	}
	if resp.Running {
		resp.Status = "connected"
	}
	if bridgeMode {
		resp.ProxyScope = "Bridge mode only. MIRAGE does not change Windows system proxy, WinHTTP, user environment variables, or TUN routes."
		resp.CaptureGap = "Use Clash Verge Rev to enable System Proxy or TUN. MIRAGE only provides local HTTP/SOCKS and the compatibility subscription."
	}
	if !resp.Running {
		resp.EnvHTTP = ""
		resp.EnvALL = ""
		resp.EnvNote = ""
		resp.ProxyScope = ""
		resp.CaptureGap = ""
	}
	return resp
}

type logEntry struct {
	Timestamp string `json:"timestamp"`
	Message   string `json:"message"`
}

type memoryLog struct {
	mu     sync.Mutex
	lines  []logEntry
	max    int
	target io.Writer
}

var installLogOnce sync.Once
var sharedMemoryLog *memoryLog

func installMemoryLog() *memoryLog {
	installLogOnce.Do(func() {
		sharedMemoryLog = &memoryLog{
			max:    500,
			target: log.Writer(),
		}
		log.SetOutput(io.MultiWriter(sharedMemoryLog.target, sharedMemoryLog))
	})
	return sharedMemoryLog
}

func (m *memoryLog) Write(p []byte) (int, error) {
	text := strings.TrimSpace(string(p))
	if text == "" {
		return len(p), nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		m.lines = append(m.lines, logEntry{
			Timestamp: time.Now().Format(time.RFC3339),
			Message:   line,
		})
	}
	if len(m.lines) > m.max {
		m.lines = append([]logEntry(nil), m.lines[len(m.lines)-m.max:]...)
	}
	return len(p), nil
}

func (m *memoryLog) Since(since time.Time) []logEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	if since.IsZero() {
		out := make([]logEntry, len(m.lines))
		copy(out, m.lines)
		return out
	}
	var out []logEntry
	for _, item := range m.lines {
		t, err := time.Parse(time.RFC3339, item.Timestamp)
		if err != nil {
			continue
		}
		if !t.Before(since) {
			out = append(out, item)
		}
	}
	return out
}

func errorsIs(err error, target error) bool {
	return errors.Is(err, target)
}

func probeListener(name, addr string) listenerStatus {
	st := listenerStatus{Name: name, Address: addr}
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		st.Error = err.Error()
		return st
	}
	st.Reachable = true
	_ = conn.Close()
	return st
}

func httpProxyTest(name, proxyAddr, target string) testResult {
	res := testResult{Name: name, Target: target, Via: proxyAddr}
	if proxyAddr == "" {
		res.Detail = "proxy not running"
		return res
	}

	u, err := url.Parse(target)
	if err != nil {
		res.Detail = err.Error()
		return res
	}
	host := u.Host
	if host == "" {
		res.Detail = "target missing host"
		return res
	}
	if !strings.Contains(host, ":") {
		port := "80"
		if u.Scheme == "https" {
			port = "443"
		}
		host = net.JoinHostPort(host, port)
	}

	start := time.Now()
	conn, err := net.DialTimeout("tcp", proxyAddr, 4*time.Second)
	if err != nil {
		res.Detail = err.Error()
		return res
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	req := &http.Request{
		Method: http.MethodConnect,
		URL:    &url.URL{Opaque: host},
		Host:   host,
		Header: make(http.Header),
	}
	if err := req.Write(conn); err != nil {
		res.Detail = err.Error()
		return res
	}

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, req)
	res.Duration = time.Since(start).String()
	if err != nil {
		res.Detail = err.Error()
		return res
	}
	defer resp.Body.Close()
	res.Success = resp.StatusCode >= 200 && resp.StatusCode < 300
	res.Detail = fmt.Sprintf("CONNECT %s -> HTTP %d", host, resp.StatusCode)
	return res
}

func socksProxyTest(name, proxyAddr, target string) testResult {
	res := testResult{Name: name, Target: target, Via: proxyAddr}
	if proxyAddr == "" {
		res.Detail = "proxy not running"
		return res
	}

	u, err := url.Parse(target)
	if err != nil {
		res.Detail = err.Error()
		return res
	}
	host := u.Host
	if host == "" {
		res.Detail = "target missing host"
		return res
	}
	if !strings.Contains(host, ":") {
		port := "80"
		if u.Scheme == "https" {
			port = "443"
		}
		host = net.JoinHostPort(host, port)
	}

	start := time.Now()
	conn, err := net.DialTimeout("tcp", proxyAddr, 4*time.Second)
	if err != nil {
		res.Detail = err.Error()
		return res
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(8 * time.Second))

	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		res.Detail = err.Error()
		return res
	}
	reply := make([]byte, 2)
	if _, err := io.ReadFull(conn, reply); err != nil {
		res.Detail = err.Error()
		return res
	}
	if reply[0] != 0x05 || reply[1] != 0x00 {
		res.Detail = fmt.Sprintf("handshake reply %v", reply)
		return res
	}

	hostName, portStr, err := net.SplitHostPort(host)
	if err != nil {
		res.Detail = err.Error()
		return res
	}
	portNum, err := strconv.Atoi(portStr)
	if err != nil {
		res.Detail = err.Error()
		return res
	}
	req := []byte{0x05, 0x01, 0x00, 0x03, byte(len(hostName))}
	req = append(req, []byte(hostName)...)
	req = append(req, byte(portNum>>8), byte(portNum))
	if _, err := conn.Write(req); err != nil {
		res.Detail = err.Error()
		return res
	}

	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil {
		res.Detail = err.Error()
		return res
	}
	if head[1] != 0x00 {
		res.Detail = fmt.Sprintf("connect reply 0x%02x", head[1])
		return res
	}
	switch head[3] {
	case 0x01:
		_, err = io.ReadFull(conn, make([]byte, 6))
	case 0x03:
		var l [1]byte
		if _, err = io.ReadFull(conn, l[:]); err == nil {
			_, err = io.ReadFull(conn, make([]byte, int(l[0])+2))
		}
	case 0x04:
		_, err = io.ReadFull(conn, make([]byte, 18))
	}
	if err != nil {
		res.Detail = err.Error()
		return res
	}
	res.Success = true
	res.Duration = time.Since(start).String()
	res.Detail = "SOCKS connect ok"
	return res
}

func diagnose(resp diagnosticsResp) []diagConclusion {
	var out []diagConclusion

	listenerOK := map[string]bool{}
	for _, item := range resp.Listeners {
		listenerOK[item.Name] = item.Reachable
	}
	testOK := map[string]bool{}
	for _, item := range resp.Tests {
		testOK[item.Name] = item.Success
	}

	if !resp.State.Running {
		out = append(out, diagConclusion{
			Level:  "warn",
			Title:  "MIRAGE is not connected",
			Detail: "The local proxy listeners are idle until a server is connected.",
		})
		return out
	}

	if !listenerOK["socks5"] || !listenerOK["http"] {
		out = append(out, diagConclusion{
			Level:  "error",
			Title:  "Local listeners are incomplete",
			Detail: "If either the HTTP or SOCKS listener is unreachable, some applications will bypass MIRAGE completely.",
		})
	}

	if !testOK["http-proxy"] || !testOK["socks5-proxy"] {
		out = append(out, diagConclusion{
			Level:  "error",
			Title:  "Proxy transport is not fully healthy",
			Detail: "At least one local proxy path cannot establish outbound connectivity. Apps using that channel will fail even if the UI shows connected.",
		})
	} else {
		out = append(out, diagConclusion{
			Level:  "ok",
			Title:  "Core proxy path is working",
			Detail: "Both local HTTP CONNECT and SOCKS5 tunnel tests succeeded, so MIRAGE can carry generic outbound traffic.",
		})
	}

	sysEnabled := resp.System.ProxyEnable == "0x1" && resp.System.ProxyServer != ""
	pacEnabled := strings.TrimSpace(resp.System.AutoConfigURL) != ""
	if sysEnabled {
		out = append(out, diagConclusion{
			Level:   "ok",
			Title:   "Browser-style system proxy is active",
			Detail:  "Apps that honor the Windows Internet Settings proxy should already be routed through MIRAGE.",
			Channel: "WinINet",
		})
	} else {
		out = append(out, diagConclusion{
			Level:   "warn",
			Title:   "Windows system proxy is not fully applied",
			Detail:  "Browser-style applications may still go direct unless both ProxyEnable and ProxyServer are present.",
			Channel: "WinINet",
		})
	}

	if pacEnabled {
		out = append(out, diagConclusion{
			Level:   "ok",
			Title:   "PAC proxy is active",
			Detail:  "Windows has an AutoConfigURL configured, so PAC-aware applications should resolve through MIRAGE.",
			Channel: "PAC",
		})
	}

	winHTTPText := strings.ToLower(resp.System.WinHTTP)
	winHTTPActive := winHTTPText != "" &&
		!strings.Contains(winHTTPText, "direct access") &&
		!strings.Contains(winHTTPText, "直接访问")
	if winHTTPActive {
		out = append(out, diagConclusion{
			Level:   "ok",
			Title:   "WinHTTP proxy is active",
			Detail:  "Services and applications that rely on the WinHTTP stack should be able to pick up the MIRAGE HTTP proxy.",
			Channel: "WinHTTP",
		})
	} else {
		out = append(out, diagConclusion{
			Level:   "warn",
			Title:   "WinHTTP is still direct",
			Detail:  "Programs that use the WinHTTP stack may bypass MIRAGE. On Windows this often means the update needs elevation or a helper service.",
			Channel: "WinHTTP",
		})
	}

	env := resp.System.Env
	envOK := env["HTTP_PROXY"] != "" || env["HTTPS_PROXY"] != "" || env["ALL_PROXY"] != ""
	if envOK {
		out = append(out, diagConclusion{
			Level:   "ok",
			Title:   "Proxy environment variables are exported",
			Detail:  "CLI tools and app backends that read proxy variables at process startup can use MIRAGE after restart.",
			Channel: "Environment",
		})
	} else {
		out = append(out, diagConclusion{
			Level:   "warn",
			Title:   "Process-level proxy variables are missing",
			Detail:  "Apps that only honor HTTP_PROXY, HTTPS_PROXY, or ALL_PROXY at startup will not be captured until these variables exist.",
			Channel: "Environment",
		})
	}

	if testOK["http-proxy"] && testOK["socks5-proxy"] && sysEnabled && envOK && !winHTTPActive {
		out = append(out, diagConclusion{
			Level:  "warn",
			Title:  "Remaining gap is above the transport layer",
			Detail: "If a specific app still fails now, the likely issue is which proxy channel it honors at startup rather than whether MIRAGE can reach the target.",
		})
	}

	if testOK["http-proxy"] && testOK["socks5-proxy"] {
		out = append(out, diagConclusion{
			Level:  "warn",
			Title:  "Full software capture is not implemented yet",
			Detail: "Some applications ignore WinINet, WinHTTP, and proxy environment variables entirely. MIRAGE still needs TUN or a service-level capture path to match the coverage of mature desktop proxy clients.",
		})
	}

	return out
}

func FormatDiagnosticsText(data Diagnostics) string {
	var b strings.Builder
	b.WriteString("Snapshot\n")
	b.WriteString(data.Timestamp)
	b.WriteString("\n\n")

	for _, item := range data.Conclusions {
		b.WriteString(item.Title)
		b.WriteString("\n")
		b.WriteString(item.Level)
		b.WriteString("\n")
		b.WriteString(item.Detail)
		b.WriteString("\n")
		if item.Channel != "" {
			b.WriteString(item.Channel)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	for _, item := range data.Listeners {
		b.WriteString(item.Name)
		b.WriteString(" listener\n")
		if item.Reachable {
			b.WriteString("OK\n")
		} else {
			b.WriteString("Check\n")
		}
		b.WriteString("Address\n")
		b.WriteString(item.Address)
		b.WriteString("\nStatus\n")
		if item.Reachable {
			b.WriteString("reachable\n\n")
		} else {
			b.WriteString(item.Error)
			b.WriteString("\n\n")
		}
	}

	b.WriteString("System proxy\n")
	if data.System.ProxyServer != "" {
		b.WriteString("OK\n")
	} else {
		b.WriteString("Check\n")
	}
	b.WriteString("ProxyEnable\n")
	b.WriteString(blankIfEmpty(data.System.ProxyEnable))
	b.WriteString("\nProxyServer\n")
	b.WriteString(blankIfEmpty(data.System.ProxyServer))
	b.WriteString("\nProxyOverride\n")
	b.WriteString(blankIfEmpty(data.System.ProxyOverride))
	b.WriteString("\nAutoConfigURL\n")
	b.WriteString(blankIfEmpty(data.System.AutoConfigURL))
	b.WriteString("\nAutoDetect\n")
	b.WriteString(blankIfEmpty(data.System.AutoDetect))
	b.WriteString("\n\nWinHTTP\nValue\n")
	b.WriteString(blankIfEmpty(data.System.WinHTTP))
	b.WriteString("\n\nEnvironment\nHTTP_PROXY\n")
	b.WriteString(blankIfEmpty(data.System.Env["HTTP_PROXY"]))
	b.WriteString("\nHTTPS_PROXY\n")
	b.WriteString(blankIfEmpty(data.System.Env["HTTPS_PROXY"]))
	b.WriteString("\nALL_PROXY\n")
	b.WriteString(blankIfEmpty(data.System.Env["ALL_PROXY"]))
	b.WriteString("\nNO_PROXY\n")
	b.WriteString(blankIfEmpty(data.System.Env["NO_PROXY"]))
	b.WriteString("\n\n")

	for _, test := range data.Tests {
		b.WriteString(test.Name)
		b.WriteString("\n")
		if test.Success {
			b.WriteString("OK\n")
		} else {
			b.WriteString("Check\n")
		}
		b.WriteString("Target\n")
		b.WriteString(blankIfEmpty(test.Target))
		b.WriteString("\nVia\n")
		b.WriteString(blankIfEmpty(test.Via))
		b.WriteString("\nDetail\n")
		b.WriteString(blankIfEmpty(test.Detail))
		b.WriteString("\nDuration\n")
		b.WriteString(blankIfEmpty(test.Duration))
		b.WriteString("\n\n")
	}

	return strings.TrimSpace(b.String())
}

func blankIfEmpty(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(empty)"
	}
	return s
}
