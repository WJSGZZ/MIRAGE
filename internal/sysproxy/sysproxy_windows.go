//go:build windows

// Package sysproxy sets and clears the Windows system proxy.
package sysproxy

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"
)

const regPath = `HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`
const envRegPath = `HKCU\Environment`

func ApplySystem(httpAddr, socksAddr string, opts ApplyOptions) error {
	opts = opts.withDefaults()
	value := "http=" + httpAddr + ";https=" + httpAddr + ";socks=" + socksAddr
	exec.Command("reg", "add", regPath, "/v", "ProxyEnable", "/t", "REG_DWORD", "/d", "1", "/f").Run() //nolint
	exec.Command("reg", "add", regPath, "/v", "ProxyServer", "/t", "REG_SZ", "/d", value, "/f").Run() //nolint
	exec.Command("reg", "add", regPath, "/v", "ProxyOverride", "/t", "REG_SZ", "/d", opts.ProxyOverride, "/f").Run() //nolint
	exec.Command("reg", "add", regPath, "/v", "AutoDetect", "/t", "REG_DWORD", "/d", "0", "/f").Run() //nolint
	exec.Command("reg", "delete", regPath, "/v", "AutoConfigURL", "/f").Run() //nolint
	if opts.ExportEnv {
		setEnv("HTTP_PROXY", "http://"+httpAddr)
		setEnv("HTTPS_PROXY", "http://"+httpAddr)
		setEnv("ALL_PROXY", "socks5://"+socksAddr)
		setEnv("NO_PROXY", "127.0.0.1,localhost,::1")
	}
	if opts.ApplyWinHTTP {
		setWinHTTP(httpAddr)
	}
	refresh()
	return nil
}

func ApplyPAC(pacURL string, opts ApplyOptions) error {
	opts = opts.withDefaults()
	exec.Command("reg", "add", regPath, "/v", "ProxyEnable", "/t", "REG_DWORD", "/d", "0", "/f").Run() //nolint
	exec.Command("reg", "delete", regPath, "/v", "ProxyServer", "/f").Run() //nolint
	exec.Command("reg", "delete", regPath, "/v", "ProxyOverride", "/f").Run() //nolint
	exec.Command("reg", "add", regPath, "/v", "AutoDetect", "/t", "REG_DWORD", "/d", "0", "/f").Run() //nolint
	exec.Command("reg", "add", regPath, "/v", "AutoConfigURL", "/t", "REG_SZ", "/d", pacURL, "/f").Run() //nolint
	if opts.ExportEnv {
		if opts.HTTPProxyAddr != "" {
			setEnv("HTTP_PROXY", "http://"+opts.HTTPProxyAddr)
			setEnv("HTTPS_PROXY", "http://"+opts.HTTPProxyAddr)
		}
		if opts.SocksProxyAddr != "" {
			setEnv("ALL_PROXY", "socks5://"+opts.SocksProxyAddr)
		}
		setEnv("NO_PROXY", "127.0.0.1,localhost,::1")
	}
	if opts.ApplyWinHTTP && opts.HTTPProxyAddr != "" {
		setWinHTTP(opts.HTTPProxyAddr)
	}
	refresh()
	return nil
}

func ClearAll(opts ApplyOptions) error {
	exec.Command("reg", "add", regPath, "/v", "ProxyEnable", "/t", "REG_DWORD", "/d", "0", "/f").Run() //nolint
	exec.Command("reg", "delete", regPath, "/v", "ProxyServer", "/f").Run() //nolint
	exec.Command("reg", "delete", regPath, "/v", "ProxyOverride", "/f").Run() //nolint
	exec.Command("reg", "delete", regPath, "/v", "AutoConfigURL", "/f").Run() //nolint
	exec.Command("reg", "add", regPath, "/v", "AutoDetect", "/t", "REG_DWORD", "/d", "0", "/f").Run() //nolint
	if opts.ExportEnv {
		clearEnv("HTTP_PROXY")
		clearEnv("HTTPS_PROXY")
		clearEnv("ALL_PROXY")
		clearEnv("NO_PROXY")
	}
	if opts.ApplyWinHTTP {
		resetWinHTTP()
	}
	refresh()
	return nil
}

func setEnv(name, value string) {
	_ = os.Setenv(name, value)
	exec.Command("reg", "add", envRegPath, "/v", name, "/t", "REG_SZ", "/d", value, "/f").Run() //nolint
}

func clearEnv(name string) {
	_ = os.Unsetenv(name)
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
		return errors.New(strings.TrimSpace(text))
	}
	return nil
}

// refresh notifies WinINet (Chrome/Edge/IE) that proxy settings changed.
func refresh() {
	wininet := syscall.NewLazyDLL("wininet.dll")
	fn := wininet.NewProc("InternetSetOptionW")
	fn.Call(0, 39, 0, 0) // INTERNET_OPTION_SETTINGS_CHANGED
	fn.Call(0, 37, 0, 0) // INTERNET_OPTION_REFRESH
	fn.Call(0, 95, 0, 0) // INTERNET_OPTION_PROXY_SETTINGS_CHANGED

	user32 := syscall.NewLazyDLL("user32.dll")
	sendMessageTimeout := user32.NewProc("SendMessageTimeoutW")
	internetSettings, _ := syscall.UTF16PtrFromString("Internet Settings")
	internetSettingsPath, _ := syscall.UTF16PtrFromString(`Software\Microsoft\Windows\CurrentVersion\Internet Settings`)
	envName, _ := syscall.UTF16PtrFromString("Environment")
	const hwndBroadcast = 0xffff
	const wmSettingChange = 0x001A
	const smtoAbortIfHung = 0x0002
	broadcastSettingChange(sendMessageTimeout, internetSettings, hwndBroadcast, wmSettingChange, smtoAbortIfHung)
	broadcastSettingChange(sendMessageTimeout, internetSettingsPath, hwndBroadcast, wmSettingChange, smtoAbortIfHung)
	broadcastSettingChange(sendMessageTimeout, envName, hwndBroadcast, wmSettingChange, smtoAbortIfHung)
}

func Rebroadcast() {
	refresh()
}

func broadcastSettingChange(sendMessageTimeout *syscall.LazyProc, value *uint16, hwndBroadcast, msg, flags uintptr) {
	sendMessageTimeout.Call(
		hwndBroadcast,
		msg,
		0,
		uintptr(unsafe.Pointer(value)),
		flags,
		5000,
		0,
	)
}
