package app

import (
	"github.com/cuikaipeng/panreel/internal/catalog"
	"github.com/cuikaipeng/panreel/internal/model"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"runtime"
)

const (
	preferencesGeneral = iota
	preferencesAppearance
	preferencesEngine
	preferencesUpdates
	preferencesAbout
)

var preferenceTabs = []string{"General", "Appearance", "Search engine", "Updates", "About"}

func (a *app) preferencesView(c *ui.Context) {
	pal := a.theme(c)
	c.Root().Background(pal.canvas)
	top := float32(12)
	if runtime.GOOS == "darwin" {
		top = topBarH + 8
	}
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Width(180).Shrink(0).Padding(top, 10, 16, 10).Gap(4).Children(func() {
			for i, name := range preferenceTabs {
				i, name := i, name
				b := enamelTab(c, ui.ButtonBase(c.Key(name)).Label(a.tr(name)).FillWidth().Height(36).Radius(8).Padding(0, 12), i == a.prefsTab, pal.accentInk)
				b.Children(func() { ui.Row(c).FillWidth().Children(func() { ui.Text(c, a.tr(name)).FontSize(13).FontWeight(500) }) }).OnClick(func() { a.prefsTab = i })
			}
		})
		ui.Column(c).Grow(1).MinWidth(0).Padding(8).Children(func() {
			enamelPanel(c, ui.Column(c).Fill().MinHeight(0).Radius(cardRadius).Clip()).Background(pal.card).Children(func() {
				ui.Row(c).Height(48).Shrink(0).FillWidth().Padding(0, 20).AlignItems(ui.Center).DragWindow().Children(func() { ui.Text(c, a.tr(preferenceTabs[a.prefsTab])).FontSize(15).FontWeight(650) })
				ui.Scroll(c.Key(preferenceTabs[a.prefsTab])).Fill().MinHeight(0).Children(func() {
					ui.Column(c).FillWidth().Padding(6, 20, 20, 20).Gap(18).Children(func() {
						switch a.prefsTab {
						case preferencesGeneral:
							a.generalPreferences(c)
						case preferencesAppearance:
							a.appearancePreferences(c)
						case preferencesEngine:
							a.enginePreferences(c)
						case preferencesUpdates:
							a.updatesPreferences(c)
						case preferencesAbout:
							a.aboutPreferences(c)
						}
					})
				})
			})
		})
	})
	if runtime.GOOS == "darwin" {
		ui.Box(c).Absolute().Top(0).Left(86).Width(90).Height(44).DragWindow()
	}
	a.noticeView(c)
}

func (a *app) generalPreferences(c *ui.Context) {
	a.preferenceRow(c, "Language", func() {
		value := a.prefs.Language
		labels := map[string]string{"system": a.tr("Follow system"), "zh-Hans": "简体中文", "en": "English"}
		selected := labels[value]
		if enamelButton(c, ui.Select(c, &selected, []string{labels["system"], labels["zh-Hans"], labels["en"]}), false).Label(a.tr("Language")).Height(30).Width(165).Changed() {
			for k, v := range labels {
				if v == selected {
					a.prefs.Language = k
					break
				}
			}
			a.savePrefs()
		}
	})
	ui.Checkbox(c, &a.prefs.CheckUpdates, a.tr("Check for updates on launch")).OnChange(a.savePrefs)
	a.section(c, "Local data", func() {
		ui.Row(c).Gap(8).Children(func() {
			button(c, a.tr("打开数据目录")).OnClick(func() { a.openData(false) })
			button(c, a.tr("打开引擎日志")).OnClick(func() { a.openData(true) })
		})
	})
}

func (a *app) appearancePreferences(c *ui.Context) {
	a.preferenceRow(c, "Theme", func() {
		if a.selectValue(c, &a.prefs.Theme, []string{"system", "light", "dark"}).Label(a.tr("Theme")).Width(165).Changed() {
			a.savePrefs()
		}
	})
	a.section(c, "Accent color", func() {
		ui.Row(c).Gap(12).Children(func() {
			for _, name := range model.AccentOptions {
				name := name
				color := accentColor(name, a.paletteFor(c).dark)
				if name == model.DefaultAccent {
					color = mistBlue
				}
				b := ui.ButtonBase(c.Key(name)).Label(a.tr(name)).Tooltip(a.tr(name)).Size(30, 30).Radius(15).Background(color).Border(3, ui.Transparent)
				if a.prefs.Accent == name {
					checkColor := c.Theme().AccentText
					if name == model.DefaultAccent {
						checkColor = lightPalette.onAccent
					}
					b.Border(3, c.Theme().Text).Children(func() { ui.Text(c, "✓").TextColor(checkColor).FontSize(13) })
				}
				b.OnClick(func() { a.prefs.Accent = name; a.savePrefs() })
			}
		})
	})

}

func (a *app) enginePreferences(c *ui.Context) {
	ui.Column(c).FillWidth().Gap(18).Disabled(a.engineUpdating).Children(func() { a.enginePreferencesContent(c) })
}
func (a *app) enginePreferencesContent(c *ui.Context) {
	a.settingsErrorView(c)
	a.section(c, "Network and search", func() {
		a.preferenceRow(c, "Network proxy", func() {
			if a.selectValue(c, &a.draft.ProxyMode, []string{model.ProxySystem, model.ProxyDirect, model.ProxyManual}).Label(a.tr("Network proxy")).Width(165).Changed() {
				a.changeSetting(settingProxy)
			}
		})
		if a.draft.ProxyMode == model.ProxyManual {
			a.field(c, settingProxy, "代理地址", &a.draft.Proxy, "http://127.0.0.1:7890 / socks5://127.0.0.1:1080", true)
		}
		ui.Row(c).FillWidth().Gap(12).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Children(func() { a.field(c, settingTimeout, "搜索超时（秒）", &a.timeoutText, "5–180", false) })
			ui.Column(c).Grow(1).MinWidth(0).Children(func() { a.field(c, settingConcurrency, "最大并发数", &a.concurrencyText, "1–64", false) })
			ui.Column(c).Grow(1).MinWidth(0).Children(func() { a.field(c, settingCache, "缓存上限（MB）", &a.cacheText, "20–2048", false) })
		})
		button(c, a.tr("重启搜索引擎")).Disabled(a.starting || a.engineUpdating).OnClick(func() {
			if a.flushSettings() {
				a.startEngine(true)
			}
			a.refreshViews()
		})
	})
	a.section(c, "结果关键词过滤", func() {
		a.field(c, settingInclude, "包含任一关键词", &a.draft.Include, "可选，例如 4K 中字（空格分隔）", false)
		a.field(c, settingExclude, "排除关键词", &a.draft.Exclude, "可选，例如 预告 花絮（空格分隔）", false)
	})
	a.section(c, "插件扩展参数", func() {
		input := enamelField(c, ui.TextArea(c, &a.extensionsText)).Label(a.tr("插件扩展参数 JSON")).Placeholder("{}").Tooltip(a.tr("Only fill this in when required by the plugin documentation.")).FillWidth().Height(120).OnChange(func() { a.queueSetting(settingExtensions) })
		a.settingError(c, input, settingExtensions)
	})
}

func (a *app) aboutPreferences(c *ui.Context) {
	ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
		brandMark(c, 48)
		ui.Column(c).Gap(4).Children(func() { ui.Text(c, "PanReel").FontSize(22).FontWeight(650); muted(c, a.tr("Version %s", appVersion)) })
	})
	muted(c, a.tr("Native desktop search, powered by MyGo and PanSou."))
	muted(c, a.tr("%d link types · %d plugins · %d enabled channels", len(catalog.Drives), len(catalog.Current().Plugins), len(a.settings.Channels)))
	ui.Row(c).Gap(8).Children(func() {
		button(c, a.tr("Project source")).OnClick(func() { _ = mygo.Shell.OpenExternal("https://github.com/cuikaipeng/panreel") })
		button(c, "PanSou").OnClick(func() { _ = mygo.Shell.OpenExternal("https://github.com/fish2018/pansou") })
	})
	button(c, a.tr("Third-party licenses")).OnClick(func() {
		path, err := mygo.App.Path(mygo.PathResources)
		if err == nil {
			_ = mygo.Shell.OpenPath(path + "/licenses")
		}
	})
	muted(c, "PanSou · "+catalog.Current().Revision[:12])
}

func (a *app) section(c *ui.Context, title string, body func()) {
	ui.Column(c).FillWidth().Gap(12).Children(func() { ui.Text(c, a.tr(title)).FontSize(13).FontWeight(650); body() })
}
func (a *app) preferenceRow(c *ui.Context, title string, control func()) {
	ui.Row(c).FillWidth().Gap(16).AlignItems(ui.Center).Children(func() {
		ui.Text(c, a.tr(title)).FontSize(13).FontWeight(500).Grow(1).MinWidth(0)
		control()
	})
}
func (a *app) field(c *ui.Context, field, label string, value *string, placeholder string, secret bool) {
	ui.Column(c).FillWidth().Gap(6).Children(func() {
		muted(c, a.tr(label))
		e := enamelField(c, ui.TextInput(c, value)).Label(a.tr(label)).Placeholder(a.tr(placeholder)).FillWidth().OnChange(func() { a.queueSetting(field) })
		a.settingError(c, e, field)
		if secret {
			e.Password()
		}
	})
}
