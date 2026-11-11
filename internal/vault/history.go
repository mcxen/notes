package vault

import (
	"encoding/json"
	"sort"
	"time"
)

// A page keeps the history its schema declares (web/schema.ts: HISTORY_LIMIT,
// HISTORY_AGE_MS, HISTORY_BUDGET_BYTES): the web side writes checkpoints and
// leaves their retirement to the host, which is this vault. Without it a
// page's history only grows, every version being a whole copy of the page.
const (
	// MaxVersions is how many versions of a page are kept.
	MaxVersions = 20
	// MaxVersionAge is how old a version may be.
	MaxVersionAge = 30 * 24 * time.Hour
	// MaxHistoryBytes is how much a page's versions may take on the disk.
	MaxHistoryBytes = 10 * 1024 * 1024
)

type versionInfo struct {
	id      string
	at      time.Time
	size    int
	oldFile bool
}

// versionAt is when a version was made: its first part's created_at, else
// the zero time (a version of unknown age is never aged out, only counted).
func versionAt(hf *historyFile) time.Time {
	rec := hf.parts[0]
	if rec == nil {
		for _, part := range hf.parts {
			rec = part
			break
		}
	}
	if s, _ := rec["created_at"].(string); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// versionSize is how much a version takes: what its file held when it was
// read, else what it would take written.
func versionSize(hf *historyFile) int {
	if hf.size > 0 {
		return hf.size
	}
	n := 0
	for _, part := range hf.parts {
		if data, err := json.Marshal(part); err == nil {
			n += len(data)
		}
	}
	hf.size = n
	return n
}

// pruneHistoryLocked retires the versions of a page that are past its
// limits: older than MaxVersionAge, beyond the newest MaxVersions, or beyond
// the bytes MaxHistoryBytes allows, newest kept first. The newest version is
// always kept. Their files go at the next flush. It reports how many it
// retired.
func (v *Vault) pruneHistoryLocked(pageID string, now time.Time) int {
	versions := v.history[pageID]
	if len(versions) == 0 {
		return 0
	}
	list := make([]versionInfo, 0, len(versions))
	for id, hf := range versions {
		list = append(list, versionInfo{id: id, at: versionAt(hf), size: versionSize(hf)})
	}
	// Newest first; ids break ties so the order is the same every time.
	sort.Slice(list, func(i, j int) bool {
		if !list[i].at.Equal(list[j].at) {
			return list[i].at.After(list[j].at)
		}
		return list[i].id > list[j].id
	})
	kept, bytes, retired := 0, 0, 0
	for i, info := range list {
		tooOld := !info.at.IsZero() && now.Sub(info.at) > MaxVersionAge
		tooMany := kept >= MaxVersions
		tooBig := bytes+info.size > MaxHistoryBytes
		if i > 0 && (tooOld || tooMany || tooBig) {
			v.retireVersionLocked(pageID, info.id)
			retired++
			continue
		}
		kept++
		bytes += info.size
	}
	return retired
}

// retireVersionLocked forgets a version; its file is removed at the next flush.
func (v *Vault) retireVersionLocked(pageID, versionID string) {
	hf := v.history[pageID][versionID]
	if hf == nil {
		return
	}
	for part := range hf.parts {
		ref := Ref{Collection: Versions, ID: HistoryRecordID(pageID, versionID, part)}
		v.set(ref, nil)
		v.dirty[ref.key()] = ref
	}
}

// pruneAllHistoryLocked retires what is past the limits in every page's
// history, and arms the flush that removes the files.
func (v *Vault) pruneAllHistoryLocked(now time.Time) int {
	total := 0
	for _, pageID := range sortedKeys(v.history) {
		total += v.pruneHistoryLocked(pageID, now)
	}
	if total > 0 {
		v.scheduleFlushLocked()
	}
	return total
}

// forgetHistoryLocked drops the whole history of a page that is gone for good.
func (v *Vault) forgetHistoryLocked(pageID string) {
	for _, versionID := range sortedKeys(v.history[pageID]) {
		v.retireVersionLocked(pageID, versionID)
	}
}

// VersionsOf returns the records of a page's history: its versions, by part.
// The window asks for them when it needs a page's history, rather than
// receiving every page's in the snapshot.
func (v *Vault) VersionsOf(pageID string) []Row {
	v.mu.Lock()
	defer v.mu.Unlock()
	var rows []Row
	for _, versionID := range sortedKeys(v.history[pageID]) {
		hf := v.history[pageID][versionID]
		for _, part := range sortedInts(hf.parts) {
			rows = append(rows, Row{Collection: Versions, ID: HistoryRecordID(pageID, versionID, part), Value: hf.parts[part]})
		}
	}
	if rows == nil {
		rows = []Row{}
	}
	return rows
}

// SnapshotWithoutHistory is Snapshot without the versions of any page: they
// are most of a vault's bytes, and a window needs one page's at a time
// (VersionsOf).
func (v *Vault) SnapshotWithoutHistory() Snapshot {
	v.mu.Lock()
	defer v.mu.Unlock()
	snap := Snapshot{VaultID: v.id, Path: v.root, Records: []Row{}, Issues: append([]string{}, v.issues...)}
	for _, ref := range v.allRefsLocked() {
		if ref.Collection == Versions {
			continue
		}
		if rec := v.get(ref); rec != nil {
			snap.Records = append(snap.Records, Row{Collection: ref.Collection, ID: ref.ID, Value: rec})
		}
	}
	return snap
}
