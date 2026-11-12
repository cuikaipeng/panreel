package storage

import (
	"fmt"
	"github.com/cuikaipeng/panreel/internal/model"
	"slices"
	"testing"
)

func TestFreshAndSavedEmptyChannelConfiguration(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	settings, err := s.Settings()
	if err != nil || len(settings.Channels) != 0 || len(settings.CustomChannels) != 0 || len(settings.Plugins) != 75 {
		t.Fatal("unexpected fresh-install sources", err)
	}
	settings.CustomChannels = []string{"disabled_channel"}
	if err = s.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.Settings()
	if err != nil || len(got.Channels) != 0 || len(got.Plugins) != 75 || !slices.Equal(got.CustomChannels, settings.CustomChannels) {
		t.Fatal("reopening re-enabled channels or changed plugins", err)
	}
}

func TestDriveSelectionSurvivesReopening(t *testing.T) {
	for _, drives := range [][]string{{"quark", "baidu"}, {}} {
		dir := t.TempDir()
		store, err := Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		s := model.Defaults()
		s.Drives = drives
		if err = store.SaveSettings(s); err != nil {
			t.Fatal(err)
		}
		store.Close()
		store, err = Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		got, err := store.Settings()
		store.Close()
		if err != nil || !slices.Equal(got.EnabledDrives(), drives) {
			t.Fatal("saved drive selection changed after reopening", err)
		}
	}
}

func TestPreviouslySavedChannelsRemainEditable(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	settings := model.Defaults()
	// Older releases kept built-in selections outside CustomChannels.
	settings.Channels = []string{"Previous_Channel", "custom_channel", "previous_channel"}
	settings.CustomChannels = []string{"custom_channel", "disabled_channel"}
	if err = s.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	got, err := s.Settings()
	if err != nil || !slices.Equal(got.Channels, []string{"previous_channel", "custom_channel"}) || !slices.Equal(got.CustomChannels, []string{"custom_channel", "disabled_channel", "previous_channel"}) {
		t.Fatal("saved enabled/disabled channels were lost", err)
	}
}

func TestPersistenceAndHistoryBound(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	settings := model.Defaults()
	settings.Plugins = settings.Plugins[:2]
	settings.Channels = []string{"example_channel"}
	settings.Theme = "dark"
	if err = s.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	item := model.Item{ID: "one", Title: "测试", Drive: "quark", URL: "https://pan.quark.cn/s/test", Password: "abcd", Sources: []string{"plugin:test"}}
	if err = s.Bookmark(item, true); err != nil {
		t.Fatal(err)
	}
	if err = s.Bookmark(item, true); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 105; i++ {
		if err = s.Record(fmt.Sprintf("query %d", i)); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	loaded, err := s.Settings()
	if err != nil || loaded.Theme != "dark" || len(loaded.Plugins) != 2 || len(loaded.Channels) != 1 {
		t.Fatal(loaded, err)
	}
	saved, err := s.Bookmarks()
	if err != nil || len(saved) != 1 || saved[0].Password != "abcd" {
		t.Fatal(saved, err)
	}
	history, err := s.History()
	if err != nil || len(history) != 100 {
		t.Fatal(len(history), err)
	}
	invalid := loaded.Clone()
	invalid.Timeout = 0
	if s.SaveSettings(invalid) == nil {
		t.Fatal("invalid settings committed")
	}
	loaded, _ = s.Settings()
	if loaded.Timeout == 0 {
		t.Fatal("previous valid preferences overwritten")
	}
	if err = s.ClearHistory(); err != nil {
		t.Fatal(err)
	}
	history, err = s.History()
	if err != nil || len(history) != 0 {
		t.Fatal(history, err)
	}
	if err = s.Bookmark(item, false); err != nil {
		t.Fatal(err)
	}
	saved, _ = s.Bookmarks()
	if len(saved) != 0 {
		t.Fatal(saved)
	}
}
