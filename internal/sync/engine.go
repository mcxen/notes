package sync

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"notes/internal/storage"
	"notes/internal/vault"
)

const trashDir = ".trash"

// Syncer brings one vault folder and one remote folder to the same files.
type Syncer struct {
	// Root is the vault folder on this machine.
	Root string
	// BaseDir keeps the page files as they were at the last sync, which a
	// merge needs.
	BaseDir string
	// ManifestPath is where the manifest is kept, outside the vault.
	ManifestPath string
	Remote       storage.Remote
	// Device names this machine in conflict copies and temporary files.
	Device string
	// Now is the clock, for trash folders and conflict names. Nil is time.Now.
	Now func() time.Time
	// BackupDir keeps each page as it was before a sync replaced or merged
	// it, for BackupDays, outside the vault. Empty keeps none.
	BackupDir string
	// Deletions says what a run does when many files are gone from one side.
	Deletions DeletionPolicy
}

// DeletionPolicy is what a run does about files that are gone from one side
// when there are many of them, or when that side holds nothing at all: a
// share that is not mounted or a folder that was emptied looks just like
// every file having been removed.
type DeletionPolicy int

const (
	// HoldManyDeletions removes nothing on the other side and reports the
	// files in Report.HeldLocal and HeldRemote, for a person to decide.
	HoldManyDeletions DeletionPolicy = iota
	// ApplyDeletions removes them: the person said the files are meant to go.
	ApplyDeletions
	// RestoreDeleted copies them back to the side they are gone from.
	RestoreDeleted
)

const (
	// BackupDays is how long a page's copy from before a sync is kept.
	BackupDays = 30
	// holdOver and holdShare are when deletions are held: more than holdOver
	// files and more than one in holdShare of those the two sides shared.
	holdOver  = 10
	holdShare = 4
)

// errChanged is a file that was changed, on either side, while the sync was
// at work on it. Nothing is written over the change: the file is left for
// the next run, which sees both versions.
var errChanged = errors.New("sync: the file changed during the sync")

// Conflict is a file both sides changed. For a page, Fields names the values
// both sides changed, which kept the local value. For any other file, Copy is
// the other side's version, kept beside it under a new name.
type Conflict struct {
	Path   string   `json:"path"`
	Copy   string   `json:"copy,omitempty"`
	Fields []string `json:"fields,omitempty"`
}

// Report says what one sync did.
type Report struct {
	Uploaded   []string   `json:"uploaded"`
	Downloaded []string   `json:"downloaded"`
	Merged     []string   `json:"merged"`
	Conflicts  []Conflict `json:"conflicts"`
	Trashed    []string   `json:"trashed"`
	Failed     []string   `json:"failed"`
	// Deferred are files that changed while the sync ran, left for the next.
	Deferred []string `json:"deferred"`
	// HeldLocal are files the remote folder no longer has, which were not
	// moved to this vault's trash because there are many of them. HeldRemote
	// are files this vault no longer has, left in the remote folder likewise.
	HeldLocal  []string `json:"heldLocal"`
	HeldRemote []string `json:"heldRemote"`
}

type localFile struct {
	hash string
	size int64
	mod  int64
}

// Run syncs once. A file that fails is reported in Failed and left for the
// next run; the others go on.
func (s *Syncer) Run(ctx context.Context) (Report, error) {
	rep := Report{Uploaded: []string{}, Downloaded: []string{}, Merged: []string{}, Conflicts: []Conflict{}, Trashed: []string{}, Failed: []string{}, Deferred: []string{}, HeldLocal: []string{}, HeldRemote: []string{}}
	s.pruneBackups()
	man, err := LoadManifest(s.ManifestPath)
	if err != nil {
		return rep, err
	}
	listed, err := s.Remote.List(ctx)
	if err != nil {
		return rep, fmt.Errorf("sync: listing the remote folder: %w", err)
	}
	remote := map[string]storage.Entry{}
	for _, e := range listed {
		if !ignoredPath(e.Path) {
			remote[e.Path] = e
		}
	}
	local, err := s.scan(man)
	if err != nil {
		return rep, err
	}
	all := map[string]bool{}
	for p := range man.Files {
		all[p] = true
	}
	for p := range local {
		all[p] = true
	}
	for p := range remote {
		all[p] = true
	}
	paths := make([]string, 0, len(all))
	for p := range all {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	held := s.holdDeletions(paths, man, local, remote, &rep)
	for _, p := range paths {
		if err := ctx.Err(); err != nil {
			return rep, err
		}
		if held[p] {
			continue
		}
		var l *localFile
		if lf, ok := local[p]; ok {
			l = &lf
		}
		var r *storage.Entry
		if re, ok := remote[p]; ok {
			r = &re
		}
		switch err := s.syncFile(ctx, p, man, l, r, &rep); {
		case errors.Is(err, errChanged):
			rep.Deferred = append(rep.Deferred, p)
		case err != nil:
			rep.Failed = append(rep.Failed, p+": "+err.Error())
		}
	}
	if err := man.Save(s.ManifestPath); err != nil {
		return rep, err
	}
	return rep, nil
}

// holdDeletions looks at the files that are gone from one side and unchanged
// on the other, which a run would remove there too. When they are many (more
// than holdOver and more than one in holdShare of the files both sides
// shared), or when the side they are gone from holds nothing at all, it does
// what Deletions says: it returns the paths this run leaves alone, or forgets them in the
// manifest so that they are copied back.
func (s *Syncer) holdDeletions(paths []string, man *Manifest, local map[string]localFile, remote map[string]storage.Entry, rep *Report) map[string]bool {
	held := map[string]bool{}
	if s.Deletions == ApplyDeletions {
		return held
	}
	var goneRemote, goneLocal []string
	for _, p := range paths {
		entry, known := man.Files[p]
		if !known {
			continue
		}
		l, hasL := local[p]
		r, hasR := remote[p]
		switch {
		case hasL && !hasR && l.hash == entry.Base:
			goneRemote = append(goneRemote, p)
		case !hasL && hasR && r.Tag == entry.Tag:
			goneLocal = append(goneLocal, p)
		}
	}
	shared := 0
	for p := range man.Files {
		if !routine(p) {
			shared++
		}
	}
	many := func(gone []string, sideEmpty bool) bool {
		n := 0
		for _, p := range gone {
			if !routine(p) {
				n++
			}
		}
		// One file gone is one file removed, even when it was the only one.
		return (sideEmpty && n > 1) || (n > holdOver && n*holdShare > shared)
	}
	for _, side := range []struct {
		gone []string
		many bool
		into *[]string
	}{
		{goneRemote, many(goneRemote, len(remote) == 0), &rep.HeldLocal},
		{goneLocal, many(goneLocal, len(local) == 0), &rep.HeldRemote},
	} {
		if !side.many {
			continue
		}
		for _, p := range side.gone {
			if s.Deletions == RestoreDeleted {
				// Unknown to the manifest, the file is new on the side that
				// has it, and is copied to the other.
				delete(man.Files, p)
				continue
			}
			held[p] = true
			*side.into = append(*side.into, p)
		}
	}
	return held
}

// routine reports whether a file comes and goes as a matter of course: a
// page's old versions, which the vault thins out by itself. They do not count
// towards many deletions.
func routine(p string) bool {
	return strings.HasPrefix(p, ".notes/history/")
}

// syncFile applies the table of the design (section 7.2) to one path.
func (s *Syncer) syncFile(ctx context.Context, p string, man *Manifest, l *localFile, r *storage.Entry, rep *Report) error {
	entry, known := man.Files[p]
	hasL, hasR := l != nil, r != nil
	localChanged := hasL != known || (hasL && l.hash != entry.Base)
	remoteChanged := hasR != known || (hasR && r.Tag != entry.Tag)

	switch {
	case !hasL && !hasR:
		delete(man.Files, p)
		s.dropBase(p)
		return nil

	case hasL && hasR:
		if !localChanged && !remoteChanged {
			return nil
		}
		if !localChanged {
			return s.download(ctx, p, l, r.Tag, man, rep)
		}
		if !remoteChanged {
			return s.upload(ctx, p, r, true, man, rep)
		}
		return s.bothChanged(ctx, p, man, r, rep)

	case hasL && !hasR:
		if !known || localChanged {
			return s.upload(ctx, p, nil, true, man, rep)
		}
		// Removed on the remote side, and not changed here: remove it here.
		if _, err := s.localNow(p, l); err != nil {
			return err
		}
		if err := s.trashLocal(p); err != nil {
			return err
		}
		rep.Trashed = append(rep.Trashed, p)
		delete(man.Files, p)
		s.dropBase(p)
		return nil

	default: // !hasL && hasR
		if !known || remoteChanged {
			return s.download(ctx, p, nil, r.Tag, man, rep)
		}
		// Removed here, and not changed remotely: remove it remotely.
		if err := s.remoteStill(ctx, p, r); err != nil {
			return err
		}
		if err := s.trashRemote(ctx, p); err != nil {
			return err
		}
		rep.Trashed = append(rep.Trashed, p)
		delete(man.Files, p)
		s.dropBase(p)
		return nil
	}
}

// bothChanged handles a file both sides changed since the last sync: a page
// merges, any other file is kept twice.
func (s *Syncer) bothChanged(ctx context.Context, p string, man *Manifest, r *storage.Entry, rep *Report) error {
	remoteData, err := s.Remote.Get(ctx, p)
	if err != nil {
		return err
	}
	localData, err := os.ReadFile(s.localPath(p))
	if err != nil {
		return err
	}
	if bytes.Equal(localData, remoteData) {
		return s.record(p, localData, r.Tag, man)
	}
	if isPage(p) {
		base, _ := os.ReadFile(s.basePath(p))
		merged, fields, err := MergePage(base, localData, remoteData)
		if err != nil {
			return err
		}
		// Neither side may have moved on since it was read: the merge is of
		// those two versions, and is written over both.
		if !bytes.Equal(merged, remoteData) {
			if err := s.remoteStill(ctx, p, r); err != nil {
				return err
			}
		}
		if !bytes.Equal(merged, localData) {
			if now, err := os.ReadFile(s.localPath(p)); err != nil || !bytes.Equal(now, localData) {
				return errChanged
			}
			if err := s.backup(p, localData); err != nil {
				return err
			}
			if err := vault.WriteFileAtomic(s.localPath(p), merged); err != nil {
				return err
			}
		}
		tag := r.Tag
		if !bytes.Equal(merged, remoteData) {
			entry, err := s.Remote.Put(ctx, p, merged)
			if err != nil {
				return err
			}
			tag = entry.Tag
		}
		if err := s.saveBase(p, merged); err != nil {
			return err
		}
		rep.Merged = append(rep.Merged, p)
		if err := s.record(p, merged, tag, man); err != nil {
			return err
		}
		if len(fields) > 0 {
			copyPath := s.checkpointName(p)
			if err := vault.WriteFileAtomic(s.localPath(copyPath), remoteData); err != nil {
				return err
			}
			rep.Conflicts = append(rep.Conflicts, Conflict{Path: p, Copy: copyPath, Fields: fields})
		}
		return nil
	}
	// Any other file: the local version stays at its path, the remote version
	// is kept beside it, and both sides get both files.
	if err := s.upload(ctx, p, r, true, man, rep); err != nil {
		return err
	}
	copyPath := s.conflictName(p)
	if err := vault.WriteFileAtomic(s.localPath(copyPath), remoteData); err != nil {
		return err
	}
	// The copy is under a name of this device's own: nobody else writes it.
	if err := s.upload(ctx, copyPath, nil, false, man, rep); err != nil {
		return err
	}
	rep.Conflicts = append(rep.Conflicts, Conflict{Path: p, Copy: copyPath})
	return nil
}

// upload puts the local file in the remote folder. expected is the remote
// file as the listing had it, nil when it had none; with check, the upload
// is given up when the remote file is no longer that, because another device
// wrote it meanwhile.
func (s *Syncer) upload(ctx context.Context, p string, expected *storage.Entry, check bool, man *Manifest, rep *Report) error {
	data, err := os.ReadFile(s.localPath(p))
	if err != nil {
		return err
	}
	tmp := path.Join(path.Dir(p), "."+path.Base(p)+".tmp-"+s.device())
	entry, err := s.Remote.Put(ctx, tmp, data)
	if err != nil {
		return err
	}
	// Asked after the bytes are up, so that only the rename is left to do.
	if check {
		if err := s.remoteStill(ctx, p, expected); err != nil {
			return err
		}
	}
	if err := s.Remote.Move(ctx, tmp, p); err != nil {
		return err
	}
	rep.Uploaded = append(rep.Uploaded, p)
	if isPage(p) {
		if err := s.saveBase(p, data); err != nil {
			return err
		}
	}
	return s.record(p, data, entry.Tag, man)
}

// download brings the remote file here. l is the local file as the scan had
// it, nil when there was none: a local file that is no longer that was
// changed while the sync ran, and is not written over.
func (s *Syncer) download(ctx context.Context, p string, l *localFile, tag string, man *Manifest, rep *Report) error {
	data, err := s.Remote.Get(ctx, p)
	if err != nil {
		return err
	}
	old, err := s.localNow(p, l)
	if err != nil {
		return err
	}
	if l != nil {
		if err := s.backup(p, old); err != nil {
			return err
		}
	}
	if err := vault.WriteFileAtomic(s.localPath(p), data); err != nil {
		return err
	}
	rep.Downloaded = append(rep.Downloaded, p)
	if isPage(p) {
		if err := s.saveBase(p, data); err != nil {
			return err
		}
	}
	return s.record(p, data, tag, man)
}

// localNow reads the local file again and sees that it is still what the
// scan found: the same content, or still no file for a nil l. Anything else
// is errChanged.
func (s *Syncer) localNow(p string, l *localFile) ([]byte, error) {
	data, err := os.ReadFile(s.localPath(p))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if l == nil {
			return nil, nil
		}
		return nil, errChanged
	case err != nil:
		return nil, err
	case l == nil || hashOf(data) != l.hash:
		return nil, errChanged
	}
	return data, nil
}

// remoteStill asks the remote folder for the file again and sees that it is
// still what the listing had: the same tag, or still no file for a nil
// expected. Anything else is errChanged.
func (s *Syncer) remoteStill(ctx context.Context, p string, expected *storage.Entry) error {
	now, err := s.Remote.Stat(ctx, p)
	switch {
	case errors.Is(err, storage.ErrNotFound):
		if expected == nil {
			return nil
		}
		return errChanged
	case err != nil:
		return err
	case expected == nil || now.Tag != expected.Tag:
		return errChanged
	}
	return nil
}

// backup keeps a page as it is, before the sync writes another version over
// it: under BackupDir, by the day and the time.
func (s *Syncer) backup(p string, data []byte) error {
	if s.BackupDir == "" || !isPage(p) {
		return nil
	}
	name := strings.TrimSuffix(path.Base(p), ".json") + "." + s.now().Format("150405.000") + ".json"
	return vault.WriteFileAtomic(filepath.Join(s.BackupDir, s.today(), filepath.FromSlash(path.Dir(p)), name), data)
}

// pruneBackups removes the days of backups older than BackupDays.
func (s *Syncer) pruneBackups() {
	if s.BackupDir == "" {
		return
	}
	days, err := os.ReadDir(s.BackupDir)
	if err != nil {
		return
	}
	oldest := s.now().AddDate(0, 0, -BackupDays).Format("2006-01-02")
	for _, day := range days {
		if _, err := time.Parse("2006-01-02", day.Name()); err == nil && day.Name() < oldest {
			_ = os.RemoveAll(filepath.Join(s.BackupDir, day.Name()))
		}
	}
}

// record notes that p now holds data on both sides.
func (s *Syncer) record(p string, data []byte, tag string, man *Manifest) error {
	info, err := os.Stat(s.localPath(p))
	if err != nil {
		return err
	}
	man.Files[p] = Entry{Base: hashOf(data), Tag: tag, LocalSize: info.Size(), LocalMod: info.ModTime().UnixNano()}
	return nil
}

// scan lists the local files with their hashes. A file whose size and
// modification time match the manifest is not read again.
func (s *Syncer) scan(man *Manifest) (map[string]localFile, error) {
	out := map[string]localFile{}
	err := filepath.WalkDir(s.Root, func(full string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(s.Root, full)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel == trashDir {
				return filepath.SkipDir
			}
			return nil
		}
		if ignoredPath(rel) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if e, ok := man.Files[rel]; ok && e.Base != "" && info.Size() == e.LocalSize && info.ModTime().UnixNano() == e.LocalMod {
			out[rel] = localFile{hash: e.Base, size: info.Size(), mod: e.LocalMod}
			return nil
		}
		data, err := os.ReadFile(full)
		if err != nil {
			return err
		}
		out[rel] = localFile{hash: hashOf(data), size: info.Size(), mod: info.ModTime().UnixNano()}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Syncer) trashLocal(p string) error {
	dst := filepath.Join(s.Root, trashDir, s.today(), filepath.FromSlash(p))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.Rename(s.localPath(p), dst)
}

func (s *Syncer) trashRemote(ctx context.Context, p string) error {
	return s.Remote.Move(ctx, p, path.Join(trashDir, s.today(), p))
}

func (s *Syncer) device() string {
	if s.Device == "" {
		return "device"
	}
	return sanitizeName(s.Device)
}

func (s *Syncer) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Syncer) today() string { return s.now().Format("2006-01-02") }

func (s *Syncer) localPath(p string) string {
	return filepath.Join(s.Root, filepath.FromSlash(p))
}

func (s *Syncer) basePath(p string) string {
	return filepath.Join(s.BaseDir, filepath.FromSlash(p))
}

func (s *Syncer) saveBase(p string, data []byte) error {
	return vault.WriteFileAtomic(s.basePath(p), data)
}

func (s *Syncer) dropBase(p string) {
	if isPage(p) {
		_ = os.Remove(s.basePath(p))
	}
}

// conflictName names the copy of a file that both sides changed:
// "photo.png" becomes "photo (冲突 MacBook 2026-11-10).png".
func (s *Syncer) conflictName(p string) string {
	dir, file := path.Split(p)
	ext := path.Ext(file)
	stem := strings.TrimSuffix(file, ext)
	return dir + fmt.Sprintf("%s (冲突 %s %s)%s", stem, s.device(), s.today(), ext)
}

// checkpointName names the copy of a page's other version kept as a file of
// its own, under .notes/conflicts.
func (s *Syncer) checkpointName(p string) string {
	stem := strings.TrimSuffix(path.Base(p), ".json")
	return path.Join(".notes", "conflicts", fmt.Sprintf("%s (冲突 %s %s).json", stem, s.device(), s.today()))
}

func isPage(p string) bool {
	return strings.HasPrefix(p, "pages/") && strings.HasSuffix(p, ".json") && !strings.Contains(p[len("pages/"):], "/")
}

// ignoredPath reports whether a path is never synced: the trash, and the
// temporary files an interrupted save leaves.
func ignoredPath(p string) bool {
	if p == trashDir || strings.HasPrefix(p, trashDir+"/") {
		return true
	}
	name := path.Base(p)
	return strings.HasPrefix(name, ".") && strings.Contains(name, ".tmp-")
}

func hashOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func sanitizeName(name string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|', '\x00':
			return '-'
		}
		return r
	}, strings.TrimSpace(name))
}
