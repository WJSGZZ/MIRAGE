//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

func openAppWindow(url string) bool {
	for _, candidate := range []struct {
		path string
		name string
	}{
		{path: `C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`, name: "edge"},
		{path: `C:\Program Files\Microsoft\Edge\Application\msedge.exe`, name: "edge"},
		{path: `C:\Program Files\Google\Chrome\Application\chrome.exe`, name: "chrome"},
		{path: `C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`, name: "chrome"},
	} {
		if _, err := os.Stat(candidate.path); err != nil {
			continue
		}
		userDataDir := browserUserDataDir(candidate.name)
		_ = os.MkdirAll(userDataDir, 0o755)
		cmd := exec.Command(
			candidate.path,
			"--app="+url,
			"--user-data-dir="+filepath.Clean(userDataDir),
			"--disable-session-crashed-bubble",
			"--no-default-browser-check",
		)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		if err := cmd.Start(); err == nil {
			return true
		}
	}
	return false
}

func showStartupError(err error) {
	logLine := "MIRAGE startup failed: " + err.Error()
	_ = exec.Command("powershell", "-NoProfile", "-Command",
		"[void][System.Reflection.Assembly]::LoadWithPartialName('System.Windows.Forms'); [System.Windows.Forms.MessageBox]::Show("+psQuote(logLine)+", 'MIRAGE startup failed')",
	).Run()
}

func psQuote(s string) string {
	out := "'"
	for _, r := range s {
		if r == '\'' {
			out += "''"
		} else {
			out += string(r)
		}
	}
	out += "'"
	return out
}
