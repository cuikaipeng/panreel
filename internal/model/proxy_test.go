package model

import (
	"encoding/json"
	"testing"
)

func TestProxyModeMigrationAndValidation(t *testing.T) {
	for _, tc := range []struct {
		json, mode string
		valid      bool
	}{
		{`{}`, ProxySystem, true},
		{`{"proxy":""}`, ProxySystem, true},
		{`{"proxy":"http://localhost:7890"}`, ProxyManual, true},
		{`{"proxy_mode":"system","proxy":"http://localhost:7890"}`, ProxySystem, true},
		{`{"proxy_mode":"direct","proxy":"http://localhost:7890"}`, ProxyDirect, true},
		{`{"proxy_mode":"manual","proxy":"socks5h://user:password@localhost:1080"}`, ProxyManual, true},
		{`{"proxy_mode":"manual","proxy":""}`, ProxyManual, false},
		{`{"proxy_mode":"manual","proxy":"file:///tmp/proxy"}`, ProxyManual, false},
		{`{"proxy_mode":"invalid"}`, "invalid", false},
	} {
		t.Run(tc.json, func(t *testing.T) {
			s := Defaults()
			if err := json.Unmarshal([]byte(tc.json), &s); err != nil {
				t.Fatal(err)
			}
			if s.EffectiveProxyMode() != tc.mode || (s.Validate() == nil) != tc.valid {
				t.Fatal("incorrect proxy mode or validation", s.EffectiveProxyMode(), s.Validate())
			}
			if s.Clone().EffectiveProxyMode() != tc.mode {
				t.Fatal("proxy mode lost in JSON round trip")
			}
		})
	}
}
