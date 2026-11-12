package app

// Native window controls retain portions adapted from Kivrix/kero (MIT).
import (
	"github.com/egoist/mygo/ui"
	"runtime"
)

const topBarH = 48
const cardRadius = 10

func (a *app) shell(c *ui.Context, pal *palette, content func()) {
	c.Root().Background(pal.canvas)
	ui.Column(c).Fill().Children(func() {
		bar := c.TitleBar()
		width, _ := c.Size()
		compact := width < 1100
		height := max(float32(topBarH), bar.Height)
		ui.Row(c.Key("window-header")).FillWidth().Height(height).Shrink(0).
			Gradient(pal.card, pal.canvas, 180).
			Padding(0, max(bar.Right+12, 16), 0, max(bar.Left+12, 18)).Gap(10).AlignItems(ui.Center).DragWindow().Children(func() {
			ui.Row(c).Gap(7).AlignItems(ui.Center).Shrink(0).Tooltip("PanReel").Children(func() {
				brandMark(c, 24)
				if !compact {
					ui.Text(c, "PanReel").FontSize(14).FontWeight(700).TextColor(pal.text).SingleLine()
				}
			})
			ui.Box(c).Width(1).Height(18).Margin(0, 4).Background(pal.divider)
			a.navigationTabs(c, pal, height)
			ui.Spacer(c)
			a.engineStateView(c, pal, compact)
			a.updateNoticeView(c, pal)
			a.engineUpdateNoticeView(c, pal)
			iconButton(c, "prefs", settingsIcon, a.tr("Preferences"), pal, a.openPreferences)
			a.windowControls(c, pal)
		})
		ui.Box(c).FillWidth().Height(1).Background(pal.divider)
		ui.Column(c).Fill().MinHeight(0).Children(content)
	})
}

func (a *app) navigationTabs(c *ui.Context, pal *palette, height float32) {
	selected := 0
	for i, page := range pages {
		if page.id == a.page {
			selected = i
			break
		}
	}
	tabs := ui.TabsBase(c.Key("main-navigation"), &selected, len(pages))
	tabs.List.Gap(5).Height(height).AlignItems(ui.Center).Shrink(0).Children(func() {
		for i, page := range pages {
			on := i == selected
			tab := enamelTab(c, tabs.Tab(i).Label(a.tr(page.title)).Height(34).Padding(0, 10).Radius(8).Center(), on, pal.accentInk)
			weight := 500
			if on {
				weight = 650
			}
			tab.Children(func() {
				ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
					glyph := ui.Icon(c, page.icon).Size(15, 15)
					if on {
						glyph.TextColor(pal.accentInk)
					}
					ui.Text(c, a.tr(page.title)).FontSize(13).FontWeight(weight).SingleLine()
					if page.id == "saved" && len(a.saved) > 0 {
						ui.Textf(c, "%d", len(a.saved)).FontSize(11).TextColor(pal.muted).SingleLine()
					}

				})
			})
		}
	}).OnChange(func() { a.navigate(pages[selected].id) })
}

func (a *app) engineStateView(c *ui.Context, pal *palette, compact bool) {
	state, color := "Engine offline", pal.faint
	if a.starting || a.engineRestartPending {
		state = "Starting engine…"
	} else if a.online {
		state, color = "Engine ready", pal.success
		if len(a.settings.Channels) == 0 {
			state, color = "Waiting for TG channels", pal.warning
		} else if len(a.settings.EnabledDrives()) == 0 {
			state, color = "Waiting for drive types", pal.warning
		}
	}
	ui.Row(c).Height(26).Padding(0, 6).Gap(7).AlignItems(ui.Center).Shrink(0).Role(ui.RoleStatus).Label(a.tr(state)).Tooltip(a.tr(state)).Children(func() {
		ui.Box(c).Size(6, 6).Radius(3).Background(color)
		if !compact {
			ui.Text(c, a.tr(state)).FontSize(11).TextColor(pal.muted).SingleLine()
		}
	})
}

func (a *app) windowControls(c *ui.Context, pal *palette) {
	if runtime.GOOS == "darwin" || a.window == nil {
		return
	}
	ui.Row(c).Gap(2).AlignItems(ui.Center).Shrink(0).Margin(0, 0, 0, 2).Children(func() {
		iconButton(c, "win-min", minimizeIcon, a.tr("Minimize"), pal, a.window.Minimize)
		maximized := a.window.IsMaximized()
		glyph, label := maximizeIcon, "Maximize"
		if maximized {
			glyph, label = restoreIcon, "Restore"
		}
		label = a.tr(label)
		b := ui.ButtonBase(c.Key("win-max")).Size(28, 26).Radius(6).Center().Label(label).Tooltip(label)
		if b.Hovered() {
			b.Background(pal.rowHover)
		}
		b.TextColor(pal.muted).Children(func() {
			ui.Icon(c, glyph).Size(14, 14)
		})
		if b.Clicked() {
			if maximized {
				a.window.Unmaximize()
			} else {
				a.window.Maximize()
			}
		}
		b = ui.ButtonBase(c.Key("win-close")).Size(28, 26).Radius(6).Center().Label(a.tr("Close")).Tooltip(a.tr("Close"))
		if b.Hovered() || b.Pressed() {
			b.Background(pal.danger).TextColor(ui.Hex("#ffffff"))
		} else {
			b.TextColor(pal.muted)
		}
		b.Children(func() {
			ui.Icon(c, closeIcon).Size(14, 14)
		})
		if b.Clicked() {
			a.window.Close()
		}
	})
}

// iconButton is a quiet square button for a toolbar.
func iconButton(c *ui.Context, key string, svg *ui.SVG, label string, pal *palette, fn func()) ui.Element {
	b := ui.ButtonBase(c.Key(key)).Size(28, 26).Radius(6).Center().
		Label(label).Tooltip(label).Border(1, ui.Transparent)
	switch {
	case b.Pressed():
		b.Background(c.Theme().SurfacePressed).TextColor(pal.text)
	case b.Hovered():
		b.Background(c.Theme().SurfaceHover).TextColor(pal.text)
	default:
		b.TextColor(pal.muted)
	}
	b.Children(func() {
		ui.Icon(c, svg).Size(15, 15)
	})
	if b.Clicked() {
		fn()
	}
	return b
}
