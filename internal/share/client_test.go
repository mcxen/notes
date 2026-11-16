package share

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// fakeWorker is the worker's API, enough of it to test the client's side of the
// conversation: the token is checked, a page lists its files, and a file is
// taken only if the page lists it.
type fakeWorker struct {
	t       *testing.T
	pages   map[string]Page
	files   map[string][]byte
	info    string
	pending int
}

func (f *fakeWorker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer secret" {
		http.Error(w, `{"error":"The token is missing or wrong."}`, http.StatusUnauthorized)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/")
	switch {
	case path == "info":
		app := "notes-share"
		if f.info != "" {
			app = f.info
		}
		_, _ = io.WriteString(w, `{"app":"`+app+`","apiVersion":1,"version":"0.1.0","migrations":{"applied":1,"bundled":1,"pending":`+string(rune('0'+f.pending))+`},"pages":`+string(rune('0'+len(f.pages)))+`}`)
	case path == "pages" && r.Method == http.MethodGet:
		var out []Published
		for id, p := range f.pages {
			out = append(out, Published{ID: id, Title: p.Title, Parent: p.Parent, URL: "https://x.test/" + id})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"pages": out})
	case strings.Contains(path, "/files/") && r.Method == http.MethodPut:
		parts := strings.Split(strings.TrimPrefix(path, "pages/"), "/files/")
		page, ok := f.pages[parts[0]]
		if !ok {
			http.Error(w, `{"error":"Publish the page before uploading its files."}`, http.StatusNotFound)
			return
		}
		listed := false
		for _, name := range page.Files {
			listed = listed || name == parts[1]
		}
		if !listed {
			http.Error(w, `{"error":"The page does not list that file."}`, http.StatusConflict)
			return
		}
		data, _ := io.ReadAll(r.Body)
		f.files[parts[0]+"/"+parts[1]] = data
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{}`)
	case strings.HasPrefix(path, "pages/") && r.Method == http.MethodPut:
		id := strings.TrimPrefix(path, "pages/")
		var page Page
		if err := json.NewDecoder(r.Body).Decode(&page); err != nil || page.Files == nil {
			http.Error(w, `{"error":"bad page"}`, http.StatusBadRequest)
			return
		}
		f.pages[id] = page
		missing := []string{}
		for _, name := range page.Files {
			if _, ok := f.files[id+"/"+name]; !ok {
				missing = append(missing, name)
			}
		}
		_ = json.NewEncoder(w).Encode(Reply{ID: id, URL: "https://x.test/" + id, Missing: missing})
	case strings.HasPrefix(path, "pages/") && r.Method == http.MethodDelete:
		id := strings.TrimPrefix(path, "pages/")
		if _, ok := f.pages[id]; !ok {
			http.Error(w, `{"error":"No page is published with that id."}`, http.StatusNotFound)
			return
		}
		delete(f.pages, id)
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}

func newFake(t *testing.T) (*fakeWorker, *Client) {
	t.Helper()
	fake := &fakeWorker{t: t, pages: map[string]Page{}, files: map[string][]byte{}}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	client, err := New(server.URL, "secret", nil)
	if err != nil {
		t.Fatal(err)
	}
	return fake, client
}

func TestAnAddressIsReadTheWayItIsTyped(t *testing.T) {
	for in, want := range map[string]string{
		"notes-share.me.workers.dev":   "https://notes-share.me.workers.dev",
		"  https://share.example.com/": "https://share.example.com",
		"localhost:8787":               "http://localhost:8787",
		"http://127.0.0.1:8787/":       "http://127.0.0.1:8787",
		"https://example.com/base/?q":  "https://example.com/base",
	} {
		u, err := NormalizeURL(in)
		if err != nil || u.String() != want {
			t.Errorf("NormalizeURL(%q) = %v, %v; want %s", in, u, err, want)
		}
	}
	for _, bad := range []string{"", "   ", "ftp://example.com", "https://"} {
		if _, err := NormalizeURL(bad); err == nil {
			t.Errorf("NormalizeURL(%q) is accepted", bad)
		}
	}
}

func TestAWorkerWithoutATokenIsNotConfigured(t *testing.T) {
	if _, err := New("https://x.test", "  ", nil); err != ErrNotConfigured {
		t.Fatalf("err = %v, want ErrNotConfigured", err)
	}
}

func TestAPageIsPublishedAndItsMissingFilesAreSent(t *testing.T) {
	fake, client := newFake(t)
	ctx := context.Background()
	reply, err := client.Publish(ctx, "aaaaaaaaaaaa", Page{Title: "Plan", HTML: "<p>x</p>", Files: []string{"one.png", "two.png"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(reply.Missing) != 2 {
		t.Fatalf("missing = %v, want both files", reply.Missing)
	}
	if err := client.UploadFile(ctx, "aaaaaaaaaaaa", "one.png", "image/png", []byte{1, 2}); err != nil {
		t.Fatal(err)
	}
	reply, _ = client.Publish(ctx, "aaaaaaaaaaaa", Page{Title: "Plan", HTML: "<p>y</p>", Files: []string{"one.png", "two.png"}})
	if len(reply.Missing) != 1 || reply.Missing[0] != "two.png" {
		t.Fatalf("missing = %v, want only two.png", reply.Missing)
	}
	if string(fake.files["aaaaaaaaaaaa/one.png"]) != "\x01\x02" {
		t.Fatalf("the file was not stored as sent")
	}
	if err := client.UploadFile(ctx, "aaaaaaaaaaaa", "other.png", "image/png", []byte{9}); err == nil {
		t.Fatal("a file the page does not list was taken")
	}
}

func TestAWrongTokenIsSaidSo(t *testing.T) {
	_, client := newFake(t)
	client.Token = "wrong"
	if _, err := client.Info(context.Background()); err != ErrUnauthorized {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
}

func TestAnAddressThatIsNotAWorkerIsSaidSo(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	client, _ := New(server.URL, "secret", nil)
	if _, err := client.Info(context.Background()); err != ErrNotShare {
		t.Fatalf("err = %v, want ErrNotShare", err)
	}
	fake, client := newFake(t)
	fake.info = "something-else"
	if _, err := client.Info(context.Background()); err != ErrNotShare {
		t.Fatalf("another app's answer: err = %v, want ErrNotShare", err)
	}
}

func TestAWorkerWhoseDatabaseIsBehindIsNotUsed(t *testing.T) {
	fake, client := newFake(t)
	fake.pending = 2
	_, err := client.Compatible(context.Background())
	if got, ok := err.(MigrationsError); !ok || got.Pending != 2 {
		t.Fatalf("err = %v, want 2 pending migrations", err)
	}
}

func TestUnpublishingAPageThatIsGoneIsNotAnError(t *testing.T) {
	fake, client := newFake(t)
	ctx := context.Background()
	if _, err := client.Publish(ctx, "bbbbbbbbbbbb", Page{Title: "T"}); err != nil {
		t.Fatal(err)
	}
	if err := client.Unpublish(ctx, "bbbbbbbbbbbb"); err != nil || len(fake.pages) != 0 {
		t.Fatalf("unpublish: err = %v, pages = %v", err, fake.pages)
	}
	if err := client.Unpublish(ctx, "bbbbbbbbbbbb"); err != nil {
		t.Fatalf("a second unpublish failed: %v", err)
	}
}

func TestThePublishedPagesAreListed(t *testing.T) {
	_, client := newFake(t)
	ctx := context.Background()
	_, _ = client.Publish(ctx, "cccccccccccc", Page{Title: "Child", Parent: "dddddddddddd"})
	pages, err := client.List(ctx)
	if err != nil || len(pages) != 1 || pages[0].Parent != "dddddddddddd" {
		t.Fatalf("list = %v, %v", pages, err)
	}
}

func TestTheSetupPromptNamesNoDomainOrAccount(t *testing.T) {
	prompt := SetupPrompt()
	for _, want := range []string{TemplateRepository, TemplatePath + "/DEPLOY.md", "Ask me which Cloudflare account", "own domain", "Do not save the token"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the prompt lacks %q", want)
		}
	}
}

func TestPublicationsAreKeptPerVaultAndForgotten(t *testing.T) {
	store := OpenStore(t.TempDir() + "/publications.json")
	if err := store.Set("vault-a", "page1", Publication{ID: "aaaaaaaaaaaa", URL: "https://x.test/a"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Set("vault-b", "page1", Publication{ID: "bbbbbbbbbbbb", URL: "https://x.test/b"}); err != nil {
		t.Fatal(err)
	}
	a, _ := store.Of("vault-a")
	if a["page1"].ID != "aaaaaaaaaaaa" || len(a) != 1 {
		t.Fatalf("vault a = %v", a)
	}
	if err := store.Forget("vault-a", "page1"); err != nil {
		t.Fatal(err)
	}
	a, _ = store.Of("vault-a")
	b, _ := store.Of("vault-b")
	if len(a) != 0 || b["page1"].ID != "bbbbbbbbbbbb" {
		t.Fatalf("after forgetting: a = %v, b = %v", a, b)
	}
}

// TestAgainstARunningWorker talks to a real worker when NOTES_SHARE_TEST_URL
// and NOTES_SHARE_TEST_TOKEN are set (`wrangler dev --local` in share-worker/).
func TestAgainstARunningWorker(t *testing.T) {
	address, token := os.Getenv("NOTES_SHARE_TEST_URL"), os.Getenv("NOTES_SHARE_TEST_TOKEN")
	if address == "" || token == "" {
		t.Skip("no worker to talk to")
	}
	client, err := New(address, token, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := client.Compatible(ctx); err != nil {
		t.Fatal(err)
	}
	id := "goclienttest1"
	defer client.Unpublish(ctx, id)
	reply, err := client.Publish(ctx, id, Page{Title: "From Go", HTML: "<p>hi</p>", Files: []string{"abc123.txt"}})
	if err != nil || len(reply.Missing) != 1 {
		t.Fatalf("publish: %v, %v", reply, err)
	}
	if err := client.UploadFile(ctx, id, "abc123.txt", "text/plain", []byte("hello")); err != nil {
		t.Fatal(err)
	}
	res, err := http.Get(reply.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 || !strings.Contains(string(body), "<p>hi</p>") {
		t.Fatalf("the page: %d %s", res.StatusCode, body)
	}
}
