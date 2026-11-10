package synclog

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestAppendKeepsTheNewestEntriesMostRecentFirst(t *testing.T) {
	log := Open(filepath.Join(t.TempDir(), "sync-log.json"))
	entries, err := log.Entries()
	if err != nil || len(entries) != 0 {
		t.Fatalf("a missing log is empty: %v %v", entries, err)
	}
	for i := 0; i < MaxEntries+5; i++ {
		if err := log.Append(Entry{Time: time.Unix(int64(i), 0), Folder: fmt.Sprint("f", i), Status: "saved"}); err != nil {
			t.Fatal(err)
		}
	}
	entries, err = log.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != MaxEntries {
		t.Fatalf("kept %d entries, want %d", len(entries), MaxEntries)
	}
	if entries[0].Folder != fmt.Sprint("f", MaxEntries+4) {
		t.Fatalf("most recent first, got %q", entries[0].Folder)
	}
	if entries[len(entries)-1].Folder != "f5" {
		t.Fatalf("oldest kept should be f5, got %q", entries[len(entries)-1].Folder)
	}
}

func TestEntriesKeepConflictsAndFailures(t *testing.T) {
	log := Open(filepath.Join(t.TempDir(), "sync-log.json"))
	if err := log.Append(Entry{Folder: "a", Status: "refused", Conflicts: []string{"pages/p_1.json"}, Failed: []string{"x: disk full"}}); err != nil {
		t.Fatal(err)
	}
	entries, _ := log.Entries()
	if len(entries) != 1 || entries[0].Conflicts[0] != "pages/p_1.json" || entries[0].Failed[0] != "x: disk full" {
		t.Fatalf("entry = %+v", entries)
	}
}
