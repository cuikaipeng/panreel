package app

import (
	"context"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cuikaipeng/panreel/internal/model"
	"github.com/cuikaipeng/panreel/internal/search"
	"github.com/egoist/mygo/ui"
)

func verificationFixture() search.VerifiedUpdate {
	items := []model.Item{
		{ID: "ok", Title: "Available result", Drive: "quark", URL: "https://pan.quark.cn/s/ok"},
		{ID: "bad", Title: "Expired result", Drive: "baidu", URL: "https://pan.baidu.com/s/bad"},
		{ID: "locked", Title: "Needs a code", Drive: "aliyun", URL: "https://www.alipan.com/s/locked"},
		{ID: "unsupported", Title: "Unsupported result", Drive: "magnet", URL: "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567"},
	}
	checks := map[string]model.Check{}
	for _, item := range items {
		checks[linkCheckKey(item)] = model.Check{State: item.ID, At: time.Now()}
	}
	return search.VerifiedUpdate{
		Items: []model.Item{items[0]}, All: items, Checks: checks,
		Progress: search.VerificationProgress{Found: 4, Checked: 4, Valid: 1, Invalid: 1, Unconfirmed: 2},
	}
}

func TestVerifiedResultsDefaultAndAllResultsRemainAccessible(t *testing.T) {
	for _, lang := range []string{"zh-Hans", "en"} {
		for _, theme := range []string{"light", "dark"} {
			t.Run(lang+"/"+theme, func(t *testing.T) {
				a := newApp()
				a.prefs.Language, a.prefs.Theme = lang, theme
				a.online = true
				a.settings.Channels = []string{"example_channel"}
				a.checkLink = func(context.Context, model.Item) (model.Check, error) {
					t.Error("selection duplicated a fresh background check")
					return model.Check{}, nil
				}
				_, id := a.beginSearch("sample")
				defer a.stopSearch()
				update := verificationFixture()
				a.applyVerifiedBatch(id, update)
				a.loading, a.searchStatus = false, "Search complete"
				tt := ui.NewTester(a.view, 960, 640)
				if len(a.filtered()) != 1 || !tt.HasText("Available result") || tt.HasText("Expired result") {
					t.Fatal("default list leaked unverified links")
				}
				status, ok := tt.AccessByRole(ui.RoleStatus, a.searchStatusLabel())
				if !ok || status.Offscreen || status.Bounds.X+status.Bounds.W > 960 {
					t.Fatal("verification summary is missing or clipped", status.Bounds)
				}
				if err := tt.Click("Available result"); err != nil {
					t.Fatal(err)
				}
				if !tt.HasText(a.tr("链接可访问")) {
					t.Fatal("detail did not reuse the verification result")
				}
				if dir := os.Getenv("PANREEL_UI_ARTIFACTS"); dir != "" {
					if err := os.MkdirAll(dir, 0755); err != nil {
						t.Fatal(err)
					}
					f, err := os.Create(filepath.Join(dir, "verified-"+lang+"-"+theme+".png"))
					if err != nil {
						t.Fatal(err)
					}
					err = png.Encode(f, tt.Image())
					closeErr := f.Close()
					if err != nil || closeErr != nil {
						t.Fatal(err, closeErr)
					}
				}
				if err := tt.Click(a.tr("Valid only")); err != nil {
					t.Fatal(err)
				}
				if err := tt.Click(a.tr("All results")); err != nil {
					t.Fatal(err)
				}
				if len(a.filtered()) != 4 || !tt.HasText("Expired result") || !tt.HasText("Unsupported result") || a.selected != -1 {
					t.Fatal("All results did not expose the excluded candidates")
				}
				a.resultMode = "Valid only"
				a.saved = update.All
				a.navigate("saved")
				tt.Frame()
				if len(a.filtered()) != 4 {
					t.Fatal("verification filter hid saved bookmarks")
				}
			})
		}
	}
}

func TestVerifiedRefreshKeepsRowsAndStaleCallbacksCannotRestoreInvalidLinks(t *testing.T) {
	a := newApp()
	a.settings.Channels = []string{"example_channel"}
	update := verificationFixture()
	a.items, a.resultChecks = update.Items, update.Checks
	a.searched, a.selected = "sample", 0
	_, id := a.beginSearch("sample")
	defer a.stopSearch()
	a.applyVerifiedBatch(id, search.VerifiedUpdate{All: update.All, Checks: map[string]model.Check{}, Progress: search.VerificationProgress{Found: 4, Searching: true}})
	if len(a.items) != 1 || a.selected != 0 {
		t.Fatal("verification wait blanked the previous results")
	}
	a.applyVerifiedBatch(id, update)
	if a.selected != 0 || a.awaitingResults {
		t.Fatal("fresh results lost selection or stayed pending")
	}
	key := linkCheckKey(update.Items[0])
	a.checks[key] = model.Check{State: "bad", At: time.Now().Add(time.Second)}
	a.applyVerifiedBatch(id, verificationFixture())
	if len(a.items) != 0 || a.selected != -1 {
		t.Fatal("background snapshot undid the newer manual verdict")
	}
	a.stopSearch()
	a.applyVerifiedBatch(id, verificationFixture())
	if len(a.items) != 0 || a.loading {
		t.Fatal("late callback from a stopped search was applied")
	}
}
