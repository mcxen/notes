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
