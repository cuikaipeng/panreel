package app

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/cuikaipeng/panreel/internal/model"
	"github.com/cuikaipeng/panreel/internal/watch"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

var watchIntervals = []struct {
	minutes int
	label   string
}{
	{30, "Every 30 minutes"}, {60, "Every hour"}, {180, "Every 3 hours"},
	{360, "Every 6 hours"}, {720, "Every 12 hours"}, {1440, "Every day"},
}

func (a *app) reloadWatches() bool {
	if a.store == nil {
		return false
	}
	watches, err := a.store.WatchSummaries()
	if err != nil {
		a.notice("Could not read update watches.")
		return false
	}
	a.watches = watches
	return true
}

func (a *app) startBookmarkWatches() {
	a.watcher = watch.NewRunner(a.store, func(event watch.Event) {
		a.update(func() { a.receiveWatchEvent(event) })
	})
	a.watchWork.Add(1)
	go func() { defer a.watchWork.Done(); a.watcher.Run(a.ctx) }()
}

func (a *app) receiveWatchEvent(event watch.Event) {
	if event.Checking {
		a.watchChecking = event.Item.ID
		return
	}
	if a.watchChecking == event.Item.ID {
		a.watchChecking = ""
	}
	if event.Err != nil {
		a.notice("Could not save the update check. Will retry.")
		return
	}
	if !a.reloadWatches() {
		return
	}
	w, ok := a.watches[event.Item.ID]
	if event.Updated && ok && w.Enabled && w.Generation == event.Watch.Generation && w.Unread() {
		a.notifyBookmarkUpdate(event.Item, w)
	}
}

func (a *app) configureWatch(item model.Item, on bool, minutes int) {
	if a.store == nil || a.closed || !a.isSaved(item.ID) || !watch.Supported(item.Drive) {
		return
	}
	if err := a.store.ConfigureWatch(item.ID, on, minutes, time.Now()); err != nil {
		a.notice("Could not save update reminders.")
		return
	}
	a.reloadWatches()
	if a.watcher != nil {
		a.watcher.Changed(item.ID)
	}
}

func (a *app) readWatch(id string) {
	if a.store == nil {
		return
	}
	if err := a.store.ReadWatch(id, time.Now()); err != nil {
		a.notice("Could not save update reminders.")
		return
	}
	a.reloadWatches()
}

func (a *app) notifyBookmarkUpdate(item model.Item, w model.BookmarkWatch) {
	title := a.tr("Resource updated")
	body := a.tr("%s: %d new, %d changed files", item.Title, w.Result.Added, w.Result.Changed)
	runes := []rune(body)
	if len(runes) > 200 {
		body = string(runes[:197]) + "…"
	}
	notify := a.watchNotify
	if notify == nil {
		notify = func(ctx context.Context, id, title, body string) error {
			if !mygo.NotificationsSupported() {
				return errors.New("notifications unavailable")
			}
			return mygo.NewNotification(mygo.NotificationOptions{ID: id, Group: "bookmark-updates", Title: title, Body: body}).ShowContext(ctx)
		}
	}
	a.watchWork.Add(1)
	go func() {
		defer a.watchWork.Done()
		err := notify(a.ctx, "bookmark:"+item.ID, title, body)
		a.update(func() {
			if err != nil {
				a.watchNotificationError = "System notifications are unavailable. Updates remain in Favorites."
			} else {
				a.watchNotificationError = ""
			}
		})
	}()
}

func (a *app) openWatchNotification(id string) {
	if !a.isSaved(id) {
		return
	}
	a.navigate("saved")
	items := a.filtered()
	index := slices.IndexFunc(items, func(i model.Item) bool { return i.ID == id })
	if index < 0 {
		a.savedQuery = ""
		a.savedFilters = defaultResultFilters()
		items = a.filtered()
		index = slices.IndexFunc(items, func(i model.Item) bool { return i.ID == id })
	}
	a.selected = index
	if index >= 0 {
		a.list.ScrollTo(index, ui.Center)
	}
	a.checkSelectedResult()
	if a.window != nil {
		a.window.Show()
		a.window.Focus()
	}
	a.refreshViews()
}

func (a *app) bookmarkWatchView(c *ui.Context, item model.Item) {
	ui.Box(c).FillWidth().Height(1).Background(c.Theme().Border)
	if !watch.Supported(item.Drive) {
		muted(c, a.tr("Update reminders are unavailable for this link type."))
		return
	}
	w, ok := a.watches[item.ID]
	if !ok {
		w = model.BookmarkWatch{ItemID: item.ID, IntervalMinutes: 60}
	}
	on := w.Enabled
	ui.Checkbox(c.Key("watch-enabled"), &on, a.tr("Update reminders")).FillWidth().OnChange(func() { a.configureWatch(item, on, w.IntervalMinutes) })
	if !on {
		return
	}
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		label := "Every hour"
		options := make([]string, len(watchIntervals))
		for i, interval := range watchIntervals {
			options[i] = interval.label
			if interval.minutes == w.IntervalMinutes {
				label = interval.label
			}
		}
		a.selectValue(c.Key("watch-interval"), &label, options).Label(a.tr("Update check frequency")).FillWidth().OnChange(func() {
			for _, interval := range watchIntervals {
				if interval.label == label {
					a.configureWatch(item, true, interval.minutes)
					break
				}
			}
		})
		state := "Waiting for first check"
		switch {
		case a.watchChecking == item.ID:
			state = "Checking share contents…"
		case w.Result.Issue != "":
			state = watchIssueText(w.Result.Issue)
		case !w.Result.Initialized:
			state = "Waiting for first check"
		case w.Unread():
			state = "Resource updated"
		default:
			state = "No unread updates"
		}
		ui.Text(c, a.tr(state)).FontSize(13).Wrap()
		if !w.Result.CheckedAt.IsZero() {
			muted(c, a.tr("Last checked: %s", w.Result.CheckedAt.Local().Format("01/02 15:04")))
		}
		if w.Unread() {
			ui.Text(c, a.tr("%d new · %d changed files", w.Result.Added, w.Result.Changed)).FontSize(13).Wrap()
			for i, name := range w.Result.Names {
				if i == 2 {
					break
				}
				muted(c, name)
			}
			button(c, a.tr("Mark as read")).FillWidth().OnClick(func() { a.readWatch(item.ID) })
		}
		button(c, a.tr("Check contents now")).FillWidth().Disabled(a.watchChecking == item.ID || time.Since(w.Result.CheckedAt) < time.Minute).OnClick(func() { a.configureWatch(item, true, w.IntervalMinutes) })
		if a.watchNotificationError != "" {
			muted(c, a.tr(a.watchNotificationError))
		}
	})
}

func watchIssueText(issue string) string {
	switch watch.Issue(issue) {
	case watch.Password:
		return "An extraction code is missing or incorrect."
	case watch.Verification:
		return "The drive requires login or verification; contents could not be checked."
	case watch.Unavailable:
		return "The share is unavailable. Keeping the previous record."
	case watch.Incomplete:
		return "The directory could not be fully read. Will retry later."
	case watch.Protocol:
		return "The drive did not return a readable directory. Will retry later."
	case watch.Unsupported:
		return "Update reminders are unavailable for this link type."
	default:
		return "The check failed. Will retry later."
	}
}
