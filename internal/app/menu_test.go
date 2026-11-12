package app

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cuikaipeng/panreel/internal/engine"
	"github.com/cuikaipeng/panreel/internal/engineupdate"
	"github.com/cuikaipeng/panreel/internal/model"
)

type menuUpdateTransport func(*http.Request) (*http.Response, error)

func (f menuUpdateTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMenuChecksBothUpdatesWithoutDownloadingOrDuplicatingRequests(t *testing.T) {
	oldFind, oldFactory := findAppUpdate, newEngineUpdater
	defer func() { findAppUpdate, newEngineUpdater = oldFind, oldFactory }()
	a := newApp()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.ctx = ctx
	a.engine = &engine.Manager{DataDir: t.TempDir()}
	// A manual check must work even if both automatic checks are turned off.
	a.prefs.CheckUpdates, a.prefs.CheckEngineUpdates = false, false
	queue := make(chan func(), 8)
	a.dispatch = func(fn func()) { queue <- fn }
	var appChecks, engineChecks, downloads atomic.Int32
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	findAppUpdate = func(ctx context.Context) (*appUpdate, error) {
		appChecks.Add(1)
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return &appUpdate{version: "0.4.0", prepare: func(context.Context, func(int64, int64)) error {
			downloads.Add(1)
			return nil
		}}, nil
	}
	newEngineUpdater = func(dir string, settings model.Settings) *engineupdate.Updater {
		u := oldFactory(dir, settings)
		u.HTTP = &http.Client{Transport: menuUpdateTransport(func(r *http.Request) (*http.Response, error) {
			engineChecks.Add(1)
			if r.URL.String() != u.Feed {
				t.Errorf("menu requested a download: %s", r.URL)
			}
			select {
			case <-release:
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
			return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not published"))}, nil
		})}
		return u
	}
	menu := a.applicationMenu()
	menu.ItemByID("about").Click(nil, nil)
	if !a.prefsOpen || a.prefsTab != preferencesAbout {
		t.Fatal("About did not open the app information page")
	}
	check := menu.ItemByID("check-updates")
	check.Click(nil, nil)
	if !a.prefsOpen || a.prefsTab != preferencesUpdates || !a.updateChecking || !a.engineChecking || !a.updateNotice {
		t.Fatal("menu must show the update page and start both manual checks")
	}
	// Selecting the menu again should bring progress back without starting
	// duplicate requests or changing an in-progress check.
	a.prefsTab = preferencesGeneral
	check.Click(nil, nil)
	if a.prefsTab != preferencesUpdates {
		t.Fatal("repeated check did not return to update progress")
	}
	unblock()
	deadline := time.After(3 * time.Second)
	for a.updateChecking || a.engineChecking {
		select {
		case fn := <-queue:
			fn()
		case <-deadline:
			t.Fatal("update checks did not complete")
		}
	}
	if appChecks.Load() != 1 || engineChecks.Load() != 1 || downloads.Load() != 0 || a.updateDownloading || a.engineUpdating {
		t.Fatalf("checks/downloads = %d/%d/%d", appChecks.Load(), engineChecks.Load(), downloads.Load())
	}
	if a.availableUpdate == nil || a.updateStatus.key != "Version %s is available" || a.engineUpdateStatus.key != "The PanSou update channel is not published yet. The bundled engine remains available." {
		t.Fatal("check outcomes were not presented separately")
	}
}
