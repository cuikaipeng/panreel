package search

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"maps"
	"slices"
	"time"

	"github.com/cuikaipeng/panreel/internal/model"
)

type VerificationProgress struct {
	Found, Checked, Valid, Invalid, Unconfirmed int
	Searching                                   bool
}

type VerifiedUpdate struct {
	Items, All []model.Item
	Checks     map[string]model.Check
	Progress   VerificationProgress
}

func CheckKey(item model.Item) string {
	// A code supplied by a later source must trigger a new check.
	sum := sha256.Sum256([]byte(item.Drive + "\x00" + item.URL + "\x00" + item.Password))
	return hex.EncodeToString(sum[:])
}

func FreshCheck(check model.Check) bool {
	ttl := 5 * time.Minute
	if check.State == "uncertain" {
		ttl = 30 * time.Second
	}
	return !check.At.IsZero() && time.Since(check.At) >= 0 && time.Since(check.At) < ttl
}

// SearchVerifiedUpdates searches and checks concurrently. One deadline covers
// both stages, and cancellation stops scheduling checks for the old query.
func (c *Client) SearchVerifiedUpdates(ctx context.Context, query string, settings model.Settings, refresh bool, cached map[string]model.Check, emit func(VerifiedUpdate)) error {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(settings.Timeout)*time.Second)
	defer cancel()
	if refresh {
		cached = nil
	}
	return verifyUpdates(ctx, func(ctx context.Context, emit func([]model.Item)) error {
		return c.SearchUpdates(ctx, query, settings, refresh, emit)
	}, c.Check, cached, emit)
}

func verifyUpdates(ctx context.Context, fetch func(context.Context, func([]model.Item)) error, check func(context.Context, model.Item) (model.Check, error), cached map[string]model.Check, emit func(VerifiedUpdate)) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type sourceUpdate struct {
		items []model.Item
		done  bool
		err   error
	}
	type checkedItem struct {
		item   model.Item
		result model.Check
	}
	source := make(chan sourceUpdate, 1)
	finished := make(chan checkedItem, 4)
	go func() {
		send := func(update sourceUpdate) {
			select {
			case source <- update:
			case <-ctx.Done():
			}
		}
		err := fetch(ctx, func(items []model.Item) { send(sourceUpdate{items: slices.Clone(items)}) })
		send(sourceUpdate{done: true, err: err})
	}()

	var all, queued []model.Item
	checks := map[string]model.Check{}
	scheduled := map[string]bool{}
	current := map[string]string{}
	activeByDrive := map[string]int{}
	nextByDrive := map[string]time.Time{}
	active := 0
	searching, dirty := true, false
	var searchErr error
	var lastEmit time.Time
	lastValid := 0
	flush := func() {
		if !dirty || ctx.Err() != nil {
			return
		}
		progress := VerificationProgress{Found: len(all), Searching: searching}
		valid := make([]model.Item, 0, len(all))
		for _, item := range all {
			result, ok := checks[CheckKey(item)]
			if !ok {
				continue
			}
			progress.Checked++
			switch result.State {
			case "ok":
				progress.Valid++
				valid = append(valid, item)
			case "bad":
				progress.Invalid++
			default:
				progress.Unconfirmed++
			}
		}
		emit(VerifiedUpdate{Items: valid, All: slices.Clone(all), Checks: maps.Clone(checks), Progress: progress})
		lastEmit, lastValid, dirty = time.Now(), len(valid), false
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		// At most four requests in total, two per provider, and at least
		// 250 ms between starts for one provider. A slow provider cannot
		// occupy all slots or block later sources from producing results.
		for i := 0; i < len(queued) && active < 4; {
			item := queued[i]
			key := CheckKey(item)
			if current[item.ID] != key {
				queued = slices.Delete(queued, i, i+1)
				continue
			}
			if activeByDrive[item.Drive] >= 2 || time.Now().Before(nextByDrive[item.Drive]) {
				i++
				continue
			}
			queued = slices.Delete(queued, i, i+1)
			active++
			activeByDrive[item.Drive]++
			nextByDrive[item.Drive] = time.Now().Add(250 * time.Millisecond)
			go func() {
				result, err := check(ctx, item)
				if err != nil {
					result = model.Check{State: "uncertain", Summary: "Link check failed. Try again later."}
					if errors.Is(err, context.DeadlineExceeded) {
						result.Summary = "Link check timed out. Try again later."
					}
				}
				if result.State != "ok" && result.State != "bad" && result.State != "locked" && result.State != "unsupported" && result.State != "uncertain" {
					result = model.Check{State: "uncertain"}
				}
				result.At = time.Now()
				select {
				case finished <- checkedItem{item, result}:
				case <-ctx.Done():
				}
			}()
		}
		if !searching && len(queued) == 0 && active == 0 {
			flush()
			return searchErr
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case update := <-source:
			if update.done {
				searching, searchErr = false, update.err
			} else {
				all = MergeItems(all, update.items)
				for _, item := range all {
					key := CheckKey(item)
					current[item.ID] = key
					if scheduled[key] {
						continue
					}
					scheduled[key] = true
					if !CanCheck(item.Drive) {
						checks[key] = model.Check{State: "unsupported", At: time.Now()}
					} else if value, ok := cached[key]; ok && FreshCheck(value) {
						checks[key] = value
					} else {
						queued = append(queued, item)
					}
				}
			}
			dirty = true
			flush()
		case done := <-finished:
			active--
			activeByDrive[done.item.Drive]--
			key := CheckKey(done.item)
			if current[done.item.ID] == key {
				checks[key] = done.result
				dirty = true
				if lastValid == 0 && done.result.State == "ok" {
					flush() // The first usable result should appear immediately.
				}
			}
		case <-ticker.C:
			if time.Since(lastEmit) >= 150*time.Millisecond {
				flush()
			}
		}
	}
}
