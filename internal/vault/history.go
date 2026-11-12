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
	// MaxVersions is how many versions of a page are kept at most.
	MaxVersions = 60
	// DenseAge is how far back every version is kept: what was written in the
	// last hour can be gone back to step by step.
	DenseAge = time.Hour
	// HourlyAge is how far back one version an hour is kept; past it, one a day
	// until MaxVersionAge. Versions thin out with age, as a page's history does in
	// Google Docs and Dropbox, so a long evening of edits cannot push out
	// yesterday's page.
	HourlyAge = 24 * time.Hour
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

// bucketOf says which stretch of time a version of this age belongs to, for
// thinning: every version in the last DenseAge is its own, then one an hour, then
// one a day. The newest version of a stretch is the one kept.
func bucketOf(at time.Time, age time.Duration) (kind int, index int64) {
	switch {
	case age <= DenseAge:
		return 0, 0
	case age <= HourlyAge:
		return 1, at.Unix() / 3600
	default:
		return 2, at.Unix() / 86400
	}
}

// pruneHistoryLocked retires the versions of a page that are past its
// limits: older than MaxVersionAge, thinned to an hour's or a day's one, beyond
// the newest MaxVersions, or beyond the bytes MaxHistoryBytes allows, newest
// kept first. The newest version is always kept. Their files go at the next
// flush. It reports how many it retired.
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
	taken := map[[2]int64]bool{}
	for i, info := range list {
		tooOld := !info.at.IsZero() && now.Sub(info.at) > MaxVersionAge
		tooMany := kept >= MaxVersions
		tooBig := bytes+info.size > MaxHistoryBytes
		// Thinning: a version of unknown age cannot be placed, and is kept by count.
		thinned := false
		if !info.at.IsZero() {
			if kind, index := bucketOf(info.at, now.Sub(info.at)); kind > 0 {
				key := [2]int64{int64(kind), index}
				thinned = taken[key]
				taken[key] = true
			}
		}
		if i > 0 && (tooOld || thinned || tooMany || tooBig) {
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
