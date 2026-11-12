package search

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cuikaipeng/panreel/internal/model"
)

func TestVerificationStreamsValidResultsAndKeepsOtherStatesDistinct(t *testing.T) {
	items := []model.Item{
		{ID: "ok", Drive: "quark"}, {ID: "bad", Drive: "baidu"},
		{ID: "uncertain", Drive: "aliyun"}, {ID: "locked", Drive: "tianyi"},
		{ID: "unsupported", Drive: "magnet"}, {ID: "slow", Drive: "uc"},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	firstValid := make(chan struct{})
	var release sync.Once
	var calls atomic.Int32
	var last VerifiedUpdate
	err := verifyUpdates(ctx, func(_ context.Context, emit func([]model.Item)) error {
		emit(items)
		emit(items) // Repeated engine snapshots must not duplicate requests.
		return nil
	}, func(ctx context.Context, item model.Item) (model.Check, error) {
		calls.Add(1)
		if item.ID == "slow" {
			select {
			case <-firstValid:
				return model.Check{State: "ok"}, nil
			case <-ctx.Done():
				return model.Check{}, ctx.Err()
			}
		}
		if item.ID == "uncertain" {
			return model.Check{}, errors.New("network unavailable")
		}
		return model.Check{State: item.ID}, nil
	}, nil, func(update VerifiedUpdate) {
		for _, item := range update.Items {
			if item.ID != "ok" && item.ID != "slow" {
				t.Error("unverified link was published", item.ID)
			}
			if item.ID == "ok" {
				release.Do(func() { close(firstValid) })
			}
		}
		last = update
	})
	if err != nil || calls.Load() != 5 || len(last.Items) != 2 || len(last.All) != 6 {
		t.Fatal("incorrect streamed verification", err, calls.Load(), last.Progress)
	}
	if last.Progress.Invalid != 1 || last.Progress.Unconfirmed != 3 || last.Progress.Checked != 6 || last.Progress.Searching {
		t.Fatal("failure/locked/unsupported were conflated with expiry", last.Progress)
	}
}

func TestLateExtractionCodeDoesNotReuseOrPublishAnOldCheck(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	oldStarted, releaseOld := make(chan struct{}), make(chan struct{})
	old := model.Item{ID: "share", Drive: "quark", URL: "https://pan.quark.cn/s/share"}
	newer := old
	newer.Password = "1234"
	var calls atomic.Int32
	var last VerifiedUpdate
	err := verifyUpdates(ctx, func(ctx context.Context, emit func([]model.Item)) error {
		emit([]model.Item{old})
		select {
		case <-oldStarted:
		case <-ctx.Done():
			return ctx.Err()
		}
		emit([]model.Item{newer})
		return nil
	}, func(ctx context.Context, item model.Item) (model.Check, error) {
		calls.Add(1)
		if item.Password == "" {
			close(oldStarted)
			select {
			case <-releaseOld:
				return model.Check{State: "bad"}, nil
			case <-ctx.Done():
				return model.Check{}, ctx.Err()
			}
		}
		close(releaseOld)
		return model.Check{State: "ok"}, nil
	}, nil, func(update VerifiedUpdate) { last = update })
	if err != nil || calls.Load() != 2 || len(last.Items) != 1 || last.Items[0].Password != "1234" {
		t.Fatal("late extraction code was not rechecked", err, last)
	}
	if _, ok := last.Checks[CheckKey(old)]; ok {
		t.Fatal("old in-flight verdict overwrote the updated share")
	}
}

func TestVerificationUsesFreshCacheAndRechecksStaleEntries(t *testing.T) {
	items := []model.Item{{ID: "fresh", Drive: "quark"}, {ID: "stale", Drive: "baidu"}}
	cached := map[string]model.Check{
		CheckKey(items[0]): {State: "ok", At: time.Now()},
		CheckKey(items[1]): {State: "ok", At: time.Now().Add(-6 * time.Minute)},
	}
	var last VerifiedUpdate
	err := verifyUpdates(context.Background(), func(_ context.Context, emit func([]model.Item)) error {
		emit(items)
		return nil
	}, func(_ context.Context, item model.Item) (model.Check, error) {
		if item.ID != "stale" {
			t.Error("fresh cache was checked again")
		}
		return model.Check{State: "bad"}, nil
	}, cached, func(update VerifiedUpdate) { last = update })
	if err != nil || len(last.Items) != 1 || last.Items[0].ID != "fresh" || last.Progress.Invalid != 1 {
		t.Fatal("incorrect cache behavior", err, last.Progress)
	}
}

func TestVerificationLimitsConcurrencyAndCancellationStopsQueuedWork(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan string, 20)
	var active, maximum, calls atomic.Int32
	var mu sync.Mutex
	byDrive := map[string]int{}
	items := make([]model.Item, 0, 20)
	for i := range 20 {
		drive := []string{"quark", "baidu"}[i%2]
		items = append(items, model.Item{ID: fmt.Sprint(i), Drive: drive, URL: fmt.Sprint(i)})
	}
	done := make(chan error, 1)
	go func() {
		done <- verifyUpdates(ctx, func(_ context.Context, emit func([]model.Item)) error {
			emit(items)
			return nil
		}, func(ctx context.Context, item model.Item) (model.Check, error) {
			calls.Add(1)
			current := active.Add(1)
			defer active.Add(-1)
			for old := maximum.Load(); current > old && !maximum.CompareAndSwap(old, current); old = maximum.Load() {
			}
			mu.Lock()
			byDrive[item.Drive]++
			if byDrive[item.Drive] > 2 {
				t.Error("provider concurrency exceeded")
			}
			mu.Unlock()
			started <- item.ID
			<-ctx.Done()
			return model.Check{}, ctx.Err()
		}, nil, func(VerifiedUpdate) {})
	}()
	for range 4 {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("worker pool failed to start")
		}
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || maximum.Load() > 4 || calls.Load() != 4 {
			t.Fatal("cancellation/concurrency failed", err, maximum.Load(), calls.Load())
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation waited for queued work")
	}
}
