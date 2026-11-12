package watch

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/cuikaipeng/panreel/internal/model"
	"github.com/cuikaipeng/panreel/internal/storage"
)

type Event struct {
	Item              model.Item
	Watch             model.BookmarkWatch
	Checking, Updated bool
	Err               error
}

// Runner performs at most one share scan at a time. It has no engine cache:
// each successful check is a complete directory listing from the provider.
type Runner struct {
	Store   *storage.Store
	Fetch   func(context.Context, model.Item, model.Settings) ([]model.ResourceFile, error)
	OnEvent func(Event)
	wake    chan struct{}
	mu      sync.Mutex
	active  string
	cancel  context.CancelFunc
}

func NewRunner(store *storage.Store, onEvent func(Event)) *Runner {
	return &Runner{Store: store, Fetch: Snapshot, OnEvent: onEvent, wake: make(chan struct{}, 1)}
}

func (r *Runner) Changed(id string) {
	r.mu.Lock()
	if r.active == id && r.cancel != nil {
		r.cancel()
	}
	r.mu.Unlock()
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

func (r *Runner) emit(e Event) {
	if r.OnEvent != nil {
		r.OnEvent(e)
	}
}

func (r *Runner) Run(ctx context.Context) {
	for ctx.Err() == nil {
		item, w, found, err := r.due(time.Now())
		if err != nil {
			r.emit(Event{Err: err})
		}
		if err != nil || !found {
			timer := time.NewTimer(30 * time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-r.wake:
				timer.Stop()
			case <-timer.C:
			}
			continue
		}
		storageFailed := r.check(ctx, item, w)
		// Spread startup catch-up work instead of bursting requests/notifications.
		delay := time.Second
		if storageFailed {
			delay = 30 * time.Second
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-r.wake:
			timer.Stop()
		case <-timer.C:
		}
	}
}

func (r *Runner) due(now time.Time) (model.Item, model.BookmarkWatch, bool, error) {
	watches, err := r.Store.WatchSummaries()
	if err != nil {
		return model.Item{}, model.BookmarkWatch{}, false, err
	}
	items, err := r.Store.Bookmarks()
	if err != nil {
		return model.Item{}, model.BookmarkWatch{}, false, err
	}
	var selected model.Item
	var next model.BookmarkWatch
	found := false
	for _, item := range items {
		w, ok := watches[item.ID]
		if !ok || !w.Enabled || w.Result.NextCheck.After(now) {
			continue
		}
		if !found || w.Result.NextCheck.Before(next.Result.NextCheck) {
			selected = item
			next = w
			found = true
		}
	}
	return selected, next, found, nil
}

func (r *Runner) check(ctx context.Context, item model.Item, w model.BookmarkWatch) bool {
	checkCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	r.mu.Lock()
	r.active = item.ID
	r.cancel = cancel
	r.mu.Unlock()
	defer func() { cancel(); r.mu.Lock(); r.active = ""; r.cancel = nil; r.mu.Unlock() }()
	current, err := r.Store.Watch(item.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return false
	}
	if err != nil {
		r.emit(Event{Item: item, Watch: w, Err: err})
		return true
	}
	if !current.Enabled || current.Generation != w.Generation || checkCtx.Err() != nil {
		return false
	}
	w = current
	r.emit(Event{Item: item, Watch: w, Checking: true})
	settings, err := r.Store.Settings()
	if err != nil {
		r.emit(Event{Item: item, Watch: w, Err: err})
		return true
	}
	files, checkErr := r.Fetch(checkCtx, item, settings)
	if ctx.Err() != nil || errors.Is(checkCtx.Err(), context.Canceled) {
		r.emit(Event{Item: item, Watch: w})
		return false
	}
	if checkCtx.Err() != nil {
		checkErr = checkCtx.Err()
	}
	result, changed := Apply(w, files, checkErr, time.Now())
	saved, err := r.Store.SaveWatchResult(w, result)
	w.Result = result
	r.emit(Event{Item: item, Watch: w, Updated: saved && changed, Err: err})
	return err != nil
}

// Apply never uses a partial response, a failed request, a filename change, or
// a folder timestamp as evidence of an update. The first success is a baseline.
func Apply(w model.BookmarkWatch, files []model.ResourceFile, err error, now time.Time) (model.WatchResult, bool) {
	r := w.Result
	r.CheckedAt = now
	interval := time.Duration(w.IntervalMinutes) * time.Minute
	if !model.ValidWatchInterval(w.IntervalMinutes) {
		interval = time.Hour
	}
	if err != nil {
		r.Issue = failure(err)
		r.Failures++
		for i := 1; i < r.Failures && interval < 24*time.Hour; i++ {
			interval *= 2
		}
		if interval > 24*time.Hour {
			interval = 24 * time.Hour
		}
		r.NextCheck = now.Add(interval)
		return r, false
	}
	r.Issue = ""
	r.Failures = 0
	r.NextCheck = now.Add(interval)
	old := make(map[string]model.ResourceFile, len(r.Files))
	for _, f := range r.Files {
		old[f.ID] = f
	}
	added, changed := 0, 0
	names := make([]string, 0)
	if r.Initialized {
		for _, f := range files {
			previous, ok := old[f.ID]
			if !ok {
				added++
			} else if previous.Size != f.Size || (previous.Hash != "" && f.Hash != "" && previous.Hash != f.Hash) {
				changed++
			} else {
				continue
			}
			names = append(names, f.Name)
		}
	}
	r.Files = append([]model.ResourceFile(nil), files...)
	r.Initialized = true
	updated := added+changed > 0
	if updated {
		r.UpdatedAt = now
		r.Added = added
		r.Changed = changed
		sort.Strings(names)
		if len(names) > 5 {
			names = names[:5]
		}
		r.Names = names
	}
	return r, updated
}
