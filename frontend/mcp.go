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
		return &mcpsvc.Access{Vault: s.vault, Changed: func(c vault.Changes) { _ = VaultChanged.Emit(s.window, c) }, Close: func() {}}, nil
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
- notes_search(query, limit?, vault?): find pages that have every word of the query, in their title or text; the best matches first.
- notes_list_pages(parent_id?, vault?): list the sub-pages of a page, or the top level.
- notes_recent_pages(limit?, vault?): list the pages changed most recently.
- notes_fetch(page_id, vault?): read a page. The answer is text: a few lines about the page (title, id, parent, sub-pages, the pages it links to and is linked from, attachments with their paths), a line of ---, then the body as Markdown.
- notes_read_attachment(page_id, path, vault?): open a picture or file of a page: a picture comes back as an image you can see, a text file as text, any other file as bytes.
- notes_add_attachment(page_id, name, data, type?, vault?): put a picture or file (base64, up to 2.5 MB) on a page, after what it holds.
- notes_create_page(title, markdown?, parent_id?, if_exists?, vault?): make a page. With if_exists "append", a page that already has this title under the same parent gets the Markdown added to it instead, so the call can be repeated (a daily log, say).
- notes_edit_page(page_id, old_text, new_text, nth?, vault?): change a part of a page. old_text is text of one paragraph (a line, a code block or a quote) as notes_fetch shows it; it becomes new_text. Several lines in new_text add paragraphs after it; an empty new_text removes the paragraph. Pass nth when old_text is on the page several times. This is the way to change a page that holds pictures, tables or links.
- notes_update_page(page_id, title?, markdown?, mode?, vault?): change a page's title, or its body as a whole. mode is "append" (the default, adds to the end) or "replace" (swaps the whole body).
- notes_move_page(page_id, parent_id?, vault?): move a page under another, or to the top level.
- notes_trash_page(page_id, vault?): move a page to the Trash.
- notes_restore_page(page_id, vault?): take a page back out of the Trash.
Every change keeps the version before it in the page's history.

Markdown: "# ", "## " and "### " headings; "- " bullets; "1. " numbered items; "- [ ] " and "- [x] " to-dos; "> " quotes; fenced code blocks with a language after the opening fence; two spaces of indentation for a nested item; **bold**, *italic*, ~~strike~~, ` + "`code`" + ` and [text](url). [[A page's title]] (or [[its id]]) is a link to that page.
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
