// Package frontend is the window layer of the notes app: one window per vault,
// the menus, and the Go services the web frontend calls through the
// TypeScript client that `mygo generate` writes.
package frontend

import (
	"context"
	"errors"
	"sync"
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
}

// Notebooks is the bound service. Each method acts on the vault of the
// window that called it.
type Notebooks struct {
	mu       sync.Mutex
	byWindow map[*mygo.Window]*session
	byID     map[string]*session
}

func newNotebooks() *Notebooks {
	return &Notebooks{
		byWindow: map[*mygo.Window]*session{},
		byID:     map[string]*session{},
	}
}

// Info describes the vault this window shows.
func (n *Notebooks) Info(ctx context.Context) (vault.Info, error) {
	s, err := n.callerSession(ctx)
	if err != nil {
		return vault.Info{}, err
	}
	return s.vault.Info(), nil
}

// Snapshot returns every record of the vault, once, when the window opens.
func (n *Notebooks) Snapshot(ctx context.Context) (vault.Snapshot, error) {
	s, err := n.callerSession(ctx)
	if err != nil {
		return vault.Snapshot{}, err
	}
	return s.vault.Snapshot(), nil
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
