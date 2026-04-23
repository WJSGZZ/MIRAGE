//go:build windows

// Package tun sets up a WinTun-based TUN device and routes all traffic
// through a local SOCKS5 proxy, capturing traffic from every process.
// It uses github.com/xjasonlyu/tun2socks for the gVisor TCP/IP stack.
package tun

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/xjasonlyu/tun2socks/v2/engine"
)

const (
	tunName = "mirage0"
	tunIP   = "198.18.0.1"
	tunMask = "255.254.0.0"
)

// Start creates the TUN device and redirects all traffic through socks5Addr.
// vpsIP is the bare IP of the VPS server — it gets a dedicated route via the
// original gateway so the MIRAGE connection itself never loops through the TUN.
// Requires wintun.dll next to miragec.exe and administrator privileges.
func Start(socks5Addr, vpsIP string) error {
	if err := checkWintun(); err != nil {
		return err
	}

	gw, err := defaultGateway()
	if err != nil {
		return fmt.Errorf("tun: detect default gateway: %w", err)
	}
	log.Printf("miragec: TUN mode  gateway=%s  VPS=%s", gw, vpsIP)

	engine.Insert(&engine.Key{
		Device:   "tun://" + tunName,
		Proxy:    "socks5://" + socks5Addr,
		LogLevel: "warn",
		MTU:      1500,
	})
	engine.Start() // fatal on error (wintun load failure, permission, etc.)

	// Give the TUN interface a moment to register with Windows before netsh.
	time.Sleep(600 * time.Millisecond)

	if err := setupRoutes(vpsIP, gw); err != nil {
		engine.Stop()
		return fmt.Errorf("tun: route setup: %w", err)
	}
	return nil
}

// Stop tears down the TUN device and restores original routing.
func Stop(vpsIP string) {
	teardownRoutes(vpsIP)
	engine.Stop()
	log.Println("miragec: TUN stopped")
}

// setupRoutes assigns the TUN interface an IP and installs routes:
//   - VPS IP  →  original gateway  (prevents routing loop)
//   - 0.0.0.0/0 →  TUN  (captures all other traffic)
func setupRoutes(vpsIP, gw string) error {
	cmds := [][]string{
		// Assign IP to the TUN interface
		{"netsh", "interface", "ip", "set", "address", tunName, "static", tunIP, tunMask},
		// Protect the VPS route so MIRAGE's own TCP connection never hits the TUN
		{"route", "ADD", vpsIP, "MASK", "255.255.255.255", gw},
		// Default route via TUN — lowest metric wins
		{"route", "ADD", "0.0.0.0", "MASK", "0.0.0.0", tunIP, "METRIC", "1"},
	}
	for _, argv := range cmds {
		out, err := exec.Command(argv[0], argv[1:]...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("command %q: %w (output: %s)", strings.Join(argv, " "), err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

func teardownRoutes(vpsIP string) {
	exec.Command("route", "DELETE", vpsIP, "MASK", "255.255.255.255").Run()          //nolint:errcheck
	exec.Command("route", "DELETE", "0.0.0.0", "MASK", "0.0.0.0", tunIP).Run()      //nolint:errcheck
}

// defaultGateway returns the current default IPv4 gateway using PowerShell.
func defaultGateway() (string, error) {
	out, err := exec.Command(
		"powershell", "-NoProfile", "-Command",
		"(Get-NetRoute -DestinationPrefix '0.0.0.0/0' | Sort-Object -Property { $_.InterfaceMetric + $_.RouteMetric } | Select-Object -First 1).NextHop",
	).Output()
	if err != nil {
		return "", err
	}
	gw := strings.TrimSpace(string(out))
	if net.ParseIP(gw) == nil {
		return "", fmt.Errorf("unexpected value from Get-NetRoute: %q", gw)
	}
	return gw, nil
}

// checkWintun looks for wintun.dll next to the executable or in the current
// directory. wintun.dll is required by the wireguard-go TUN driver on Windows.
func checkWintun() error {
	exe, _ := os.Executable()
	for _, dir := range []string{filepath.Dir(exe), "."} {
		if _, err := os.Stat(filepath.Join(dir, "wintun.dll")); err == nil {
			return nil
		}
	}
	return fmt.Errorf(
		"TUN mode requires wintun.dll — download from https://www.wintun.net/\n" +
			"  Place wintun.dll in the same folder as miragec.exe and retry.",
	)
}
