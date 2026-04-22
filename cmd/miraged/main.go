package main

import (
	"crypto/ecdh"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"

	"miraged/internal/certutil"
	"miraged/internal/config"
	"miraged/internal/server"
	"miraged/internal/uri"
)

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	cfgPath := flag.String("c", "config.json", "server config file")
	doGenKey := flag.Bool("genkey", false, "generate sample bootstrap material, then exit")
	flag.Parse()

	if *doGenKey {
		genKey()
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

func genKey() {
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	psk := make([]byte, 32)
	serverSeed := make([]byte, 16)
	clientSeed := make([]byte, 16)
	_, _ = rand.Read(psk)
	_, _ = rand.Read(serverSeed)
	_, _ = rand.Read(clientSeed)

	privB64 := base64.StdEncoding.EncodeToString(priv.Bytes())
	pubB64 := base64.StdEncoding.EncodeToString(priv.PublicKey().Bytes())
	pskB64 := base64.StdEncoding.EncodeToString(psk)
	serverSeedB64 := base64.StdEncoding.EncodeToString(serverSeed)
	clientSeedB64 := base64.StdEncoding.EncodeToString(clientSeed)

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
		// Temporary compatibility fields while the transport is being migrated.
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
		// Temporary compatibility fields while the transport is being migrated.
		"listen":       "127.0.0.1:1080",
		"serverPubKey": pubB64,
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
