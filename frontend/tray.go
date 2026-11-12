package frontend

import (
	"context"
	_ "embed"
	"log"
	"runtime"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"

	"notes/internal/mcpsvc"
	"notes/internal/settings"
)

// The tray icon keeps Notes in the menu bar (macOS) or the notification area
// (Windows, Linux). Left click shows the windows, or hides them when they are
// up; right click opens the menu. Closing a window hides it to the tray, so
// the vaults stay open and the MCP server keeps answering, unless the settings
// say a close quits.

//go:embed tray/tray-template.png
var trayTemplateIcon []byte

//go:embed tray/tray-color.png
var trayColorIcon []byte

// quickTarget is the vault a new page goes into from the tray: the window in
// front, else a visible one, else the last vault opened. Empty when there is
// none. `visible` lists open vaults in front first; `recent` is most recent first.
func quickTarget(visible, recent []string) string {
	if len(visible) > 0 {
		return visible[0]
	}
	if len(recent) > 0 {
		return recent[0]
	}
	return ""
}

// dockHidden says whether the macOS Dock icon is hidden: only when the user
// asked for it, with the tray icon shown, and no window is open.
func dockHidden(hideDock, trayShown, anyVisible bool) bool {
	return hideDock && trayShown && !anyVisible
}

// loadSettings reads the settings file; a missing or unreadable one is empty.
func (a *appActions) loadSettings() settings.Settings {
	st, err := settings.Load(a.settings)
	if err != nil {
		log.Printf("notes: settings: %v", err)
	}
	return st
}

func (a *appActions) saveSettings(st settings.Settings) {
	if err := st.Save(a.settings); err != nil {
		log.Printf("notes: settings: %v", err)
	}
}

// setTrayShown puts the tray icon up or takes it away, as the settings say
// after they were saved. Taking it away lets the Dock come back.
func (a *appActions) setTrayShown(on bool) {
	if on == (a.tray != nil) {
		return
	}
	if on {
		a.installTray()
		return
	}
	a.tray.Destroy()
	a.tray = nil
	a.applyDock()
}

// installTray puts the tray icon up unless the settings take it away.
func (a *appActions) installTray() {
	if a.loadSettings().HideTray {
		return
	}
	mac := runtime.GOOS == "darwin"
	icon := trayColorIcon
	if mac {
		icon = trayTemplateIcon
	}
	tray, err := mygo.NewTray(mygo.TrayOptions{ID: "notes", Icon: icon, IconIsTemplate: mac, ToolTip: appName})
	if err != nil {
		// Without the tray (on Linux, without AppIndicator) the app runs as before.
		log.Printf("notes: tray: %v", err)
		return
	}
	a.tray = tray
	tray.OnClick(a.toggleWindows)
	tray.OnRightClick(a.popTrayMenu)
}

// anyWindowVisible says whether a vault window is on the screen.
func (a *appActions) anyWindowVisible() bool {
	for _, s := range a.notebooks.sessionList() {
		if s.window.IsVisible() {
			return true
		}
	}
	return false
}

// visibleFolders lists the folders of the vaults on screen, the window in
// front first.
func (a *appActions) visibleFolders() []string {
	var front, rest []string
	for _, s := range a.notebooks.sessionList() {
		if !s.window.IsVisible() {
			continue
		}
		if s.window.IsFocused() {
			front = append(front, s.vault.Info().Path)
		} else {
			rest = append(rest, s.vault.Info().Path)
		}
	}
	return append(front, rest...)
}

// toggleWindows shows the windows, or hides them to the tray when they are up.
func (a *appActions) toggleWindows() {
	if a.anyWindowVisible() {
		for _, s := range a.notebooks.sessionList() {
			if s.window.IsVisible() {
				a.notebooks.hideVault(s)
			}
		}
		return
	}
	a.bringForward()
	a.applyDock()
}

// keepsInTray says whether closing a window hides it: the tray icon is shown,
// and the settings do not ask a close to quit.
func (a *appActions) keepsInTray() bool {
	return a.tray != nil && !a.loadSettings().QuitOnClose
}

// applyDock hides or shows the macOS Dock icon as the settings ask.
func (a *appActions) applyDock() {
	if runtime.GOOS != "darwin" || mygo.App.Dock == nil {
		return
	}
	if dockHidden(a.loadSettings().HideDock, a.tray != nil, a.anyWindowVisible()) {
		mygo.App.Dock.Hide()
		return
	}
	mygo.App.Dock.Show()
}

// popTrayMenu shows the menu at the icon. The menu is set only while it is
// open, so a left click keeps its own meaning.
func (a *appActions) popTrayMenu() {
	if a.tray == nil {
		return
	}
	a.tray.SetMenu(a.buildTrayMenu())
	a.tray.PopUpMenu()
	a.tray.SetMenu(nil)
}

// buildTrayMenu is the tray's menu: the windows, new pages and vaults, the MCP
// server, settings, updates and quit.
func (a *appActions) buildTrayMenu() *mygo.Menu {
	st := a.loadSettings()
	show := tr("Show Notes")
	if a.anyWindowVisible() {
		show = tr("Hide Notes")
	}
	items := []*mygo.MenuItem{
		{Label: show, Accelerator: a.hotkey, Click: func(*mygo.MenuItem, *mygo.Window) { a.toggleWindows() }},
		mygo.Separator(),
		{Label: tr("New Page"), Click: func(*mygo.MenuItem, *mygo.Window) { a.quickNewPage() }},
		{Label: tr("Open Vault…"), Click: func(*mygo.MenuItem, *mygo.Window) { go a.openVault() }},
		{Label: tr("New Vault…"), Click: func(*mygo.MenuItem, *mygo.Window) { go a.newVault() }},
		{Label: tr("Open Recent"), Submenu: a.trayRecentItems()},
	}
	if w := a.activeRemoteWindow(); w != nil {
		items = append(items, &mygo.MenuItem{Label: tr("Sync Now"), Click: func(*mygo.MenuItem, *mygo.Window) { a.syncWindow(w) }})
	}
	items = append(items,
		mygo.Separator(),
		a.trayMCPItem(st),
		mygo.Separator(),
		&mygo.MenuItem{Label: tr("Settings…"), Click: func(*mygo.MenuItem, *mygo.Window) { go a.openSettings("") }},
		updater.MenuItem(),
		mygo.Separator(),
		&mygo.MenuItem{Label: tr("Quit Notes"), Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() }},
	)
	return mygo.NewMenu(items)
}

// trayRecentItems lists the recent vaults that still exist, and clears the list.
func (a *appActions) trayRecentItems() []*mygo.MenuItem {
	items := recentItems(a)
	if len(items) == 0 {
		return []*mygo.MenuItem{{Label: tr("No recent vaults"), Disabled: true}}
	}
	return append(items, mygo.Separator(), &mygo.MenuItem{Label: tr("Clear Recent Vaults"), Click: func(*mygo.MenuItem, *mygo.Window) {
		st := a.loadSettings()
		st.Recent = nil
		a.saveSettings(st)
		a.refreshMenu()
	}})
}

// activeRemoteWindow is the window in front, or a visible one, when its vault
// is kept in a remote folder: only then does the tray offer a sync.
func (a *appActions) activeRemoteWindow() *mygo.Window {
	for _, folder := range a.visibleFolders() {
		if _, ok := remoteOf(folder); ok {
			if s := a.notebooks.sessionAt(folder); s != nil {
				return s.window
			}
		}
	}
	return nil
}

// trayMCPItem is the MCP server's submenu: its state, switching it on and off,
// the prompt, and read only.
func (a *appActions) trayMCPItem(st settings.Settings) *mygo.MenuItem {
	toggle := tr("Turn On MCP Server")
	if st.MCP.Enabled {
		toggle = tr("Turn Off MCP Server")
	}
	return &mygo.MenuItem{Label: tr("MCP Server"), Submenu: []*mygo.MenuItem{
		{Label: a.mcp.status(st), Disabled: true},
		mygo.Separator(),
		{Label: toggle, Click: func(*mygo.MenuItem, *mygo.Window) { a.setMCPEnabled(!st.MCP.Enabled) }},
		{Label: tr("Copy the prompt"), Disabled: st.MCP.Key == "", Click: func(*mygo.MenuItem, *mygo.Window) {
			mygo.Clipboard.WriteText(mcpPrompt(a.loadSettings()))
		}},
		{Label: tr("Read only"), Type: mygo.MenuItemCheckbox, Checked: st.MCP.ReadOnly, Click: func(*mygo.MenuItem, *mygo.Window) {
			cur := a.loadSettings()
			cur.MCP.ReadOnly = !cur.MCP.ReadOnly
			a.saveSettings(cur)
			a.refreshSettingsView()
		}},
	}}
}

// setMCPEnabled switches the MCP server on or off, making its key the first time.
func (a *appActions) setMCPEnabled(on bool) {
	st := a.loadSettings()
	st.MCP.Enabled = on
	if on && st.MCP.Key == "" {
		st.MCP.Key = mcpsvc.NewKey()
	}
	a.saveSettings(st)
	a.applyMCP()
	a.refreshSettingsView()
}

// refreshSettingsView redraws the settings window, when it is open, from the
// settings as they are now.
func (a *appActions) refreshSettingsView() {
	if a.settingsWin != nil && !a.settingsWin.IsDestroyed() && a.settingsView != nil {
		a.settingsView.refresh()
	}
}

// quickNewPage makes a page in the vault quickTarget picks, and shows its window.
// The page itself is made by the page: the command waits for it there.
func (a *appActions) quickNewPage() {
	target := quickTarget(a.visibleFolders(), a.recentExisting())
	if target == "" {
		a.showPicker()
		return
	}
	if a.notebooks.sessionAt(target) == nil {
		if err := a.open(target); err != nil {
			mygo.Dialog.Error(appName, tr("The folder could not be opened as a vault: %v", err))
			return
		}
	}
	s := a.notebooks.sessionAt(target)
	if s == nil {
		return
	}
	a.raiseWindow(s)
	a.notebooks.command(s, "newPage")
}

// recentExisting lists the recent vault folders that are still there.
func (a *appActions) recentExisting() []string {
	var out []string
	for _, folder := range a.recentFolders() {
		if isDirectory(folder) {
			out = append(out, folder)
		}
	}
	return out
}

// raiseWindow shows a vault's window, restored and in front, and fixes the Dock.
func (a *appActions) raiseWindow(s *session) {
	if s.window.IsMinimized() {
		s.window.Restore()
	}
	s.window.Show()
	s.window.Focus()
	a.applyDock()
}

// applyHotkey registers the shortcut that shows or hides the windows, and
// removes the old one. An empty shortcut means none.
func (a *appActions) applyHotkey(acc string) error {
	acc = strings.TrimSpace(acc)
	if acc == a.hotkey {
		return nil
	}
	if a.hotkey != "" {
		mygo.GlobalShortcut.Unregister(a.hotkey)
		a.hotkey = ""
	}
	if acc == "" {
		return nil
	}
	if err := mygo.GlobalShortcut.Register(acc, a.toggleWindows); err != nil {
		return err
	}
	a.hotkey = acc
	return nil
}

// hideVault hides a vault's window to the tray. The vault stays open, and is
// saved, so the window shows it as it is when it comes back.
func (n *Notebooks) hideVault(s *session) {
	s.window.Hide()
	goSafe("save", func() { _ = s.vault.Flush() })
	if n.onVisibility != nil {
		n.onVisibility()
	}
}

// shouldHide says whether closing w hides it to the tray, rather than closing
// it: the app is not quitting, the tray keeps the app, and w is not replaced.
func (n *Notebooks) shouldHide(w *mygo.Window) bool {
	if n.leaving.Load() || n.keepInTray == nil || !n.keepInTray() {
		return false
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.forceClose[w] {
		delete(n.forceClose, w)
		return false
	}
	return true
}

// command asks a vault's page to do cmd. It is kept until the page takes it,
// so a page that is still loading gets it too.
func (n *Notebooks) command(s *session, cmd string) {
	n.mu.Lock()
	s.pending = cmd
	n.mu.Unlock()
	_ = ViewCommand.Emit(s.window, "pending")
}

// TakeCommand is the command the window's page has been asked to do, if any,
// and it is taken: a command is done once.
func (n *Notebooks) TakeCommand(ctx context.Context) (string, error) {
	s, err := n.callerSession(ctx)
	if err != nil {
		return "", err
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	cmd := s.pending
	s.pending = ""
	return cmd, nil
}
