// Package vault keeps a notebook's records in memory and writes them to a
// folder of JSON files: one file per page (its record and its paragraphs),
// one per checkpoint of a page, and one for the workspace settings. Records
// are the same objects the web app keeps (web/src/platform/records.ts), so a
// write here means exactly what it means in the browser.
//
// Writes change memory at once and reach the disk after a short quiet
// period, one atomic file replacement per changed file. Changes made to the
// folder by other programs are merged in record by record (Reconcile),
// keeping whatever this process has not saved yet.
package vault

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"
)

// FormatVersion is the version of the files this package reads and writes.
const FormatVersion = 1

const (
	defaultFlushDelay = 500 * time.Millisecond
	retryFlushDelay   = 5 * time.Second
	pagesDir          = "pages"
	attachmentsDir    = "attachments"
	notesDir          = ".notes"
	historyDir        = ".notes/history"
	vaultFile         = ".notes/vault.json"
	workspaceFile     = ".notes/workspace.json"
)

// Record is one record: a JSON object.
type Record = map[string]any

// Collection names a kind of record, as web/src/platform/records.ts does.
type Collection string

// The collections a vault holds.
const (
	Pages    Collection = "pages"
	Blocks   Collection = "blocks"
	Versions Collection = "versions"
	Settings Collection = "workspace_settings"
)

// Write is one change to a record: a record made whole (put), changed field
// by field with a JSON Merge Patch (patch), or removed (delete).
type Write struct {
	Action     string     `json:"action"`
	Collection Collection `json:"collection"`
	ID         string     `json:"id"`
	Value      Record     `json:"value,omitempty"`
	Patch      Record     `json:"patch,omitempty"`
}

// Ack is what a write left the record as: nil when it was removed.
type Ack struct {
	Value Record `json:"value"`
}

// Row is a record of a snapshot.
type Row struct {
	Collection Collection `json:"collection"`
	ID         string     `json:"id"`
	Value      Record     `json:"value"`
}

// Snapshot is every record of the vault, as it is now.
type Snapshot struct {
	VaultID string   `json:"vaultId"`
	Path    string   `json:"path"`
	Records []Row    `json:"records"`
	Issues  []string `json:"issues"`
}

// Change is a record that changed outside this process's own writes.
type Change struct {
	Collection Collection `json:"collection"`
	ID         string     `json:"id"`
	Value      Record     `json:"value"`
	Deleted    bool       `json:"deleted"`
}

// Changes is what Reconcile found different.
type Changes struct {
	Changes []Change `json:"changes"`
}

// Info describes an open vault.
type Info struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

// Ref names one record.
type Ref struct {
	Collection Collection
	ID         string
}

func (r Ref) key() string { return string(r.Collection) + "\x00" + r.ID }

type pageFile struct {
	page   Record            // the page's own record; nil when absent
	blocks map[string]Record // paragraphs by block id, without "page"
}

type historyFile struct {
	parts map[int]Record
	// size is what the version's file held when it was read or written; 0
	// until then.
	size int
}

// Vault is an open notebook folder. Its methods are safe for concurrent use.
type Vault struct {
	mu        sync.Mutex
	root      string
	id        string
	pages     map[string]*pageFile
	history   map[string]map[string]*historyFile
	settings  map[string]Record
	dirty     map[string]Ref
	sums      map[string][sha256.Size]byte
	issues    []string
	timer     *time.Timer
	flushWait time.Duration
	closed    bool
}

// Open opens the vault in folder root, or makes the folder a new vault when
// it is not one yet. Leftover temporary files of an interrupted save are
// removed.
func Open(root string) (*Vault, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	v := &Vault{
		root:      abs,
		pages:     map[string]*pageFile{},
		history:   map[string]map[string]*historyFile{},
		settings:  map[string]Record{},
		dirty:     map[string]Ref{},
		sums:      map[string][sha256.Size]byte{},
		flushWait: defaultFlushDelay,
	}
	if err := v.initialize(); err != nil {
		return nil, err
	}
	if err := v.load(); err != nil {
		return nil, err
	}
	return v, nil
}

// Info returns the vault's id and folder.
func (v *Vault) Info() Info {
	return Info{ID: v.id, Path: v.root}
}

func (v *Vault) initialize() error {
	for _, dir := range []string{pagesDir, attachmentsDir, historyDir} {
		if err := os.MkdirAll(filepath.Join(v.root, filepath.FromSlash(dir)), 0o755); err != nil {
			return err
		}
	}
	data, err := os.ReadFile(filepath.Join(v.root, filepath.FromSlash(vaultFile)))
	if err == nil {
		obj, err := decodeObject(data)
		if err != nil {
			return fmt.Errorf("vault: %s is not readable: %w", vaultFile, err)
		}
		id, _ := obj["id"].(string)
		if !ValidID(id) {
			return fmt.Errorf("vault: %s has no valid vault id", vaultFile)
		}
		v.id = id
		return nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	raw := make([]byte, 6)
	if _, err := rand.Read(raw); err != nil {
		return err
	}
	v.id = "v_" + hex.EncodeToString(raw)
	data, err = encodeJSON(Record{
		"format":     FormatVersion,
		"id":         v.id,
		"created_at": time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(v.root, filepath.FromSlash(vaultFile)), data)
}

func (v *Vault) load() error {
	removeLeftovers(v.root)
	pages, err := os.ReadDir(filepath.Join(v.root, pagesDir))
	if err != nil {
		return err
	}
	for _, entry := range pages {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		rel := pagesDir + "/" + name
		kind, pageID, _ := classify(rel)
		if kind != kindPage {
			v.issues = append(v.issues, "skipped "+rel)
			continue
		}
		data, err := os.ReadFile(filepath.Join(v.root, filepath.FromSlash(rel)))
		if err != nil {
			return err
		}
		pf, err := parsePageFile(data)
		if err != nil {
			v.issues = append(v.issues, rel+": "+err.Error())
			continue
		}
		v.pages[pageID] = pf
		v.sums[rel] = sha256.Sum256(data)
	}
	if err := v.loadHistory(); err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(v.root, filepath.FromSlash(workspaceFile)))
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return err
	default:
		obj, err := decodeObject(data)
		if err != nil {
			v.issues = append(v.issues, workspaceFile+": "+err.Error())
			break
		}
		for _, key := range []string{"preferences", "roots"} {
			if rec, ok := obj[key].(map[string]any); ok {
				v.settings[key] = rec
			}
		}
		v.sums[workspaceFile] = sha256.Sum256(data)
	}
	// What is past the history limits goes now, so that a vault does not carry
	// versions nobody can reach: its files are removed at the first flush.
	v.pruneAllHistoryLocked(time.Now())
	return nil
}

func (v *Vault) loadHistory() error {
	pageDirs, err := os.ReadDir(filepath.Join(v.root, filepath.FromSlash(historyDir)))
	if err != nil {
		return err
	}
	for _, pageDir := range pageDirs {
		if !pageDir.IsDir() || !ValidID(pageDir.Name()) {
			continue
		}
		files, err := os.ReadDir(filepath.Join(v.root, filepath.FromSlash(historyDir), pageDir.Name()))
		if err != nil {
			return err
		}
		for _, file := range files {
			rel := historyDir + "/" + pageDir.Name() + "/" + file.Name()
			kind, pageID, versionID := classify(rel)
			if file.IsDir() || kind != kindHistory {
				continue
			}
			data, err := os.ReadFile(filepath.Join(v.root, filepath.FromSlash(rel)))
			if err != nil {
				return err
			}
			hf, err := parseHistoryFile(data)
			if err != nil {
				v.issues = append(v.issues, rel+": "+err.Error())
				continue
			}
			if v.history[pageID] == nil {
				v.history[pageID] = map[string]*historyFile{}
			}
			hf.size = len(data)
			v.history[pageID][versionID] = hf
			v.sums[rel] = sha256.Sum256(data)
		}
	}
	return nil
}

// Snapshot returns every record of the vault as it is now.
func (v *Vault) Snapshot() Snapshot {
	v.mu.Lock()
	defer v.mu.Unlock()
	snap := Snapshot{VaultID: v.id, Path: v.root, Records: []Row{}, Issues: append([]string{}, v.issues...)}
	for _, ref := range v.allRefsLocked() {
		if rec := v.get(ref); rec != nil {
			snap.Records = append(snap.Records, Row{Collection: ref.Collection, ID: ref.ID, Value: rec})
		}
	}
	return snap
}

// allRefsLocked lists every record in a stable order.
func (v *Vault) allRefsLocked() []Ref {
	var refs []Ref
	for _, pageID := range sortedKeys(v.pages) {
		pf := v.pages[pageID]
		if pf.page != nil {
			refs = append(refs, Ref{Pages, pageID})
		}
		for _, blockID := range sortedKeys(pf.blocks) {
			refs = append(refs, Ref{Blocks, BlockRecordID(pageID, blockID)})
		}
	}
	for _, pageID := range sortedKeys(v.history) {
		for _, versionID := range sortedKeys(v.history[pageID]) {
			for _, part := range sortedInts(v.history[pageID][versionID].parts) {
				refs = append(refs, Ref{Versions, HistoryRecordID(pageID, versionID, part)})
			}
		}
	}
	for _, name := range []string{"preferences", "roots"} {
		if _, ok := v.settings[name]; ok {
			refs = append(refs, Ref{Settings, name})
		}
	}
	return refs
}

// Write applies one change and returns the record as it now is. The change
// reaches the disk after a short quiet period, or at Flush.
func (v *Vault) Write(w Write) (Ack, error) {
	ref := Ref{Collection: w.Collection, ID: w.ID}
	if err := validRef(ref); err != nil {
		return Ack{}, err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.closed {
		return Ack{}, errors.New("vault: the vault is closed")
	}
	before := v.get(ref)
	var after Record
	switch w.Action {
	case "put":
		if w.Value == nil {
			return Ack{}, errors.New("vault: a put needs a value")
		}
		after = w.Value
	case "patch":
		after = mergePatch(before, w.Patch)
	case "delete":
	default:
		return Ack{}, fmt.Errorf("vault: unknown action %q", w.Action)
	}
	if ref.Collection == Blocks && after != nil {
		if page, ok := after["page"]; ok && page != pageOf(ref) {
			return Ack{}, errors.New("vault: a paragraph cannot move to another page")
		}
	}
	if reflect.DeepEqual(before, after) {
		return Ack{Value: after}, nil
	}
	v.set(ref, after)
	v.dirty[ref.key()] = ref
	switch {
	case ref.Collection == Versions && after != nil:
		pageID, _, _, _ := ParseHistoryRecordID(ref.ID)
		v.pruneHistoryLocked(pageID, time.Now())
	case ref.Collection == Pages && after == nil:
		// A page deleted for good takes its history with it, whether or not the
		// window had it loaded to delete.
		v.forgetHistoryLocked(ref.ID)
	}
	v.scheduleFlushLocked()
	return Ack{Value: v.get(ref)}, nil
}

// Flush writes every change not yet on the disk.
func (v *Vault) Flush() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.flushLocked()
}

// Close writes what is pending and refuses further writes.
func (v *Vault) Close() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	err := v.flushLocked()
	if err == nil {
		v.closed = true
	}
	return err
}

// Pending reports whether changes are waiting to be written.
func (v *Vault) Pending() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return len(v.dirty) > 0
}

// scheduleFlushLocked arms the quiet-period flush once per burst of writes.
func (v *Vault) scheduleFlushLocked() {
	if v.timer == nil {
		v.timer = time.AfterFunc(v.flushWait, v.onTimer)
	}
}

// onTimer runs the scheduled flush. A failed one is retried later.
func (v *Vault) onTimer() {
	v.mu.Lock()
	defer v.mu.Unlock()
	// A panic here would end the program with the writes still in memory: the
	// save is tried again later instead.
	defer func() {
		if r := recover(); r != nil {
			log.Printf("vault: saving failed and will be tried again: %v\n%s", r, debug.Stack())
			if v.timer == nil {
				v.timer = time.AfterFunc(retryFlushDelay, v.onTimer)
			}
		}
	}()
	v.timer = nil
	_ = v.flushLocked()
}

// flushLocked writes every dirty file. Files that fail stay dirty, and a
// retry is armed for them.
func (v *Vault) flushLocked() error {
	if v.timer != nil {
		v.timer.Stop()
		v.timer = nil
	}
	files := map[string]bool{}
	for _, ref := range v.dirty {
		files[fileOf(ref)] = true
	}
	var first error
	for _, rel := range sortedKeys(files) {
		if err := v.writeFileLocked(rel); err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		for key, ref := range v.dirty {
			if fileOf(ref) == rel {
				delete(v.dirty, key)
			}
		}
	}
	if first != nil && v.timer == nil {
		v.timer = time.AfterFunc(retryFlushDelay, v.onTimer)
	}
	return first
}

// writeFileLocked writes one vault file from memory, or removes it when
// nothing is left to keep in it.
func (v *Vault) writeFileLocked(rel string) error {
	full := filepath.Join(v.root, filepath.FromSlash(rel))
	obj, empty := v.fileRecordLocked(rel)
	if empty {
		if err := os.Remove(full); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		delete(v.sums, rel)
		return nil
	}
	data, err := encodeJSON(obj)
	if err != nil {
		return err
	}
	if err := writeAtomic(full, data); err != nil {
		return err
	}
	if kind, pageID, versionID := classify(rel); kind == kindHistory {
		if hf := v.history[pageID][versionID]; hf != nil {
			hf.size = len(data)
		}
	}
	v.sums[rel] = sha256.Sum256(data)
	return nil
}

// fileRecordLocked is the content a vault file should hold, given memory.
func (v *Vault) fileRecordLocked(rel string) (Record, bool) {
	kind, pageID, versionID := classify(rel)
	switch kind {
	case kindPage:
		pf := v.pages[pageID]
		if pf == nil || (pf.page == nil && len(pf.blocks) == 0) {
			return nil, true
		}
		obj := Record{"format": FormatVersion, "blocks": blocksObject(pf.blocks)}
		if pf.page != nil {
			obj["page"] = pf.page
		}
		return obj, false
	case kindHistory:
		hf := v.history[pageID][versionID]
		if hf == nil || len(hf.parts) == 0 {
			return nil, true
		}
		parts := Record{}
		for part, rec := range hf.parts {
			parts[fmt.Sprint(part)] = rec
		}
		return Record{"format": FormatVersion, "parts": parts}, false
	case kindSettings:
		obj := Record{"format": FormatVersion}
		for _, name := range []string{"preferences", "roots"} {
			if rec, ok := v.settings[name]; ok {
				obj[name] = rec
			}
		}
		return obj, len(obj) == 1
	}
	return nil, true
}

func blocksObject(blocks map[string]Record) Record {
	out := Record{}
	for id, rec := range blocks {
		out[id] = rec
	}
	return out
}

// get returns a record as this vault holds it: a paragraph carries its page.
func (v *Vault) get(ref Ref) Record {
	switch ref.Collection {
	case Pages:
		if pf := v.pages[ref.ID]; pf != nil {
			return pf.page
		}
	case Blocks:
		pageID, blockID, ok := ParseBlockRecordID(ref.ID)
		if pf := v.pages[pageID]; ok && pf != nil {
			if rec := pf.blocks[blockID]; rec != nil {
				out := make(Record, len(rec)+1)
				for k, val := range rec {
					out[k] = val
				}
				out["page"] = pageID
				return out
			}
		}
	case Versions:
		pageID, versionID, part, ok := ParseHistoryRecordID(ref.ID)
		if hf := v.history[pageID][versionID]; ok && hf != nil {
			return hf.parts[part]
		}
	case Settings:
		return v.settings[ref.ID]
	}
	return nil
}

// set stores a record, or removes it when rec is nil. Ids are checked by
// the caller.
func (v *Vault) set(ref Ref, rec Record) {
	switch ref.Collection {
	case Pages:
		if rec == nil {
			if pf := v.pages[ref.ID]; pf != nil {
				pf.page = nil
				v.dropEmptyPage(ref.ID)
			}
			return
		}
		v.pageFor(ref.ID).page = rec
	case Blocks:
		pageID, blockID, _ := ParseBlockRecordID(ref.ID)
		if rec == nil {
			if pf := v.pages[pageID]; pf != nil {
				delete(pf.blocks, blockID)
				v.dropEmptyPage(pageID)
			}
			return
		}
		pf := v.pageFor(pageID)
		stored := make(Record, len(rec))
		for k, val := range rec {
			if k != "page" {
				stored[k] = val
			}
		}
		pf.blocks[blockID] = stored
	case Versions:
		pageID, versionID, part, _ := ParseHistoryRecordID(ref.ID)
		if rec == nil {
			if hf := v.history[pageID][versionID]; hf != nil {
				delete(hf.parts, part)
				if len(hf.parts) == 0 {
					delete(v.history[pageID], versionID)
					if len(v.history[pageID]) == 0 {
						delete(v.history, pageID)
					}
				}
			}
			return
		}
		if v.history[pageID] == nil {
			v.history[pageID] = map[string]*historyFile{}
		}
		hf := v.history[pageID][versionID]
		if hf == nil {
			hf = &historyFile{parts: map[int]Record{}}
			v.history[pageID][versionID] = hf
		}
		hf.parts[part] = rec
		hf.size = 0
	case Settings:
		if rec == nil {
			delete(v.settings, ref.ID)
			return
		}
		v.settings[ref.ID] = rec
	}
}

func (v *Vault) pageFor(pageID string) *pageFile {
	pf := v.pages[pageID]
	if pf == nil {
		pf = &pageFile{blocks: map[string]Record{}}
		v.pages[pageID] = pf
	}
	return pf
}

func (v *Vault) dropEmptyPage(pageID string) {
	if pf := v.pages[pageID]; pf != nil && pf.page == nil && len(pf.blocks) == 0 {
		delete(v.pages, pageID)
	}
}

// --- ids and files -------------------------------------------------------

type fileKind int

const (
	kindNone fileKind = iota
	kindPage
	kindHistory
	kindSettings
)

// classify says which kind of vault file rel is, and the ids in its name.
func classify(rel string) (fileKind, string, string) {
	if rel == workspaceFile {
		return kindSettings, "", ""
	}
	if name, ok := strings.CutPrefix(rel, pagesDir+"/"); ok && !strings.Contains(name, "/") && strings.HasSuffix(name, ".json") {
		if id := strings.TrimSuffix(name, ".json"); ValidID(id) {
			return kindPage, id, ""
		}
		return kindNone, "", ""
	}
	if rest, ok := strings.CutPrefix(rel, historyDir+"/"); ok {
		pageID, file, found := strings.Cut(rest, "/")
		if !found || strings.Contains(file, "/") || !strings.HasSuffix(file, ".json") {
			return kindNone, "", ""
		}
		versionID := strings.TrimSuffix(file, ".json")
		if ValidID(pageID) && ValidID(versionID) {
			return kindHistory, pageID, versionID
		}
	}
	return kindNone, "", ""
}

// fileOf is the vault file a record lives in.
func fileOf(ref Ref) string {
	switch ref.Collection {
	case Pages:
		return pagesDir + "/" + ref.ID + ".json"
	case Blocks:
		pageID, _, _ := ParseBlockRecordID(ref.ID)
		return pagesDir + "/" + pageID + ".json"
	case Versions:
		pageID, versionID, _, _ := ParseHistoryRecordID(ref.ID)
		return historyDir + "/" + pageID + "/" + versionID + ".json"
	case Settings:
		return workspaceFile
	}
	return ""
}

func pageOf(ref Ref) string {
	pageID, _, _ := ParseBlockRecordID(ref.ID)
	return pageID
}

func validRef(ref Ref) error {
	switch ref.Collection {
	case Pages:
		if ValidID(ref.ID) {
			return nil
		}
	case Blocks:
		if _, _, ok := ParseBlockRecordID(ref.ID); ok {
			if pageID, _, _ := ParseBlockRecordID(ref.ID); ValidID(pageID) {
				return nil
			}
		}
	case Versions:
		if pageID, versionID, _, ok := ParseHistoryRecordID(ref.ID); ok && ValidID(pageID) && ValidID(versionID) {
			return nil
		}
	case Settings:
		if ref.ID == "preferences" || ref.ID == "roots" {
			return nil
		}
	default:
		return fmt.Errorf("vault: unknown collection %q", ref.Collection)
	}
	return fmt.Errorf("vault: invalid %s record id %q", ref.Collection, ref.ID)
}

// --- JSON and files ------------------------------------------------------

// mergePatch applies a JSON Merge Patch to a copy of target. A null in the
// patch removes its field; an object merges into the field it names.
func mergePatch(target, patch Record) Record {
	out := make(Record, len(target)+len(patch))
	for k, val := range target {
		out[k] = val
	}
	for k, pv := range patch {
		if pv == nil {
			delete(out, k)
			continue
		}
		if pm, ok := pv.(map[string]any); ok {
			tm, _ := out[k].(map[string]any)
			out[k] = mergePatch(tm, pm)
			continue
		}
		out[k] = pv
	}
	return out
}

func decodeObject(data []byte) (Record, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var obj Record
	if err := dec.Decode(&obj); err != nil {
		return nil, err
	}
	if obj == nil {
		return nil, errors.New("not a JSON object")
	}
	if format, ok := obj["format"]; ok && format != json.Number("1") {
		return nil, fmt.Errorf("unsupported format %v", format)
	}
	return obj, nil
}

// encodeJSON writes an object with its keys sorted and two spaces of
// indentation, so the same content is always the same bytes.
func encodeJSON(obj Record) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(obj); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func parsePageFile(data []byte) (*pageFile, error) {
	obj, err := decodeObject(data)
	if err != nil {
		return nil, err
	}
	pf := &pageFile{blocks: map[string]Record{}}
	if raw, ok := obj["page"]; ok {
		rec, ok := raw.(map[string]any)
		if !ok {
			return nil, errors.New("page is not an object")
		}
		pf.page = rec
	}
	if raw, ok := obj["blocks"]; ok {
		blocks, ok := raw.(map[string]any)
		if !ok {
			return nil, errors.New("blocks is not an object")
		}
		for id, value := range blocks {
			rec, ok := value.(map[string]any)
			if !ok || id == "" {
				return nil, fmt.Errorf("block %q is not an object", id)
			}
			delete(rec, "page")
			pf.blocks[id] = rec
		}
	}
	return pf, nil
}

func parseHistoryFile(data []byte) (*historyFile, error) {
	obj, err := decodeObject(data)
	if err != nil {
		return nil, err
	}
	raw, ok := obj["parts"].(map[string]any)
	if !ok {
		return nil, errors.New("parts is not an object")
	}
	hf := &historyFile{parts: map[int]Record{}}
	for key, value := range raw {
		part, err := parsePartIndex(key)
		if err != nil {
			return nil, err
		}
		rec, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("part %s is not an object", key)
		}
		hf.parts[part] = rec
	}
	return hf, nil
}

func parsePartIndex(key string) (int, error) {
	n := 0
	if key == "" {
		return 0, errors.New("empty part index")
	}
	for _, c := range key {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("part index %q is not a number", key)
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

// writeAtomic replaces name with data: the bytes go to a temporary file in
// the same folder, which is synced and then renamed over the target, so a
// reader sees the old file or the new one, never a part of either.
func writeAtomic(name string, data []byte) error {
	dir := filepath.Dir(name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(name)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	renamed := false
	defer func() {
		if !renamed {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, name); err != nil {
		return err
	}
	renamed = true
	syncDir(dir)
	return nil
}

// syncDir makes a rename durable where the system allows it. Windows cannot
// sync a directory, so it is skipped there.
func syncDir(dir string) {
	if runtime.GOOS == "windows" {
		return
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
}

// removeLeftovers deletes the temporary files an interrupted save left.
func removeLeftovers(root string) {
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && strings.HasPrefix(d.Name(), ".") && strings.Contains(d.Name(), ".tmp-") {
			_ = os.Remove(p)
		}
		return nil
	})
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedInts(m map[int]Record) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}

// WriteFileAtomic replaces the file at name with data in one step: readers see
// the old file or the new one. Sync uses it for the files it brings down.
func WriteFileAtomic(name string, data []byte) error { return writeAtomic(name, data) }

// DecodeObject reads a vault file's JSON object.
func DecodeObject(data []byte) (Record, error) { return decodeObject(data) }

// EncodeObject writes a vault file's JSON object: keys sorted, two spaces of
// indentation, so the same content is always the same bytes.
func EncodeObject(obj Record) ([]byte, error) { return encodeJSON(obj) }
