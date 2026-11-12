package model

import (
	"fmt"
	"github.com/cuikaipeng/panreel/internal/catalog"
	"slices"
	"strings"
	"testing"
)

func TestCatalogDefaultsAndSourceSelection(t *testing.T) {
	s := Defaults()
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(s.Plugins) != len(catalog.PluginIDs()) || len(s.Channels) != 0 || len(s.CustomChannels) != 0 {
		t.Fatalf("unexpected fresh-install sources: %d/%d", len(s.Plugins), len(s.Channels))
	}
	for _, source := range []string{"all", "plugin", "tg"} {
		s.Source = source
		if _, err := s.EffectiveSource(); err == nil {
			t.Fatalf("%s bypassed required TG configuration", source)
		}
	}
	s.Channels = []string{"example_channel"}
	s.Source = "all"
	if src, err := s.EffectiveSource(); err != nil || src != "all" {
		t.Fatal(src, err)
	}
	s.Source = "plugin"
	if src, err := s.EffectiveSource(); err != nil || src != "plugin" {
		t.Fatal(src, err)
	}
	s.Source = "tg"
	if src, err := s.EffectiveSource(); err != nil || src != "tg" {
		t.Fatal(src, err)
	}
	s.Source = "plugin"
	s.Plugins = nil
	if _, err := s.EffectiveSource(); err == nil {
		t.Fatal("empty plugin selection accepted")
	}
	s.Source = "all"
	if src, err := s.EffectiveSource(); err != nil || src != "tg" {
		t.Fatal(src, err)
	}
}
func TestParseChannels(t *testing.T) {
	got, err := ParseChannels("@Example_Channel, https://t.me/s/second_channel，t.me/third_channel\nhttps://t.me/example_channel\t fourth_channel；fifth_channel; sixth_channel\r\n　")
	if err != nil || !slices.Equal(got, []string{"example_channel", "second_channel", "third_channel", "fourth_channel", "fifth_channel", "sixth_channel"}) {
		t.Fatal(got, err)
	}
	for _, bad := range []string{"https://evil.test/channel", "https://t.me/+private", "https://t.me/channel/123", "short!"} {
		if partial, err := ParseChannels("valid_channel," + bad + ",another_channel"); err == nil || len(partial) > 0 {
			t.Errorf("invalid batch was partially accepted: %q", bad)
		}
	}
	if got, err := ParseChannels(" ,，\n\t；"); err != nil || len(got) != 0 {
		t.Fatal("blank separators", got, err)
	}
	channels := make([]string, 1001)
	for i := range channels {
		channels[i] = fmt.Sprintf("channel_%d", i)
	}
	if _, err := ParseChannels(strings.Join(channels, ",")); err == nil {
		t.Fatal("channel limit not enforced")
	}
}
func TestSettingsRejectInvalidAndCloneIsIndependent(t *testing.T) {
	s := Defaults()
	c := s.Clone()
	c.Plugins[0] = "unknown"
	c.PluginEnv["PORT"] = "80"
	if s.Plugins[0] == "unknown" || len(s.PluginEnv) != 0 {
		t.Fatal("clone shares storage")
	}
	for _, change := range []func(*Settings){func(s *Settings) { s.Plugins = []string{"unknown"} }, func(s *Settings) { s.Timeout = 0 }, func(s *Settings) { s.Proxy = "file:///tmp/proxy" }, func(s *Settings) { s.Extensions = []byte(`[]`) }, func(s *Settings) { s.PluginEnv["PORT"] = "80" }} {
		candidate := Defaults()
		change(&candidate)
		if candidate.Validate() == nil {
			t.Fatal("invalid settings accepted", candidate)
		}
	}
}
