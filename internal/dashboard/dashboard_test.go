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
		if got.Mode != proxyModeSystem || !got.ApplyWinHTTP || !got.ExportEnv {
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
}
