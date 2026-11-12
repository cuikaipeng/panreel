package app

import (
	"context"
	"errors"
	"fmt"
	"github.com/cuikaipeng/panreel/internal/model"
	"github.com/egoist/mygo/ui"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMinimumWindowLayoutsWithoutAppShortcuts(t *testing.T) {
	for _, lang := range []string{"zh-Hans", "en"} {
		for _, theme := range []string{"light", "dark"} {
			t.Run(lang+"/"+theme, func(t *testing.T) {
				a := newApp()
				a.prefs.Language = lang
				a.prefs.Theme = theme
				a.online = true
				a.settings.Channels = []string{"example_channel"}
				a.searched = "sample"
				a.items = []model.Item{{ID: "one", Title: "Sample result", Drive: "quark", URL: "https://pan.quark.cn/s/example", Quality: "4K", Year: "2026", Sources: []string{"plugin:test"}}}
				tt := ui.NewTester(a.view, 960, 640)
				visible := func(name string) {
					t.Helper()
					info, ok := tt.Access(name)
					if !ok || info.Offscreen || info.Bounds.X < 0 || info.Bounds.X+info.Bounds.W > 961 || info.Bounds.Y+info.Bounds.H > 641 {
						t.Fatalf("%q missing or clipped: %+v", name, info)
					}
				}
				for _, key := range []string{"搜索来源范围", "网盘筛选", "结果排序", "Sample result"} {
					visible(a.tr(key))
				}
				if err := tt.Click("Sample result"); err != nil {
					t.Fatal(err)
				}
				tt.Scroll(820, 520, 0, 400)
				visible(a.tr("加入收藏"))
				a.navigate("sources")
				tt.Frame()
				if _, ok := tt.Access(a.tr("保存并应用")); ok {
					t.Fatal("manual save button remains")
				}
				a.sourceTab = sourceTabChannels
				tt.Frame()
				for _, key := range []string{"添加 Telegram 频道", "添加频道", "全选频道"} {
					visible(a.tr(key))
				}
				for _, key := range []ui.Key{ui.KeyK, ui.KeyF, ui.KeyB, ui.Key1, ui.KeyR, ui.KeyS} {
					tt.Key(ui.Cmd, key)
				}
				if a.page != "sources" {
					t.Fatal("removed shortcuts changed application state")
				}
				if _, ok := tt.Access(a.tr("Command palette")); ok {
					t.Fatal("command palette still exists")
				}
				for _, tab := range preferenceTabs {
					if tab == "Shortcuts" {
						t.Fatal("shortcut settings still exist")
					}
				}
				for tab := range preferenceTabs {
					a.prefsTab = tab
					prefs := ui.NewTester(a.preferencesView, 760, 560)
					if tab == 2 {
						if err := prefs.Click(a.tr(model.ProxySystem)); err != nil {
							t.Fatal(err)
						}
						if err := prefs.Click(a.tr(model.ProxyManual)); err != nil {
							t.Fatal(err)
						}
						info, ok := prefs.AccessByRole(ui.RoleTextField, a.tr("代理地址"))
						if !ok || !info.Password || info.Offscreen {
							t.Fatal("proxy field not protected or accessible")
						}
					}
				}
			})
		}
	}
}

func TestLocaleSwitchKeepsSelectionsAndMessages(t *testing.T) {
	a := newApp()
	a.searchFilters.drive = "115 网盘"
	a.sourceChoice = "仅 Telegram"
	a.prefs.Language = "zh-Hans"
	a.noticef("Added %d channels", 42)
	if a.tr(a.message, a.messageArgs...) != "已添加 42 个频道" {
		t.Fatal("status not translated to Chinese")
	}
	a.prefs.Language = "en"
	if a.tr(a.message, a.messageArgs...) != "Added 42 channels" {
		t.Fatal("status not translated from stored arguments")
	}
	if a.searchFilters.drive != "115 网盘" || a.sourceChoice != "仅 Telegram" {
		t.Fatal("locale changed filter values")
	}
	for key := range dictionaries["en"] {
		if dictionaries["zh-Hans"][key] == "" {
			t.Fatalf("missing Chinese translation %q", key)
		}
	}
}

func TestUpdateCheckDownloadFailureAndShutdown(t *testing.T) {
	oldFind := findAppUpdate
	defer func() { findAppUpdate = oldFind }()
	queue := make(chan func(), 10)
	a := newApp()
	a.dispatch = func(fn func()) { queue <- fn }
	drain := func() {
		t.Helper()
		select {
		case fn := <-queue:
			fn()
		case <-time.After(3 * time.Second):
			t.Fatal("missing update callback")
		}
	}
	findAppUpdate = func(context.Context) (*appUpdate, error) {
		return &appUpdate{version: "0.4.0", prepare: func(context.Context, func(int64, int64)) error { return errors.New("signature mismatch") }}, nil
	}
	a.checkUpdates()
	drain()
	if a.availableUpdate == nil || a.updateChecking {
		t.Fatal("update not offered")
	}
	a.downloadUpdate()
	drain()
	if a.updateReady || a.updateDownloading || a.updateErr == "" {
		t.Fatal("failed verification became installable")
	}
	a.availableUpdate.prepare = func(context.Context, func(int64, int64)) error { return nil }
	a.downloadUpdate()
	drain()
	if !a.updateReady {
		t.Fatal("successful preparation not ready")
	}
	a.updateReady = false
	findAppUpdate = func(context.Context) (*appUpdate, error) {
		return nil, fmt.Errorf("network: %w", context.DeadlineExceeded)
	}
	a.checkUpdates()
	drain()
	if a.updateStatus.key != "The update server could not be reached. Check your network and try again." {
		t.Fatal("timeout has no actionable message")
	}
	a.updateReady = false
	a.checkUpdates()
	a.closed = true
	drain()
	if !a.updateChecking {
		t.Fatal("late callback mutated closed UI")
	}
}

// Optional visual artifacts use the same native views as the shipped program.
func TestRenderDesktopArtifacts(t *testing.T) {
	dest := os.Getenv("PANREEL_UI_ARTIFACTS")
	if dest == "" {
		t.Skip("set PANREEL_UI_ARTIFACTS to save native UI renders")
	}
	if err := os.MkdirAll(dest, 0755); err != nil {
		t.Fatal(err)
	}
	for _, lang := range []string{"zh-Hans", "en"} {
		for _, theme := range []string{"light", "dark"} {
			a := newApp()
			a.prefs.Language = lang
			a.prefs.Theme = theme
			a.online = true
			for _, page := range []string{"search", "sources", "history", "saved"} {
				a.page = page
				tt := ui.NewTester(a.view, 960, 640)
				f, err := os.Create(filepath.Join(dest, fmt.Sprintf("%s-%s-%s.png", page, lang, theme)))
				if err != nil {
					t.Fatal(err)
				}
				err = png.Encode(f, tt.Image())
				f.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			for tab := range preferenceTabs {
				a.prefsTab = tab
				tt := ui.NewTester(a.preferencesView, 760, 560)
				f, err := os.Create(filepath.Join(dest, fmt.Sprintf("preferences-%d-%s-%s.png", tab, lang, theme)))
				if err != nil {
					t.Fatal(err)
				}
				err = png.Encode(f, tt.Image())
				f.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}
