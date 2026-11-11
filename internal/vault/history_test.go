package vault

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func putVersion(t *testing.T, v *Vault, page, id string, at time.Time, pad int) {
	t.Helper()
	_, err := v.Write(Write{Action: "put", Collection: Versions, ID: HistoryRecordID(page, id, 0), Value: Record{
		"id": id, "page_id": page, "created_at": at.UTC().Format(time.RFC3339),
		"snapshot": Record{"title": strings.Repeat("x", pad)},
	}})
	if err != nil {
		t.Fatal(err)
	}
}

func versionIDs(v *Vault, page string) []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return sortedKeys(v.history[page])
}

func TestHistoryKeepsTheNewestVersionsOnly(t *testing.T) {
	root := t.TempDir()
	v, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for i := 0; i < MaxVersions+7; i++ {
		putVersion(t, v, "page_a", fmt.Sprintf("ver_%03d", i), now.Add(-time.Duration(MaxVersions+7-i)*time.Hour), 10)
	}
	ids := versionIDs(v, "page_a")
	if len(ids) != MaxVersions {
		t.Fatalf("kept %d versions, want %d", len(ids), MaxVersions)
	}
	if ids[0] != "ver_007" || ids[len(ids)-1] != "ver_026" {
		t.Fatalf("kept %v: the newest %d must stay", ids, MaxVersions)
	}
	if err := v.Flush(); err != nil {
		t.Fatal(err)
	}
	files, _ := os.ReadDir(filepath.Join(root, historyDir, "page_a"))
	if len(files) != MaxVersions {
		t.Fatalf("%d history files on disk, want %d", len(files), MaxVersions)
	}
}

func TestHistoryAgesOutButKeepsTheNewest(t *testing.T) {
	v, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-MaxVersionAge - 48*time.Hour)
	putVersion(t, v, "page_a", "ver_old1", old, 10)
	putVersion(t, v, "page_a", "ver_old2", old.Add(time.Hour), 10)
	if ids := versionIDs(v, "page_a"); len(ids) != 1 || ids[0] != "ver_old2" {
		t.Fatalf("an old page keeps its newest version and no more: %v", ids)
	}
	putVersion(t, v, "page_a", "ver_new", time.Now(), 10)
	if ids := versionIDs(v, "page_a"); len(ids) != 1 || ids[0] != "ver_new" {
		t.Fatalf("a new version retires the aged one: %v", ids)
	}
}

func TestHistoryStaysWithinItsBytes(t *testing.T) {
	v, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for i := 0; i < 6; i++ {
		putVersion(t, v, "page_a", fmt.Sprintf("ver_%d", i), now.Add(time.Duration(i)*time.Minute), 3*1024*1024)
	}
	ids := versionIDs(v, "page_a")
	if len(ids) != 3 || ids[2] != "ver_5" {
		t.Fatalf("three 3 MB versions fit in %d bytes, newest first: %v", MaxHistoryBytes, ids)
	}
}

func TestOpenRetiresWhatIsPastTheLimits(t *testing.T) {
	root := t.TempDir()
	v, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	// Written straight to the disk, as an older version of the app left them.
	dir := filepath.Join(root, historyDir, "page_a")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = v.Close()
	now := time.Now()
	for i := 0; i < MaxVersions+5; i++ {
		at := now.Add(-time.Duration(MaxVersions+5-i) * time.Hour).UTC().Format(time.RFC3339)
		data := fmt.Sprintf(`{"format":1,"parts":{"0":{"id":"ver_%03d","page_id":"page_a","created_at":%q,"snapshot":{}}}}`, i, at)
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("ver_%03d.json", i)), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	v2, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(versionIDs(v2, "page_a")); got != MaxVersions {
		t.Fatalf("a vault that opens with %d versions keeps %d", MaxVersions+5, got)
	}
	if err := v2.Flush(); err != nil {
		t.Fatal(err)
	}
	files, _ := os.ReadDir(dir)
	if len(files) != MaxVersions {
		t.Fatalf("%d files left after opening, want %d", len(files), MaxVersions)
	}
}

func TestDeletingAPageDeletesItsHistory(t *testing.T) {
	root := t.TempDir()
	v, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.Write(Write{Action: "put", Collection: Pages, ID: "page_a", Value: Record{"id": "page_a", "title": "A"}}); err != nil {
		t.Fatal(err)
	}
	putVersion(t, v, "page_a", "ver_1", time.Now(), 10)
	if _, err := v.Write(Write{Action: "delete", Collection: Pages, ID: "page_a"}); err != nil {
		t.Fatal(err)
	}
	if ids := versionIDs(v, "page_a"); len(ids) != 0 {
		t.Fatalf("history of a deleted page: %v", ids)
	}
	if err := v.Flush(); err != nil {
		t.Fatal(err)
	}
	if files, _ := os.ReadDir(filepath.Join(root, historyDir, "page_a")); len(files) != 0 {
		t.Fatalf("%d history files left", len(files))
	}
}

func TestSnapshotWithoutHistoryAndVersionsOf(t *testing.T) {
	v, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, _ = v.Write(Write{Action: "put", Collection: Pages, ID: "page_a", Value: Record{"id": "page_a", "title": "A"}})
	putVersion(t, v, "page_a", "ver_1", time.Now(), 10)
	putVersion(t, v, "page_b", "ver_1", time.Now(), 10)
	snap := v.SnapshotWithoutHistory()
	for _, row := range snap.Records {
		if row.Collection == Versions {
			t.Fatalf("the snapshot holds a version: %+v", row)
		}
	}
	if len(snap.Records) != 1 {
		t.Fatalf("records = %d, want the page alone", len(snap.Records))
	}
	rows := v.VersionsOf("page_a")
	if len(rows) != 1 || rows[0].Collection != Versions || rows[0].ID != HistoryRecordID("page_a", "ver_1", 0) {
		t.Fatalf("VersionsOf = %+v", rows)
	}
	if rows := v.VersionsOf("page_none"); rows == nil || len(rows) != 0 {
		t.Fatalf("a page with no history: %#v", rows)
	}
}
