package frontend

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"

	"notes/internal/settings"
	"notes/internal/storage"
	vsync "notes/internal/sync"
)

// ConnectRequest describes the remote folder a new vault is kept in, as the
// connect window's form has it.
type ConnectRequest struct {
	// Kind is "webdav" or "smb".
	Kind string
	// Address is the folder's URL for WebDAV, or the server for SMB.
	Address  string
	Share    string
	Folder   string
	User     string
	Password string
	Domain   string
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

// remote is the remote folder the request names, as the settings keep it. An
// address is read the way people write one: a WebDAV address without a scheme
// is https, and an SMB server may be given as smb://server/share/folder or
// \\server\share\folder, which fills the share and the folder too.
func (req ConnectRequest) remote() (settings.Remote, error) {
	cfg := settings.Remote{
		Kind:    req.Kind,
		Address: strings.TrimSpace(req.Address),
		Share:   strings.Trim(strings.TrimSpace(req.Share), `/\`),
		Folder:  strings.Trim(strings.ReplaceAll(strings.TrimSpace(req.Folder), `\`, "/"), "/"),
		User:    strings.TrimSpace(req.User),
		Domain:  strings.TrimSpace(req.Domain),
	}
	switch cfg.Kind {
	case "webdav":
		if cfg.Address == "" {
			return cfg, errors.New(tr("Enter the address of the folder."))
		}
		if !strings.Contains(cfg.Address, "://") {
			cfg.Address = "https://" + cfg.Address
		}
		cfg.Share, cfg.Folder, cfg.Domain = "", "", ""
	case "smb":
		host, share, folder := splitSMBAddress(cfg.Address)
		cfg.Address = host
		if share != "" {
			cfg.Share = share
			if folder != "" {
				cfg.Folder = folder
			}
		}
		if cfg.Address == "" {
			return cfg, errors.New(tr("Enter the name or the address of the server."))
		}
		if cfg.Share == "" {
			return cfg, errors.New(tr("Enter the name of the share."))
		}
	default:
		return cfg, errors.New(tr("choose WebDAV or SMB"))
	}
	return cfg, nil
}

// splitSMBAddress reads an SMB server as typed: "nas.local", "nas.local:445",
// "smb://nas.local/share/folder" or "\\nas\share\folder".
func splitSMBAddress(address string) (host, share, folder string) {
	address = strings.ReplaceAll(strings.TrimSpace(address), `\`, "/")
	address = strings.TrimPrefix(address, "smb:")
	parts := strings.SplitN(strings.Trim(address, "/"), "/", 3)
	host = parts[0]
	if len(parts) > 1 {
		share = parts[1]
	}
	if len(parts) > 2 {
		folder = strings.Trim(parts[2], "/")
	}
	return host, share, folder
}

// remoteCheck is what a test of a connection found in the folder.
type remoteCheck struct {
	// Files is how many files the folder holds, Pages how many of them are a
	// vault's pages.
	Files, Pages int
	// Vault is true when the folder holds a vault already.
	Vault bool
}

// checkRemote reaches the folder and lists it, without changing anything on
// either side. It gives up when ctx ends, even while the server keeps a
// connection open and says nothing.
func checkRemote(ctx context.Context, cfg settings.Remote, password string) (remoteCheck, error) {
	type result struct {
		check remoteCheck
		err   error
	}
	done := make(chan result, 1)
	go func() {
		remote, err := dialRemote(cfg, password)
		if err != nil {
			done <- result{err: err}
			return
		}
		defer remote.Close()
		entries, err := remote.List(ctx)
		done <- result{check: countRemote(entries), err: err}
	}()
	select {
	case r := <-done:
		return r.check, r.err
	case <-ctx.Done():
		return remoteCheck{}, ctx.Err()
	}
}

// countRemote says what a listing of a remote folder holds. What a sync
// moved to the folder's trash is not counted.
func countRemote(entries []storage.Entry) remoteCheck {
	var check remoteCheck
	for _, e := range entries {
		switch {
		case strings.HasPrefix(e.Path, ".trash/"):
			continue
		case e.Path == ".notes/vault.json":
			check.Vault = true
		case strings.HasPrefix(e.Path, "pages/") && strings.HasSuffix(e.Path, ".json"):
			check.Pages++
		}
		check.Files++
	}
	return check
}

// connectRemote keeps a vault in a remote folder. The folder is synced into a
// new working copy on this machine, which is opened in a window. A remote
// folder that already holds a vault brings that vault with it.
func connectRemote(ctx context.Context, n *Notebooks, req ConnectRequest) (string, error) {
	cfg, err := req.remote()
	if err != nil {
		return "", err
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
		return "", fmt.Errorf("%s: %w", tr("the password could not be saved"), err)
	}
	if _, err := runSync(ctx, folder, cfg, vsync.HoldManyDeletions); err != nil {
		cleanupConnect(folder)
		return "", err
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
	if err := openVault(n, folder); err != nil {
		return "", err
	}
	return folder, nil
}

// signInAgain gives a remote vault a new user name and password, after they
// changed on the server. The folder it syncs with stays the same.
func signInAgain(n *Notebooks, folder string, cfg settings.Remote, password string) error {
	if err := mygo.Secrets.Set(passwordKey(folder), []byte(password)); err != nil {
		return fmt.Errorf("%s: %w", tr("the password could not be saved"), err)
	}
	st, err := settings.Load(settingsPath())
	if err != nil {
		return err
	}
	st.SetRemote(folder, cfg)
	if err := st.Save(settingsPath()); err != nil {
		return err
	}
	n.setRemote(folder, cfg)
	return nil
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
