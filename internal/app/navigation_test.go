package app

import (
	"testing"

	"github.com/cuikaipeng/panreel/internal/model"
	"github.com/egoist/mygo/ui"
)

func TestPageSwitchPreservesIndependentResultFilters(t *testing.T) {
	for _, lang := range []string{"zh-Hans", "en"} {
		t.Run(lang, func(t *testing.T) {
			a := newApp()
			a.prefs.Language = lang
			a.settings.Channels = []string{"example_channel"}
			a.searched = "sample"
			a.items = []model.Item{
				{ID: "ali", Drive: "aliyun", Title: "Ali result", Quality: "4K", Year: "2026", Subtitle: "中文字幕"},
				{ID: "quark", Drive: "quark", Title: "Quark result", Quality: "1080P", Year: "2025", Subtitle: "中英字幕"},
			}
			a.saved = a.items
			tt := ui.NewTester(a.view, 960, 640)
			click := func(label string) {
				t.Helper()
				if err := tt.Click(a.tr(label)); err != nil {
					t.Fatal(err)
				}
			}
			choose := func(before, after string) {
				t.Helper()
				click(before)
				option, ok := tt.AccessByRole(ui.RoleOption, a.tr(after))
				if !ok || option.Offscreen {
					t.Fatal("filter option missing", after)
				}
				tt.ClickAt(option.Bounds.X+option.Bounds.W/2, option.Bounds.Y+option.Bounds.H/2)
			}
			assertResult := func(id string) {
				t.Helper()
				if items := a.filtered(); len(items) != 1 || items[0].ID != id {
					t.Fatalf("%s page has wrong filtered results: %+v", a.page, items)
				}
			}

			choose("全部网盘", "阿里云盘")
			choose("全部画质", "4K")
			choose("全部年份", "2026")
			choose("全部字幕", "含中文字幕")
			choose("默认排序", "最新分享")
			searchFilters := a.searchFilters
			assertResult("ali")
			click("收藏夹")
			if a.savedFilters != defaultResultFilters() || len(a.filtered()) != 2 {
				t.Fatal("search filters leaked into favorites")
			}
			choose("全部网盘", "夸克网盘")
			choose("全部画质", "1080P")
			choose("全部年份", "2025")
			choose("全部字幕", "中英字幕")
			savedFilters := a.savedFilters
			assertResult("quark")

			click("探索")
			if a.searchFilters != searchFilters {
				t.Fatal("returning from favorites reset search filters")
			}
			assertResult("ali")
			for _, page := range []string{"搜索历史", "搜索来源"} {
				click(page)
				click("探索")
				if a.searchFilters != searchFilters {
					t.Fatal("navigation reset search filters", page)
				}
				assertResult("ali")
			}
			click("收藏夹")
			if a.savedFilters != savedFilters {
				t.Fatal("favorites did not retain its own filters")
			}
			assertResult("quark")
			choose("2025", "2026") // No Quark bookmark matches this year.
			click("清除筛选")
			if a.savedFilters != defaultResultFilters() || a.searchFilters != searchFilters {
				t.Fatal("clearing favorites filters changed search filters")
			}

			// Searching from another page bypasses navigate; it must still use
			// search filters rather than the favorites controls last rendered.
			choose("全部网盘", "夸克网盘")
			_, _ = a.beginSearch("another query")
			defer a.stopSearch()
			if a.page != "search" || *a.activeResultFilters() != searchFilters || a.savedFilters.drive != "夸克网盘" {
				t.Fatal("starting a search used or overwrote favorites filters")
			}
		})
	}
}
