//go:build !windows

package sysproxy

type Snapshot struct {
	ProxyEnable   string            `json:"proxyEnable"`
	ProxyServer   string            `json:"proxyServer"`
	ProxyOverride string            `json:"proxyOverride"`
	AutoDetect    string            `json:"autoDetect"`
	WinHTTP       string            `json:"winHttp"`
	Env           map[string]string `json:"env"`
}

func SnapshotState() Snapshot {
	return Snapshot{
		Env: map[string]string{},
	}
}

