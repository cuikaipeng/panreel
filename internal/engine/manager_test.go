package engine

import (
	"context"
	"encoding/json"
	"github.com/cuikaipeng/panreel/internal/catalog"
	"github.com/cuikaipeng/panreel/internal/model"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestBundledEngineProgress(t *testing.T) {
	if os.Getenv("PANREEL_ENGINE_TEST") != "1" {
		t.Skip("opt-in packaged engine integration")
	}
	cache, err := filepath.Abs("../../.cache/pansou")
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := filepath.Abs("../../buildtools/pansou.mod")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "-modfile="+manifest, "-mod=readonly", "-count=1", "-timeout=30s", "./service", "-run", "^TestPanReelProgress")
	cmd.Dir = filepath.Join(cache, "source")
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=go1.27.4", "GOCACHE="+filepath.Join(cache, "go-build"), "GOMODCACHE="+filepath.Join(cache, "go-mod"))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("engine progress integration failed: %v\n%s", err, output)
	}
}

// Run with PANREEL_ENGINE_TEST=1 after go run ./cmd/prepare.
func TestBundledEngineLifecycle(t *testing.T) {
	if os.Getenv("PANREEL_ENGINE_TEST") != "1" {
		t.Skip("opt-in packaged engine integration")
	}
	resources, err := filepath.Abs("../../resources")
	if err != nil {
		t.Fatal(err)
	}
	m := &Manager{Resources: resources, DataDir: t.TempDir()}
	defer m.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	s := model.Defaults()
	if len(s.Channels) != 0 || len(s.Plugins) != len(catalog.PluginIDs()) {
		t.Fatal("unexpected fresh-install configuration")
	}
	startup, finishStartup := context.WithCancel(ctx)
	if err = m.Start(startup, s); err != nil {
		finishStartup()
		t.Fatal(err)
	}
	finishStartup()
	// Start's context may end after an update commits. The relay must live
	// until the helper exits, rather than until the startup operation ends.
	var proxyURL *url.URL
	for _, value := range m.cmd.Env {
		if raw, ok := strings.CutPrefix(value, "PROXY="); ok {
			proxyURL, err = url.Parse(raw)
		}
	}
	if err != nil || proxyURL == nil || proxyURL.Hostname() != "127.0.0.1" || proxyURL.User == nil {
		t.Fatal("system proxy relay was not passed to PanSou")
	}
	proxyTransport := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	defer proxyTransport.CloseIdleConnections()
	proxyClient := &http.Client{Transport: proxyTransport, Timeout: 2 * time.Second}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer origin.Close()
	response, err := proxyClient.Get(origin.URL)
	if err != nil {
		t.Fatal("relay did not survive startup context", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatal("relay did not authenticate or forward requests")
	}
	c := m.Client()
	h, err := c.Health(ctx)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(h.Plugins)
	want := catalog.PluginIDs()
	slices.Sort(want)
	if !slices.Equal(h.Plugins, want) || !slices.Equal(h.Channels, s.Channels) {
		t.Fatalf("runtime catalog differs: plugins %d/%d, channels %d/%d", len(h.Plugins), len(want), len(h.Channels), len(s.Channels))
	}
	for _, path := range []string{"/api/health", "/api/panreel/search/snapshot"} {
		response, err := c.HTTP.Get(c.Base + path)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusUnauthorized {
			t.Fatalf("unauthenticated HTTP allowed on %s: %d", path, response.StatusCode)
		}
	}
	// A cache-only snapshot must work before any search, without making network
	// requests or treating the fresh install's empty channel list as defaults.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Base+"/api/panreel/search/snapshot", strings.NewReader(`{"kw":"PanReel progress probe","src":"tg"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	response, err = c.HTTP.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot struct {
		Data struct {
			Total   int
			Pending *bool
		}
	}
	decodeErr := json.NewDecoder(response.Body).Decode(&snapshot)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || decodeErr != nil || snapshot.Data.Pending == nil || *snapshot.Data.Pending || snapshot.Data.Total != 0 {
		t.Fatalf("invalid cache snapshot: %+v, %v", snapshot, decodeErr)
	}
	// Closing the parent-owned pipe must gracefully stop and reap the helper.
	m.stdin.Close()
	select {
	case <-m.done:
	case <-time.After(15 * time.Second):
		t.Fatal("engine survived parent pipe EOF")
	}
	m.Stop()
	if m.Client() != nil {
		t.Fatal("engine state not cleared")
	}
	if _, err = c.Health(ctx); err == nil {
		t.Fatal("engine port remains open after quit")
	}
	if _, err = proxyClient.Get(origin.URL); err == nil {
		t.Fatal("system proxy relay survived child process exit")
	}
	logs, err := os.ReadFile(filepath.Join(m.DataDir, "logs", "engine.log"))
	password, _ := proxyURL.User.Password()
	if err != nil || strings.Contains(string(logs), password) {
		t.Fatal("relay credential leaked to engine logs", err)
	}
	s.Plugins = s.Plugins[:1]
	s.Channels = []string{"channel_one", "channel_two"}
	s.ProxyMode = model.ProxyDirect
	s.Proxy = "http://ignored:secret@127.0.0.1:1"
	if err = m.Start(ctx, s); err != nil {
		t.Fatal(err)
	}
	for _, value := range m.cmd.Env {
		key, val, _ := strings.Cut(value, "=")
		if slices.Contains([]string{"PROXY", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY"}, strings.ToUpper(key)) && val != "" {
			t.Fatal("direct mode leaked a proxy into the child environment")
		}
	}
	h, err = m.Client().Health(ctx)
	if err != nil || !slices.Equal(h.Channels, s.Channels) || len(h.Plugins) != 1 {
		t.Fatalf("configured channels were not applied: %+v %v", h, err)
	}
	m.Stop()
	s.Channels = nil
	if err = m.Start(ctx, s); err != nil {
		t.Fatal(err)
	}
	h, err = m.Client().Health(ctx)
	if err != nil || len(h.Channels) != 0 || len(h.Plugins) != 1 {
		t.Fatalf("empty channels re-enabled upstream defaults: %+v %v", h, err)
	}
}
