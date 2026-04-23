//go:build !windows

package tun

import "errors"

func Start(_, _ string) error {
	return errors.New("TUN mode is only supported on Windows")
}

func Stop(_ string) {}
