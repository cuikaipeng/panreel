package network

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"time"

	"github.com/egoist/mygo/netproxy"
	"golang.org/x/net/proxy"
)

// Bridge lets the standalone PanSou process use native per-destination proxy
// rules (including PAC and bypasses) without modifying its binary. It listens
// only on loopback and requires a fresh credential for each engine lifetime.
// HTTPS stays an opaque CONNECT tunnel; it is never decrypted here.
type Bridge struct {
	address   string
	auth      string
	resolve   func(*http.Request) (*url.URL, error)
	transport *http.Transport
	forward   *httputil.ReverseProxy
	server    *http.Server
	cancel    context.CancelFunc
	once      sync.Once
	mu        sync.Mutex
	closed    bool
	conns     map[net.Conn]struct{}
}

func StartBridge() (*Bridge, error) { return startBridge(netproxy.Proxy) }

func startBridge(resolve func(*http.Request) (*url.URL, error)) (*Bridge, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	credential := "panreel:" + hex.EncodeToString(secret)
	u := &url.URL{Scheme: "http", Host: listener.Addr().String(), User: url.UserPassword("panreel", hex.EncodeToString(secret))}
	ctx, cancel := context.WithCancel(context.Background())
	b := &Bridge{address: u.String(), auth: "Basic " + base64.StdEncoding.EncodeToString([]byte(credential)), resolve: resolve,
		cancel: cancel, conns: make(map[net.Conn]struct{})}
	b.transport = http.DefaultTransport.(*http.Transport).Clone()
	b.transport.Proxy = resolve
	b.transport.ResponseHeaderTimeout = 30 * time.Second
	b.forward = &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			// ReverseProxy already removes hop-by-hop and forwarding headers.
			r.Out.Header.Del("Proxy-Authorization")
		},
		Transport: b.transport,
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(w, "proxy connection failed", http.StatusBadGateway)
		},
		ErrorLog: log.New(io.Discard, "", 0),
	}
	b.server = &http.Server{Handler: b, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20,
		BaseContext: func(net.Listener) context.Context { return ctx }, ErrorLog: log.New(io.Discard, "", 0)}
	go func() { _ = b.server.Serve(listener) }()
	return b, nil
}

func (b *Bridge) URL() string { return b.address }

func (b *Bridge) Close() {
	b.once.Do(func() {
		b.cancel()
		b.mu.Lock()
		b.closed = true
		for conn := range b.conns {
			_ = conn.Close()
		}
		b.mu.Unlock()
		_ = b.server.Close()
		b.transport.CloseIdleConnections()
	})
}

func (b *Bridge) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("Proxy-Authorization")), []byte(b.auth)) != 1 {
		w.Header().Set("Proxy-Authenticate", `Basic realm="PanReel"`)
		http.Error(w, "proxy authentication required", http.StatusProxyAuthRequired)
		return
	}
	if r.Method == http.MethodConnect {
		b.tunnel(w, r)
		return
	}
	if !r.URL.IsAbs() || (r.URL.Scheme != "http" && r.URL.Scheme != "https") || r.URL.Hostname() == "" || r.URL.User != nil {
		http.Error(w, "invalid proxy target", http.StatusBadRequest)
		return
	}
	b.forward.ServeHTTP(w, r)
}

func (b *Bridge) tunnel(w http.ResponseWriter, r *http.Request) {
	host, port, err := net.SplitHostPort(r.Host)
	if err != nil || host == "" || port == "" {
		http.Error(w, "invalid tunnel target", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	target := &url.URL{Scheme: "https", Host: r.Host}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		http.Error(w, "invalid tunnel target", http.StatusBadRequest)
		return
	}
	p, err := b.resolve(request)
	if err != nil {
		http.Error(w, "proxy lookup failed", http.StatusBadGateway)
		return
	}
	upstream, err := dialTunnel(ctx, target.Host, p)
	if err != nil {
		http.Error(w, "proxy connection failed", http.StatusBadGateway)
		return
	}
	defer upstream.Close()
	client, buffered, err := w.(http.Hijacker).Hijack()
	if err != nil {
		return
	}
	defer client.Close()
	// Server.Close does not close hijacked connections. Track both sides so
	// quitting/restarting the engine also cancels in-flight tunnels.
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.conns[client], b.conns[upstream] = struct{}{}, struct{}{}
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.conns, client)
		delete(b.conns, upstream)
		b.mu.Unlock()
	}()
	if _, err = buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	if err = buffered.Flush(); err != nil {
		return
	}
	finished := make(chan struct{})
	go func() {
		_, _ = io.Copy(upstream, buffered)
		_ = upstream.Close()
		close(finished)
	}()
	_, _ = io.Copy(client, upstream)
	_ = client.Close()
	<-finished
}

func dialTunnel(ctx context.Context, target string, p *url.URL) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	if p == nil {
		return dialer.DialContext(ctx, "tcp", target)
	}
	if p.Scheme == "socks5" || p.Scheme == "socks5h" {
		d, err := proxy.FromURL(p, dialer)
		if err != nil {
			return nil, err
		}
		return d.(proxy.ContextDialer).DialContext(ctx, "tcp", target)
	}
	if p.Scheme != "http" && p.Scheme != "https" {
		return nil, errors.New("unsupported proxy protocol")
	}
	address := p.Host
	if p.Port() == "" {
		port := "80"
		if p.Scheme == "https" {
			port = "443"
		}
		address = net.JoinHostPort(p.Hostname(), port)
	}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	// Cancel stalled proxy/TLS handshakes even before their deadline.
	rawConn := conn
	stop := context.AfterFunc(ctx, func() { _ = rawConn.Close() })
	success := false
	defer func() {
		stop()
		if !success {
			_ = conn.Close()
		}
	}()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	if p.Scheme == "https" {
		tlsConn := tls.Client(conn, &tls.Config{ServerName: p.Hostname(), MinVersion: tls.VersionTLS12})
		if err = tlsConn.HandshakeContext(ctx); err != nil {
			return nil, err
		}
		conn = tlsConn
	}
	req := &http.Request{Method: http.MethodConnect, URL: &url.URL{Opaque: target}, Host: target, Header: make(http.Header)}
	if p.User != nil {
		password, _ := p.User.Password()
		req.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(p.User.Username()+":"+password)))
	}
	if err = req.Write(conn); err != nil {
		return nil, err
	}
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, req)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		return nil, errors.New("upstream proxy refused tunnel")
	}
	// A successful CONNECT body is the tunnel; closing it would close conn.
	if !stop() || ctx.Err() != nil {
		return nil, ctx.Err()
	}
	_ = conn.SetDeadline(time.Time{})
	success = true
	return &bufferedConn{Conn: conn, reader: reader}, nil
}

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) { return c.reader.Read(p) }
