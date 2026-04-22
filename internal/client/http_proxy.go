package client

import (
	"bufio"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// ServeHTTPProxy serves an HTTP proxy listener backed by the MIRAGE client.
func ServeHTTPProxy(ln net.Listener, c *Client, meter TrafficMeter) {
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handleHTTPProxy(w, r, c, meter)
		}),
	}
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		log.Printf("miragec: http proxy serve: %v", err)
	}
}

func handleHTTPProxy(w http.ResponseWriter, r *http.Request, c *Client, meter TrafficMeter) {
	if strings.EqualFold(r.Method, http.MethodConnect) {
		handleHTTPConnect(w, r, c, meter)
		return
	}
	handleHTTPForward(w, r, c, meter)
}

func handleHTTPConnect(w http.ResponseWriter, r *http.Request, c *Client, meter TrafficMeter) {
	dest := canonicalAddr(r.Host, "443")
	st, err := c.Dial(dest)
	if err != nil {
		http.Error(w, "proxy connect failed", http.StatusBadGateway)
		log.Printf("miragec: http connect %s: %v", dest, err)
		return
	}

	hj, ok := w.(http.Hijacker)
	if !ok {
		st.Close()
		http.Error(w, "hijacking unsupported", http.StatusInternalServerError)
		return
	}

	conn, buf, err := hj.Hijack()
	if err != nil {
		st.Close()
		log.Printf("miragec: http hijack %s: %v", dest, err)
		return
	}
	defer conn.Close()

	if _, err := conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		st.Close()
		return
	}
	if buf.Reader.Buffered() > 0 {
		n := buf.Reader.Buffered()
		pending, err := buf.Reader.Peek(n)
		if err != nil {
			st.Close()
			return
		}
		if _, err := st.Write(pending); err != nil {
			st.Close()
			return
		}
		if _, err := buf.Reader.Discard(n); err != nil {
			st.Close()
			return
		}
	}

	Relay(conn, st, meter)
}

func handleHTTPForward(w http.ResponseWriter, r *http.Request, c *Client, meter TrafficMeter) {
	dest := requestDest(r)
	st, err := c.Dial(dest)
	if err != nil {
		http.Error(w, "proxy connect failed", http.StatusBadGateway)
		log.Printf("miragec: http forward %s: %v", dest, err)
		return
	}
	defer st.Close()
	upstream := &meteredConn{Conn: st, meter: meter}

	outReq := new(http.Request)
	*outReq = *r
	outReq.URL = cloneURL(r.URL)
	outReq.RequestURI = ""
	outReq.Close = true
	outReq.Header = r.Header.Clone()
	outReq.Header.Del("Proxy-Connection")
	outReq.Header.Del("Proxy-Authenticate")
	outReq.Header.Del("Proxy-Authorization")
	outReq.Header.Del("Connection")

	if outReq.URL != nil {
		outReq.URL.Scheme = ""
		outReq.URL.Host = ""
	}

	if err := outReq.Write(upstream); err != nil {
		http.Error(w, "proxy write failed", http.StatusBadGateway)
		log.Printf("miragec: http write %s: %v", dest, err)
		return
	}

	resp, err := http.ReadResponse(bufio.NewReader(upstream), r)
	if err != nil {
		http.Error(w, "proxy read failed", http.StatusBadGateway)
		log.Printf("miragec: http read %s: %v", dest, err)
		return
	}
	defer resp.Body.Close()

	copyHeader(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		log.Printf("miragec: http body %s: %v", dest, err)
	}
}

type meteredConn struct {
	net.Conn
	meter TrafficMeter
}

func (m *meteredConn) Read(p []byte) (int, error) {
	n, err := m.Conn.Read(p)
	if m.meter != nil {
		m.meter.AddDownload(int64(n))
	}
	return n, err
}

func (m *meteredConn) Write(p []byte) (int, error) {
	n, err := m.Conn.Write(p)
	if m.meter != nil {
		m.meter.AddUpload(int64(n))
	}
	return n, err
}

func requestDest(r *http.Request) string {
	if r.URL != nil && r.URL.Host != "" {
		if r.URL.Scheme == "https" {
			return canonicalAddr(r.URL.Host, "443")
		}
		return canonicalAddr(r.URL.Host, "80")
	}
	return canonicalAddr(r.Host, "80")
}

func canonicalAddr(hostport, defaultPort string) string {
	if _, _, err := net.SplitHostPort(hostport); err == nil {
		return hostport
	}
	if strings.HasPrefix(hostport, "[") && strings.Contains(hostport, "]:") {
		return hostport
	}
	return net.JoinHostPort(hostport, defaultPort)
}

func cloneURL(src *url.URL) *url.URL {
	if src == nil {
		return nil
	}
	dst := *src
	return &dst
}

func copyHeader(dst, src http.Header) {
	for k, vals := range src {
		for _, v := range vals {
			dst.Add(k, v)
		}
	}
}
