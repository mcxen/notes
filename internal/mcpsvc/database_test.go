package mcpsvc

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"notes/internal/vault"
)

func TestCreateDatabaseWithTypedPropertiesAndViews(t *testing.T) {
	dir := t.TempDir()
	c := newClient(t, testHost{folders: []string{dir}}, "sk-test")

	res := c.tool("notes_create_database", map[string]any{
		"title":      "Roadmap",
		"icon":       "🗺️",
		"title_name": "Initiative",
		"view_kind":  "table",
		"properties": []map[string]any{
			{
				"name": "Stage",
				"type": "status",
				"status_options": []map[string]any{
					{"name": "Shipped", "group": "complete", "color": "green"},
					{"name": "Queued", "group": "todo", "color": "gray"},
					{"name": "Building", "group": "progress", "color": "blue"},
				},
				"default_status": "Queued",
			},
			{"name": "Area", "type": "select", "options": []string{"Core", "Core", "Sync"}},
			{"name": "Due", "type": "date"},
			{"name": "Notes", "type": "text"},
			{"name": "Owner", "type": "person"},
			{"name": "Mockup", "type": "image"},
		},
	})
	if res["_error"] != nil {
		t.Fatalf("create database: %v", res)
	}
	dbID := res["id"].(string)
	props := res["properties"].([]any)
	if len(props) != 6 {
		t.Fatalf("props = %v", props)
	}
	stage := props[0].(map[string]any)
	// Status options sorted by group order: todo -> progress -> complete.
	if fmt.Sprint(stage["options"]) != "[Queued Building Shipped]" || stage["default_status"] != "Queued" {
		t.Errorf("stage summary = %v", stage)
	}
	area := props[1].(map[string]any)
	if fmt.Sprint(area["options"]) != "[Core Sync]" {
		t.Errorf("area dedup options = %v", area)
	}

	about := c.fetch(map[string]any{"page_id": dbID})["about"].(string)
	for _, want := range []string{
		"Kind: database (its rows are its sub-pages)",
		"- Stage (status): Queued, Building, Shipped",
		"- Area (select): Core, Sync",
		"Views: Table (table)",
	} {
		if !strings.Contains(about, want) {
			t.Errorf("about missing %q:\n%s", want, about)
		}
	}

	// Board view without status/select property auto-creates Status column and sets group_by.
	board := c.tool("notes_create_database", map[string]any{
		"title":     "Sprint",
		"view_kind": "board",
	})
	boardID := board["id"].(string)
	if len(board["properties"].([]any)) != 1 {
		t.Fatalf("board auto-properties = %v", board)
	}
	boardAbout := c.fetch(map[string]any{"page_id": boardID})["about"].(string)
	if !strings.Contains(boardAbout, `Board view "Board" groups by Status`) {
		t.Errorf("board about = %s", boardAbout)
	}

	// Calendar view without date property auto-creates Date column and sets date_by.
	cal := c.tool("notes_create_database", map[string]any{
		"title":     "Events",
		"view_kind": "calendar",
	})
	calID := cal["id"].(string)
	calRec := pageRecord(t, dir, calID)["database"].(map[string]any)
	calView := calRec["views"].([]any)[0].(map[string]any)
	calPropID := cal["properties"].([]any)[0].(map[string]any)["id"]
	if calView["date_by"] != calPropID {
		t.Errorf("calendar view date_by = %v, want %v", calView, calPropID)
	}

	// Validation errors and ReadOnly check.
	for name, args := range map[string]map[string]any{
		"empty title":         {"title": "  "},
		"invalid view":        {"title": "Bad", "view_kind": "kanban"},
		"duplicate property":  {"title": "Bad", "properties": []map[string]any{{"name": "A", "type": "text"}, {"name": "a", "type": "date"}}},
		"invalid prop type":   {"title": "Bad", "properties": []map[string]any{{"name": "A", "type": "number"}}},
		"options on text":     {"title": "Bad", "properties": []map[string]any{{"name": "A", "type": "text", "options": []string{"x"}}}},
		"status opts on date": {"title": "Bad", "properties": []map[string]any{{"name": "A", "type": "date", "status_options": []map[string]any{{"name": "x"}}}}},
		"bad default_status":  {"title": "Bad", "properties": []map[string]any{{"name": "S", "type": "status", "default_status": "Missing"}}},
		"inside database":     {"title": "Nested", "parent_id": dbID},
	} {
		if r := c.tool("notes_create_database", args); r["_error"] == nil {
			t.Errorf("%s was accepted: %v", name, r)
		}
	}

	ro := newClient(t, testHost{folders: []string{dir}, readOnly: true}, "sk-test")
	if r := ro.tool("notes_create_database", map[string]any{"title": "Blocked"}); r["_error"] == nil {
		t.Errorf("read-only created database: %v", r)
	}
}

func TestUpdateDatabaseSchemaAndCleansUpViews(t *testing.T) {
	dir := t.TempDir()
	dbID := putDatabase(t, dir)
	c := newClient(t, testHost{folders: []string{dir}}, "sk-test")

	// Add filter, sort, widths, wrap, card_properties referencing prop_area on the view.
	v, _ := vault.Open(dir)
	rec := pageRecord(t, dir, dbID)
	cfg := rec["database"].(map[string]any)
	view := cfg["views"].([]any)[0].(map[string]any)
	view["filter"] = map[string]any{"property_id": "prop_area", "value": "Sync"}
	view["sorts"] = []any{
		map[string]any{"property_id": "prop_area", "direction": "asc"},
		map[string]any{"property_id": "prop_due", "direction": "desc"},
	}
	view["widths"] = map[string]any{"prop_area": 200}
	view["wrap"] = map[string]any{"prop_area": false}
	view["card_properties"] = []any{"prop_status", "prop_area"}
	_, _ = v.Write(vault.Write{Action: "put", Collection: vault.Pages, ID: dbID, Value: rec})
	_ = v.Flush()
	_ = v.Close()

	// Invalid update must refuse atomically without changing the database.
	before := fmt.Sprint(pageRecord(t, dir, dbID)["database"])
	if r := c.tool("notes_update_database", map[string]any{
		"database_id":       dbID,
		"rename_properties": []map[string]any{{"from": "Area", "to": "Status"}},
	}); r["_error"] == nil {
		t.Fatal("colliding rename was accepted")
	}
	if after := fmt.Sprint(pageRecord(t, dir, dbID)["database"]); after != before {
		t.Fatalf("failed update mutated database:\n%s\nvs\n%s", before, after)
	}

	// Valid update: rename Note -> Summary, remove Area (cleaning view), add Priority, add status option.
	up := c.tool("notes_update_database", map[string]any{
		"database_id":       dbID,
		"title":             "Product Board",
		"title_name":        "Epic",
		"rename_properties": []map[string]any{{"from": "Note", "to": "Summary"}},
		"remove_properties": []string{"Area"},
		"add_properties":    []map[string]any{{"name": "Priority", "type": "select", "options": []string{"P0"}}},
		"add_options": []map[string]any{
			{"property": "Priority", "options": []string{"P1", "P0"}},
			{"property": "Status", "status_options": []map[string]any{{"name": "Review", "group": "progress", "color": "purple"}}},
		},
	})
	if up["_error"] != nil {
		t.Fatalf("update_database: %v", up)
	}
	if up["title"] != "Product Board" {
		t.Errorf("title = %v", up["title"])
	}

	dbAfter := pageRecord(t, dir, dbID)["database"].(map[string]any)
	if dbAfter["title_name"] != "Epic" {
		t.Errorf("title_name = %v", dbAfter["title_name"])
	}
	viewAfter := dbAfter["views"].([]any)[0].(map[string]any)
	if _, has := viewAfter["filter"]; has {
		t.Errorf("removed property filter remained on view: %v", viewAfter)
	}
	if sorts := viewAfter["sorts"].([]any); len(sorts) != 1 || sorts[0].(map[string]any)["property_id"] != "prop_due" {
		t.Errorf("view sorts after removal = %v", viewAfter["sorts"])
	}
	if _, has := viewAfter["widths"]; has {
		t.Errorf("removed property width remained: %v", viewAfter["widths"])
	}
	if _, has := viewAfter["wrap"]; has {
		t.Errorf("removed property wrap remained: %v", viewAfter["wrap"])
	}
	if cp := viewAfter["card_properties"].([]any); len(cp) != 1 || cp[0] != "prop_status" {
		t.Errorf("card_properties after removal = %v", cp)
	}

	// Verify status options kept group order and Priority got P1.
	about := c.fetch(map[string]any{"page_id": dbID})["about"].(string)
	if !strings.Contains(about, "- Status (status): Wanted, Doing, Review, Done") {
		t.Errorf("status options order = %s", about)
	}
	if !strings.Contains(about, "- Priority (select): P0, P1") {
		t.Errorf("priority options = %s", about)
	}
	if !strings.Contains(about, "- Summary (text)") {
		t.Errorf("renamed property missing = %s", about)
	}

	// Also verify ReadOnly and nothing-to-change rejections.
	if r := c.tool("notes_update_database", map[string]any{"database_id": dbID}); r["_error"] == nil {
		t.Error("empty update_database accepted")
	}
	ro := newClient(t, testHost{folders: []string{dir}, readOnly: true}, "sk-test")
	if r := ro.tool("notes_update_database", map[string]any{"database_id": dbID, "title": "Nope"}); r["_error"] == nil {
		t.Error("read-only update_database accepted")
	}
}

func TestQueryDatabaseFiltersSortsPaginatesAndGroups(t *testing.T) {
	dir := t.TempDir()
	dbID := putDatabase(t, dir)
	c := newClient(t, testHost{folders: []string{dir}}, "sk-test")

	c.tool("notes_create_page", map[string]any{"title": "Task A", "parent_id": dbID,
		"properties": map[string]any{"Status": "Wanted", "Area": "Sync", "Due": "2026-11-10", "Note": "10"}})
	time.Sleep(3 * time.Millisecond)
	c.tool("notes_create_page", map[string]any{"title": "Task B", "parent_id": dbID,
		"properties": map[string]any{"Status": "Doing", "Area": "Sync", "Due": "2026-11-05", "Note": "2"}})
	time.Sleep(3 * time.Millisecond)
	c.tool("notes_create_page", map[string]any{"title": "Task C", "parent_id": dbID,
		"properties": map[string]any{"Status": "Done", "Area": "Editor", "Note": "0 alpha beta"}})
	time.Sleep(3 * time.Millisecond)
	c.tool("notes_create_page", map[string]any{"title": "Task D", "parent_id": dbID})

	// Filter eq (case-insensitive) + range gt on date + contains on title.
	res := c.tool("notes_query_database", map[string]any{
		"database_id": dbID,
		"filters": []map[string]any{
			{"property": "area", "op": "eq", "value": "sync"},
			{"property": "Due", "op": "gte", "value": "2026-11-05"},
		},
		"sort_by":  "Due",
		"sort_dir": "asc",
	})
	if got := titles(map[string]any{"pages": res["rows"]}); fmt.Sprint(got) != "[Task B Task A]" {
		t.Errorf("date asc filter+sort = %v", got)
	}

	// Numeric comparison in text column: "10" > "2" (not lexical "10" < "2").
	numRes := c.tool("notes_query_database", map[string]any{
		"database_id": dbID,
		"filters":     []map[string]any{{"property": "Note", "op": "gt", "value": "5"}},
	})
	if got := titles(map[string]any{"pages": numRes["rows"]}); fmt.Sprint(got) != "[Task A]" {
		t.Errorf("numeric text gt = %v", got)
	}

	// Empty values sink in BOTH asc and desc sorts.
	descDue := c.tool("notes_query_database", map[string]any{
		"database_id": dbID,
		"sort_by":     "Due",
		"sort_dir":    "desc",
	})
	if got := titles(map[string]any{"pages": descDue["rows"]}); fmt.Sprint(got) != "[Task A Task B Task C Task D]" {
		t.Errorf("date desc with empty sink = %v", got)
	}

	// Status range gt/lte compares by option order: Wanted < Doing < Done.
	inProgOrDone := c.tool("notes_query_database", map[string]any{
		"database_id": dbID,
		"filters":     []map[string]any{{"property": "Status", "op": "gt", "value": "Wanted"}},
		"sort_by":     "Status",
		"sort_dir":    "desc",
	})
	if got := titles(map[string]any{"pages": inProgOrDone["rows"]}); fmt.Sprint(got) != "[Task C Task B]" {
		t.Errorf("status gt+desc = %v", got)
	}

	// View applies group_by and computes groups across ALL matched rows even with limit=1 & offset=1.
	paged := c.tool("notes_query_database", map[string]any{
		"database_id": dbID,
		"view":        "Kanban",
		"limit":       1,
		"offset":      1,
	})
	if int(asFloat(paged["total"])) != 4 || int(asFloat(paged["returned"])) != 1 || paged["has_more"] != true {
		t.Errorf("pagination = %v", paged)
	}
	if got := titles(map[string]any{"pages": paged["rows"]}); fmt.Sprint(got) != "[Task B]" {
		t.Errorf("paged row = %v", got)
	}
	groups := paged["groups"].([]any)
	// Task D has default status Wanted, so Wanted=2, Doing=1, Done=1.
	if len(groups) != 3 ||
		groups[0].(map[string]any)["value"] != "Wanted" || int(asFloat(groups[0].(map[string]any)["count"])) != 2 ||
		groups[1].(map[string]any)["value"] != "Doing" || int(asFloat(groups[1].(map[string]any)["count"])) != 1 ||
		groups[2].(map[string]any)["value"] != "Done" || int(asFloat(groups[2].(map[string]any)["count"])) != 1 {
		t.Errorf("groups = %v", groups)
	}

	// Query text search + title filter via database title_name ("Feature").
	qRes := c.tool("notes_query_database", map[string]any{
		"database_id": dbID,
		"query":       "alpha",
		"filters":     []map[string]any{{"property": "Feature", "op": "contains", "value": "Task C"}},
	})
	if got := titles(map[string]any{"pages": qRes["rows"]}); fmt.Sprint(got) != "[Task C]" {
		t.Errorf("query + title_name filter = %v", got)
	}

	// Group by Area includes (empty) for Task D.
	areaGrouped := c.tool("notes_query_database", map[string]any{
		"database_id": dbID,
		"group_by":    "Area",
	})
	areaGroups := areaGrouped["groups"].([]any)
	if len(areaGroups) != 3 || areaGroups[2].(map[string]any)["value"] != "(empty)" || int(asFloat(areaGroups[2].(map[string]any)["count"])) != 1 {
		t.Errorf("area groups = %v", areaGroups)
	}

	// Validation errors: bad view, bad filter op, invalid date, image sort.
	if r := c.tool("notes_query_database", map[string]any{"database_id": dbID, "view": "NoSuchView"}); r["_error"] == nil {
		t.Error("unknown view accepted")
	}
	if r := c.tool("notes_query_database", map[string]any{"database_id": dbID, "filters": []map[string]any{{"property": "Due", "op": "gt", "value": "bad"}}}); r["_error"] == nil {
		t.Error("bad date in filter accepted")
	}
	c.tool("notes_update_database", map[string]any{"database_id": dbID, "add_properties": []map[string]any{{"name": "Pic", "type": "image"}}})
	if r := c.tool("notes_query_database", map[string]any{"database_id": dbID, "sort_by": "Pic"}); r["_error"] == nil {
		t.Error("image property accepted as sort_by")
	}
}

func TestMovePageIntoDatabaseAndRejectDatabaseInsideDatabase(t *testing.T) {
	dir := t.TempDir()
	dbID := putDatabase(t, dir)
	c := newClient(t, testHost{folders: []string{dir}}, "sk-test")

	docID := c.tool("notes_create_page", map[string]any{"title": "Inbox Item", "markdown": "details"})["id"].(string)
	moved := c.tool("notes_move_page", map[string]any{"page_id": docID, "parent_id": dbID})
	if moved["_error"] != nil {
		t.Fatalf("move page into database: %v", moved)
	}
	rec := pageRecord(t, dir, docID)
	if asFloat(rec["row_order"]) < 1.7e12 {
		t.Errorf("moved row missing row_order: %v", rec)
	}

	// Moved page behaves as a database row.
	if up := c.tool("notes_update_page", map[string]any{"page_id": docID, "properties": map[string]any{"Status": "Doing"}}); up["_error"] != nil {
		t.Fatalf("update moved row properties: %v", up)
	}
	q := c.tool("notes_query_database", map[string]any{
		"database_id": dbID,
		"filters":     []map[string]any{{"property": "Status", "value": "Doing"}},
	})
	if got := titles(map[string]any{"pages": q["rows"]}); fmt.Sprint(got) != "[Inbox Item]" {
		t.Errorf("moved row in query = %v", got)
	}

	// Moving a database into another database is refused.
	db2 := c.tool("notes_create_database", map[string]any{"title": "Nested DB"})["id"].(string)
	if r := c.tool("notes_move_page", map[string]any{"page_id": db2, "parent_id": dbID}); r["_error"] == nil {
		t.Error("database was moved inside another database")
	}
}
