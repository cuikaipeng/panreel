package watch

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/cuikaipeng/panreel/internal/model"
	"github.com/cuikaipeng/panreel/internal/storage"
)

func TestSnapshotsOnlyNotifyNewOrChangedContent(t *testing.T) {
	now := time.Now()
	w := model.BookmarkWatch{Enabled: true, IntervalMinutes: 60}
	first := []model.ResourceFile{{ID: "ep1", Name: "Season 1/Episode 1.mkv", Size: 10, Hash: "a"}}
	r, updated := Apply(w, first, nil, now)
	if updated || !r.Initialized {
		t.Fatal("baseline was reported as an update")
	}
	w.Result = r
	r, updated = Apply(w, []model.ResourceFile{{ID: "ep1", Name: "Renamed/Episode 1.mkv", Size: 10, Hash: "a"}}, nil, now.Add(time.Hour))
	if updated {
		t.Fatal("rename was reported as new content")
	}
	w.Result = r
	next := []model.ResourceFile{{ID: "ep1", Name: "Renamed/Episode 1.mkv", Size: 10, Hash: "b"}, {ID: "ep2", Name: "Renamed/Episode 2.mkv", Size: 11, Hash: "c"}}
	r, updated = Apply(w, next, nil, now.Add(2*time.Hour))
	if !updated || r.Added != 1 || r.Changed != 1 || len(r.Names) != 2 {
		t.Fatal(r, updated)
	}
	w.Result = r
	r, updated = Apply(w, nil, Incomplete, now.Add(3*time.Hour))
	if updated || !reflect.DeepEqual(r.Files, next) || !r.UpdatedAt.Equal(w.Result.UpdatedAt) || r.Issue != "incomplete" {
		t.Fatal("failed scan lost baseline or unread updates", r)
	}
	w.Result = r
	r, updated = Apply(w, next, nil, now.Add(4*time.Hour))
	if updated || r.Issue != "" || r.Failures != 0 {
		t.Fatal("same update repeated after a failure", r)
	}
	w.Result = r
	r, updated = Apply(w, next[:1], nil, now.Add(5*time.Hour))
	if updated || len(r.Files) != 1 {
		t.Fatal("deletion sent update reminder", r)
	}
	for i := 0; i < 12; i++ {
		w.Result = r
		r, _ = Apply(w, nil, Network, now)
	}
	if r.NextCheck.Sub(now) != 24*time.Hour {
		t.Fatal("failure backoff not bounded", r.NextCheck.Sub(now))
	}
}

func TestRunnerPersistsBeforeNotifyingAndIgnoresDisabledWork(t *testing.T) {
	s, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	item := model.Item{ID: "series", Drive: "quark", Title: "Series", URL: "https://pan.quark.cn/s/demo"}
	if err = s.Bookmark(item, true); err != nil {
		t.Fatal(err)
	}
	if err = s.ConfigureWatch(item.ID, true, 30, time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	files := []model.ResourceFile{{ID: "one", Name: "one.mkv", Size: 1}}
	notifications := 0
	r := NewRunner(s, func(e Event) {
		if e.Updated {
			notifications++
			saved, err := s.Watch(item.ID)
			if err != nil || len(saved.Result.Files) != 2 {
				t.Error("notification preceded persistence", err)
			}
		}
	})
	r.Fetch = func(context.Context, model.Item, model.Settings) ([]model.ResourceFile, error) { return files, nil }
	i, w, found, err := r.due(time.Now())
	if err != nil || !found || i.ID != item.ID {
		t.Fatal(i, w, found, err)
	}
	r.check(context.Background(), item, w)
	if notifications != 0 {
		t.Fatal("baseline notified")
	}
	_, _, found, _ = r.due(time.Now())
	if found {
		t.Fatal("scan ignored next due time")
	}
	w, err = s.Watch(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	files = append(files, model.ResourceFile{ID: "two", Name: "two.mkv", Size: 2})
	r.check(context.Background(), item, w)
	if notifications != 1 {
		t.Fatal("missing update reminder")
	}
	w, err = s.Watch(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	r.check(context.Background(), item, w)
	if notifications != 1 {
		t.Fatal("duplicate reminder")
	}
	r.Fetch = func(ctx context.Context, _ model.Item, _ model.Settings) ([]model.ResourceFile, error) {
		if err := s.ConfigureWatch(item.ID, false, 30, time.Now()); err != nil {
			t.Fatal(err)
		}
		r.Changed(item.ID)
		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Error("disable failed to cancel network work")
		}
		return append(files, model.ResourceFile{ID: "three", Name: "three.mkv"}), nil
	}
	r.check(context.Background(), item, w)
	current, err := s.Watch(item.ID)
	if err != nil || notifications != 1 || len(current.Result.Files) != 2 {
		t.Fatal("disabled work committed", err)
	}
}
