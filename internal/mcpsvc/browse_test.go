package mcpsvc

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
	"time"

	"notes/internal/vault"
)

func TestToolsHaveTitlesAndAnnotations(t *testing.T) {
	dir := t.TempDir()
	c := newClient(t, testHost{folders: []string{dir}}, "sk-test")
	rawTools := c.call("tools/list", map[string]any{}, "Bearer sk-test")["result"].(map[string]any)["tools"].([]any)

	expectedReadOnly := map[string]bool{
		"notes_list_vaults":     true,
		"notes_search":          true,
		"notes_list_pages":      true,
		"notes_page_tree":       true,
		"notes_recent_pages":    true,
		"notes_list_trash":      true,
		"notes_list_todos":      true,
		"notes_fetch":           true,
		"notes_read_attachment": true,
		"notes_page_history":    true,
		"notes_read_version":    true,
		"notes_query_database":  true,
	}
	expectedMutating := map[string]bool{
		"notes_create_page":     false, // destructive=false
		"notes_create_database": false,
		"notes_add_attachment":  false,
		"notes_move_page":       false,
		"notes_restore_page":    false,
		"notes_update_page":     true, // destructive=true
		"notes_edit_page":       true,
		"notes_update_database": true,
		"notes_trash_page":      true,
		"notes_restore_version": true,
	}

	seen := map[string]bool{}
	for _, item := range rawTools {
		tool := item.(map[string]any)
		name := tool["name"].(string)
		seen[name] = true
		if str(tool["title"]) == "" {
			t.Errorf("tool %s missing title", name)
		}
		ann, ok := tool["annotations"].(map[string]any)
		if !ok {
			t.Errorf("tool %s missing annotations", name)
			continue
		}
		if ann["openWorldHint"] != false {
			t.Errorf("tool %s openWorldHint = %v, want false", name, ann["openWorldHint"])
		}
		if expectedReadOnly[name] {
			if ann["readOnlyHint"] != true {
				t.Errorf("read-only tool %s readOnlyHint = %v", name, ann["readOnlyHint"])
			}
		} else if wantDestructive, ok := expectedMutating[name]; ok {
			if ann["readOnlyHint"] != false {
				t.Errorf("mutating tool %s readOnlyHint = %v", name, ann["readOnlyHint"])
			}
			if ann["destructiveHint"] != wantDestructive {
				t.Errorf("mutating tool %s destructiveHint = %v, want %v", name, ann["destructiveHint"], wantDestructive)
			}
		} else {
			t.Errorf("unexpected tool %s without expected annotation category", name)
		}
	}
	for name := range expectedReadOnly {
		if !seen[name] {
			t.Errorf("missing read-only tool %s", name)
		}
	}
	for name := range expectedMutating {
		if !seen[name] {
			t.Errorf("missing mutating tool %s", name)
		}
	}
}

func TestListPagesIncludesKindChildCountRootsAndRowOrder(t *testing.T) {
	dir := t.TempDir()
	c := newClient(t, testHost{folders: []string{dir}}, "sk-test")

	alpha := c.tool("notes_create_page", map[string]any{"title": "Alpha"})["id"].(string)
	beta := c.tool("notes_create_page", map[string]any{"title": "Beta"})["id"].(string)
	c.tool("notes_create_page", map[string]any{"title": "Child", "parent_id": alpha})
	db := c.tool("notes_create_database", map[string]any{"title": "Tasks"})["id"].(string)

	// Set workspace_settings/roots so Beta precedes Alpha, while Tasks stays unranked after both.
	v, err := vault.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.Write(vault.Write{Action: "put", Collection: vault.Settings, ID: "roots", Value: vault.Record{"roots": []any{beta, alpha}}}); err != nil {
		t.Fatal(err)
	}
	if err := v.Flush(); err != nil {
		t.Fatal(err)
	}
	_ = v.Close()

	top := c.tool("notes_list_pages", map[string]any{})["pages"].([]any)
	if got := titles(map[string]any{"pages": top}); fmt.Sprint(got) != "[Beta Alpha Tasks]" {
		t.Fatalf("top order with roots = %v, want [Beta Alpha Tasks]", got)
	}
	alphaHit := top[1].(map[string]any)
	if alphaHit["kind"] != "document" || asFloat(alphaHit["child_count"]) != 1 {
		t.Errorf("alpha hit = %v", alphaHit)
	}
	tasksHit := top[2].(map[string]any)
	if tasksHit["kind"] != "database" {
		t.Errorf("tasks hit = %v", tasksHit)
	}

	// Database children sort by row_order ascending, not alphabetically.
	rZ := c.tool("notes_create_page", map[string]any{"title": "Zebra", "parent_id": db})["id"].(string)
	time.Sleep(5 * time.Millisecond)
	rA := c.tool("notes_create_page", map[string]any{"title": "Apple", "parent_id": db})["id"].(string)
	rows := c.tool("notes_list_pages", map[string]any{"parent_id": db})["pages"].([]any)
	if got := titles(map[string]any{"pages": rows}); fmt.Sprint(got) != "[Zebra Apple]" {
		t.Errorf("database rows order = %v, want [Zebra Apple] (by row_order)", got)
	}
	_ = rZ
	_ = rA
}

func TestListTrashFindsArchivedPages(t *testing.T) {
	dir := t.TempDir()
	c := newClient(t, testHost{folders: []string{dir}}, "sk-test")
	parent := c.tool("notes_create_page", map[string]any{"title": "Folder"})["id"].(string)
	sub := c.tool("notes_create_page", map[string]any{"title": "Sub", "parent_id": parent})["id"].(string)
	_ = sub
	old := c.tool("notes_create_page", map[string]any{"title": "Old Note"})["id"].(string)
	newer := c.tool("notes_create_page", map[string]any{"title": "Newer Note"})["id"].(string)
	c.tool("notes_create_page", map[string]any{"title": "Live Note"})

	c.tool("notes_trash_page", map[string]any{"page_id": old})
	time.Sleep(5 * time.Millisecond)
	c.tool("notes_trash_page", map[string]any{"page_id": newer})
	time.Sleep(5 * time.Millisecond)
	c.tool("notes_trash_page", map[string]any{"page_id": sub})
	c.tool("notes_trash_page", map[string]any{"page_id": parent})

	res := c.tool("notes_list_trash", map[string]any{"limit": 2})
	if int(asFloat(res["total"])) != 4 {
		t.Fatalf("trash total = %v, want 4", res["total"])
	}
	pages := res["pages"].([]any)
	if len(pages) != 2 {
		t.Fatalf("limit 2 gave %d pages", len(pages))
	}
	first := pages[0].(map[string]any)
	if first["id"] != parent || asFloat(first["child_count"]) != 1 {
		t.Errorf("newest trashed = %v", first)
	}
	second := pages[1].(map[string]any)
	if second["id"] != sub || second["parent_title"] != "Folder" {
		t.Errorf("second trashed = %v", second)
	}

	c.tool("notes_restore_page", map[string]any{"page_id": old})
	after := c.tool("notes_list_trash", map[string]any{})
	if int(asFloat(after["total"])) != 3 {
		t.Errorf("after restore total = %v, want 3", after["total"])
	}
}

func TestPageTreeWalksHierarchyRespectsDepthRowsAndLimit(t *testing.T) {
	dir := t.TempDir()
	c := newClient(t, testHost{folders: []string{dir}}, "sk-test")
	a := c.tool("notes_create_page", map[string]any{"title": "A"})["id"].(string)
	a1 := c.tool("notes_create_page", map[string]any{"title": "A1", "parent_id": a})["id"].(string)
	c.tool("notes_create_page", map[string]any{"title": "A1a", "parent_id": a1})
	db := c.tool("notes_create_database", map[string]any{"title": "DB"})["id"].(string)
	c.tool("notes_create_page", map[string]any{"title": "Row 1", "parent_id": db})
	gone := c.tool("notes_create_page", map[string]any{"title": "Gone"})["id"].(string)
	c.tool("notes_trash_page", map[string]any{"page_id": gone})

	// Default include_rows=false: DB node is shown with child_count=1 and truncated=true; Row 1 is excluded.
	tree := c.tool("notes_page_tree", map[string]any{})
	nodes := tree["nodes"].([]any)
	if len(nodes) != 4 {
		t.Fatalf("default tree nodes = %d, want 4: %v", len(nodes), nodes)
	}
	dbNode := nodes[3].(map[string]any)
	if dbNode["id"] != db || dbNode["kind"] != "database" || asFloat(dbNode["child_count"]) != 1 || dbNode["truncated"] != true {
		t.Errorf("db node = %v", dbNode)
	}

	// include_rows=true expands DB rows.
	withRows := c.tool("notes_page_tree", map[string]any{"include_rows": true})["nodes"].([]any)
	if len(withRows) != 5 || withRows[4].(map[string]any)["title"] != "Row 1" {
		t.Errorf("with rows = %v", withRows)
	}

	// parent_id=db expands rows directly even with include_rows=false.
	dbOnly := c.tool("notes_page_tree", map[string]any{"parent_id": db})["nodes"].([]any)
	if len(dbOnly) != 1 || dbOnly[0].(map[string]any)["title"] != "Row 1" {
		t.Errorf("db sub-tree = %v", dbOnly)
	}

	// depth=1 only shows top-level nodes and marks A truncated.
	shallow := c.tool("notes_page_tree", map[string]any{"depth": 1})["nodes"].([]any)
	if len(shallow) != 2 || shallow[0].(map[string]any)["truncated"] != true {
		t.Errorf("depth 1 = %v", shallow)
	}

	// limit=2 truncates traversal cleanly.
	limited := c.tool("notes_page_tree", map[string]any{"limit": 2})
	if limited["truncated"] != true || len(limited["nodes"].([]any)) != 2 {
		t.Errorf("limit 2 = %v", limited)
	}
}

func TestListTodosAggregatesByStatusAndSubtree(t *testing.T) {
	dir := t.TempDir()
	c := newClient(t, testHost{folders: []string{dir}}, "sk-test")
	c.tool("notes_create_page", map[string]any{"title": "Roadmap"})
	p1 := c.tool("notes_create_page", map[string]any{"title": "Work", "markdown": "- [ ] Open 1\n  - [ ] Nested [[Roadmap]]\n- [x] Done 1"})["id"].(string)
	c.tool("notes_create_page", map[string]any{"title": "Child", "parent_id": p1, "markdown": "- [ ] Sub open"})
	c.tool("notes_create_page", map[string]any{"title": "Other", "markdown": "- [ ] Outside\n- [x] Outside done"})
	gone := c.tool("notes_create_page", map[string]any{"title": "Trashed", "markdown": "- [ ] Ignore me"})["id"].(string)
	c.tool("notes_trash_page", map[string]any{"page_id": gone})

	open := c.tool("notes_list_todos", map[string]any{})
	if int(asFloat(open["total"])) != 4 {
		t.Fatalf("open total = %v, want 4", open["total"])
	}
	done := c.tool("notes_list_todos", map[string]any{"status": "done"})
	if int(asFloat(done["total"])) != 2 {
		t.Fatalf("done total = %v, want 2", done["total"])
	}
	scoped := c.tool("notes_list_todos", map[string]any{"parent_id": p1, "status": "all"})
	if int(asFloat(scoped["total"])) != 4 {
		t.Fatalf("scoped all total = %v, want 4", scoped["total"])
	}
	var texts []string
	for _, item := range scoped["todos"].([]any) {
		texts = append(texts, item.(map[string]any)["text"].(string))
	}
	joined := strings.Join(texts, "|")
	if !strings.Contains(joined, "Nested Roadmap") || !strings.Contains(joined, "Sub open") {
		t.Errorf("scoped texts = %v", texts)
	}
	if res := c.tool("notes_list_todos", map[string]any{"status": "bogus"}); res["_error"] == nil {
		t.Error("invalid status was accepted")
	}
}

func TestSearchFiltersAndSearchesAcrossVaults(t *testing.T) {
	v1, v2 := t.TempDir(), t.TempDir()
	c := newClient(t, testHost{folders: []string{v1, v2}}, "sk-test")

	proj := c.tool("notes_create_page", map[string]any{"title": "Project Hub", "markdown": "alpha overview"})["id"].(string)
	time.Sleep(3 * time.Millisecond)
	sub := c.tool("notes_create_page", map[string]any{"title": "Sub Doc", "parent_id": proj, "markdown": "alpha detail"})["id"].(string)
	db := c.tool("notes_create_database", map[string]any{"title": "Alpha DB"})["id"].(string)
	c.tool("notes_create_page", map[string]any{"title": "Alpha Row", "parent_id": db, "markdown": "alpha row"})
	c.tool("notes_create_page", map[string]any{"title": "Vault Two Page", "vault": v2, "markdown": "alpha second"})

	// Filter by parent_id (both match body, newest updated first).
	if got := titles(c.tool("notes_search", map[string]any{"query": "alpha", "parent_id": proj})); fmt.Sprint(got) != "[Sub Doc Project Hub]" {
		t.Errorf("parent_id search = %v", got)
	}
	// Filter by kind.
	if got := titles(c.tool("notes_search", map[string]any{"query": "alpha", "kind": "database"})); fmt.Sprint(got) != "[Alpha DB]" {
		t.Errorf("kind=database search = %v", got)
	}
	if got := titles(c.tool("notes_search", map[string]any{"query": "alpha", "kind": "row"})); fmt.Sprint(got) != "[Alpha Row]" {
		t.Errorf("kind=row search = %v", got)
	}
	if got := titles(c.tool("notes_search", map[string]any{"query": "alpha", "kind": "document"})); fmt.Sprint(got) != "[Sub Doc Project Hub]" {
		t.Errorf("kind=document search = %v", got)
	}

	// Filter by date bounds. Backdate Sub Doc.
	v, _ := vault.Open(v1)
	_, _ = v.Write(vault.Write{Action: "patch", Collection: vault.Pages, ID: sub, Patch: vault.Record{"updated_at": "2025-01-15T12:00:00.000Z"}})
	_ = v.Flush()
	_ = v.Close()
	if got := titles(c.tool("notes_search", map[string]any{"query": "alpha", "updated_before": "2025-02-01"})); fmt.Sprint(got) != "[Sub Doc]" {
		t.Errorf("updated_before search = %v", got)
	}
	if got := titles(c.tool("notes_search", map[string]any{"query": "alpha", "updated_after": "2026-01-01", "kind": "document"})); fmt.Sprint(got) != "[Project Hub]" {
		t.Errorf("updated_after search = %v", got)
	}

	// Cross-vault search with vault="*".
	cross := c.tool("notes_search", map[string]any{"query": "alpha", "vault": "*"})["pages"].([]any)
	vaultsHit := map[string]bool{}
	for _, p := range cross {
		vaultsHit[str(p.(map[string]any)["vault"])] = true
	}
	if len(vaultsHit) != 2 {
		t.Errorf("cross-vault search did not tag both vaults: %v", cross)
	}
	if res := c.tool("notes_search", map[string]any{"query": "alpha", "vault": "*", "parent_id": proj}); res["_error"] == nil {
		t.Error("parent_id combined with vault=* was accepted")
	}
	if res := c.tool("notes_search", map[string]any{"query": "alpha", "updated_after": "not-a-date"}); res["_error"] == nil {
		t.Error("invalid updated_after was accepted")
	}
}

func TestBatchEditPageIsAtomicAndSupportsTables(t *testing.T) {
	dir := t.TempDir()
	c := newClient(t, testHost{folders: []string{dir}}, "sk-test")
	id := c.tool("notes_create_page", map[string]any{
		"title":    "Report",
		"markdown": "# Summary\nplaceholder\n- [ ] send report\nremove me\n| Col | Val |\n| --- | --- |\n| A | 1 |",
	})["id"].(string)

	// Initial table was parsed into a real table block and round-trips.
	initial := c.fetch(map[string]any{"page_id": id})["markdown"].(string)
	if !strings.Contains(initial, "| Col | Val |\n| --- | --- |\n| A | 1 |") {
		t.Fatalf("initial markdown missing table: %q", initial)
	}

	// Failed second edit in batch leaves page untouched and creates no history version.
	bad := c.tool("notes_edit_page", map[string]any{
		"page_id": id,
		"edits": []map[string]any{
			{"old_text": "- [ ] send report", "new_text": "- [x] send report"},
			{"old_text": "does not exist", "new_text": "x"},
		},
	})
	if bad["_error"] == nil || !strings.Contains(fmt.Sprint(bad["_error"]), "edit 2:") {
		t.Fatalf("bad batch = %v", bad)
	}
	if got := c.fetch(map[string]any{"page_id": id})["markdown"]; got != initial {
		t.Fatalf("failed batch modified page:\n%s", got)
	}
	if vers := c.tool("notes_page_history", map[string]any{"page_id": id})["versions"].([]any); len(vers) != 0 {
		t.Fatalf("failed batch wrote %d history versions", len(vers))
	}

	// Successful batch applies multiple edits in order (including turning a line into a pipe table) with 1 checkpoint.
	res := c.tool("notes_edit_page", map[string]any{
		"page_id": id,
		"edits": []map[string]any{
			{"old_text": "placeholder", "new_text": "| K | V |\n| --- | --- |\n| x | 42 |"},
			{"old_text": "- [ ] send report", "new_text": "- [x] send report"},
			{"old_text": "remove me", "new_text": ""},
		},
	})
	if res["note"] != "applied 3 edits" {
		t.Fatalf("batch note = %v", res)
	}
	want := "# Summary\n| K | V |\n| --- | --- |\n| x | 42 |\n- [x] send report\n| Col | Val |\n| --- | --- |\n| A | 1 |"
	if got := c.fetch(map[string]any{"page_id": id})["markdown"]; got != want {
		t.Fatalf("after batch:\n%s\n--- want\n%s", got, want)
	}
	if vers := c.tool("notes_page_history", map[string]any{"page_id": id})["versions"].([]any); len(vers) != 1 {
		t.Errorf("batch wrote %d history versions, want 1", len(vers))
	}

	// Reject mixing single and batch params.
	if errRes := c.tool("notes_edit_page", map[string]any{
		"page_id":  id,
		"old_text": "x",
		"edits":    []map[string]any{{"old_text": "- [x] send report", "new_text": "- [ ] send report"}},
	}); errRes["_error"] == nil {
		t.Error("mixing old_text and edits was accepted")
	}
}

func TestReadAttachmentReadsDatabaseRowPropertyFiles(t *testing.T) {
	dir := t.TempDir()
	c := newClient(t, testHost{folders: []string{dir}}, "sk-test")
	db := c.tool("notes_create_database", map[string]any{
		"title":      "Gallery",
		"properties": []map[string]any{{"name": "Photo", "type": "image"}},
	})
	dbID := db["id"].(string)
	photoPropID := db["properties"].([]any)[0].(map[string]any)["id"].(string)
	rowID := c.tool("notes_create_page", map[string]any{"title": "Shot", "parent_id": dbID})["id"].(string)

	pic := smallPNG(t)
	filePath := "files/" + rowID + "/file_prop1/thumb.png"
	v, err := vault.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.WriteFile(filePath, pic); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Write(vault.Write{
		Action:     "patch",
		Collection: vault.Pages,
		ID:         rowID,
		Patch: vault.Record{
			"property_files": map[string]any{
				photoPropID: []any{
					map[string]any{"path": filePath, "owner": "local-user", "name": "thumb.png", "size": len(pic), "type": "image/png"},
				},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	_ = v.Flush()
	_ = v.Close()

	about := c.fetch(map[string]any{"page_id": rowID})["about"].(string)
	if !strings.Contains(about, "Attachments:\n- thumb.png (image/png, a picture, ") || !strings.Contains(about, "path: "+filePath) {
		t.Fatalf("row attachments not listed in fetch:\n%s", about)
	}
	got := c.toolResult("notes_read_attachment", map[string]any{"page_id": rowID, "path": filePath})
	content := got["content"].([]any)
	img := content[len(content)-1].(map[string]any)
	if img["type"] != "image" || img["mimeType"] != "image/png" {
		t.Fatalf("read row image = %v", img)
	}
	if raw, _ := base64.StdEncoding.DecodeString(img["data"].(string)); !bytes.Equal(raw, pic) {
		t.Fatal("row image bytes mismatch")
	}
}
