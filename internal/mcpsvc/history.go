package mcpsvc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"notes/internal/markdown"
	"notes/internal/vault"
)

func addHistoryTools(srv *mcp.Server, s *server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "notes_page_history",
		Title:       "Page History",
		Description: "List the earlier versions of a page, newest first: the ones the app kept as the page was written, and the one kept before each change an assistant made. Read one with notes_read_version.",
		Annotations: readOnlyTool("Page History"),
	}, s.pageHistory)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "notes_read_version",
		Title:       "Read Page Version",
		Description: "Read an earlier version of a page, by a version_id from notes_page_history: a few lines about it, a line of ---, then its body as Markdown, as notes_fetch gives a page.",
		Annotations: readOnlyTool("Read Page Version"),
	}, s.readVersion)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "notes_restore_version",
		Title:       "Restore Page Version",
		Description: "Bring a page's title and body back to an earlier version. The page as it is now is kept in its history first, so a restore can itself be taken back. The page's place, icon and properties stay as they are.",
		Annotations: mutatingTool("Restore Page Version", true, false),
	}, s.restoreVersion)
}

type historyIn struct {
	Vault  string `json:"vault,omitempty" jsonschema:"the vault's name or folder; omit for the default"`
	PageID string `json:"page_id" jsonschema:"the page's id"`
	Limit  int    `json:"limit,omitempty" jsonschema:"at most this many versions (default 20, up to 60)"`
}

type versionIn struct {
	Vault     string `json:"vault,omitempty" jsonschema:"the vault's name or folder; omit for the default"`
	PageID    string `json:"page_id" jsonschema:"the page's id"`
	VersionID string `json:"version_id" jsonschema:"the version's id, from notes_page_history"`
}

type versionHit struct {
	ID      string `json:"id"`
	Saved   string `json:"saved"`
	By      string `json:"by,omitempty"`
	Summary string `json:"summary,omitempty"`
	Title   string `json:"title"`
}

type historyOut struct {
	PageID   string       `json:"page_id"`
	Versions []versionHit `json:"versions"`
}

// version is one checkpoint of a page, put together from its parts.
type version struct {
	ID        string         `json:"id"`
	PageID    string         `json:"page_id"`
	AuthorID  string         `json:"author_id"`
	Author    string         `json:"author_name"`
	Summary   string         `json:"summary"`
	CreatedAt string         `json:"created_at"`
	Snapshot  map[string]any `json:"snapshot"`
}

func (ver version) at() time.Time {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.000Z"} {
		if t, err := time.Parse(layout, ver.CreatedAt); err == nil {
			return t
		}
	}
	return time.Time{}
}

func (ver version) title() string {
	if t := markdown.StoredText(storedRuns(ver.Snapshot["title_content"])); t != "" {
		return t
	}
	t, _ := ver.Snapshot["title"].(string)
	return t
}

func count(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	case int:
		return n
	}
	return -1
}

// versionsOf reads the history of a page: each version whole, newest first. A
// version whose parts are not all there is left out.
func versionsOf(v *vault.Vault, pageID string) []version {
	type gen struct {
		created string
		parts   map[int]string
		of      int
	}
	byVersion := map[string]map[string]*gen{}
	for _, row := range v.VersionsOf(pageID) {
		id, _ := row.Value["version"].(string)
		g, _ := row.Value["generation"].(string)
		payload, _ := row.Value["payload"].(string)
		created, _ := row.Value["created_at"].(string)
		part := count(row.Value["part"])
		if id == "" || part < 0 {
			continue
		}
		if byVersion[id] == nil {
			byVersion[id] = map[string]*gen{}
		}
		if byVersion[id][g] == nil {
			byVersion[id][g] = &gen{parts: map[int]string{}}
		}
		one := byVersion[id][g]
		one.parts[part] = payload
		if part == 0 {
			one.created, one.of = created, count(row.Value["parts"])
		}
	}
	var out []version
	for id, gens := range byVersion {
		var best *gen
		for _, g := range gens {
			if g.of > 0 && len(g.parts) == g.of && (best == nil || g.created > best.created) {
				best = g
			}
		}
		if best == nil {
			continue
		}
		var text strings.Builder
		for i := 0; i < best.of; i++ {
			text.WriteString(best.parts[i])
		}
		var ver version
		if json.Unmarshal([]byte(text.String()), &ver) != nil || ver.ID != id || ver.PageID != pageID || ver.Snapshot == nil {
			continue
		}
		out = append(out, ver)
	}
	sort.Slice(out, func(i, j int) bool {
		if a, b := out[i].at(), out[j].at(); !a.Equal(b) {
			return a.After(b)
		}
		return out[i].ID > out[j].ID
	})
	return out
}

func findVersion(v *vault.Vault, pageID, id string) (version, error) {
	for _, ver := range versionsOf(v, pageID) {
		if ver.ID == id {
			return ver, nil
		}
	}
	return version{}, fmt.Errorf("no version %q of this page: notes_page_history lists them", id)
}

// storedRuns is the rich text of a version, which keeps a mention as a
// character with the mention beside it, as the vault keeps it: an embed.
func storedRuns(value any) []any {
	list, _ := value.([]any)
	out := make([]any, 0, len(list))
	for _, item := range list {
		m, _ := item.(map[string]any)
		if m == nil {
			continue
		}
		if mention, ok := m["mention"]; ok {
			run := map[string]any{"embed": map[string]any{"mention": mention}}
			if marks, ok := m["marks"]; ok {
				run["marks"] = marks
			}
			out = append(out, run)
			continue
		}
		out = append(out, m)
	}
	return out
}

// storedBlock is a paragraph of a version as the vault keeps it: the inverse
// of webBlock.
func storedBlock(id string, block map[string]any) map[string]any {
	record := make(map[string]any, len(block))
	for key, value := range block {
		record[key] = value
	}
	record["id"] = id
	record["content"] = storedRuns(block["content"])
	if table, ok := block["table"].(map[string]any); ok {
		record["table"] = storedTable(table)
	}
	return record
}

func storedTable(table map[string]any) map[string]any {
	rows, ok := table["rows"].(map[string]any)
	if !ok {
		return table
	}
	out := make(map[string]any, len(table))
	for key, value := range table {
		out[key] = value
	}
	fresh := make(map[string]any, len(rows))
	for id, value := range rows {
		row, _ := value.(map[string]any)
		cells, ok := row["cells"].(map[string]any)
		if !ok {
			fresh[id] = value
			continue
		}
		copied := make(map[string]any, len(row))
		for key, value := range row {
			copied[key] = value
		}
		runs := make(map[string]any, len(cells))
		for column, cell := range cells {
			runs[column] = storedRuns(cell)
		}
		copied["cells"] = runs
		fresh[id] = copied
	}
	out["rows"] = fresh
	return out
}

// versionBlocks are the paragraphs a version holds, as the vault keeps them.
func versionBlocks(ver version) (map[string]map[string]any, error) {
	tree, _ := ver.Snapshot["block_tree"].(map[string]any)
	byID, ok := tree["by_id"].(map[string]any)
	if !ok {
		return nil, errors.New("this version holds no body")
	}
	out := make(map[string]map[string]any, len(byID))
	for id, value := range byID {
		if block, ok := value.(map[string]any); ok {
			out[id] = storedBlock(id, block)
		}
	}
	return out, nil
}

func (s *server) pageHistory(_ context.Context, _ *mcp.CallToolRequest, in historyIn) (*mcp.CallToolResult, historyOut, error) {
	out := historyOut{PageID: in.PageID, Versions: []versionHit{}}
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	limit = min(limit, vault.MaxVersions)
	err := s.within(in.Vault, func(v *vault.Vault, _ *Access) error {
		if _, ok := load(v).pages[in.PageID]; !ok {
			return fmt.Errorf("no page %q in this vault", in.PageID)
		}
		for _, ver := range versionsOf(v, in.PageID) {
			if len(out.Versions) == limit {
				break
			}
			out.Versions = append(out.Versions, versionHit{ID: ver.ID, Saved: ver.CreatedAt, By: ver.Author, Summary: ver.Summary, Title: ver.title()})
		}
		return nil
	})
	return nil, out, err
}

func (s *server) readVersion(_ context.Context, _ *mcp.CallToolRequest, in versionIn) (*mcp.CallToolResult, any, error) {
	var text string
	err := s.within(in.Vault, func(v *vault.Vault, _ *Access) error {
		lib := load(v)
		if _, ok := lib.pages[in.PageID]; !ok {
			return fmt.Errorf("no page %q in this vault", in.PageID)
		}
		ver, err := findVersion(v, in.PageID, in.VersionID)
		if err != nil {
			return err
		}
		records, err := versionBlocks(ver)
		if err != nil {
			return err
		}
		var blocks []markdown.Block
		for id, record := range records {
			blocks = append(blocks, markdown.FromRecord(id, record))
		}
		var b strings.Builder
		fmt.Fprintf(&b, "Title: %s\nPage: %s\nVersion: %s\nSaved: %s\n", ver.title(), in.PageID, ver.ID, ver.CreatedAt)
		if ver.Author != "" {
			fmt.Fprintf(&b, "By: %s\n", ver.Author)
		}
		if ver.Summary != "" {
			fmt.Fprintf(&b, "Summary: %s\n", ver.Summary)
		}
		b.WriteString("\n" + bodyMark + "\n" + markdown.ToMarkdown(blocks, lib.name))
		text = b.String()
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil, nil
}

func (s *server) restoreVersion(_ context.Context, _ *mcp.CallToolRequest, in versionIn) (*mcp.CallToolResult, pageOut, error) {
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
		ver, err := findVersion(v, in.PageID, in.VersionID)
		if err != nil {
			return err
		}
		records, err := versionBlocks(ver)
		if err != nil {
			return err
		}
		title := storedRuns(ver.Snapshot["title_content"])
		if markdown.StoredText(title) == "" {
			title = markdown.StoredRuns(ver.title())
		}
		now := stamp()
		// The page as it is now, kept apart from any version before: a restore
		// can be taken back.
		writes, err := keepVersion(in.PageID, page, lib.stored[in.PageID], now, "Before an assistant restored an earlier version", true)
		if err != nil {
			return err
		}
		for id := range lib.stored[in.PageID] {
			writes = append(writes, vault.Write{Action: "delete", Collection: vault.Blocks, ID: vault.BlockRecordID(in.PageID, id)})
		}
		ids := make([]string, 0, len(records))
		for id := range records {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			writes = append(writes, vault.Write{Action: "put", Collection: vault.Blocks, ID: vault.BlockRecordID(in.PageID, id), Value: records[id]})
		}
		writes = append(writes, vault.Write{Action: "patch", Collection: vault.Pages, ID: in.PageID,
			Patch: map[string]any{"title_content": title, "updated_at": now, "updated_by": authorName}})
		changes, err := apply(v, writes)
		if err != nil {
			return err
		}
		access.Changed(vault.Changes{Changes: changes})
		out = pageOut{ID: in.PageID, Title: markdown.StoredText(title), Note: "restored the version saved " + ver.CreatedAt + "; the page as it was is kept in its history"}
		return nil
	})
	return nil, out, err
}
