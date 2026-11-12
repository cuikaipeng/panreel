package app

import (
	"slices"

	"github.com/cuikaipeng/panreel/internal/model"
	"github.com/cuikaipeng/panreel/internal/search"
	"github.com/egoist/mygo/ui"
)

func (a *app) searchCollection() []model.Item {
	if a.resultMode == "All results" && a.allItems != nil {
		return a.allItems
	}
	return a.items
}

func (a *app) selectedResultID() string {
	items := a.filtered()
	if a.selected >= 0 && a.selected < len(items) {
		return items[a.selected].ID
	}
	return ""
}

func (a *app) applyVerifiedBatch(id uint64, update search.VerifiedUpdate) {
	if id != a.searchID || !a.loading {
		return
	}
	a.verification = update.Progress
	if len(update.All) == 0 && a.awaitingResults {
		return // Keep the previous list while the engine starts this request.
	}
	selectedID := ""
	if a.page == "search" {
		selectedID = a.selectedResultID()
	}
	// A manual recheck can finish after the collector's snapshot was made.
	// Its newer verdict must not be undone by a later background callback.
	for key, value := range a.checks {
		if previous, ok := update.Checks[key]; ok && value.At.After(previous.At) {
			update.Checks[key] = value
		}
	}
	update.Items = nil
	update.Progress.Checked, update.Progress.Valid, update.Progress.Invalid, update.Progress.Unconfirmed = 0, 0, 0, 0
	validIDs := make(map[string]bool)
	for _, item := range update.All {
		if result, ok := update.Checks[linkCheckKey(item)]; ok {
			update.Progress.Checked++
			switch result.State {
			case "ok":
				update.Progress.Valid++
				update.Items = append(update.Items, item)
				validIDs[item.ID] = true
			case "bad":
				update.Progress.Invalid++
			default:
				update.Progress.Unconfirmed++
			}
		}
	}
	a.verification = update.Progress
	reset := a.awaitingResults && a.resetSearchPosition && len(update.Items) > 0
	a.resultChecks = update.Checks
	a.applySearchBatch(id, update.Items)
	if !a.awaitingResults {
		// A late extraction code can invalidate an earlier verdict. Replace
		// that verdict without changing the order of existing usable rows.
		a.items = slices.DeleteFunc(a.items, func(item model.Item) bool { return !validIDs[item.ID] })
	}
	a.allItems = update.All
	if a.page == "search" {
		if reset {
			selectedID = ""
		}
		a.selected = slices.IndexFunc(a.filtered(), func(item model.Item) bool { return selectedID != "" && item.ID == selectedID })
		a.checkSelectedResult()
	}
}

func (a *app) searchStatusLabel() string {
	p := a.verification
	if p.Found == 0 {
		return a.tr(a.searchStatus)
	}
	if a.loading {
		if p.Searching {
			return a.tr("Searching · checked %d/%d", p.Checked, p.Found)
		}
		return a.tr("Checking links · %d/%d", p.Checked, p.Found)
	}
	if a.searchStatus == "Search complete" && (p.Invalid > 0 || p.Unconfirmed > 0) {
		return a.tr("Complete · %d expired · %d unconfirmed", p.Invalid, p.Unconfirmed)
	}
	return a.tr(a.searchStatus)
}

func (a *app) changeResultMode() {
	a.clearSelection()
	a.list = ui.ListState{}
}

func (a *app) cachedLinkCheck(item model.Item) (model.Check, bool) {
	key := linkCheckKey(item)
	result, found := a.resultChecks[key]
	if other, ok := a.checks[key]; ok && (!found || other.At.After(result.At)) {
		return other, true
	}
	return result, found
}

func (a *app) searchCheckPending(item model.Item) bool {
	if !a.loading || !search.CanCheck(item.Drive) {
		return false
	}
	key := linkCheckKey(item)
	if _, ok := a.resultChecks[key]; ok {
		return false
	}
	return slices.ContainsFunc(a.allItems, func(value model.Item) bool {
		return value.ID == item.ID && linkCheckKey(value) == key
	})
}
