package app

import (
	"context"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cuikaipeng/panreel/internal/model"
	"github.com/cuikaipeng/panreel/internal/storage"
	"github.com/cuikaipeng/panreel/internal/watch"
	"github.com/egoist/mygo/ui"
)

func TestFavoriteUpdateControlsSaveAndRemainAccessible(t *testing.T) {
	for _, lang := range []string{"zh-Hans", "en"} {
		for _, theme := range []string{"light", "dark"} {
			t.Run(lang+"/"+theme, func(t *testing.T) {
				a := newApp()
				a.prefs.Language = lang
				a.prefs.Theme = theme
				store, err := storage.Open(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				defer store.Close()
				a.store = store
				item := model.Item{ID: "series", Title: "Example series", Drive: "aliyun", URL: "https://www.alipan.com/s/demo", Password: "1234"}
				a.toggleSaved(item)
				a.navigate("saved")
				tt := ui.NewTester(a.view, 960, 640)
				click := func(key string) {
					t.Helper()
					if err := tt.Click(a.tr(key)); err != nil {
						t.Fatal(key, err)
					}
				}
				click("Example series")
				tt.Scroll(820, 520, 0, 500)
				click("Update reminders")
				if !a.watches[item.ID].Enabled || a.watches[item.ID].IntervalMinutes != 60 {
					t.Fatal("toggle was not saved")
				}
				tt.Scroll(820, 520, 0, 500)
				click("Every hour")
				option, ok := tt.AccessByRole(ui.RoleOption, a.tr("Every 3 hours"))
				if !ok || option.Offscreen {
					t.Fatal("interval option not visible")
				}
				tt.ClickAt(option.Bounds.X+option.Bounds.W/2, option.Bounds.Y+option.Bounds.H/2)
				if a.watches[item.ID].IntervalMinutes != 180 {
					t.Fatal("frequency was not saved")
				}
				w := a.watches[item.ID]
				result := model.WatchResult{Initialized: true, CheckedAt: time.Now().Add(-time.Hour), NextCheck: time.Now().Add(time.Hour), UpdatedAt: time.Now().Add(-time.Minute), Added: 2, Names: []string{"Season 1/Episode 09.mkv", "Season 1/Episode 10.mkv"}}
				if ok, err := store.SaveWatchResult(w, result); !ok || err != nil {
					t.Fatal(ok, err)
				}
				a.reloadWatches()
				tt.Frame()
				tt.Scroll(820, 520, 0, 500)
				for _, key := range []string{"Check contents now", "Mark as read"} {
					info, ok := tt.Access(a.tr(key))
					if !ok || info.Offscreen || info.Bounds.X+info.Bounds.W > 960 {
						t.Fatal("clipped update action", key, info)
					}
				}
				if dir := os.Getenv("PANREEL_UI_ARTIFACTS"); dir != "" {
					if err := os.MkdirAll(dir, 0755); err != nil {
						t.Fatal(err)
					}
					f, err := os.Create(filepath.Join(dir, "watch-"+lang+"-"+theme+".png"))
					if err != nil {
						t.Fatal(err)
					}
					err = png.Encode(f, tt.Image())
					closeErr := f.Close()
					if err != nil || closeErr != nil {
						t.Fatal(err, closeErr)
					}
				}
				click("Mark as read")
				if a.watches[item.ID].Unread() {
					t.Fatal("mark as read not persisted")
				}
				// A notification must still find its item when favorite filters hide it,
				// while leaving the independently preserved search filters intact.
				a.searchFilters.drive = "夸克网盘"
				a.savedFilters.drive = "百度网盘"
				a.savedQuery = "hidden"
				a.openWatchNotification(item.ID)
				if a.page != "saved" || a.selected != 0 || len(a.filtered()) != 1 || a.searchFilters.drive != "夸克网盘" {
					t.Fatal("notification did not reveal the matching favorite")
				}
				a.toggleSaved(item)
				if _, exists := a.watches[item.ID]; exists {
					t.Fatal("unfavoriting retained subscription")
				}
			})
		}
	}
}

func TestUpdateNotificationOnlyForCurrentEnabledFavorite(t *testing.T) {
	a := newApp()
	s, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a.store = s
	item := model.Item{ID: "series", Title: "Example series", Drive: "quark", URL: "https://pan.quark.cn/s/demo"}
	a.toggleSaved(item)
	a.configureWatch(item, true, 60)
	w := a.watches[item.ID]
	w.Result = model.WatchResult{Initialized: true, UpdatedAt: time.Now(), Added: 1}
	if ok, err := s.SaveWatchResult(w, w.Result); !ok || err != nil {
		t.Fatal(ok, err)
	}
	sent := make(chan string, 3)
	a.watchNotify = func(_ context.Context, id, title, body string) error { sent <- id; return nil }
	// No native calls are made in the headless test. Marshal callbacks as a
	// production window would; a nil callback queue intentionally drops redraws.
	a.dispatch = func(func()) {}
	a.receiveWatchEvent(watch.Event{Item: item, Watch: w, Updated: true})
	a.watchWork.Wait()
	select {
	case id := <-sent:
		if id != "bookmark:series" {
			t.Fatal(id)
		}
	default:
		t.Fatal("missing system notification")
	}
	a.configureWatch(item, false, 60)
	a.receiveWatchEvent(watch.Event{Item: item, Watch: w, Updated: true})
	a.watchWork.Wait()
	if len(sent) != 0 {
		t.Fatal("late update notified after disabling")
	}
	a.toggleSaved(item)
	a.receiveWatchEvent(watch.Event{Item: item, Watch: w, Updated: true})
	a.watchWork.Wait()
	if len(sent) != 0 {
		t.Fatal("late update notified after removal")
	}
}
