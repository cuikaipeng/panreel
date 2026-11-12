package storage

import (
	"testing"
	"time"

	"github.com/cuikaipeng/panreel/internal/model"
)

func TestBookmarkWatchPersistenceAndStaleWrites(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	item := model.Item{ID: "series", Title: "Series", Drive: "quark", URL: "https://pan.quark.cn/s/series"}
	if err = s.Bookmark(item, true); err != nil {
		t.Fatal(err)
	}
	initial, err := s.WatchSummaries()
	if err != nil || len(initial) != 0 {
		t.Fatal(initial, err)
	}
	now := time.Now().Truncate(time.Millisecond)
	if err = s.ConfigureWatch(item.ID, true, 60, now); err != nil {
		t.Fatal(err)
	}
	w, err := s.Watch(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	result := model.WatchResult{Initialized: true, Files: []model.ResourceFile{{ID: "ep1", Name: "Episode 1.mkv", Size: 42}}, CheckedAt: now, NextCheck: now.Add(time.Hour), UpdatedAt: now, Added: 1}
	if ok, err := s.SaveWatchResult(w, result); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if err = s.ReadWatch(item.ID, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	// A check that began before marking as read must not make it unread again.
	if ok, err := s.SaveWatchResult(w, result); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.Watch(item.ID)
	if err != nil || !got.Enabled || got.IntervalMinutes != 60 || !got.Result.Initialized || len(got.Result.Files) != 1 || got.Unread() {
		t.Fatal(got, err)
	}
	summaries, err := s.WatchSummaries()
	if err != nil || len(summaries[item.ID].Result.Files) != 0 || !summaries[item.ID].Result.Initialized {
		t.Fatal("UI loaded the full directory", summaries, err)
	}
	if err = s.ConfigureWatch(item.ID, false, 180, now); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.SaveWatchResult(w, result); ok || err != nil {
		t.Fatal("disabled watch accepted late result", ok, err)
	}
	if err = s.ConfigureWatch(item.ID, true, 180, now); err != nil {
		t.Fatal(err)
	}
	w, err = s.Watch(item.ID)
	if err != nil || !w.Result.Initialized || len(w.Result.Files) != 1 {
		t.Fatal("reenabling discarded baseline", err)
	}
	if err = s.Bookmark(item, false); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.SaveWatchResult(w, result); ok || err != nil {
		t.Fatal("removed favorite resurrected", ok, err)
	}
	if err = s.Bookmark(item, true); err != nil {
		t.Fatal(err)
	}
	summaries, err = s.WatchSummaries()
	if err != nil || len(summaries) != 0 {
		t.Fatal("refavorite retained old subscription", summaries, err)
	}
	if err = s.ConfigureWatch(item.ID, true, 60, now); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.SaveWatchResult(w, result); ok || err != nil {
		t.Fatal("old generation matched recreated watch", ok, err)
	}
	if err = s.ConfigureWatch(item.ID, true, 1, now); err == nil {
		t.Fatal("accepted a one-minute polling loop")
	}
	if err = s.ConfigureWatch("missing", true, 60, now); err == nil {
		t.Fatal("created orphan watch")
	}
}
