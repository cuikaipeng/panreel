package app

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cuikaipeng/panreel/internal/model"
	"github.com/egoist/mygo/ui"
)

func linkCheckTestApp(t *testing.T) (*app, chan func()) {
	t.Helper()
	a := settingsTestApp(t)
	a.online, a.searched = true, "test"
	a.settings.Channels = []string{"example_channel"}
	a.items = []model.Item{
		{ID: "first", Drive: "quark", Title: "First result", URL: "https://pan.quark.cn/s/first"},
		{ID: "second", Drive: "baidu", Title: "Second result", URL: "https://pan.baidu.com/s/second"},
		{ID: "magnet", Drive: "magnet", Title: "Magnet result", URL: "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567"},
	}
	queue := make(chan func(), 20)
	a.dispatch = func(fn func()) { queue <- fn }
	t.Cleanup(a.cancelLinkCheck)
	return a, queue
}

func receiveCheck(t *testing.T, queue chan func()) {
	t.Helper()
	select {
	case fn := <-queue:
		fn()
	case <-time.After(2 * time.Second):
		t.Fatal("missing link check callback")
	}
}

func TestSelectingResultChecksAutomaticallyAndCachesWithManualRetry(t *testing.T) {
	for _, lang := range []string{"zh-Hans", "en"} {
		for _, theme := range []string{"light", "dark"} {
			t.Run(lang+"/"+theme, func(t *testing.T) {
				a, queue := linkCheckTestApp(t)
				a.prefs.Language, a.prefs.Theme = lang, theme
				a.items[0].Description = strings.Repeat("A long description. ", 40)
				started := make(chan struct{}, 10)
				replies := make(chan model.Check, 10)
				var calls atomic.Int32
				a.checkLink = func(ctx context.Context, item model.Item) (model.Check, error) {
					calls.Add(1)
					started <- struct{}{}
					select {
					case result := <-replies:
						return result, nil
					case <-ctx.Done():
						return model.Check{}, ctx.Err()
					}
				}
				waitStarted := func() {
					t.Helper()
					select {
					case <-started:
					case <-time.After(time.Second):
						t.Fatal("selection did not start a check")
					}
				}
				tt := ui.NewTester(a.view, 960, 640)
				if err := tt.Click("First result"); err != nil {
					t.Fatal(err)
				}
				waitStarted()
				tt.Frame()
				status, ok := tt.AccessByRole(ui.RoleStatus, a.tr("正在检测…"))
				if !ok || status.Offscreen || status.Bounds.Y > 350 {
					t.Fatal("automatic check status is not visible near detail title", status.Bounds)
				}
				if err := tt.Click("First result"); err != nil {
					t.Fatal(err)
				}
				if calls.Load() != 1 {
					t.Fatal("repeated selection duplicated an active request")
				}
				replies <- model.Check{State: "ok"}
				receiveCheck(t, queue)
				tt.Frame()
				if !tt.HasText(a.tr("链接可访问")) || a.checkActive != "" {
					t.Fatal("completed check not displayed")
				}
				if err := tt.Click("Magnet result"); err != nil {
					t.Fatal(err)
				}
				if !tt.HasText(a.tr("不支持自动检测")) || calls.Load() != 1 {
					t.Fatal("unsupported type was checked or misreported")
				}
				if err := tt.Click("First result"); err != nil {
					t.Fatal(err)
				}
				if calls.Load() != 1 || !tt.HasText(a.tr("链接可访问")) {
					t.Fatal("fresh cached result not reused")
				}
				if err := tt.Click(a.tr("Recheck link")); err != nil {
					t.Fatal(err)
				}
				waitStarted()
				replies <- model.Check{State: "bad"}
				receiveCheck(t, queue)
				tt.Frame()
				if calls.Load() != 2 || !tt.HasText(a.tr("链接已失效")) {
					t.Fatal("manual retry failed to replace cached status")
				}
			})
		}
	}
}

func TestSlowPreviousCheckCannotOverwriteNewSelection(t *testing.T) {
	a, queue := linkCheckTestApp(t)
	firstDone, secondDone := make(chan struct{}), make(chan struct{})
	firstContext := make(chan context.Context, 1)
	a.checkLink = func(ctx context.Context, item model.Item) (model.Check, error) {
		if item.ID == "first" {
			firstContext <- ctx
			<-firstDone // Deliberately simulate a late completion after cancellation.
			return model.Check{State: "ok"}, nil
		}
		<-secondDone
		return model.Check{State: "bad"}, nil
	}
	tt := ui.NewTester(a.view, 960, 640)
	if err := tt.Click("First result"); err != nil {
		t.Fatal(err)
	}
	var ctx context.Context
	select {
	case ctx = <-firstContext:
	case <-time.After(time.Second):
		t.Fatal("first check did not start")
	}
	if err := tt.Click("Second result"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("previous selection was not cancelled")
	}
	close(secondDone)
	receiveCheck(t, queue)
	close(firstDone)
	receiveCheck(t, queue)
	tt.Frame()
	if a.selected != 1 || !tt.HasText(a.tr("链接已失效")) || tt.HasText(a.tr("链接可访问")) || a.checkActive != "" {
		t.Fatal("late response replaced current details")
	}
}

func TestCheckErrorsRemainUncertainAndNewPasswordOrExpiredCacheRechecks(t *testing.T) {
	a, queue := linkCheckTestApp(t)
	var calls atomic.Int32
	a.checkLink = func(ctx context.Context, item model.Item) (model.Check, error) {
		calls.Add(1)
		if item.Password == "" {
			return model.Check{}, errors.New("network unavailable")
		}
		return model.Check{State: "ok"}, nil
	}
	a.selected = 0
	a.checkSelectedResult()
	receiveCheck(t, queue)
	oldKey := linkCheckKey(a.items[0])
	if a.checks[oldKey].State != "uncertain" || a.message != "" {
		t.Fatal("network failure marked link invalid or showed a global notice")
	}
	a.checkSelectedResult()
	if calls.Load() != 1 {
		t.Fatal("uncertain result retried without a delay")
	}
	// A later batch can enrich the very same selected resource with a code.
	a.loading, a.searchID = true, 1
	newItem := a.items[0]
	newItem.Password = "1234"
	a.applySearchBatch(1, []model.Item{newItem})
	receiveCheck(t, queue)
	key := linkCheckKey(a.items[0])
	if key == oldKey || calls.Load() != 2 || a.checks[key].State != "ok" {
		t.Fatal("new extraction code reused an old check")
	}
	cached := a.checks[key]
	cached.At = time.Now().Add(-5*time.Minute - time.Second)
	a.checks[key] = cached
	a.checkSelectedResult()
	receiveCheck(t, queue)
	if calls.Load() != 3 {
		t.Fatal("expired check was not refreshed")
	}
}

func TestKeyboardSelectionAlsoChecksAndNavigationCancels(t *testing.T) {
	a, _ := linkCheckTestApp(t)
	started := make(chan string, 5)
	a.checkLink = func(ctx context.Context, item model.Item) (model.Check, error) {
		started <- item.ID
		<-ctx.Done()
		return model.Check{}, ctx.Err()
	}
	tt := ui.NewTester(a.view, 960, 640)
	if err := tt.Click("First result"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyDown)
	seenSecond := false
	deadline := time.After(time.Second)
	for !seenSecond {
		select {
		case id := <-started:
			seenSecond = id == "second"
		case <-deadline:
			t.Fatal("keyboard selection did not start a check")
		}
	}
	a.navigate("sources")
	if a.checkActive != "" || a.checkCancel != nil {
		t.Fatal("leaving the list retained an active check")
	}
}
