package main

import (
	"encoding/base64"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"miraged/internal/client"
	"miraged/internal/config"
	"miraged/internal/dashboard"
	"miraged/internal/sysproxy"
	tunmode "miraged/internal/tun"
	"miraged/internal/uri"
)

const dashAddr = "127.0.0.1:9099"

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	cfgPath     := flag.String("c", "", "client config file — starts headless proxy (no UI)")
	uriStr      := flag.String("uri", "", "mirage:// URI — starts headless proxy directly")
	socks5Addr  := flag.String("socks5", "", "override SOCKS5 listen addr (headless mode)")
	httpAddr    := flag.String("http", "", "override HTTP proxy listen addr (headless mode)")
	tunMode     := flag.Bool("tun", false, "TUN mode: capture all traffic (requires admin + wintun.dll)")
	serversFile := flag.String("servers", defaultServersFile(), "servers.json path (dashboard mode)")
	noBrowser   := flag.Bool("no-browser", false, "do not open browser in dashboard mode")
	flag.Parse()

	switch {
	case strings.TrimSpace(*cfgPath) != "":
		cfg, err := config.LoadClient(*cfgPath)
		if err != nil {
			log.Fatalf("miragec: config: %v", err)
		}
		runHeadless(cfg, *socks5Addr, *httpAddr, *tunMode)

	case strings.TrimSpace(*uriStr) != "":
		cfg, err := configFromURI(*uriStr)
		if err != nil {
			log.Fatalf("miragec: uri: %v", err)
		}
		runHeadless(cfg, *socks5Addr, *httpAddr, *tunMode)

	default:
		if err := runDashboard(*serversFile, *noBrowser); err != nil {
			log.Fatalf("miragec: %v", err)
		}
	}
}

// runHeadless starts the SOCKS5 and HTTP proxy listeners directly, no UI.
// Blocks until SIGINT/SIGTERM.
func runHeadless(cfg *config.ClientConfig, socks5Override, httpOverride string, tun bool) {
	socks5 := cfg.LocalSocks5
	if strings.TrimSpace(socks5Override) != "" {
		socks5 = socks5Override
	}
	if strings.TrimSpace(socks5) == "" {
		socks5 = "127.0.0.1:1080"
	}

	httpListen := cfg.LocalHTTP
	if strings.TrimSpace(httpOverride) != "" {
		httpListen = httpOverride
	}

	c := client.New(cfg)

	socks5Ln, err := net.Listen("tcp", socks5)
	if err != nil {
		log.Fatalf("miragec: socks5 listen %s: %v", socks5, err)
	}
	log.Printf("miragec: SOCKS5 proxy on %s", socks5)
	go client.Serve(socks5Ln, c, nil)

	if strings.TrimSpace(httpListen) != "" {
		httpLn, err := net.Listen("tcp", httpListen)
		if err != nil {
			log.Printf("miragec: http proxy listen %s: %v (skipping)", httpListen, err)
		} else {
			log.Printf("miragec: HTTP  proxy on %s", httpListen)
			go client.ServeHTTPProxy(httpLn, c, nil)
		}
	}

	fmt.Printf("\nMIRAGE proxy running\n")
	fmt.Printf("  SOCKS5 : %s\n", socks5)
	if strings.TrimSpace(httpListen) != "" {
		fmt.Printf("  HTTP   : %s\n", httpListen)
	}
	fmt.Printf("  Server : %s\n\n", cfg.Server)

	if tun {
		vpsHost, _, _ := net.SplitHostPort(cfg.Server)
		if err := tunmode.Start(socks5, vpsHost); err != nil {
			log.Fatalf("miragec: TUN: %v", err)
		}
		fmt.Println("TUN mode active — all traffic captured. Press Ctrl+C to stop.")
	} else {
		sysproxy.Set(httpListen, socks5)
		fmt.Println("System proxy set. Press Ctrl+C to stop.")
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Println("miragec: shutting down")

	if tun {
		vpsHost, _, _ := net.SplitHostPort(cfg.Server)
		tunmode.Stop(vpsHost)
	} else {
		sysproxy.Clear()
	}
	socks5Ln.Close()
}

// configFromURI parses a mirage:// URI into a ClientConfig ready for use.
func configFromURI(raw string) (*config.ClientConfig, error) {
	s, err := uri.Decode(raw)
	if err != nil {
		return nil, err
	}
	// uri.Decode returns CertPinBase64 as standard base64, but parseSpecClientFields
	// calls ParseBase64URLNoPad which expects base64url no-pad — convert here.
	certPin := ""
	if strings.TrimSpace(s.CertPinBase64) != "" {
		pinBytes, err := base64.StdEncoding.DecodeString(s.CertPinBase64)
		if err != nil {
			return nil, fmt.Errorf("cert_pin decode: %w", err)
		}
		certPin = base64.RawURLEncoding.EncodeToString(pinBytes)
	}
	cfg := &config.ClientConfig{
		Name:              s.Name,
		Server:            s.Addr,
		PSK:               s.PSKBase64,
		SNI:               s.SNI,
		CertPin:           certPin,
		ClientPaddingSeed: s.PaddingSeedBase64,
		UTLSProfile:       "chrome_auto",
		ProxyMode:         "manual",
	}
	if strings.TrimSpace(s.PubKeyBase64) != "" {
		cfg.ServerPubKey       = s.PubKeyBase64
		cfg.ShortID            = s.ShortID
		cfg.InsecureSkipVerify = s.InsecureSkipVerify
	}
	if err := config.ParseClientFields(cfg); err != nil {
		return nil, fmt.Errorf("parse config from uri: %w", err)
	}
	return cfg, nil
}

// runDashboard starts the embedded web UI at dashAddr.
func runDashboard(serversFile string, noBrowser bool) error {
	dash := dashboard.New(serversFile)
	dash.SetProbeAddr(dashAddr)
	ln, err := net.Listen("tcp", dashAddr)
	if err != nil {
		return fmt.Errorf("dashboard listen %s: %w", dashAddr, err)
	}

	dashURL := "http://" + dashAddr
	log.Printf("miragec: dashboard at %s", dashURL)
	fmt.Printf("\nMIRAGE client dashboard: %s\n\n", dashURL)

	if !noBrowser {
		go openBrowser(dashURL)
	}

	return http.Serve(ln, dash.Handler())
}

func defaultServersFile() string {
	exe, err := os.Executable()
	if err != nil {
		return "servers.json"
	}
	return filepath.Join(filepath.Dir(exe), "servers.json")
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}
