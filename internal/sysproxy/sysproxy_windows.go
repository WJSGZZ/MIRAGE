//go:build windows

// Package sysproxy sets and clears the Windows system proxy.
package sysproxy

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"
)

const regPath = `HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`
const envRegPath = `HKCU\Environment`

// Set enables the Windows system proxy, routing HTTP/HTTPS and SOCKS-aware
// apps through the local proxy endpoints.
func Set(httpAddr, socksAddr string) {
	exec.Command("reg", "add", regPath, "/v", "ProxyEnable", "/t", "REG_DWORD", "/d", "1", "/f").Run() //nolint
	value := "http=" + httpAddr + ";https=" + httpAddr + ";socks=" + socksAddr
	exec.Command("reg", "add", regPath, "/v", "ProxyServer", "/t", "REG_SZ", "/d", value, "/f").Run() //nolint
	exec.Command("reg", "add", regPath, "/v", "ProxyOverride", "/t", "REG_SZ", "/d", "<local>;localhost;127.0.0.1;::1", "/f").Run() //nolint
	exec.Command("reg", "add", regPath, "/v", "AutoDetect", "/t", "REG_DWORD", "/d", "0", "/f").Run()                                     //nolint
	setEnv("HTTP_PROXY", "http://"+httpAddr)
	setEnv("HTTPS_PROXY", "http://"+httpAddr)
	setEnv("ALL_PROXY", "socks5://"+socksAddr)
	setEnv("NO_PROXY", "127.0.0.1,localhost,::1")
	setWinHTTP(httpAddr)
	refresh()
}

// Clear disables the Windows system proxy.
func Clear() {
	exec.Command("reg", "add", regPath, "/v", "ProxyEnable", "/t", "REG_DWORD", "/d", "0", "/f").Run() //nolint
	clearEnv("HTTP_PROXY")
	clearEnv("HTTPS_PROXY")
	clearEnv("ALL_PROXY")
	clearEnv("NO_PROXY")
	resetWinHTTP()
	refresh()
}

func setEnv(name, value string) {
	exec.Command("reg", "add", envRegPath, "/v", name, "/t", "REG_SZ", "/d", value, "/f").Run() //nolint
}

func clearEnv(name string) {
	exec.Command("reg", "delete", envRegPath, "/v", name, "/f").Run() //nolint
}

func setWinHTTP(httpAddr string) {
	exec.Command(
		"netsh",
		"winhttp",
		"set",
		"proxy",
		"proxy-server="+httpAddr,
		"bypass-list=localhost;127.0.0.1;::1",
	).Run() //nolint
}

func resetWinHTTP() {
	exec.Command("netsh", "winhttp", "reset", "proxy").Run() //nolint
}

// ApplyWinHTTPElevated prompts for elevation and applies the MIRAGE HTTP proxy to
// the machine-wide WinHTTP stack. This is required for software that ignores
// WinINet and only reads WinHTTP.
func ApplyWinHTTPElevated(httpAddr string) error {
	script := fmt.Sprintf(
		"$p = Start-Process -FilePath 'netsh' -Verb RunAs -Wait -PassThru -ArgumentList @('winhttp','set','proxy','proxy-server=%s','bypass-list=localhost;127.0.0.1;::1'); exit $p.ExitCode",
		httpAddr,
	)
	out, err := exec.Command(
		"powershell",
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy", "Bypass",
		"-Command", script,
	).CombinedOutput()
	if err != nil {
		text := decodeWindowsText(out)
		if text == "" {
			return err
		}
		return fmt.Errorf(strings.TrimSpace(text))
	}
	return nil
}

// refresh notifies WinINet (Chrome/Edge/IE) that proxy settings changed.
func refresh() {
	wininet := syscall.NewLazyDLL("wininet.dll")
	fn := wininet.NewProc("InternetSetOptionW")
	fn.Call(0, 39, 0, 0) // INTERNET_OPTION_SETTINGS_CHANGED
	fn.Call(0, 37, 0, 0) // INTERNET_OPTION_REFRESH

	user32 := syscall.NewLazyDLL("user32.dll")
	sendMessageTimeout := user32.NewProc("SendMessageTimeoutW")
	envName, _ := syscall.UTF16PtrFromString("Environment")
	const hwndBroadcast = 0xffff
	const wmSettingChange = 0x001A
	const smtoAbortIfHung = 0x0002
	sendMessageTimeout.Call(
		uintptr(hwndBroadcast),
		uintptr(wmSettingChange),
		0,
		uintptr(unsafe.Pointer(envName)),
		uintptr(smtoAbortIfHung),
		5000,
		0,
	)
}
