package app

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// Adapted from Kivrix (MIT); see resources/licenses/kivrix.txt.

// MyGo uses the release feed and public key embedded by the packager. Like
// Kero, we prepare a signed update in place, then let the user restart into it.
type appUpdate struct {
	version string
	prepare func(context.Context, func(int64, int64)) error
}

// Native boundaries are replaceable so flow tests never update the test runner.
var (
	findAppUpdate = func(ctx context.Context) (*appUpdate, error) {
		up, err := mygo.Updater.Check(ctx)
		if err != nil || up == nil {
			return nil, err
		}
		return &appUpdate{version: up.Version, prepare: up.Install}, nil
	}
	restartApp = mygo.App.Relaunch
)

func (a *app) checkUpdatesOnLaunch() {
	if a.prefs.CheckUpdates {
		a.beginUpdateCheck(false)
	}
}

func (a *app) checkUpdates() { a.beginUpdateCheck(true) }

func (a *app) beginUpdateCheck(manual bool) {
	if a.closed {
		return
	}
	a.updateNotice = a.updateNotice || manual
	if a.updateChecking || a.updateDownloading || a.updateReady {
		a.refreshUpdateViews()
		return
	}
	a.updateChecking = true
	a.updateStatus = msg("Checking…")
	a.updateErr = ""
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	a.updateCancel = cancel
	go func() {
		defer cancel()
		up, err := findAppUpdate(ctx)
		a.update(func() {
			if a.closed {
				return
			}
			a.updateChecking, a.updateCancel = false, nil
			if errors.Is(err, mygo.ErrUpdatesDisabled) {
				a.updateStatus = msg("This installation cannot update itself. Use an installed release in a writable location.")
			} else if errors.Is(err, context.DeadlineExceeded) {
				a.updateErr = err.Error()
				a.updateStatus = msg("The update server could not be reached. Check your network and try again.")
			} else if err != nil && strings.Contains(err.Error(), "404") {
				a.updateStatus = msg("No published updates yet")
			} else if err != nil {
				a.updateErr = err.Error()
				a.updateStatus = msg("Update check failed: %s", err)
			} else {
				a.availableUpdate = up
				if up == nil {
					a.updateStatus = msg("Up to date")
				} else {
					a.updateStatus = msg("Version %s is available", up.version)
				}
			}
			a.refreshUpdateViews()
		})
	}()
	a.refreshUpdateViews()
}

// Both windows show the same state. Window.Update invalidates only the main
// window, so the separate Settings window also needs to be redrawn.
func (a *app) refreshUpdateViews() {
	if a.window != nil {
		a.window.Invalidate()
	}
	if a.settingsWin != nil {
		a.settingsWin.Invalidate()
	}
}

func (a *app) downloadUpdate() {
	if a.closed || a.updateChecking || a.updateDownloading || a.updateReady || a.availableUpdate == nil {
		return
	}
	a.updateDownloading, a.updateNotice = true, true
	a.updateErr = ""
	a.updateBytes, a.updateTotal = 0, 0
	a.updateStatus = msg("Downloading and preparing update…")
	up := a.availableUpdate
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	a.updateCancel = cancel
	// At most one progress callback is queued, even while the UI is busy.
	var downloaded, total atomic.Int64
	var queued atomic.Bool
	progress := func(n, size int64) {
		downloaded.Store(n)
		total.Store(size)
		if queued.CompareAndSwap(false, true) {
			a.update(func() {
				queued.Store(false)
				if !a.closed && a.updateDownloading {
					a.updateBytes, a.updateTotal = downloaded.Load(), total.Load()
					a.refreshUpdateViews()
				}
			})
		}
	}
	go func() {
		defer cancel()
		err := up.prepare(ctx, progress)
		a.update(func() {
			if a.closed {
				return
			}
			a.updateDownloading, a.updateCancel = false, nil
			if err != nil {
				a.updateErr = err.Error()
				a.updateStatus = msg("Update download failed: %s", err)
			} else {
				a.updateReady = true
				a.updateStatus = msg("Update ready. Install and restart when you are ready.")
			}
			a.refreshUpdateViews()
		})
	}()
	a.refreshUpdateViews()
}

func (a *app) installUpdate() {
	if !a.updateReady || a.closed {
		return
	}
	if !a.flushSettings() {
		return
	}
	a.savePrefs()
	restartApp()
	a.refreshUpdateViews()
}

func (a *app) updateAction() (string, func()) {
	switch {
	case a.updateDownloading:
		if a.updateTotal > 0 {
			percent := int(min(a.updateBytes, a.updateTotal) * 100 / a.updateTotal)
			return a.tr("Downloading… %d%%", percent), nil
		}
		return a.tr("Downloading…"), nil
	case a.updateReady:
		return a.tr("Install and restart"), a.installUpdate
	case a.availableUpdate != nil:
		if a.updateStatus.key == "Update download failed: %s" {
			return a.tr("Retry download"), a.downloadUpdate
		}
		return a.tr("Download update"), a.downloadUpdate
	}
	return "", nil
}

func (a *app) updatePreferences(c *ui.Context) {
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		if a.updateStatus.key != "" {
			muted(c, a.updateStatus.text(a))
		}
		ui.Row(c).Gap(8).Children(func() {
			button(c, a.tr("Check for updates")).Disabled(a.updateChecking || a.updateDownloading).OnClick(a.checkUpdates)
			if label, action := a.updateAction(); label != "" {
				primaryButton(c, label).Disabled(action == nil).OnClick(func() {
					if action != nil {
						action()
					}
				})
			}
		})
	})
}

func (a *app) updateNoticeView(c *ui.Context, pal *palette) {
	if label, action := a.updateAction(); label != "" {
		iconButton(c, "app-update", updateIcon, label, pal, func() {
			if action != nil {
				action()
			}
		}).Disabled(action == nil).TextColor(pal.accentInk)
	} else if a.updateNotice && a.updateErr != "" {
		iconButton(c, "app-update-error", alertIcon, a.tr("Update check failed"), pal, func() { a.openPreferencesTab(preferencesUpdates) }).TextColor(pal.warning)
	}
}
