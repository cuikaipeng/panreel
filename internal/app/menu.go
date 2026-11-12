package app

import (
	"runtime"

	"github.com/egoist/mygo"
)

func (a *app) applicationMenu() *mygo.Menu {
	appItems := []*mygo.MenuItem{
		{ID: "about", Label: a.tr("About PanReel"), Click: func(*mygo.MenuItem, *mygo.Window) { a.openPreferencesTab(preferencesAbout) }},
		{ID: "check-updates", Label: a.tr("Check for updates"), Click: func(*mygo.MenuItem, *mygo.Window) {
			a.openPreferencesTab(preferencesUpdates)
			a.checkUpdates()
			a.checkEngineUpdate()
			a.refreshViews()
		}},
		mygo.Separator(),
		{ID: "preferences", Label: a.tr("Preferences") + "…", Click: func(*mygo.MenuItem, *mygo.Window) { a.openPreferences() }},
		mygo.Separator(),
	}
	windowItems := []*mygo.MenuItem{{Role: mygo.RoleMinimize}}
	if runtime.GOOS == "darwin" {
		appItems = append(appItems,
			&mygo.MenuItem{Role: mygo.RoleHide, Label: a.tr("Hide PanReel")},
			&mygo.MenuItem{Role: mygo.RoleHideOthers},
			&mygo.MenuItem{Role: mygo.RoleUnhide},
			mygo.Separator(),
		)
		windowItems = append(windowItems, &mygo.MenuItem{Role: mygo.RoleZoom})
	}
	appItems = append(appItems, &mygo.MenuItem{Role: mygo.RoleQuit, Label: a.tr("Quit PanReel")})
	windowItems = append(windowItems, &mygo.MenuItem{Role: mygo.RoleClose})
	if runtime.GOOS == "darwin" {
		windowItems = append(windowItems, mygo.Separator(), &mygo.MenuItem{Role: mygo.RoleFront})
	}

	return mygo.NewMenu([]*mygo.MenuItem{
		{Label: "PanReel", Submenu: appItems},
		// PanReel has plain text fields, so expose only applicable editing
		// actions. Native roles keep validation tied to the focused field.
		{Role: mygo.RoleEditMenu, Label: a.tr("Edit"), Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleUndo}, {Role: mygo.RoleRedo},
			mygo.Separator(),
			{Role: mygo.RoleCut}, {Role: mygo.RoleCopy}, {Role: mygo.RolePaste}, {Role: mygo.RoleDelete},
			mygo.Separator(), {Role: mygo.RoleSelectAll},
		}},
		{Role: mygo.RoleWindowMenu, Label: a.tr("Window"), Submenu: windowItems},
	})
}
