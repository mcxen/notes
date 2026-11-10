package sync

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTrashIsListedAndRestoredWithoutOverwriting(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".trash", "2026-11-09", "notes", "todo.txt"), "old")
	writeFile(t, filepath.Join(root, ".trash", "2026-11-10", "attachments", "a.png"), "picture")

	items, err := ListTrash(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Day != "2026-11-10" || items[0].Path != "attachments/a.png" {
		t.Fatalf("items = %+v", items)
	}

	restored, err := RestoreTrash(root, items[0].Trashed)
	if err != nil || restored != "attachments/a.png" {
		t.Fatalf("restore = %q %v", restored, err)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "attachments", "a.png")); string(got) != "picture" {
		t.Fatalf("restored content = %q", got)
	}

	// Something is at the original place now: the trashed copy keeps both.
	writeFile(t, filepath.Join(root, "notes", "todo.txt"), "new")
	restored, err = RestoreTrash(root, ".trash/2026-11-09/notes/todo.txt")
	if err != nil {
		t.Fatal(err)
	}
	if restored != "notes/todo (恢复 2026-11-09).txt" {
		t.Fatalf("restored under %q", restored)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "notes", "todo.txt")); string(got) != "new" {
		t.Fatal("the file in place must not be overwritten")
	}
}

func TestRestoreRefusesPathsOutsideTheTrash(t *testing.T) {
	root := t.TempDir()
	for _, bad := range []string{"notes/todo.txt", ".trash/../notes/x", ".trash/2026-11-10/../../x", ".trash/2026-11-10/"} {
		if _, err := RestoreTrash(root, bad); err == nil {
			t.Errorf("RestoreTrash(%q) should be refused", bad)
		}
	}
}

func TestListTrashOfAVaultWithoutOne(t *testing.T) {
	items, err := ListTrash(t.TempDir())
	if err != nil || len(items) != 0 {
		t.Fatalf("no trash folder means nothing trashed: %v %v", items, err)
	}
}
