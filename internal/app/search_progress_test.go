package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/cuikaipeng/panreel/internal/model"
	"github.com/cuikaipeng/panreel/internal/search"
	"github.com/egoist/mygo/ui"
)

func TestRepeatSearchKeepsVisibleRowsSelectionAndLayout(t *testing.T) {
	for _, lang := range []string{"zh-Hans", "en"} {
		for _, theme := range []string{"light", "dark"} {
			t.Run(lang+"/"+theme, func(t *testing.T) {
				a := newApp()
				a.prefs.Language, a.prefs.Theme = lang, theme
				a.settings.Channels = []string{"example"}
				a.online, a.query, a.searched = true, "test", "test"
				for i := range 40 {
					a.items = append(a.items, model.Item{ID: fmt.Sprint(i), Title: fmt.Sprintf("Result %d", i), Drive: "quark"})
				}
				tt := ui.NewTester(a.view, 960, 640)
				a.list.ScrollTo(12, ui.Start)
				tt.Frame()
				if err := tt.Click("Result 12"); err != nil {
					t.Fatal(err)
				}
				row, ok := tt.AccessByRole(ui.RoleListItem, "Result 12")
				if !ok || !row.Selected || row.Offscreen {
					t.Fatal("selected result is not visible")
				}
				input, _ := tt.Access(a.tr("搜索片名或关键词"))
				action, _ := tt.Access(a.tr("搜索"))
				_, id := a.beginSearch("test")
				defer a.stopSearch()
				assertStable := func() {
					t.Helper()
					tt.Frame()
					current, ok := tt.AccessByRole(ui.RoleListItem, "Result 12")
					field, _ := tt.Access(a.tr("搜索片名或关键词"))
					stop, _ := tt.Access(a.tr("停止搜索"))
					if !ok || current.Offscreen || !current.Selected || current.Bounds != row.Bounds || field.Bounds != input.Bounds || stop.Bounds != action.Bounds {
						t.Fatalf("refresh moved or removed the results/input/action: row=%+v field=%+v action=%+v", current.Bounds, field.Bounds, stop.Bounds)
					}
				}
				assertStable()
				a.applySearchBatch(id, nil)
				assertStable()
				fresh := append(slices.Clone(a.items), model.Item{ID: "late", Title: "Late result"})
				a.applySearchBatch(id, fresh)
				assertStable()
				if len(a.items) != 41 {
					t.Fatal("new results were not applied")
				}
			})
		}
	}
}

func TestNewSearchReplacesPreviousResultsWithoutMixingBatches(t *testing.T) {
	a := newApp()
	a.settings.Channels = []string{"example"}
	a.searched = "previous"
	a.items = []model.Item{{ID: "old", Title: "Old result"}}
	a.selected = 0
	_, id := a.beginSearch("new query")
	defer a.stopSearch()
	a.applySearchBatch(id-1, []model.Item{{ID: "stale"}})
	a.applySearchBatch(id, nil)
	if len(a.items) != 1 || a.items[0].ID != "old" || a.selected != 0 {
		t.Fatal("waiting for the new query removed the displayed result")
	}
	a.applySearchBatch(id, []model.Item{{ID: "fresh", Title: "New result"}})
	if len(a.items) != 1 || a.items[0].ID != "fresh" || a.selected != -1 {
		t.Fatal("new query was mixed with the previous query or kept a stale selection")
	}
	a.applySearchBatch(id, []model.Item{{ID: "later", Title: "Later result"}})
	if len(a.items) != 2 || a.items[0].ID != "fresh" {
		t.Fatal("later batches replaced this search's existing results")
	}
}

func TestRefreshWithoutNewResultsFinishesEmptyOrKeepsPreviousOnFailure(t *testing.T) {
	for _, outcome := range []string{"complete", "timeout", "error", "cancel", "superseded"} {
		t.Run(outcome, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				switch outcome {
				case "timeout":
					<-r.Context().Done()
				case "error":
					w.WriteHeader(http.StatusServiceUnavailable)
				default:
					fmt.Fprint(w, `{"data":{"pending":false,"total":0}}`)
				}
			}))
			defer server.Close()
			a := newApp()
			a.settings.Channels = []string{"example"}
			a.items = []model.Item{{ID: "old", Title: "Previous result"}}
			ctx, id := a.beginSearch("test")
			defer a.stopSearch()
			switch outcome {
			case "cancel":
				a.stopSearch()
			case "superseded":
				a.beginSearch("new query")
			case "timeout":
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 50*time.Millisecond)
				defer cancel()
			}
			a.collectSearch(ctx, id, search.NewClient(server.URL, "secret"), "test", a.settings, false, nil)
			if outcome == "complete" {
				if len(a.items) != 0 || a.loading || a.awaitingResults {
					t.Fatal("completed empty search left stale results")
				}
			} else if len(a.items) != 1 || a.items[0].ID != "old" || a.loading != (outcome == "superseded") {
				t.Fatal("failed/cancelled refresh dropped results or old completion stopped the new search")
			}
		})
	}
}

func TestLiveResultsKeepSelectionAndIgnoreStoppedOrOldSearch(t *testing.T) {
	a := newApp()
	a.settings.Channels = []string{"example"}
	a.loading, a.searched, a.searchID, a.searchStatus = true, "test", 1, "Searching…"
	a.items = []model.Item{{ID: "first", Drive: "quark", Title: "First result", PublishedAt: time.Unix(100, 0)}}
	a.selected, a.searchFilters.sort = 0, "最新分享"
	tt := ui.NewTester(a.view, 960, 640)
	if _, ok := tt.AccessByRole(ui.RoleStatus, a.tr("Searching…")); !ok {
		t.Fatal("search status missing")
	}
	if _, ok := tt.Access("读取补齐结果"); ok {
		t.Fatal("manual background refresh still present")
	}
	a.applySearchBatch(1, []model.Item{{ID: "late", Drive: "quark", Title: "Late result", PublishedAt: time.Unix(200, 0)}})
	tt.Frame()
	if len(a.items) != 2 || a.filtered()[a.selected].ID != "first" || !tt.HasText("Late result") {
		t.Fatal("new batch lost selection or did not appear")
	}
	a.applySearchBatch(0, []model.Item{{ID: "stale"}})
	if len(a.items) != 2 {
		t.Fatal("previous search contaminated current results")
	}
	a.page, a.selected = "saved", 3
	a.applySearchBatch(1, []model.Item{{ID: "third"}})
	if a.selected != 3 || a.page != "saved" {
		t.Fatal("background update changed favorites navigation or selection")
	}
	a.page = "search"
	a.stopSearch()
	a.applySearchBatch(1, []model.Item{{ID: "after-stop"}})
	tt.Frame()
	if a.loading || len(a.items) != 3 || !tt.HasText(a.tr("Search stopped")) {
		t.Fatal("stop erased results or accepted a late callback")
	}
}

func TestSearchCollectorFinishesOrTimesOutWithoutDroppingResults(t *testing.T) {
	for _, outcome := range []string{"complete", "timeout", "error", "superseded"} {
		t.Run(outcome, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				if r.URL.Path == "/api/check/links" {
					fmt.Fprint(w, `{"results":[{"state":"ok"}]}`)
					return
				}
				if r.URL.Path == "/api/search" {
					fmt.Fprint(w, `{"data":{"merged_by_type":{"quark":[{"url":"https://pan.quark.cn/s/first"}]}}}`)
					return
				}
				switch outcome {
				case "timeout":
					<-r.Context().Done()
				case "error":
					w.WriteHeader(http.StatusServiceUnavailable)
				default:
					fmt.Fprint(w, `{"data":{"pending":false,"total":0}}`)
				}
			}))
			defer server.Close()
			a := newApp()
			a.settings.Channels = []string{"example"}
			a.loading, a.searchID = true, 1
			if outcome == "superseded" {
				a.searchID, a.searchStatus = 2, "Searching…"
			}
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()
			a.collectSearch(ctx, 1, search.NewClient(server.URL, "secret"), "test", a.settings, false, nil)
			if outcome == "superseded" {
				if !a.loading || len(a.items) != 0 || a.searchStatus != "Searching…" {
					t.Fatal("old completion overwrote new search")
				}
				return
			}
			want := map[string]string{"complete": "Search complete", "timeout": "Search timed out", "error": "Search interrupted"}[outcome]
			if a.loading || len(a.items) != 1 || a.searchStatus != want {
				t.Fatal("incorrect terminal state", a.loading, a.items, a.searchStatus)
			}
		})
	}
}
