//go:build !windows

package sysproxy

func ApplySystem(_, _ string, _ ApplyOptions) error { return nil }
func ApplyPAC(_ string, _ ApplyOptions) error       { return nil }
func ClearAll(_ ApplyOptions) error                 { return nil }
func Rebroadcast()                                  {}
func ApplyWinHTTPElevated(string) error { return nil }
