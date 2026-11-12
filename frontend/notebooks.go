// Package frontend is the window layer of the notes app: one window per vault,
// the menus, and the Go services the web frontend calls through the
// TypeScript client that `mygo generate` writes.
package frontend

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"

	"notes/internal/vault"
)

// VaultChanged tells a window that its records changed outside its own
// writes: another program edited the folder, or a sync brought changes in.
var VaultChanged = mygo.NewEvent[vault.Changes]("vault:changed")

// WindowMaximized tells a window's page whether it is maximized, so its own
// window controls show the right button.
var WindowMaximized = mygo.NewEvent[bool]("window:maximized")

var errNoVault = errors.New("this window has no vault")

// session is an open vault and the window that shows it. A vault kept in a
// remote folder has a remote sync as well.
type session struct {
	vault  *vault.Vault
	window *mygo.Window
	cancel context.CancelFunc
	remote *remoteSync
	// pending is a command the window's page has not taken yet, such as
	// "newPage" from the tray. The page takes it once it is ready (TakeCommand).
	pending string
}

// Notebooks is the bound service. Each method acts on the vault of the
// window that called it.
type Notebooks struct {
	mu       sync.Mutex
	byWindow map[*mygo.Window]*session
	byID     map[string]*session
	// forceClose lists the windows that are to close for real, though closing
	// hides them to the tray: a window that is replaced by another vault.
	forceClose map[*mygo.Window]bool

	// keepInTray says whether closing a window hides it to the tray instead,
	// and onVisibility runs whenever a window shows or hides (the Dock follows
	// it). The app sets both. leaving is set when the app quits, so its windows
	// really close.
	keepInTray   func() bool
	onVisibility func()
	leaving      atomic.Bool
}

func newNotebooks() *Notebooks {
	return &Notebooks{
		byWindow:   map[*mygo.Window]*session{},
		byID:       map[string]*session{},
		forceClose: map[*mygo.Window]bool{},
	}
}

// closeWindow closes a window for real, as a window that is replaced: the
// tray does not take it.
func (n *Notebooks) closeWindow(w *mygo.Window) {
	n.mu.Lock()
	n.forceClose[w] = true
	n.mu.Unlock()
	w.Close()
}

// sessionList lists the open vaults, in no particular order.
func (n *Notebooks) sessionList() []*session {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]*session, 0, len(n.byWindow))
	for _, s := range n.byWindow {
		out = append(out, s)
	}
	return out
}

// Info describes the vault this window shows.
func (n *Notebooks) Info(ctx context.Context) (vault.Info, error) {
	s, err := n.callerSession(ctx)
	if err != nil {
		return vault.Info{}, err
	}
	return s.vault.Info(), nil
}

// Snapshot returns the records of the vault, once, when the window opens: all
// but the pages' history.
func (n *Notebooks) Snapshot(ctx context.Context) (vault.Snapshot, error) {
	s, err := n.callerSession(ctx)
	if err != nil {
		return vault.Snapshot{}, err
	}
	// Without the pages' history: it is most of a vault's bytes, and a window
	// asks for one page's at a time (Versions).
	return s.vault.SnapshotWithoutHistory(), nil
}

// Versions returns the history records of one page: its versions, by part.
func (n *Notebooks) Versions(ctx context.Context, pageID string) ([]vault.Row, error) {
	s, err := n.callerSession(ctx)
	if err != nil {
		return nil, err
	}
	return s.vault.VersionsOf(pageID), nil
}

// Write applies one record change and returns the record as it now is. A
// vault kept in a remote folder syncs a few seconds after its last change.
func (n *Notebooks) Write(ctx context.Context, w vault.Write) (vault.Ack, error) {
	s, err := n.callerSession(ctx)
	if err != nil {
		return vault.Ack{}, err
	}
	ack, err := s.vault.Write(w)
	if err == nil && s.remote != nil {
		s.remote.Schedule(3 * time.Second)
	}
	return ack, err
}

// WriteFile stores an attachment's bytes at its record's path.
func (n *Notebooks) WriteFile(ctx context.Context, path string, data []byte) error {
	s, err := n.callerSession(ctx)
	if err != nil {
		return err
	}
	return s.vault.WriteFile(path, data)
}

// ImportBackup merges a browser backup into this window's vault.
func (n *Notebooks) ImportBackup(ctx context.Context, backup string) (vault.ImportReport, error) {
	s, err := n.callerSession(ctx)
	if err != nil {
		return vault.ImportReport{}, err
	}
	return s.vault.ImportBackup([]byte(backup))
}

// Flush writes this window's pending changes to the disk now.
func (n *Notebooks) Flush(ctx context.Context) error {
	s, err := n.callerSession(ctx)
	if err != nil {
		return err
	}
	return s.vault.Flush()
}

func (n *Notebooks) callerSession(ctx context.Context) (*session, error) {
	w := mygo.CallerWindow(ctx)
	n.mu.Lock()
	defer n.mu.Unlock()
	s := n.byWindow[w]
	if s == nil {
		return nil, errNoVault
	}
	return s, nil
}

// window returns the window showing the vault with the given id, or nil.
func (n *Notebooks) window(id string) *mygo.Window {
	n.mu.Lock()
	defer n.mu.Unlock()
	if s := n.byID[id]; s != nil {
		return s.window
	}
	return nil
}

func (n *Notebooks) add(s *session) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.byWindow[s.window] = s
	n.byID[s.vault.Info().ID] = s
}

func (n *Notebooks) remove(s *session) {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.byWindow, s.window)
	delete(n.byID, s.vault.Info().ID)
}

// windows lists the windows of the open vaults.
func (n *Notebooks) windows() []*mygo.Window {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]*mygo.Window, 0, len(n.byWindow))
	for w := range n.byWindow {
		out = append(out, w)
	}
	return out
}

// openPaths is the set of folders whose vaults have a window.
func (n *Notebooks) openPaths() map[string]bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make(map[string]bool, len(n.byWindow))
	for _, s := range n.byWindow {
		out[s.vault.Info().Path] = true
	}
	return out
}

// byVaultID finds the open vault a vault:// address names.
func (n *Notebooks) byVaultID(id string) *session {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.byID[id]
}

// closeAll flushes and closes every open vault. It runs when the app quits.
func (n *Notebooks) closeAll() {
	n.mu.Lock()
	open := make([]*session, 0, len(n.byWindow))
	for _, s := range n.byWindow {
		open = append(open, s)
	}
	n.mu.Unlock()
	for _, s := range open {
		s.close()
	}
}

// close stops watching and syncing the folder, and writes what is pending.
// Closing a vault twice is harmless.
func (s *session) close() {
	s.cancel()
	_ = s.vault.Close()
}
