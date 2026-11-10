package storage

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/net/webdav"
)

// newWebDAVServer serves a folder called Notes under /dav, as a WebDAV server
// would. user and password, when set, are required.
func newWebDAVServer(t *testing.T, user, password string) (endpoint string, root string) {
	t.Helper()
	root = t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	handler := &webdav.Handler{Prefix: "/dav", FileSystem: webdav.Dir(root), LockSystem: webdav.NewMemLS()}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user != "" {
			u, p, ok := r.BasicAuth()
			if !ok || u != user || p != password {
				w.Header().Set("WWW-Authenticate", `Basic realm="notes"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/dav/Notes/", root
}

func TestWebDAVRoundTrip(t *testing.T) {
	endpoint, root := newWebDAVServer(t, "", "")
	w, err := NewWebDAV(endpoint, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	entry, err := w.Put(ctx, "pages/p_1.json", []byte(`{"format":1}`))
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if entry.Path != "pages/p_1.json" || entry.Tag == "" {
		t.Fatalf("put entry = %+v", entry)
	}
	if _, err := os.Stat(filepath.Join(root, "Notes", "pages", "p_1.json")); err != nil {
		t.Fatalf("file not on the server: %v", err)
	}
	got, err := w.Get(ctx, "pages/p_1.json")
	if err != nil || string(got) != `{"format":1}` {
		t.Fatalf("get = %q %v", got, err)
	}
	if _, err := w.Get(ctx, "pages/missing.json"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a missing file is ErrNotFound, got %v", err)
	}
	if _, err := w.Put(ctx, "attachments/a/b/deep.bin", []byte{1, 2}); err != nil {
		t.Fatalf("put into new folders: %v", err)
	}
	entries, err := w.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]bool{}
	for _, e := range entries {
		paths[e.Path] = true
		if e.Tag == "" {
			t.Errorf("entry %s has no tag", e.Path)
		}
	}
	if !paths["pages/p_1.json"] || !paths["attachments/a/b/deep.bin"] || len(paths) != 2 {
		t.Fatalf("list = %v", paths)
	}
	if err := w.Move(ctx, "pages/p_1.json", "pages/p_2.json"); err != nil {
		t.Fatalf("move: %v", err)
	}
	if _, err := w.Get(ctx, "pages/p_2.json"); err != nil {
		t.Fatalf("moved file: %v", err)
	}
	if err := w.Move(ctx, "pages/nothing.json", "pages/x.json"); err == nil {
		t.Fatal("moving a missing file must fail")
	}
}

func TestWebDAVRejectsWrongPassword(t *testing.T) {
	endpoint, _ := newWebDAVServer(t, "me", "secret")
	bad, err := NewWebDAV(endpoint, "me", "wrong", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bad.List(context.Background()); err == nil {
		t.Fatal("a wrong password must fail")
	}
	good, err := NewWebDAV(endpoint, "me", "secret", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := good.Put(context.Background(), "x.txt", []byte("ok")); err != nil {
		t.Fatalf("right password: %v", err)
	}
}

func TestNewWebDAVRejectsOtherSchemes(t *testing.T) {
	if _, err := NewWebDAV("smb://host/share", "", "", nil); err == nil {
		t.Fatal("only http and https are WebDAV addresses")
	}
}
