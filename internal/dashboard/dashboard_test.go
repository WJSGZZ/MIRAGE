package dashboard

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestSpecEndpoints(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dash := New(filepath.Join(dir, "servers.json"))
	srv := httptest.NewServer(dash.Handler())
	defer srv.Close()

	t.Run("health", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/health")
		if err != nil {
			t.Fatalf("GET /health: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /health: status=%d", resp.StatusCode)
		}
	})

	t.Run("version", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/version")
		if err != nil {
			t.Fatalf("GET /version: %v", err)
		}
		defer resp.Body.Close()
		var got versionResp
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			t.Fatalf("decode /version: %v", err)
		}
		if got.Protocol == "" || got.Core == "" {
			t.Fatalf("unexpected version payload: %+v", got)
		}
	})

	t.Run("profiles", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/profiles")
		if err != nil {
			t.Fatalf("GET /profiles: %v", err)
		}
		defer resp.Body.Close()
		var got []profileResp
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			t.Fatalf("decode /profiles: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("expected empty profiles, got %d", len(got))
		}
	})

	t.Run("proxy config defaults", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/proxy/config")
		if err != nil {
			t.Fatalf("GET /proxy/config: %v", err)
		}
		defer resp.Body.Close()
		var got proxyConfigResp
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			t.Fatalf("decode /proxy/config: %v", err)
		}
		if got.Mode != proxyModeManual || got.ApplyWinHTTP || got.ExportEnv {
			t.Fatalf("unexpected proxy config: %+v", got)
		}
	})

	t.Run("pac endpoint", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/pac.js")
		if err != nil {
			t.Fatalf("GET /pac.js: %v", err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read /pac.js: %v", err)
		}
		text := string(body)
		if !strings.Contains(text, "FindProxyForURL") || !strings.Contains(text, "DIRECT") || !strings.Contains(text, "PROXY 127.0.0.1:1081") {
			t.Fatalf("unexpected pac body: %s", text)
		}
	})

	t.Run("mihomo compatibility profile", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/compat/mihomo.yaml")
		if err != nil {
			t.Fatalf("GET /compat/mihomo.yaml: %v", err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read /compat/mihomo.yaml: %v", err)
		}
		text := string(body)
		for _, want := range []string{"type: socks5", "server: 127.0.0.1", "port: 1080", "MATCH,PROXY"} {
			if !strings.Contains(text, want) {
				t.Fatalf("mihomo profile missing %q: %s", want, text)
			}
		}
	})

	t.Run("v2rayn compatibility config", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/compat/v2rayn.json")
		if err != nil {
			t.Fatalf("GET /compat/v2rayn.json: %v", err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read /compat/v2rayn.json: %v", err)
		}
		text := string(body)
		for _, want := range []string{`"protocol": "socks"`, `"address": "127.0.0.1"`, `"port": 1080`, `"tag": "mirage"`} {
			if !strings.Contains(text, want) {
				t.Fatalf("v2rayn config missing %q: %s", want, text)
			}
		}
	})
}

func TestMihomoBypassRuleForAddr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		addr string
		want string
	}{
		{name: "ipv4", addr: "107.173.160.207:8443", want: "IP-CIDR,107.173.160.207/32,DIRECT,no-resolve"},
		{name: "domain", addr: "edge.example.com:443", want: "DOMAIN,edge.example.com,DIRECT"},
		{name: "ipv6", addr: "[2001:db8::1]:8443", want: "IP-CIDR6,2001:db8::1/128,DIRECT,no-resolve"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := mihomoBypassRuleForAddr(tt.addr); got != tt.want {
				t.Fatalf("mihomoBypassRuleForAddr(%q)=%q, want %q", tt.addr, got, tt.want)
			}
		})
	}
}

func TestMihomoProfileIncludesActiveServerBypass(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dash := New(filepath.Join(dir, "servers.json"))
	dash.mu.Lock()
	dash.servers = []SavedServer{{
		ID:   "active",
		Addr: "107.173.160.207:8443",
	}}
	dash.activeID = "active"
	dash.mu.Unlock()

	srv := httptest.NewServer(dash.Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/compat/mihomo.yaml")
	if err != nil {
		t.Fatalf("GET /compat/mihomo.yaml: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read /compat/mihomo.yaml: %v", err)
	}
	text := string(body)
	want := "IP-CIDR,107.173.160.207/32,DIRECT,no-resolve"
	if !strings.Contains(text, want) {
		t.Fatalf("mihomo profile missing active server bypass %q: %s", want, text)
	}
	if strings.Index(text, want) > strings.Index(text, "MATCH,PROXY") {
		t.Fatalf("server bypass rule must appear before MATCH: %s", text)
	}
}
