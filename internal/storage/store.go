package storage

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/cuikaipeng/panreel/internal/model"
	_ "modernc.org/sqlite"
)

type Store struct {
	settingsMu sync.Mutex
	db         *sql.DB
	Dir        string
}

func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "panreel.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`PRAGMA busy_timeout=5000; PRAGMA journal_mode=WAL; PRAGMA secure_delete=ON;
CREATE TABLE IF NOT EXISTS preferences (id INTEGER PRIMARY KEY CHECK(id=1), data TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS desktop_preferences (id INTEGER PRIMARY KEY CHECK(id=1), data TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS bookmarks (id TEXT PRIMARY KEY, data TEXT NOT NULL, created_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS bookmark_watches (bookmark_id TEXT PRIMARY KEY, enabled INTEGER NOT NULL, interval_minutes INTEGER NOT NULL, generation INTEGER NOT NULL, read_at INTEGER NOT NULL, state TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS history (query TEXT PRIMARY KEY, searched_at INTEGER NOT NULL);
PRAGMA user_version=3;`)
	if err != nil {
		db.Close()
		return nil, err
	}
	_ = os.Chmod(path, 0600)
	return &Store{db: db, Dir: dir}, nil
}
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) Settings() (model.Settings, error) {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	var data string
	err := s.db.QueryRow("SELECT data FROM preferences WHERE id=1").Scan(&data)
	if err == sql.ErrNoRows {
		return model.Defaults(), nil
	}
	if err != nil {
		return model.Settings{}, err
	}
	settings := model.Defaults()
	plain, legacy, err := s.openSettings([]byte(data))
	if err != nil {
		return settings, err
	}
	if err = json.Unmarshal(plain, &settings); err != nil {
		return settings, err
	}
	// Preserve saved selections from older versions, including formerly
	// bundled channels. All channels now belong to the user's editable list.
	if settings.Channels, err = model.ParseChannels(strings.Join(settings.Channels, "\n")); err != nil {
		return settings, err
	}
	if settings.CustomChannels, err = model.ParseChannels(strings.Join(settings.CustomChannels, "\n")); err != nil {
		return settings, err
	}
	for _, id := range settings.Channels {
		if !slices.Contains(settings.CustomChannels, id) {
			settings.CustomChannels = append(settings.CustomChannels, id)
		}
	}
	if legacy {
		if err = s.saveSettings(settings, true); err != nil {
			return settings, err
		}
		// The original row is securely overwritten; retire WAL copies after
		// committing the migration. Existing user backups remain untouched.
		if _, err = s.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
			return settings, err
		}
	}
	return settings, nil
}
func (s *Store) SaveSettings(settings model.Settings) error {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	var existing string
	err := s.db.QueryRow("SELECT data FROM preferences WHERE id=1").Scan(&existing)
	create := err == sql.ErrNoRows
	if err != nil && !create {
		return err
	}
	if !create {
		_, legacy, err := s.openSettings([]byte(existing))
		if err != nil {
			return err
		}
		create = legacy
	}
	return s.saveSettings(settings, create)
}
func (s *Store) saveSettings(settings model.Settings, create bool) error {
	if err := settings.Validate(); err != nil {
		return err
	}
	data, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	data, err = s.sealSettings(data, create)
	if err != nil {
		return err
	}
	_, err = s.db.Exec("INSERT INTO preferences(id,data) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data", string(data))
	return err
}
func (s *Store) Bookmarks() ([]model.Item, error) {
	rows, err := s.db.Query("SELECT data FROM bookmarks ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []model.Item{}
	for rows.Next() {
		var data string
		if err = rows.Scan(&data); err != nil {
			return nil, err
		}
		var item model.Item
		if err = json.Unmarshal([]byte(data), &item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
func (s *Store) Bookmark(item model.Item, on bool) error {
	if !on {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if _, err = tx.Exec("DELETE FROM bookmark_watches WHERE bookmark_id=?", item.ID); err != nil {
			return err
		}
		if _, err = tx.Exec("DELETE FROM bookmarks WHERE id=?", item.ID); err != nil {
			return err
		}
		return tx.Commit()
	}
	data, err := json.Marshal(item)
	if err != nil {
		return err
	}
	_, err = s.db.Exec("INSERT INTO bookmarks(id,data,created_at) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data", item.ID, string(data), time.Now().UnixMilli())
	return err
}
func (s *Store) Record(query string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("INSERT INTO history(query,searched_at) VALUES(?,?) ON CONFLICT(query) DO UPDATE SET searched_at=excluded.searched_at", query, time.Now().UnixMilli()); err != nil {
		return err
	}
	if _, err = tx.Exec("DELETE FROM history WHERE query NOT IN (SELECT query FROM history ORDER BY searched_at DESC LIMIT 100)"); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) History() ([]model.History, error) {
	rows, err := s.db.Query("SELECT query,searched_at FROM history ORDER BY searched_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.History{}
	for rows.Next() {
		var h model.History
		var at int64
		if err = rows.Scan(&h.Query, &at); err != nil {
			return nil, err
		}
		h.At = time.UnixMilli(at)
		out = append(out, h)
	}
	return out, rows.Err()
}
func (s *Store) ClearHistory() error { _, err := s.db.Exec("DELETE FROM history"); return err }
