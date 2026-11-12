package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"github.com/cuikaipeng/panreel/internal/catalog"
	"github.com/cuikaipeng/panreel/internal/engine"
	"github.com/cuikaipeng/panreel/internal/engineupdate"
	"github.com/cuikaipeng/panreel/internal/model"
	"github.com/cuikaipeng/panreel/internal/search"
	"github.com/egoist/mygo/ui"
)

var newEngineUpdater = engineupdate.New

func (a *app) loadEngineVersion() {
	u := newEngineUpdater(a.engine.DataDir, a.settings)
	installed, err := u.Active()
	selected := catalog.Bundled
	if err != nil {
		a.engineUpdateStatus = msg("The installed engine is damaged. Using the bundled version.")
		_ = u.Activate(nil)
		installed = nil
	}
	if installed != nil {
		a.engineInstalled = installed
		a.engine.BinaryPath = installed.Binary
		a.engine.Catalog = &installed.Release.Catalog
		selected = installed.Release.Catalog
	}
	catalog.SetCurrent(selected)
	// On launch this is the same engine, not an upgrade: do not re-enable
	// newly introduced plugins that the user has since disabled. Filtering
	// also recovers a crash between saving settings and moving the pointer.
	a.settings = model.ReconcileEngine(a.settings, selected, selected)
	a.restoreDraft()
}

func (a *app) watchEngineUpdates() {
	if a.prefs.CheckEngineUpdates {
		a.checkEngineUpdate()
	}
	go func() {
		ticker := time.NewTicker(engineupdate.CheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-a.ctx.Done():
				return
			case <-ticker.C:
				a.update(func() {
					if a.prefs.CheckEngineUpdates {
						a.checkEngineUpdate()
					}
				})
			}
		}
	}()
}

func (a *app) checkEngineUpdate() {
	if a.closed || a.engine == nil || a.engineChecking || a.engineUpdating {
		return
	}
	a.engineChecking = true
	a.engineUpdateStatus = msg("Checking PanSou…")
	u := newEngineUpdater(a.engine.DataDir, a.settings)
	current := catalog.Current().Revision
	var installedAt time.Time
	if a.engineInstalled != nil {
		installedAt = a.engineInstalled.Release.BuiltAt
	}
	ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
	a.engineUpdateCancel = cancel
	go func() {
		defer cancel()
		r, err := u.Check(ctx, current, installedAt)
		a.update(func() {
			a.engineChecking = false
			a.engineUpdateCancel = nil
			switch {
			case errors.Is(err, engineupdate.ErrNotPublished):
				a.engineUpdateStatus = msg("The PanSou update channel is not published yet. The bundled engine remains available.")
			case err != nil:
				a.engineUpdateStatus = msg("PanSou update check failed. Try again later.")
			case r == nil:
				a.engineAvailable = nil
				a.engineUpdateStatus = msg("PanSou is up to date.")
			default:
				a.engineAvailable = r
				a.engineUpdateStatus = msg("PanSou %s is available", r.Revision[:12])
			}
		})
	}()
}

func (a *app) installEngineUpdate() {
	if a.closed || a.engine == nil || a.engineAvailable == nil || a.engineChecking || a.engineUpdating || a.starting || a.loading {
		return
	}
	if !a.flushSettings() || a.engineRestartPending {
		return
	}
	a.engineUpdating = true
	a.cancelLinkCheck()
	a.engineDownloaded, a.engineTotal = 0, 0
	a.engineUpdateStatus = msg("Downloading and checking PanSou…")
	a.refreshViews()
	u := newEngineUpdater(a.engine.DataDir, a.settings)
	release := *a.engineAvailable
	before, previous := a.settings.Clone(), catalog.Current()
	previousInstall := a.engineInstalled
	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Minute)
	a.engineUpdateCancel = cancel
	var bytes, total atomic.Int64
	var queued atomic.Bool
	progress := func(n, size int64) {
		bytes.Store(n)
		total.Store(size)
		if queued.CompareAndSwap(false, true) {
			a.update(func() {
				queued.Store(false)
				if a.engineUpdating {
					a.engineDownloaded = bytes.Load()
					a.engineTotal = total.Load()
				}
			})
		}
	}
	a.engineWork.Add(1)
	go func() {
		defer a.engineWork.Done()
		defer cancel()
		installed, err := u.Prepare(ctx, release, progress)
		next := before
		if err == nil {
			next = model.ReconcileEngine(before, previous, installed.Release.Catalog)
			err = a.probeEngine(ctx, installed, next)
		}
		if err == nil {
			err = a.switchEngine(ctx, u, installed, next, previousInstall, before, previous)
		}
		var health = a.healthSnapshot(ctx)
		a.update(func() {
			a.engineUpdating = false
			defer func() { a.flushSettings(); a.scheduleEngineApply(); a.checkSelectedResult() }()
			a.engineUpdateCancel = nil
			a.online = a.engine.Client() != nil
			if err != nil {
				if a.online {
					a.engineUpdateStatus = msg("PanSou update failed; the previous engine is still active. %s", err)
				} else {
					a.engineUpdateStatus = msg("PanSou update failed. Restart the engine in Preferences. %s", err)
				}
				return
			}
			a.engineInstalled = installed
			a.engineAvailable = nil
			a.settings = next
			a.health = health
			a.draft = model.ReconcileEngine(a.draft, previous, installed.Release.Catalog)
			a.engineUpdateStatus = msg("PanSou updated. No app restart required.")
		})
	}()
}

func (a *app) probeEngine(ctx context.Context, installed *engineupdate.Installed, settings model.Settings) error {
	dir, err := os.MkdirTemp(a.engine.DataDir, ".engine-check-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	probe := &engine.Manager{Resources: a.engine.Resources, DataDir: dir, BinaryPath: installed.Binary, Catalog: &installed.Release.Catalog}
	defer probe.Stop()
	return probe.Start(ctx, settings)
}

// Commit only after the replacement starts. Roll back process, catalog,
// configuration and active pointer together if any persistence step fails.
func (a *app) switchEngine(ctx context.Context, u *engineupdate.Updater, next *engineupdate.Installed, settings model.Settings, old *engineupdate.Installed, oldSettings model.Settings, oldCatalog catalog.Catalog) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a.engine.Stop()
	a.engine.BinaryPath = next.Binary
	a.engine.Catalog = &next.Release.Catalog
	err := a.engine.Start(ctx, settings)
	if err == nil {
		catalog.SetCurrent(next.Release.Catalog)
		if a.store != nil {
			err = a.store.SaveSettings(settings)
		}
	}
	if err == nil {
		err = u.Activate(next)
	}
	if err == nil {
		return nil
	}
	a.engine.Stop()
	catalog.SetCurrent(oldCatalog)
	a.engine.Catalog = &oldCatalog
	a.engine.BinaryPath = ""
	if old != nil {
		a.engine.BinaryPath = old.Binary
	}
	var rollback error
	if a.store != nil {
		rollback = a.store.SaveSettings(oldSettings)
	}
	rollback = errors.Join(rollback, u.Activate(old))
	if a.ctx.Err() == nil {
		rollback = errors.Join(rollback, a.engine.Start(a.ctx, oldSettings))
	}
	if rollback != nil {
		return fmt.Errorf("%w; restore: %v", err, rollback)
	}
	return err
}

func (a *app) updatesPreferences(c *ui.Context) {
	a.section(c, "PanSou search engine", func() {
		muted(c, a.tr("Current engine: %s", catalog.Current().Revision[:12]))
		ui.Checkbox(c, &a.prefs.CheckEngineUpdates, a.tr("Check for PanSou updates every 30 minutes")).Tooltip(a.tr("Only checks while PanReel is running. Updates require your click and replace the engine without reinstalling the app.")).OnChange(func() {
			a.savePrefs()
			if a.prefs.CheckEngineUpdates {
				a.checkEngineUpdate()
			}
		})
		if a.engineUpdateStatus.key != "" {
			muted(c, a.engineUpdateStatus.text(a))
		}
		if a.engineUpdating && a.engineTotal > 0 {
			muted(c, a.tr("Downloading… %d%%", min(a.engineDownloaded, a.engineTotal)*100/a.engineTotal))
		}
		ui.Row(c).Gap(8).Children(func() {
			button(c, a.tr("Check PanSou updates")).Disabled(a.engineChecking || a.engineUpdating).OnClick(a.checkEngineUpdate)
			if a.engineAvailable != nil {
				primaryButton(c, a.tr("Update PanSou")).Disabled(a.engineChecking || a.engineUpdating || a.starting || a.loading || a.engineRestartPending || len(a.pendingSettings) > 0 || a.settingsSaveError != "").OnClick(a.installEngineUpdate)
			}
			if a.engineUpdating {
				button(c, a.tr("Cancel")).OnClick(func() {
					if a.engineUpdateCancel != nil {
						a.engineUpdateCancel()
					}
				})
			}
		})
		if a.engineAvailable != nil && a.loading {
			muted(c, a.tr("Finish searching before updating."))
		}
	})
	a.section(c, "PanReel application", func() { a.updatePreferences(c) })
}

func (a *app) engineUpdateNoticeView(c *ui.Context, pal *palette) {
	if a.engineAvailable != nil || a.engineUpdating {
		label := "PanSou update available"
		if a.engineUpdating {
			label = "Updating PanSou…"
		}
		iconButton(c, "engine-update", engineIcon, a.tr(label), pal, func() { a.openPreferencesTab(preferencesUpdates) }).TextColor(pal.accentInk)
	}
}

func (a *app) healthSnapshot(ctx context.Context) search.Health {
	if c := a.engine.Client(); c != nil {
		h, _ := c.Health(ctx)
		return h
	}
	return search.Health{}
}
