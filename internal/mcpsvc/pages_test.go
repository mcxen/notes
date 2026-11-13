package mcpsvc

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"notes/internal/vault"
)

func titles(res map[string]any) []string {
	var out []string
	pages, _ := res["pages"].([]any)
	for _, page := range pages {
		out = append(out, page.(map[string]any)["title"].(string))
	}
	return out
}

func TestALinkToAPageIsReadAndWrittenByItsTitle(t *testing.T) {
	dir := t.TempDir()
	c := newClient(t, testHost{folders: []string{dir}}, "sk-test")
	target := c.tool("notes_create_page", map[string]any{"title": "Target"})["id"].(string)
	source := c.tool("notes_create_page", map[string]any{"title": "Source", "markdown": "see [[target]] and [[Nowhere]], also [[" + target + "]]\n`[[Target]]` in code"})["id"].(string)

	// The link is a link in the vault, not the words written for it.
	raw, _ := json.Marshal(stored(t, dir, source))
	if n := strings.Count(string(raw), `"mention":{"kind":"page","page_id":"`+target+`"}`); n != 2 {
		t.Fatalf("%d links to the page are kept, want 2: %s", n, raw)
	}
	read := c.fetch(map[string]any{"page_id": source})
	if read["markdown"] != "see [[Target]] and [[Nowhere]], also [[Target]]\n`[[Target]]` in code" {
		t.Errorf("the body: %q", read["markdown"])
	}
	if about := read["about"].(string); !strings.Contains(about, "Links to:\n- Target ("+target+")") {
		t.Errorf("the page does not say what it links to: %q", about)
	}
	if about := c.fetch(map[string]any{"page_id": target})["about"].(string); !strings.Contains(about, "Linked from:\n- Source ("+source+")") {
		t.Errorf("the page does not say what links to it: %q", about)
	}

	// A link follows its page's title, and is found by it.
	c.tool("notes_update_page", map[string]any{"page_id": target, "title": "Renamed"})
	if md := c.fetch(map[string]any{"page_id": source})["markdown"].(string); !strings.HasPrefix(md, "see [[Renamed]] and") {
		t.Errorf("the link does not follow the page's title: %q", md)
	}
	if got := titles(c.tool("notes_search", map[string]any{"query": "see renamed"})); fmt.Sprint(got) != "[Source]" {
		t.Errorf("a link is not found by its page's title: %v", got)
	}

	// Two pages of one title: the name says neither, and stays words.
	c.tool("notes_create_page", map[string]any{"title": "Twin"})
	c.tool("notes_create_page", map[string]any{"title": "Twin"})
	twins := c.tool("notes_create_page", map[string]any{"title": "Twins", "markdown": "[[Twin]]"})["id"].(string)
	if raw, _ := json.Marshal(stored(t, dir, twins)); strings.Contains(string(raw), `"mention"`) {
		t.Errorf("a title two pages share was made a link: %s", raw)
	}
}

func TestSearchFindsEveryWordAndPutsTheBestFirst(t *testing.T) {
	dir := t.TempDir()
	c := newClient(t, testHost{folders: []string{dir}}, "sk-test")
	c.tool("notes_create_page", map[string]any{"title": "Notes", "markdown": "first line\nalpha beta gamma, with hello **brave** world"})
	c.tool("notes_create_page", map[string]any{"title": "Gamma alpha", "markdown": "nothing else"})
	c.tool("notes_create_page", map[string]any{"title": "Alpha only", "markdown": "no third letter"})

	// Words in any order, in the title or the text; the title's page first.
	if got := titles(c.tool("notes_search", map[string]any{"query": "alpha  GAMMA"})); fmt.Sprint(got) != "[Gamma alpha Notes]" {
		t.Errorf("alpha gamma: %v", got)
	}
	if got := titles(c.tool("notes_search", map[string]any{"query": "alpha delta"})); len(got) != 0 {
		t.Errorf("a page without one of the words is found: %v", got)
	}
	// Marks are not in the way of words that run across them.
	found := c.tool("notes_search", map[string]any{"query": "hello brave world"})
	if got := titles(found); fmt.Sprint(got) != "[Notes]" {
		t.Fatalf("hello brave world: %v", got)
	}
	hit := found["pages"].([]any)[0].(map[string]any)
	if hit["snippet"] != "alpha beta gamma, with hello brave world" {
		t.Errorf("the snippet is not the paragraph found, in plain words: %q", hit["snippet"])
	}
	if hit["updated_at"] == nil || hit["updated_at"] == "" {
		t.Errorf("a page found does not say when it was changed: %v", hit)
	}
	if got := titles(c.tool("notes_search", map[string]any{"query": "alpha", "limit": 2})); len(got) != 2 {
		t.Errorf("limit 2 gave %d pages", len(got))
	}
}

func TestRecentPagesAreNewestFirst(t *testing.T) {
	dir := t.TempDir()
	c := newClient(t, testHost{folders: []string{dir}}, "sk-test")
	first := c.tool("notes_create_page", map[string]any{"title": "First"})["id"].(string)
	c.tool("notes_create_page", map[string]any{"title": "Second"})
	gone := c.tool("notes_create_page", map[string]any{"title": "Gone"})["id"].(string)
	c.tool("notes_trash_page", map[string]any{"page_id": gone})
	// Stamps are to the millisecond: a change a moment later is the newest.
	for i := 0; i < 50; i++ {
		c.tool("notes_update_page", map[string]any{"page_id": first, "markdown": "more"})
		if got := titles(c.tool("notes_recent_pages", map[string]any{})); fmt.Sprint(got) == "[First Second]" {
			return
		}
	}
	t.Errorf("recent: %v", titles(c.tool("notes_recent_pages", map[string]any{})))
}

func TestAPartOfAPageIsChangedWhereItStands(t *testing.T) {
	dir := t.TempDir()
	c := newClient(t, testHost{folders: []string{dir}}, "sk-test")
	id := c.tool("notes_create_page", map[string]any{"title": "Trip", "markdown": "# Plan\n- flights\n  - window seat\n- [ ] book hotel\n1. first\n2. second\nA teh typo"})["id"].(string)
	// A picture makes the page one a whole new body would refuse.
	c.tool("notes_add_attachment", map[string]any{"page_id": id, "name": "map.png", "data": base64.StdEncoding.EncodeToString(smallPNG(t))})
	if res := c.tool("notes_update_page", map[string]any{"page_id": id, "markdown": "new", "mode": "replace"}); res["_error"] == nil {
		t.Fatalf("a page with a picture took a whole new body: %v", res)
	}
	before := stored(t, dir, id)
	body := func() string { return c.fetch(map[string]any{"page_id": id})["markdown"].(string) }
	edit := func(args map[string]any) map[string]any {
		args["page_id"] = id
		return c.tool("notes_edit_page", args)
	}

	// A word is put right, a box is ticked, a numbered line by its number.
	for _, change := range [][2]string{{"teh", "the"}, {"- [ ] book hotel", "- [x] book hotel"}, {"2. second", "2. **second**"}} {
		if res := edit(map[string]any{"old_text": change[0], "new_text": change[1]}); res["_error"] != nil {
			t.Fatalf("%q: %v", change[0], res)
		}
	}
	want := "# Plan\n- flights\n  - window seat\n- [x] book hotel\n1. first\n2. **second**\nA the typo\n![map.png](map.png)"
	if got := body(); got != want {
		t.Fatalf("after three changes:\n%s\n--- want\n%s", got, want)
	}
	// Each paragraph is the one it was: none made, none removed.
	after := stored(t, dir, id)
	if len(after) != len(before) {
		t.Errorf("%d paragraphs became %d", len(before), len(after))
	}
	for blockID := range before {
		if after[blockID] == nil {
			t.Errorf("the paragraph %s is gone", blockID)
		}
	}

	// More lines than one: the first in its place, those under it under it,
	// the rest after it and before what came next. Found by its nested line.
	if res := edit(map[string]any{"old_text": "  - window seat", "new_text": "  - aisle seat\n    - near the front\n  - extra bag"}); res["_error"] != nil {
		t.Fatal(res)
	}
	want = "# Plan\n- flights\n  - aisle seat\n    - near the front\n  - extra bag\n- [x] book hotel\n1. first\n2. **second**\nA the typo\n![map.png](map.png)"
	if got := body(); got != want {
		t.Fatalf("after adding lines:\n%s\n--- want\n%s", got, want)
	}

	// Emptied, a paragraph is removed; one with others under it is not.
	if res := edit(map[string]any{"old_text": "- extra bag", "new_text": ""}); res["note"] != "removed a paragraph" {
		t.Fatalf("removing: %v", res)
	}
	if res := edit(map[string]any{"old_text": "- flights", "new_text": ""}); res["_error"] == nil {
		t.Errorf("a paragraph was removed from over the ones under it: %v", res)
	}
	if strings.Contains(body(), "extra bag") || !strings.Contains(body(), "- flights\n  - aisle seat") {
		t.Errorf("after removing: %q", body())
	}

	// What is not there, or runs over two paragraphs, is said.
	for _, old := range []string{"no such words", "- flights\n  - aisle seat"} {
		if res := edit(map[string]any{"old_text": old, "new_text": "x"}); res["_error"] == nil {
			t.Errorf("%q was changed: %v", old, res)
		}
	}
	// The history holds the page as it was before these changes: one version
	// for the run of them.
	v, err := vault.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	if n := len(v.VersionsOf(id)); n != 1 {
		t.Errorf("the changes kept %d versions of the page, want one", n)
	}
}

func TestWordsThatAreThereSeveralTimesNeedTheirPlace(t *testing.T) {
	dir := t.TempDir()
	c := newClient(t, testHost{folders: []string{dir}}, "sk-test")
	id := c.tool("notes_create_page", map[string]any{"title": "Calls", "markdown": "- [ ] call\n- [ ] call\n- [ ] call"})["id"].(string)
	if res := c.tool("notes_edit_page", map[string]any{"page_id": id, "old_text": "- [ ] call", "new_text": "- [x] call"}); res["_error"] == nil {
		t.Fatalf("one of three was changed without saying which: %v", res)
	}
	if res := c.tool("notes_edit_page", map[string]any{"page_id": id, "old_text": "- [ ] call", "new_text": "- [x] call", "nth": 4}); res["_error"] == nil {
		t.Fatalf("the fourth of three was changed: %v", res)
	}
	if res := c.tool("notes_edit_page", map[string]any{"page_id": id, "old_text": "- [ ] call", "new_text": "- [x] call", "nth": 2}); res["_error"] != nil {
		t.Fatal(res)
	}
	if got := c.fetch(map[string]any{"page_id": id})["markdown"]; got != "- [ ] call\n- [x] call\n- [ ] call" {
		t.Fatalf("the second of three: %q", got)
	}
}

func TestAChangeKeepsWhatMarkdownCannotSay(t *testing.T) {
	dir := t.TempDir()
	c := newClient(t, testHost{folders: []string{dir}}, "sk-test")
	target := c.tool("notes_create_page", map[string]any{"title": "Target"})["id"].(string)
	id := c.tool("notes_create_page", map[string]any{"title": "Styled"})["id"].(string)
	words := func(text string) map[string]any { return map[string]any{"text": text} }
	mention := func(m map[string]any) map[string]any {
		return map[string]any{"embed": map[string]any{"mention": m}}
	}
	file := map[string]any{"path": "files/" + id + "/file_1/pic.png", "owner": "local-user", "name": "pic.png", "size": 10, "type": "image/png"}
	put(t, dir, id, map[string]any{"id": "blk_mix", "kind": "text", "order": 2048, "color": "red", "align": "center", "content": []any{
		words("see "), mention(map[string]any{"kind": "page", "page_id": target}), words(" on "),
		mention(map[string]any{"kind": "date", "date": "2026-11-13"}), words(" with "),
		mention(map[string]any{"kind": "image", "file": file, "width": 200}), words(" today")}})
	put(t, dir, id, map[string]any{"id": "blk_fold", "kind": "toggle", "order": 3072, "collapsed": true, "content": []any{words("Folded title")}})
	put(t, dir, id, map[string]any{"id": "blk_call", "kind": "callout", "order": 4096, "icon": "🔥", "content": []any{words("Careful here")}})
	put(t, dir, id, map[string]any{"id": "blk_soft", "kind": "text", "order": 5120, "content": []any{words("one line\nand a second")}})
	put(t, dir, id, map[string]any{"id": "blk_grid", "kind": "table", "order": 6144, "content": []any{}, "table": map[string]any{
		"columns": []any{map[string]any{"id": "c1"}},
		"rows":    map[string]any{"r1": map[string]any{"order": 1, "cells": map[string]any{"c1": []any{words("cell words")}}}}}})

	for _, change := range [][2]string{{"today", "now"}, {"Folded title", "Folded heading"}, {"Careful", "Mind"}, {"a second", "another"}} {
		if res := c.tool("notes_edit_page", map[string]any{"page_id": id, "old_text": change[0], "new_text": change[1]}); res["_error"] != nil {
			t.Fatalf("%q: %v", change[0], res)
		}
	}
	read := c.fetch(map[string]any{"page_id": id})
	want := "see [[Target]] on 2026-11-13 with ![pic.png](pic.png) now\n- Folded heading\n> Mind here\none line\nand another\n| cell words |\n| --- |"
	if read["markdown"] != want {
		t.Fatalf("the body:\n%s\n--- want\n%s", read["markdown"], want)
	}
	now := stored(t, dir, id)
	mix, _ := json.Marshal(now["blk_mix"])
	for _, part := range []string{`"color":"red"`, `"align":"center"`, `"kind":"page","page_id":"` + target, `"date":"2026-11-13"`, `"kind":"image"`, `"width":200`} {
		if !strings.Contains(string(mix), part) {
			t.Errorf("the changed paragraph lost %s: %s", part, mix)
		}
	}
	if now["blk_fold"]["kind"] != "toggle" || now["blk_fold"]["collapsed"] != true {
		t.Errorf("the toggle: %v", now["blk_fold"])
	}
	if now["blk_call"]["kind"] != "callout" || now["blk_call"]["icon"] != "🔥" {
		t.Errorf("the callout: %v", now["blk_call"])
	}
	if len(now) != 5 {
		t.Errorf("%d paragraphs, want the five there were: a line break inside one made more", len(now))
	}
	// A table's words are not changed here, and it says so.
	if res := c.tool("notes_edit_page", map[string]any{"page_id": id, "old_text": "cell words", "new_text": "x"}); res["_error"] == nil || !strings.Contains(res["_error"].(string), "table") {
		t.Errorf("a table's cell: %v", res)
	}
	// A link the change takes out is gone; one it writes is a link.
	c.tool("notes_edit_page", map[string]any{"page_id": id, "old_text": "see [[Target]] on", "new_text": "see nothing on"})
	c.tool("notes_edit_page", map[string]any{"page_id": id, "old_text": "Mind here", "new_text": "Mind [[Target]]"})
	now = stored(t, dir, id)
	if raw, _ := json.Marshal(now["blk_mix"]); strings.Contains(string(raw), `"kind":"page"`) || !strings.Contains(string(raw), `"kind":"image"`) {
		t.Errorf("after taking the link out: %s", raw)
	}
	if raw, _ := json.Marshal(now["blk_call"]); !strings.Contains(string(raw), `"kind":"page","page_id":"`+target) {
		t.Errorf("a link written in a change is not a link: %s", raw)
	}
}

func TestAPageIsMovedAndComesBackFromTheTrash(t *testing.T) {
	dir := t.TempDir()
	c := newClient(t, testHost{folders: []string{dir}}, "sk-test")
	a := c.tool("notes_create_page", map[string]any{"title": "A"})["id"].(string)
	b := c.tool("notes_create_page", map[string]any{"title": "B", "parent_id": a})["id"].(string)
	x := c.tool("notes_create_page", map[string]any{"title": "X"})["id"].(string)

	if res := c.tool("notes_move_page", map[string]any{"page_id": x, "parent_id": b}); res["_error"] != nil {
		t.Fatal(res)
	}
	if got := titles(c.tool("notes_list_pages", map[string]any{"parent_id": b})); fmt.Sprint(got) != "[X]" {
		t.Errorf("under B: %v", got)
	}
	if about := c.fetch(map[string]any{"page_id": x})["about"].(string); !strings.Contains(about, "Parent: B ("+b+")") {
		t.Errorf("X does not say where it is: %q", about)
	}
	// Never under itself or a page it holds.
	for _, under := range []string{a, b, x} {
		if res := c.tool("notes_move_page", map[string]any{"page_id": a, "parent_id": under}); res["_error"] == nil {
			t.Errorf("A was moved under %s: %v", under, res)
		}
	}
	if res := c.tool("notes_move_page", map[string]any{"page_id": x}); res["note"] != "moved to the top level" {
		t.Fatalf("to the top: %v", res)
	}
	if got := titles(c.tool("notes_list_pages", map[string]any{})); fmt.Sprint(got) != "[A X]" {
		t.Errorf("the top level: %v", got)
	}

	// In the Trash, a page is not a place to move to; restored, it is.
	c.tool("notes_trash_page", map[string]any{"page_id": a})
	if res := c.tool("notes_move_page", map[string]any{"page_id": x, "parent_id": b}); res["_error"] == nil {
		t.Errorf("X was moved into the Trash: %v", res)
	}
	if res := c.tool("notes_restore_page", map[string]any{"page_id": b}); !strings.Contains(fmt.Sprint(res["note"]), "page above it") {
		t.Errorf("restoring a page under a trashed one: %v", res)
	}
	if res := c.tool("notes_restore_page", map[string]any{"page_id": a}); res["note"] != "restored" {
		t.Fatalf("restoring: %v", res)
	}
	if got := titles(c.tool("notes_list_pages", map[string]any{})); fmt.Sprint(got) != "[A X]" {
		t.Errorf("after restoring: %v", got)
	}
	if c.fetch(map[string]any{"page_id": b})["in_trash"] != false {
		t.Errorf("B is still in the Trash")
	}
}

func TestAPageMadeAgainIsAddedTo(t *testing.T) {
	dir := t.TempDir()
	c := newClient(t, testHost{folders: []string{dir}}, "sk-test")
	daily := map[string]any{"title": "Daily 2026-11-13", "markdown": "- first item", "if_exists": "append"}
	first := c.tool("notes_create_page", daily)
	daily["markdown"] = "- second item"
	second := c.tool("notes_create_page", daily)
	if first["id"] != second["id"] || second["note"] != "added to the page that was already there" {
		t.Fatalf("made twice: %v / %v", first, second)
	}
	if got := c.fetch(map[string]any{"page_id": first["id"]})["markdown"]; got != "- first item\n- second item" {
		t.Errorf("the page: %q", got)
	}
	// The same title somewhere else is another page; without if_exists, too.
	parent := c.tool("notes_create_page", map[string]any{"title": "Parent"})["id"].(string)
	daily["parent_id"] = parent
	if other := c.tool("notes_create_page", daily); other["id"] == first["id"] {
		t.Errorf("a page under another parent was taken for the same: %v", other)
	}
	if again := c.tool("notes_create_page", map[string]any{"title": "Daily 2026-11-13"}); again["id"] == first["id"] {
		t.Errorf("without if_exists the page was not made: %v", again)
	}
}

func TestNothingToChangeIsRefused(t *testing.T) {
	dir := t.TempDir()
	c := newClient(t, testHost{folders: []string{dir}}, "sk-test")
	id := c.tool("notes_create_page", map[string]any{"title": "Still"})["id"].(string)
	if res := c.tool("notes_update_page", map[string]any{"page_id": id}); res["_error"] == nil {
		t.Fatalf("an update of nothing was taken: %v", res)
	}
	if res := c.tool("notes_edit_page", map[string]any{"page_id": id, "old_text": "  ", "new_text": "x"}); res["_error"] == nil {
		t.Fatalf("an edit of nothing was taken: %v", res)
	}
}

func TestReadOnlyRefusesTheNewChanges(t *testing.T) {
	dir := t.TempDir()
	host := testHost{folders: []string{dir}}
	c := newClient(t, host, "sk-test")
	id := c.tool("notes_create_page", map[string]any{"title": "Keep", "markdown": "text"})["id"].(string)
	host.readOnly = true
	c2 := newClient(t, host, "sk-test")
	for name, args := range map[string]map[string]any{
		"notes_edit_page":    {"page_id": id, "old_text": "text", "new_text": "changed"},
		"notes_move_page":    {"page_id": id},
		"notes_restore_page": {"page_id": id},
	} {
		if res := c2.tool(name, args); res["_error"] == nil {
			t.Errorf("a read-only server took %s: %v", name, res)
		}
	}
	if got := titles(c2.tool("notes_recent_pages", map[string]any{})); fmt.Sprint(got) != "[Keep]" {
		t.Errorf("reading in read-only mode: %v", got)
	}
}
