package frontend

import (
	"notes/internal/vault"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExpandHomeReadsTypedPaths(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home folder")
	}
	for in, want := range map[string]string{
		"~":                  home,
		"~/Documents":        filepath.Join(home, "Documents"),
		"  ~/Documents/Foo ": filepath.Join(home, "Documents", "Foo"),
		"/tmp/x":             "/tmp/x",
		"~other":             "~other",
	} {
		if got := expandHome(in); got != want {
			t.Errorf("expandHome(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUniqueNameSkipsWhatIsThere(t *testing.T) {
	dir := t.TempDir()
	if got := uniqueName(dir, "Notes"); got != "Notes" {
		t.Fatalf("free name = %q", got)
	}
	_ = os.Mkdir(filepath.Join(dir, "Notes"), 0o755)
	if got := uniqueName(dir, "Notes"); got != "Notes 2" {
		t.Fatalf("second = %q", got)
	}
	_ = os.WriteFile(filepath.Join(dir, "Notes 2"), nil, 0o644)
	if got := uniqueName(dir, "Notes"); got != "Notes 3" {
		t.Fatalf("third = %q", got)
	}
}

func TestCreateVaultFolder(t *testing.T) {
	dir := t.TempDir()
	path, err := createVaultFolder(dir, "  My Notes ")
	if err != nil || path != filepath.Join(dir, "My Notes") || !isDirectory(path) {
		t.Fatalf("created %q, %v", path, err)
	}
	if _, err := createVaultFolder(dir, "My Notes"); err == nil || !strings.Contains(err.Error(), "My Notes") {
		t.Fatalf("a name already taken must be refused by name: %v", err)
	}
	for _, name := range []string{"", "  ", ".", "..", "a/b", `a\b`, "a:b"} {
		if _, err := createVaultFolder(dir, name); err == nil {
			t.Errorf("name %q was accepted", name)
		}
	}
	if _, err := createVaultFolder(filepath.Join(dir, "nowhere"), "X"); err == nil {
		t.Error("a location that is not there was accepted")
	}
	// A file is not a location.
	file := filepath.Join(dir, "file")
	_ = os.WriteFile(file, nil, 0o644)
	if _, err := createVaultFolder(file, "X"); err == nil {
		t.Error("a file was accepted as a location")
	}
}

func TestDefaultLocationIsAFolder(t *testing.T) {
	if !isDirectory(defaultLocation()) {
		t.Fatalf("%q is not a folder", defaultLocation())
	}
}

func TestSeedWelcomePageLeavesAFirstPage(t *testing.T) {
	dir := t.TempDir()
	if err := seedWelcomePage(dir); err != nil {
		t.Fatal(err)
	}
	v, err := vault.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	pages, blocks := 0, 0
	for _, row := range v.SnapshotWithoutHistory().Records {
		switch row.Collection {
		case vault.Pages:
			pages++
			if _, ok := row.Value["title_content"]; !ok {
				t.Error("the page has no title")
			}
		case vault.Blocks:
			blocks++
			if row.Value["page"] == nil {
				t.Error("a paragraph without its page")
			}
		}
	}
	if pages != 1 || blocks != 2 {
		t.Fatalf("%d pages and %d paragraphs, want 1 and 2", pages, blocks)
	}
}
