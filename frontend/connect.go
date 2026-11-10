package frontend

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"

	"notes/internal/settings"
)

// ConnectRequest describes the remote folder a new vault is kept in.
type ConnectRequest struct {
	// Kind is "webdav" or "smb".
	Kind string `json:"kind"`
	// Address is the folder's URL for WebDAV, or the host for SMB.
	Address  string `json:"address"`
	Share    string `json:"share"`
	Folder   string `json:"folder"`
	User     string `json:"user"`
	Password string `json:"password"`
	Domain   string `json:"domain"`
}

// Connections is the bound service of the windows that are not a vault: the
// connect form, the settings and the picker. It is not tied to a vault.
type Connections struct {
	n   *Notebooks
	app *appActions
}

// OpenSettings opens the settings window on a page: "general", "appearance",
// "vaults" or "sync".
func (c *Connections) OpenSettings(ctx context.Context, page string) {
	if c.app != nil {
		go c.app.openSettings(page)
	}
}

// OpenPicker shows the vault picker.
func (c *Connections) OpenPicker(ctx context.Context) {
	if c.app != nil {
		go c.app.showPicker()
	}
}

// Connect keeps a vault in a remote folder. The folder is synced into a new
// working copy on this machine, which is opened in a window. A remote folder
// that already holds a vault brings that vault with it.
func (c *Connections) Connect(ctx context.Context, req ConnectRequest) (string, error) {
	cfg := settings.Remote{Kind: req.Kind, Address: req.Address, Share: req.Share, Folder: req.Folder, User: req.User, Domain: req.Domain}
	if cfg.Kind != "webdav" && cfg.Kind != "smb" {
		return "", errors.New("choose WebDAV or SMB")
	}
	if cfg.Address == "" {
		return "", errors.New("enter the address of the folder")
	}
	root, err := userDataDir()
	if err != nil {
		return "", err
	}
	folder, err := newWorkingCopy(root)
	if err != nil {
		return "", err
	}
	if err := mygo.Secrets.Set(passwordKey(folder), []byte(req.Password)); err != nil {
		_ = os.RemoveAll(folder)
		return "", fmt.Errorf("the password could not be saved: %w", err)
	}
	if _, err := runSync(ctx, folder, cfg); err != nil {
		cleanupConnect(folder)
		return "", fmt.Errorf("the folder could not be reached: %w", err)
	}
	st, err := settings.Load(settingsPath())
	if err != nil {
		return "", err
	}
	st.SetRemote(folder, cfg)
	st.Remember(folder)
	if err := st.Save(settingsPath()); err != nil {
		return "", err
	}
	if err := openVault(c.n, folder); err != nil {
		return "", err
	}
	if w := mygo.CallerWindow(ctx); w != nil {
		w.Close()
	}
	return folder, nil
}

// cleanupConnect removes a working copy whose connection failed.
func cleanupConnect(folder string) {
	_ = os.RemoveAll(folder)
	_ = mygo.Secrets.Delete(passwordKey(folder))
}

// newWorkingCopy makes an empty folder for a remote vault's working copy.
func newWorkingCopy(root string) (string, error) {
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	folder := filepath.Join(root, "vaults", hex.EncodeToString(raw))
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return "", err
	}
	return folder, nil
}
