package sync

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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
}

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
}

type localFile struct {
	hash string
	size int64
	mod  int64
}

// Run syncs once. A file that fails is reported in Failed and left for the
// next run; the others go on.
func (s *Syncer) Run(ctx context.Context) (Report, error) {
	rep := Report{Uploaded: []string{}, Downloaded: []string{}, Merged: []string{}, Conflicts: []Conflict{}, Trashed: []string{}, Failed: []string{}}
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
	for _, p := range paths {
		if err := ctx.Err(); err != nil {
			return rep, err
		}
		var l *localFile
		if lf, ok := local[p]; ok {
			l = &lf
		}
		var r *storage.Entry
		if re, ok := remote[p]; ok {
			r = &re
		}
		if err := s.syncFile(ctx, p, man, l, r, &rep); err != nil {
			rep.Failed = append(rep.Failed, p+": "+err.Error())
		}
	}
	if err := man.Save(s.ManifestPath); err != nil {
		return rep, err
	}
	return rep, nil
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
			return s.download(ctx, p, r.Tag, man, rep)
		}
		if !remoteChanged {
			return s.upload(ctx, p, man, rep)
		}
		return s.bothChanged(ctx, p, man, r, rep)

	case hasL && !hasR:
		if !known || localChanged {
			return s.upload(ctx, p, man, rep)
		}
		// Removed on the remote side, and not changed here: remove it here.
		if err := s.trashLocal(p); err != nil {
			return err
		}
		rep.Trashed = append(rep.Trashed, p)
		delete(man.Files, p)
		s.dropBase(p)
		return nil

	default: // !hasL && hasR
		if !known || remoteChanged {
			return s.download(ctx, p, r.Tag, man, rep)
		}
		// Removed here, and not changed remotely: remove it remotely.
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
		if !bytes.Equal(merged, localData) {
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
	if err := s.upload(ctx, p, man, rep); err != nil {
		return err
	}
	copyPath := s.conflictName(p)
	if err := vault.WriteFileAtomic(s.localPath(copyPath), remoteData); err != nil {
		return err
	}
	if err := s.upload(ctx, copyPath, man, rep); err != nil {
		return err
	}
	rep.Conflicts = append(rep.Conflicts, Conflict{Path: p, Copy: copyPath})
	return nil
}

func (s *Syncer) upload(ctx context.Context, p string, man *Manifest, rep *Report) error {
	data, err := os.ReadFile(s.localPath(p))
	if err != nil {
		return err
	}
	tmp := path.Join(path.Dir(p), "."+path.Base(p)+".tmp-"+s.device())
	entry, err := s.Remote.Put(ctx, tmp, data)
	if err != nil {
		return err
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

func (s *Syncer) download(ctx context.Context, p, tag string, man *Manifest, rep *Report) error {
	data, err := s.Remote.Get(ctx, p)
	if err != nil {
		return err
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
