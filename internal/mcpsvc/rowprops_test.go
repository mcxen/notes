package mcpsvc

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"notes/internal/vault"
)

// A database as the app writes it: a status, a select, a date and a text
// column, and a board view grouped by the status.
func putDatabase(t *testing.T, dir string) string {
	t.Helper()
	const id = "page_board"
	v, err := vault.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	page := map[string]any{"id": id, "kind": "database", "icon": "", "title_content": []any{map[string]any{"text": "Board"}},
		"created_at": "2026-11-01T00:00:00.000Z", "updated_at": "2026-11-01T00:00:00.000Z", "created_by": "t", "updated_by": "t",
		"database": map[string]any{
			"title_name": "Feature",
			"properties": []any{
				map[string]any{"id": "prop_status", "name": "Status", "type": "status", "default_status": "wanted", "status_options": []any{
					map[string]any{"id": "done", "name": "Done", "color": "green", "group": "complete"},
					map[string]any{"id": "wanted", "name": "Wanted", "color": "gray", "group": "todo"},
					map[string]any{"id": "doing", "name": "Doing", "color": "blue", "group": "progress"},
				}},
				map[string]any{"id": "prop_area", "name": "Area", "type": "select", "options": []any{"Sync", "Editor"}},
				map[string]any{"id": "prop_due", "name": "Due", "type": "date"},
				map[string]any{"id": "prop_note", "name": "Note", "type": "text"},
				map[string]any{"id": "prop_owner", "name": "Owner", "type": "person"},
			},
			"views": []any{map[string]any{"id": "view_board", "name": "Kanban", "kind": "board", "group_by": "prop_status"}},
		}}
	if _, err := v.Write(vault.Write{Action: "put", Collection: vault.Pages, ID: id, Value: page}); err != nil {
		t.Fatal(err)
	}
	if err := v.Flush(); err != nil {
		t.Fatal(err)
	}
	return id
}

// pageRecord is a page as the vault keeps it.
func pageRecord(t *testing.T, dir, id string) map[string]any {
	t.Helper()
	v, err := vault.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	for _, row := range v.SnapshotWithoutHistory().Records {
		if row.Collection == vault.Pages && row.ID == id {
			return row.Value
		}
	}
	t.Fatalf("no page %s", id)
	return nil
}

func TestADatabaseListsItsPropertiesAndARowSaysItsValues(t *testing.T) {
	dir := t.TempDir()
	db := putDatabase(t, dir)
	c := newClient(t, testHost{folders: []string{dir}}, "sk-test")
	row := c.tool("notes_create_page", map[string]any{"title": "Sync", "parent_id": db,
		"properties": map[string]any{"Area": "sync", "Due": "2026-12-01", "Note": "first", "Status": "doing"}})["id"].(string)

	about := c.fetch(map[string]any{"page_id": db})["about"].(string)
	for _, line := range []string{
		"Kind: database (its rows are its sub-pages)",
		"Properties:\n- Status (status): Wanted, Doing, Done\n- Area (select): Sync, Editor\n- Due (date, YYYY-MM-DD)\n- Note (text)\n- Owner (person)",
		`Board view "Kanban" groups by Status`,
	} {
		if !strings.Contains(about, line) {
			t.Errorf("the database does not say %q:\n%s", line, about)
		}
	}
	about = c.fetch(map[string]any{"page_id": row})["about"].(string)
	for _, line := range []string{"Database: Board (" + db + ")", "Properties: Status: Doing; Area: Sync; Due: 2026-12-01; Note: first"} {
		if !strings.Contains(about, line) {
			t.Errorf("the row does not say %q:\n%s", line, about)
		}
	}
}

func TestARowWithNoStatusReadsAsTheDefault(t *testing.T) {
	dir := t.TempDir()
	db := putDatabase(t, dir)
	c := newClient(t, testHost{folders: []string{dir}}, "sk-test")
	row := c.tool("notes_create_page", map[string]any{"title": "Bare", "parent_id": db})["id"].(string)
	if about := c.fetch(map[string]any{"page_id": row})["about"].(string); !strings.Contains(about, "Properties: Status: Wanted\n") {
		t.Errorf("a row with no value reads as the default status:\n%s", about)
	}
	if _, kept := pageRecord(t, dir, row)["properties"]; kept {
		t.Error("a default is read, not written")
	}
}

func TestAListOfARowsCarriesItsValuesAndOtherPagesNone(t *testing.T) {
	dir := t.TempDir()
	db := putDatabase(t, dir)
	c := newClient(t, testHost{folders: []string{dir}}, "sk-test")
	c.tool("notes_create_page", map[string]any{"title": "Row", "parent_id": db, "properties": map[string]any{"Status": "Done", "Area": "Editor"}})
	plain := c.tool("notes_create_page", map[string]any{"title": "Plain"})["id"].(string)
	c.tool("notes_create_page", map[string]any{"title": "Under plain", "parent_id": plain})

	rows := c.tool("notes_list_pages", map[string]any{"parent_id": db})["pages"].([]any)
	if len(rows) != 1 {
		t.Fatalf("rows = %v", rows)
	}
	got := rows[0].(map[string]any)["properties"].(map[string]any)
	if got["Status"] != "Done" || got["Area"] != "Editor" || len(got) != 2 {
		t.Errorf("a listed row's values = %v", got)
	}
	for _, name := range []string{"notes_list_pages", "notes_recent_pages"} {
		for _, p := range c.tool(name, map[string]any{})["pages"].([]any) {
			page := p.(map[string]any)
			if _, has := page["properties"]; has && page["title"] != "Row" {
				t.Errorf("%s gives %v values", name, page["title"])
			}
		}
	}
	found := c.tool("notes_search", map[string]any{"query": "row"})["pages"].([]any)
	if len(found) != 1 || found[0].(map[string]any)["properties"].(map[string]any)["Status"] != "Done" {
		t.Errorf("a found row has no values: %v", found)
	}
}

func TestPropertiesAreSetUnderADatabaseAndNotUnderAPage(t *testing.T) {
	dir := t.TempDir()
	db := putDatabase(t, dir)
	c := newClient(t, testHost{folders: []string{dir}}, "sk-test")
	row := c.tool("notes_create_page", map[string]any{"title": "Made", "parent_id": db,
		"properties": map[string]any{"Status": "doing", "Area": "SYNC"}})["id"].(string)
	rec := pageRecord(t, dir, row)
	props := rec["properties"].(map[string]any)
	// The values are kept by the property's id, as the option's own spelling.
	if props["prop_status"] != "Doing" || props["prop_area"] != "Sync" || len(props) != 2 {
		t.Fatalf("stored properties = %v", props)
	}
	if order := asFloat(rec["row_order"]); order < 1.7e12 {
		t.Errorf("a new row has a row order in milliseconds: %v", rec["row_order"])
	}

	plain := c.tool("notes_create_page", map[string]any{"title": "Plain"})["id"].(string)
	if res := c.tool("notes_create_page", map[string]any{"title": "Child", "parent_id": plain, "properties": map[string]any{"Status": "Done"}}); res["_error"] == nil {
		t.Fatal("properties were taken under a page that is not a database")
	}
	if res := c.tool("notes_update_page", map[string]any{"page_id": plain, "properties": map[string]any{"Status": "Done"}}); res["_error"] == nil {
		t.Fatal("properties were set on a page that is not a row")
	}
	if got := titles(c.tool("notes_search", map[string]any{"query": "child"})); len(got) != 0 {
		t.Errorf("a refused call made a page: %v", got)
	}
}

func TestAnUpdateOfPropertiesChangesThemAlone(t *testing.T) {
	dir := t.TempDir()
	db := putDatabase(t, dir)
	c := newClient(t, testHost{folders: []string{dir}}, "sk-test")
	row := c.tool("notes_create_page", map[string]any{"title": "Card", "markdown": "the body", "parent_id": db,
		"properties": map[string]any{"Status": "Wanted", "Area": "Sync", "Note": "keep me"}})["id"].(string)
	before := len(c.tool("notes_recent_pages", map[string]any{})["pages"].([]any))

	if res := c.tool("notes_update_page", map[string]any{"page_id": row, "properties": map[string]any{"Status": "done", "Due": "2026-02-28"}}); res["_error"] != nil {
		t.Fatal(res["_error"])
	}
	props := pageRecord(t, dir, row)["properties"].(map[string]any)
	if props["prop_status"] != "Done" || props["prop_due"] != "2026-02-28" || props["prop_area"] != "Sync" || props["prop_note"] != "keep me" {
		t.Fatalf("an update changed more or less than it was asked: %v", props)
	}
	if md := c.fetch(map[string]any{"page_id": row})["markdown"]; md != "the body" {
		t.Errorf("the body = %q", md)
	}
	if len(c.tool("notes_recent_pages", map[string]any{})["pages"].([]any)) != before {
		t.Error("an update made a page")
	}

	// An empty string clears one value and leaves the rest.
	c.tool("notes_update_page", map[string]any{"page_id": row, "properties": map[string]any{"Note": "", "Due": " "}})
	props = pageRecord(t, dir, row)["properties"].(map[string]any)
	if _, has := props["prop_note"]; has || props["prop_status"] != "Done" || len(props) != 2 {
		t.Errorf("after clearing: %v", props)
	}
	// Clearing the status brings back the default, which is read, not kept.
	c.tool("notes_update_page", map[string]any{"page_id": row, "properties": map[string]any{"Status": ""}})
	if about := c.fetch(map[string]any{"page_id": row})["about"].(string); !strings.Contains(about, "Status: Wanted") {
		t.Errorf("a cleared status:\n%s", about)
	}

	// The page as it was is kept in its history, as for any change.
	v, _ := vault.Open(dir)
	defer v.Close()
	if len(v.VersionsOf(row)) == 0 {
		t.Error("the update kept no version of the page")
	}
}

func TestAWrongValueRefusesTheWholeCallAndWritesNothing(t *testing.T) {
	dir := t.TempDir()
	db := putDatabase(t, dir)
	c := newClient(t, testHost{folders: []string{dir}}, "sk-test")
	row := c.tool("notes_create_page", map[string]any{"title": "Card", "parent_id": db, "properties": map[string]any{"Status": "Wanted"}})["id"].(string)
	before := fmt.Sprint(pageRecord(t, dir, row))

	for name, props := range map[string]map[string]any{
		"an unknown property":    {"Status": "Done", "Priority": "High"},
		"a status not an option": {"Area": "Sync", "Status": "Blocked"},
		"a select not an option": {"Area": "Nowhere"},
		"a date not a day":       {"Due": "2026-02-30"},
		"a date in words":        {"Due": "tomorrow"},
		"a person":               {"Owner": "ann"},
		"too long a text":        {"Note": strings.Repeat("x", 2001)},
	} {
		res := c.tool("notes_update_page", map[string]any{"page_id": row, "properties": props})
		if res["_error"] == nil {
			t.Errorf("%s was taken", name)
			continue
		}
		if after := fmt.Sprint(pageRecord(t, dir, row)); after != before {
			t.Errorf("%s changed the row:\n%s\n%s", name, before, after)
		}
	}
	// The error says what the values may be.
	msg := fmt.Sprint(c.tool("notes_update_page", map[string]any{"page_id": row, "properties": map[string]any{"Status": "Blocked"}})["_error"])
	if !strings.Contains(msg, "Wanted, Doing, Done") {
		t.Errorf("the error does not list the options: %s", msg)
	}
	msg = fmt.Sprint(c.tool("notes_update_page", map[string]any{"page_id": row, "properties": map[string]any{"Nope": "x"}})["_error"])
	if !strings.Contains(msg, "Status, Area, Due, Note, Owner") {
		t.Errorf("the error does not list the properties: %s", msg)
	}
	// A row is not made by a call that has a wrong value.
	if res := c.tool("notes_create_page", map[string]any{"title": "Never", "parent_id": db, "properties": map[string]any{"Status": "Blocked"}}); res["_error"] == nil {
		t.Error("a row with a wrong value was made")
	}
	if got := c.tool("notes_search", map[string]any{"query": "never"})["pages"].([]any); len(got) != 0 {
		t.Errorf("a refused create left a page: %v", got)
	}
}

func TestTwoColumnsOfOneNameAreNotGuessedBetween(t *testing.T) {
	dir := t.TempDir()
	db := putDatabase(t, dir)
	v, _ := vault.Open(dir)
	page := pageRecord(t, dir, db)
	cfg := page["database"].(map[string]any)
	cfg["properties"] = append(cfg["properties"].([]any), map[string]any{"id": "prop_twin", "name": "Note", "type": "text"})
	v.Write(vault.Write{Action: "put", Collection: vault.Pages, ID: db, Value: page})
	v.Flush()
	v.Close()
	c := newClient(t, testHost{folders: []string{dir}}, "sk-test")
	if res := c.tool("notes_create_page", map[string]any{"title": "x", "parent_id": db, "properties": map[string]any{"Note": "a"}}); res["_error"] == nil {
		t.Fatal("a name two columns share was taken")
	}
}

func TestPropertiesAreNotSetByAReadOnlyServerOrWithIfExistsAppend(t *testing.T) {
	dir := t.TempDir()
	db := putDatabase(t, dir)
	host := testHost{folders: []string{dir}}
	c := newClient(t, host, "sk-test")
	row := c.tool("notes_create_page", map[string]any{"title": "Card", "parent_id": db})["id"].(string)
	if res := c.tool("notes_create_page", map[string]any{"title": "Card", "parent_id": db, "if_exists": "append", "properties": map[string]any{"Status": "Done"}}); res["_error"] == nil {
		t.Error("properties were taken with if_exists append")
	}
	host.readOnly = true
	c2 := newClient(t, host, "sk-test")
	if res := c2.tool("notes_update_page", map[string]any{"page_id": row, "properties": map[string]any{"Status": "Done"}}); res["_error"] == nil {
		t.Error("a read-only server set a property")
	}
	if _, set := pageRecord(t, dir, row)["properties"]; set {
		t.Error("a read-only server wrote a property")
	}
	// Reading is still allowed.
	if about := c2.fetch(map[string]any{"page_id": row})["about"].(string); !strings.Contains(about, "Status: Wanted") {
		t.Errorf("a read-only server cannot read a row:\n%s", about)
	}
}

func TestAnUpdateWithNothingInItIsStillRefused(t *testing.T) {
	dir := t.TempDir()
	db := putDatabase(t, dir)
	c := newClient(t, testHost{folders: []string{dir}}, "sk-test")
	row := c.tool("notes_create_page", map[string]any{"title": "Card", "parent_id": db})["id"].(string)
	if res := c.tool("notes_update_page", map[string]any{"page_id": row, "properties": map[string]any{}}); res["_error"] == nil {
		t.Fatal("an update of nothing was taken")
	}
}

// asFloat reads a number as the vault keeps it: decoded from JSON, a json.Number.
func asFloat(v any) float64 {
	switch n := v.(type) {
	case json.Number:
		f, _ := n.Float64()
		return f
	case float64:
		return n
	}
	return 0
}
