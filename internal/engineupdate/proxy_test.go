package engineupdate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cuikaipeng/panreel/internal/model"
)

func TestUpdaterProxyModes(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Header().Set("X-Route", "direct") }))
	defer origin.Close()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !r.URL.IsAbs() {
			t.Error("updater did not send a forward proxy request")
		}
		w.Header().Set("X-Route", "proxy")
	}))
	defer proxy.Close()
	for _, tc := range []struct {
		mode string
		want string
	}{{model.ProxySystem, "direct"}, {model.ProxyDirect, "direct"}, {model.ProxyManual, "proxy"}, {"", "proxy"}} {
		t.Run(tc.mode, func(t *testing.T) {
			s := model.Defaults()
			s.Proxy, s.ProxyMode = proxy.URL, tc.mode
			u := New(t.TempDir(), s)
			defer u.HTTP.CloseIdleConnections()
			r, err := u.get(context.Background(), origin.URL)
			if err != nil {
				t.Fatal(err)
			}
			r.Body.Close()
			if r.Header.Get("X-Route") != tc.want {
				t.Fatalf("proxy mode ignored: %s", r.Header.Get("X-Route"))
			}
			tr := u.HTTP.Transport.(*http.Transport)
			if tc.mode == model.ProxySystem && tr.Proxy == nil {
				t.Fatal("automatic mode has no system resolver")
			}
			if tc.mode == model.ProxyDirect && tr.Proxy != nil {
				t.Fatal("direct mode can inherit proxy environment variables")
			}
		})
	}
}
