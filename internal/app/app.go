package app

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/cuikaipeng/panreel/internal/catalog"
	"github.com/cuikaipeng/panreel/internal/engine"
	"github.com/cuikaipeng/panreel/internal/engineupdate"
	"github.com/cuikaipeng/panreel/internal/model"
	"github.com/cuikaipeng/panreel/internal/search"
	"github.com/cuikaipeng/panreel/internal/storage"
	"github.com/cuikaipeng/panreel/internal/watch"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const appVersion = "0.1.0"

type resultFilters struct {
	drive, quality, year, subtitle, sort string
}

func defaultResultFilters() resultFilters {
	return resultFilters{drive: "全部网盘", quality: "全部画质", year: "全部年份", subtitle: "全部字幕", sort: "默认排序"}
}

type app struct {
	watches                                                         map[string]model.BookmarkWatch
	watcher                                                         *watch.Runner
	watchWork                                                       sync.WaitGroup
	watchChecking, watchNotificationError, pendingWatchNotification string
	watchNotify                                                     func(context.Context, string, string, string) error
	engineChecking, engineUpdating                                  bool
	engineUpdateStatus                                              message
	engineUpdateCancel                                              context.CancelFunc
	engineAvailable                                                 *engineupdate.Release
	engineInstalled                                                 *engineupdate.Installed
	engineDownloaded, engineTotal                                   int64
	engineWork                                                      sync.WaitGroup
	dispatch                                                        func(func()) // Headless tests provide a serialized UI event queue.
	updateChecking, updateDownloading, updateReady, updateNotice    bool
	updateStatus                                                    message
	updateErr                                                       string
	updateCancel                                                    context.CancelFunc
	availableUpdate                                                 *appUpdate
	updateBytes, updateTotal                                        int64

	prefs                                   model.Preferences
	settingsWin                             *mygo.Window
	prefsTab                                int
	prefsOpen                               bool
	focusSearch                             bool
	queryError                              string
	messageArgs                             []any
	closed                                  bool
	settingsSaveTimer, engineApplyTimer     *time.Timer
	settingsSaveVersion, engineApplyVersion uint64
	pendingSettings                         map[string]bool
	settingErrors                           map[string]string
	settingsSaveError                       string
	settingsNeedWrite                       bool
	engineRestartPending                    bool

	window                                                  *mygo.Window
	store                                                   *storage.Store
	engine                                                  *engine.Manager
	ctx                                                     context.Context
	settings, draft                                         model.Settings
	page, query, searched, savedQuery, sourceChoice         string
	items, saved, allItems                                  []model.Item
	resultMode                                              string
	resultChecks                                            map[string]model.Check
	verification                                            search.VerificationProgress
	history                                                 []model.History
	checks                                                  map[string]model.Check
	checkLink                                               func(context.Context, model.Item) (model.Check, error) // Optional network boundary for headless tests.
	checkCancel                                             context.CancelFunc
	checkActive                                             string
	checkSequence                                           uint64
	loading, starting, online                               bool
	searchCancel                                            context.CancelFunc
	searchID                                                uint64
	searchStatus                                            string
	awaitingResults, resetSearchPosition                    bool
	health                                                  search.Health
	err, message                                            string
	messageAt                                               time.Time
	searchFilters, savedFilters                             resultFilters
	selected                                                int
	list, sourcesList, historyList                          ui.ListState
	sourceTab                                               int
	sourceFilter, selectedPlugin, newChannels               string
	timeoutText, concurrencyText, cacheText, extensionsText string
	clearHistory                                            bool
}

func newApp() *app {
	a := &app{ctx: context.Background(), settings: model.Defaults(), prefs: model.DefaultPreferences(), page: "search", searchFilters: defaultResultFilters(), savedFilters: defaultResultFilters(), sourceChoice: "全部来源", resultMode: "Valid only", selected: -1, checks: map[string]model.Check{}}
	a.sourceTab = sourceTabChannels
	a.restoreDraft()
	return a
}
func Run() error {
	mygo.App.SetName("PanReel")
	mygo.App.SetVersion(appVersion)
	initializeLanguages()
	if !mygo.App.RequestSingleInstanceLock() {
		return nil
	}
	a := newApp()
	ctx, cancel := context.WithCancel(context.Background())
	a.ctx = ctx
	defer cancel()
	mygo.App.OnSecondInstance(func([]string, string) {
		if a.window != nil {
			a.window.Show()
			a.window.Focus()
		}
	})
	mygo.App.OnWindowAllClosed(func() { mygo.App.Quit() })
	mygo.App.OnNotificationClick(func(id string) {
		if target, ok := strings.CutPrefix(id, "bookmark:"); ok {
			if a.window == nil {
				a.pendingWatchNotification = target
			} else {
				a.openWatchNotification(target)
			}
		}
	})
	mygo.App.OnBeforeQuit(func(e *mygo.QuitEvent) {
		if !a.flushSettings() {
			e.PreventDefault()
			a.openPreferencesTab(preferencesEngine)
			return
		}
		if a.updateCancel != nil {
			a.updateCancel()
		}
		if a.engineUpdateCancel != nil {
			a.engineUpdateCancel()
		}
		a.closed = true
		a.cancelLinkCheck()
		a.stopSettingsSave()
		a.stopEngineApply()
		a.flushPrefs()
		cancel()
		if a.searchCancel != nil {
			a.searchCancel()
		}
	})
	mygo.App.WhenReady(func() {
		dir, err := mygo.App.Path(mygo.PathUserData)
		if override := os.Getenv("PANREEL_DATA_DIR"); override != "" {
			dir = override
		}
		if err == nil {
			a.store, err = storage.Open(dir)
		}
		if err == nil {
			a.settings, err = a.store.Settings()
		}
		if err == nil {
			a.prefs, err = a.store.Preferences(a.settings.Theme)
		}
		if err == nil {
			a.saved, err = a.store.Bookmarks()
		}
		if err == nil {
			a.watches, err = a.store.WatchSummaries()
		}
		if err == nil {
			a.history, err = a.store.History()
		}
		if err != nil {
			a.err = "本地数据无法读取：" + err.Error()
		}
		a.restoreDraft()
		resources, _ := mygo.App.Path(mygo.PathResources)
		a.engine = &engine.Manager{Resources: resources, DataDir: dir}
		a.loadEngineVersion()
		a.setLocale()
		a.window = mygo.NewWindow(framed(mygo.WindowOptions{Title: "PanReel", Width: 1180, Height: 760, MinWidth: 960, MinHeight: 640, StateKey: "main", BackgroundColor: windowBackground, Content: ui.View(a.view)}))
		mygo.App.OnLocaleChanged(func(string) { a.refreshLocale() })
		a.window.OnClose(func(e *mygo.CloseEvent) {
			e.PreventDefault()
			mygo.App.Quit()
		})
		a.applyAppearance()
		a.applyLocale()
		a.checkUpdatesOnLaunch()

		if a.err == "" {
			a.startEngine(false)
		}
		go a.watchHealth()
		a.watchEngineUpdates()
		if a.store != nil && a.err == "" {
			a.startBookmarkWatches()
		}
		if a.pendingWatchNotification != "" {
			a.openWatchNotification(a.pendingWatchNotification)
			a.pendingWatchNotification = ""
		}
	})
	err := mygo.App.Run()
	cancel()
	a.engineWork.Wait()
	a.watchWork.Wait()
	if a.engine != nil {
		a.engine.Stop()
	}
	if a.store != nil {
		_ = a.store.Close()
	}
	return err
}
func (a *app) update(fn func()) {
	if a.ctx.Err() != nil {
		return
	}
	apply := func() {
		if a.ctx.Err() != nil || a.closed {
			return
		}
		fn()
		a.refreshViews()
	}
	if a.dispatch != nil {
		a.dispatch(apply)
	} else if a.window != nil {
		a.window.Update(apply)
	} else {
		apply()
	}
}
func (a *app) notice(text string) { a.noticef(text) }
func (a *app) noticef(key string, args ...any) {
	a.message = key
	a.messageArgs = args
	a.messageAt = time.Now()
}
func (a *app) restoreDraft() {
	a.draft = a.settings.Clone()
	a.draft.Drives = slices.Clone(a.settings.EnabledDrives())
	a.draft.ProxyMode = a.settings.EffectiveProxyMode()
	if a.draft.PluginEnv == nil {
		a.draft.PluginEnv = map[string]string{}
	}
	a.timeoutText = strconv.Itoa(a.draft.Timeout)
	a.concurrencyText = strconv.Itoa(a.draft.Concurrency)
	a.cacheText = strconv.Itoa(a.draft.CacheMB)
	a.extensionsText = string(a.draft.Extensions)
	a.sourceChoice = map[string]string{"all": "全部来源", "plugin": "仅搜索插件", "tg": "仅 Telegram"}[a.settings.Source]
}
func (a *app) startEngine(restart bool) {
	if a.starting || a.engineUpdating || a.engine == nil {
		return
	}
	a.stopEngineApply()
	a.engineRestartPending = false
	a.starting = true
	a.cancelLinkCheck()
	a.online = false
	a.err = ""
	if a.searchCancel != nil {
		a.searchCancel()
		a.searchID++
		a.loading = false
		a.awaitingResults = false
		a.searchStatus = "Search stopped"
	}
	s := a.settings.Clone()
	previousCatalog := catalog.Current()
	a.engineWork.Add(1)
	go func() {
		defer a.engineWork.Done()
		if restart {
			a.engine.Stop()
		}
		err := a.engine.Start(a.ctx, s)
		fallback := false
		if err != nil && a.engine.BinaryPath != "" && a.ctx.Err() == nil {
			a.engine.Stop()
			a.engine.BinaryPath = ""
			a.engine.Catalog = &catalog.Bundled
			catalog.SetCurrent(catalog.Bundled)
			s = model.ReconcileEngine(s, previousCatalog, catalog.Bundled)
			err = a.engine.Start(a.ctx, s)
			if err == nil {
				fallback = true
				_ = engineupdate.New(a.engine.DataDir, s).Activate(nil)
			}
		}
		var h search.Health
		if err == nil {
			h, err = a.engine.Client().Health(a.ctx)
		}
		a.update(func() {
			if fallback {
				// Changes may have been saved while startup was in flight.
				// Reconcile the latest settings, never overwrite them with s.
				next := model.ReconcileEngine(a.settings, previousCatalog, catalog.Bundled)
				a.draft = model.ReconcileEngine(a.draft, previousCatalog, catalog.Bundled)
				if a.store != nil {
					if saveErr := a.store.SaveSettings(next); saveErr != nil {
						a.settingsSaveError = saveErr.Error()
						a.settingsNeedWrite = true
					}
				}
				a.settings = next
				a.engineRestartPending = a.engineRestartPending || engineSettingsChanged(s, next)
				a.engineInstalled = nil
				a.engineUpdateStatus = msg("The installed engine could not start. Restored the bundled version.")
			}
			a.starting = false
			if err != nil {
				a.err = err.Error()
				a.online = false
			} else {
				a.health = h
				a.online = !a.engineRestartPending
			}
			a.scheduleEngineApply()
			a.checkSelectedResult()
		})
	}()
}
func (a *app) watchHealth() {
	ticker := time.NewTicker(8 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			if a.engine == nil {
				continue
			}
			client := a.engine.Client()
			if client == nil {
				continue
			}
			h, err := client.Health(a.ctx)
			a.update(func() {
				if client != a.engine.Client() {
					return
				}
				wasOnline := a.online
				a.online = err == nil && !a.starting && !a.engineRestartPending
				if err == nil {
					a.health = h
				}
				if !wasOnline && a.online {
					a.checkSelectedResult()
				}
			})
		}
	}
}
func (a *app) runSearch(query string, force bool) {
	query = strings.TrimSpace(query)
	a.queryError = searchQueryError(query)
	if a.queryError != "" {
		a.focusSearch = true
		return
	}
	if a.engineUpdating {
		a.notice("PanSou is updating. Search will be available shortly.")
		return
	}
	if !a.flushSettings() {
		return
	}
	settings := a.settings.Clone()
	settings.Source = map[string]string{"全部来源": "all", "仅搜索插件": "plugin", "仅 Telegram": "tg"}[a.sourceChoice]
	if _, err := settings.EffectiveSource(); err != nil {
		if len(settings.Channels) == 0 {
			a.configureChannels()
		} else if len(settings.EnabledDrives()) == 0 {
			a.configureDrives()
		}
		a.notice(err.Error())
		return
	}
	if !a.online || a.engine == nil || a.engine.Client() == nil {
		a.notice("搜索引擎尚未就绪，请稍候或在设置中重启")
		return
	}
	ctx, id := a.beginSearch(query)
	if a.store != nil {
		if err := a.store.Record(query); err != nil {
			a.notice("搜索记录保存失败")
		}
		a.history, _ = a.store.History()
	}
	client := a.engine.Client()
	cached := maps.Clone(a.resultChecks)
	if cached == nil {
		cached = map[string]model.Check{}
	}
	for key, value := range a.checks {
		if value.At.After(cached[key].At) {
			cached[key] = value
		}
	}
	go a.collectSearch(ctx, id, client, query, settings, force, cached)
}

func (a *app) beginSearch(query string) (context.Context, uint64) {
	if a.searchCancel != nil {
		a.searchCancel()
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.searchCancel = cancel
	a.searchID++
	a.resetSearchPosition = a.page != "search" || query != a.searched
	if a.page != "search" {
		a.selected = -1
		a.list = ui.ListState{}
	}
	a.page = "search"
	a.query = query
	a.searched = query
	a.loading = true
	// Keep the displayed list until this request produces results. Empty
	// intermediate batches must not briefly replace it with a loading screen.
	a.awaitingResults = true
	a.verification = search.VerificationProgress{Searching: true}
	a.searchStatus = "Searching…"
	a.err = ""
	return ctx, a.searchID
}

func (a *app) collectSearch(ctx context.Context, id uint64, client *search.Client, query string, settings model.Settings, force bool, cached map[string]model.Check) {
	err := client.SearchVerifiedUpdates(ctx, query, settings, force, cached, func(update search.VerifiedUpdate) {
		a.update(func() { a.applyVerifiedBatch(id, update) })
	})
	a.update(func() {
		if id != a.searchID {
			return
		}
		// A successful search with no results replaces the previous list once,
		// at completion. Cancellation and errors leave it available to browse.
		if err == nil && a.awaitingResults {
			a.items = nil
			if a.page == "search" && a.resultMode == "Valid only" {
				a.clearSelection()
				a.list = ui.ListState{}
			}
		}
		if err == nil && a.verification.Found == 0 {
			a.allItems, a.resultChecks = nil, nil
		}
		a.awaitingResults = false
		a.loading = false
		if a.searchCancel != nil {
			a.searchCancel()
			a.searchCancel = nil
		}
		switch {
		case errors.Is(err, context.Canceled):
			a.searchStatus = "Search stopped"
		case errors.Is(err, context.DeadlineExceeded):
			a.searchStatus = "Search timed out"
		case err != nil:
			a.searchStatus = "Search interrupted"
			a.err = err.Error()
		default:
			a.searchStatus = "Search complete"
		}
		if len(a.items) >= 10000 {
			a.notice("结果较多，当前展示前 10000 条。请细化关键词缩小范围。")
		}
	})
}

func (a *app) applySearchBatch(id uint64, items []model.Item) {
	if id != a.searchID || !a.loading {
		return
	}
	if a.awaitingResults && len(items) == 0 {
		return
	}
	selectedID := ""
	if a.page == "search" {
		visible := a.filtered()
		if a.selected >= 0 && a.selected < len(visible) {
			selectedID = visible[a.selected].ID
		}
	}
	if a.awaitingResults {
		// Replace, rather than merge with, the previous request's results.
		a.items = nil
		a.awaitingResults = false
		if a.resetSearchPosition && a.page == "search" {
			a.selected = -1
			a.list = ui.ListState{}
			selectedID = ""
		}
	}
	a.items = search.MergeItems(a.items, items)
	if selectedID != "" {
		a.selected = slices.IndexFunc(a.filtered(), func(item model.Item) bool { return item.ID == selectedID })
	}
	a.checkSelectedResult()
}

func searchQueryError(query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return "请输入片名或关键词"
	}
	if utf8.RuneCountInString(query) > 100 {
		return "搜索关键词最多 100 字"
	}
	return ""
}

func (a *app) stopSearch() {
	if a.searchCancel != nil {
		a.searchCancel()
		a.searchCancel = nil
	}
	a.searchID++
	a.loading = false
	a.awaitingResults = false
	a.searchStatus = "Search stopped"
}
func (a *app) isSaved(id string) bool {
	return slices.ContainsFunc(a.saved, func(i model.Item) bool { return i.ID == id })
}
func (a *app) toggleSaved(item model.Item) {
	if a.store == nil {
		return
	}
	on := !a.isSaved(item.ID)
	if err := a.store.Bookmark(item, on); err != nil {
		a.noticef("收藏保存失败：%s", err)
		return
	}
	a.saved, _ = a.store.Bookmarks()
	if !on {
		delete(a.watches, item.ID)
		if a.watcher != nil {
			a.watcher.Changed(item.ID)
		}
	}
	if on {
		a.notice("已加入收藏夹")
	} else {
		a.notice("已移出收藏夹")
	}
}
func (a *app) copy(item model.Item) {
	text := item.Title + "\n" + item.URL
	if item.Password != "" {
		text += "\n提取码：" + item.Password
	}
	mygo.Clipboard.WriteText(text)
	a.notice("分享链接与已有提取码已复制")
}
func (a *app) open(item model.Item) {
	link, _, _, err := search.NormalizeLink(item.URL, item.Drive)
	if err != nil {
		a.notice(err.Error())
		return
	}
	if err = mygo.Shell.OpenExternal(link); err != nil {
		a.notice("无法打开链接，可复制后使用浏览器或下载工具")
	}
}
func setSelected(values *[]string, id string, on bool) {
	if on && !slices.Contains(*values, id) {
		*values = append(*values, id)
	} else if !on {
		*values = slices.DeleteFunc(*values, func(v string) bool { return v == id })
	}
}
func (a *app) filtered() []model.Item {
	filters := a.activeResultFilters()
	source := a.searchCollection()
	if a.page == "saved" {
		source = a.saved
	}
	out := make([]model.Item, 0, len(source))
	for _, item := range source {
		if a.page != "saved" && !a.settings.AllowsDrive(item.Drive) {
			continue
		}
		if a.page == "saved" && a.savedQuery != "" && !strings.Contains(strings.ToLower(item.Title), strings.ToLower(a.savedQuery)) {
			continue
		}
		if filters.drive != "全部网盘" && catalog.DriveName(item.Drive) != filters.drive {
			continue
		}
		if filters.quality != "全部画质" && item.Quality != filters.quality {
			continue
		}
		if filters.year != "全部年份" && ((filters.year == "年份未标注" && item.Year != "") || (filters.year != "年份未标注" && item.Year != filters.year)) {
			continue
		}
		if filters.subtitle == "含中文字幕" && item.Subtitle == "" || filters.subtitle == "中英字幕" && item.Subtitle != "中英字幕" || filters.subtitle == "字幕未标注" && item.Subtitle != "" {
			continue
		}
		out = append(out, item)
	}
	if filters.sort == "最新分享" {
		slices.SortStableFunc(out, func(a, b model.Item) int { return b.PublishedAt.Compare(a.PublishedAt) })
	}
	return out
}
func (a *app) activeResultFilters() *resultFilters {
	if a.page == "saved" {
		return &a.savedFilters
	}
	return &a.searchFilters
}
func (a *app) resetFilters() {
	filters := a.activeResultFilters()
	filters.drive = "全部网盘"
	filters.quality = "全部画质"
	filters.year = "全部年份"
	filters.subtitle = "全部字幕"
	a.clearSelection()
}
func (a *app) navigate(page string) {
	if page == "settings" {
		a.openPreferences()
		return
	}
	a.page = page
	a.clearSelection()
	a.list = ui.ListState{}
}
func (a *app) configureChannels() {
	a.navigate("sources")
	a.sourceTab = sourceTabChannels
	a.sourceFilter = ""
	a.sourcesList = ui.ListState{}
}

func (a *app) configureDrives() {
	a.navigate("sources")
	a.sourceTab = sourceTabDrives
}
func (a *app) sourceTitle(source string) string {
	if strings.HasPrefix(source, "plugin:") {
		return a.tr("Plugin · %s", strings.TrimPrefix(source, "plugin:"))
	}
	if strings.HasPrefix(source, "tg:") {
		return "TG · " + strings.TrimPrefix(source, "tg:")
	}
	return source
}
func stateTitle(s string) string {
	switch s {
	case "ok":
		return "链接可访问"
	case "bad":
		return "链接已失效"
	case "locked":
		return "需要提取码"
	case "unsupported":
		return "不支持自动检测"
	case "uncertain":
		return "暂时无法确认"
	}
	return "尚未检测"
}
func (a *app) openData(log bool) {
	if a.store == nil {
		return
	}
	path := a.store.Dir
	if log {
		path = filepath.Join(path, "logs")
	}
	if err := mygo.Shell.OpenPath(path); err != nil {
		a.noticef("无法打开目录：%s", err)
	}
}
