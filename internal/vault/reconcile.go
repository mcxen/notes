package vault

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
)

// entry is a record read from a file, by its reference.
type entry struct {
	ref Ref
	rec Record
}

// Reconcile reads a vault file again, after something other than this
// process changed it, and merges what the disk now holds into memory record
// by record. A record this process has written and not yet saved keeps its
// local value. A file this process itself wrote has the content it was
// saved with, and is skipped. rel is the file's path under the vault folder,
// separated by slashes. It returns the records that changed.
func (v *Vault) Reconcile(rel string) (Changes, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.reconcileLocked(rel)
}

// ReconcileAll reconciles every page, checkpoint and settings file, both the
// ones on disk and the ones in memory, so that files removed outside the
// vault are forgotten too.
func (v *Vault) ReconcileAll() (Changes, error) {
	rels := v.knownFiles()
	onDisk, err := v.diskFiles()
	if err != nil {
		return Changes{}, err
	}
	for _, rel := range onDisk {
		rels[rel] = true
	}
	all := Changes{Changes: []Change{}}
	for _, rel := range sortedKeys(rels) {
		changes, err := v.Reconcile(rel)
		if err != nil {
			return all, err
		}
		all.Changes = append(all.Changes, changes.Changes...)
	}
	return all, nil
}

// IsVaultFile reports whether a changed path under the vault folder can hold
// records. Attachments and temporary files cannot.
func IsVaultFile(rel string) bool {
	kind, _, _ := classify(filepath.ToSlash(rel))
	return kind != kindNone
}

func (v *Vault) reconcileLocked(rel string) (Changes, error) {
	none := Changes{Changes: []Change{}}
	if !IsVaultFile(rel) {
		return none, nil
	}
	full := filepath.Join(v.root, filepath.FromSlash(rel))
	data, err := os.ReadFile(full)
	missing := errors.Is(err, fs.ErrNotExist)
	if err != nil && !missing {
		return none, err
	}
	disk := map[string]entry{}
	if missing {
		delete(v.sums, rel)
	} else {
		sum := sha256.Sum256(data)
		if v.sums[rel] == sum {
			return none, nil
		}
		disk, err = entriesOf(rel, data)
		if err != nil {
			v.issues = appendIssue(v.issues, rel+": "+err.Error())
			return none, nil
		}
		v.sums[rel] = sum
	}
	local := v.localEntriesLocked(rel)
	keys := map[string]bool{}
	for key := range local {
		keys[key] = true
	}
	for key := range disk {
		keys[key] = true
	}
	changes := []Change{}
	for _, key := range sortedKeys(keys) {
		if _, dirty := v.dirty[key]; dirty {
			continue
		}
		l, hasLocal := local[key]
		d, hasDisk := disk[key]
		var oldRec, newRec Record
		if hasLocal {
			oldRec = l.rec
		}
		if hasDisk {
			newRec = d.rec
		}
		if reflect.DeepEqual(oldRec, newRec) {
			continue
		}
		ref := l.ref
		if hasDisk {
			ref = d.ref
		}
		v.set(ref, newRec)
		changes = append(changes, Change{Collection: ref.Collection, ID: ref.ID, Value: newRec, Deleted: newRec == nil})
	}
	return Changes{Changes: changes}, nil
}

// localEntriesLocked lists the records memory holds in one vault file.
func (v *Vault) localEntriesLocked(rel string) map[string]entry {
	out := map[string]entry{}
	add := func(ref Ref, rec Record) {
		if rec != nil {
			out[ref.key()] = entry{ref: ref, rec: rec}
		}
	}
	kind, pageID, versionID := classify(rel)
	switch kind {
	case kindPage:
		pf := v.pages[pageID]
		if pf == nil {
			return out
		}
		add(Ref{Pages, pageID}, pf.page)
		for blockID, rec := range pf.blocks {
			add(Ref{Blocks, BlockRecordID(pageID, blockID)}, withPage(rec, pageID))
		}
	case kindHistory:
		for part, rec := range v.history[pageID][versionID].partsOrEmpty() {
			add(Ref{Versions, HistoryRecordID(pageID, versionID, part)}, rec)
		}
	case kindSettings:
		for _, name := range []string{"preferences", "roots"} {
			add(Ref{Settings, name}, v.settings[name])
		}
	}
	return out
}

func (hf *historyFile) partsOrEmpty() map[int]Record {
	if hf == nil {
		return nil
	}
	return hf.parts
}

// entriesOf reads the records one vault file holds, by reference.
func entriesOf(rel string, data []byte) (map[string]entry, error) {
	out := map[string]entry{}
	add := func(ref Ref, rec Record) {
		out[ref.key()] = entry{ref: ref, rec: rec}
	}
	kind, pageID, versionID := classify(rel)
	switch kind {
	case kindPage:
		pf, err := parsePageFile(data)
		if err != nil {
			return nil, err
		}
		if pf.page != nil {
			add(Ref{Pages, pageID}, pf.page)
		}
		for blockID, rec := range pf.blocks {
			add(Ref{Blocks, BlockRecordID(pageID, blockID)}, withPage(rec, pageID))
		}
	case kindHistory:
		hf, err := parseHistoryFile(data)
		if err != nil {
			return nil, err
		}
		for part, rec := range hf.parts {
			add(Ref{Versions, HistoryRecordID(pageID, versionID, part)}, rec)
		}
	case kindSettings:
		obj, err := decodeObject(data)
		if err != nil {
			return nil, err
		}
		for _, name := range []string{"preferences", "roots"} {
			if rec, ok := obj[name].(map[string]any); ok {
				add(Ref{Settings, name}, rec)
			}
		}
	default:
		return nil, fmt.Errorf("%s is not a vault file", rel)
	}
	return out, nil
}

// withPage is a paragraph as a record names it: with its page.
func withPage(rec Record, pageID string) Record {
	out := make(Record, len(rec)+1)
	for k, val := range rec {
		out[k] = val
	}
	out["page"] = pageID
	return out
}

// knownFiles lists the vault files memory has records for.
func (v *Vault) knownFiles() map[string]bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	files := map[string]bool{}
	for pageID := range v.pages {
		files[pagesDir+"/"+pageID+".json"] = true
	}
	for pageID, versions := range v.history {
		for versionID := range versions {
			files[historyDir+"/"+pageID+"/"+versionID+".json"] = true
		}
	}
	files[workspaceFile] = true
	return files
}

// diskFiles lists the vault files that exist on the disk.
func (v *Vault) diskFiles() ([]string, error) {
	var rels []string
	pages, err := os.ReadDir(filepath.Join(v.root, pagesDir))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	for _, entry := range pages {
		if !entry.IsDir() {
			rels = append(rels, pagesDir+"/"+entry.Name())
		}
	}
	err = filepath.WalkDir(filepath.Join(v.root, historyDir), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			rel, relErr := filepath.Rel(v.root, p)
			if relErr == nil {
				rels = append(rels, filepath.ToSlash(rel))
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	rels = append(rels, workspaceFile)
	sort.Strings(rels)
	return rels, nil
}

func appendIssue(issues []string, issue string) []string {
	for _, existing := range issues {
		if existing == issue {
			return issues
		}
	}
	return append(issues, issue)
}
