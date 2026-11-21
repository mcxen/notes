package frontend

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"

	"notes/internal/mcpsvc"
	"notes/internal/settings"
	"notes/internal/vault"
)

// The MCP server runs inside the app, so that a vault a window has open is
// changed through that window's own copy: its changes reach the window at once.

// mcpRunner starts and stops the server as the settings say.
type mcpRunner struct {
	mu   sync.Mutex
	srv  *http.Server
	addr string
	err  string
}

// status is what the settings page says about the server.
func (r *mcpRunner) status(st settings.Settings) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch {
	case r.srv != nil:
		return tr("Listening on port %s.", r.addr)
	case r.err != "":
		return tr("Not started: %s", r.err)
	case !st.MCP.Enabled:
		return tr("Off.")
	}
	return tr("Starting…")
}

// apply makes the server match the settings: stopped when off, else listening.
func (r *mcpRunner) apply(st settings.Settings, host mcpsvc.Host) {
	r.stop()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.err = ""
	if !st.MCP.Enabled {
		return
	}
	if st.MCP.Key == "" {
		r.err = tr("there is no key yet")
		return
	}
	port := fmt.Sprint(st.MCP.PortOrDefault())
	address := ":" + port
	if st.MCP.LocalOnly {
		address = "127.0.0.1:" + port
	}
	ln, err := net.Listen("tcp", address)
	if err != nil {
		r.err = err.Error()
		return
	}
	srv := &http.Server{Handler: mcpsvc.NewHandler(host, st.MCP.Key, mygo.App.Version()), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("notes: mcp: %v", err)
		}
	}()
	r.srv = srv
	r.addr = port
}

func (r *mcpRunner) stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.srv != nil {
		_ = r.srv.Close()
		r.srv = nil
	}
}

// mcpHost gives the server the vaults the settings allow, and their windows.
type mcpHost struct {
	n        *Notebooks
	settings string
}

func (h mcpHost) Vaults() []mcpsvc.VaultRef {
	st, err := settings.Load(h.settings)
	if err != nil {
		return nil
	}
	var refs []mcpsvc.VaultRef
	for _, folder := range st.MCP.Vaults {
		if isDirectory(folder) {
			refs = append(refs, mcpsvc.VaultRef{Name: folderName(folder), Folder: folder})
		}
	}
	return refs
}

func (h mcpHost) ReadOnly() bool {
	st, err := settings.Load(h.settings)
	return err == nil && st.MCP.ReadOnly
}

func (h mcpHost) Open(folder string) (*mcpsvc.Access, error) {
	if s := h.n.sessionAt(folder); s != nil {
		return &mcpsvc.Access{
			Vault: s.vault,
			Changed: func(c vault.Changes) {
				if s.window != nil {
					_ = VaultChanged.Emit(s.window, c)
				}
				if len(c.Changes) > 0 && s.remote != nil {
					s.remote.Schedule(3 * time.Second)
				}
			},
			Close: func() {},
		}, nil
	}
	v, err := vault.Open(folder)
	if err != nil {
		return nil, err
	}
	return &mcpsvc.Access{Vault: v, Changed: func(vault.Changes) {}, Close: func() { _ = v.Close() }}, nil
}

// sessionAt is the session of the vault in folder, when a window has it open.
func (n *Notebooks) sessionAt(folder string) *session {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, s := range n.byWindow {
		if s.vault != nil && filepath.Clean(s.vault.Info().Path) == filepath.Clean(folder) {
			return s
		}
	}
	return nil
}

// applyMCP makes the server match the settings as they are now.
func (a *appActions) applyMCP() {
	st, err := settings.Load(a.settings)
	if err != nil {
		log.Printf("notes: mcp: %v", err)
		return
	}
	a.mcp.apply(st, mcpHost{n: a.notebooks, settings: a.settings})
}

// RunMCPStdio is the --mcp-stdio mode: a client that speaks only stdio talks to
// the MCP server of the running app. No window opens and nothing is changed here.
func RunMCPStdio(in io.Reader, out, errw io.Writer) error {
	// The settings are in the app's own folder, which is named by the app.
	mygo.App.SetName(appName)
	st, err := settings.Load(settingsPath())
	if err != nil {
		return err
	}
	if !st.MCP.Enabled {
		fmt.Fprintln(errw, "notes mcp: the MCP server is off; turn it on in Notes › Settings › Integrations")
	}
	endpoint := fmt.Sprintf("http://127.0.0.1:%d/mcp", st.MCP.PortOrDefault())
	return mcpsvc.RunStdio(in, out, errw, endpoint, st.MCP.Key)
}

// connectURL is the address a client should use: the public address when one
// is set, else this computer's address on the network, else this computer.
func connectURL(st settings.Settings) string {
	if u := strings.TrimSpace(st.MCP.PublicURL); u != "" {
		if !strings.HasSuffix(u, "/mcp") {
			u = strings.TrimRight(u, "/") + "/mcp"
		}
		return u
	}
	host := "127.0.0.1"
	if !st.MCP.LocalOnly {
		if ip := lanAddress(); ip != "" {
			host = ip
		}
	}
	return fmt.Sprintf("http://%s:%d/mcp", host, st.MCP.PortOrDefault())
}

// lanAddress is this computer's first IPv4 address on a network, or "".
func lanAddress() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.To4() != nil && !ipnet.IP.IsLoopback() && !ipnet.IP.IsLinkLocalUnicast() {
			return ipnet.IP.String()
		}
	}
	return ""
}

// vaultNames lists the vaults the assistants may use, default first.
func vaultNames(st settings.Settings) []string {
	var names []string
	for _, folder := range st.MCP.Vaults {
		if isDirectory(folder) {
			names = append(names, folderName(folder))
		}
	}
	return names
}

// mcpPrompt is the whole prompt for an AI assistant: where the server is, the
// key, the vaults and the tools. It is copied from the settings page.
func mcpPrompt(st settings.Settings) string {
	names := vaultNames(st)
	vaults := "none yet: choose the vaults in Notes › Settings › Integrations first"
	if len(names) > 0 {
		vaults = strings.Join(names, ", ") + fmt.Sprintf(" (default: %s)", names[0])
	}
	var b strings.Builder
	fmt.Fprintf(&b, "You can read and edit my notes in Notes, a local notes app, through its MCP server.\n\n")
	fmt.Fprintf(&b, "Server URL: %s\n", connectURL(st))
	fmt.Fprintf(&b, "Transport: Streamable HTTP (MCP protocol %s)\n", mcpsvc.DefaultProtocol)
	fmt.Fprintf(&b, "Authentication: send this HTTP header with every request:\n  Authorization: Bearer %s\n\n", st.MCP.Key)
	fmt.Fprintf(&b, "Vaults you may use: %s.\nPass the \"vault\" argument to pick another one by name; omit it for the default.\n\n", vaults)
	b.WriteString(`Tools:
- notes_list_vaults: list the vaults you may use.
- notes_search(query, parent_id?, kind?, updated_after?, updated_before?, limit?, vault?): find pages that have every word of the query, in their title or text; the best matches first. Filter by parent_id (sub-tree), kind ("document", "database", "row"), or date bounds (YYYY-MM-DD / RFC3339), or pass vault "*" to search every allowed vault.
- notes_list_pages(parent_id?, vault?): list the sub-pages of a page, or the top level (with kind and child_count, in row_order under a database and roots order at the top).
- notes_page_tree(parent_id?, depth?, include_rows?, limit?, vault?): return a flat pre-order outline of the page hierarchy in one call.
- notes_recent_pages(limit?, vault?): list the pages changed most recently.
- notes_list_trash(limit?, vault?): list pages currently in the Trash so they can be inspected or restored.
- notes_list_todos(status?, parent_id?, limit?, vault?): aggregate to-do items ("open", "done", or "all") across pages.
- notes_fetch(page_id, vault?): read a page. The answer is text: a few lines about the page (title, id, parent, sub-pages, the pages it links to and is linked from, attachments with their paths), a line of ---, then the body as Markdown.
- notes_read_attachment(page_id, path, vault?): open a picture or file on a page (or in a database row's Image property): a picture comes back as an image you can see, a text file as text, any other file as bytes.
- notes_add_attachment(page_id, name, data, type?, vault?): put a picture or file (base64, up to 2.5 MB) on a page, after what it holds.
- notes_create_page(title, markdown?, parent_id?, if_exists?, properties?, vault?): make a page; under a database it is a row, and properties ({"Status": "Doing"}, by the property's name) sets its values. With if_exists "append", a page that already has this title under the same parent gets the Markdown added to it instead, so the call can be repeated (a daily log, say).
- notes_edit_page(page_id, old_text?, new_text?, nth?, edits?, vault?): change parts of a page. Pass old_text (text of one paragraph as notes_fetch shows it) and new_text (with optional nth), or pass edits ([{old_text, new_text, nth?}]) to apply multiple changes atomically in one call. Several lines in new_text add paragraphs after it; an empty new_text removes the paragraph. This is the way to change a page that holds pictures, tables or links.
- notes_update_page(page_id, title?, markdown?, mode?, properties?, vault?): change a page's title, or its body as a whole, or the values of a database row (properties, by the property's name; an empty string clears one; notes_fetch on the database lists the properties and their options). mode is "append" (the default, adds to the end) or "replace" (swaps the whole body).
- notes_move_page(page_id, parent_id?, vault?): move a page under another (including into a database to make it a row), or to the top level.
- notes_trash_page(page_id, vault?): move a page to the Trash.
- notes_restore_page(page_id, vault?): take a page back out of the Trash.
- notes_query_database(database_id, view?, query?, filters?, sort_by?, sort_dir?, group_by?, limit?, offset?, vault?): filter, sort, paginate, and count groups over a database's rows (optionally reusing a saved view).
- notes_create_database(title, parent_id?, icon?, title_name?, view_kind?, properties?, vault?): create a database with typed columns (text, select, status, date, person, image) and an initial view (table, board, list, gallery, calendar, chart).
- notes_update_database(database_id, title?, title_name?, add_properties?, rename_properties?, add_options?, remove_properties?, vault?): update a database's title or schema (add/rename/remove columns, add select/status options).
- notes_page_history(page_id, limit?, vault?): list earlier versions of a page, newest first.
- notes_read_version(page_id, version_id, vault?): read an earlier version of a page as Markdown.
- notes_restore_version(page_id, version_id, vault?): restore a page's title and body to an earlier version.
Every change keeps the version before it in the page's history.

Markdown: "# ", "## " and "### " headings; "- " bullets; "1. " numbered items; "- [ ] " and "- [x] " to-dos; "> " quotes; fenced code blocks with a language after the opening fence; "$$ ... $$" display equations; "| A | B |" pipe tables; two spaces of indentation for a nested item; **bold**, *italic*, ~~strike~~, ` + "`code`" + ` and [text](url). [[A page's title]] (or [[its id]]) is a link to that page.
replace refuses a page with pictures, files, tables, embeds or page links: use notes_edit_page or append for those.
`)
	if st.MCP.ReadOnly {
		b.WriteString("\nThis server is read-only: only the read tools work.\n")
	}
	return b.String()
}

// claudeCodeCommand adds the server to Claude Code.
func claudeCodeCommand(st settings.Settings) string {
	return fmt.Sprintf("claude mcp add --transport http notes %s --header \"Authorization: Bearer %s\"", connectURL(st), st.MCP.Key)
}

// stdioConfig is the entry a client that runs a stdio server takes.
func stdioConfig() string {
	exe, err := os.Executable()
	if err != nil {
		exe = "notes"
	}
	return fmt.Sprintf("{\n  \"mcpServers\": {\n    \"notes\": {\n      \"command\": %q,\n      \"args\": [\"--mcp-stdio\"]\n    }\n  }\n}", exe)
}

// maskedKey shows the start and the end of a key, so it can be told apart.
func maskedKey(key string) string {
	if len(key) <= 10 {
		return key
	}
	return key[:4] + "…" + key[len(key)-6:]
}
