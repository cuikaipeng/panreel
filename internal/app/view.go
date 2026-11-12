package app

import (
	"slices"
	"strings"
	"time"

	"github.com/cuikaipeng/panreel/internal/catalog"
	"github.com/cuikaipeng/panreel/internal/model"
	"github.com/egoist/mygo/ui"
)

var pages = []struct {
	id, title string
	icon      *ui.SVG
}{{"search", "探索", searchIcon}, {"saved", "收藏夹", bookmarkIcon}, {"history", "搜索历史", historyIcon}, {"sources", "搜索来源", sourcesIcon}}

func (a *app) view(c *ui.Context) {
	pal := a.theme(c)
	a.shell(c, pal, func() {
		ui.Column(c).Fill().MinWidth(0).MinHeight(0).Padding(20, 24).Gap(12).Children(func() {
			if a.err != "" {
				ui.Row(c).FillWidth().Gap(12).Padding(10, 12).Background(pal.danger.Alpha(.08)).Radius(8).Children(func() {
					ui.Text(c, a.tr(a.err)).Wrap().Grow(1).TextColor(pal.danger)
					button(c, a.tr("关闭提示")).OnClick(func() { a.err = "" })
				})
			}
			switch a.page {
			case "sources":
				a.sourcesView(c)
			case "history":
				a.historyView(c)
			default:
				a.searchView(c)
			}
		})
	})
	if ui.AlertDialog(c.Key("clear-history"), &a.clearHistory, a.tr("清空搜索历史？"), a.tr("收藏夹不会受到影响。"), a.tr("取消"), a.tr("清空历史")) == 1 && a.store != nil {
		if err := a.store.ClearHistory(); err != nil {
			a.notice(err.Error())
		} else {
			a.history = nil
			a.notice("搜索历史已清空")
		}
	}
	a.noticeView(c)
}

func (a *app) noticeView(c *ui.Context) {
	// Each window presents a notice once. MyGo owns its timeout and redraws,
	// so it neither occupies permanent space nor lingers until the next input.
	type shownNotice struct {
		at   time.Time
		text string
	}
	shown := ui.Local(c.Root(), "app-notice", func() shownNotice { return shownNotice{} })
	remaining := 7*time.Second - time.Since(a.messageAt)
	if a.message == "" || remaining <= 0 {
		return
	}
	text := a.tr(a.message, a.messageArgs...)
	if shown.at != a.messageAt || shown.text != text {
		c.AddToast(ui.Toast{ID: "app-notice", Title: text, Timeout: remaining})
		*shown = shownNotice{at: a.messageAt, text: text}
	}
}

func (a *app) heading(c *ui.Context, title string) {
	ui.Text(c, a.tr(title)).FontSize(22).FontWeight(650)
}
func (a *app) searchView(c *ui.Context) {
	needsChannels := len(a.settings.Channels) == 0
	if a.page == "search" && (needsChannels || len(a.settings.EnabledDrives()) == 0 || (a.searched == "" && !a.loading)) {
		a.discoverHome(c, needsChannels)
		return
	}
	if a.page == "saved" {
		a.heading(c, "收藏夹")
		enamelField(c, ui.SearchField(c.Key("saved-filter"), &a.savedQuery)).Label(a.tr("搜索收藏")).Placeholder(a.tr("在收藏中查找…")).FillWidth().Height(36).OnChange(a.clearSelection)
	} else {
		a.searchForm(c, false)
	}
	a.filtersView(c)
	items := a.filtered()
	ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, a.tr("%d 条结果", len(items))).FontWeight(600)
		if a.page == "search" {
			if a.loading {
				ui.Spinner(c).Size(15, 15)
			}
			if a.searchStatus != "" {
				status := ui.Text(c, a.searchStatusLabel()).FontSize(13).TextColor(c.Theme().TextMuted).Role(ui.RoleStatus).MinWidth(0).Wrap()
				if a.loading {
					status.TextColor(a.paletteFor(c).accentInk)
				}
			}
		}
		ui.Spacer(c)
		if a.page == "search" && a.searched != "" {
			a.selectValue(c.Key("result-mode"), &a.resultMode, []string{"Valid only", "All results"}).Label(a.tr("Link status filter")).Width(120).OnChange(a.changeResultMode)
			button(c, a.tr("重新抓取")).Disabled(a.loading || !a.online || a.engineUpdating || needsChannels || len(a.settings.EnabledDrives()) == 0).OnClick(func() { a.runSearch(a.searched, true) })
		}
	})
	if a.page == "search" && a.loading && len(items) == 0 {
		ui.Column(c).Fill().Center().Children(func() { ui.Spinner(c).Size(28, 28) })
		return
	}
	if len(items) == 0 {
		ui.Column(c).Fill().Center().Gap(13).Children(func() {
			ui.Icon(c, searchIcon).Size(48, 48).TextColor(c.Theme().TextMuted)
			title := "这次还没有找到"
			description := "可以调整关键词、放宽筛选，或检查搜索来源是否已启用。"
			showAll := a.page == "search" && a.resultMode == "Valid only" && len(a.items) == 0 && len(a.allItems) > 0
			if a.page == "saved" {
				title = "收藏夹里的故事，从这里开始"
			} else if showAll {
				title = "No verified links found"
				description = "Expired and unconfirmed links are available under All results."
			}
			ui.Text(c, a.tr(title)).FontSize(17)
			muted(c, a.tr(description))
			if showAll {
				button(c, a.tr("Show all results")).OnClick(func() {
					a.resultMode = "All results"
					a.changeResultMode()
				})
			} else {
				button(c, a.tr("清除筛选")).OnClick(a.resetFilters)
			}
		})
		return
	}
	ui.Row(c).Fill().MinHeight(0).Gap(12).AlignItems(ui.Stretch).Children(func() {
		a.resultsList(c, items)
		width, _ := c.Size()
		enamelPanel(c, ui.Column(c).Width(min(float32(340), max(float32(280), (width-60)*.28))).Shrink(0).FillHeight().MinHeight(0).Padding(16).Radius(10)).Children(func() {
			if a.selected < 0 || a.selected >= len(items) {
				ui.Column(c).Fill().Center().Gap(12).Children(func() {
					brandMark(c, 56)
					muted(c, a.tr("选中一条结果，查看分享详情"))
				})
				return
			}
			a.detailView(c, items[a.selected])
		})
	})
}

func (a *app) searchForm(c *ui.Context, centered bool) {
	t := c.Theme()
	ui.Column(c).FillWidth().Gap(7).Children(func() {
		var input ui.Element
		bar := ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center)
		height := float32(36)
		stopLabel := a.tr("停止搜索")
		actionWidth, _ := c.MeasureText(0, ui.Span{Text: stopLabel, Size: 13, Weight: 550})
		actionWidth = max(float32(92), actionWidth+26)
		if centered {
			height = 44
			enamelField(c, bar).Padding(5).Radius(12).Shadow(0, 1, 3, 0, enamelShadow(c))
		}
		bar.Children(func() {
			input = a.searchInput(c.Key("search-input")).Label(a.tr("搜索片名或关键词")).Placeholder(a.tr("Film title or keyword…")).Height(height).Grow(1).MinWidth(0).OnSubmit(func() { a.runSearch(a.query, false) })
			if centered {
				input.Background(ui.Transparent).Border(0, ui.Transparent).NoShadow().Radius(8).FontSize(16)
			} else {
				a.selectValue(c.Key("search-source"), &a.sourceChoice, []string{"全部来源", "仅搜索插件", "仅 Telegram"}).Label(a.tr("搜索来源范围")).Width(132).Height(height)
			}
			if a.loading {
				button(c, stopLabel).Height(height).Width(actionWidth).OnClick(a.stopSearch)
			} else {
				primaryButton(c, a.tr("搜索")).Height(height).Width(actionWidth).Disabled(!a.online || a.engineUpdating || len(a.settings.Channels) == 0 || len(a.settings.EnabledDrives()) == 0).OnClick(func() { a.runSearch(a.query, false) })
			}
		})
		if centered && input.FocusWithin() {
			bar.Border(1, t.Accent.Alpha(.55))
		}
		if a.queryError != "" {
			if centered {
				bar.Border(1.5, t.Danger)
			}
			ui.Row(c).FillWidth().Gap(7).AlignItems(ui.Center).Padding(8, 10).Radius(6).Background(t.Danger.Alpha(.07)).Children(func() {
				ui.Icon(c, alertIcon).Size(16, 16).TextColor(t.Danger).Role(ui.RoleNone)
				message := ui.Text(c, a.tr(a.queryError)).FontSize(13).FontWeight(550).TextColor(t.Danger).Wrap().Grow(1).Role(ui.RoleAlert)
				input.ErrorMessage(message)
			})
		}
	})
}

func (a *app) resultsList(c *ui.Context, items []model.Item) {
	t := c.Theme()
	listTheme := *t
	// MyGo paints an accent behind selected rows, including an inactive-window
	// fallback. Keep that backing neutral; the rows draw their own selection.
	listTheme.Accent = t.Surface
	listTheme.InactiveAccent = t.Surface
	listTheme.AccentText = t.Text
	listTheme.SurfaceHover = t.Surface
	a.list.Selected = &a.selected
	a.list.Key = func(i int) any { return items[i].ID }
	a.list.Label = func(i int) string { return items[i].Title }
	enamelPanel(c, ui.Themed(c, &listTheme).Grow(1).MinWidth(260).FillHeight().MinHeight(0).Radius(10).Clip()).Children(func() {
		ui.List(c.Key("results-list"), &a.list, len(items)).Gap(0).Fill().MinHeight(0).Rows(func(r ui.ListRow) {
			c := r.Context
			item := items[r.Index]
			row := ui.Column(c.Key(item.ID)).FillWidth().Padding(14, 16).Gap(6).Background(t.Surface).TextColor(t.Text).BorderWidth(0, 0, 1, 0).BorderColor(t.Border)
			if r.Selected() {
				row.Background(t.Surface.Mix(t.Accent, .055))
			} else if row.Hovered() {
				row.Background(t.Surface.Mix(t.Accent, .025))
			}
			row.Children(func() {
				if r.Selected() {
					ui.Box(c).Absolute().Left(0).Top(0).Bottom(0).Width(3).Background(t.Accent).Role(ui.RoleNone)
				}
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					badge(c, catalog.DriveName(item.Drive))
					if a.page == "search" && a.resultMode == "All results" {
						if check, ok := a.cachedLinkCheck(item); ok {
							badge(c, a.tr(stateTitle(check.State)))
						}
					}
					ui.Spacer(c)
					if a.isSaved(item.ID) {
						if a.watches[item.ID].Unread() {
							badge(c, a.tr("Resource updated"))
						}
						ui.Icon(c, bookmarkIcon).Size(15, 15).TextColor(a.paletteFor(c).accentInk)
					}
					if r.Selected() {
						ui.Icon(c, checkIcon).Size(16, 16).TextColor(a.paletteFor(c).accentInk).Role(ui.RoleNone)
					}
				})
				ui.Text(c, item.Title).FontSize(15).FontWeight(600).Wrap()
				parts := []string{}
				for _, v := range []string{item.Year, item.Quality, item.Subtitle, item.Size} {
					if v != "" {
						parts = append(parts, v)
					}
				}
				if len(parts) > 0 {
					muted(c, strings.Join(parts, " · "))
				}
				if len(item.Sources) > 0 {
					muted(c, a.sourceTitle(item.Sources[0]))
				}
			})
		}).OnChange(a.checkSelectedResult).OnClick(a.checkSelectedResult)
	})
}

func (a *app) discoverHome(c *ui.Context, needsChannels bool) {
	ui.Column(c).Fill().Center().Children(func() {
		ui.Column(c).WidthPercent(76).MinWidth(520).MaxWidth(880).Gap(14).Children(func() {
			a.searchForm(c, true)
			if needsChannels {
				button(c, a.tr("Configure TG channels")).AlignSelf(ui.Center).OnClick(a.configureChannels)
			} else if len(a.settings.EnabledDrives()) == 0 {
				button(c, a.tr("Configure drive types")).AlignSelf(ui.Center).OnClick(a.configureDrives)
			}
		})
	})
}

func (a *app) filtersView(c *ui.Context) {
	filters := a.activeResultFilters()
	drives := []string{"全部网盘"}
	for _, d := range catalog.Drives {
		if a.page == "saved" || a.settings.AllowsDrive(d.ID) {
			drives = append(drives, d.Name)
		}
	}
	years := []string{}
	collection := a.searchCollection()
	if a.page == "saved" {
		collection = a.saved
	}
	for _, i := range collection {
		if i.Year != "" && !slices.Contains(years, i.Year) {
			years = append(years, i.Year)
		}
	}
	slices.Sort(years)
	slices.Reverse(years)
	years = append([]string{"全部年份"}, years...)
	years = append(years, "年份未标注")
	enamelField(c, ui.Row(c).FillWidth().Gap(8).Padding(6)).Radius(10).Children(func() {
		a.selectValue(c, &filters.drive, drives).Label(a.tr("网盘筛选")).Width(140).OnChange(a.clearSelection)
		a.selectValue(c, &filters.quality, []string{"全部画质", "4K", "1080P", "720P"}).Label(a.tr("画质筛选")).Width(140).OnChange(a.clearSelection)
		a.selectValue(c, &filters.year, years).Label(a.tr("年份筛选")).Width(120).OnChange(a.clearSelection)
		a.selectValue(c, &filters.subtitle, []string{"全部字幕", "含中文字幕", "中英字幕", "字幕未标注"}).Label(a.tr("字幕筛选")).Width(130).OnChange(a.clearSelection)
		ui.Spacer(c)
		a.selectValue(c, &filters.sort, []string{"默认排序", "最新分享"}).Label(a.tr("结果排序")).Width(144).OnChange(a.clearSelection)
	})
}
func (a *app) detailView(c *ui.Context, item model.Item) {
	gap := float32(14)
	if _, height := c.Size(); height < 700 {
		gap = 8
	}
	ui.Scroll(c.Key("detail-" + item.ID)).Fill().Children(func() {
		ui.Column(c).FillWidth().Gap(gap).Children(func() {
			badge(c, catalog.DriveName(item.Drive))
			ui.Text(c, item.Title).FontSize(17).FontWeight(600).Wrap()
			a.linkCheckView(c, item)
			if item.Description != "" && item.Description != item.Title {
				muted(c, item.Description)
			}
			for _, entry := range []struct{ label, value string }{{"画质", item.Quality}, {"年份", item.Year}, {"字幕", item.Subtitle}, {"体积", item.Size}} {
				v := entry.value
				if v == "" {
					continue
				}
				ui.Row(c).FillWidth().Children(func() { muted(c, a.tr(entry.label)); ui.Spacer(c); ui.Text(c, a.tr(v)).FontSize(13) })
			}
			ui.Box(c).FillWidth().Height(1).Background(c.Theme().Border)
			muted(c, a.tr("分享来源"))
			for _, source := range item.Sources {
				ui.Text(c, a.sourceTitle(source)).FontSize(13).Wrap()
			}
			if !item.PublishedAt.IsZero() {
				muted(c, a.tr("Shared on %s", item.PublishedAt.Local().Format("2006/01/02")))
			}
			if item.Password != "" {
				ui.Text(c, a.tr("Extraction code: %s", item.Password)).FontWeight(600).Wrap()
			}
			primaryButton(c, a.tr("打开分享链接")).FillWidth().OnClick(func() { a.open(item) })
			ui.Row(c).FillWidth().Gap(8).Children(func() {
				button(c, a.tr("复制链接")).Grow(1).OnClick(func() { a.copy(item) })
				label := "加入收藏"
				if a.isSaved(item.ID) {
					label = "取消收藏"
				}
				label = a.tr(label)
				labelWidth, _ := c.MeasureText(0, ui.Span{Text: label, Size: 13, Weight: 550})
				button(c, label).Grow(1).MinWidth(labelWidth + 26).OnClick(func() { a.toggleSaved(item) })
			})
			if a.isSaved(item.ID) {
				a.bookmarkWatchView(c, item)
			}

		})
	})
}
func (a *app) historyView(c *ui.Context) {
	a.heading(c, "搜索历史")
	ui.Row(c).FillWidth().Children(func() {
		ui.Text(c, a.tr("%d 条记录", len(a.history)))
		ui.Spacer(c)
		button(c, a.tr("清空历史")).Disabled(len(a.history) == 0).OnClick(func() { a.clearHistory = true })
	})
	ui.List(c, &a.historyList, len(a.history), func(i int) {
		h := a.history[i]
		enamelPanel(c, ui.Row(c.Key(h.Query)).FillWidth().Padding(15, 18).Gap(14).Margin(0, 0, 7, 0).Radius(10).AlignItems(ui.Center)).Children(func() {
			ui.Icon(c, historyIcon).Size(18, 18).TextColor(c.Theme().TextMuted)
			ui.Column(c).Grow(1).Gap(5).Children(func() { ui.Text(c, h.Query).FontWeight(600); muted(c, h.At.Format("2006/01/02 15:04")) })
			button(c, a.tr("再次搜索")).Label(a.tr("再次搜索：") + h.Query).OnClick(func() { a.runSearch(h.Query, false) })
		})
	}).Fill().MinHeight(0)
}

func (a *app) searchInput(c *ui.Context) ui.Element {
	e := enamelField(c, ui.SearchField(c, &a.query)).Invalid(a.queryError != "").OnChange(func() {
		if a.queryError != "" {
			a.queryError = searchQueryError(a.query)
		}
	})
	if a.queryError != "" {
		e.Border(1.5, c.Theme().Danger).FocusRingColor(c.Theme().Danger.Alpha(.5))
	}
	if a.focusSearch {
		e.Focus()
		a.focusSearch = false
	}
	return e
}
