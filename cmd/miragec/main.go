package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"runtime"

	"miraged/internal/dashboard"
)

const dashAddr = "127.0.0.1:9099"

func runDashboard(serversFile string, webMode bool, noBrowser bool) error {
	dash := dashboard.New(serversFile)
	dash.SetProbeAddr(dashAddr)
	ln, err := net.Listen("tcp", dashAddr)
	if err != nil {
		return fmt.Errorf("dashboard listen %s: %w", dashAddr, err)
	}

	url := "http://" + dashAddr
	log.Printf("miragec: dashboard at %s", url)
	fmt.Printf("\nMIRAGE client dashboard: %s\n\n", url)

	if !noBrowser {
		go func() {
			if webMode || !openAppWindow(url) {
				openBrowser(url)
			}
		}()
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

func browserUserDataDir(name string) string {
	base, err := os.UserCacheDir()
	if err != nil || strings.TrimSpace(base) == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "MIRAGE", name)
}
