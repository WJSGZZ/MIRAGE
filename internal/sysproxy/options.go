package sysproxy

type ApplyOptions struct {
	ApplyWinHTTP bool   `json:"applyWinHttp"`
	ExportEnv    bool   `json:"exportEnv"`
	ProxyOverride string `json:"proxyOverride,omitempty"`
	HTTPProxyAddr string `json:"httpProxyAddr,omitempty"`
	SocksProxyAddr string `json:"socksProxyAddr,omitempty"`
}

func (o ApplyOptions) withDefaults() ApplyOptions {
	if o.ProxyOverride == "" {
		o.ProxyOverride = "<local>;localhost;127.0.0.1;::1"
	}
	return o
}
