//go:build !windows

package main

func openAppWindow(string) bool {
	return false
}
