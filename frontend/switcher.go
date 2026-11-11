package frontend

import (
	"context"
	"encoding/base64"
	"errors"
	"path/filepath"

	"github.com/egoist/mygo"

	"notes/internal/settings"
)

// VaultEntry is one vault in the sidebar's switcher.
type VaultEntry struct {
	// Path is the vault's folder: a remote vault's working copy.
	Path string `json:"path"`
	// Name is what the list shows: the folder's name, or the remote's address.
	Name string `json:"name"`
	// Detail is where it lives: the folder, or the remote's kind.
	Detail string `json:"detail"`
	// Remote is true for a vault kept in a WebDAV or SMB folder.
	Remote bool `json:"remote"`
	// Open is true when some window shows it.
	Open bool `json:"open"`
	// Current is true for the vault of the window that asked.
	Current bool `json:"current"`
	// Missing is true when its folder is gone.
	Missing bool `json:"missing"`
}

// Vaults lists the vaults the app knows, most recent first, with which ones
// are open and which one this window shows.
func (c *Connections) Vaults(ctx context.Context) []VaultEntry {
	current := ""
	if s, err := c.n.callerSession(ctx); err == nil {
		current = s.vault.Info().Path
	}
	return c.entries(current)
}

func (c *Connections) entries(current string) []VaultEntry {
	st, err := settings.Load(c.app.settings)
	if err != nil {
		return nil
	}
	folders := append([]string(nil), st.Recent...)
	if current != "" {
		seen := false
		for _, f := range folders {
			seen = seen || f == current
		}
		if !seen {
			folders = append([]string{current}, folders...)
		}
	}
	open := c.n.openPaths()
	out := make([]VaultEntry, 0, len(folders))
	for _, folder := range folders {
		e := VaultEntry{Path: folder, Name: filepath.Base(folder), Detail: shortPath(folder), Open: open[folder], Current: folder == current}
		if cfg, ok := st.RemoteOf(folder); ok {
			e.Remote = true
			e.Name = cfg.Address
			e.Detail = remoteKind(cfg.Kind) + " · working copy " + filepath.Base(folder)
		}
		e.Missing = !isDirectory(folder)
		out = append(out, e)
	}
	return out
}

// OpenVault shows a vault from the list. Without newWindow it replaces the
// vault of the window that asked (the window closes once the vault is up);
// with it, the vault gets a window of its own and this one stays. A vault that
// is open already is brought forward, not opened twice.
func (c *Connections) OpenVault(ctx context.Context, path string, newWindow bool) error {
	if !isDirectory(path) {
		return errors.New("this vault folder is no longer there")
	}
	caller := mygo.CallerWindow(ctx)
	var here string
	if s, err := c.n.callerSession(ctx); err == nil {
		here = s.vault.Info().Path
	}
	if here != "" && filepath.Clean(here) == filepath.Clean(path) {
		return nil
	}
	if err := c.app.open(path); err != nil {
		return err
	}
	if !newWindow && caller != nil && here != "" {
		caller.Close()
	}
	return nil
}

// ChooseVault asks for an existing vault folder and opens it in a window of
// its own.
func (c *Connections) ChooseVault(ctx context.Context) {
	go c.app.openVault()
}

// NewVault asks for an empty or new folder and makes it a vault.
func (c *Connections) NewVault(ctx context.Context) {
	go c.app.newVault()
}

// OpenConnect shows the form that keeps a vault in a remote folder.
func (c *Connections) OpenConnect(ctx context.Context) {
	go c.app.connect()
}

// CopyImage puts a PNG, given as base64, on the system clipboard. The page
// draws the selected paragraphs; the clipboard is the system's to write.
func (n *Notebooks) CopyImage(ctx context.Context, png string) error {
	data, err := base64.StdEncoding.DecodeString(png)
	if err != nil {
		return err
	}
	return mygo.Clipboard.WriteImage(data)
}
