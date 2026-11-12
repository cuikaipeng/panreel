// Package network shares the search engine's network policy with its updater.
package network

import (
	"errors"
	"net/http"
	"net/url"
	"slices"

	"github.com/cuikaipeng/panreel/internal/model"
	"github.com/egoist/mygo/netproxy"
)

func Transport(settings model.Settings) *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	switch settings.EffectiveProxyMode() {
	case model.ProxySystem:
		t.Proxy = netproxy.Proxy
	case model.ProxyDirect:
		t.Proxy = nil
	case model.ProxyManual:
		u, err := url.Parse(settings.Proxy)
		if err == nil && u.Hostname() != "" && slices.Contains([]string{"http", "https", "socks5", "socks5h"}, u.Scheme) {
			t.Proxy = http.ProxyURL(u)
			return t
		}
		fallthrough
	default:
		// Invalid settings must not quietly bypass an intended proxy.
		t.Proxy = func(*http.Request) (*url.URL, error) { return nil, errors.New("invalid proxy settings") }
	}
	return t
}
