package frontend

import (
	"context"
	"log"
	"path/filepath"
	"runtime"

	"github.com/egoist/mygo"

	"notes/internal/settings"
	"notes/internal/vault"
)

// openVault opens the vault in folder and shows it in a new window, or brings
// its window forward when it is already open.
func openVault(n *Notebooks, folder string) error {
	v, err := vault.Open(folder)
	if err != nil {
		return err
	}
	if w := n.window(v.Info().ID); w != nil {
		_ = v.Close()
		w.Focus()
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &session{vault: v, cancel: cancel}
	s.window = mygo.NewWindow(windowOptions(v.Info()))
	cascade(n, s.window)
	n.add(s)
	if cfg, ok := remoteOf(folder); ok {
		s.remote = newRemoteSync(n, s, cfg)
		go s.remote.loop(ctx)
		s.remote.Wake()
	}
	s.window.OnFocus(func() {
		if s.remote != nil {
			s.remote.Wake()
		}
	})
	s.window.OnBlur(func() { go func() { _ = v.Flush() }() })
	s.window.OnMaximize(func() { _ = WindowMaximized.Emit(s.window, true) })
	s.window.OnUnmaximize(func() { _ = WindowMaximized.Emit(s.window, false) })
	s.window.OnClosed(func() {
		n.remove(s)
		s.close()
	})
	go watch(ctx, n, s)
	return nil
}

// cascade moves a new window down and right when it came up exactly over
// another vault's, so that two windows are not taken for one.
func cascade(n *Notebooks, w *mygo.Window) {
	x, y := w.Position()
	for moved := true; moved; {
		moved = false
		for _, other := range n.windows() {
			if ox, oy := other.Position(); ox == x && oy == y {
				x, y, moved = x+28, y+28, true
			}
		}
	}
	w.SetPosition(x, y)
}

// remoteOf is the remote folder a working copy syncs with, if it has one.
func remoteOf(folder string) (settings.Remote, bool) {
	st, err := settings.Load(settingsPath())
	if err != nil {
		return settings.Remote{}, false
	}
	return st.RemoteOf(folder)
}

// windowOptions is how a vault's window is made: the same on every platform
// except its title bar, which platformWindowOptions sets.
func windowOptions(info vault.Info) mygo.WindowOptions {
	st, _ := settings.Load(settingsPath())
	opts := mygo.WindowOptions{
		Title:           windowTitle(info),
		URL:             "/",
		Width:           1180,
		Height:          780,
		MinWidth:        720,
		MinHeight:       480,
		TitleBarHeight:  titleRow,
		StateKey:        "vault:" + info.ID,
		BackgroundColor: "light-dark(#f5f5f7, #1c1c1e)",
		// A transparent window lets the sidebar's material show through it.
		Transparent: runtime.GOOS == "darwin",
		Vibrancy:    vibrancyFor(st),
	}
	platformWindowOptions(&opts)
	return opts
}

func windowTitle(info vault.Info) string {
	return filepath.Base(info.Path)
}

// watch reports changes made to the vault's folder by anything but this
// window. Its own saves are recognized by content and ignored.
func watch(ctx context.Context, n *Notebooks, s *session) {
	info := s.vault.Info()
	watcher, err := mygo.WatchFiles(ctx, info.Path, mygo.FileWatchOptions{
		Recursive: true,
		Ignore: func(path string, isDir bool) bool {
			// Attachments are read when they are shown, not watched.
			return isDir && (path == "attachments" || path == ".trash")
		},
	})
	if err != nil {
		return
	}
	defer watcher.Close()
	for {
		events, err := watcher.Next(ctx)
		if err != nil {
			return
		}
		changes := reconcileEvents(s.vault, events)
		if len(changes) > 0 {
			_ = VaultChanged.Emit(s.window, vault.Changes{Changes: changes})
		}
	}
}

func reconcileEvents(v *vault.Vault, events []mygo.FileEvent) []vault.Change {
	changes := []vault.Change{}
	for _, ev := range events {
		var result vault.Changes
		var err error
		switch {
		case ev.Path == ".":
			result, err = v.ReconcileAll()
		case ev.Op == mygo.FileRemove && ev.IsDir:
			result, err = v.ReconcileAll()
		default:
			if ev.Op == mygo.FileRename && ev.OldPath != "" {
				if old, oldErr := v.Reconcile(ev.OldPath); oldErr == nil {
					changes = append(changes, old.Changes...)
				}
			}
			if !vault.IsVaultFile(ev.Path) {
				continue
			}
			result, err = v.Reconcile(ev.Path)
		}
		if err != nil {
			log.Printf("notes: could not read %s: %v", ev.Path, err)
			continue
		}
		changes = append(changes, result.Changes...)
	}
	return changes
}
