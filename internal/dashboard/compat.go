package dashboard

import (
	"fmt"
	"net"
	"net/http"
	"strings"
)

func (dash *Dashboard) serveMihomoCompat(w http.ResponseWriter, r *http.Request) {
	state := dash.currentState()
	socksAddr := state.Socks5
	if strings.TrimSpace(socksAddr) == "" {
		socksAddr = "127.0.0.1:1080"
	}
	host, port := splitHostPortDefault(socksAddr, "127.0.0.1", "1080")
	serverBypassRule := dash.mihomoServerBypassRule()
	if serverBypassRule != "" {
		serverBypassRule = "  - " + serverBypassRule + "\n"
	}

	w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
	_, _ = fmt.Fprintf(w, `# MIRAGE compatibility profile for Clash Verge Rev / mihomo.
# Keep MirageClient connected, then import this URL in Clash Verge:
# http://127.0.0.1:9099/compat/mihomo.yaml

mixed-port: 7890
allow-lan: false
mode: rule
log-level: info

dns:
  enable: true
  listen: 127.0.0.1:1053
  enhanced-mode: fake-ip
  nameserver:
    - 1.1.1.1
    - 8.8.8.8

tun:
  enable: true
  stack: mixed
  auto-route: true
  auto-detect-interface: true
  dns-hijack:
    - any:53

proxies:
  - name: MIRAGE
    type: socks5
    server: %s
    port: %s
    udp: false

proxy-groups:
  - name: PROXY
    type: select
    proxies:
      - MIRAGE
      - DIRECT

rules:
%s  - DOMAIN-SUFFIX,local,DIRECT
  - IP-CIDR,127.0.0.0/8,DIRECT,no-resolve
  - IP-CIDR,10.0.0.0/8,DIRECT,no-resolve
  - IP-CIDR,172.16.0.0/12,DIRECT,no-resolve
  - IP-CIDR,192.168.0.0/16,DIRECT,no-resolve
  - IP-CIDR,224.0.0.0/4,DIRECT,no-resolve
  - MATCH,PROXY
`, host, port, serverBypassRule)
}

func (dash *Dashboard) serveV2RayNCompat(w http.ResponseWriter, r *http.Request) {
	state := dash.currentState()
	socksAddr := state.Socks5
	if strings.TrimSpace(socksAddr) == "" {
		socksAddr = "127.0.0.1:1080"
	}
	host, port := splitHostPortDefault(socksAddr, "127.0.0.1", "1080")

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = fmt.Fprintf(w, `{
  "log": {
    "loglevel": "warning"
  },
  "inbounds": [
    {
      "tag": "socks-in",
      "listen": "127.0.0.1",
      "port": 10808,
      "protocol": "socks",
      "settings": {
        "auth": "noauth",
        "udp": false
      }
    },
    {
      "tag": "http-in",
      "listen": "127.0.0.1",
      "port": 10809,
      "protocol": "http",
      "settings": {}
    }
  ],
  "outbounds": [
    {
      "tag": "mirage",
      "protocol": "socks",
      "settings": {
        "servers": [
          {
            "address": "%s",
            "port": %s
          }
        ]
      }
    },
    {
      "tag": "direct",
      "protocol": "freedom"
    }
  ],
  "routing": {
    "domainStrategy": "AsIs",
    "rules": [
      {
        "type": "field",
        "ip": ["geoip:private"],
        "outboundTag": "direct"
      }
    ]
  }
}
`, host, port)
}

func splitHostPortDefault(addr, defaultHost, defaultPort string) (string, string) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return defaultHost, defaultPort
	}
	idx := strings.LastIndex(addr, ":")
	if idx <= 0 || idx == len(addr)-1 {
		return defaultHost, defaultPort
	}
	return addr[:idx], addr[idx+1:]
}

func (dash *Dashboard) mihomoServerBypassRule() string {
	dash.mu.Lock()
	activeID := dash.activeID
	servers := make([]SavedServer, len(dash.servers))
	copy(servers, dash.servers)
	dash.mu.Unlock()

	if activeID == "" {
		return ""
	}
	for _, srv := range servers {
		if srv.ID == activeID {
			return mihomoBypassRuleForAddr(srv.Addr)
		}
	}
	return ""
}

func mihomoBypassRuleForAddr(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ""
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if host == "" {
		return ""
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.To4() != nil {
			return fmt.Sprintf("IP-CIDR,%s/32,DIRECT,no-resolve", ip.String())
		}
		return fmt.Sprintf("IP-CIDR6,%s/128,DIRECT,no-resolve", ip.String())
	}
	return fmt.Sprintf("DOMAIN,%s,DIRECT", host)
}
