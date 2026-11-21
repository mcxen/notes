package mcpsvc

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"notes/internal/markdown"
	"notes/internal/vault"
)

func addBrowseTools(srv *mcp.Server, s *server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "notes_list_trash",
		Title:       "List Trash",
		Description: "List pages currently in the Trash (newest archived first), so deleted pages can be inspected or restored with notes_restore_page.",
		Annotations: readOnlyTool("List Trash"),
	}, s.listTrash)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "notes_page_tree",
		Title:       "Page Tree",
		Description: "Return a flat pre-order outline of the page hierarchy (up to depth 10), including kind and child_count, so you can explore a vault in one call.",
		Annotations: readOnlyTool("Page Tree"),
	}, s.pageTree)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "notes_list_todos",
		Title:       "List To-Dos",
		Description: "Aggregate to-do items across pages (optionally scoped under parent_id and filtered by status: open, done, or all).",
		Annotations: readOnlyTool("List To-Dos"),
	}, s.listTodos)
}

type listTrashIn struct {
	Vault string `json:"vault,omitempty" jsonschema:"the vault's name or folder; omit for the default"`
	Limit int    `json:"limit,omitempty" jsonschema:"at most this many trashed pages (default 50, up to 200)"`
}

type trashedPage struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Kind        string `json:"kind"`
	Icon        string `json:"icon,omitempty"`
	ParentID    string `json:"parent_id,omitempty"`
	ParentTitle string `json:"parent_title,omitempty"`
	ArchivedAt  string `json:"archived_at,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
	ChildCount  int    `json:"child_count,omitempty"`
}

type listTrashOut struct {
	Pages []trashedPage `json:"pages"`
	Total int           `json:"total"`
}

func (s *server) listTrash(_ context.Context, _ *mcp.CallToolRequest, in listTrashIn) (*mcp.CallToolResult, listTrashOut, error) {
	out := listTrashOut{Pages: []trashedPage{}}
	limit := in.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	err := s.within(in.Vault, func(v *vault.Vault, _ *Access) error {
		lib := load(v)
		direct := map[string]int{}
		for _, page := range lib.pages {
			if p := str(page["parent_id"]); p != "" {
				direct[p]++
			}
		}
		var all []trashedPage
		for id, page := range lib.pages {
			if !lib.archived(id) {
				continue
			}
			parentID := str(page["parent_id"])
			parentTitle := ""
			if parentID != "" && lib.pages[parentID] != nil {
				parentTitle = lib.shown(parentID)
			}
			all = append(all, trashedPage{
				ID:          id,
				Title:       lib.title(id),
				Kind:        lib.kindOf(id),
				Icon:        str(page["icon"]),
				ParentID:    parentID,
				ParentTitle: parentTitle,
				ArchivedAt:  str(page["archived_at"]),
				UpdatedAt:   str(page["updated_at"]),
				ChildCount:  direct[id],
			})
		}
		sort.Slice(all, func(i, j int) bool {
			ki := all[i].ArchivedAt
			if ki == "" {
				ki = all[i].UpdatedAt
			}
			kj := all[j].ArchivedAt
			if kj == "" {
				kj = all[j].UpdatedAt
			}
			if ki != kj {
				return ki > kj
			}
			return all[i].ID < all[j].ID
		})
		out.Total = len(all)
		if len(all) > limit {
			all = all[:limit]
		}
		out.Pages = all
		return nil
	})
	return nil, out, err
}

type pageTreeIn struct {
	Vault       string `json:"vault,omitempty" jsonschema:"the vault's name or folder; omit for the default"`
	ParentID    string `json:"parent_id,omitempty" jsonschema:"start under this page; omit for the top level"`
	Depth       int    `json:"depth,omitempty" jsonschema:"how many levels to expand (default 3, up to 10)"`
	IncludeRows bool   `json:"include_rows,omitempty" jsonschema:"also expand rows under databases encountered in the tree (default false)"`
	Limit       int    `json:"limit,omitempty" jsonschema:"at most this many nodes in total (default 200, up to 500)"`
}

type treeNode struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Kind       string `json:"kind"`
	Icon       string `json:"icon,omitempty"`
	ParentID   string `json:"parent_id,omitempty"`
	Depth      int    `json:"depth"`
	ChildCount int    `json:"child_count,omitempty"`
	Truncated  bool   `json:"truncated,omitempty"`
}

type pageTreeOut struct {
	Nodes     []treeNode `json:"nodes"`
	Truncated bool       `json:"truncated,omitempty"`
}

func (s *server) pageTree(_ context.Context, _ *mcp.CallToolRequest, in pageTreeIn) (*mcp.CallToolResult, pageTreeOut, error) {
	out := pageTreeOut{Nodes: []treeNode{}}
	depth := in.Depth
	if depth <= 0 {
		depth = 3
	}
	if depth > 10 {
		depth = 10
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 200
	}
	if limit > 500 {
		limit = 500
	}
	err := s.within(in.Vault, func(v *vault.Vault, _ *Access) error {
		lib := load(v)
		if in.ParentID != "" && (lib.pages[in.ParentID] == nil || lib.trashed(in.ParentID)) {
			return fmt.Errorf("no page %q in this vault", in.ParentID)
		}
		seen := map[string]bool{}
		if in.ParentID != "" {
			seen[in.ParentID] = true
		}
		var walk func(parentID string, level int) bool
		walk = func(parentID string, level int) bool {
			kids := lib.children(parentID)
			complete := true
			for _, id := range kids {
				if seen[id] {
					continue
				}
				if len(out.Nodes) >= limit {
					out.Truncated = true
					return false
				}
				seen[id] = true
				childCount := len(lib.children(id))
				canExpand := childCount > 0 && level+1 < depth && (!lib.isDatabase(id) || in.IncludeRows)
				node := treeNode{
					ID:         id,
					Title:      lib.title(id),
					Kind:       lib.kindOf(id),
					Icon:       str(lib.pages[id]["icon"]),
					ParentID:   parentID,
					Depth:      level,
					ChildCount: childCount,
					Truncated:  childCount > 0 && !canExpand,
				}
				idx := len(out.Nodes)
				out.Nodes = append(out.Nodes, node)
				if canExpand {
					if !walk(id, level+1) {
						out.Nodes[idx].Truncated = true
						complete = false
					}
				}
			}
			return complete
		}
		walk(in.ParentID, 0)
		return nil
	})
	return nil, out, err
}

type listTodosIn struct {
	Vault    string `json:"vault,omitempty" jsonschema:"the vault's name or folder; omit for the default"`
	Status   string `json:"status,omitempty" jsonschema:"which to-dos to list: open (default), done, or all"`
	ParentID string `json:"parent_id,omitempty" jsonschema:"limit to this page and its sub-pages"`
	Limit    int    `json:"limit,omitempty" jsonschema:"at most this many to-dos (default 50, up to 200)"`
}

type todoItem struct {
	PageID    string `json:"page_id"`
	PageTitle string `json:"page_title"`
	BlockID   string `json:"block_id"`
	Text      string `json:"text"`
	Checked   bool   `json:"checked"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

type listTodosOut struct {
	Todos []todoItem `json:"todos"`
	Total int        `json:"total"`
}

func (s *server) listTodos(_ context.Context, _ *mcp.CallToolRequest, in listTodosIn) (*mcp.CallToolResult, listTodosOut, error) {
	out := listTodosOut{Todos: []todoItem{}}
	status := strings.TrimSpace(strings.ToLower(in.Status))
	if status == "" {
		status = "open"
	}
	if status != "open" && status != "done" && status != "all" {
		return nil, out, fmt.Errorf("status %q must be open, done, or all", in.Status)
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	err := s.within(in.Vault, func(v *vault.Vault, _ *Access) error {
		lib := load(v)
		var scope map[string]bool
		if in.ParentID != "" {
			if lib.pages[in.ParentID] == nil || lib.trashed(in.ParentID) {
				return fmt.Errorf("no page %q in this vault", in.ParentID)
			}
			scope = lib.subtree(in.ParentID)
		}
		var pageIDs []string
		for id := range lib.pages {
			if lib.trashed(id) {
				continue
			}
			if scope != nil && !scope[id] {
				continue
			}
			pageIDs = append(pageIDs, id)
		}
		sort.Slice(pageIDs, func(i, j int) bool {
			if a, b := str(lib.pages[pageIDs[i]]["updated_at"]), str(lib.pages[pageIDs[j]]["updated_at"]); a != b {
				return a > b
			}
			return pageIDs[i] < pageIDs[j]
		})
		for _, id := range pageIDs {
			title := lib.shown(id)
			updated := str(lib.pages[id]["updated_at"])
			for _, entry := range markdown.Outline(lib.blocks[id], lib.name) {
				if entry.Block.Kind != "todo" {
					continue
				}
				if status == "open" && entry.Block.Checked {
					continue
				}
				if status == "done" && !entry.Block.Checked {
					continue
				}
				out.Total++
				if len(out.Todos) < limit {
					text := strings.Join(strings.Fields(markdown.Words(entry.Block, lib.name)), " ")
					out.Todos = append(out.Todos, todoItem{
						PageID:    id,
						PageTitle: title,
						BlockID:   entry.Block.ID,
						Text:      text,
						Checked:   entry.Block.Checked,
						UpdatedAt: updated,
					})
				}
			}
		}
		return nil
	})
	return nil, out, err
}
