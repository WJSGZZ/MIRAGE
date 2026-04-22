package sysproxy

func Set(httpAddr, socksAddr string) {
	_ = ApplySystem(httpAddr, socksAddr, ApplyOptions{
		ApplyWinHTTP: true,
		ExportEnv:    true,
	})
}

func Clear() {
	_ = ClearAll(ApplyOptions{
		ApplyWinHTTP: true,
		ExportEnv:    true,
	})
}
