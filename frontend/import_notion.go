package frontend

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"

	"notes/internal/notion"
	"notes/internal/vault"
)

// importing is set while a window's vault takes a Notion import: one at a time.
var importing sync.Map

// importNotion asks for a Notion export (the Markdown & CSV zip) and imports it
// into the vault of the window that asked, under a page of its own. Nothing the
// vault holds is changed.
func (a *appActions) importNotion(w *mygo.Window) {
	s := a.notebooks.sessionOf(w)
	if s == nil {
		mygo.Dialog.Error(appName, tr("Open a vault first: the pages are imported into it."))
		return
	}
	if _, busy := importing.LoadOrStore(s, true); busy {
		mygo.Dialog.Error(appName, tr("An import is already running in this vault."))
		return
	}
	defer importing.Delete(s)
	files, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:      w,
		Title:       tr("Import from Notion"),
		Message:     tr("Choose the zip that Notion made: Export › Markdown & CSV."),
		ButtonLabel: tr("Choose"),
		Filters:     []mygo.FileFilter{{Name: tr("Notion export (zip)"), Extensions: []string{"zip"}}},
	})
	if err != nil || len(files) == 0 {
		return
	}
	zipPath := files[0]
	title := tr("Notion import %s", time.Now().Format("2006-01-02"))
	res, err := mygo.Dialog.Message(mygo.MessageOptions{
		Parent:        w,
		Type:          mygo.MessageQuestion,
		Message:       tr("Import “%s”?", filepath.Base(zipPath)),
		Detail:        tr("Its pages are added to this vault under a new page, “%s”. Nothing that is in the vault now is changed.", title),
		Buttons:       []string{tr("Import"), tr("Cancel")},
		DefaultButton: 0,
		CancelButton:  1,
	})
	if err != nil || res.Button != 0 {
		return
	}
	a.runNotionImport(w, s, zipPath, title)
}

// runNotionImport imports the zip, shows how far it has come in the window's
// title, and says how it went.
func (a *appActions) runNotionImport(w *mygo.Window, s *session, zipPath, title string) {
	name := windowTitle(s.vault.Info())
	var last atomic.Int64
	defer w.SetTitle(name)
	opts := notion.Options{
		Title:       title,
		Untitled:    tr("Untitled"),
		ReportTitle: tr("Import report"),
		ViewName:    tr("Table"),
		Progress: func(done, total int) {
			if now := time.Now().UnixMilli(); now-last.Load() > 250 || done == total {
				last.Store(now)
				w.SetTitle(tr("%s — importing %d of %d", name, done, total))
			}
		},
	}
	var rep notion.Report
	var changes []vault.Change
	var err error
	guard("notion import", func() {
		rep, changes, err = notion.Import(context.Background(), s.vault, zipPath, opts)
	})
	// Whatever was written is shown, even when the import stopped half way.
	tellWindow(s, changes)
	if s.remote != nil {
		s.remote.Schedule(3 * time.Second)
	}
	if err != nil {
		mygo.Dialog.Message(mygo.MessageOptions{Parent: w, Type: mygo.MessageError, Message: tr("The import stopped."), Detail: err.Error()})
		return
	}
	detail := tr("%d page(s), %d database(s), %d file(s), %d link(s) between pages.", rep.Pages, rep.Databases, rep.Files, rep.Links)
	if n := len(rep.Warnings); n > 0 {
		detail += "\n\n" + tr("%d thing(s) could not be carried over: they are listed on the page “%s”.", n, opts.ReportTitle)
	}
	mygo.Dialog.Message(mygo.MessageOptions{Parent: w, Type: mygo.MessageInfo, Message: tr("Imported from Notion."), Detail: detail})
}

// tellWindow gives a window the records a write made behind its back, a few
// hundred at a time.
func tellWindow(s *session, changes []vault.Change) {
	const batch = 200
	for len(changes) > 0 {
		n := min(batch, len(changes))
		_ = VaultChanged.Emit(s.window, vault.Changes{Changes: changes[:n]})
		changes = changes[n:]
	}
}

// ImportNotion starts an import into this window's vault, from the page's
// vault menu.
func (c *Connections) ImportNotion(ctx context.Context) {
	if w := mygo.CallerWindow(ctx); w != nil && c.app != nil {
		go c.app.importNotion(w)
	}
}

// sessionOf is the open vault of a window, or nil.
func (n *Notebooks) sessionOf(w *mygo.Window) *session {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.byWindow[w]
}
