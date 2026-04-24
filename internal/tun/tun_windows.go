//go:build windows

// Package tun sets up a WinTun-based TUN device and routes traffic through the
// local MIRAGE SOCKS5 proxy.
package tun

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/xjasonlyu/tun2socks/v2/engine"
)

const (
	tunName = "mirage0"
	tunIP   = "198.18.0.1"
	tunMask = "255.254.0.0"
)

// Start creates the TUN device and redirects IPv4 traffic through socks5Addr.
// vpsIP is protected through the original gateway so MIRAGE never loops through
// its own TUN route.
func Start(socks5Addr, vpsIP string) error {
	if err := checkWintun(); err != nil {
		return err
	}

	gw, err := defaultGateway()
	if err != nil {
		return fmt.Errorf("tun: detect default gateway: %w", err)
	}
	log.Printf("miragec: TUN mode gateway=%s VPS=%s", gw, vpsIP)

	engine.Insert(&engine.Key{
		Device:   "tun://" + tunName,
		Proxy:    "socks5://" + socks5Addr,
		LogLevel: "warn",
		MTU:      1500,
	})
	engine.Start()

	time.Sleep(900 * time.Millisecond)
	if err := setupRoutes(vpsIP, gw); err != nil {
		engine.Stop()
		return fmt.Errorf("tun: route setup: %w", err)
	}
	return nil
}

// Stop tears down the TUN device and restores MIRAGE-managed routes.
func Stop(vpsIP string) {
	teardownRoutes(vpsIP)
	engine.Stop()
	log.Println("miragec: TUN stopped")
}

var dnsServers = []string{"8.8.8.8", "8.8.4.4", "1.1.1.1", "1.0.0.1"}

func setupRoutes(vpsIP, gw string) error {
	ifIndex, err := interfaceIndex(tunName)
	if err != nil {
		return err
	}
	cmds := [][]string{
		{"netsh", "interface", "ip", "set", "address", tunName, "static", tunIP, tunMask},
		{"netsh", "interface", "ipv4", "set", "interface", tunName, "metric=1"},
		{"route", "ADD", vpsIP, "MASK", "255.255.255.255", gw},
		{"route", "ADD", "0.0.0.0", "MASK", "128.0.0.0", "0.0.0.0", "IF", strconv.Itoa(ifIndex), "METRIC", "1"},
		{"route", "ADD", "128.0.0.0", "MASK", "128.0.0.0", "0.0.0.0", "IF", strconv.Itoa(ifIndex), "METRIC", "1"},
	}
	for _, argv := range cmds {
		out, err := exec.Command(argv[0], argv[1:]...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("command %q: %w (output: %s)", strings.Join(argv, " "), err, strings.TrimSpace(string(out)))
		}
	}
	for _, dns := range dnsServers {
		exec.Command("route", "ADD", dns, "MASK", "255.255.255.255", gw).Run() //nolint:errcheck
	}
	exec.Command("netsh", "interface", "ip", "set", "dns", tunName, "static", "8.8.8.8").Run() //nolint:errcheck
	return nil
}

func teardownRoutes(vpsIP string) {
	exec.Command("route", "DELETE", vpsIP, "MASK", "255.255.255.255").Run()    //nolint:errcheck
	exec.Command("route", "DELETE", "0.0.0.0", "MASK", "128.0.0.0").Run()      //nolint:errcheck
	exec.Command("route", "DELETE", "128.0.0.0", "MASK", "128.0.0.0").Run()    //nolint:errcheck
	exec.Command("route", "DELETE", "0.0.0.0", "MASK", "0.0.0.0", tunIP).Run() //nolint:errcheck
	for _, dns := range dnsServers {
		exec.Command("route", "DELETE", dns, "MASK", "255.255.255.255").Run() //nolint:errcheck
	}
	exec.Command("netsh", "interface", "ip", "set", "dns", tunName, "dhcp").Run() //nolint:errcheck
}

func interfaceIndex(name string) (int, error) {
	out, err := exec.Command(
		"powershell", "-NoProfile", "-Command",
		fmt.Sprintf("(Get-NetAdapter -Name '%s' -ErrorAction Stop).ifIndex", strings.ReplaceAll(name, "'", "''")),
	).Output()
	if err != nil {
		return 0, fmt.Errorf("tun: find interface index for %s: %w", name, err)
	}
	idx, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil || idx <= 0 {
		return 0, fmt.Errorf("tun: invalid interface index for %s: %q", name, strings.TrimSpace(string(out)))
	}
	return idx, nil
}

func defaultGateway() (string, error) {
	out, err := exec.Command(
		"powershell", "-NoProfile", "-Command",
		"(Get-NetRoute -DestinationPrefix '0.0.0.0/0' | Where-Object { $_.NextHop -ne '0.0.0.0' } | Sort-Object -Property { $_.InterfaceMetric + $_.RouteMetric } | Select-Object -First 1).NextHop",
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

func checkWintun() error {
	exe, _ := os.Executable()
	for _, dir := range []string{filepath.Dir(exe), "."} {
		if _, err := os.Stat(filepath.Join(dir, "wintun.dll")); err == nil {
			return nil
		}
	}
	return fmt.Errorf(
		"TUN mode requires wintun.dll. Download it from https://www.wintun.net/\n" +
			"  Place wintun.dll in the same folder as miragec.exe and retry.",
	)
}
