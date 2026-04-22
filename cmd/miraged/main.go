package main

import (
	"crypto/ecdh"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"miraged/internal/certutil"
	"miraged/internal/config"
	"miraged/internal/server"
	"miraged/internal/uri"
)

type bootstrapOptions struct {
	Dir         string
	Listen      string
	PublicHost  string
	SNI         string
	Fallback    string
	UserName    string
	ProfileName string
	Overwrite   bool
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	cfgPath := flag.String("c", "config.json", "server config file")
	doGenKey := flag.Bool("genkey", false, "generate sample bootstrap material, then exit")
	doBootstrap := flag.Bool("bootstrap", false, "generate server config, cert, client config, and final mirage:// URI")
	bootstrapDir := flag.String("bootstrap-dir", ".", "output directory for -bootstrap")
	publicHost := flag.String("public-host", "", "public IP or domain for the generated client profile; auto-detected when empty")
	listenAddr := flag.String("listen", "0.0.0.0:443", "listen address for generated bootstrap config")
	sni := flag.String("sni", "www.microsoft.com", "SNI for generated client profile")
	fallback := flag.String("fallback", "www.microsoft.com:443", "fallback target for generated server config")
	userName := flag.String("user", "user1", "user name for generated server/client profile")
	profileName := flag.String("name", "my-vps-1", "profile name embedded in generated mirage:// URI")
	overwrite := flag.Bool("overwrite", false, "overwrite existing bootstrap files when using -bootstrap")
	flag.Parse()

	switch {
	case *doGenKey:
		genKey()
		return
	case *doBootstrap:
		opts := bootstrapOptions{
			Dir:         *bootstrapDir,
			Listen:      *listenAddr,
			PublicHost:  *publicHost,
			SNI:         *sni,
			Fallback:    *fallback,
			UserName:    *userName,
			ProfileName: *profileName,
			Overwrite:   *overwrite,
		}
		if err := bootstrap(opts); err != nil {
			log.Fatalf("bootstrap: %v", err)
		}
		return
	}

	cfg, err := config.LoadServer(*cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	cert, err := certutil.LoadOrGenerate(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		log.Fatalf("tls cert: %v", err)
	}
	if pin, err := certutil.SPKIPinBase64URL(cert); err == nil {
		log.Printf("miraged: cert pin (SPKI SHA-256, base64url) = %s", pin)
	}

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
	}

	if err := server.Run(cfg, tlsCfg); err != nil {
		log.Fatalf("server: %v", err)
	}
}

func bootstrap(opts bootstrapOptions) error {
	if strings.TrimSpace(opts.Dir) == "" {
		opts.Dir = "."
	}
	if strings.TrimSpace(opts.Listen) == "" {
		opts.Listen = "0.0.0.0:443"
	}
	if strings.TrimSpace(opts.SNI) == "" {
		opts.SNI = "www.microsoft.com"
	}
	if strings.TrimSpace(opts.Fallback) == "" {
		opts.Fallback = "www.microsoft.com:443"
	}
	if strings.TrimSpace(opts.UserName) == "" {
		opts.UserName = "user1"
	}
	if strings.TrimSpace(opts.ProfileName) == "" {
		opts.ProfileName = "my-vps-1"
	}

	if err := os.MkdirAll(opts.Dir, 0o755); err != nil {
		return fmt.Errorf("create bootstrap dir: %w", err)
	}

	configPath := filepath.Join(opts.Dir, "config.json")
	clientPath := filepath.Join(opts.Dir, "client.json")
	certPath := filepath.Join(opts.Dir, "mirage-cert.pem")
	keyPath := filepath.Join(opts.Dir, "mirage-key.pem")

	if !opts.Overwrite {
		for _, path := range []string{configPath, clientPath} {
			if _, err := os.Stat(path); err == nil {
				return fmt.Errorf("%s already exists; rerun with -overwrite to replace bootstrap output", path)
			}
		}
	}

	if err := ensureListenAvailable(opts.Listen); err != nil {
		return err
	}

	publicHost := strings.TrimSpace(opts.PublicHost)
	if publicHost == "" {
		host, err := detectPublicHost()
		if err != nil {
			return fmt.Errorf("detect public host: %w; pass -public-host explicitly", err)
		}
		publicHost = host
	}

	_, port, err := net.SplitHostPort(opts.Listen)
	if err != nil {
		return fmt.Errorf("parse listen address %q: %w", opts.Listen, err)
	}
	publicAddr := net.JoinHostPort(publicHost, port)

	privB64, pubB64, pskB64, serverSeedB64, clientSeedB64, err := generateBootstrapMaterial()
	if err != nil {
		return err
	}

	cert, err := certutil.LoadOrGenerate(certPath, keyPath)
	if err != nil {
		return fmt.Errorf("generate bootstrap certificate: %w", err)
	}
	certPinURL, err := certutil.SPKIPinBase64URL(cert)
	if err != nil {
		return fmt.Errorf("derive cert pin: %w", err)
	}
	certPinStd, err := base64URLToStd(certPinURL)
	if err != nil {
		return fmt.Errorf("normalize cert pin: %w", err)
	}

	serverCfg := map[string]interface{}{
		"listen":              opts.Listen,
		"fallback":            opts.Fallback,
		"cert":                certPath,
		"key":                 keyPath,
		"server_padding_seed": serverSeedB64,
		"replay_cache_ttl":    90,
		"replay_cache_cap":    10000,
		"stats_api":           "127.0.0.1:9999",
		"control_token":       "",
		"users": []map[string]string{
			{"name": opts.UserName, "psk": pskB64},
		},
		"serverKey": privB64,
		"certFile":  certPath,
		"keyFile":   keyPath,
	}
	clientCfg := map[string]interface{}{
		"name":                opts.ProfileName,
		"server":              publicAddr,
		"psk":                 pskB64,
		"sni":                 opts.SNI,
		"cert_pin":            certPinURL,
		"client_padding_seed": clientSeedB64,
		"local_socks5":        "127.0.0.1:1080",
		"stats_api":           "127.0.0.1:9999",
		"utls_profile":        "chrome_auto",
		"proxy_mode":          "system",
		"listen":              "127.0.0.1:1080",
		"serverPubKey":        pubB64,
	}

	if err := writeJSON(configPath, serverCfg); err != nil {
		return err
	}
	if err := writeJSON(clientPath, clientCfg); err != nil {
		return err
	}

	mirageURI := uri.Encode(uri.Server{
		Name:              opts.ProfileName,
		UserName:          opts.UserName,
		Addr:              publicAddr,
		PSKBase64:         pskB64,
		SNI:               opts.SNI,
		CertPinBase64:     certPinStd,
		PaddingSeedBase64: clientSeedB64,
	})

	fmt.Println("MIRAGE bootstrap complete")
	fmt.Printf("Directory:\n  %s\n\n", opts.Dir)
	fmt.Printf("Listen:\n  %s\n\n", opts.Listen)
	fmt.Printf("Public address:\n  %s\n\n", publicAddr)
	fmt.Printf("Certificate files:\n  %s\n  %s\n\n", certPath, keyPath)
	fmt.Printf("Config:\n  %s\n\n", configPath)
	fmt.Printf("Client config:\n  %s\n\n", clientPath)
	fmt.Printf("Cert pin (base64url):\n  %s\n\n", certPinURL)
	fmt.Printf("Final mirage:// URI:\n%s\n\n", mirageURI)
	fmt.Println("Start command:")
	fmt.Printf("  ./miraged -c %s\n", configPath)
	return nil
}

func genKey() {
	privB64, pubB64, pskB64, serverSeedB64, clientSeedB64, err := generateBootstrapMaterial()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Println("MIRAGE bootstrap material")
	fmt.Printf("Legacy server private key (serverKey):\n  %s\n\n", privB64)
	fmt.Printf("Legacy server public key  (serverPubKey):\n  %s\n\n", pubB64)
	fmt.Printf("Spec PSK (base64):\n  %s\n\n", pskB64)
	fmt.Printf("Spec server_padding_seed (base64):\n  %s\n\n", serverSeedB64)
	fmt.Printf("Spec client_padding_seed (base64):\n  %s\n\n", clientSeedB64)

	serverCfg := map[string]interface{}{
		"listen":              "0.0.0.0:443",
		"fallback":            "www.microsoft.com:443",
		"cert":                "",
		"key":                 "",
		"server_padding_seed": serverSeedB64,
		"replay_cache_ttl":    90,
		"replay_cache_cap":    10000,
		"stats_api":           "127.0.0.1:9999",
		"control_token":       "",
		"users": []map[string]string{
			{"name": "user1", "psk": pskB64},
		},
		"serverKey": privB64,
		"certFile":  "",
		"keyFile":   "",
	}
	serverJSON, _ := json.MarshalIndent(serverCfg, "", "  ")
	fmt.Println("server config.json")
	fmt.Println(string(serverJSON))

	clientCfg := map[string]interface{}{
		"server":              "YOUR_VPS_IP:443",
		"psk":                 pskB64,
		"sni":                 "www.microsoft.com",
		"cert_pin":            "<fill_after_first_server_boot>",
		"client_padding_seed": clientSeedB64,
		"local_socks5":        "127.0.0.1:1080",
		"stats_api":           "127.0.0.1:9999",
		"utls_profile":        "chrome_auto",
		"proxy_mode":          "system",
		"listen":              "127.0.0.1:1080",
		"serverPubKey":        pubB64,
	}
	clientJSON, _ := json.MarshalIndent(clientCfg, "", "  ")
	fmt.Println("\nclient client.json")
	fmt.Println(string(clientJSON))

	mirageURI := uri.Encode(uri.Server{
		Name:              "my-vps-1",
		UserName:          "user1",
		Addr:              "YOUR_VPS_IP:443",
		PSKBase64:         pskB64,
		SNI:               "www.microsoft.com",
		CertPinBase64:     "",
		PaddingSeedBase64: clientSeedB64,
	})
	fmt.Println("\nmirage:// import URI (fill cert_pin before sharing)")
	fmt.Println(mirageURI)
}

func generateBootstrapMaterial() (privB64, pubB64, pskB64, serverSeedB64, clientSeedB64 string, err error) {
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return "", "", "", "", "", err
	}

	psk := make([]byte, 32)
	serverSeed := make([]byte, 16)
	clientSeed := make([]byte, 16)
	_, _ = rand.Read(psk)
	_, _ = rand.Read(serverSeed)
	_, _ = rand.Read(clientSeed)

	return base64.StdEncoding.EncodeToString(priv.Bytes()),
		base64.StdEncoding.EncodeToString(priv.PublicKey().Bytes()),
		base64.StdEncoding.EncodeToString(psk),
		base64.StdEncoding.EncodeToString(serverSeed),
		base64.StdEncoding.EncodeToString(clientSeed),
		nil
}

func ensureListenAvailable(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err == nil {
		_ = ln.Close()
		return nil
	}
	if strings.Contains(strings.ToLower(err.Error()), "address already in use") {
		return fmt.Errorf("listen address %s is already occupied; inspect with: ss -ltnp | grep %s", addr, listenPort(addr))
	}
	if errors.Is(err, os.ErrPermission) {
		return fmt.Errorf("listen address %s requires elevated privileges", addr)
	}
	return fmt.Errorf("listen preflight failed for %s: %w", addr, err)
}

func listenPort(addr string) string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return ":" + port
}

func detectPublicHost() (string, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	for _, endpoint := range []string{
		"https://api.ipify.org",
		"https://ipv4.icanhazip.com",
		"https://ifconfig.me/ip",
	} {
		resp, err := client.Get(endpoint)
		if err != nil {
			continue
		}
		body, readErr := ioReadAllAndClose(resp)
		if readErr != nil {
			continue
		}
		host := strings.TrimSpace(string(body))
		if net.ParseIP(host) != nil {
			return host, nil
		}
	}
	return "", fmt.Errorf("could not auto-detect public IP")
}

func ioReadAllAndClose(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func base64URLToStd(s string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}

func writeJSON(path string, v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
