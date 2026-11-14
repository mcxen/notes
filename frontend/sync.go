package frontend

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"

	"notes/internal/settings"
	"notes/internal/storage"
	vsync "notes/internal/sync"
	"notes/internal/synclog"
)

// SyncState tells a window how its vault's sync is going. Status is "saved"
// when the last sync finished, "saving" while one runs, "offline" when the
// remote folder could not be reached, and "refused" when it reached it but
// some files could not be brought across.
type SyncState struct {
	Status     string `json:"status"`
	Message    string `json:"message"`
	Conflicts  int    `json:"conflicts"`
	Uploaded   int    `json:"uploaded"`
	Downloaded int    `json:"downloaded"`
	// Held is how many files are gone from one side and were not removed on
	// the other, because there are many: the person decides in the settings.
	Held int `json:"held"`
}

// SyncStates is the sync state of each window's vault.
var SyncStates = mygo.NewEvent[SyncState]("sync:state")

// remoteSync keeps one vault's working copy in step with its remote folder.
// Runs are serialized: a run asked for while another goes on waits for it.
type remoteSync struct {
	n       *Notebooks
	s       *session
	cfg     settings.Remote
	folder  string
	mu      sync.Mutex
	wake    chan struct{}
	timer   *time.Timer
	stopped atomic.Bool
}

// stop ends this vault's syncing: the vault stays open, as a local vault.
func (r *remoteSync) stop() {
	r.stopped.Store(true)
}

func newRemoteSync(n *Notebooks, s *session, cfg settings.Remote) *remoteSync {
	return &remoteSync{n: n, s: s, cfg: cfg, folder: s.vault.Info().Path, wake: make(chan struct{}, 1)}
}

// Schedule runs a sync after delay, unless one is already due. A burst of
// edits is synced once, a few seconds after the last of them.
func (r *remoteSync) Schedule(delay time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.timer != nil {
		return
	}
	r.timer = time.AfterFunc(delay, func() {
		r.mu.Lock()
		r.timer = nil
		r.mu.Unlock()
		r.Wake()
	})
}

// Wake asks the background loop to sync as soon as it can.
func (r *remoteSync) Wake() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// loop syncs on request, and every five minutes while the window is open.
func (r *remoteSync) loop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-r.wake:
		}
		// A failure is reported through SyncStates, so the result is not needed. A
		// round that panics is reported the same way, and the loop goes on.
		guard("sync", func() {
			defer func() {
				if p := recover(); p != nil {
					r.report(SyncState{Status: "refused", Message: fmt.Sprint("sync failed: ", p)})
					panic(p)
				}
			}()
			_, _ = r.Run(ctx)
		})
	}
}

// Run syncs now. The vault's own pending writes go to the disk first, so the
// sync sees what the window shows.
func (r *remoteSync) Run(ctx context.Context) (vsync.Report, error) {
	return r.run(ctx, vsync.HoldManyDeletions)
}

// run syncs now, with what to do about many deletions.
func (r *remoteSync) run(ctx context.Context, deletions vsync.DeletionPolicy) (vsync.Report, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped.Load() {
		return vsync.Report{}, errors.New(tr("this vault is no longer kept in a remote folder"))
	}
	r.report(SyncState{Status: "saving"})
	if err := r.s.vault.Flush(); err != nil {
		r.report(SyncState{Status: "refused", Message: err.Error()})
		return vsync.Report{}, err
	}
	rep, err := runSync(ctx, r.folder, r.cfg, deletions)
	if err != nil {
		recordSync(r.folder, "offline", rep, err)
		r.report(SyncState{Status: "offline", Message: syncMessage(err)})
		return rep, err
	}
	// The sync wrote files behind the vault's back: read them in now rather
	// than waiting for the folder watch, and tell the window what changed.
	if changes, err := r.s.vault.ReconcileAll(); err == nil && len(changes.Changes) > 0 {
		_ = VaultChanged.Emit(r.s.window, changes)
	}
	state, status := syncOutcome(rep)
	recordSync(r.folder, status, rep, nil)
	r.report(state)
	// A file that changed while the sync ran was left alone: it is synced
	// in a moment, when both of its versions can be seen.
	if len(rep.Deferred) > 0 {
		go r.Schedule(3 * time.Second)
	}
	return rep, nil
}

// syncOutcome is how a sync that reached the remote folder went: what the
// window is told, and the status the log keeps ("saved", "refused" when some
// files could not be brought across, "held" when deletions wait for the
// person). The window knows no "held": it is told "refused", with the reason.
func syncOutcome(rep vsync.Report) (SyncState, string) {
	state := SyncState{
		Status:     "saved",
		Conflicts:  len(rep.Conflicts),
		Uploaded:   len(rep.Uploaded),
		Downloaded: len(rep.Downloaded),
		Held:       len(rep.HeldLocal) + len(rep.HeldRemote),
	}
	switch {
	case len(rep.Failed) > 0:
		state.Status, state.Message = "refused", rep.Failed[0]
		return state, "refused"
	case state.Held > 0:
		state.Status = "refused"
		state.Message = tr("%d file(s) are gone from one side, so nothing was removed on the other. Choose what to do in Settings › Sync.", state.Held)
		return state, "held"
	}
	return state, "saved"
}

// syncMessage says why a sync could not reach the remote folder: in words
// when the error is one that has them, else the error itself.
func syncMessage(err error) string {
	if text := problemText(storage.ProblemOf(err)); text != "" {
		return text
	}
	return err.Error()
}

func emitSync(s *session, state SyncState) {
	_ = SyncStates.Emit(s.window, state)
}

// report tells the window how the sync is going, and keeps it for the
// settings window.
func (r *remoteSync) report(state SyncState) {
	emitSync(r.s, state)
	r.n.noteSync(r.folder, state)
}

// noteSync keeps a working copy's latest sync state, for the settings window,
// and tells whoever watches that it changed.
func (n *Notebooks) noteSync(folder string, state SyncState) {
	n.mu.Lock()
	if n.syncStates == nil {
		n.syncStates = map[string]SyncState{}
	}
	n.syncStates[folder] = state
	changed := n.onSyncState
	n.mu.Unlock()
	if changed != nil {
		changed()
	}
}

// watchSync has fn run whenever a working copy's sync state changes; nil
// stops it.
func (n *Notebooks) watchSync(fn func()) {
	n.mu.Lock()
	n.onSyncState = fn
	n.mu.Unlock()
}

// syncStateOf is the latest sync state of a working copy since the app
// started, if it has synced.
func (n *Notebooks) syncStateOf(folder string) (SyncState, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	state, ok := n.syncStates[folder]
	return state, ok
}

// setRemote gives the open vault of a working copy its remote folder's
// settings again, after the user name or the domain changed, and syncs.
func (n *Notebooks) setRemote(folder string, cfg settings.Remote) {
	for _, s := range n.sessionList() {
		if s.remote != nil && s.vault.Info().Path == folder {
			s.remote.mu.Lock()
			s.remote.cfg = cfg
			s.remote.mu.Unlock()
			s.remote.Wake()
		}
	}
}

// runSync brings a working copy folder and its remote folder together once.
// The working copy need not be open in a window, which is how a new remote
// vault is first filled.
func runSync(ctx context.Context, folder string, cfg settings.Remote, deletions vsync.DeletionPolicy) (vsync.Report, error) {
	var rep vsync.Report
	remote, err := openRemote(cfg, folder)
	if err != nil {
		return rep, err
	}
	defer remote.Close()
	dirs, err := syncDirs(folder)
	if err != nil {
		return rep, err
	}
	host, _ := os.Hostname()
	engine := &vsync.Syncer{
		Root:         folder,
		BaseDir:      dirs.base,
		ManifestPath: dirs.manifest,
		Remote:       remote,
		Device:       host,
		BackupDir:    dirs.backup,
		Deletions:    deletions,
	}
	return engine.Run(ctx)
}

type syncPaths struct {
	base     string
	manifest string
	// backup keeps the pages a sync replaced, as they were, for a month.
	backup string
}

// syncDirs is where a working copy's sync state is kept: outside the vault.
func syncDirs(folder string) (syncPaths, error) {
	root, err := userDataDir()
	if err != nil {
		return syncPaths{}, err
	}
	dir := filepath.Join(root, "sync", filepath.Base(folder))
	return syncPaths{base: filepath.Join(dir, "base"), manifest: filepath.Join(dir, "manifest.json"), backup: filepath.Join(dir, "backup")}, nil
}

func userDataDir() (string, error) {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return "", fmt.Errorf("no user data folder: %w", err)
	}
	return dir, nil
}

// passwordKey names the credential-store entry of a working copy's password.
func passwordKey(folder string) string {
	return "sync-" + filepath.Base(folder)
}

// openRemote connects to the remote folder a working copy syncs with, with
// the password the credential store keeps for it.
func openRemote(cfg settings.Remote, folder string) (storage.Remote, error) {
	password, err := mygo.Secrets.Get(passwordKey(folder))
	if err != nil && !errors.Is(err, mygo.ErrSecretNotFound) {
		return nil, fmt.Errorf("%s: %w", tr("the password could not be read"), err)
	}
	return dialRemote(cfg, string(password))
}

// dialRemote connects to a remote folder with a password.
func dialRemote(cfg settings.Remote, password string) (storage.Remote, error) {
	var remote storage.Remote
	var err error
	switch cfg.Kind {
	case "webdav":
		remote, err = storage.NewWebDAV(cfg.Address, cfg.User, password, nil)
	case "smb":
		remote, err = storage.NewSMB(cfg.Address, cfg.Share, cfg.Folder, cfg.User, password, cfg.Domain)
	default:
		return nil, fmt.Errorf("unknown remote kind %q", cfg.Kind)
	}
	if err != nil {
		return nil, err
	}
	return storage.WithRetry(remote), nil
}

// syncLog is the record of syncs, shared by every window.
func syncLog() *synclog.Log {
	dir, err := userDataDir()
	if err != nil {
		return synclog.Open(filepath.Join(os.TempDir(), "notes-sync-log.json"))
	}
	return synclog.Open(filepath.Join(dir, "sync-log.json"))
}

// recordSync adds one sync to the log: what it moved, and any failure.
func recordSync(folder, status string, rep vsync.Report, err error) {
	entry := synclog.Entry{
		Time:       time.Now(),
		Folder:     folder,
		Status:     status,
		Uploaded:   rep.Uploaded,
		Downloaded: rep.Downloaded,
		Merged:     rep.Merged,
		Trashed:    rep.Trashed,
		Failed:     rep.Failed,
		Deferred:   rep.Deferred,
		HeldLocal:  rep.HeldLocal,
		HeldRemote: rep.HeldRemote,
	}
	for _, c := range rep.Conflicts {
		entry.Conflicts = append(entry.Conflicts, c.Path)
	}
	if err != nil {
		entry.Message = err.Error()
		entry.Problem = storage.ProblemOf(err).String()
	}
	_ = syncLog().Append(entry)
}

// Disconnect stops a vault syncing with its remote folder and forgets the
// folder and its password. The vault itself stays on this computer.
func (n *Notebooks) Disconnect(folder string) error {
	st, err := settings.Load(settingsPath())
	if err != nil {
		return err
	}
	st.RemoveRemote(folder)
	if err := st.Save(settingsPath()); err != nil {
		return err
	}
	n.mu.Lock()
	for _, s := range n.byWindow {
		if s.remote != nil && s.vault.Info().Path == folder {
			s.remote.stop()
		}
	}
	n.mu.Unlock()
	_ = mygo.Secrets.Delete(passwordKey(folder))
	return nil
}

// syncFolder syncs a remote vault's working copy now, from the settings window,
// through the window that shows it when one is open.
func (n *Notebooks) syncFolder(folder string) {
	n.syncFolderWith(folder, vsync.HoldManyDeletions)
}

// syncFolderWith syncs a working copy now, with what the person chose to do
// about the many files gone from one side.
func (n *Notebooks) syncFolderWith(folder string, deletions vsync.DeletionPolicy) {
	n.mu.Lock()
	var open *remoteSync
	for _, s := range n.byWindow {
		if s.vault.Info().Path == folder && s.remote != nil {
			open = s.remote
		}
	}
	n.mu.Unlock()
	if open != nil {
		// The open window's sync runs now, and the caller reads the result.
		_, _ = open.run(context.Background(), deletions)
		return
	}
	cfg, ok := remoteOf(folder)
	if !ok {
		return
	}
	n.noteSync(folder, SyncState{Status: "saving"})
	rep, err := runSync(context.Background(), folder, cfg, deletions)
	if err != nil {
		recordSync(folder, "offline", rep, err)
		n.noteSync(folder, SyncState{Status: "offline", Message: syncMessage(err)})
		return
	}
	state, status := syncOutcome(rep)
	recordSync(folder, status, rep, nil)
	n.noteSync(folder, state)
}

// SyncNow syncs this window's vault now and returns what changed.
func (n *Notebooks) SyncNow(ctx context.Context) (vsync.Report, error) {
	s, err := n.callerSession(ctx)
	if err != nil {
		return vsync.Report{}, err
	}
	if s.remote == nil {
		return vsync.Report{}, errors.New(tr("this vault is not kept in a remote folder"))
	}
	return s.remote.Run(ctx)
}

// syncWindow asks the sync of the window's vault to run now.
func (n *Notebooks) syncWindow(w *mygo.Window) {
	n.mu.Lock()
	s := n.byWindow[w]
	n.mu.Unlock()
	if s != nil && s.remote != nil {
		s.remote.Wake()
	}
}
