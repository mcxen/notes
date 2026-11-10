package storage

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/webdav"
)

// A server that answers 429 to the first `busy` GETs, then serves normally.
func busyServer(t *testing.T, busy int32) (string, *atomic.Int32) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Notes", "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	handler := &webdav.Handler{Prefix: "/dav", FileSystem: webdav.Dir(root), LockSystem: webdav.NewMemLS()}
	var gets atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && gets.Add(1) <= busy {
			http.Error(w, "slow down", http.StatusTooManyRequests)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/dav/Notes/", &gets
}

func TestBusyServerIsRetriedWithBackoff(t *testing.T) {
	endpoint, gets := busyServer(t, 2)
	w, err := NewWebDAV(endpoint, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	var waits []time.Duration
	r := &retrying{Remote: w, sleep: func(_ context.Context, d time.Duration) error {
		waits = append(waits, d)
		return nil
	}}
	data, err := r.Get(context.Background(), "a.txt")
	if err != nil || string(data) != "hello" {
		t.Fatalf("get after busy answers = %q %v", data, err)
	}
	if gets.Load() != 3 {
		t.Fatalf("server saw %d GETs, want 3", gets.Load())
	}
	if len(waits) != 2 || waits[0] != time.Second || waits[1] != 2*time.Second {
		t.Fatalf("backoff waits = %v, want [1s 2s]", waits)
	}
}

func TestBusyForeverGivesUpAfterFiveAttempts(t *testing.T) {
	endpoint, gets := busyServer(t, 100)
	w, err := NewWebDAV(endpoint, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	r := &retrying{Remote: w, sleep: func(context.Context, time.Duration) error { return nil }}
	_, err = r.Get(context.Background(), "a.txt")
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("err = %v, want ErrBusy", err)
	}
	if gets.Load() != maxAttempts {
		t.Fatalf("attempts = %d, want %d", gets.Load(), maxAttempts)
	}
}

func TestNotFoundIsNotRetried(t *testing.T) {
	endpoint, _ := busyServer(t, 0)
	w, _ := NewWebDAV(endpoint, "", "", nil)
	calls := 0
	r := &retrying{Remote: w, sleep: func(context.Context, time.Duration) error { calls++; return nil }}
	if _, err := r.Get(context.Background(), "missing.txt"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	if calls != 0 {
		t.Fatalf("a missing file must not be retried (%d waits)", calls)
	}
}

// Servers that refuse infinite-depth PROPFIND are walked folder by folder,
// and servers that accept it are asked once.
func TestListFindsEveryFileWithOrWithoutInfiniteDepth(t *testing.T) {
	for _, refuseInfinity := range []bool{false, true} {
		root := t.TempDir()
		for _, rel := range []string{"pages/p_1.json", "attachments/a/b.bin", "top.txt"} {
			full := filepath.Join(root, "Notes", filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(full, []byte(rel), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		handler := &webdav.Handler{Prefix: "/dav", FileSystem: webdav.Dir(root), LockSystem: webdav.NewMemLS()}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if refuseInfinity && r.Method == "PROPFIND" && strings.EqualFold(r.Header.Get("Depth"), "infinity") {
				http.Error(w, "no infinite depth", http.StatusForbidden)
				return
			}
			handler.ServeHTTP(w, r)
		}))
		w, err := NewWebDAV(srv.URL+"/dav/Notes/", "", "", nil)
		if err != nil {
			t.Fatal(err)
		}
		entries, err := w.List(context.Background())
		srv.Close()
		if err != nil {
			t.Fatalf("refuseInfinity=%v: %v", refuseInfinity, err)
		}
		got := map[string]bool{}
		for _, e := range entries {
			got[e.Path] = true
		}
		if len(got) != 3 || !got["pages/p_1.json"] || !got["attachments/a/b.bin"] || !got["top.txt"] {
			t.Fatalf("refuseInfinity=%v: listed %v", refuseInfinity, got)
		}
	}
}
