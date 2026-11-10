package frontend

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"notes/internal/vault"
)

// The vault:// handler serves attachments of the vault the address names, and
// nothing else: not the pages, not the folders, not a path that leaves files/.
func TestVaultFilesServesOnlyTheAttachmentsOfTheNamedVault(t *testing.T) {
	dir := t.TempDir()
	v, err := vault.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	if err := v.WriteFile("files/owner_1/a.txt", []byte("hello")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "attachments", "owner_1", "folder"), 0o755); err != nil {
		t.Fatal(err)
	}
	n := newNotebooks()
	n.add(&session{vault: v})
	handler := serveVaultFiles(n)
	id := v.Info().ID

	get := func(url string, header ...string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, url, nil)
		for i := 0; i+1 < len(header); i += 2 {
			req.Header.Set(header[i], header[i+1])
		}
		handler.ServeHTTP(rec, req)
		return rec
	}

	ok := get("vault://localhost/" + id + "/files/owner_1/a.txt")
	if ok.Code != http.StatusOK {
		t.Fatalf("attachment status = %d", ok.Code)
	}
	if body, _ := io.ReadAll(ok.Body); string(body) != "hello" {
		t.Fatalf("attachment body = %q", body)
	}

	ranged := get("vault://localhost/"+id+"/files/owner_1/a.txt", "Range", "bytes=1-2")
	if ranged.Code != http.StatusPartialContent {
		t.Fatalf("range status = %d", ranged.Code)
	}
	if body, _ := io.ReadAll(ranged.Body); string(body) != "el" {
		t.Fatalf("range body = %q", body)
	}

	refused := map[string]string{
		"missing file":            "vault://localhost/" + id + "/files/owner_1/missing.txt",
		"a folder":                "vault://localhost/" + id + "/files/owner_1/folder",
		"another vault":           "vault://localhost/v_0000000000/files/owner_1/a.txt",
		"outside files":           "vault://localhost/" + id + "/pages/" + id + ".json",
		"not an attachment":       "vault://localhost/" + id + "/attachments/owner_1/a.txt",
		"leaving the attachments": "vault://localhost/" + id + "/files/../.notes/vault.json",
	}
	for what, url := range refused {
		if rec := get(url); rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404 (%s)", what, rec.Code, url)
		}
	}
}
