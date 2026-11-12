package app

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"runtime"
)

func framed(o mygo.WindowOptions) mygo.WindowOptions {
	o.TitleBarHeight = topBarH
	if runtime.GOOS == "darwin" {
		o.TitleBarStyle = mygo.TitleBarHiddenInset
		o.TrafficLightPosition = &mygo.Point{X: 18, Y: topBarH/2 - 7}
	} else {
		o.TitleBarStyle = mygo.TitleBarHidden
		o.HideWindowButtons = true
		o.AutoHideMenuBar = true
	}
	return o
}

func (a *app) openPreferences() {
	a.prefsOpen = true
	if a.window == nil {
		return
	}
	if a.settingsWin != nil {
		a.settingsWin.Invalidate()
		a.settingsWin.Show()
		a.settingsWin.Focus()
		return
	}
	opts := mygo.WindowOptions{Title: a.tr("Preferences"), Width: 840, Height: 650, MinWidth: 760, MinHeight: 560, StateKey: "preferences", BackgroundColor: windowBackground, Content: ui.View(a.preferencesView)}
	// Windows keeps standard controls on the secondary window.
	if runtime.GOOS == "darwin" {
		opts = framed(opts)
	}
	a.settingsWin = mygo.NewWindow(opts)
	a.settingsWin.OnClose(func(e *mygo.CloseEvent) {
		if !a.flushSettings() {
			e.PreventDefault()
		}
	})
	a.settingsWin.OnClosed(func() { a.settingsWin = nil; a.prefsOpen = false })
}

func (a *app) openPreferencesTab(tab int) {
	a.prefsTab = tab
	a.openPreferences()
}

func (a *app) refreshViews() {
	if a.closed {
		return
	}
	if a.window != nil {
		a.window.Invalidate()
	}
	if a.settingsWin != nil {
		a.settingsWin.Invalidate()
	}
}

func (a *app) applyAppearance() {
	if a.window == nil {
		return
	}
	source := mygo.ThemeSource(a.prefs.Theme)
	if mygo.Theme.Source() != source {
		mygo.Theme.SetSource(source)
	}
}

func (a *app) savePrefs() {
	a.prefs.Normalize()
	a.flushPrefs()
	a.applyAppearance()
	a.applyLocale()
	a.refreshViews()
}

func (a *app) flushPrefs() {
	if a.store != nil {
		if err := a.store.SavePreferences(a.prefs); err != nil {
			a.noticef("Could not save preferences: %s", err)
		}
	}
}
