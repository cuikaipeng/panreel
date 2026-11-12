package app

import (
	"slices"
	"testing"

	"github.com/cuikaipeng/panreel/internal/catalog"
	"github.com/cuikaipeng/panreel/internal/engine"
	"github.com/cuikaipeng/panreel/internal/model"
	"github.com/egoist/mygo/ui"
)

func TestDriveCheckboxesAutoSaveAndFilterSearchWithoutHidingFavorites(t *testing.T) {
	for _, lang := range []string{"zh-Hans", "en"} {
		for _, theme := range []string{"light", "dark"} {
			t.Run(lang+"/"+theme, func(t *testing.T) {
				a := settingsTestApp(t)
				a.prefs.Language, a.prefs.Theme = lang, theme
				a.settings.Channels = []string{"example_channel"}
				a.restoreDraft()
				a.engine, a.online = &engine.Manager{}, true
				a.navigate("sources")
				tt := ui.NewTester(a.view, 960, 640)
				if err := tt.Click(a.tr("Drive types · %d", len(catalog.Drives))); err != nil {
					t.Fatal(err)
				}
				for _, drive := range catalog.Drives {
					info, ok := tt.AccessByRole(ui.RoleCheckBox, a.tr("Enable type %s", a.tr(drive.Name)))
					if !ok || !info.Checked {
						t.Fatal("type is missing or initially unchecked", drive.ID)
					}
				}
				if err := tt.Click(a.tr("Enable type %s", a.tr("百度网盘"))); err != nil {
					t.Fatal(err)
				}
				stored, err := a.store.Settings()
				if err != nil || stored.AllowsDrive("baidu") || len(stored.EnabledDrives()) != 13 || a.engineRestartPending {
					t.Fatal("drive selection did not save without restart", err)
				}
				a.items = []model.Item{{ID: "a", Drive: "quark", Title: "Keep"}, {ID: "b", Drive: "baidu", Title: "Hide from search"}}
				a.saved = slices.Clone(a.items)
				a.navigate("search")
				if got := a.filtered(); len(got) != 1 || got[0].ID != "a" {
					t.Fatal("disabled drive still shown in search")
				}
				a.navigate("saved")
				if len(a.filtered()) != 2 {
					t.Fatal("search selection hid existing favorites")
				}
				a.navigate("sources")
				tt.Frame()
				if err := tt.Click(a.tr("清空选择")); err != nil {
					t.Fatal(err)
				}
				stored, err = a.store.Settings()
				if err != nil || len(stored.EnabledDrives()) != 0 {
					t.Fatal("empty selection reverted to all", err)
				}
				a.navigate("search")
				tt.Frame()
				search, ok := tt.AccessByRole(ui.RoleButton, a.tr("搜索"))
				if !ok || !search.Disabled {
					t.Fatal("empty selection did not disable search")
				}
				if err := tt.Click(a.tr("Configure drive types")); err != nil {
					t.Fatal(err)
				}
				if a.page != "sources" || a.sourceTab != sourceTabDrives {
					t.Fatal("drive setup action opened wrong page")
				}
				if err := tt.Click(a.tr("Select all types")); err != nil {
					t.Fatal(err)
				}
				if len(a.settings.EnabledDrives()) != len(catalog.Drives) {
					t.Fatal("select all did not apply")
				}
			})
		}
	}
}
