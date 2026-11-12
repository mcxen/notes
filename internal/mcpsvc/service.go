// Package mcpsvc is the Model Context Protocol server: the tools an AI
// assistant can call to search, read and edit the notes of the vaults the user
// allowed. It serves Streamable HTTP, guarded by a key the user sets, and the
// stdio carrier that forwards a client's stdio to that server.
package mcpsvc

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"notes/internal/markdown"
	"notes/internal/vault"
)

// VaultRef names a vault the assistants may use.
type VaultRef struct {
	Name   string
	Folder string
}

// Access is a vault for one tool call. Changed tells the app's windows what
// changed; Close ends the access (a vault that a window holds stays open).
type Access struct {
	Vault   *vault.Vault
	Changed func(vault.Changes)
	Close   func()
}

// Host is what the server needs from the app.
type Host interface {
	// Vaults lists the vaults the assistants may use; the first is the default.
	Vaults() []VaultRef
	// Open gives access to the vault in folder.
	Open(folder string) (*Access, error)
	// ReadOnly says whether changes are refused.
	ReadOnly() bool
}

// Author is who the server's changes are recorded as.
const (
	author     = "mcp"
	authorName = "Notes MCP"
	// coalesce is how long after an assistant's version the next one is kept
	// apart: edits within it share that version, as the app's history does.
	coalesce = 5 * time.Minute
)

// NewHandler serves the MCP server at its endpoint, for requests that carry key
// as "Authorization: Bearer <key>". The origin of a request is not checked: the
// key is the guard, and a proxy in front of the server may use any name.
func NewHandler(host Host, key, version string) http.Handler {
	srv := newServer(host, version)
	h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, &mcp.StreamableHTTPOptions{
		// Each request stands alone and is answered as one JSON object: a stdio
		// carrier, a proxy and a plain HTTP client all handle that.
		Stateless:                  true,
		JSONResponse:               true,
		DisableLocalhostProtection: true,
	})
	return bearer(key, h)
}

func bearer(key string, next http.Handler) http.Handler {
	want := []byte("Bearer " + key)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if key == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), want) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="notes"`)
			http.Error(w, "unauthorized: send Authorization: Bearer <key>", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type server struct {
	host Host
	mu   sync.Mutex // one tool call at a time: each may open a vault of its own
}

func newServer(host Host, version string) *mcp.Server {
	s := &server{host: host}
	srv := mcp.NewServer(&mcp.Implementation{Name: "notes", Title: "Notes", Version: version}, &mcp.ServerOptions{
		Instructions: "Read and edit the user's notes. Pages hold paragraphs; read a page with notes_fetch as Markdown, " +
			"change it with notes_update_page (append by default; replace swaps the body), and move pages to the Trash " +
			"with notes_trash_page. Pass vault to pick a vault by name; omit it for the default.",
	})
	mcp.AddTool(srv, &mcp.Tool{Name: "notes_list_vaults", Description: "List the vaults you may use, the first being the default."}, s.listVaults)
	mcp.AddTool(srv, &mcp.Tool{Name: "notes_search", Description: "Find pages by words in their title or text."}, s.search)
	mcp.AddTool(srv, &mcp.Tool{Name: "notes_list_pages", Description: "List the pages under a parent page, or at the top when parent_id is omitted."}, s.listPages)
	mcp.AddTool(srv, &mcp.Tool{Name: "notes_fetch", Description: "Read a page: its title and body as Markdown, and its sub-pages."}, s.fetch)
	mcp.AddTool(srv, &mcp.Tool{Name: "notes_create_page", Description: "Create a page with a title and an optional Markdown body, under parent_id when given."}, s.create)
	mcp.AddTool(srv, &mcp.Tool{Name: "notes_update_page", Description: "Change a page's title, and its body: mode append (default) adds Markdown to the end, replace swaps the whole body. The page's earlier version is kept in its history."}, s.update)
	mcp.AddTool(srv, &mcp.Tool{Name: "notes_trash_page", Description: "Move a page to the Trash. It can be restored from the Trash in Notes."}, s.trash)
	return srv
}

// ---- tool arguments and results -------------------------------------------

type listVaultsIn struct{}

type vaultOut struct {
	Name    string `json:"name"`
	Folder  string `json:"folder"`
	Default bool   `json:"default"`
}

type listVaultsOut struct {
	Vaults []vaultOut `json:"vaults"`
}

type searchIn struct {
	Vault string `json:"vault,omitempty" jsonschema:"the vault's name or folder; omit for the default"`
	Query string `json:"query" jsonschema:"the words to find in page titles and text"`
	Limit int    `json:"limit,omitempty" jsonschema:"at most this many pages (default 20)"`
}

type listPagesIn struct {
	Vault    string `json:"vault,omitempty" jsonschema:"the vault's name or folder; omit for the default"`
	ParentID string `json:"parent_id,omitempty" jsonschema:"the page whose sub-pages to list; omit for the top level"`
}

type pageHit struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	ParentID string `json:"parent_id,omitempty"`
	Snippet  string `json:"snippet,omitempty"`
}

type pagesOut struct {
	Pages []pageHit `json:"pages"`
}

type fetchIn struct {
	Vault  string `json:"vault,omitempty" jsonschema:"the vault's name or folder; omit for the default"`
	PageID string `json:"page_id" jsonschema:"the page's id, from notes_search or notes_list_pages"`
}

type fetchOut struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Markdown string    `json:"markdown"`
	SubPages []pageHit `json:"subpages"`
}

type createIn struct {
	Vault    string `json:"vault,omitempty" jsonschema:"the vault's name or folder; omit for the default"`
	Title    string `json:"title" jsonschema:"the page's title"`
	Markdown string `json:"markdown,omitempty" jsonschema:"the body, as Markdown"`
	ParentID string `json:"parent_id,omitempty" jsonschema:"the page to make it a sub-page of; omit for the top level"`
}

type pageOut struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Note  string `json:"note,omitempty"`
}

type updateIn struct {
	Vault    string `json:"vault,omitempty" jsonschema:"the vault's name or folder; omit for the default"`
	PageID   string `json:"page_id" jsonschema:"the page's id"`
	Title    string `json:"title,omitempty" jsonschema:"a new title"`
	Markdown string `json:"markdown,omitempty" jsonschema:"the new body, as Markdown"`
	Mode     string `json:"mode,omitempty" jsonschema:"append (default) adds to the end; replace swaps the whole body"`
}

type trashIn struct {
	Vault  string `json:"vault,omitempty" jsonschema:"the vault's name or folder; omit for the default"`
	PageID string `json:"page_id" jsonschema:"the page's id"`
}

// ---- vault access ----------------------------------------------------------

// pick finds the vault a call names, or the default.
func (s *server) pick(name string) (VaultRef, error) {
	refs := s.host.Vaults()
	if len(refs) == 0 {
		return VaultRef{}, errors.New("no vault is open to assistants: choose vaults in Notes › Settings › Integrations")
	}
	if strings.TrimSpace(name) == "" {
		return refs[0], nil
	}
	for _, ref := range refs {
		if strings.EqualFold(ref.Name, name) || filepath.Clean(ref.Folder) == filepath.Clean(name) {
			return ref, nil
		}
	}
	var names []string
	for _, ref := range refs {
		names = append(names, ref.Name)
	}
	return VaultRef{}, fmt.Errorf("the vault %q is not open to assistants; the vaults are: %s", name, strings.Join(names, ", "))
}

// within runs fn with the vault a call names, serialised with other calls.
func (s *server) within(name string, fn func(*vault.Vault, *Access) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ref, err := s.pick(name)
	if err != nil {
		return err
	}
	access, err := s.host.Open(ref.Folder)
	if err != nil {
		return err
	}
	defer access.Close()
	return fn(access.Vault, access)
}

// library is a vault's pages and their paragraphs, read once for a call.
type library struct {
	pages  map[string]map[string]any
	blocks map[string][]markdown.Block
}

func load(v *vault.Vault) library {
	lib := library{pages: map[string]map[string]any{}, blocks: map[string][]markdown.Block{}}
	for _, row := range v.SnapshotWithoutHistory().Records {
		switch row.Collection {
		case vault.Pages:
			lib.pages[row.ID] = row.Value
		case vault.Blocks:
			pageID, blockID, ok := vault.ParseBlockRecordID(row.ID)
			if ok {
				lib.blocks[pageID] = append(lib.blocks[pageID], markdown.FromRecord(blockID, row.Value))
			}
		}
	}
	return lib
}

func (lib library) title(id string) string {
	page := lib.pages[id]
	if t := markdown.StoredText(page["title_content"]); t != "" {
		return t
	}
	if s, ok := page["title"].(string); ok {
		return s
	}
	return ""
}

func (lib library) archived(id string) bool {
	return lib.pages[id]["archived"] == true
}

// children lists the pages under parent (or at the top), not in the Trash, by title.
func (lib library) children(parent string) []string {
	var ids []string
	for id, page := range lib.pages {
		if str(page["parent_id"]) == parent && !lib.archived(id) {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool {
		if a, b := lib.title(ids[i]), lib.title(ids[j]); a != b {
			return a < b
		}
		return ids[i] < ids[j]
	})
	return ids
}

func (lib library) hit(id, snippet string) pageHit {
	return pageHit{ID: id, Title: lib.title(id), ParentID: str(lib.pages[id]["parent_id"]), Snippet: snippet}
}

// ---- reads ------------------------------------------------------------------

func (s *server) listVaults(_ context.Context, _ *mcp.CallToolRequest, _ listVaultsIn) (*mcp.CallToolResult, listVaultsOut, error) {
	out := listVaultsOut{Vaults: []vaultOut{}}
	for i, ref := range s.host.Vaults() {
		out.Vaults = append(out.Vaults, vaultOut{Name: ref.Name, Folder: ref.Folder, Default: i == 0})
	}
	return nil, out, nil
}

func (s *server) search(_ context.Context, _ *mcp.CallToolRequest, in searchIn) (*mcp.CallToolResult, pagesOut, error) {
	out := pagesOut{Pages: []pageHit{}}
	query := strings.ToLower(strings.TrimSpace(in.Query))
	if query == "" {
		return nil, out, errors.New("the query is empty")
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	err := s.within(in.Vault, func(v *vault.Vault, _ *Access) error {
		lib := load(v)
		var ids []string
		for id := range lib.pages {
			if !lib.archived(id) {
				ids = append(ids, id)
			}
		}
		sort.Strings(ids)
		for _, id := range ids {
			if len(out.Pages) >= limit {
				break
			}
			if strings.Contains(strings.ToLower(lib.title(id)), query) {
				out.Pages = append(out.Pages, lib.hit(id, ""))
				continue
			}
			for _, b := range lib.blocks[id] {
				text := markdown.ToMarkdown([]markdown.Block{b}, noName)
				if strings.Contains(strings.ToLower(text), query) {
					out.Pages = append(out.Pages, lib.hit(id, snippet(text)))
					break
				}
			}
		}
		return nil
	})
	return nil, out, err
}

func (s *server) listPages(_ context.Context, _ *mcp.CallToolRequest, in listPagesIn) (*mcp.CallToolResult, pagesOut, error) {
	out := pagesOut{Pages: []pageHit{}}
	err := s.within(in.Vault, func(v *vault.Vault, _ *Access) error {
		lib := load(v)
		if in.ParentID != "" {
			if _, ok := lib.pages[in.ParentID]; !ok {
				return fmt.Errorf("no page %q in this vault", in.ParentID)
			}
		}
		for _, id := range lib.children(in.ParentID) {
			out.Pages = append(out.Pages, lib.hit(id, ""))
		}
		return nil
	})
	return nil, out, err
}

func (s *server) fetch(_ context.Context, _ *mcp.CallToolRequest, in fetchIn) (*mcp.CallToolResult, fetchOut, error) {
	out := fetchOut{SubPages: []pageHit{}}
	err := s.within(in.Vault, func(v *vault.Vault, _ *Access) error {
		lib := load(v)
		if _, ok := lib.pages[in.PageID]; !ok {
			return fmt.Errorf("no page %q in this vault", in.PageID)
		}
		out.ID = in.PageID
		out.Title = lib.title(in.PageID)
		out.Markdown = markdown.ToMarkdown(lib.blocks[in.PageID], noName)
		for _, id := range lib.children(in.PageID) {
			out.SubPages = append(out.SubPages, lib.hit(id, ""))
		}
		return nil
	})
	return nil, out, err
}

// ---- writes -----------------------------------------------------------------

func (s *server) create(_ context.Context, _ *mcp.CallToolRequest, in createIn) (*mcp.CallToolResult, pageOut, error) {
	var out pageOut
	if s.host.ReadOnly() {
		return nil, out, errors.New("this server is read-only: changes are turned off in Notes › Settings › Integrations")
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return nil, out, errors.New("the title is empty")
	}
	err := s.within(in.Vault, func(v *vault.Vault, access *Access) error {
		lib := load(v)
		if in.ParentID != "" && (lib.pages[in.ParentID] == nil || lib.archived(in.ParentID)) {
			return fmt.Errorf("no page %q to put it under in this vault", in.ParentID)
		}
		now := stamp()
		id := newID("page")
		page := map[string]any{"id": id, "kind": "document", "icon": "", "title_content": markdown.StoredRuns(title),
			"created_at": now, "updated_at": now, "created_by": authorName, "updated_by": authorName}
		if in.ParentID != "" {
			page["parent_id"] = in.ParentID
		}
		writes := []vault.Write{{Action: "put", Collection: vault.Pages, ID: id, Value: page}}
		writes = append(writes, bodyWrites(id, markdown.FromMarkdown(in.Markdown, func() string { return newID("block") }), 0)...)
		changes, err := apply(v, writes)
		if err != nil {
			return err
		}
		access.Changed(vault.Changes{Changes: changes})
		out = pageOut{ID: id, Title: title}
		return nil
	})
	return nil, out, err
}

func (s *server) update(_ context.Context, _ *mcp.CallToolRequest, in updateIn) (*mcp.CallToolResult, pageOut, error) {
	var out pageOut
	if s.host.ReadOnly() {
		return nil, out, errors.New("this server is read-only: changes are turned off in Notes › Settings › Integrations")
	}
	mode := in.Mode
	if mode == "" {
		mode = "append"
	}
	if mode != "append" && mode != "replace" {
		return nil, out, fmt.Errorf("mode %q is not append or replace", mode)
	}
	err := s.within(in.Vault, func(v *vault.Vault, access *Access) error {
		lib := load(v)
		page, ok := lib.pages[in.PageID]
		if !ok {
			return fmt.Errorf("no page %q in this vault", in.PageID)
		}
		if lib.archived(in.PageID) {
			return errors.New("the page is in the Trash: restore it in Notes first")
		}
		now := stamp()
		var writes []vault.Write
		// The version to keep is the page as it was before this run of changes.
		cp, err := checkpoint(v, in.PageID, page, lib.blocks[in.PageID], now)
		if err != nil {
			return err
		}
		writes = append(writes, cp...)
		if in.Markdown != "" {
			existing := lib.blocks[in.PageID]
			fresh := markdown.FromMarkdown(in.Markdown, func() string { return newID("block") })
			switch mode {
			case "replace":
				for _, b := range existing {
					if b.HasMentionsOrFiles() {
						return errors.New("this page holds pictures, files, tables, embeds or page links, which Markdown cannot carry back: use mode append, or edit the page in Notes")
					}
					writes = append(writes, vault.Write{Action: "delete", Collection: vault.Blocks, ID: vault.BlockRecordID(in.PageID, b.ID)})
				}
				writes = append(writes, bodyWrites(in.PageID, fresh, 0)...)
			default:
				var last float64
				for _, b := range existing {
					if b.ParentID == "" && b.Order > last {
						last = b.Order
					}
				}
				writes = append(writes, bodyWrites(in.PageID, fresh, last)...)
			}
		}
		patch := map[string]any{"updated_at": now, "updated_by": authorName}
		if t := strings.TrimSpace(in.Title); t != "" {
			patch["title_content"] = markdown.StoredRuns(t)
		}
		writes = append(writes, vault.Write{Action: "patch", Collection: vault.Pages, ID: in.PageID, Patch: patch})
		changes, err := apply(v, writes)
		if err != nil {
			return err
		}
		access.Changed(vault.Changes{Changes: changes})
		out = pageOut{ID: in.PageID, Title: lib.title(in.PageID)}
		if t := strings.TrimSpace(in.Title); t != "" {
			out.Title = t
		}
		return nil
	})
	return nil, out, err
}

func (s *server) trash(_ context.Context, _ *mcp.CallToolRequest, in trashIn) (*mcp.CallToolResult, pageOut, error) {
	var out pageOut
	if s.host.ReadOnly() {
		return nil, out, errors.New("this server is read-only: changes are turned off in Notes › Settings › Integrations")
	}
	err := s.within(in.Vault, func(v *vault.Vault, access *Access) error {
		lib := load(v)
		if _, ok := lib.pages[in.PageID]; !ok {
			return fmt.Errorf("no page %q in this vault", in.PageID)
		}
		out = pageOut{ID: in.PageID, Title: lib.title(in.PageID)}
		if lib.archived(in.PageID) {
			out.Note = "already in the Trash"
			return nil
		}
		now := stamp()
		changes, err := apply(v, []vault.Write{{Action: "patch", Collection: vault.Pages, ID: in.PageID,
			Patch: map[string]any{"archived": true, "archived_at": now, "updated_at": now, "updated_by": authorName}}})
		if err != nil {
			return err
		}
		access.Changed(vault.Changes{Changes: changes})
		out.Note = "moved to the Trash"
		return nil
	})
	return nil, out, err
}

// bodyWrites puts paragraphs of a page. Top-level paragraphs are numbered on
// from base, so they follow what is already there.
func bodyWrites(pageID string, blocks []markdown.Block, base float64) []vault.Write {
	writes := make([]vault.Write, 0, len(blocks))
	for _, b := range blocks {
		if b.ParentID == "" {
			b.Order += base
		}
		writes = append(writes, vault.Write{Action: "put", Collection: vault.Blocks,
			ID: vault.BlockRecordID(pageID, b.ID), Value: b.ToRecord()})
	}
	return writes
}

// apply makes the writes, saves them, and says what changed.
func apply(v *vault.Vault, writes []vault.Write) ([]vault.Change, error) {
	var changes []vault.Change
	for _, w := range writes {
		ack, err := v.Write(w)
		if err != nil {
			return changes, err
		}
		changes = append(changes, vault.Change{Collection: w.Collection, ID: w.ID, Value: ack.Value, Deleted: ack.Value == nil})
	}
	if err := v.Flush(); err != nil {
		return changes, err
	}
	return changes, nil
}

// checkpoint keeps the page as it is now, unless an assistant's version of it
// was kept within the coalescing window: that one already holds the state from
// before this run of changes.
func checkpoint(v *vault.Vault, pageID string, page map[string]any, blocks []markdown.Block, now string) ([]vault.Write, error) {
	at, _ := time.Parse("2006-01-02T15:04:05.000Z", now)
	for _, row := range v.VersionsOf(pageID) {
		var version struct {
			AuthorID  string `json:"author_id"`
			CreatedAt string `json:"created_at"`
		}
		if err := json.Unmarshal([]byte(str(row.Value["payload"])), &version); err != nil {
			continue
		}
		then, err := time.Parse(time.RFC3339Nano, version.CreatedAt)
		if err != nil {
			then, _ = time.Parse("2006-01-02T15:04:05.000Z", version.CreatedAt)
		}
		if version.AuthorID == author && !then.IsZero() && at.Sub(then) < coalesce {
			return nil, nil
		}
	}
	versionID := newID("version")
	snapshot := map[string]any{
		"id": pageID, "kind": "document", "icon": str(page["icon"]),
		"title": markdown.StoredText(page["title_content"]), "title_content": markdown.WebRuns(page["title_content"]),
		"created_at": str(page["created_at"]), "updated_at": str(page["updated_at"]),
		"created_by": str(page["created_by"]), "updated_by": str(page["updated_by"]),
	}
	if parent := str(page["parent_id"]); parent != "" {
		snapshot["parent_id"] = parent
	}
	tree := map[string]any{}
	for _, b := range blocks {
		record := b.ToRecord()
		record["content"] = markdown.WebRuns(record["content"])
		tree[b.ID] = record
	}
	snapshot["block_tree"] = map[string]any{"by_id": tree}
	payload, err := json.Marshal(map[string]any{"id": versionID, "page_id": pageID, "author_id": author, "author_name": authorName,
		"summary": "Changed by an assistant", "created_at": now, "snapshot": snapshot})
	if err != nil {
		return nil, err
	}
	return []vault.Write{{Action: "put", Collection: vault.Versions, ID: vault.HistoryRecordID(pageID, versionID, 0),
		Value: map[string]any{"page": pageID, "version": versionID, "generation": newID("gen"), "created_at": now,
			"part": 0, "parts": 1, "payload": string(payload)}}}, nil
}

// ---- small helpers ----------------------------------------------------------

func noName(map[string]any) string { return "" }

func str(v any) string {
	s, _ := v.(string)
	return s
}

func stamp() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
}

func newID(prefix string) string {
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Sprintf("%s_%016x", prefix, time.Now().UnixNano())
	}
	return prefix + "_" + hex.EncodeToString(raw)
}

func snippet(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > 160 {
		return text[:160] + "…"
	}
	return text
}

// NewKey makes a key for the server: "sk-" and 40 hex digits.
func NewKey() string {
	raw := make([]byte, 20)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Sprintf("sk-%040x", time.Now().UnixNano())
	}
	return "sk-" + hex.EncodeToString(raw)
}
