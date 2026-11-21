package mcpsvc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"notes/internal/markdown"
	"notes/internal/vault"
)

const maxStatusOptions = 100

var (
	validPropertyTypes = map[string]bool{"text": true, "select": true, "status": true, "date": true, "person": true, "image": true}
	validViewKinds     = map[string]bool{"table": true, "board": true, "list": true, "gallery": true, "calendar": true, "chart": true}
	validOptionColors  = map[string]bool{
		"default": true, "gray": true, "brown": true, "orange": true, "yellow": true,
		"green": true, "blue": true, "purple": true, "pink": true, "red": true,
	}
	defaultGroupColor = map[string]string{"todo": "default", "progress": "blue", "complete": "green"}
)

func addDatabaseTools(srv *mcp.Server, s *server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "notes_query_database",
		Title:       "Query Database",
		Description: "Filter, sort, paginate, and group rows of a database. Can apply a saved view (by name or id), full-text query, property filters, custom sort, and group counts.",
		Annotations: readOnlyTool("Query Database"),
	}, s.queryDatabase)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "notes_create_database",
		Title:       "Create Database",
		Description: "Create a new database page with typed properties (text, select, status, date, person, image) and an initial view (table, board, list, gallery, calendar, or chart).",
		Annotations: additiveTool("Create Database", false),
	}, s.createDatabase)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "notes_update_database",
		Title:       "Update Database",
		Description: "Update a database's title, title_name, or schema: add properties, rename properties, add options to select/status properties, or remove properties (also cleaning up views).",
		Annotations: mutatingTool("Update Database", true, false),
	}, s.updateDatabase)
}

// ---- notes_query_database ---------------------------------------------------

type dbFilter struct {
	Property string `json:"property" jsonschema:"the property name (or title/name for the row title)"`
	Op       string `json:"op,omitempty" jsonschema:"comparison operator: eq (default), neq, contains, gt, gte, lt, lte, empty, or not_empty"`
	Value    string `json:"value,omitempty" jsonschema:"value to compare against (omit for empty or not_empty)"`
}

type queryDatabaseIn struct {
	Vault      string     `json:"vault,omitempty" jsonschema:"the vault's name or folder; omit for the default"`
	DatabaseID string     `json:"database_id" jsonschema:"the database page's id"`
	View       string     `json:"view,omitempty" jsonschema:"saved view name or id to apply its filter, sorts, and grouping"`
	Query      string     `json:"query,omitempty" jsonschema:"filter rows whose title or property values contain this text"`
	Filters    []dbFilter `json:"filters,omitempty" jsonschema:"property filters combined with AND"`
	SortBy     string     `json:"sort_by,omitempty" jsonschema:"sort by property name, title, row_order, updated_at, or created_at"`
	SortDir    string     `json:"sort_dir,omitempty" jsonschema:"sort direction: asc (default) or desc"`
	GroupBy    string     `json:"group_by,omitempty" jsonschema:"property name to count matching rows by"`
	Limit      int        `json:"limit,omitempty" jsonschema:"at most this many rows to return (default 50, up to 200)"`
	Offset     int        `json:"offset,omitempty" jsonschema:"number of matched rows to skip for pagination (default 0)"`
}

type groupCount struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

type queryDatabaseOut struct {
	DatabaseID string       `json:"database_id"`
	Title      string       `json:"title"`
	Total      int          `json:"total"`
	Returned   int          `json:"returned"`
	Offset     int          `json:"offset"`
	HasMore    bool         `json:"has_more"`
	Groups     []groupCount `json:"groups,omitempty"`
	Rows       []pageHit    `json:"rows"`
}

func (s *server) queryDatabase(_ context.Context, _ *mcp.CallToolRequest, in queryDatabaseIn) (*mcp.CallToolResult, queryDatabaseOut, error) {
	out := queryDatabaseOut{DatabaseID: in.DatabaseID, Rows: []pageHit{}}
	if in.Offset < 0 {
		return nil, out, errors.New("offset must be 0 or greater")
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	sortDir := strings.TrimSpace(strings.ToLower(in.SortDir))
	if sortDir != "" && sortDir != "asc" && sortDir != "desc" {
		return nil, out, fmt.Errorf("sort_dir %q must be asc or desc", in.SortDir)
	}

	err := s.within(in.Vault, func(v *vault.Vault, _ *Access) error {
		lib := load(v)
		dbPage, ok := lib.pages[in.DatabaseID]
		if !ok {
			return fmt.Errorf("no page %q in this vault", in.DatabaseID)
		}
		if !lib.isDatabase(in.DatabaseID) {
			return fmt.Errorf("page %q is not a database", in.DatabaseID)
		}
		out.Title = lib.title(in.DatabaseID)
		cols := lib.columns(in.DatabaseID)
		config, _ := dbPage["database"].(map[string]any)
		dbTitleName := strings.TrimSpace(str(config["title_name"]))
		if dbTitleName == "" {
			dbTitleName = "Name"
		}

		// Resolve saved view if named.
		var view map[string]any
		if strings.TrimSpace(in.View) != "" {
			var err error
			view, err = findView(config, in.View)
			if err != nil {
				return err
			}
		}

		// Resolve target field (column or row title) for filters/sort.
		resolveField := func(name string) (*column, bool, error) {
			trimmed := strings.TrimSpace(name)
			if trimmed == "" {
				return nil, false, errors.New("property name is empty")
			}
			if strings.EqualFold(trimmed, dbTitleName) {
				return nil, true, nil
			}
			col, err := columnLookup(cols, trimmed)
			if err == nil {
				return &col, false, nil
			}
			if strings.EqualFold(trimmed, "title") || strings.EqualFold(trimmed, "name") {
				return nil, true, nil
			}
			return nil, false, err
		}

		type compiledFilter struct {
			col   *column
			title bool
			op    string
			val   string
			exact bool
		}
		var filters []compiledFilter
		if view != nil {
			if vf, ok := view["filter"].(map[string]any); ok {
				if pid := str(vf["property_id"]); pid != "" {
					for i := range cols {
						if cols[i].id == pid {
							c := cols[i]
							filters = append(filters, compiledFilter{col: &c, op: "eq", val: str(vf["value"]), exact: true})
							break
						}
					}
				}
			}
		}
		for _, f := range in.Filters {
			op := strings.TrimSpace(strings.ToLower(f.Op))
			if op == "" {
				op = "eq"
			}
			switch op {
			case "eq", "neq", "contains", "gt", "gte", "lt", "lte", "empty", "not_empty":
			default:
				return fmt.Errorf("filter op %q must be eq, neq, contains, gt, gte, lt, lte, empty, or not_empty", f.Op)
			}
			col, isTitle, err := resolveField(f.Property)
			if err != nil {
				return err
			}
			val := strings.TrimSpace(f.Value)
			if (op == "gt" || op == "gte" || op == "lt" || op == "lte" || op == "contains") && val == "" {
				return fmt.Errorf("filter %s with op %q requires a non-empty value", f.Property, op)
			}
			if col != nil && col.kind == "date" && (op == "gt" || op == "gte" || op == "lt" || op == "lte" || ((op == "eq" || op == "neq") && val != "")) {
				if _, err := col.checked(val); err != nil {
					return err
				}
			}
			if col != nil && (col.kind == "status" || col.kind == "select") && (op == "gt" || op == "gte" || op == "lt" || op == "lte") {
				if _, err := col.checked(val); err != nil {
					return err
				}
			}
			filters = append(filters, compiledFilter{col: col, title: isTitle, op: op, val: val})
		}

		// Resolve sorting.
		type sortKey struct {
			mode string // "row_order", "title", "updated_at", "created_at", "column"
			col  *column
			desc bool
		}
		var sorts []sortKey
		if strings.TrimSpace(in.SortBy) != "" || sortDir != "" {
			desc := sortDir == "desc"
			by := strings.TrimSpace(in.SortBy)
			switch {
			case by == "" || strings.EqualFold(by, "row_order"):
				sorts = append(sorts, sortKey{mode: "row_order", desc: desc})
			case strings.EqualFold(by, "updated_at"):
				sorts = append(sorts, sortKey{mode: "updated_at", desc: desc})
			case strings.EqualFold(by, "created_at"):
				sorts = append(sorts, sortKey{mode: "created_at", desc: desc})
			default:
				col, isTitle, err := resolveField(by)
				if err != nil {
					return err
				}
				if isTitle {
					sorts = append(sorts, sortKey{mode: "title", desc: desc})
				} else {
					if col.kind == "image" {
						return fmt.Errorf("%s is an image property and cannot be sorted by", col.name)
					}
					sorts = append(sorts, sortKey{mode: "column", col: col, desc: desc})
				}
			}
		} else if view != nil {
			if rawSorts, ok := view["sorts"].([]any); ok {
				for _, item := range rawSorts {
					sm, _ := item.(map[string]any)
					if sm == nil {
						continue
					}
					pid := str(sm["property_id"])
					desc := strings.EqualFold(str(sm["direction"]), "desc")
					if pid == "name" {
						sorts = append(sorts, sortKey{mode: "title", desc: desc})
						continue
					}
					for i := range cols {
						if cols[i].id == pid && cols[i].kind != "image" {
							c := cols[i]
							sorts = append(sorts, sortKey{mode: "column", col: &c, desc: desc})
							break
						}
					}
				}
			}
		}

		// Resolve grouping column.
		var groupCol *column
		if strings.TrimSpace(in.GroupBy) != "" {
			c, err := columnLookup(cols, in.GroupBy)
			if err != nil {
				return err
			}
			groupCol = &c
		} else if view != nil && str(view["group_by"]) != "" {
			pid := str(view["group_by"])
			for i := range cols {
				if cols[i].id == pid {
					c := cols[i]
					groupCol = &c
					break
				}
			}
		}

		readCell := func(id string, col *column, isTitle bool) string {
			if isTitle || col == nil {
				return strings.TrimSpace(lib.title(id))
			}
			if col.kind == "image" {
				files, _ := lib.pages[id]["property_files"].(map[string]any)
				if list, _ := files[col.id].([]any); len(list) > 0 {
					return fmt.Sprintf("%d file(s)", len(list))
				}
				return ""
			}
			stored, _ := lib.pages[id]["properties"].(map[string]any)
			return col.readsAs(str(stored[col.id]))
		}

		needle := strings.ToLower(strings.TrimSpace(in.Query))
		allRows := lib.children(in.DatabaseID)
		var matched []string
		for _, id := range allRows {
			if needle != "" {
				hit := strings.Contains(strings.ToLower(lib.title(id)), needle)
				if !hit {
					for _, rv := range lib.rowValues(id) {
						if strings.Contains(strings.ToLower(rv.text), needle) {
							hit = true
							break
						}
					}
				}
				if !hit {
					continue
				}
			}
			ok := true
			for _, f := range filters {
				got := readCell(id, f.col, f.title)
				if !matchFilter(got, f.col, f.op, f.val, f.exact) {
					ok = false
					break
				}
			}
			if ok {
				matched = append(matched, id)
			}
		}

		if len(sorts) > 0 {
			sort.SliceStable(matched, func(i, j int) bool {
				aID, bID := matched[i], matched[j]
				for _, sk := range sorts {
					if sk.mode == "row_order" {
						ka, kb := lib.rowOrderKey(aID), lib.rowOrderKey(bID)
						if ka != kb {
							if sk.desc {
								return ka > kb
							}
							return ka < kb
						}
						continue
					}
					var a, b string
					var col *column
					switch sk.mode {
					case "title":
						a, b = lib.title(aID), lib.title(bID)
					case "updated_at":
						a, b = str(lib.pages[aID]["updated_at"]), str(lib.pages[bID]["updated_at"])
					case "created_at":
						a, b = str(lib.pages[aID]["created_at"]), str(lib.pages[bID]["created_at"])
					case "column":
						col = sk.col
						a, b = readCell(aID, col, false), readCell(bID, col, false)
					}
					// Empty values sink in both directions.
					if a == "" && b != "" {
						return false
					}
					if a != "" && b == "" {
						return true
					}
					if a == b {
						continue
					}
					cmp := compareColumnValues(col, a, b)
					if cmp != 0 {
						if sk.desc {
							return cmp > 0
						}
						return cmp < 0
					}
				}
				return false
			})
		}

		if groupCol != nil {
			out.Groups = buildGroupCounts(matched, groupCol, func(id string) string {
				return readCell(id, groupCol, false)
			})
		}

		out.Total = len(matched)
		start := min(in.Offset, len(matched))
		end := min(start+limit, len(matched))
		pageSlice := matched[start:end]
		out.Returned = len(pageSlice)
		out.Offset = start
		out.HasMore = end < out.Total
		for _, id := range pageSlice {
			out.Rows = append(out.Rows, lib.hit(id, ""))
		}
		return nil
	})
	return nil, out, err
}

func findView(config map[string]any, name string) (map[string]any, error) {
	views, _ := config["views"].([]any)
	wanted := strings.TrimSpace(name)
	var fold []map[string]any
	var available []string
	for _, item := range views {
		v, _ := item.(map[string]any)
		if v == nil {
			continue
		}
		vID := str(v["id"])
		vName := str(v["name"])
		if vName != "" {
			available = append(available, vName)
		}
		if vID == wanted || strings.TrimSpace(vName) == wanted {
			return v, nil
		}
		if strings.EqualFold(strings.TrimSpace(vName), wanted) {
			fold = append(fold, v)
		}
	}
	if len(fold) == 1 {
		return fold[0], nil
	}
	if len(fold) > 1 {
		return nil, fmt.Errorf("the database has several views called %q", wanted)
	}
	if len(available) == 0 {
		return nil, fmt.Errorf("no view %q in this database", wanted)
	}
	return nil, fmt.Errorf("no view %q in this database; views are: %s", wanted, strings.Join(available, ", "))
}

func optionRank(c column, value string) int {
	switch c.kind {
	case "status":
		for i, o := range c.statuses {
			if strings.EqualFold(o.name, value) {
				return i
			}
		}
		return len(c.statuses)
	case "select":
		for i, o := range c.options {
			if strings.EqualFold(strings.TrimSpace(o), strings.TrimSpace(value)) {
				return i
			}
		}
		return len(c.options)
	}
	return 0
}

func compareColumnValues(col *column, a, b string) int {
	if col != nil && (col.kind == "status" || col.kind == "select") {
		ra, rb := optionRank(*col, a), optionRank(*col, b)
		if ra < rb {
			return -1
		}
		if ra > rb {
			return 1
		}
		return 0
	}
	if na, errA := strconv.ParseFloat(a, 64); errA == nil && !math.IsNaN(na) && !math.IsInf(na, 0) {
		if nb, errB := strconv.ParseFloat(b, 64); errB == nil && !math.IsNaN(nb) && !math.IsInf(nb, 0) {
			if na < nb {
				return -1
			}
			if na > nb {
				return 1
			}
			return 0
		}
	}
	la, lb := strings.ToLower(a), strings.ToLower(b)
	if la != lb {
		return strings.Compare(la, lb)
	}
	return strings.Compare(a, b)
}

func matchFilter(got string, col *column, op, want string, exact bool) bool {
	switch op {
	case "empty":
		return got == ""
	case "not_empty":
		return got != ""
	case "eq":
		if exact {
			return got == want
		}
		return strings.EqualFold(strings.TrimSpace(got), want)
	case "neq":
		return !strings.EqualFold(strings.TrimSpace(got), want)
	case "contains":
		return strings.Contains(strings.ToLower(got), strings.ToLower(want))
	case "gt", "gte", "lt", "lte":
		if got == "" {
			return false
		}
		var cmp int
		if col != nil && col.kind == "date" {
			cmp = strings.Compare(got, want)
		} else {
			cmp = compareColumnValues(col, got, want)
		}
		switch op {
		case "gt":
			return cmp > 0
		case "gte":
			return cmp >= 0
		case "lt":
			return cmp < 0
		case "lte":
			return cmp <= 0
		}
	}
	return false
}

func buildGroupCounts(matched []string, col *column, read func(string) string) []groupCount {
	counts := map[string]int{}
	for _, id := range matched {
		v := read(id)
		if v == "" {
			v = "(empty)"
		}
		counts[v]++
	}
	var out []groupCount
	seen := map[string]bool{}
	if col.kind == "status" {
		for _, o := range col.statuses {
			out = append(out, groupCount{Value: o.name, Count: counts[o.name]})
			seen[o.name] = true
		}
	} else if col.kind == "select" {
		for _, o := range col.options {
			out = append(out, groupCount{Value: o, Count: counts[o]})
			seen[o] = true
		}
	}
	var extra []string
	for v := range counts {
		if !seen[v] && v != "(empty)" {
			extra = append(extra, v)
		}
	}
	if col.kind == "status" || col.kind == "select" {
		sort.Strings(extra)
	} else {
		sort.Slice(extra, func(i, j int) bool {
			if counts[extra[i]] != counts[extra[j]] {
				return counts[extra[i]] > counts[extra[j]]
			}
			return extra[i] < extra[j]
		})
	}
	for _, v := range extra {
		out = append(out, groupCount{Value: v, Count: counts[v]})
	}
	if counts["(empty)"] > 0 {
		out = append(out, groupCount{Value: "(empty)", Count: counts["(empty)"]})
	}
	return out
}

// ---- notes_create_database & notes_update_database --------------------------

type statusOptionSpecIn struct {
	Name  string `json:"name" jsonschema:"the status option's display name"`
	Group string `json:"group,omitempty" jsonschema:"status group: todo (default), progress, or complete"`
	Color string `json:"color,omitempty" jsonschema:"pill color: default, gray, brown, orange, yellow, green, blue, purple, pink, or red"`
}

type propertySpecIn struct {
	Name          string               `json:"name" jsonschema:"the column's name"`
	Type          string               `json:"type" jsonschema:"property type: text, select, status, date, person, or image"`
	Options       []string             `json:"options,omitempty" jsonschema:"choices for a select property"`
	StatusOptions []statusOptionSpecIn `json:"status_options,omitempty" jsonschema:"custom options for a status property; omit to use Not started, In progress, Done"`
	DefaultStatus string               `json:"default_status,omitempty" jsonschema:"the default option name for a status property"`
}

type propertySummary struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Type          string   `json:"type"`
	Options       []string `json:"options,omitempty"`
	DefaultStatus string   `json:"default_status,omitempty"`
}

type createDatabaseIn struct {
	Vault      string           `json:"vault,omitempty" jsonschema:"the vault's name or folder; omit for the default"`
	Title      string           `json:"title" jsonschema:"the database's title"`
	ParentID   string           `json:"parent_id,omitempty" jsonschema:"the page to put it under; omit for the top level"`
	Icon       string           `json:"icon,omitempty" jsonschema:"optional emoji icon"`
	TitleName  string           `json:"title_name,omitempty" jsonschema:"display name for the row title column (default Name)"`
	ViewKind   string           `json:"view_kind,omitempty" jsonschema:"initial view layout: table (default), board, list, gallery, calendar, or chart"`
	Properties []propertySpecIn `json:"properties,omitempty" jsonschema:"columns to create on the database"`
}

type createDatabaseOut struct {
	ID         string            `json:"id"`
	Title      string            `json:"title"`
	Properties []propertySummary `json:"properties"`
}

func (s *server) createDatabase(_ context.Context, _ *mcp.CallToolRequest, in createDatabaseIn) (*mcp.CallToolResult, createDatabaseOut, error) {
	var out createDatabaseOut
	if s.host.ReadOnly() {
		return nil, out, errors.New(readOnlyError)
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return nil, out, errors.New("the title is empty")
	}
	viewKind := strings.TrimSpace(strings.ToLower(in.ViewKind))
	if viewKind == "" {
		viewKind = "table"
	}
	if !validViewKinds[viewKind] {
		return nil, out, fmt.Errorf("view_kind %q must be table, board, list, gallery, calendar, or chart", in.ViewKind)
	}
	if in.TitleName != "" && strings.TrimSpace(in.TitleName) == "" {
		return nil, out, errors.New("title_name cannot be blank")
	}

	var props []map[string]any
	takenNames := map[string]bool{}
	for _, spec := range in.Properties {
		prop, err := buildPropertyRecord(spec, takenNames)
		if err != nil {
			return nil, out, err
		}
		takenNames[strings.ToLower(str(prop["name"]))] = true
		props = append(props, prop)
	}

	view := map[string]any{
		"id":   newID("view"),
		"name": viewLabel(viewKind),
		"kind": viewKind,
	}
	switch viewKind {
	case "board", "chart":
		var groupID string
		for _, p := range props {
			if str(p["type"]) == "status" {
				groupID = str(p["id"])
				break
			}
			if str(p["type"]) == "select" && groupID == "" {
				if opts, _ := p["options"].([]any); len(opts) > 0 {
					groupID = str(p["id"])
				}
			}
		}
		if groupID == "" {
			p, err := buildPropertyRecord(propertySpecIn{Name: nextAvailableName(takenNames, "Status"), Type: "status"}, takenNames)
			if err != nil {
				return nil, out, err
			}
			takenNames[strings.ToLower(str(p["name"]))] = true
			props = append(props, p)
			groupID = str(p["id"])
		}
		view["group_by"] = groupID
	case "calendar":
		var dateID string
		for _, p := range props {
			if str(p["type"]) == "date" {
				dateID = str(p["id"])
				break
			}
		}
		if dateID == "" {
			p, err := buildPropertyRecord(propertySpecIn{Name: nextAvailableName(takenNames, "Date"), Type: "date"}, takenNames)
			if err != nil {
				return nil, out, err
			}
			takenNames[strings.ToLower(str(p["name"]))] = true
			props = append(props, p)
			dateID = str(p["id"])
		}
		view["date_by"] = dateID
	}

	propList := make([]any, len(props))
	for i, p := range props {
		propList[i] = p
	}
	dbConfig := map[string]any{
		"properties": propList,
		"views":      []any{view},
	}
	if tn := strings.TrimSpace(in.TitleName); tn != "" {
		dbConfig["title_name"] = tn
	}

	err := s.within(in.Vault, func(v *vault.Vault, access *Access) error {
		lib := load(v)
		if in.ParentID != "" {
			if lib.pages[in.ParentID] == nil || lib.trashed(in.ParentID) {
				return fmt.Errorf("no page %q to put it under in this vault", in.ParentID)
			}
			if lib.isDatabase(in.ParentID) {
				return errors.New("a database cannot be created as a row inside another database")
			}
		}
		now := stamp()
		id := newID("page")
		page := map[string]any{
			"id":            id,
			"kind":          "database",
			"icon":          strings.TrimSpace(in.Icon),
			"title_content": markdown.StoredRuns(title),
			"created_at":    now,
			"updated_at":    now,
			"created_by":    authorName,
			"updated_by":    authorName,
			"database":      dbConfig,
		}
		if in.ParentID != "" {
			page["parent_id"] = in.ParentID
		}
		changes, err := apply(v, []vault.Write{{Action: "put", Collection: vault.Pages, ID: id, Value: page}})
		if err != nil {
			return err
		}
		access.Changed(vault.Changes{Changes: changes})
		out = createDatabaseOut{ID: id, Title: title, Properties: summarizePropMaps(props)}
		return nil
	})
	return nil, out, err
}

type renamePropertyIn struct {
	From string `json:"from" jsonschema:"the existing property name"`
	To   string `json:"to" jsonschema:"the new property name"`
}

type addOptionsIn struct {
	Property      string               `json:"property" jsonschema:"the select or status property's name"`
	Options       []string             `json:"options,omitempty" jsonschema:"option names to add to a select property"`
	StatusOptions []statusOptionSpecIn `json:"status_options,omitempty" jsonschema:"status options to add to a status property"`
}

type updateDatabaseIn struct {
	Vault            string             `json:"vault,omitempty" jsonschema:"the vault's name or folder; omit for the default"`
	DatabaseID       string             `json:"database_id" jsonschema:"the database page's id"`
	Title            string             `json:"title,omitempty" jsonschema:"a new title for the database"`
	TitleName        string             `json:"title_name,omitempty" jsonschema:"a new display name for the row title column"`
	AddProperties    []propertySpecIn   `json:"add_properties,omitempty" jsonschema:"new properties (columns) to add"`
	RenameProperties []renamePropertyIn `json:"rename_properties,omitempty" jsonschema:"existing properties to rename"`
	AddOptions       []addOptionsIn     `json:"add_options,omitempty" jsonschema:"new options to add to existing select or status properties"`
	RemoveProperties []string           `json:"remove_properties,omitempty" jsonschema:"names of properties to remove from the database and its views"`
}

type updateDatabaseOut struct {
	ID         string            `json:"id"`
	Title      string            `json:"title"`
	Properties []propertySummary `json:"properties"`
	Note       string            `json:"note,omitempty"`
}

func (s *server) updateDatabase(_ context.Context, _ *mcp.CallToolRequest, in updateDatabaseIn) (*mcp.CallToolResult, updateDatabaseOut, error) {
	var out updateDatabaseOut
	if s.host.ReadOnly() {
		return nil, out, errors.New(readOnlyError)
	}
	if in.Title != "" && strings.TrimSpace(in.Title) == "" {
		return nil, out, errors.New("the title is empty")
	}
	if in.TitleName != "" && strings.TrimSpace(in.TitleName) == "" {
		return nil, out, errors.New("title_name cannot be blank")
	}
	if strings.TrimSpace(in.Title) == "" && strings.TrimSpace(in.TitleName) == "" &&
		len(in.AddProperties) == 0 && len(in.RenameProperties) == 0 && len(in.AddOptions) == 0 && len(in.RemoveProperties) == 0 {
		return nil, out, errors.New("nothing to change: provide title, title_name, or property changes")
	}

	err := s.within(in.Vault, func(v *vault.Vault, access *Access) error {
		lib := load(v)
		page, ok := lib.pages[in.DatabaseID]
		if !ok {
			return fmt.Errorf("no page %q in this vault", in.DatabaseID)
		}
		if !lib.isDatabase(in.DatabaseID) {
			return fmt.Errorf("page %q is not a database", in.DatabaseID)
		}
		if lib.trashed(in.DatabaseID) {
			return errors.New("the database is in the Trash: restore it first (notes_restore_page)")
		}

		config, err := cloneDatabaseConfig(page["database"])
		if err != nil {
			return err
		}
		props := asMapSlice(config["properties"])
		views := asMapSlice(config["views"])

		// 1. Rename properties.
		for _, rn := range in.RenameProperties {
			idx, err := lookupPropMap(props, rn.From)
			if err != nil {
				return err
			}
			newName := strings.TrimSpace(rn.To)
			if newName == "" {
				return errors.New("new property name cannot be empty")
			}
			for j, other := range props {
				if j != idx && strings.EqualFold(strings.TrimSpace(str(other["name"])), newName) {
					return fmt.Errorf("the database already has a property named %q", newName)
				}
			}
			props[idx]["name"] = newName
		}

		// 2. Remove properties and clean up views.
		removedIDs := map[string]bool{}
		for _, name := range in.RemoveProperties {
			idx, err := lookupPropMap(props, name)
			if err != nil {
				return err
			}
			removedIDs[str(props[idx]["id"])] = true
			props = append(props[:idx], props[idx+1:]...)
		}
		if len(removedIDs) > 0 {
			for _, view := range views {
				cleanViewRemovedProperties(view, removedIDs)
			}
		}

		// 3. Add new properties.
		takenNames := map[string]bool{}
		for _, p := range props {
			takenNames[strings.ToLower(strings.TrimSpace(str(p["name"])))] = true
		}
		for _, spec := range in.AddProperties {
			p, err := buildPropertyRecord(spec, takenNames)
			if err != nil {
				return err
			}
			takenNames[strings.ToLower(str(p["name"]))] = true
			props = append(props, p)
		}

		// 4. Add options to existing select or status properties.
		for _, addOpt := range in.AddOptions {
			idx, err := lookupPropMap(props, addOpt.Property)
			if err != nil {
				return err
			}
			p := props[idx]
			pType := str(p["type"])
			switch pType {
			case "select":
				if len(addOpt.StatusOptions) > 0 {
					return fmt.Errorf("%s is a select property: use options, not status_options", str(p["name"]))
				}
				if len(addOpt.Options) == 0 {
					return fmt.Errorf("no options given for %s", str(p["name"]))
				}
				existing := asAnySlice(p["options"])
				seen := map[string]bool{}
				for _, o := range existing {
					seen[strings.ToLower(strings.TrimSpace(str(o)))] = true
				}
				for _, raw := range addOpt.Options {
					opt := strings.TrimSpace(raw)
					if opt == "" {
						return fmt.Errorf("option name for %s cannot be empty", str(p["name"]))
					}
					key := strings.ToLower(opt)
					if !seen[key] {
						seen[key] = true
						existing = append(existing, opt)
					}
				}
				p["options"] = existing
			case "status":
				if len(addOpt.Options) > 0 {
					return fmt.Errorf("%s is a status property: use status_options, not options", str(p["name"]))
				}
				if len(addOpt.StatusOptions) == 0 {
					return fmt.Errorf("no status_options given for %s", str(p["name"]))
				}
				existing := materializeStatusOptions(p)
				seen := map[string]bool{}
				for _, o := range existing {
					seen[strings.ToLower(strings.TrimSpace(str(o["name"])))] = true
				}
				for _, spec := range addOpt.StatusOptions {
					built, err := buildStatusOptionRecord(spec)
					if err != nil {
						return err
					}
					key := strings.ToLower(str(built["name"]))
					if !seen[key] {
						seen[key] = true
						existing = append(existing, built)
					}
				}
				if len(existing) > maxStatusOptions {
					return fmt.Errorf("%s cannot have more than %d status options", str(p["name"]), maxStatusOptions)
				}
				sort.SliceStable(existing, func(i, j int) bool {
					return groupRank[str(existing[i]["group"])] < groupRank[str(existing[j]["group"])]
				})
				rawList := make([]any, len(existing))
				for i, o := range existing {
					rawList[i] = o
				}
				p["status_options"] = rawList
			default:
				return fmt.Errorf("%s is a %s property, which does not take options", str(p["name"]), pType)
			}
		}

		// Write back into config.
		propList := make([]any, len(props))
		for i, p := range props {
			propList[i] = p
		}
		config["properties"] = propList
		viewList := make([]any, len(views))
		for i, view := range views {
			viewList[i] = view
		}
		config["views"] = viewList
		if tn := strings.TrimSpace(in.TitleName); tn != "" {
			config["title_name"] = tn
		}

		now := stamp()
		patch := map[string]any{
			"database":   config,
			"updated_at": now,
			"updated_by": authorName,
		}
		title := lib.title(in.DatabaseID)
		if t := strings.TrimSpace(in.Title); t != "" {
			title = t
			patch["title_content"] = markdown.StoredRuns(t)
		}
		changes, err := apply(v, []vault.Write{{Action: "patch", Collection: vault.Pages, ID: in.DatabaseID, Patch: patch}})
		if err != nil {
			return err
		}
		access.Changed(vault.Changes{Changes: changes})
		out = updateDatabaseOut{
			ID:         in.DatabaseID,
			Title:      title,
			Properties: summarizePropMaps(props),
			Note:       "updated database",
		}
		return nil
	})
	return nil, out, err
}

func viewLabel(kind string) string {
	if kind == "" {
		return "Table"
	}
	return strings.ToUpper(kind[:1]) + kind[1:]
}

func nextAvailableName(taken map[string]bool, base string) string {
	if !taken[strings.ToLower(base)] {
		return base
	}
	for n := 2; ; n++ {
		cand := fmt.Sprintf("%s %d", base, n)
		if !taken[strings.ToLower(cand)] {
			return cand
		}
	}
}

func buildStatusOptionRecord(spec statusOptionSpecIn) (map[string]any, error) {
	name := strings.TrimSpace(spec.Name)
	if name == "" {
		return nil, errors.New("status option name cannot be empty")
	}
	group := strings.TrimSpace(strings.ToLower(spec.Group))
	if group == "" {
		group = "todo"
	}
	if _, ok := groupRank[group]; !ok {
		return nil, fmt.Errorf("status option %q group %q must be todo, progress, or complete", name, spec.Group)
	}
	color := strings.TrimSpace(strings.ToLower(spec.Color))
	if color == "" {
		color = defaultGroupColor[group]
	}
	if !validOptionColors[color] {
		return nil, fmt.Errorf("status option %q color %q is not valid", name, spec.Color)
	}
	return map[string]any{
		"id":    newID("status"),
		"name":  name,
		"color": color,
		"group": group,
	}, nil
}

func buildPropertyRecord(spec propertySpecIn, taken map[string]bool) (map[string]any, error) {
	name := strings.TrimSpace(spec.Name)
	if name == "" {
		return nil, errors.New("property name cannot be empty")
	}
	if taken[strings.ToLower(name)] {
		return nil, fmt.Errorf("duplicate property name %q", name)
	}
	pType := strings.TrimSpace(strings.ToLower(spec.Type))
	if !validPropertyTypes[pType] {
		return nil, fmt.Errorf("property %q type %q must be text, select, status, date, person, or image", name, spec.Type)
	}
	if pType != "select" && len(spec.Options) > 0 {
		return nil, fmt.Errorf("property %q of type %s does not accept options", name, pType)
	}
	if pType != "status" && (len(spec.StatusOptions) > 0 || strings.TrimSpace(spec.DefaultStatus) != "") {
		return nil, fmt.Errorf("property %q of type %s does not accept status_options or default_status", name, pType)
	}

	prop := map[string]any{
		"id":   newID("property"),
		"name": name,
		"type": pType,
	}
	switch pType {
	case "select":
		opts := []any{}
		seen := map[string]bool{}
		for _, raw := range spec.Options {
			opt := strings.TrimSpace(raw)
			if opt == "" {
				return nil, fmt.Errorf("option name for %q cannot be empty", name)
			}
			key := strings.ToLower(opt)
			if !seen[key] {
				seen[key] = true
				opts = append(opts, opt)
			}
		}
		prop["options"] = opts
	case "status":
		var statuses []map[string]any
		if len(spec.StatusOptions) > 0 {
			seen := map[string]bool{}
			for _, so := range spec.StatusOptions {
				rec, err := buildStatusOptionRecord(so)
				if err != nil {
					return nil, err
				}
				key := strings.ToLower(str(rec["name"]))
				if seen[key] {
					return nil, fmt.Errorf("duplicate status option %q on %q", str(rec["name"]), name)
				}
				seen[key] = true
				statuses = append(statuses, rec)
			}
			if len(statuses) > maxStatusOptions {
				return nil, fmt.Errorf("%s cannot have more than %d status options", name, maxStatusOptions)
			}
			sort.SliceStable(statuses, func(i, j int) bool {
				return groupRank[str(statuses[i]["group"])] < groupRank[str(statuses[j]["group"])]
			})
			rawList := make([]any, len(statuses))
			for i, o := range statuses {
				rawList[i] = o
			}
			prop["status_options"] = rawList
		} else {
			for _, d := range defaultStatuses {
				statuses = append(statuses, map[string]any{
					"id":    d.id,
					"name":  d.name,
					"color": defaultGroupColor[d.group],
					"group": d.group,
				})
			}
		}
		if def := strings.TrimSpace(spec.DefaultStatus); def != "" {
			var foundID string
			var names []string
			for _, o := range statuses {
				names = append(names, str(o["name"]))
				if strings.EqualFold(str(o["name"]), def) {
					foundID = str(o["id"])
				}
			}
			if foundID == "" {
				return nil, fmt.Errorf("default_status %q is not an option of %s; options are: %s", def, name, strings.Join(names, ", "))
			}
			prop["default_status"] = foundID
		}
	}
	return prop, nil
}

func cloneDatabaseConfig(raw any) (map[string]any, error) {
	if raw == nil {
		return map[string]any{"properties": []any{}, "views": []any{}}, nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var copyCfg map[string]any
	if err := dec.Decode(&copyCfg); err != nil {
		return nil, err
	}
	if copyCfg == nil {
		copyCfg = map[string]any{}
	}
	return copyCfg, nil
}

func asMapSlice(v any) []map[string]any {
	raw, _ := v.([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if m, ok := item.(map[string]any); ok && m != nil {
			out = append(out, m)
		}
	}
	return out
}

func asAnySlice(v any) []any {
	raw, _ := v.([]any)
	out := make([]any, len(raw))
	copy(out, raw)
	return out
}

func lookupPropMap(props []map[string]any, name string) (int, error) {
	wanted := strings.TrimSpace(name)
	var exact []int
	var fold []int
	var names []string
	for i, p := range props {
		pName := strings.TrimSpace(str(p["name"]))
		names = append(names, str(p["name"]))
		if pName == wanted {
			exact = append(exact, i)
		}
		if strings.EqualFold(pName, wanted) {
			fold = append(fold, i)
		}
	}
	found := exact
	if len(found) == 0 {
		found = fold
	}
	switch len(found) {
	case 1:
		return found[0], nil
	case 0:
		return -1, fmt.Errorf("the database has no property %q; its properties are: %s", wanted, strings.Join(names, ", "))
	}
	return -1, fmt.Errorf("the database has several properties called %q: rename one of them in Notes first", wanted)
}

func materializeStatusOptions(p map[string]any) []map[string]any {
	existing := asMapSlice(p["status_options"])
	if len(existing) > 0 {
		return existing
	}
	out := make([]map[string]any, 0, len(defaultStatuses))
	for _, d := range defaultStatuses {
		out = append(out, map[string]any{
			"id":    d.id,
			"name":  d.name,
			"color": defaultGroupColor[d.group],
			"group": d.group,
		})
	}
	return out
}

func cleanViewRemovedProperties(view map[string]any, removed map[string]bool) {
	if vf, ok := view["filter"].(map[string]any); ok && removed[str(vf["property_id"])] {
		delete(view, "filter")
	}
	if rawSorts, ok := view["sorts"].([]any); ok {
		var kept []any
		for _, item := range rawSorts {
			sm, _ := item.(map[string]any)
			if sm != nil && !removed[str(sm["property_id"])] {
				kept = append(kept, sm)
			}
		}
		if len(kept) > 0 {
			view["sorts"] = kept
		} else {
			delete(view, "sorts")
		}
	}
	for _, key := range []string{"group_by", "date_by", "card_preview"} {
		if removed[str(view[key])] {
			delete(view, key)
		}
	}
	if cp, ok := view["card_properties"].([]any); ok {
		kept := []any{}
		for _, item := range cp {
			if !removed[str(item)] {
				kept = append(kept, item)
			}
		}
		view["card_properties"] = kept
	}
	if widths, ok := view["widths"].(map[string]any); ok {
		for id := range removed {
			delete(widths, id)
		}
		if len(widths) == 0 {
			delete(view, "widths")
		}
	}
	if wrap, ok := view["wrap"].(map[string]any); ok {
		for id := range removed {
			delete(wrap, id)
		}
		if len(wrap) == 0 {
			delete(view, "wrap")
		}
	}
}

func summarizePropMaps(props []map[string]any) []propertySummary {
	out := make([]propertySummary, 0, len(props))
	for _, p := range props {
		s := propertySummary{
			ID:   str(p["id"]),
			Name: str(p["name"]),
			Type: str(p["type"]),
		}
		switch s.Type {
		case "select":
			for _, o := range asAnySlice(p["options"]) {
				if name := str(o); name != "" {
					s.Options = append(s.Options, name)
				}
			}
		case "status":
			opts := materializeStatusOptions(p)
			sort.SliceStable(opts, func(i, j int) bool {
				return groupRank[str(opts[i]["group"])] < groupRank[str(opts[j]["group"])]
			})
			defID := str(p["default_status"])
			for _, o := range opts {
				name := str(o["name"])
				s.Options = append(s.Options, name)
				if defID != "" && str(o["id"]) == defID {
					s.DefaultStatus = name
				}
			}
		}
		out = append(out, s)
	}
	return out
}
