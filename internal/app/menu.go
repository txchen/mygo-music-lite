package app

import (
	"runtime"

	"github.com/egoist/mygo"
)

// setMenu gives the app its menu bar, in the listener's language and named as
// the app is, with the settings in the app menu and the player's controls in a
// menu of their own. A change of language sets it again.
func (a *app) setMenu() {
	run := func(fn func()) func(*mygo.MenuItem, *mygo.Window) {
		return func(*mygo.MenuItem, *mygo.Window) {
			fn()
			a.redraw()
		}
	}
	name := mygo.App.Name()
	appMenu := []*mygo.MenuItem{
		{Role: mygo.RoleAbout, Label: Tf("About %s", name)},
		mygo.Separator(),
		{Label: T("Settings…"), Accelerator: "CmdOrCtrl+,", Click: run(a.openSettings)},
		mygo.Separator(),
	}
	// Services and hiding are macOS's alone. Elsewhere MyGo leaves their
	// items out but keeps the separators around them, which then stack.
	if runtime.GOOS == "darwin" {
		appMenu = append(appMenu,
			&mygo.MenuItem{Role: mygo.RoleServices, Label: T("Services")},
			mygo.Separator(),
			&mygo.MenuItem{Role: mygo.RoleHide, Label: Tf("Hide %s", name)},
			&mygo.MenuItem{Role: mygo.RoleHideOthers, Label: T("Hide Others")},
			&mygo.MenuItem{Role: mygo.RoleUnhide, Label: T("Show All")},
			mygo.Separator(),
		)
	}
	appMenu = append(appMenu, &mygo.MenuItem{Role: mygo.RoleQuit, Label: Tf("Quit %s", name)})
	mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
		{Label: name, Submenu: appMenu},
		{Label: T("File"), Submenu: []*mygo.MenuItem{
			{Label: T("New Playlist…"), Accelerator: "CmdOrCtrl+N", Click: run(func() {
				a.showMain()
				a.nowPlaying = false
				a.newPlaylist(nil)
			})},
		}},
		{Label: T("Edit"), Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleUndo, Label: T("Undo")},
			{Role: mygo.RoleRedo, Label: T("Redo")},
			mygo.Separator(),
			{Role: mygo.RoleCut, Label: T("Cut")},
			{Role: mygo.RoleCopy, Label: T("Copy")},
			{Role: mygo.RolePaste, Label: T("Paste")},
			{Role: mygo.RoleSelectAll, Label: T("Select All")},
		}},
		{Label: T("Controls"), Submenu: []*mygo.MenuItem{
			{Label: T("Play/Pause"), Click: run(func() { a.pl.TogglePlay() })},
			{Label: T("Next"), Accelerator: "CmdOrCtrl+Right", Click: run(func() { a.pl.Next() })},
			{Label: T("Previous"), Accelerator: "CmdOrCtrl+Left", Click: run(func() { a.pl.Prev() })},
			mygo.Separator(),
			{Label: T("Like/Unlike"), Accelerator: "CmdOrCtrl+D", Click: run(a.likeCurrent)},
			mygo.Separator(),
			{Label: T("Shuffle"), Click: run(func() { a.pl.ToggleShuffle() })},
			{Label: T("Change Repeat Mode"), Click: run(func() { a.pl.CycleRepeat() })},
			mygo.Separator(),
			{Label: T("Now Playing"), Accelerator: "CmdOrCtrl+L", Click: run(func() {
				a.showMain()
				a.nowPlaying = !a.nowPlaying && a.pl.Current() != nil
			})},
		}},
		{Label: T("Window"), Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleMinimize, Label: T("Minimize")},
			{Role: mygo.RoleZoom, Label: T("Zoom")},
			{Role: mygo.RoleClose, Label: T("Close")},
			mygo.Separator(),
			{Label: name, Accelerator: "CmdOrCtrl+0", Click: run(a.showMain)},
			{Role: mygo.RoleFront, Label: T("Bring All to Front")},
		}},
	}))
}
