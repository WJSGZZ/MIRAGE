// Package server implements the MIRAGE server.
package server

import (
	"crypto/tls"
	"io"
	"log"
	"net"
	"time"

	"miraged/internal/config"
	"miraged/internal/mux"
)

// Run starts the server and blocks until it returns an error.
func Run(cfg *config.ServerConfig, tlsCfg *tls.Config) error {
	rt := newRuntime(cfg, tlsCfg)
	return rt.run()
}

// relay copies bidirectionally between a and b until either side closes.
func relay(a, b io.ReadWriter) {
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(b, a)
		if tc, ok := b.(interface{ CloseWrite() error }); ok {
			_ = tc.CloseWrite()
		}
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(a, b)
		done <- struct{}{}
	}()
	<-done
}

func serveStream(st *mux.Stream) {
	defer st.Close()

	dst, err := net.DialTimeout("tcp", st.Dest(), 15*time.Second)
	if err != nil {
		log.Printf("miraged: dial %s: %v", st.Dest(), err)
		return
	}
	defer dst.Close()

	relay(st, dst)
}

// rejectHTTP sends a minimal HTTP 400 response to make the server look like
// a normal web server that rejects bad requests.
func rejectHTTP(w io.Writer) {
	const resp = "HTTP/1.1 400 Bad Request\r\n" +
		"Content-Type: text/plain\r\n" +
		"Content-Length: 11\r\n" +
		"Connection: close\r\n\r\n" +
		"Bad Request"
	_, _ = w.Write([]byte(resp))
}
