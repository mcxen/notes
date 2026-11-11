package frontend

import (
	"log"
	"os"
	"path/filepath"
	"runtime"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"

	"notes/internal/settings"
)

const appName = "Notes"

// Run starts the app: it binds the services, registers the vault:// scheme,
// and opens the last vault (or asks for one) once MyGo is ready.
func Run() error {
	app := mygo.App
	app.SetName(appName)
	// One process per user: two would each hold a vault in memory and save
	// over one another. A second launch hands over to the first and exits.
	if !app.RequestSingleInstanceLock() {
		return nil
	}

	// Looks for a signed release in the background a few seconds after launch,
	// then daily, and shows the release notes with Install / Later / Skip.
	mygo.Use(updater.Plugin)

	notebooks := newNotebooks()
	connections := &Connections{n: notebooks}
	mygo.Bind(notebooks, connections)
	if err := mygo.Protocol.Handle("vault", serveVaultFiles(notebooks)); err != nil {
		return err
	}

	actions := &appActions{notebooks: notebooks, settings: settingsPath()}
	connections.app = actions
	app.WhenReady(func() {
		if st, err := settings.Load(actions.settings); err == nil {
			applyUpdateSource(st.UpdateSource)
		}
		app.SetMenu(appMenu(actions))
		actions.startup()
	})
	app.OnSecondInstance(func([]string, string) { actions.bringForward() })
	// On macOS the app stays open after its last window closes, as Mac apps
	// do, and opens a vault again when the Dock icon is clicked. Elsewhere it
	// has nothing left to show, so it quits.
	if runtime.GOOS == "darwin" {
		app.OnWindowAllClosed(func() {})
	}
	app.OnActivate(func(hasVisibleWindows bool) {
		if !hasVisibleWindows {
			actions.startup()
		}
	})
	app.OnBeforeQuit(func(*mygo.QuitEvent) { notebooks.closeAll() })
	return app.Run()
}

// settingsPath is where the app keeps its settings file.
func settingsPath() string {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		log.Printf("notes: no user data folder: %v", err)
		dir = "."
	}
	return filepath.Join(dir, "settings.json")
}

// appActions are the app-wide actions: the menu's, and the first window.
type appActions struct {
	notebooks    *Notebooks
	settings     string
	connectWin   *mygo.Window
	pickerWin    *mygo.Window
	settingsWin  *mygo.Window
	settingsView *settingsWindow
	pickerView   *picker
}

// recentFolders are the vault folders opened before, most recent first.
func (a *appActions) recentFolders() []string {
	s, err := settings.Load(a.settings)
	if err != nil {
		return nil
	}
	return s.Recent
}

// recentLabel names a recent vault folder in the menu: a remote vault's
// working copy by its address, a local vault by its folder name.
func (a *appActions) recentLabel(folder string) string {
	if cfg, ok := remoteOf(folder); ok {
		return cfg.Address + " (remote)"
	}
	return filepath.Base(folder)
}

// refreshMenu rebuilds the menu, so Open Recent lists the latest vaults.
func (a *appActions) refreshMenu() {
	mygo.App.SetMenu(appMenu(a))
}

// openFolder shows a vault folder that was opened before.
func (a *appActions) openFolder(folder string) {
	if !isDirectory(folder) {
		mygo.Dialog.Error(appName, "This vault folder is no longer there.")
		return
	}
	if err := a.open(folder); err != nil {
		mygo.Dialog.Error(appName, "The folder could not be opened as a vault: "+err.Error())
	}
}

// openFromPicker opens a vault chosen in the picker, and closes the picker.
func (a *appActions) openFromPicker(p *picker, folder string) {
	if !isDirectory(folder) {
		mygo.Dialog.Error(appName, "This vault folder is no longer there.")
		return
	}
	if err := a.open(folder); err != nil {
		mygo.Dialog.Error(appName, "The folder could not be opened as a vault: "+err.Error())
		return
	}
	if p.win != nil {
		p.win.Close()
	}
}

// connect opens the window that keeps a vault in a remote folder.
func (a *appActions) connect() {
	if a.connectWin != nil && !a.connectWin.IsDestroyed() {
		a.connectWin.Focus()
		return
	}
	a.connectWin = mygo.NewWindow(connectWindowOptions())
	a.connectWin.OnClosed(func() { a.connectWin = nil })
}

// syncWindow syncs the vault of the window that asked, when it is kept in a
// remote folder.
func (a *appActions) syncWindow(w *mygo.Window) {
	a.notebooks.syncWindow(w)
}

// startup opens the last vault, or shows the picker when the settings ask for
// it, or when no vault can be opened.
func (a *appActions) startup() {
	st, err := settings.Load(a.settings)
	if err != nil {
		log.Printf("notes: settings: %v", err)
	}
	if !st.PickerAtLaunch {
		for _, folder := range st.Recent {
			if isDirectory(folder) {
				if err := a.open(folder); err == nil {
					return
				}
			}
		}
	}
	a.showPicker()
}

// bringForward shows the app's windows when it is launched again: the open
// vaults, or the first window as at launch when none is open.
func (a *appActions) bringForward() {
	windows := a.notebooks.windows()
	if len(windows) == 0 {
		a.startup()
		return
	}
	for _, w := range windows {
		if w.IsMinimized() {
			w.Restore()
		}
		w.Show()
	}
	windows[0].Focus()
}

// newVault asks for an empty or new folder and makes it a vault.
func (a *appActions) newVault() {
	a.chooseFolder("Choose a folder for the new vault")
}

// openVault asks for an existing vault folder.
func (a *appActions) openVault() {
	a.chooseFolder("Open a vault")
}

func (a *appActions) chooseFolder(title string) {
	folders, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Title:             title,
		Directory:         true,
		CreateDirectories: true,
		ButtonLabel:       "Choose",
	})
	if err != nil || len(folders) == 0 {
		return
	}
	if err := a.open(folders[0]); err != nil {
		mygo.Dialog.Error(appName, "The folder could not be opened as a vault: "+err.Error())
	}
}

// open shows a vault folder in a window and remembers it.
func (a *appActions) open(folder string) error {
	if err := openVault(a.notebooks, folder); err != nil {
		return err
	}
	s, _ := settings.Load(a.settings)
	s.Remember(folder)
	if err := s.Save(a.settings); err != nil {
		log.Printf("notes: settings: %v", err)
	}
	a.refreshMenu()
	return nil
}

// connectWindowOptions is the window of the connect form.
func connectWindowOptions() mygo.WindowOptions {
	opts := mygo.WindowOptions{
		Title:           "Connect to a Remote Folder",
		URL:             "/?connect=1",
		Width:           440,
		Height:          520,
		MinWidth:        440,
		MinHeight:       520,
		TitleBarHeight:  titleRow,
		DisableMaximize: true,
		BackgroundColor: "light-dark(#f5f5f7, #1c1c1e)",
	}
	platformWindowOptions(&opts)
	return opts
}

func isDirectory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
