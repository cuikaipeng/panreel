package network

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func bridgeClient(t *testing.T, b *Bridge) *http.Client {
	t.Helper()
	u, err := url.Parse(b.URL())
	if err != nil {
		t.Fatal(err)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyURL(u)
	t.Cleanup(transport.CloseIdleConnections)
	return &http.Client{Transport: transport, Timeout: 3 * time.Second}
}

func getBody(t *testing.T, c *http.Client, address string) string {
	t.Helper()
	r, err := c.Get(address)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	body, err := io.ReadAll(r.Body)
	if err != nil || r.StatusCode != http.StatusOK {
		t.Fatalf("GET failed: %d %v", r.StatusCode, err)
	}
	return string(body)
}

func TestBridgeHTTPRoutingAuthenticationAndChanges(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Proxy-Authorization") != "" {
			t.Error("bridge credentials reached origin")
		}
		_, _ = io.WriteString(w, "direct")
	}))
	defer origin.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := "Basic " + base64.StdEncoding.EncodeToString([]byte("upstream:secret"))
		if r.Header.Get("Proxy-Authorization") != want || !r.URL.IsAbs() {
			t.Error("upstream proxy request lost its target or used the bridge credential")
		}
		_, _ = io.WriteString(w, "proxied")
	}))
	defer upstream.Close()
	p, _ := url.Parse(upstream.URL)
	p.User = url.UserPassword("upstream", "secret")
	var useProxy atomic.Bool
	b, err := startBridge(func(r *http.Request) (*url.URL, error) {
		if r.URL.Scheme != "http" || r.URL.Path != "/search" {
			t.Error("resolver did not receive the destination URL")
		}
		if r.URL.Hostname() == "proxy-only.invalid" || useProxy.Load() {
			return p, nil
		}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	c := bridgeClient(t, b)
	if getBody(t, c, origin.URL+"/search") != "direct" || getBody(t, c, "http://proxy-only.invalid/search") != "proxied" {
		t.Fatal("per-target proxy/bypass rules not applied")
	}
	useProxy.Store(true)
	if getBody(t, c, origin.URL+"/search") != "proxied" {
		t.Fatal("proxy change required restarting the bridge")
	}
	useProxy.Store(false)
	if getBody(t, c, origin.URL+"/search") != "direct" {
		t.Fatal("switching back to direct retained the old proxy")
	}
	unauthenticated := bridgeClient(t, b)
	publicURL, _ := url.Parse(b.URL())
	publicURL.User = nil
	unauthenticated.Transport.(*http.Transport).Proxy = http.ProxyURL(publicURL)
	response, err := unauthenticated.Get(origin.URL + "/search")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusProxyAuthRequired {
		t.Fatal("unauthenticated relay allowed")
	}
	if _, err := unauthenticated.Get("https://target.invalid"); err == nil {
		t.Fatal("unauthenticated tunnel allowed")
	}
	b.Close()
	if _, err := c.Get(origin.URL + "/search"); err == nil {
		t.Fatal("closed bridge still accepts requests")
	}
}

func TestBridgeHTTPSDirectAndUpstreamProxy(t *testing.T) {
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Proxy-Authorization") != "" {
			t.Error("credentials reached TLS origin")
		}
		_, _ = io.WriteString(w, "encrypted response")
	}))
	defer origin.Close()
	var connects atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect || r.Header.Get("Proxy-Authorization") != "Basic dXNlcjpwYXNz" {
			http.Error(w, "bad CONNECT or auth", http.StatusProxyAuthRequired)
			return
		}
		connects.Add(1)
		remote, err := net.DialTimeout("tcp", r.Host, time.Second)
		if err != nil {
			t.Error(err)
			return
		}
		defer remote.Close()
		local, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer local.Close()
		_, _ = rw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		_ = rw.Flush()
		finished := make(chan struct{})
		go func() { _, _ = io.Copy(remote, rw); _ = remote.Close(); close(finished) }()
		_, _ = io.Copy(local, remote)
		_ = local.Close()
		<-finished
	}))
	defer upstream.Close()
	p, _ := url.Parse(upstream.URL)
	p.User = url.UserPassword("user", "pass")
	for _, route := range []struct {
		name  string
		proxy *url.URL
	}{{"direct", nil}, {"http-proxy", p}} {
		t.Run(route.name, func(t *testing.T) {
			b, err := startBridge(func(r *http.Request) (*url.URL, error) {
				if r.URL.Scheme != "https" || r.URL.Host != origin.Listener.Addr().String() {
					t.Error("CONNECT lookup used the wrong scheme or destination")
				}
				return route.proxy, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			defer b.Close()
			c := bridgeClient(t, b)
			c.Transport.(*http.Transport).TLSClientConfig = origin.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
			if getBody(t, c, origin.URL) != "encrypted response" {
				t.Fatal("HTTPS response changed")
			}
			b.Close()
			if _, err := c.Get(origin.URL); err == nil {
				t.Fatal("hijacked tunnel survived bridge shutdown")
			}
		})
	}
	if connects.Load() != 1 {
		t.Fatal("HTTPS did not pass through the upstream proxy")
	}
}

func TestTunnelHandshakeCancellation(t *testing.T) {
	accepted := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(accepted)
		<-r.Context().Done()
	}))
	defer server.Close()
	p, _ := url.Parse(server.URL)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := dialTunnel(ctx, "target.invalid:443", p); result <- err }()
	select {
	case <-accepted:
	case <-time.After(2 * time.Second):
		t.Fatal("proxy handshake did not start")
	}
	cancel()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("cancelled tunnel succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not interrupt proxy handshake")
	}
}

func TestTunnelPreservesBufferedBytesAndRejectsUntrustedTLS(t *testing.T) {
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		_, _ = io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\nhello")
	}))
	defer proxyServer.Close()
	p, _ := url.Parse(proxyServer.URL)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, err := dialTunnel(ctx, "target.invalid:443", p)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	data := make([]byte, 5)
	if _, err = io.ReadFull(conn, data); err != nil || string(data) != "hello" {
		t.Fatal("CONNECT lost buffered tunnel data", err)
	}
	tlsProxy := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("untrusted TLS proxy accepted") }))
	defer tlsProxy.Close()
	p, _ = url.Parse(tlsProxy.URL)
	if _, err := dialTunnel(ctx, "target.invalid:443", p); err == nil {
		t.Fatal("TLS verification was bypassed")
	}
}

func TestTunnelSOCKS5(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	finished := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			finished <- err
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		r := bufio.NewReader(conn)
		greeting := make([]byte, 3)
		if _, err = io.ReadFull(r, greeting); err != nil || string(greeting) != "\x05\x01\x00" {
			finished <- fmt.Errorf("unexpected SOCKS5 greeting: %v", err)
			return
		}
		_, _ = conn.Write([]byte{5, 0})
		header := make([]byte, 5)
		if _, err = io.ReadFull(r, header); err != nil || string(header[:4]) != "\x05\x01\x00\x03" {
			finished <- fmt.Errorf("unexpected SOCKS5 CONNECT: %v", err)
			return
		}
		hostAndPort := make([]byte, int(header[4])+2)
		_, err = io.ReadFull(r, hostAndPort)
		if err != nil || string(hostAndPort) != "target.invalid\x01\xbb" {
			finished <- fmt.Errorf("SOCKS5 did not preserve remote DNS target: %v", err)
			return
		}
		_, err = conn.Write(append([]byte{5, 0, 0, 1, 127, 0, 0, 1, 1, 187}, []byte("hello")...))
		finished <- err
	}()
	p, _ := url.Parse("socks5://" + listener.Addr().String())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := dialTunnel(ctx, "target.invalid:443", p)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	data, err := io.ReadAll(conn)
	if err != nil || strings.TrimSpace(string(data)) != "hello" {
		t.Fatal("SOCKS5 tunnel failed", err)
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
}
