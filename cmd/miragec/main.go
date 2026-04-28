package main

import (
	"bufio"
	"encoding/base64"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
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

	cfgPath := flag.String("c", "", "client config file; starts headless proxy without API")
	uriStr := flag.String("uri", "", "mirage:// URI; starts headless proxy directly")
	socks5Addr := flag.String("socks5", "", "override SOCKS5 listen addr (headless mode)")
	httpAddr := flag.String("http", "", "override HTTP proxy listen addr (headless mode)")
	tunMode := flag.Bool("tun", false, "TUN mode for headless proxy (requires admin + wintun.dll)")
	setSystemProxy := flag.Bool("set-system-proxy", false, "headless mode: also write Windows system proxy settings")

	coreMode := flag.Bool("core", false, "run local MIRAGE control API")
	dashListen := flag.String("dashboard", dashAddr, "control API listen address")
	serversFile := flag.String("servers", defaultServersFile(), "servers.json path (API mode)")
	importURI := flag.String("import-uri", "", "import mirage:// URI into servers.json before serving API")
	connectID := flag.String("connect-id", "", "auto-connect profile ID after startup")
	connectLast := flag.Bool("connect-last", false, "auto-connect the last profile in servers.json after startup")

	flag.Parse()

	if *tunMode || *setSystemProxy {
		log.Fatalf("miragec: direct TUN/system-proxy control is disabled; run bridge mode and let Clash Verge manage System Proxy or TUN")
	}

	switch {
	case strings.TrimSpace(*cfgPath) != "":
		cfg, err := config.LoadClient(*cfgPath)
		if err != nil {
			log.Fatalf("miragec: config: %v", err)
		}
		runHeadless(cfg, *socks5Addr, *httpAddr, *tunMode, *setSystemProxy)

	case strings.TrimSpace(*uriStr) != "":
		cfg, err := configFromURI(*uriStr)
		if err != nil {
			log.Fatalf("miragec: uri: %v", err)
		}
		runHeadless(cfg, *socks5Addr, *httpAddr, *tunMode, *setSystemProxy)

	default:
		if flag.NFlag() == 0 {
			if err := runInteractive(defaultServersFile(), dashAddr); err != nil {
				log.Fatalf("miragec: %v", err)
			}
			return
		}
		if err := runCore(*serversFile, *dashListen, *coreMode, *importURI, *connectID, *connectLast); err != nil {
			log.Fatalf("miragec: %v", err)
		}
	}
}

func runInteractive(serversFile, addr string) error {
	fmt.Println("MIRAGE Clash bridge")
	fmt.Printf("Profiles are saved in: %s\n", serversFile)
	fmt.Println("Choose a saved profile or import a new mirage:// link.")
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)
	dash := dashboard.New(serversFile)
	profiles := dash.Servers()
	for i, profile := range profiles {
		fmt.Printf("  %d) %s  [%s]\n", i+1, profile.Name, profile.Addr)
	}
	if len(profiles) > 0 {
		fmt.Println("  n) Import new mirage://")
		fmt.Println("  q) Quit")
		fmt.Print("Select profile [1]: ")
		choice, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("read selection: %w", err)
		}
		choice = strings.TrimSpace(strings.ToLower(choice))
		if choice == "" {
			choice = "1"
		}
		if choice == "q" || choice == "quit" {
			return nil
		}
		if choice != "n" && choice != "new" {
			idx, err := strconv.Atoi(choice)
			if err != nil || idx < 1 || idx > len(profiles) {
				return fmt.Errorf("invalid selection %q", choice)
			}
			fmt.Println()
			fmt.Printf("Starting MIRAGE core with %s...\n", profiles[idx-1].Name)
			return runCore(serversFile, addr, true, "", profiles[idx-1].ID, false)
		}
	}

	if len(profiles) == 0 {
		fmt.Println("No saved profiles yet.")
	}
	fmt.Print("mirage://... > ")
	rawURI, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("read URI: %w", err)
	}
	rawURI = strings.TrimSpace(rawURI)
	if rawURI == "" {
		return fmt.Errorf("empty mirage:// link")
	}
	if !strings.HasPrefix(strings.ToLower(rawURI), "mirage://") {
		return fmt.Errorf("expected a mirage:// link")
	}

	fmt.Println()
	fmt.Println("Starting MIRAGE core in bridge mode...")
	return runCore(serversFile, addr, true, rawURI, "", true)
}

// runHeadless starts SOCKS5 and HTTP listeners directly without the API service.
func runHeadless(cfg *config.ClientConfig, socks5Override, httpOverride string, tun, setSystemProxy bool) {
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
	if strings.TrimSpace(httpListen) == "" {
		httpListen = "127.0.0.1:1081"
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
			log.Printf("miragec: HTTP proxy on %s", httpListen)
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
		fmt.Println("TUN mode active; all traffic captured. Press Ctrl+C to stop.")
	} else if setSystemProxy {
		sysproxy.Set(httpListen, socks5)
		fmt.Println("System proxy set. Press Ctrl+C to stop.")
	} else {
		fmt.Println("Bridge mode active; system proxy was not changed. Press Ctrl+C to stop.")
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Println("miragec: shutting down")

	if tun {
		vpsHost, _, _ := net.SplitHostPort(cfg.Server)
		tunmode.Stop(vpsHost)
	} else if setSystemProxy {
		sysproxy.Clear()
	}
	_ = socks5Ln.Close()
}

// configFromURI parses a mirage:// URI into a ClientConfig.
func configFromURI(raw string) (*config.ClientConfig, error) {
	s, err := uri.Decode(raw)
	if err != nil {
		return nil, err
	}

	// uri.Decode returns cert pin in standard base64.
	// ParseClientFields accepts base64url without padding, so convert here.
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
		cfg.ServerPubKey = s.PubKeyBase64
		cfg.ShortID = s.ShortID
		cfg.InsecureSkipVerify = s.InsecureSkipVerify
	}
	if err := config.ParseClientFields(cfg); err != nil {
		return nil, fmt.Errorf("parse config from uri: %w", err)
	}
	return cfg, nil
}

func runCore(serversFile, addr string, coreMode bool, importURI, connectID string, connectLast bool) error {
	dash := dashboard.New(serversFile)
	dash.SetBridgeMode()
	if strings.TrimSpace(addr) == "" {
		addr = dashAddr
	}
	dash.SetProbeAddr(addr)

	if strings.TrimSpace(importURI) != "" {
		saved, err := dash.ImportURI(importURI, "")
		if err != nil {
			return fmt.Errorf("import-uri failed: %w", err)
		}
		log.Printf("miragec: imported profile %s (%s)", saved.ID, saved.Name)
		if strings.TrimSpace(connectID) == "" {
			connectID = saved.ID
		}
	}
	if connectLast && strings.TrimSpace(connectID) == "" {
		list := dash.Servers()
		if len(list) > 0 {
			connectID = list[len(list)-1].ID
		}
	}
	if strings.TrimSpace(connectID) != "" {
		if _, err := dash.Connect(connectID); err != nil {
			return fmt.Errorf("auto connect failed: %w", err)
		}
		log.Printf("miragec: auto connected profile %s", connectID)
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("dashboard listen %s: %w", addr, err)
	}
	if coreMode {
		log.Printf("miragec: core API at http://%s", addr)
	} else {
		log.Printf("miragec: control API at http://%s", addr)
	}
	printClashInstructions(addr, dash.State())
	return http.Serve(ln, dash.Handler())
}

func printClashInstructions(addr string, state dashboard.State) {
	if strings.TrimSpace(addr) == "" {
		addr = dashAddr
	}
	url := "http://" + addr + "/compat/mihomo.yaml"
	fmt.Println()
	fmt.Println("MIRAGE bridge is ready.")
	if state.Running {
		fmt.Printf("Local SOCKS : %s\n", state.Socks5)
		fmt.Printf("Local HTTP  : %s\n", state.HTTP)
	}
	fmt.Println("Clash URL   : " + url)
	fmt.Println()
	fmt.Println("Import that URL in Clash Verge Rev, then enable Clash System Proxy or TUN.")
	fmt.Println("MIRAGE will not change Windows proxy, WinHTTP, or proxy environment variables.")
	fmt.Println("Press Ctrl+C to stop MIRAGE core.")
}

func defaultServersFile() string {
	exe, err := os.Executable()
	if err != nil {
		return "profiles"
	}
	return filepath.Join(filepath.Dir(exe), "profiles")
}
