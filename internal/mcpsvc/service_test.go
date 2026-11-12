package mcpsvc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"notes/internal/vault"
)

type testHost struct {
	folders  []string
	readOnly bool
}

func (h testHost) Vaults() []VaultRef {
	var refs []VaultRef
	for _, f := range h.folders {
		refs = append(refs, VaultRef{Name: filepath.Base(f), Folder: f})
	}
	return refs
}

func (h testHost) Open(folder string) (*Access, error) {
	v, err := vault.Open(folder)
	if err != nil {
		return nil, err
	}
	return &Access{Vault: v, Changed: func(vault.Changes) {}, Close: func() { _ = v.Close() }}, nil
}

func (h testHost) ReadOnly() bool { return h.readOnly }

type client struct {
	t        *testing.T
	url, key string
	version  string
	nextID   int
}

// call sends one JSON-RPC request and returns the decoded response.
func (c *client) call(method string, params any, auth string) map[string]any {
	c.t.Helper()
	c.nextID++
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": c.nextID, "method": method, "params": params})
	req, _ := http.NewRequest(http.MethodPost, c.url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	if c.version != "" {
		req.Header.Set("MCP-Protocol-Version", c.version)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return map[string]any{"_status": resp.StatusCode, "_body": string(raw)}
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		c.t.Fatalf("response is not JSON: %v\n%s", err, raw)
	}
	return out
}

func (c *client) tool(name string, args map[string]any) map[string]any {
	c.t.Helper()
	res := c.call("tools/call", map[string]any{"name": name, "arguments": args}, "Bearer "+c.key)
	result, ok := res["result"].(map[string]any)
	if !ok {
		c.t.Fatalf("%s: no result: %v", name, res)
	}
	if result["isError"] == true {
		return map[string]any{"_error": fmt.Sprint(result["content"])}
	}
	out, _ := result["structuredContent"].(map[string]any)
	return out
}

func newClient(t *testing.T, host Host, key string) *client {
	t.Helper()
	ts := httptest.NewServer(NewHandler(host, key, "test"))
	t.Cleanup(ts.Close)
	c := &client{t: t, url: ts.URL + "/mcp", key: key}
	init := c.call("initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "test", "version": "1"}}, "Bearer "+key)
	result, ok := init["result"].(map[string]any)
	if !ok {
		t.Fatalf("initialize: %v", init)
	}
	c.version, _ = result["protocolVersion"].(string)
	return c
}

func TestTheKeyIsRequired(t *testing.T) {
	host := testHost{folders: []string{t.TempDir()}}
	c := newClient(t, host, "sk-test")
	if res := c.call("tools/list", map[string]any{}, ""); res["_status"] != http.StatusUnauthorized {
		t.Fatalf("no key: %v", res)
	}
	if res := c.call("tools/list", map[string]any{}, "Bearer sk-wrong"); res["_status"] != http.StatusUnauthorized {
		t.Fatalf("wrong key: %v", res)
	}
	if res := c.call("tools/list", map[string]any{}, "Bearer sk-test"); res["result"] == nil {
		t.Fatalf("right key: %v", res)
	}
}

func TestAnEmptyKeyNeverOpensTheServer(t *testing.T) {
	c := newClient(t, testHost{folders: []string{t.TempDir()}}, "sk-test")
	c.key = ""
	if res := c.call("tools/list", map[string]any{}, "Bearer "); res["_status"] != http.StatusUnauthorized {
		t.Fatalf("an empty key: %v", res)
	}
}

func TestToolsWriteAndReadAVault(t *testing.T) {
	dir := t.TempDir()
	c := newClient(t, testHost{folders: []string{dir}}, "sk-test")

	tools := c.call("tools/list", map[string]any{}, "Bearer sk-test")["result"].(map[string]any)["tools"].([]any)
	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.(map[string]any)["name"].(string)] = true
	}
	for _, want := range []string{"notes_list_vaults", "notes_search", "notes_list_pages", "notes_fetch", "notes_create_page", "notes_update_page", "notes_trash_page"} {
		if !names[want] {
			t.Errorf("tool %s is not listed", want)
		}
	}

	created := c.tool("notes_create_page", map[string]any{"title": "Trip", "markdown": "# Plan\n- flights\n- [ ] book hotel"})
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatalf("create: %v", created)
	}
	fetched := c.tool("notes_fetch", map[string]any{"page_id": id})
	if fetched["markdown"] != "# Plan\n- flights\n- [ ] book hotel" {
		t.Fatalf("fetch: %q", fetched["markdown"])
	}

	c.tool("notes_update_page", map[string]any{"page_id": id, "markdown": "- visa"})
	after := c.tool("notes_fetch", map[string]any{"page_id": id})
	if after["markdown"] != "# Plan\n- flights\n- [ ] book hotel\n- visa" {
		t.Fatalf("append: %q", after["markdown"])
	}

	c.tool("notes_update_page", map[string]any{"page_id": id, "title": "Trip to Lisbon", "markdown": "## Only this now", "mode": "replace"})
	replaced := c.tool("notes_fetch", map[string]any{"page_id": id})
	if replaced["markdown"] != "## Only this now" || replaced["title"] != "Trip to Lisbon" {
		t.Fatalf("replace: %q / %q", replaced["markdown"], replaced["title"])
	}

	found := c.tool("notes_search", map[string]any{"query": "lisbon"})
	if pages, _ := found["pages"].([]any); len(pages) != 1 {
		t.Fatalf("search by title: %v", found)
	}
	found = c.tool("notes_search", map[string]any{"query": "only this"})
	if pages, _ := found["pages"].([]any); len(pages) != 1 {
		t.Fatalf("search by text: %v", found)
	}

	c.tool("notes_trash_page", map[string]any{"page_id": id})
	listed := c.tool("notes_list_pages", map[string]any{})
	if pages, _ := listed["pages"].([]any); len(pages) != 0 {
		t.Fatalf("a trashed page is listed: %v", listed)
	}

	// The writes are in the files, as the app reads them.
	v, err := vault.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	if n := len(v.VersionsOf(id)); n == 0 {
		t.Error("an update kept no version of the page")
	}
}

func TestOneVersionPerRunOfChanges(t *testing.T) {
	dir := t.TempDir()
	c := newClient(t, testHost{folders: []string{dir}}, "sk-test")
	id := c.tool("notes_create_page", map[string]any{"title": "Runs", "markdown": "a"})["id"].(string)
	c.tool("notes_update_page", map[string]any{"page_id": id, "markdown": "b"})
	c.tool("notes_update_page", map[string]any{"page_id": id, "markdown": "c"})
	v, _ := vault.Open(dir)
	defer v.Close()
	if n := len(v.VersionsOf(id)); n != 1 {
		t.Fatalf("three edits in a row kept %d versions, want one (the state before them)", n)
	}
}

func TestReadOnlyRefusesChanges(t *testing.T) {
	dir := t.TempDir()
	host := testHost{folders: []string{dir}}
	c := newClient(t, host, "sk-test")
	id := c.tool("notes_create_page", map[string]any{"title": "Keep", "markdown": "text"})["id"].(string)
	host.readOnly = true
	c2 := newClient(t, host, "sk-test")
	if res := c2.tool("notes_update_page", map[string]any{"page_id": id, "markdown": "changed"}); res["_error"] == nil {
		t.Fatalf("a read-only server changed a page: %v", res)
	}
	if res := c2.tool("notes_fetch", map[string]any{"page_id": id}); res["markdown"] != "text" {
		t.Fatalf("reading in read-only mode: %v", res)
	}
}

func TestVaultsAreChosenByName(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	host := testHost{folders: []string{a}}
	c := newClient(t, host, "sk-test")
	if res := c.tool("notes_list_vaults", map[string]any{}); len(res["vaults"].([]any)) != 1 {
		t.Fatalf("list: %v", res)
	}
	if res := c.tool("notes_search", map[string]any{"query": "x", "vault": filepath.Base(b)}); res["_error"] == nil || !strings.Contains(fmt.Sprint(res["_error"]), "not open to assistants") {
		t.Fatalf("a vault that is not allowed: %v", res)
	}
}

func TestDefaultVaultIsTheFirst(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	c := newClient(t, testHost{folders: []string{a, b}}, "sk-test")
	id := c.tool("notes_create_page", map[string]any{"title": "Second vault", "vault": filepath.Base(b)})["id"].(string)
	if res := c.tool("notes_fetch", map[string]any{"page_id": id}); res["_error"] == nil {
		t.Fatal("a page of the second vault was found in the default one")
	}
	if res := c.tool("notes_fetch", map[string]any{"page_id": id, "vault": filepath.Base(b)}); res["title"] != "Second vault" {
		t.Fatalf("the named vault: %v", res)
	}
}

func TestNewKeyHasTheSkPrefix(t *testing.T) {
	k := NewKey()
	if !strings.HasPrefix(k, "sk-") || len(k) != 43 || k == NewKey() {
		t.Fatalf("key %q", k)
	}
}
