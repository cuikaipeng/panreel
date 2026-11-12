package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/cuikaipeng/panreel/internal/catalog"
	"github.com/cuikaipeng/panreel/internal/model"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAllLinkTypesAndUnsafeLinks(t *testing.T) {
	cases := map[string]string{"baidu": "https://pan.baidu.com/s/abc?pwd=1234", "aliyun": "https://www.alipan.com/s/abc", "quark": "https://pan.quark.cn/s/abc", "guangya": "https://www.guangyapan.com/s/abc", "tianyi": "https://cloud.189.cn/t/abc", "uc": "https://drive.uc.cn/s/abc", "mobile": "https://caiyun.139.com/m/i?x=abc", "115": "https://115cdn.com/s/abc", "pikpak": "https://mypikpak.com/s/abc", "xunlei": "https://pan.xunlei.com/s/abc", "123": "https://www.123865.com/s/abc", "magnet": "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567", "ed2k": "ed2k://|file|test.mp4|123|0123456789abcdef0123456789abcdef|/", "others": "https://example.org/share/abc"}
	for drive, link := range cases {
		t.Run(drive, func(t *testing.T) {
			got, id, _, err := NormalizeLink(link, drive)
			if err != nil || got == "" || id == "" {
				t.Fatal(got, id, err)
			}
		})
	}
	for _, link := range []string{"javascript:alert(1)", "file:///tmp/test", "https://pan.quark.cn.evil.test/s/abc", "https://user@pan.quark.cn/s/abc", "https://pan.quark.cn:8888/s/abc", "https://pan.quark.cn/", "https://pan.quark.cn/s/abc\nX:bad"} {
		if _, _, _, err := NormalizeLink(link, "quark"); err == nil {
			t.Error("unsafe link accepted", link)
		}
	}
	for _, link := range []string{"https://127.0.0.1/test", "http://localhost/test", "http://host.local/test"} {
		if _, _, _, err := NormalizeLink(link, "others"); err == nil {
			t.Error("local link accepted", link)
		}
	}
}
func TestNormalizeDedupeMetadataAndSchema(t *testing.T) {
	raw := `{"code":0,"data":{"merged_by_type":{"aliyun":[{"url":"https://www.alipan.com/s/abc?utm_source=test","note":"<b>测试影片</b> 2024 2160p 中英字幕 3.5GB","source":"plugin:test"},{"url":"https://www.aliyundrive.com/s/abc?pwd=1234","source":"tg:example"}],"quark":[{"url":"https://pan.quark.cn/s/xyz","work_title":"编号 10720","datetime":"0001-01-01T00:00:00Z"}]}}}`
	items, err := Normalize([]byte(raw))
	if err != nil || len(items) != 2 {
		t.Fatal(items, err)
	}
	item := items[0]
	if len(item.Sources) != 2 || item.Password != "1234" || item.Quality != "4K" || item.Year != "2024" || item.Size != "3.5GB" || item.Subtitle != "中英字幕" || strings.Contains(item.Title, "<") {
		t.Fatal(item)
	}
	if items[1].Quality != "" || !items[1].PublishedAt.IsZero() {
		t.Fatal(items[1])
	}
	for _, raw := range []string{`{}`, `null`, `{"code":500,"data":{"total":0}}`, `<html>`, `{"data":null}`} {
		if _, err := Normalize([]byte(raw)); err == nil {
			t.Error("accepted invalid response", raw)
		}
	}
	for _, raw := range []string{`{"total":0}`, `{"code":200,"data":{"merged_by_type":{}}}`} {
		got, err := Normalize([]byte(raw))
		if err != nil || len(got) != 0 {
			t.Fatal(got, err)
		}
	}
}
func TestSearchRequestAndCancellation(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("token missing")
		}
		if r.URL.Path == "/api/panreel/search/snapshot" {
			fmt.Fprint(w, `{"data":{"pending":false,"total":0}}`)
			return
		}
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if string(body["src"]) != `"all"` || string(body["channels"]) != `["channel_one","channel_two"]` || string(body["refresh"]) != "true" || len(body["plugins"]) < 100 || string(body["ext"]) != `{"test":true}` || len(body["filter"]) == 0 {
			t.Error("incorrect request", body)
		}
		var types []string
		if json.Unmarshal(body["cloud_types"], &types) != nil || len(types) != len(catalog.Drives) {
			t.Error("default search must select all cloud types")
		}
		fmt.Fprint(w, `{"data":{"total":0}}`)
	}))
	defer server.Close()
	s := model.Defaults()
	s.Channels = []string{"channel_one", "channel_two"}
	s.Extensions = []byte(`{"test":true}`)
	s.Include = "4K 中字"
	c := NewClient(server.URL, "secret")
	if err := c.SearchUpdates(context.Background(), "测试", s, true, func([]model.Item) {}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.SearchUpdates(ctx, "测试", s, true, func([]model.Item) {}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal("canceled request was sent")
	}
	s.Channels = nil
	if err := c.SearchUpdates(context.Background(), "测试", s, true, func([]model.Item) {}); err == nil {
		t.Fatal("empty selection sent")
	}
}
func TestNoSearchRequestBeforeChannelSetup(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, `{"data":{"total":0}}`)
	}))
	defer server.Close()
	c := NewClient(server.URL, "secret")
	for _, source := range []string{"all", "plugin", "tg"} {
		s := model.Defaults() // Every plugin is enabled on a fresh install.
		s.Source = source
		s.CustomChannels = []string{"disabled_channel"}
		for _, refresh := range []bool{false, true} {
			if err := c.SearchUpdates(context.Background(), "test", s, refresh, func([]model.Item) {}); err == nil {
				t.Errorf("%s searched without an enabled channel", source)
			}
		}
	}
	if calls.Load() != 0 {
		t.Fatal("an unconfigured search reached the engine")
	}
}
func TestHealthAndCheckStates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			fmt.Fprint(w, `{"status":"ok","plugins":["test"],"channels":[],"tg":{"reachable":false,"checked_seconds_ago":null}}`)
		case "/api/check/links":
			fmt.Fprint(w, `{"results":[{"state":"uncertain","summary":"网络超时"}]}`)
		default:
			t.Error(r.URL.Path)
		}
	}))
	defer server.Close()
	c := NewClient(server.URL, "secret")
	h, err := c.Health(context.Background())
	if err != nil || h.TG.Age != nil {
		t.Fatal(h, err)
	}
	state, err := c.Check(context.Background(), model.Item{Drive: "quark", URL: "https://pan.quark.cn/s/example"})
	if err != nil || state.State != "uncertain" || time.Since(state.At) > time.Second {
		t.Fatal(state, err)
	}
	state, err = c.Check(context.Background(), model.Item{Drive: "magnet"})
	if err != nil || state.State != "unsupported" {
		t.Fatal(state, err)
	}
}
