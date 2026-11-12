package model

import (
	"github.com/cuikaipeng/panreel/internal/catalog"
	"slices"
	"testing"
)

func TestEngineUpdatePreservesChannelsAndPluginChoices(t *testing.T) {
	old := catalog.Catalog{Plugins: []catalog.Plugin{{ID: "one", Env: []string{"ONE_TOKEN"}}, {ID: "removed"}}}
	next := catalog.Catalog{Plugins: []catalog.Plugin{{ID: "one", Env: []string{"ONE_TOKEN"}}, {ID: "new"}}}
	s := Defaults()
	s.Plugins = []string{"one", "removed"}
	s.Channels = []string{"kept_channel"}
	s.CustomChannels = []string{"kept_channel", "disabled_channel"}
	s.PluginEnv = map[string]string{"ONE_TOKEN": "kept", "OLD_TOKEN": "old"}
	got := ReconcileEngine(s, old, next)
	if !slices.Equal(got.Plugins, []string{"one", "new"}) || !slices.Equal(got.Channels, s.Channels) || !slices.Equal(got.CustomChannels, s.CustomChannels) || got.PluginEnv["ONE_TOKEN"] != "kept" || len(got.PluginEnv) != 1 {
		t.Fatal("settings changed incorrectly", got)
	}
	if len(s.PluginEnv) != 2 {
		t.Fatal("original settings mutated")
	}
	s.Plugins = []string{"one"}
	got = ReconcileEngine(s, old, next)
	if !slices.Equal(got.Plugins, s.Plugins) {
		t.Fatal("explicit selection changed")
	}
	s.Plugins = nil
	got = ReconcileEngine(s, old, next)
	if len(got.Plugins) != 0 {
		t.Fatal("disabled plugins re-enabled")
	}
}
