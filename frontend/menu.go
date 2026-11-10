package frontend

import (
	"github.com/egoist/mygo"
)

// appMenu is the menu bar of the app. Its items act on the window that has
// the focus, or on the app as a whole.
func appMenu(actions *appActions) *mygo.Menu {
	return mygo.NewMenu([]*mygo.MenuItem{
		{Role: mygo.RoleAppMenu, Submenu: []*mygo.MenuItem{
			{Label: "Settings…", Accelerator: "CmdOrCtrl+,", Click: func(*mygo.MenuItem, *mygo.Window) { go actions.openSettings("") }},
			mygo.Separator(),
			{Role: mygo.RoleQuit},
		}},
		{Label: "File", Submenu: []*mygo.MenuItem{
			{Label: "New Vault…", Accelerator: "CmdOrCtrl+N", Click: func(*mygo.MenuItem, *mygo.Window) { go actions.newVault() }},
			{Label: "Open Vault…", Accelerator: "CmdOrCtrl+O", Click: func(*mygo.MenuItem, *mygo.Window) { go actions.openVault() }},
			{Label: "Vault Picker…", Accelerator: "CmdOrCtrl+Shift+O", Click: func(*mygo.MenuItem, *mygo.Window) { go actions.showPicker() }},
			{Label: "Open Recent", Submenu: recentItems(actions)},
			mygo.Separator(),
			{Label: "Show Vault in Folder", Click: func(_ *mygo.MenuItem, w *mygo.Window) { actions.revealVault(w) }},
			{Label: "Connect to a Remote Folder…", Click: func(*mygo.MenuItem, *mygo.Window) { go actions.connect() }},
			{Label: "Sync Now", Accelerator: "CmdOrCtrl+Shift+S", Click: func(_ *mygo.MenuItem, w *mygo.Window) { actions.syncWindow(w) }},
			mygo.Separator(),
			{Role: mygo.RoleClose},
		}},
		{Role: mygo.RoleEditMenu},
		{Label: "View", Submenu: []*mygo.MenuItem{
			{Label: "Toggle Sidebar", Accelerator: "CmdOrCtrl+\\", Click: func(_ *mygo.MenuItem, w *mygo.Window) { sendViewCommand(actions.notebooks, w, "sidebar") }},
			{Label: "Toggle Dark Appearance", Accelerator: "CmdOrCtrl+Shift+D", Click: func(_ *mygo.MenuItem, w *mygo.Window) { sendViewCommand(actions.notebooks, w, "dark") }},
			mygo.Separator(),
			{Role: mygo.RoleViewMenu},
		}},
		{Role: mygo.RoleWindowMenu},
	})
}

// recentItems lists the vault folders opened before, most recent first.
func recentItems(actions *appActions) []*mygo.MenuItem {
	folders := actions.recentFolders()
	items := make([]*mygo.MenuItem, 0, len(folders))
	for _, folder := range folders {
		items = append(items, &mygo.MenuItem{
			Label: actions.recentLabel(folder),
			Click: func(*mygo.MenuItem, *mygo.Window) { go actions.openFolder(folder) },
		})
	}
	return items
}
