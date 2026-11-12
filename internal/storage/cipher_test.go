package storage

import (
	"bytes"
	"encoding/json"
	"github.com/cuikaipeng/panreel/internal/model"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLegacyMigrationPreservesDataAndEncryptsSettings(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	settings := model.Defaults()
	settings.Proxy = "http://user:migration-secret@localhost:8080"
	settings.Extensions = json.RawMessage(`{"token":"extension-secret"}`)
	settings.Theme = "dark"
	settings.Channels = append(settings.Channels, "custom_channel")
	raw, _ := json.Marshal(settings)
	if _, err = s.db.Exec("INSERT INTO preferences(id,data) VALUES(1,?)", string(raw)); err != nil {
		t.Fatal(err)
	}
	if err = s.Bookmark(model.Item{ID: "kept", Title: "kept favorite"}, true); err != nil {
		t.Fatal(err)
	}
	if err = s.Record("kept search"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Settings()
	if err != nil || got.Proxy != settings.Proxy || string(got.Extensions) != string(settings.Extensions) {
		t.Fatalf("migration failed: %v", err)
	}
	if got.EffectiveProxyMode() != model.ProxyManual {
		t.Fatal("legacy manual proxy changed during migration")
	}
	if len(got.CustomChannels) != 1 || got.CustomChannels[0] != "custom_channel" {
		t.Fatal("custom channel lost")
	}
	prefs, err := s.Preferences(got.Theme)
	if err != nil || prefs.Theme != "dark" || prefs.Language != "zh-Hans" {
		t.Fatal("legacy appearance not preserved")
	}
	for _, name := range []string{"panreel.db", "panreel.db-wal"} {
		data, _ := os.ReadFile(filepath.Join(dir, name))
		for _, secret := range []string{"migration-secret", "extension-secret"} {
			if bytes.Contains(data, []byte(secret)) {
				t.Fatalf("plaintext remains in %s", name)
			}
		}
	}
	info, err := os.Stat(filepath.Join(dir, "secrets.key"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatal("key permissions", info.Mode())
	}
	s.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err = s.Settings()
	if err != nil || got.Proxy != settings.Proxy {
		t.Fatal("encrypted settings did not survive reopening", err)
	}
	saved, _ := s.Bookmarks()
	history, _ := s.History()
	if len(saved) != 1 || len(history) != 1 {
		t.Fatal("migration changed user records")
	}
}

func TestMissingOrCorruptKeyNeverOverwritesConfiguration(t *testing.T) {
	for _, damage := range []string{"missing", "wrong", "short"} {
		t.Run(damage, func(t *testing.T) {
			s, err := Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			settings := model.Defaults()
			settings.Proxy = "http://private:secret@localhost:7890"
			if err = s.SaveSettings(settings); err != nil {
				t.Fatal(err)
			}
			var before string
			_ = s.db.QueryRow("SELECT data FROM preferences WHERE id=1").Scan(&before)
			key := filepath.Join(s.Dir, "secrets.key")
			switch damage {
			case "missing":
				err = os.Remove(key)
			case "wrong":
				err = os.WriteFile(key, bytes.Repeat([]byte{1}, 32), 0600)
			case "short":
				err = os.WriteFile(key, []byte{1}, 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.Settings(); err == nil {
				t.Fatal("accepted unreadable configuration")
			}
			if err = s.SaveSettings(model.Defaults()); err == nil {
				t.Fatal("silently reset encrypted settings")
			}
			var after string
			_ = s.db.QueryRow("SELECT data FROM preferences WHERE id=1").Scan(&after)
			if before != after {
				t.Fatal("encrypted data overwritten")
			}
		})
	}
}

func TestPreferencesAreIndependentOfEngineConfiguration(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	settings := model.Defaults()
	if err = s.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	var before string
	_ = s.db.QueryRow("SELECT data FROM preferences WHERE id=1").Scan(&before)
	p := model.DefaultPreferences()
	p.Language = "en"
	p.Theme = "dark"
	p.Accent = "Teal"
	p.CheckEngineUpdates = false
	if err = s.SavePreferences(p); err != nil {
		t.Fatal(err)
	}
	got, err := s.Preferences(settings.Theme)
	if err != nil || got != p {
		t.Fatal("desktop preferences changed", err)
	}
	var after string
	_ = s.db.QueryRow("SELECT data FROM preferences WHERE id=1").Scan(&after)
	if before != after {
		t.Fatal("appearance rewrote engine configuration")
	}
}
