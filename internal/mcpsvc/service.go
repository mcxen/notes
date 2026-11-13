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
	"unicode"

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
		Instructions: "Read and edit the user's notes. Pages hold paragraphs; notes_fetch reads a page: a few lines about it, then a line of ---, " +
			"then its body as Markdown. Change a part of a page with notes_edit_page (old_text to new_text, one paragraph at a time), add to its end " +
			"with notes_update_page (append by default; replace swaps the whole body), and move pages to the Trash with notes_trash_page. " +
			"[[A page's title]] in Markdown is a link to that page. Pictures and files on a page are listed by notes_fetch; open one with " +
			"notes_read_attachment and add one with notes_add_attachment. Pass vault to pick a vault by name; omit it for the default.",
	})
	mcp.AddTool(srv, &mcp.Tool{Name: "notes_list_vaults", Description: "List the vaults you may use, the first being the default."}, s.listVaults)
	mcp.AddTool(srv, &mcp.Tool{Name: "notes_search", Description: "Find pages by words in their title or text: every word of the query has to be there, in any order. The best matches come first (the title before the text, then the most recently changed)."}, s.search)
	mcp.AddTool(srv, &mcp.Tool{Name: "notes_list_pages", Description: "List the pages under a parent page, or at the top when parent_id is omitted."}, s.listPages)
	mcp.AddTool(srv, &mcp.Tool{Name: "notes_recent_pages", Description: "List the pages changed most recently, newest first."}, s.recent)
	mcp.AddTool(srv, &mcp.Tool{Name: "notes_fetch", Description: "Read a page. The answer is text: a few lines about the page (its title, id, parent, sub-pages, the pages it links to and is linked from, its attachments with the path notes_read_attachment takes), then a line of ---, then its body as Markdown."}, s.fetch)
	mcp.AddTool(srv, &mcp.Tool{Name: "notes_create_page", Description: "Create a page with a title and an optional Markdown body, under parent_id when given. With if_exists append, a page that already has this title (under the same parent) gets the Markdown added to it instead: safe to call again and again."}, s.create)
	mcp.AddTool(srv, &mcp.Tool{Name: "notes_update_page", Description: "Change a page's title, and its body: mode append (default) adds Markdown to the end, replace swaps the whole body. To change a part of a page, use notes_edit_page. The page's earlier version is kept in its history."}, s.update)
	mcp.AddTool(srv, &mcp.Tool{Name: "notes_edit_page", Description: "Change a part of a page: old_text, as notes_fetch shows it, becomes new_text. old_text has to be within one paragraph (one line, one code block or one quote) and be there once; make it longer, or pass nth, when it is there several times. new_text may be several lines: the first takes the paragraph's place, the rest follow it. An empty new_text that leaves the paragraph empty removes it. Pictures, links to pages and everything else on the page stay as they are. The page's earlier version is kept in its history."}, s.edit)
	mcp.AddTool(srv, &mcp.Tool{Name: "notes_move_page", Description: "Move a page under another page, or to the top level when parent_id is omitted. Its sub-pages go with it."}, s.move)
	mcp.AddTool(srv, &mcp.Tool{Name: "notes_read_attachment", Description: "Open a picture or file on a page, by a path from the attachments of notes_fetch: a picture is shown as an image, a text file as text, any other file as bytes. Very large pictures come as their smaller preview."}, s.readAttachment)
	mcp.AddTool(srv, &mcp.Tool{Name: "notes_add_attachment", Description: "Put a picture or file on a page, after what it holds: name it, and give its bytes base64-encoded (up to 2.5 MB). The page's earlier version is kept in its history."}, s.addAttachment)
	mcp.AddTool(srv, &mcp.Tool{Name: "notes_trash_page", Description: "Move a page to the Trash. It can be restored with notes_restore_page, or from the Trash in Notes."}, s.trash)
	mcp.AddTool(srv, &mcp.Tool{Name: "notes_restore_page", Description: "Take a page back out of the Trash."}, s.restore)
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
	Limit int    `json:"limit,omitempty" jsonschema:"at most this many pages (default 20, up to 100)"`
}

type recentIn struct {
	Vault string `json:"vault,omitempty" jsonschema:"the vault's name or folder; omit for the default"`
	Limit int    `json:"limit,omitempty" jsonschema:"at most this many pages (default 20, up to 100)"`
}

type listPagesIn struct {
	Vault    string `json:"vault,omitempty" jsonschema:"the vault's name or folder; omit for the default"`
	ParentID string `json:"parent_id,omitempty" jsonschema:"the page whose sub-pages to list; omit for the top level"`
}

type pageHit struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	ParentID  string `json:"parent_id,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
	Snippet   string `json:"snippet,omitempty"`
}

type pagesOut struct {
	Pages []pageHit `json:"pages"`
}

type fetchIn struct {
	Vault  string `json:"vault,omitempty" jsonschema:"the vault's name or folder; omit for the default"`
	PageID string `json:"page_id" jsonschema:"the page's id, from notes_search or notes_list_pages"`
}

type createIn struct {
	Vault    string `json:"vault,omitempty" jsonschema:"the vault's name or folder; omit for the default"`
	Title    string `json:"title" jsonschema:"the page's title"`
	Markdown string `json:"markdown,omitempty" jsonschema:"the body, as Markdown"`
	ParentID string `json:"parent_id,omitempty" jsonschema:"the page to make it a sub-page of; omit for the top level"`
	IfExists string `json:"if_exists,omitempty" jsonschema:"append: when a page with this title is already there under the same parent, add the Markdown to it instead of making another; omit to always make a page"`
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

type moveIn struct {
	Vault    string `json:"vault,omitempty" jsonschema:"the vault's name or folder; omit for the default"`
	PageID   string `json:"page_id" jsonschema:"the page to move"`
	ParentID string `json:"parent_id,omitempty" jsonschema:"the page to put it under; omit for the top level"`
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
	// stored is each page's paragraphs as the vault keeps them, by id: a
	// version of the page keeps all of a paragraph, not only what Markdown says.
	stored map[string]map[string]map[string]any
	// byTitle is the pages not in the Trash by their title in lower case, for
	// a [[title]] written in Markdown.
	byTitle map[string][]string
}

func load(v *vault.Vault) library {
	lib := library{pages: map[string]map[string]any{}, blocks: map[string][]markdown.Block{},
		stored: map[string]map[string]map[string]any{}}
	for _, row := range v.SnapshotWithoutHistory().Records {
		switch row.Collection {
		case vault.Pages:
			lib.pages[row.ID] = row.Value
		case vault.Blocks:
			pageID, blockID, ok := vault.ParseBlockRecordID(row.ID)
			if ok {
				lib.blocks[pageID] = append(lib.blocks[pageID], markdown.FromRecord(blockID, row.Value))
				if lib.stored[pageID] == nil {
					lib.stored[pageID] = map[string]map[string]any{}
				}
				lib.stored[pageID][blockID] = row.Value
			}
		}
	}
	lib.byTitle = map[string][]string{}
	for id := range lib.pages {
		if title := strings.ToLower(strings.TrimSpace(lib.title(id))); title != "" && !lib.trashed(id) {
			lib.byTitle[title] = append(lib.byTitle[title], id)
		}
	}
	return lib
}

// shown is a page's title as it is said: on one line, and never empty.
func (lib library) shown(id string) string {
	if title := strings.Join(strings.Fields(lib.title(id)), " "); title != "" {
		return title
	}
	return "Untitled"
}

// name says the words of a mention: a page by its title, a date as it is kept.
func (lib library) name(mention map[string]any) string {
	switch str(mention["kind"]) {
	case "page":
		if id := str(mention["page_id"]); lib.pages[id] != nil {
			return lib.shown(id)
		}
		return ""
	case "date":
		return str(mention["date"])
	}
	return ""
}

// resolve finds the page a [[name]] means: a page's id, or the title of the
// one page that has it. Nil when no page, or more than one, answers to it.
func (lib library) resolve(name string) map[string]any {
	link := func(id string) map[string]any { return map[string]any{"kind": "page", "page_id": id} }
	if lib.pages[name] != nil && !lib.trashed(name) {
		return link(name)
	}
	if ids := lib.byTitle[strings.ToLower(name)]; len(ids) == 1 {
		return link(ids[0])
	}
	return nil
}

// read reads Markdown into paragraphs, a [[page's title]] as a link to it.
func (lib library) read(src string) []markdown.Block {
	return markdown.LinkPages(markdown.FromMarkdown(src, func() string { return newID("block") }), lib.resolve)
}

// linked lists the pages the paragraphs link to, each once, in reading order.
func (lib library) linked(blocks []markdown.Block) []string {
	var ids []string
	seen := map[string]bool{}
	for _, entry := range markdown.Outline(blocks, lib.name) {
		for _, run := range entry.Block.Runs {
			if id := str(run.Mention["page_id"]); str(run.Mention["kind"]) == "page" && lib.pages[id] != nil && !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	return ids
}

// backlinks lists the pages, not in the Trash, that link to id, by title.
func (lib library) backlinks(id string) []string {
	var ids []string
	for from, blocks := range lib.blocks {
		if from == id || lib.pages[from] == nil || lib.trashed(from) {
			continue
		}
	scan:
		for _, b := range blocks {
			for _, run := range b.Runs {
				if str(run.Mention["kind"]) == "page" && str(run.Mention["page_id"]) == id {
					ids = append(ids, from)
					break scan
				}
			}
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

// trashed says whether a page is in the Trash: itself, or a page above it, as
// the app reads it. A page whose parents loop is read only as far as the loop.
func (lib library) trashed(id string) bool {
	seen := map[string]bool{}
	for id != "" && !seen[id] {
		seen[id] = true
		page, ok := lib.pages[id]
		if !ok {
			return false
		}
		if page["archived"] == true {
			return true
		}
		id = str(page["parent_id"])
	}
	return false
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
	return pageHit{ID: id, Title: lib.title(id), ParentID: str(lib.pages[id]["parent_id"]),
		UpdatedAt: str(lib.pages[id]["updated_at"]), Snippet: snippet}
}

// ---- reads ------------------------------------------------------------------

func (s *server) listVaults(_ context.Context, _ *mcp.CallToolRequest, _ listVaultsIn) (*mcp.CallToolResult, listVaultsOut, error) {
	out := listVaultsOut{Vaults: []vaultOut{}}
	for i, ref := range s.host.Vaults() {
		out.Vaults = append(out.Vaults, vaultOut{Name: ref.Name, Folder: ref.Folder, Default: i == 0})
	}
	return nil, out, nil
}

// most is how many pages a list holds: asked, 20 unless said, never over 100.
func most(asked int) int {
	switch {
	case asked <= 0:
		return 20
	case asked > 100:
		return 100
	}
	return asked
}

func (s *server) search(_ context.Context, _ *mcp.CallToolRequest, in searchIn) (*mcp.CallToolResult, pagesOut, error) {
	out := pagesOut{Pages: []pageHit{}}
	words := strings.Fields(strings.ToLower(in.Query))
	if len(words) == 0 {
		return nil, out, errors.New("the query is empty")
	}
	query := strings.Join(words, " ")
	err := s.within(in.Vault, func(v *vault.Vault, _ *Access) error {
		lib := load(v)
		type found struct {
			hit  pageHit
			rank int
		}
		var all []found
		for id := range lib.pages {
			if lib.trashed(id) {
				continue
			}
			title := strings.ToLower(lib.title(id))
			// The words of each paragraph, as they are read: without marks, and
			// a link to a page by the page's title.
			var plain, lower []string
			for _, entry := range markdown.Outline(lib.blocks[id], lib.name) {
				text := strings.Join(strings.Fields(markdown.Words(entry.Block, lib.name)), " ")
				if text != "" {
					plain = append(plain, text)
					lower = append(lower, strings.ToLower(text))
				}
			}
			within := func(word string) int {
				for i, text := range lower {
					if strings.Contains(text, word) {
						return i
					}
				}
				return -1
			}
			inTitle, missing := 0, false
			for _, word := range words {
				if strings.Contains(title, word) {
					inTitle++
				} else if within(word) < 0 {
					missing = true
					break
				}
			}
			if missing {
				continue
			}
			// The words together in the title, then all of them there, then
			// some of them, then together in the text.
			rank := inTitle * 10
			if strings.Contains(title, query) {
				rank += 1000
			}
			if inTitle == len(words) {
				rank += 500
			}
			// The paragraph shown: the one with the words together, else the
			// first with a word the title does not have, else with any of them.
			at, around := within(query), query
			if at >= 0 {
				rank += 5
			}
			for _, word := range words {
				if at < 0 && !strings.Contains(title, word) {
					at, around = within(word), word
				}
			}
			for _, word := range words {
				if at < 0 {
					at, around = within(word), word
				}
			}
			text := ""
			if at >= 0 {
				text = snippet(plain[at], around)
			}
			all = append(all, found{hit: lib.hit(id, text), rank: rank})
		}
		sort.Slice(all, func(i, j int) bool {
			a, b := all[i], all[j]
			if a.rank != b.rank {
				return a.rank > b.rank
			}
			if a.hit.UpdatedAt != b.hit.UpdatedAt {
				return a.hit.UpdatedAt > b.hit.UpdatedAt
			}
			return a.hit.ID < b.hit.ID
		})
		for _, f := range all {
			if len(out.Pages) >= most(in.Limit) {
				break
			}
			out.Pages = append(out.Pages, f.hit)
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

func (s *server) recent(_ context.Context, _ *mcp.CallToolRequest, in recentIn) (*mcp.CallToolResult, pagesOut, error) {
	out := pagesOut{Pages: []pageHit{}}
	err := s.within(in.Vault, func(v *vault.Vault, _ *Access) error {
		lib := load(v)
		for id := range lib.pages {
			if !lib.trashed(id) {
				out.Pages = append(out.Pages, lib.hit(id, ""))
			}
		}
		sort.Slice(out.Pages, func(i, j int) bool {
			if a, b := out.Pages[i].UpdatedAt, out.Pages[j].UpdatedAt; a != b {
				return a > b
			}
			return out.Pages[i].ID < out.Pages[j].ID
		})
		if n := most(in.Limit); len(out.Pages) > n {
			out.Pages = out.Pages[:n]
		}
		return nil
	})
	return nil, out, err
}

// fetch answers in words, once: what is said about the page, a line of ---,
// then its body. (A result with a structure is sent twice over, as text too.)
func (s *server) fetch(_ context.Context, _ *mcp.CallToolRequest, in fetchIn) (*mcp.CallToolResult, any, error) {
	var text string
	err := s.within(in.Vault, func(v *vault.Vault, _ *Access) error {
		lib := load(v)
		if _, ok := lib.pages[in.PageID]; !ok {
			return fmt.Errorf("no page %q in this vault", in.PageID)
		}
		text = lib.pageText(in.PageID)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil, nil
}

// bodyMark is the line between what is said about a page and its body.
const bodyMark = "---"

// pageText is a page as notes_fetch gives it.
func (lib library) pageText(id string) string {
	page, blocks := lib.pages[id], lib.blocks[id]
	var b strings.Builder
	fmt.Fprintf(&b, "Title: %s\nPage: %s\n", lib.shown(id), id)
	if parent := str(page["parent_id"]); lib.pages[parent] != nil {
		fmt.Fprintf(&b, "Parent: %s (%s)\n", lib.shown(parent), parent)
	}
	if str(page["kind"]) == "database" {
		b.WriteString("Kind: database (its rows are its sub-pages)\n")
	}
	if updated := str(page["updated_at"]); updated != "" {
		fmt.Fprintf(&b, "Updated: %s\n", updated)
	}
	if lib.trashed(id) {
		b.WriteString("In the Trash: yes (it can be read; restore it before changing it)\n")
	}
	pages := func(label string, ids []string) {
		if len(ids) == 0 {
			return
		}
		b.WriteString(label + ":\n")
		for _, id := range ids {
			fmt.Fprintf(&b, "- %s (%s)\n", lib.shown(id), id)
		}
	}
	pages("Sub-pages", lib.children(id))
	pages("Links to", lib.linked(blocks))
	pages("Linked from", lib.backlinks(id))
	if files := attachmentsOf(blocks); len(files) > 0 {
		b.WriteString("Attachments:\n")
		for _, f := range files {
			kind := f.Type
			if f.Picture {
				kind += ", a picture"
			}
			fmt.Fprintf(&b, "- %s (%s, %s) path: %s\n", f.Name, kind, sizeWords(f.Size), f.Path)
		}
	}
	b.WriteString("\n" + bodyMark + "\n")
	b.WriteString(markdown.ToMarkdown(blocks, lib.name))
	return b.String()
}

// ---- writes -----------------------------------------------------------------

const readOnlyError = "this server is read-only: changes are turned off in Notes › Settings › Integrations"

func (s *server) create(_ context.Context, _ *mcp.CallToolRequest, in createIn) (*mcp.CallToolResult, pageOut, error) {
	var out pageOut
	if s.host.ReadOnly() {
		return nil, out, errors.New(readOnlyError)
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return nil, out, errors.New("the title is empty")
	}
	if in.IfExists != "" && in.IfExists != "append" {
		return nil, out, fmt.Errorf("if_exists %q is not append", in.IfExists)
	}
	err := s.within(in.Vault, func(v *vault.Vault, access *Access) error {
		lib := load(v)
		if in.ParentID != "" && (lib.pages[in.ParentID] == nil || lib.trashed(in.ParentID)) {
			return fmt.Errorf("no page %q to put it under in this vault", in.ParentID)
		}
		if in.IfExists == "append" {
			// The page of this title in the same place, the oldest when there are several.
			var same []string
			for _, id := range lib.byTitle[strings.ToLower(title)] {
				if str(lib.pages[id]["parent_id"]) == in.ParentID {
					same = append(same, id)
				}
			}
			sort.Slice(same, func(i, j int) bool {
				if a, b := str(lib.pages[same[i]]["created_at"]), str(lib.pages[same[j]]["created_at"]); a != b {
					return a < b
				}
				return same[i] < same[j]
			})
			if len(same) > 0 {
				out = pageOut{ID: same[0], Title: lib.title(same[0]), Note: "this page was already there: nothing was added"}
				if strings.TrimSpace(in.Markdown) == "" {
					return nil
				}
				out.Note = "added to the page that was already there"
				return s.change(v, access, lib, same[0], "", in.Markdown, "append")
			}
		}
		now := stamp()
		id := newID("page")
		page := map[string]any{"id": id, "kind": "document", "icon": "", "title_content": markdown.StoredRuns(title),
			"created_at": now, "updated_at": now, "created_by": authorName, "updated_by": authorName}
		if in.ParentID != "" {
			page["parent_id"] = in.ParentID
		}
		writes := []vault.Write{{Action: "put", Collection: vault.Pages, ID: id, Value: page}}
		writes = append(writes, bodyWrites(id, lib.read(in.Markdown), 0)...)
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
		return nil, out, errors.New(readOnlyError)
	}
	mode := in.Mode
	if mode == "" {
		mode = "append"
	}
	if mode != "append" && mode != "replace" {
		return nil, out, fmt.Errorf("mode %q is not append or replace", mode)
	}
	if strings.TrimSpace(in.Title) == "" && in.Markdown == "" {
		return nil, out, errors.New("nothing to change: give a title, Markdown, or both")
	}
	err := s.within(in.Vault, func(v *vault.Vault, access *Access) error {
		lib := load(v)
		if _, ok := lib.pages[in.PageID]; !ok {
			return fmt.Errorf("no page %q in this vault", in.PageID)
		}
		if err := s.change(v, access, lib, in.PageID, in.Title, in.Markdown, mode); err != nil {
			return err
		}
		out = pageOut{ID: in.PageID, Title: lib.title(in.PageID)}
		if t := strings.TrimSpace(in.Title); t != "" {
			out.Title = t
		}
		return nil
	})
	return nil, out, err
}

// change gives a page that is there a title, Markdown at its end (append) or
// in place of its body (replace), or both, keeping the version before.
func (s *server) change(v *vault.Vault, access *Access, lib library, pageID, title, body, mode string) error {
	page := lib.pages[pageID]
	if lib.trashed(pageID) {
		return errors.New("the page is in the Trash: restore it first (notes_restore_page)")
	}
	now := stamp()
	var writes []vault.Write
	// The version to keep is the page as it was before this run of changes.
	cp, err := checkpoint(v, pageID, page, lib.stored[pageID], now)
	if err != nil {
		return err
	}
	writes = append(writes, cp...)
	if body != "" {
		existing := lib.blocks[pageID]
		fresh := lib.read(body)
		switch mode {
		case "replace":
			for _, b := range existing {
				if b.HasMentionsOrFiles() {
					return errors.New("this page holds pictures, files, tables, embeds or page links, which a whole new body would lose: change a part of it with notes_edit_page, add to it with mode append, or edit the page in Notes")
				}
				writes = append(writes, vault.Write{Action: "delete", Collection: vault.Blocks, ID: vault.BlockRecordID(pageID, b.ID)})
			}
			writes = append(writes, bodyWrites(pageID, fresh, 0)...)
		default:
			writes = append(writes, bodyWrites(pageID, fresh, lastOrder(existing, ""))...)
		}
	}
	patch := map[string]any{"updated_at": now, "updated_by": authorName}
	if t := strings.TrimSpace(title); t != "" {
		patch["title_content"] = markdown.StoredRuns(t)
	}
	writes = append(writes, vault.Write{Action: "patch", Collection: vault.Pages, ID: pageID, Patch: patch})
	changes, err := apply(v, writes)
	if err != nil {
		return err
	}
	access.Changed(vault.Changes{Changes: changes})
	return nil
}

// lastOrder is the order of the last paragraph under parent ("": at the top).
func lastOrder(blocks []markdown.Block, parent string) float64 {
	var last float64
	for _, b := range blocks {
		if b.ParentID == parent && b.Order > last {
			last = b.Order
		}
	}
	return last
}

func (s *server) move(_ context.Context, _ *mcp.CallToolRequest, in moveIn) (*mcp.CallToolResult, pageOut, error) {
	var out pageOut
	if s.host.ReadOnly() {
		return nil, out, errors.New(readOnlyError)
	}
	err := s.within(in.Vault, func(v *vault.Vault, access *Access) error {
		lib := load(v)
		page, ok := lib.pages[in.PageID]
		if !ok {
			return fmt.Errorf("no page %q in this vault", in.PageID)
		}
		if lib.trashed(in.PageID) {
			return errors.New("the page is in the Trash: restore it first (notes_restore_page)")
		}
		out = pageOut{ID: in.PageID, Title: lib.title(in.PageID)}
		if in.ParentID != "" {
			target, ok := lib.pages[in.ParentID]
			switch {
			case !ok || lib.trashed(in.ParentID):
				return fmt.Errorf("no page %q to put it under in this vault", in.ParentID)
			case str(target["kind"]) == "database":
				return errors.New("a page is not moved into a database here: its rows are made in Notes")
			}
			// Never under itself or a page it holds, which would cut the branch off.
			seen := map[string]bool{}
			for id := in.ParentID; id != "" && !seen[id]; id = str(lib.pages[id]["parent_id"]) {
				if id == in.PageID {
					return errors.New("a page is not moved under itself or a page it holds")
				}
				seen[id] = true
			}
		}
		if str(page["parent_id"]) == in.ParentID {
			out.Note = "already there"
			return nil
		}
		var parent any
		if in.ParentID != "" {
			parent = in.ParentID
		}
		changes, err := apply(v, []vault.Write{{Action: "patch", Collection: vault.Pages, ID: in.PageID, Patch: map[string]any{"parent_id": parent}}})
		if err != nil {
			return err
		}
		access.Changed(vault.Changes{Changes: changes})
		out.Note = "moved to the top level"
		if in.ParentID != "" {
			out.Note = "moved under " + lib.shown(in.ParentID)
		}
		return nil
	})
	return nil, out, err
}

func (s *server) restore(_ context.Context, _ *mcp.CallToolRequest, in trashIn) (*mcp.CallToolResult, pageOut, error) {
	var out pageOut
	if s.host.ReadOnly() {
		return nil, out, errors.New(readOnlyError)
	}
	err := s.within(in.Vault, func(v *vault.Vault, access *Access) error {
		lib := load(v)
		page, ok := lib.pages[in.PageID]
		if !ok {
			return fmt.Errorf("no page %q in this vault", in.PageID)
		}
		out = pageOut{ID: in.PageID, Title: lib.title(in.PageID)}
		parent := str(page["parent_id"])
		if !lib.archived(in.PageID) {
			out.Note = "not in the Trash"
			if lib.trashed(in.PageID) {
				out.Note = "it is in the Trash with a page above it: restore that page"
			}
			return nil
		}
		patch := map[string]any{"archived": nil, "archived_at": nil}
		out.Note = "restored"
		switch {
		case parent != "" && lib.pages[parent] == nil:
			// A restored page whose parent is gone comes back at the top level.
			patch["parent_id"] = nil
			out.Note = "restored, at the top level: the page it was under is gone"
		case parent != "" && lib.trashed(parent):
			out.Note = "restored, but a page above it is still in the Trash: restore that one too, or move this page"
		}
		changes, err := apply(v, []vault.Write{{Action: "patch", Collection: vault.Pages, ID: in.PageID, Patch: patch}})
		if err != nil {
			return err
		}
		access.Changed(vault.Changes{Changes: changes})
		return nil
	})
	return nil, out, err
}

func (s *server) trash(_ context.Context, _ *mcp.CallToolRequest, in trashIn) (*mcp.CallToolResult, pageOut, error) {
	var out pageOut
	if s.host.ReadOnly() {
		return nil, out, errors.New(readOnlyError)
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
func checkpoint(v *vault.Vault, pageID string, page map[string]any, stored map[string]map[string]any, now string) ([]vault.Write, error) {
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
	for id, record := range stored {
		tree[id] = webBlock(id, record)
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

// webBlock is a stored paragraph as a version of its page keeps it, the way the
// app reads one: everything it holds (its colour, its fold, its alignment, a
// picture's width), without the page it is of, and its words, a table's too,
// in the form the app keeps in memory.
func webBlock(id string, record map[string]any) map[string]any {
	block := make(map[string]any, len(record))
	for key, value := range record {
		if key != "page" {
			block[key] = value
		}
	}
	block["id"] = id
	block["content"] = markdown.WebRuns(record["content"])
	if table, ok := record["table"].(map[string]any); ok {
		block["table"] = webTable(table)
	}
	return block
}

// webTable is a stored table with the words of its cells as the app keeps them.
func webTable(table map[string]any) map[string]any {
	rows, ok := table["rows"].(map[string]any)
	if !ok {
		return table
	}
	out := make(map[string]any, len(table))
	for key, value := range table {
		out[key] = value
	}
	webRows := make(map[string]any, len(rows))
	for id, value := range rows {
		row, _ := value.(map[string]any)
		cells, ok := row["cells"].(map[string]any)
		if !ok {
			webRows[id] = value
			continue
		}
		webRow := make(map[string]any, len(row))
		for key, value := range row {
			webRow[key] = value
		}
		webCells := make(map[string]any, len(cells))
		for column, cell := range cells {
			webCells[column] = markdown.WebRuns(cell)
		}
		webRow["cells"] = webCells
		webRows[id] = webRow
	}
	out["rows"] = webRows
	return out
}

// ---- small helpers ----------------------------------------------------------

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

// snippetLength is how many characters of a paragraph a search shows, and
// snippetLead how many of them come before the words found.
const (
	snippetLength = 160
	snippetLead   = 40
)

// snippet is the part of a paragraph around the words found (query, in lower
// case): whole characters, with an ellipsis where the paragraph goes on.
func snippet(text, query string) string {
	chars := []rune(strings.Join(strings.Fields(text), " "))
	if len(chars) <= snippetLength {
		return string(chars)
	}
	start := 0
	if at := runeIndex(chars, []rune(query)); at > snippetLead {
		start = at - snippetLead
	}
	if start+snippetLength > len(chars) {
		start = len(chars) - snippetLength
	}
	out := string(chars[start : start+snippetLength])
	if start > 0 {
		out = "…" + out
	}
	if start+snippetLength < len(chars) {
		out += "…"
	}
	return out
}

// runeIndex is where find (in lower case) first stands in chars, whatever the
// case of chars, or -1.
func runeIndex(chars, find []rune) int {
	if len(find) == 0 {
		return -1
	}
	for i := 0; i+len(find) <= len(chars); i++ {
		match := true
		for k, want := range find {
			if unicode.ToLower(chars[i+k]) != want {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// NewKey makes a key for the server: "sk-" and 40 hex digits.
func NewKey() string {
	raw := make([]byte, 20)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Sprintf("sk-%040x", time.Now().UnixNano())
	}
	return "sk-" + hex.EncodeToString(raw)
}
