package app

// Adapted from Kivrix (MIT). See resources/licenses/kivrix.txt.

// Portions of the desktop layout and controls are adapted from kero.
// Copyright (c) 2026 EGOIST. MIT License; see resources/licenses/kero.txt.

// PanReel uses pale porcelain surfaces, mist-blue enamel controls and a compact library bar.

import (
	"github.com/cuikaipeng/panreel/internal/model"
	"github.com/egoist/mygo/ui"
)

// The default accent is a cool blue derived from the logo. Text on a light
// surface uses a darker ink tone to stay legible over selected backgrounds.
var mistBlue = ui.Hex("#4575c6")
var mistBlueLight = mistBlue
var mistBlueDark = ui.Hex("#8eafe6")
var mistBlueInk = ui.Hex("#365fa5")

const windowBackground = "light-dark(#f5f6f9, #171d27)"

type palette struct {
	dark bool

	canvas ui.Color
	card   ui.Color // the working surface
	text   ui.Color
	muted  ui.Color
	faint  ui.Color

	accent    ui.Color
	accentInk ui.Color
	onAccent  ui.Color
	danger    ui.Color
	warning   ui.Color
	success   ui.Color

	selection   ui.Color
	rowHover    ui.Color
	control     ui.Color
	controlDown ui.Color
	divider     ui.Color
}

var lightPalette = palette{
	canvas: ui.Hex("#f5f6f9"),
	card:   ui.Hex("#ffffff"),
	text:   ui.Hex("#263440"),
	muted:  ui.Hex("#637184"),
	faint:  ui.Hex("#728096"),

	accent:    mistBlueLight,
	accentInk: mistBlueInk,
	onAccent:  ui.Hex("#ffffff"),
	danger:    ui.Hex("#d93a35"),
	warning:   ui.Hex("#c27b0f"),
	success:   ui.Hex("#1f8f4e"),

	selection:   mistBlueLight.Alpha(.15),
	rowHover:    ui.Hex("#eaf0f8"),
	control:     ui.Hex("#ffffff"),
	controlDown: ui.Hex("#e7edf5"),
	divider:     ui.Hex("#dbe2ec"),
}

var darkPalette = palette{
	canvas: ui.Hex("#171d27"),
	dark:   true,
	card:   ui.Hex("#202937"),
	text:   ui.Hex("#e9eef6"),
	muted:  ui.Hex("#afbbce"),
	faint:  ui.Hex("#9ba9bf"),

	accent:    mistBlueDark,
	accentInk: mistBlueDark,
	onAccent:  ui.Hex("#17233e"),
	danger:    ui.Hex("#ff6961"),
	warning:   ui.Hex("#f0a33a"),
	success:   ui.Hex("#4fd17a"),

	selection:   mistBlueDark.Alpha(.15),
	rowHover:    ui.Hex("#2b384c"),
	control:     ui.Hex("#263244"),
	controlDown: ui.Hex("#202b3b"),
	divider:     ui.Hex("#39485e"),
}

// accentColors are the appearance presets, with a brighter dark variant.
var accentColors = map[string]struct{ light, dark ui.Color }{
	model.DefaultAccent: {mistBlueLight, mistBlueDark},
	"Teal":              {ui.Hex("#167b6b"), ui.Hex("#65cbb6")},
	"Blue":              {ui.Hex("#3b6cf6"), ui.Hex("#6b93ff")},
	"Violet":            {ui.Hex("#7c5cf6"), ui.Hex("#a78bfa")},
	"Green":             {ui.Hex("#0f9f6e"), ui.Hex("#34d399")},
	"Orange":            {ui.Hex("#d97706"), ui.Hex("#fbbf24")},
	"Red":               {ui.Hex("#dc3a4a"), ui.Hex("#f87171")},
	"Pink":              {ui.Hex("#db2777"), ui.Hex("#f472b6")},
}

func accentColor(name string, dark bool) ui.Color {
	if a, ok := accentColors[name]; ok {
		if dark {
			return a.dark
		}
		return a.light
	}
	if dark {
		return darkPalette.accent
	}
	return lightPalette.accent
}

func borderless(c *ui.Context, pal *palette) *ui.Theme {
	t := *c.Theme()
	t.Dark = pal.dark
	t.Background = pal.card
	t.Text = pal.text
	t.TextMuted = pal.muted
	t.Accent = pal.accent
	t.AccentText = pal.onAccent
	interaction := ui.Hex("#ffffff")
	if pal.onAccent == ui.Hex("#ffffff") {
		interaction = ui.Hex("#000000")
	}
	t.AccentHover = pal.accent.Mix(interaction, .06)
	t.AccentPressed = pal.accent.Mix(interaction, .12)
	t.Danger = pal.danger
	t.Warning = pal.warning
	t.Success = pal.success
	t.Selection = pal.selection
	t.Focus = pal.accent.Alpha(0.6)
	t.Border = pal.divider
	t.Surface = pal.control
	t.SurfaceHover = pal.control.Mix(pal.accent, .055)
	t.SurfacePressed = pal.controlDown
	t.Inverse = pal.text
	t.InverseText = pal.card
	t.Scrollbar = pal.faint.Alpha(0.55)
	t.Radius = 8
	t.Spacing = 4
	t.FontSize = 13
	return &t
}

func (a *app) paletteFor(c *ui.Context) *palette {
	p := lightPalette
	if a.prefs.Theme == "dark" || (a.prefs.Theme != "light" && c.Theme().Dark) {
		p = darkPalette
	}
	p.accent = accentColor(a.prefs.Accent, p.dark)
	p.accentInk = p.accent
	if a.prefs.Accent == model.DefaultAccent && !p.dark {
		p.accentInk = mistBlueInk
	}
	p.selection = p.accent.Alpha(.15)
	return &p
}
func (a *app) theme(c *ui.Context) *palette {
	p := a.paletteFor(c)
	c.SetTheme(borderless(c, p))
	return p
}

// Line icons include Lucide/Feather geometry adapted through Kivrix.
// See resources/licenses/lucide.txt for the ISC and MIT notices.
func icon(body string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">` + body + `</svg>`))
}

var searchIcon = icon(`<circle cx="10" cy="10" r="6"/><path d="m15 15 6 6"/>`)
var bookmarkIcon = icon(`<path d="M6 3h12v18l-6-4-6 4z"/>`)
var checkIcon = icon(`<path d="m5 12 4 4L19 6"/>`)
var alertIcon = icon(`<circle cx="12" cy="12" r="9"/><path d="M12 7v6m0 4h.01"/>`)
var historyIcon = icon(`<circle cx="12" cy="12" r="9"/><path d="M12 7v5l4 2"/>`)
var sourcesIcon = icon(`<path d="M7 4h10v6H7zM3 16h6v5H3zm12 0h6v5h-6zM12 10v3m-6 3v-3h12v3"/>`)
var settingsIcon = icon(`<path d="M3 7h18M3 17h18"/><circle cx="8" cy="7" r="3"/><circle cx="16" cy="17" r="3"/>`)
var updateIcon = icon(`<path d="M12 3v12m-4-4 4 4 4-4M5 16v4h14v-4"/>`)
var engineIcon = icon(`<rect x="6" y="6" width="12" height="12" rx="2"/><path d="M9 2v4m6-4v4M9 18v4m6-4v4M2 9h4m-4 6h4m12-6h4m-4 6h4"/><path d="M10 10h4v4h-4z"/>`)

func muted(c *ui.Context, text string) {
	ui.Text(c, text).FontSize(13).TextColor(c.Theme().TextMuted).Wrap()
}
func card(c *ui.Context, fn func()) ui.Element {
	return enamelPanel(c, ui.Column(c).FillWidth().Padding(20).Gap(12).Radius(10)).Children(fn)
}
func badge(c *ui.Context, text string) {
	t := c.Theme()
	ui.Text(c, text).FontSize(11.5).TextColor(t.TextMuted).Padding(3, 7).Radius(4).Background(t.Selection).AlignSelf(ui.Start)
}

var minimizeIcon = icon(`<path d="M4 12h16"/>`)
var maximizeIcon = icon(`<rect x="5" y="5" width="14" height="14" rx="1"/>`)
var restoreIcon = icon(`<path d="M9 5h10v10M5 9h10v10H5z"/>`)
var closeIcon = icon(`<path d="m6 6 12 12M6 18 18 6"/>`)

// Shared controls keep the library and settings visually consistent.
func button(c *ui.Context, label string) ui.Element        { return actionButton(c, label, false) }
func primaryButton(c *ui.Context, label string) ui.Element { return actionButton(c, label, true) }
func actionButton(c *ui.Context, label string, primary bool) ui.Element {
	b := enamelButton(c, ui.ButtonBase(c).Label(label).Height(32).Padding(0, 12).Radius(8).Center(), primary)
	b.Children(func() { ui.Text(c, label).FontSize(13).FontWeight(550).SingleLine() })
	return b
}

// These are native control surfaces, not raster artwork. Keep a stable border
// and footprint in every state so pressing and hovering cannot move content.
// Cast shadows stay dark in both themes; deriving them from Text makes dark
// surfaces glow. Panels and selected tabs use less elevation than buttons.
func enamelShadow(c *ui.Context) ui.Color {
	if c.Theme().Dark {
		return ui.RGBA(0, 0, 0, .16)
	}
	return ui.RGBA(31, 53, 86, .055)
}

func enamelButton(c *ui.Context, e ui.Element, primary bool) ui.Element {
	t := c.Theme()
	face, ink, edge := t.Surface, t.Text, t.Border
	highlight, shade := ui.RGBA(255, 255, 255, .72), ui.RGBA(25, 47, 80, .055)
	if t.Dark {
		highlight, shade = ui.RGBA(255, 255, 255, .06), ui.RGBA(0, 0, 0, .16)
	}
	if primary {
		face, ink, edge = t.Accent, t.AccentText, t.Accent.Mix(t.AccentText, .15)
		highlight = ui.RGBA(255, 255, 255, .20)
		if e.Pressed() {
			face = t.AccentPressed
		} else if e.Hovered() {
			face = t.AccentHover
		}
	} else if e.Pressed() {
		face = t.SurfacePressed
	} else if e.Hovered() {
		face, edge = t.SurfaceHover, t.Border.Mix(t.Accent, .25)
	}
	bottom := face.Mix(ui.Hex("#000000"), .018)
	if primary {
		bottom = face.Mix(ui.Hex("#000000"), .06)
	}
	e.NoShadow().Gradient(face, bottom, 180).Border(1, edge).TextColor(ink)
	if e.Pressed() {
		return e.InsetShadow(0, 1, 2, 0, shade).InsetShadow(0, -1, 0, 0, highlight.Alpha(.12))
	}
	// A border plus one contact shadow defines the lower edge. Adding another
	// inset seam here makes small controls look like they have a double border.
	return e.InsetShadow(0, 1, 0, 0, highlight).
		Shadow(0, 1, 1.5, 0, enamelShadow(c))
}

func enamelField(c *ui.Context, e ui.Element) ui.Element {
	t := c.Theme()
	shade, highlight := ui.RGBA(31, 53, 86, .035), ui.RGBA(255, 255, 255, .45)
	if t.Dark {
		shade, highlight = ui.RGBA(0, 0, 0, .14), ui.RGBA(255, 255, 255, .025)
	}
	return e.NoShadow().Background(t.Surface).Border(1, t.Border).Radius(8).
		InsetShadow(0, 1, 2, 0, shade).InsetShadow(0, -1, 0, 0, highlight)
}

func enamelPanel(c *ui.Context, e ui.Element) ui.Element {
	t := c.Theme()
	highlight := ui.RGBA(255, 255, 255, .65)
	if t.Dark {
		highlight = ui.RGBA(255, 255, 255, .04)
	}
	return e.NoShadow().Background(t.Surface).Border(1, t.Border).
		InsetShadow(0, 1, 0, 0, highlight).Shadow(0, 1, 2, 0, enamelShadow(c).Alpha(.6))
}

func enamelTab(c *ui.Context, e ui.Element, selected bool, accentInk ui.Color) ui.Element {
	t := c.Theme()
	e.NoShadow().Border(1, ui.Transparent).Background(ui.Transparent).TextColor(t.TextMuted)
	if selected {
		top, bottom := t.Surface.Mix(t.Accent, .055), t.Surface.Mix(t.Accent, .14)
		if e.Pressed() {
			top = bottom
		} else if e.Hovered() {
			top, bottom = top.Mix(t.Accent, .025), bottom.Mix(t.Accent, .025)
		}
		e.Gradient(top, bottom, 180).Border(1, t.Accent.Alpha(.30)).TextColor(accentInk)
		if e.Pressed() {
			return e.InsetShadow(0, 1, 2, 0, enamelShadow(c))
		}
		return e.InsetShadow(0, 1, 0, 0, t.Surface.Alpha(.6)).
			Shadow(0, 1, 1.5, 0, enamelShadow(c).Alpha(.6))
	}
	if e.Pressed() {
		return e.Background(t.SurfacePressed).TextColor(t.Text)
	}
	if e.Hovered() {
		return e.Background(t.SurfaceHover).TextColor(t.Text)
	}
	return e
}
