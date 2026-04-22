//go:build !windows

package sysproxy

func Set(_, _ string) {}
func Clear()          {}
func ApplyWinHTTPElevated(string) error { return nil }
