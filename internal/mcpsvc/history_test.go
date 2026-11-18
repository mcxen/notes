package mcpsvc

import (
	"fmt"
	"strings"
	"testing"
)

// text is what a tool that answers in words says.
func (c *client) text(name string, args map[string]any) string {
	c.t.Helper()
	res := c.call("tools/call", map[string]any{"name": name, "arguments": args}, "Bearer "+c.key)
	result, _ := res["result"].(map[string]any)
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		c.t.Fatalf("%s: no answer: %v", name, res)
	}
	return content[0].(map[string]any)["text"].(string)
}

func TestAnAssistantCanReadAndRestoreAnEarlierVersion(t *testing.T) {
	dir := t.TempDir()
	c := newClient(t, testHost{folders: []string{dir}}, "sk-test")
	id := c.tool("notes_create_page", map[string]any{"title": "Plan", "markdown": "first line\n\n- [ ] ship it\n\nsee [[Plan]]"})["id"].(string)
	original := c.fetch(map[string]any{"page_id": id})["markdown"]
	c.tool("notes_update_page", map[string]any{"page_id": id, "title": "Plan v2", "mode": "replace", "markdown": "second line"})

	history := c.tool("notes_page_history", map[string]any{"page_id": id})
	versions, _ := history["versions"].([]any)
	if len(versions) != 1 {
		t.Fatalf("versions: %v", history)
	}
	first := versions[0].(map[string]any)
	if first["title"] != "Plan" || first["by"] != "Notes MCP" || first["summary"] != "Changed by an assistant" {
		t.Errorf("the version: %v", first)
	}

	read := c.text("notes_read_version", map[string]any{"page_id": id, "version_id": first["id"]})
	if !strings.HasPrefix(read, "Title: Plan\n") || !strings.Contains(read, "\n---\nfirst line") || !strings.Contains(read, "- [ ] ship it") {
		t.Errorf("the version's text: %q", read)
	}

	// Restoring brings the words back, and keeps the page as it was.
	got := c.tool("notes_restore_version", map[string]any{"page_id": id, "version_id": first["id"]})
	if got["title"] != "Plan" || !strings.Contains(got["note"].(string), "restored") {
		t.Fatalf("restore: %v", got)
	}
	body := c.fetch(map[string]any{"page_id": id})
	if body["markdown"] != original {
		t.Errorf("the restored body: %q", body["markdown"])
	}
	if !strings.Contains(body["about"].(string), "Title: Plan\n") {
		t.Errorf("the restored title: %q", body["about"])
	}
	after := c.tool("notes_page_history", map[string]any{"page_id": id})["versions"].([]any)
	if len(after) != 2 {
		t.Fatalf("a restore keeps the page as it was: %v", after)
	}
	newest := after[0].(map[string]any)
	if newest["title"] != "Plan v2" {
		t.Errorf("the page before the restore: %v", newest)
	}
	// ... so the restore can be taken back.
	c.tool("notes_restore_version", map[string]any{"page_id": id, "version_id": newest["id"]})
	if md := c.fetch(map[string]any{"page_id": id})["markdown"]; md != "second line" {
		t.Errorf("after taking the restore back: %q", md)
	}

	// What it is not.
	if res := c.tool("notes_read_version", map[string]any{"page_id": id, "version_id": "nope"}); res != nil && res["_error"] == nil {
		t.Errorf("a version that is not there was read: %v", res)
	}
	if res := c.tool("notes_page_history", map[string]any{"page_id": "nope"}); res["_error"] == nil {
		t.Errorf("a page that is not there has a history: %v", res)
	}
}

func TestARestoreIsRefusedForAPageInTheTrashAndWhenReadOnly(t *testing.T) {
	dir := t.TempDir()
	c := newClient(t, testHost{folders: []string{dir}}, "sk-test")
	id := c.tool("notes_create_page", map[string]any{"title": "Gone", "markdown": "words"})["id"].(string)
	c.tool("notes_update_page", map[string]any{"page_id": id, "mode": "replace", "markdown": "other words"})
	version := c.tool("notes_page_history", map[string]any{"page_id": id})["versions"].([]any)[0].(map[string]any)["id"]
	c.tool("notes_trash_page", map[string]any{"page_id": id})
	if res := c.tool("notes_restore_version", map[string]any{"page_id": id, "version_id": version}); res["_error"] == nil {
		t.Errorf("a page in the Trash was restored: %v", res)
	}
	ro := newClient(t, testHost{folders: []string{dir}, readOnly: true}, "sk-test")
	if res := ro.tool("notes_restore_version", map[string]any{"page_id": id, "version_id": version}); res["_error"] == nil {
		t.Errorf("a read-only server restored a version: %v", res)
	}
	if len(ro.tool("notes_page_history", map[string]any{"page_id": id})["versions"].([]any)) == 0 {
		t.Error("a read-only server hides the history")
	}
}

func TestAVersionsParagraphGoesBackToTheFormTheVaultKeeps(t *testing.T) {
	mention := map[string]any{"kind": "page", "page_id": "p1"}
	record := map[string]any{"id": "b1", "kind": "table", "order": 1.0,
		"content": []any{map[string]any{"text": "a", "marks": map[string]any{"bold": true}}, map[string]any{"embed": map[string]any{"mention": mention}}},
		"table": map[string]any{"rows": map[string]any{"r1": map[string]any{"cells": map[string]any{
			"c1": []any{map[string]any{"embed": map[string]any{"mention": mention}}, map[string]any{"text": "x"}}}}}}}
	got := storedBlock("b1", webBlock("b1", record))
	if fmt.Sprint(got) != fmt.Sprint(record) {
		t.Errorf("a paragraph does not come back as it was:\n got %v\nwant %v", got, record)
	}
}
