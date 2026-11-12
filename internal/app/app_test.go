package app

import (
	"github.com/cuikaipeng/panreel/internal/catalog"
	"github.com/cuikaipeng/panreel/internal/model"
	"github.com/cuikaipeng/panreel/internal/storage"
	"github.com/egoist/mygo/ui"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestNativeNavigationAndSourceSelection(t *testing.T) {
	a := newApp()
	tt := ui.NewTester(a.view, 1040, 680)
	click := func(name string) {
		t.Helper()
		if err := tt.Click(name); err != nil {
			t.Fatal(err)
		}
	}
	click("搜索来源")
	if a.page != "sources" {
		t.Fatal("navigation did not change page")
	}
	click(a.tr("搜索插件 · %d", len(catalog.Current().Plugins)))
	click("清空选择")
	if len(a.draft.Plugins) != 0 || len(a.settings.Plugins) != 0 {
		t.Fatal("selection not cleared")
	}
	click("全选插件")
	if len(a.draft.Plugins) != len(catalog.Current().Plugins) {
		t.Fatal("selection incomplete")
	}
	id := catalog.PluginIDs()[0]
	click("启用插件 " + id)
	if slices.Contains(a.draft.Plugins, id) {
		t.Fatal("checkbox did not update draft")
	}
	click("配置插件 " + id)
	if a.selectedPlugin != id {
		t.Fatal("plugin detail not selected")
	}
	a.sourceTab = sourceTabChannels
	tt.Frame()
	click("添加 Telegram 频道")
	tt.Type("@example_channel")
	click("添加频道")
	if !slices.Contains(a.draft.Channels, "example_channel") {
		t.Fatal("custom channel not added")
	}
	setSelected(&a.draft.Channels, "example_channel", false)
	if !slices.Contains(a.allChannels(), "example_channel") {
		t.Fatal("disabled custom channel disappeared")
	}
	click("偏好设置")
	if !a.prefsOpen {
		t.Fatal("preferences window did not open")
	}
	a.prefsTab = 2
	prefs := ui.NewTester(a.preferencesView, 840, 650)
	tt = prefs
	if _, ok := tt.AccessByRole(ui.RoleTextField, "代理地址"); ok {
		t.Fatal("system proxy should not ask for an address")
	}
	click(a.tr(model.ProxySystem))
	click("手动配置")
	input, ok := tt.AccessByRole(ui.RoleTextField, "代理地址")
	if !ok || input.Offscreen {
		t.Fatal("proxy input missing or clipped")
	}
	tt.ClickAt(input.Bounds.X+10, input.Bounds.Y+input.Bounds.H/2)
	tt.Type("http://127.0.0.1:7890")
	if a.draft.Proxy != "http://127.0.0.1:7890" {
		t.Fatal("proxy input not bound", a.draft.Proxy)
	}
	a.flushSettings()
	if a.settings.Proxy != a.draft.Proxy || a.settings.EffectiveProxyMode() != model.ProxyManual {
		t.Fatal("proxy did not save automatically")
	}
	tt = ui.NewTester(a.view, 1040, 680)
	click("搜索历史")
	click("收藏夹")
	click("探索")
	if a.page != "search" {
		t.Fatal("navigation failed")
	}
}
func TestNativeResultSelectionBookmarkAndFilters(t *testing.T) {
	a := newApp()
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	a.store = store
	a.settings.Channels = []string{"example_channel"}
	a.searched = "测试"
	a.items = []model.Item{{ID: "one", Drive: "quark", Title: "原生 UI 测试影片", URL: "https://pan.quark.cn/s/example", Quality: "4K", Year: "2024", Sources: []string{"plugin:test"}}, {ID: "two", Drive: "baidu", Title: "第二条结果", URL: "https://pan.baidu.com/s/example", Quality: "1080P", Sources: []string{"tg:example"}}}
	tt := ui.NewTester(a.view, 1040, 680)
	if err := tt.Click("原生 UI 测试影片"); err != nil {
		t.Fatal(err)
	}
	if a.selected != 0 {
		t.Fatal("result selection failed", a.selected)
	}
	// The detail scroll must expose the real action at the minimum window size.
	tt.Scroll(850, 550, 0, 350)
	if err := tt.Click("加入收藏"); err != nil {
		t.Fatal(err)
	}
	if len(a.saved) != 1 {
		t.Fatal("bookmark missing")
	}
	a.searchFilters.quality = "4K"
	if got := a.filtered(); len(got) != 1 || got[0].ID != "one" {
		t.Fatal(got)
	}
	a.navigate("saved")
	tt.Frame()
	if len(a.filtered()) != 1 {
		t.Fatal("saved view did not use its own filters")
	}
	a.savedQuery = "不匹配"
	if len(a.filtered()) != 0 {
		t.Fatal("saved query filter failed")
	}
	a.prefs.Theme = "dark"
	tt.Frame()
}

func TestSearchValidationStaysBesideInput(t *testing.T) {
	for _, lang := range []string{"zh-Hans", "en"} {
		for _, theme := range []string{"light", "dark"} {
			for _, tc := range []struct {
				name, query, errorKey string
				enter                 bool
			}{
				{"empty-click", "", "请输入片名或关键词", false},
				{"whitespace-enter", " \u3000 ", "请输入片名或关键词", true},
				{"long-click", strings.Repeat("片", 101), "搜索关键词最多 100 字", false},
			} {
				t.Run(lang+"/"+theme+"/"+tc.name, func(t *testing.T) {
					a := newApp()
					a.prefs.Language, a.prefs.Theme = lang, theme
					a.online = true
					a.settings.Channels = []string{"example_channel"}
					a.query, a.searched = tc.query, "previous"
					a.items = []model.Item{{ID: "previous", Title: "Previous result"}}
					a.selected = 0
					store, err := storage.Open(t.TempDir())
					if err != nil {
						t.Fatal(err)
					}
					defer store.Close()
					a.store = store
					tt := ui.NewTester(a.view, 960, 640)
					field := a.tr("搜索片名或关键词")
					if tc.enter {
						if err := tt.Click(field); err != nil {
							t.Fatal(err)
						}
						tt.Key(0, ui.KeyEnter)
					} else if err := tt.Click(a.tr("搜索")); err != nil {
						t.Fatal(err)
					}
					input, ok := tt.Access(field)
					if !ok || !input.Invalid || !tt.Focused(field) || !slices.Contains(input.ErrorMessage, a.tr(tc.errorKey)) {
						t.Fatalf("input did not expose and focus its error: %+v", input)
					}
					alert, ok := tt.AccessByRole(ui.RoleAlert, a.tr(tc.errorKey))
					// SearchField exposes the inner text area's bounds, without its padding.
					if !ok || alert.Offscreen || alert.Bounds.Y < input.Bounds.Y+input.Bounds.H || alert.Bounds.Y > input.Bounds.Y+input.Bounds.H+48 {
						t.Fatalf("error not visible directly below input: input=%+v alert=%+v", input.Bounds, alert.Bounds)
					}
					history, err := store.History()
					if err != nil || len(history) != 0 || a.loading || a.searched != "previous" || len(a.items) != 1 || a.selected != 0 || a.message != "" {
						t.Fatal("invalid submission changed results/history or produced a remote notice", err)
					}
					tt.Key(ui.Cmd, ui.KeyA)
					tt.Type("纪录片")
					input, _ = tt.Access(field)
					if a.queryError != "" || input.Invalid || len(input.ErrorMessage) != 0 || tt.HasText(a.tr(tc.errorKey)) {
						t.Fatal("correcting the input did not clear its error")
					}
				})
			}
		}
	}
}

func TestOperationNoticeExpiresWithoutInteraction(t *testing.T) {
	a := newApp()
	a.notice("已加入收藏夹")
	tt := ui.NewTester(a.view, 960, 640)
	if !tt.HasText(a.tr(a.message)) {
		t.Fatal("operation feedback missing")
	}
	tt.Frame()
	tt.Advance(8 * time.Second)
	tt.Frame()
	if tt.HasText(a.tr(a.message)) {
		t.Fatal("notice lingered or was replayed after its timeout")
	}
	a.notice("已加入收藏夹")
	tt.Frame()
	if !tt.HasText(a.tr(a.message)) {
		t.Fatal("repeating an action did not show fresh feedback")
	}
}

func TestFirstLaunchChannelsSaveAutomatically(t *testing.T) {
	for _, lang := range []string{"zh-Hans", "en"} {
		t.Run(lang, func(t *testing.T) {
			a := newApp()
			a.prefs.Language = lang
			a.online = true
			store, err := storage.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			a.store = store
			tt := ui.NewTester(a.view, 960, 640)
			click := func(key string) {
				t.Helper()
				if err := tt.Click(a.tr(key)); err != nil {
					t.Fatal(err)
				}
			}
			searchDisabled := func(want bool) {
				t.Helper()
				info, ok := tt.AccessByRole(ui.RoleButton, a.tr("搜索"))
				if !ok || info.Offscreen || info.Disabled != want {
					t.Fatalf("unexpected search availability: %+v", info)
				}
			}
			searchDisabled(true)
			click("Configure TG channels")
			if a.sourceTab != sourceTabChannels || len(a.allChannels()) != 0 {
				t.Fatal("first launch contains default channels")
			}
			input, ok := tt.Access(a.tr("添加 Telegram 频道"))
			if !ok || !input.Multiline || input.Offscreen {
				t.Fatal("batch channel input missing or clipped")
			}
			click("添加 Telegram 频道")
			tt.Type("@Channel_One, @channel_two，https://t.me/channel_one\nhttps://t.me/s/channel_three")
			click("添加频道")
			want := []string{"channel_one", "channel_two", "channel_three"}
			if !slices.Equal(a.draft.Channels, want) || !slices.Equal(a.settings.Channels, want) {
				t.Fatal("batch was not deduplicated or automatically saved", a.draft.Channels)
			}
			got, err := store.Settings()
			if err != nil || !slices.Equal(got.Channels, want) || len(got.Plugins) != 75 || len(a.pendingSettings) != 0 {
				t.Fatal("configured channels or default plugins not saved", err)
			}
			click("探索")
			searchDisabled(false)
			click("搜索来源")
			click("清空选择")
			click("探索")
			searchDisabled(true)
			// History, keyboard submit and command actions share this guard.
			a.runSearch("a previous query", false)
			if a.page != "sources" || a.sourceTab != sourceTabChannels || a.loading || a.searched != "" {
				t.Fatal("search action bypassed channel setup")
			}
			history, err := store.History()
			if err != nil || len(history) != 0 {
				t.Fatal("blocked search was written to history", err)
			}
			tt.Frame()
			for _, id := range want {
				click(a.tr("Remove channel ") + id)
			}
			got, err = store.Settings()
			if err != nil || len(got.Channels) != 0 || len(got.CustomChannels) != 0 || len(a.allChannels()) != 0 {
				t.Fatal("removed channels returned", err)
			}
		})
	}
}

func TestChannelBatchesAreAtomicAndDeduplicated(t *testing.T) {
	a := newApp()
	a.newChannels = "@Channel_One,@channel_two"
	a.addChannels()
	a.newChannels = "https://t.me/CHANNEL_ONE, https://t.me/s/channel_two"
	a.addChannels()
	if len(a.draft.Channels) != 2 || len(a.allChannels()) != 2 {
		t.Fatal("duplicates across batches were retained")
	}
	a.newChannels = "valid_channel,https://t.me/+private"
	a.addChannels()
	if len(a.draft.Channels) != 2 || len(a.allChannels()) != 2 || len(a.pendingSettings) != 0 || a.newChannels == "" {
		t.Fatal("invalid batch changed existing channels or discarded input")
	}
}
