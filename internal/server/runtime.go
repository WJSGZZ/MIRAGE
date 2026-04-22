package server

import (
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"miraged/internal/auth"
	"miraged/internal/config"
	"miraged/internal/mux"
	"miraged/internal/protocol"
	"miraged/internal/record"
	"miraged/internal/replayconn"
	"miraged/internal/tlspeek"
)

type runtimeState struct {
	cfg    *config.ServerConfig
	tlsCfg *tls.Config

	replayMu    sync.Mutex
	replayCache map[[32]byte]time.Time
}

func newRuntime(cfg *config.ServerConfig, tlsCfg *tls.Config) *runtimeState {
	return &runtimeState{
		cfg:         cfg,
		tlsCfg:      tlsCfg,
		replayCache: make(map[[32]byte]time.Time),
	}
}

func (rt *runtimeState) run() error {
	ln, err := net.Listen("tcp", rt.cfg.Listen)
	if err != nil {
		return fmt.Errorf("listen %s: %w", rt.cfg.Listen, err)
	}
	log.Printf("miraged: listening on %s", rt.cfg.Listen)
	if rt.cfg.Fallback != "" {
		log.Printf("miraged: fallback target %s", rt.cfg.Fallback)
	}
	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("miraged: accept: %v", err)
			continue
		}
		go rt.handleConn(conn)
	}
}

func (rt *runtimeState) handleConn(rawConn net.Conn) {
	defer rawConn.Close()

	hello, err := tlspeek.ReadClientHello(rawConn, 10*time.Second)
	if err != nil {
		rt.fallbackOrReject(rawConn, nil)
		return
	}

	if rt.canTrySpecAuth() {
		ok, err := rt.trySpecHandshake(rawConn, hello)
		if err == nil && ok {
			return
		}
		if err != nil {
			log.Printf("miraged: spec auth miss from %s: %v", rawConn.RemoteAddr(), err)
		}
		if rt.canTryLegacyAuth() {
			rt.handleLegacyTLS(rawConn, hello.Raw)
			return
		}
		rt.fallbackOrReject(rawConn, hello.Raw)
		return
	}

	rt.handleLegacyTLS(rawConn, hello.Raw)
}

func (rt *runtimeState) canTrySpecAuth() bool {
	return len(rt.cfg.UserByUID) > 0
}

func (rt *runtimeState) canTryLegacyAuth() bool {
	if rt.cfg.PrivKey == nil {
		return false
	}
	for _, u := range rt.cfg.Users {
		if len(u.ShortIDBytes) > 0 {
			return true
		}
	}
	return false
}

func (rt *runtimeState) trySpecHandshake(rawConn net.Conn, hello *tlspeek.ClientHello) (bool, error) {
	if len(hello.SessionID) != 32 {
		return false, fmt.Errorf("session_id length %d", len(hello.SessionID))
	}
	uid, token, err := protocol.SplitSessionID(hello.SessionID)
	if err != nil {
		return false, err
	}
	user := rt.cfg.FindUserByUID(uid)
	if user == nil || len(user.PSKBytes) == 0 {
		return false, fmt.Errorf("uid not found")
	}

	nowWindow := protocol.TimeWindow(time.Now().Unix())
	var matchedWindow int64
	var matched bool
	for _, tw := range []int64{nowWindow - 1, nowWindow, nowWindow + 1} {
		expected, err := protocol.DeriveHMACToken(user.PSKBytes, tw, hello.Random)
		if err != nil {
			return false, err
		}
		if hmacTokenEqual(token[:], expected[:]) {
			matchedWindow = tw
			matched = true
			break
		}
	}
	if !matched {
		return false, fmt.Errorf("token mismatch")
	}

	replayKey := protocol.ReplayKey(uid, matchedWindow, hello.Random)
	if !rt.rememberReplay(replayKey) {
		return false, fmt.Errorf("replay detected")
	}

	replayed := replayconn.New(rawConn, hello.Raw)
	tlsConn := tls.Server(replayed, rt.tlsCfg)
	_ = tlsConn.SetDeadline(time.Now().Add(30 * time.Second))
	if err := tlsConn.Handshake(); err != nil {
		return false, fmt.Errorf("tls handshake after spec auth: %w", err)
	}
	_ = tlsConn.SetDeadline(time.Time{})

	log.Printf("miraged: spec session established for %s uid=%x sni=%s", user.Name, uid, hello.ServerName)
	return true, rt.serveEstablished(tlsConn, rt.cfg.ServerPaddingSeedBytes[:], false)
}

func (rt *runtimeState) handleLegacyTLS(rawConn net.Conn, prefix []byte) {
	replayed := replayconn.New(rawConn, prefix)
	tlsConn := tls.Server(replayed, rt.tlsCfg)
	defer tlsConn.Close()

	_ = tlsConn.SetDeadline(time.Now().Add(30 * time.Second))
	if err := tlsConn.Handshake(); err != nil {
		return
	}

	shortIDs := make([][]byte, len(rt.cfg.Users))
	for i, u := range rt.cfg.Users {
		shortIDs[i] = u.ShortIDBytes
	}
	_ = tlsConn.SetDeadline(time.Now().Add(20 * time.Second))
	_, err := auth.ReadAndVerify(tlsConn, rt.cfg.PrivKey, shortIDs, rt.cfg.MaxTimeDiff)
	if err != nil {
		rejectHTTP(tlsConn)
		return
	}
	if _, err := tlsConn.Write([]byte{0x00}); err != nil {
		return
	}
	_ = tlsConn.SetDeadline(time.Time{})
	log.Printf("miraged: legacy session established")
	_ = rt.serveEstablished(tlsConn, rt.cfg.ServerPaddingSeedBytes[:], true)
}

func (rt *runtimeState) serveEstablished(conn net.Conn, seed []byte, closeUnderlying bool) error {
	if closeUnderlying {
		defer conn.Close()
	}

	transport := conn
	if len(seed) == 16 {
		recordConn, err := record.NewConn(conn, seed)
		if err != nil {
			return err
		}
		transport = recordConn
	}

	sess := mux.NewServerSession(transport)
	defer sess.Close()

	for {
		st, err := sess.Accept()
		if err != nil || st == nil {
			return err
		}
		go serveStream(st)
	}
}

func (rt *runtimeState) fallbackOrReject(src net.Conn, prefix []byte) {
	if rt.cfg.Fallback == "" {
		if len(prefix) > 0 {
			rejectHTTP(src)
		}
		return
	}

	dst, err := net.DialTimeout("tcp", rt.cfg.Fallback, 15*time.Second)
	if err != nil {
		log.Printf("miraged: fallback dial %s: %v", rt.cfg.Fallback, err)
		return
	}
	defer dst.Close()

	if len(prefix) > 0 {
		if _, err := dst.Write(prefix); err != nil {
			return
		}
	}

	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(dst, src)
		if cw, ok := dst.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		}
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(src, dst)
		if cw, ok := src.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		}
		done <- struct{}{}
	}()
	<-done
}

func (rt *runtimeState) rememberReplay(key [32]byte) bool {
	rt.replayMu.Lock()
	defer rt.replayMu.Unlock()

	now := time.Now()
	ttl := time.Duration(rt.cfg.ReplayCacheTTL) * time.Second
	if ttl <= 0 {
		ttl = 90 * time.Second
	}
	for k, exp := range rt.replayCache {
		if now.After(exp) {
			delete(rt.replayCache, k)
		}
	}
	if _, exists := rt.replayCache[key]; exists {
		return false
	}
	if capHint := rt.cfg.ReplayCacheCap; capHint > 0 && len(rt.replayCache) >= capHint {
		for k := range rt.replayCache {
			delete(rt.replayCache, k)
			break
		}
	}
	rt.replayCache[key] = now.Add(ttl)
	return true
}

func hmacTokenEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}
