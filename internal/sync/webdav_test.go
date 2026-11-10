package sync

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/net/webdav"

	"notes/internal/storage"
)

// Two machines share one WebDAV folder. Each edits the same page offline, in
// different paragraphs, and both end with the merged page.
func TestTwoMachinesOverWebDAVMergeAPage(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	handler := &webdav.Handler{Prefix: "/dav", FileSystem: webdav.Dir(root), LockSystem: webdav.NewMemLS()}
	srv := httptest.NewServer(http.HandlerFunc(handler.ServeHTTP))
	t.Cleanup(srv.Close)
	endpoint := srv.URL + "/dav/Notes/"

	remoteA, err := storage.NewWebDAV(endpoint, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	remoteB, err := storage.NewWebDAV(endpoint, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	a := newDevice(t, "Mac", remoteA)
	b := newDevice(t, "PC", remoteB)

	a.write("pages/p_1.json", page(map[string]string{"b1": "one", "b2": "two"}))
	a.sync()
	b.sync()

	a.write("pages/p_1.json", page(map[string]string{"b1": "A-edit", "b2": "two"}))
	a.sync()
	b.write("pages/p_1.json", page(map[string]string{"b1": "one", "b2": "B-edit"}))
	rep := b.sync()
	if len(rep.Merged) != 1 || len(rep.Conflicts) != 0 {
		t.Fatalf("merged=%v conflicts=%v", rep.Merged, rep.Conflicts)
	}
	a.sync()
	gotA, _ := a.read("pages/p_1.json")
	gotB, _ := b.read("pages/p_1.json")
	if gotA != gotB || !strings.Contains(gotA, "A-edit") || !strings.Contains(gotA, "B-edit") {
		t.Fatalf("both machines must hold the merged page:\n%s\n---\n%s", gotA, gotB)
	}
}
