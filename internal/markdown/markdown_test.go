package markdown

import (
	"fmt"
	"testing"
)

func ids() func() string {
	n := 0
	return func() string { n++; return fmt.Sprintf("block_%03d", n) }
}

func noName(map[string]any) string { return "" }

func TestMarkdownReadsTheShapesTheEditorWrites(t *testing.T) {
	src := `# Plan
## Goals
- one
  - nested
1. first
2. second
- [ ] open
- [x] done
> quoted
> second line
---
` + "```go\nfmt.Println(1)\n```" + `
plain paragraph`
	blocks := FromMarkdown(src, ids())
	kinds := []string{}
	for _, b := range blocks {
		kinds = append(kinds, b.Kind)
	}
	want := []string{"heading1", "heading2", "bullet", "bullet", "numbered", "numbered", "todo", "todo", "quote", "divider", "code", "text"}
	if fmt.Sprint(kinds) != fmt.Sprint(want) {
		t.Fatalf("kinds = %v, want %v", kinds, want)
	}
	// The nested bullet is under the bullet above it; the rest are at the top.
	if blocks[3].ParentID != blocks[2].ID {
		t.Errorf("nested bullet parent = %q, want %q", blocks[3].ParentID, blocks[2].ID)
	}
	if blocks[6].Checked || !blocks[7].Checked {
		t.Errorf("to-do boxes: %v %v", blocks[6].Checked, blocks[7].Checked)
	}
	if blocks[8].Runs[0].Text != "quoted\nsecond line" {
		t.Errorf("quote = %q", blocks[8].Runs[0].Text)
	}
	if blocks[10].Language != "go" || blocks[10].Runs[0].Text != "fmt.Println(1)" {
		t.Errorf("code = %q %q", blocks[10].Language, blocks[10].Runs[0].Text)
	}
}

func TestMarksAreRead(t *testing.T) {
	runs := inline("a **bold** and *ital* and `code` and ~~gone~~ and [site](https://x.test)")
	var bold, ital, code, strike, link bool
	for _, r := range runs {
		bold = bold || (r.Text == "bold" && r.Bold)
		ital = ital || (r.Text == "ital" && r.Italic)
		code = code || (r.Text == "code" && r.Code)
		strike = strike || (r.Text == "gone" && r.Strike)
		link = link || (r.Text == "site" && r.Link == "https://x.test")
	}
	if !bold || !ital || !code || !strike || !link {
		t.Fatalf("marks not read: %+v", runs)
	}
}

func TestWritingAndReadingBackKeepsTheText(t *testing.T) {
	src := "# Title\n- a\n  - b\n1. one\n2. two\n- [x] yes\n> q\n---\nend"
	blocks := FromMarkdown(src, ids())
	out := ToMarkdown(blocks, noName)
	if out != src {
		t.Fatalf("round trip:\n%s\n---\n%s", out, src)
	}
}

func TestNumberedRestartsAfterAnotherLine(t *testing.T) {
	blocks := FromMarkdown("1. a\n2. b\n- x\n1. c", ids())
	out := ToMarkdown(blocks, noName)
	if out != "1. a\n2. b\n- x\n1. c" {
		t.Fatalf("numbers: %q", out)
	}
}

func TestRecordsRoundTrip(t *testing.T) {
	b := Block{ID: "b1", Kind: "todo", Checked: true, Order: 1024, Runs: []Run{{Text: "word", Bold: true}, {Text: " more"}}}
	back := FromRecord("b1", b.ToRecord())
	if back.Kind != "todo" || !back.Checked || len(back.Runs) != 2 || !back.Runs[0].Bold || back.Runs[1].Text != " more" {
		t.Fatalf("record round trip: %+v", back)
	}
}

func TestMentionsAreNamed(t *testing.T) {
	page := map[string]any{"kind": "page", "page_id": "p1"}
	blocks := []Block{{ID: "x", Kind: "text", Order: 1024, Runs: []Run{{Text: "see "}, {Mention: page}}}}
	out := ToMarkdown(blocks, func(m map[string]any) string { return "Other" })
	if out != "see [[Other]]" {
		t.Fatalf("mention: %q", out)
	}
	if !blocks[0].HasMentionsOrFiles() {
		t.Error("a page link is something Markdown cannot carry back")
	}
}

func TestWordsThatOnlyLookLikeAListStayWords(t *testing.T) {
	src := "**Summary:** the plan\n*note* in italics\n3.14 is pi\n2026.11.13 log\n-5 degrees\n+1 from me"
	blocks := FromMarkdown(src, ids())
	for _, b := range blocks {
		if b.Kind != "text" {
			t.Errorf("%q was read as %s, want text", plainText(b.Runs), b.Kind)
		}
	}
	if out := ToMarkdown(blocks, noName); out != src {
		t.Fatalf("round trip:\n%s\n---\n%s", out, src)
	}
	// A mark alone on its line is still an empty item, as the editor writes one.
	kinds := []string{}
	for _, b := range FromMarkdown("-\n1.\n- item\n2) item", ids()) {
		kinds = append(kinds, b.Kind)
	}
	if want := []string{"bullet", "numbered", "bullet", "numbered"}; fmt.Sprint(kinds) != fmt.Sprint(want) {
		t.Fatalf("kinds = %v, want %v", kinds, want)
	}
}

func TestANameInDoubleBracketsIsALinkToItsPage(t *testing.T) {
	resolve := func(name string) map[string]any {
		if name == "Plan *B*" {
			return map[string]any{"kind": "page", "page_id": "p1"}
		}
		return nil
	}
	blocks := LinkPages(FromMarkdown("**see [[Plan *B*]] and [[Nothing]]** then `[[Plan *B*]]`\n```\n[[Plan *B*]]\n```", ids()), resolve)
	first := blocks[0].Runs
	if len(first) != 5 || first[0].Text != "see " || !first[0].Bold || first[1].Mention["page_id"] != "p1" ||
		first[2].Text != " and [[Nothing]]" || !first[2].Bold || first[3].Text != " then " || !first[4].Code || first[4].Text != "[[Plan *B*]]" {
		t.Fatalf("runs: %+v", first)
	}
	if blocks[1].Kind != "code" || len(blocks[1].Runs) != 1 || blocks[1].Runs[0].Mention != nil {
		t.Fatalf("code: %+v", blocks[1])
	}
	name := func(map[string]any) string { return "Plan *B*" }
	if out := ToMarkdown(blocks[:1], name); out != "**see **[[Plan *B*]]** and [[Nothing]]** then `[[Plan *B*]]`" {
		t.Fatalf("written: %q", out)
	}
	if words := Words(blocks[0], name); words != "see Plan *B* and [[Nothing]] then [[Plan *B*]]" {
		t.Fatalf("words: %q", words)
	}
}

func TestTheOutlineIsInReadingOrder(t *testing.T) {
	blocks := FromMarkdown("# Title\n- a\n  - b\n1. one\n2. two", ids())
	var got []string
	for _, entry := range Outline(blocks, noName) {
		got = append(got, fmt.Sprintf("%d:%s", entry.Depth, entry.Text()))
	}
	if want := []string{"0:# Title", "0:- a", "1:- b", "0:1. one", "0:2. two"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("outline = %v, want %v", got, want)
	}
}
