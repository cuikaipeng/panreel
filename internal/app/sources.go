package app

import (
	"slices"
	"strings"

	"github.com/cuikaipeng/panreel/internal/catalog"
	"github.com/cuikaipeng/panreel/internal/model"
	"github.com/cuikaipeng/panreel/internal/search"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const (
	sourceTabPlugins = iota
	sourceTabDrives
	sourceTabChannels
)

func (a *app) sourcesView(c *ui.Context) {
	ui.Column(c).Fill().MinHeight(0).Gap(14).Disabled(a.engineUpdating).Children(func() { a.sourcesViewContent(c) })
}
func (a *app) sourcesViewContent(c *ui.Context) {
	a.heading(c, "搜索来源")
	a.searchScopeSummary(c)
	a.settingsErrorView(c)
	ui.Row(c).FillWidth().Gap(12).Wrap().AlignItems(ui.Center).Children(func() {
		a.sourceTabs(c)
	})
	if a.sourceTab == sourceTabChannels {
		a.channelsView(c)
		return
	}
	if a.sourceTab == sourceTabDrives {
		a.drivesView(c)
		return
	}
	ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
		enamelField(c, ui.SearchField(c, &a.sourceFilter)).Label(a.tr("查找插件")).Placeholder(a.tr("查找搜索插件…")).Grow(1)
		muted(c, a.tr("已选 %d / %d", len(a.draft.Plugins), len(catalog.Current().Plugins)))
		button(c, a.tr("全选插件")).OnClick(func() { a.draft.Plugins = catalog.PluginIDs(); a.changeSetting(settingPlugins) })
		button(c, a.tr("清空选择")).OnClick(func() { a.draft.Plugins = nil; a.changeSetting(settingPlugins) })
	})
	plugins := []catalog.Plugin{}
	for _, p := range catalog.Current().Plugins {
		if strings.Contains(strings.ToLower(p.ID), strings.ToLower(a.sourceFilter)) {
			plugins = append(plugins, p)
		}
	}
	ui.Row(c).Fill().MinHeight(0).Gap(12).AlignItems(ui.Stretch).Children(func() {
		ui.List(c.Key("plugins"), &a.sourcesList, len(plugins), func(i int) {
			p := plugins[i]
			on := slices.Contains(a.draft.Plugins, p.ID)
			enamelPanel(c, ui.Row(c.Key(p.ID)).FillWidth().Padding(12, 14).Gap(12).Margin(0, 0, 5, 0).Radius(9).AlignItems(ui.Center)).Children(func() {
				ui.Checkbox(c, &on, p.ID).Label(a.tr("启用插件 ") + p.ID).Grow(1).OnChange(func() { setSelected(&a.draft.Plugins, p.ID, on); a.changeSetting(settingPlugins) })
				if state := a.sourceState(p.ID, false); state != "" {
					badge(c, a.tr(state))
				}
				button(c, a.tr("配置")).Label(a.tr("配置插件 ") + p.ID).OnClick(func() { a.selectedPlugin = p.ID })
			})
		}).Grow(1).MinWidth(280).FillHeight().MinHeight(0)
		enamelPanel(c, ui.Column(c).Width(280).Shrink(0).FillHeight().Padding(16).Radius(10)).Children(func() { a.pluginDetails(c) })
	})
}

func (a *app) searchScopeSummary(c *ui.Context) {
	enamelPanel(c, ui.Row(c).FillWidth().Padding(12, 16).Gap(20).Wrap().AlignItems(ui.Center).Radius(10)).Children(func() {
		ui.Text(c, a.tr("Your search space")).FontSize(13).FontWeight(600)
		ui.Spacer(c)
		for _, stat := range []struct {
			value int
			label string
		}{{len(a.settings.Channels), "Enabled TG channels"}, {len(a.settings.Plugins), "Enabled plugins"}, {len(a.settings.EnabledDrives()), "Enabled link types"}} {
			ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
				ui.Textf(c, "%d", stat.value).FontSize(18).FontWeight(650).TextColor(a.paletteFor(c).accentInk)
				ui.Text(c, a.tr(stat.label)).FontSize(12).TextColor(c.Theme().TextMuted)
			})
		}
	})
}

func (a *app) sourceTabs(c *ui.Context) {
	t := c.Theme()
	labels := []string{
		sourceTabPlugins:  a.tr("搜索插件 · %d", len(catalog.Current().Plugins)),
		sourceTabDrives:   a.tr("Drive types · %d", len(a.draft.EnabledDrives())),
		sourceTabChannels: a.tr("TG 频道 · %d", len(a.draft.Channels)),
	}
	tabs := ui.SegmentedBase(c.Key("source-tabs"), &a.sourceTab, len(labels))
	enamelField(c, tabs.Track.Label(a.tr("来源分类")).Padding(4).Gap(6)).Radius(10).Background(t.Background.Mix(t.Text, .025)).Children(func() {
		for i, label := range labels {
			tab := enamelTab(c, tabs.Segment(i).Label(label).Height(36).MinWidth(142).Padding(0, 16).Radius(7), i == a.sourceTab, a.paletteFor(c).accentInk)
			weight := 500
			if i == a.sourceTab {
				weight = 650
			}
			tab.Children(func() { ui.Text(c, label).FontSize(13).FontWeight(weight).SingleLine() })
		}
	}).OnChange(func() { a.sourceFilter = ""; a.sourcesList = ui.ListState{} })
}

func (a *app) drivesView(c *ui.Context) {
	ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
		muted(c, a.tr("已选 %d / %d", len(a.draft.EnabledDrives()), len(catalog.Drives)))
		ui.Spacer(c)
		button(c, a.tr("Select all types")).OnClick(func() { a.draft.Drives = catalog.DriveIDs(); a.changeSetting(settingDrives) })
		button(c, a.tr("清空选择")).OnClick(func() { a.draft.Drives = []string{}; a.changeSetting(settingDrives) })
	})
	ui.Scroll(c.Key("drive-types")).Fill().MinHeight(0).Children(func() {
		ui.Column(c).FillWidth().Gap(10).Children(func() {
			for row := 0; row < len(catalog.Drives); row += 3 {
				ui.Row(c).FillWidth().Gap(10).Children(func() {
					for column := 0; column < 3; column++ {
						i := row + column
						if i >= len(catalog.Drives) {
							ui.Box(c).Grow(1).Basis(0)
							continue
						}
						d := catalog.Drives[i]
						on := a.draft.AllowsDrive(d.ID)
						enamelPanel(c, ui.Box(c.Key(d.ID)).Grow(1).Basis(0).MinWidth(0).Padding(16).Radius(9)).Children(func() {
							ui.Checkbox(c, &on, a.tr(d.Name)).FillWidth().Label(a.tr("Enable type %s", a.tr(d.Name))).OnChange(func() {
								setSelected(&a.draft.Drives, d.ID, on)
								a.changeSetting(settingDrives)
							})
						})
					}
				})
			}
		})
	})
}

func (a *app) pluginDetails(c *ui.Context) {
	var selected *catalog.Plugin
	for i := range catalog.Current().Plugins {
		if catalog.Current().Plugins[i].ID == a.selectedPlugin {
			selected = &catalog.Current().Plugins[i]
			break
		}
	}
	if selected == nil {
		ui.Column(c).Fill().Center().Gap(14).Children(func() {
			ui.Icon(c, sourcesIcon).Size(40, 40).TextColor(c.Theme().TextMuted)
			muted(c, a.tr("Select a plugin to configure"))
		})
		return
	}
	p := *selected
	ui.Scroll(c.Key("plugin-" + p.ID)).Fill().Children(func() {
		ui.Column(c).FillWidth().Gap(12).Children(func() {
			ui.Text(c, p.ID).FontSize(17).FontWeight(600)
			if info := a.sourceInfo(p.ID, false); info != nil {
				muted(c, a.tr("最近 %d 轮 · %d 轮有结果 · %d 轮出错", info.Rounds, info.Yielded, info.Failed))
				if info.LastError != "" {
					muted(c, info.LastError)
				}
			}
			count := 0
			for _, key := range p.Env {
				if model.ReservedEnv(key) {
					continue
				}
				count++
				key := key
				value := a.draft.PluginEnv[key]
				ui.Column(c).Gap(6).FillWidth().Children(func() {
					muted(c, key)
					input := enamelField(c, ui.TextInput(c.Key(key), &value)).Password().Label(p.ID + " " + key).Placeholder(a.tr("留空使用插件默认值")).FillWidth().OnChange(func() { a.draft.PluginEnv[key] = value; a.queueSetting("plugin:" + key) })
					a.settingError(c, input, "plugin:"+key)
				})
			}
			if count == 0 {
				muted(c, a.tr("No additional settings"))
			}
			button(c, a.tr("查看插件文档与源码")).FillWidth().OnClick(func() {
				_ = mygo.Shell.OpenExternal("https://github.com/fish2018/pansou/tree/" + catalog.Current().Revision + "/plugin/" + p.ID)
			})
		})
	})
}
func (a *app) sourceInfo(id string, tg bool) *search.LivenessItem {
	lists := [][]search.LivenessItem{a.health.Liveness.FailingPlugins, a.health.Liveness.ZeroPlugins, a.health.Liveness.DegradedPlugins}
	if tg {
		lists = [][]search.LivenessItem{a.health.Liveness.FailingChannels, a.health.Liveness.ZeroChannels}
	}
	for _, list := range lists {
		for _, info := range list {
			if info.Name == id {
				out := info
				return &out
			}
		}
	}
	return nil
}
func (a *app) sourceState(id string, tg bool) string {
	if info := a.sourceInfo(id, tg); info != nil {
		switch info.Status {
		case "failing":
			return "近期出错"
		case "zero_yield":
			return "暂未产出"
		case "degraded":
			return "部分失败"
		default:
			if info.Failed > 0 {
				return "近期出错"
			}
			return "暂未产出"
		}
	}
	return ""
}
func (a *app) channelsView(c *ui.Context) {
	card(c, func() {
		ui.Row(c).FillWidth().Gap(10).Children(func() {
			ui.Text(c, a.tr("Telegram 连通状态")).FontWeight(600)
			ui.Spacer(c)
			status := "尚未检测"
			if a.health.TG.Age != nil {
				if a.health.TG.Reachable {
					status = "可以连接"
				} else {
					status = "暂不可达"
				}
			}
			badge(c, a.tr(status))
		})
		if a.health.TG.Age != nil && !a.health.TG.Reachable {
			muted(c, a.tr("Telegram is unreachable. Search plugins are still available. Check your network or system proxy."))
		}
	}).Padding(15, 18)
	ui.Row(c).FillWidth().Gap(10).Children(func() {
		enamelField(c, ui.TextArea(c.Key("add-channels"), &a.newChannels)).Label(a.tr("添加 Telegram 频道")).Placeholder("@channel_one, @channel_two, https://t.me/channel_three").Height(64).Grow(1)
		button(c, a.tr("添加频道")).OnClick(a.addChannels)
	})
	muted(c, a.tr("Separate with commas, spaces or newlines."))
	ui.Row(c).FillWidth().Gap(9).Wrap().AlignItems(ui.Center).Children(func() {
		enamelField(c, ui.SearchField(c, &a.sourceFilter)).Label(a.tr("查找频道")).Placeholder(a.tr("查找频道…")).Grow(1)
		muted(c, a.tr("已选 %d 个", len(a.draft.Channels)))
		button(c, a.tr("全选频道")).OnClick(func() { a.draft.Channels = a.allChannels(); a.changeSetting(settingChannels) })
		button(c, a.tr("清空选择")).OnClick(func() { a.draft.Channels = nil; a.changeSetting(settingChannels) })
	})
	channels := []string{}
	for _, id := range a.allChannels() {
		if strings.Contains(strings.ToLower(id), strings.ToLower(a.sourceFilter)) {
			channels = append(channels, id)
		}
	}
	if len(channels) == 0 {
		ui.Column(c).Fill().Center().Gap(10).Children(func() {
			if len(a.allChannels()) == 0 {
				ui.Text(c, a.tr("No channels configured")).FontWeight(600)
				muted(c, a.tr("Add your public channels above. At least one enabled channel is required to search."))
			} else {
				muted(c, a.tr("No matching channels"))
			}
		})
		return
	}
	ui.List(c.Key("channels"), &a.sourcesList, len(channels), func(i int) {
		id := channels[i]
		on := slices.Contains(a.draft.Channels, id)
		enamelPanel(c, ui.Row(c.Key(id)).FillWidth().Padding(11, 16).Gap(12).Margin(0, 0, 5, 0).Radius(9).AlignItems(ui.Center)).Children(func() {
			ui.Checkbox(c, &on, id).Label(a.tr("启用频道 ") + id).Grow(1).OnChange(func() { setSelected(&a.draft.Channels, id, on); a.changeSetting(settingChannels) })
			if state := a.sourceState(id, true); state != "" {
				badge(c, a.tr(state))
			}
			button(c, a.tr("打开频道")).Label(a.tr("打开频道 ") + id).OnClick(func() { _ = mygo.Shell.OpenExternal("https://t.me/" + id) })
			button(c, a.tr("移除")).Label(a.tr("Remove channel ") + id).OnClick(func() {
				setSelected(&a.draft.CustomChannels, id, false)
				setSelected(&a.draft.Channels, id, false)
				a.changeSetting(settingChannels)
			})
		})
	}).Fill().MinHeight(0)
}
func (a *app) allChannels() []string {
	out := slices.Clone(a.draft.CustomChannels)
	for _, id := range a.draft.Channels {
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}
func (a *app) addChannels() {
	items, err := model.ParseChannels(a.newChannels)
	if err != nil {
		a.notice(err.Error())
		return
	}
	if len(items) == 0 {
		a.notice("请输入公开频道用户名")
		return
	}
	// Validate the entire merged batch before changing either selection.
	if _, err = model.ParseChannels(strings.Join(append(a.allChannels(), items...), "\n")); err != nil {
		a.notice(err.Error())
		return
	}
	for _, id := range items {
		setSelected(&a.draft.CustomChannels, id, true)
		setSelected(&a.draft.Channels, id, true)
	}
	if a.changeSetting(settingChannels) {
		a.newChannels = ""
		a.noticef("Added %d channels", len(items))
	}
}
