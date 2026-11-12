package app

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/cuikaipeng/panreel/internal/model"
	"github.com/cuikaipeng/panreel/internal/search"
	"github.com/egoist/mygo/ui"
)

func linkCheckKey(item model.Item) string {
	// Late search results can supply an extraction code. That needs a fresh
	// check, even when the link ID and selected row stay the same.
	return search.CheckKey(item)
}

func (a *app) cancelLinkCheck() {
	a.checkSequence++
	if a.checkCancel != nil {
		a.checkCancel()
		a.checkCancel = nil
	}
	a.checkActive = ""
}

func (a *app) checkSelectedResult() {
	if a.page == "search" || a.page == "saved" {
		items := a.filtered()
		if a.selected >= 0 && a.selected < len(items) {
			a.check(items[a.selected], false)
			return
		}
	}
	a.cancelLinkCheck()
}

func (a *app) clearSelection() {
	a.selected = -1
	a.cancelLinkCheck()
}

func (a *app) check(item model.Item, force bool) {
	key := linkCheckKey(item)
	if a.checkActive == key {
		return
	}
	a.cancelLinkCheck()
	if !search.CanCheck(item.Drive) || !a.online || a.starting || a.engineUpdating {
		return
	}
	if !force && a.searchCheckPending(item) {
		return // The search already owns this request.
	}
	if cached, ok := a.cachedLinkCheck(item); ok && !force && search.FreshCheck(cached) {
		return
	}
	check := a.checkLink
	if check == nil {
		if a.engine == nil || a.engine.Client() == nil {
			return
		}
		check = a.engine.Client().Check
	}
	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
	a.checkCancel, a.checkActive = cancel, key
	sequence := a.checkSequence
	go func() {
		defer cancel()
		result, err := check(ctx, item)
		a.update(func() {
			if sequence != a.checkSequence {
				return // A slower previous selection must not replace the new one.
			}
			a.checkCancel, a.checkActive = nil, ""
			if errors.Is(err, context.Canceled) {
				return
			}
			if err != nil {
				result = model.Check{State: "uncertain", Summary: "Link check failed. Try again later."}
				if errors.Is(err, context.DeadlineExceeded) {
					result.Summary = "Link check timed out. Try again later."
				}
			}
			result.At = time.Now()
			if len(a.checks) >= 256 {
				var oldest string
				for k, value := range a.checks {
					if oldest == "" || value.At.Before(a.checks[oldest].At) {
						oldest = k
					}
				}
				delete(a.checks, oldest)
			}
			a.checks[key] = result
			if _, ok := a.resultChecks[key]; ok {
				selectedID := a.selectedResultID()
				a.resultChecks[key] = result
				if result.State != "ok" {
					a.items = slices.DeleteFunc(a.items, func(value model.Item) bool { return linkCheckKey(value) == key })
				} else {
					a.items = search.MergeItems(a.items, []model.Item{item})
				}
				a.selected = slices.IndexFunc(a.filtered(), func(value model.Item) bool { return selectedID != "" && value.ID == selectedID })
			}
		})
	}()
}

func (a *app) linkCheckView(c *ui.Context, item model.Item) {
	key := linkCheckKey(item)
	ui.Column(c).FillWidth().Gap(7).Children(func() {
		if !search.CanCheck(item.Drive) {
			muted(c, a.tr("不支持自动检测"))
			return
		}
		if a.checkActive == key || a.searchCheckPending(item) {
			ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c).Size(14, 14)
				ui.Text(c, a.tr("正在检测…")).FontSize(13).TextColor(c.Theme().TextMuted).Role(ui.RoleStatus)
			})
			return
		}
		if check, ok := a.cachedLinkCheck(item); ok {
			color := c.Theme().TextMuted
			if check.State == "ok" {
				color = a.paletteFor(c).success
			} else if check.State == "bad" {
				color = c.Theme().Danger
			}
			ui.Text(c, a.tr(stateTitle(check.State))).FontSize(13).FontWeight(550).TextColor(color).Role(ui.RoleStatus)
			if check.Summary != "" {
				muted(c, a.tr(check.Summary))
			}
		} else if !a.online {
			muted(c, a.tr("Waiting for the engine to check this link"))
		} else {
			muted(c, a.tr("尚未检测"))
		}
		button(c, a.tr("Recheck link")).FillWidth().Disabled(!a.online || a.starting || a.engineUpdating).OnClick(func() { a.check(item, true) })
	})
}
