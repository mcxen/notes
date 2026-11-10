package frontend

import (
	"github.com/egoist/mygo"
)

// ViewCommand asks a vault window's page to do a view action it owns: toggle
// the sidebar, or switch the appearance. The payload names the action.
var ViewCommand = mygo.NewEvent[string]("view:command")

// sendViewCommand tells the page of the focused window to run a view action.
func sendViewCommand(n *Notebooks, w *mygo.Window, command string) {
	if w == nil {
		return
	}
	n.mu.Lock()
	_, open := n.byWindow[w]
	n.mu.Unlock()
	if open {
		_ = ViewCommand.Emit(w, command)
	}
}

// revealVault shows the folder of the focused vault window in the file manager.
func (a *appActions) revealVault(w *mygo.Window) {
	a.notebooks.mu.Lock()
	s := a.notebooks.byWindow[w]
	a.notebooks.mu.Unlock()
	if s != nil {
		mygo.Shell.ShowItemInFolder(s.vault.Info().Path)
	}
}
