//go:build windows

package sysproxy

import (
	"bytes"
	"io"
	"os/exec"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

type Snapshot struct {
	ProxyEnable  string            `json:"proxyEnable"`
	ProxyServer  string            `json:"proxyServer"`
	ProxyOverride string           `json:"proxyOverride"`
	AutoDetect   string            `json:"autoDetect"`
	WinHTTP      string            `json:"winHttp"`
	Env          map[string]string `json:"env"`
}

func SnapshotState() Snapshot {
	return Snapshot{
		ProxyEnable:   queryReg(regPath, "ProxyEnable"),
		ProxyServer:   queryReg(regPath, "ProxyServer"),
		ProxyOverride: queryReg(regPath, "ProxyOverride"),
		AutoDetect:    queryReg(regPath, "AutoDetect"),
		WinHTTP:       queryWinHTTP(),
		Env: map[string]string{
			"HTTP_PROXY":  queryReg(envRegPath, "HTTP_PROXY"),
			"HTTPS_PROXY": queryReg(envRegPath, "HTTPS_PROXY"),
			"ALL_PROXY":   queryReg(envRegPath, "ALL_PROXY"),
			"NO_PROXY":    queryReg(envRegPath, "NO_PROXY"),
		},
	}
}

func queryReg(path, value string) string {
	out, err := exec.Command("reg", "query", path, "/v", value).CombinedOutput()
	text := decodeWindowsText(out)
	if err != nil {
		if strings.Contains(text, "unable to find") || strings.Contains(text, "找不到") || strings.Contains(text, "无法找到") {
			return ""
		}
		return strings.TrimSpace(text)
	}
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(line, value) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 3 {
			return strings.Join(fields[2:], " ")
		}
	}
	return strings.TrimSpace(text)
}

func queryWinHTTP() string {
	out, err := exec.Command("netsh", "winhttp", "show", "proxy").CombinedOutput()
	text := decodeWindowsText(out)
	if err != nil {
		return strings.TrimSpace(text)
	}
	return normalizeMultiline(text)
}

func normalizeMultiline(s string) string {
	lines := strings.Split(s, "\n")
	var out []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return strings.Join(out, " | ")
}

func ParseNetshProxy(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "\ufeff")
	return normalizeMultiline(raw)
}

func CombinedOutput(command string, args ...string) string {
	cmd := exec.Command(command, args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	_ = cmd.Run()
	return strings.TrimSpace(decodeWindowsText(buf.Bytes()))
}

func decodeWindowsText(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	reader := transform.NewReader(bytes.NewReader(b), simplifiedchinese.GBK.NewDecoder())
	decoded, err := io.ReadAll(reader)
	if err == nil && utf8.Valid(decoded) {
		return string(decoded)
	}
	return string(b)
}
