// Package daemon manages the lifecycle of the SOCKS5 proxy.
// Start/Stop can be called repeatedly as the user connects and disconnects.
package daemon

import (
	"fmt"
	"net"
	"strconv"
	"sync"

	"miraged/internal/client"
	"miraged/internal/config"
)

// Daemon owns the local proxy listeners and one MIRAGE client session.
type Daemon struct {
	mu         sync.Mutex
	lnSocks    net.Listener
	lnHTTP     net.Listener
	cli        *client.Client
	running    bool
	listenSocks string // e.g. "127.0.0.1:1080"
	listenHTTP  string // e.g. "127.0.0.1:1081"
}

// Start connects to the server described by cfg and begins accepting local
// SOCKS5 and HTTP proxy connections. Stops any previously running session first.
func (d *Daemon) Start(cfg *config.ClientConfig) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.stopLocked()

	httpListen, err := nextPortAddr(cfg.Listen)
	if err != nil {
		return fmt.Errorf("daemon: derive http listen from %s: %w", cfg.Listen, err)
	}

	lnSocks, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return fmt.Errorf("daemon: socks5 listen %s: %w", cfg.Listen, err)
	}
	lnHTTP, err := net.Listen("tcp", httpListen)
	if err != nil {
		lnSocks.Close()
		return fmt.Errorf("daemon: http listen %s: %w", httpListen, err)
	}

	c := client.New(cfg)
	d.lnSocks = lnSocks
	d.lnHTTP = lnHTTP
	d.cli = c
	d.running = true
	d.listenSocks = cfg.Listen
	d.listenHTTP = httpListen

	go client.Serve(lnSocks, c)
	go client.ServeHTTPProxy(lnHTTP, c)
	return nil
}

// Stop tears down the active session and listener.
func (d *Daemon) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.stopLocked()
}

func (d *Daemon) stopLocked() {
	if !d.running {
		return
	}
	if d.lnSocks != nil {
		d.lnSocks.Close()
		d.lnSocks = nil
	}
	if d.lnHTTP != nil {
		d.lnHTTP.Close()
		d.lnHTTP = nil
	}
	d.listenSocks = ""
	d.listenHTTP = ""
	d.running = false
}

// Running reports whether the proxy is currently active.
func (d *Daemon) Running() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.running
}

// SocksListen returns the SOCKS5 listen address (empty when not running).
func (d *Daemon) SocksListen() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.running {
		return ""
	}
	return d.listenSocks
}

// HTTPListen returns the HTTP proxy listen address (empty when not running).
func (d *Daemon) HTTPListen() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.running {
		return ""
	}
	return d.listenHTTP
}

func nextPortAddr(addr string) (string, error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return "", err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return "", err
	}
	if port >= 65535 {
		return "", fmt.Errorf("port %d cannot be incremented", port)
	}
	return net.JoinHostPort(host, strconv.Itoa(port+1)), nil
}
