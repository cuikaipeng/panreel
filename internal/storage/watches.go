package storage

import (
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"time"

	"github.com/cuikaipeng/panreel/internal/model"
)

// UI refreshes and scheduler ticks need metadata, not every watched file name.
func (s *Store) WatchSummaries() (map[string]model.BookmarkWatch, error) {
	return s.readWatches("json_remove(w.state, '$.files')", "", nil)
}

func (s *Store) Watch(id string) (model.BookmarkWatch, error) {
	rows, err := s.readWatches("w.state", " WHERE w.bookmark_id=?", []any{id})
	if err != nil {
		return model.BookmarkWatch{}, err
	}
	w, ok := rows[id]
	if !ok {
		return model.BookmarkWatch{}, sql.ErrNoRows
	}
	return w, nil
}

func (s *Store) readWatches(state, where string, args []any) (map[string]model.BookmarkWatch, error) {
	rows, err := s.db.Query(`SELECT w.bookmark_id,w.enabled,w.interval_minutes,w.generation,w.read_at,`+state+`
FROM bookmark_watches w JOIN bookmarks b ON b.id=w.bookmark_id`+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]model.BookmarkWatch{}
	for rows.Next() {
		var w model.BookmarkWatch
		var data string
		var at int64
		if err := rows.Scan(&w.ItemID, &w.Enabled, &w.IntervalMinutes, &w.Generation, &at, &data); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(data), &w.Result); err != nil {
			return nil, err
		}
		if at != 0 {
			w.ReadAt = time.Unix(0, at)
		}
		out[w.ItemID] = w
	}
	return out, rows.Err()
}

// ConfigureWatch invalidates any in-flight check. The file baseline survives
// disabling/re-enabling a watch and restarting the application.
func (s *Store) ConfigureWatch(id string, enabled bool, minutes int, now time.Time) error {
	if !model.ValidWatchInterval(minutes) {
		return errors.New("invalid watch interval")
	}
	// A fresh generation must also differ after removing and re-adding the
	// same bookmark; restarting an integer counter would accept stale work.
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	generation := int64(binary.LittleEndian.Uint64(nonce[:]) & (1<<63 - 1))
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRow("SELECT 1 FROM bookmarks WHERE id=?", id).Scan(&exists); err != nil {
		return err
	}
	result := model.WatchResult{}
	var data string
	err = tx.QueryRow("SELECT state FROM bookmark_watches WHERE bookmark_id=?", id).Scan(&data)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err == nil {
		if err := json.Unmarshal([]byte(data), &result); err != nil {
			return err
		}
	}
	result.NextCheck = now
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO bookmark_watches(bookmark_id,enabled,interval_minutes,generation,read_at,state) VALUES(?,?,?,?,0,?)
ON CONFLICT(bookmark_id) DO UPDATE SET enabled=excluded.enabled,interval_minutes=excluded.interval_minutes,generation=excluded.generation,state=excluded.state`, id, enabled, minutes, generation, string(raw))
	if err != nil {
		return err
	}
	return tx.Commit()
}

// A deleted, disabled, or reconfigured bookmark cannot be resurrected by a late
// response. read_at lives outside state so acknowledging a notification cannot
// be undone by a check that was already in progress.
func (s *Store) SaveWatchResult(w model.BookmarkWatch, result model.WatchResult) (bool, error) {
	data, err := json.Marshal(result)
	if err != nil {
		return false, err
	}
	r, err := s.db.Exec(`UPDATE bookmark_watches SET state=? WHERE bookmark_id=? AND generation=? AND enabled=1
AND EXISTS (SELECT 1 FROM bookmarks WHERE id=bookmark_id)`, string(data), w.ItemID, w.Generation)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	return n == 1, err
}

func (s *Store) ReadWatch(id string, now time.Time) error {
	_, err := s.db.Exec("UPDATE bookmark_watches SET read_at=? WHERE bookmark_id=?", now.UnixNano(), id)
	return err
}
