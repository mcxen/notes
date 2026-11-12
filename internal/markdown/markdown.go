// Package markdown writes a page's paragraphs as Markdown and reads Markdown
// back into paragraphs, in the shapes the editor writes: headings, bullet,
// numbered and to-do lines, quotes, code fences, dividers, nesting by two
// spaces, and the marks bold, italic, strike, code and links. Paragraphs are
// the records a vault keeps (text runs in their stored form).
package markdown

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Run is a stretch of words with its marks, or a mention (a link to a page, a
// date or a picture) that the Markdown writes as its name.
type Run struct {
	Text      string
	Bold      bool
	Italic    bool
	Underline bool
	Strike    bool
	Code      bool
	Link      string
	Color     string
	// Mention is the stored mention, when the run is one.
	Mention map[string]any
}

// Block is a paragraph as the vault keeps it.
type Block struct {
	ID       string
	Kind     string
	ParentID string
	Order    float64
	Checked  bool
	Language string
	Icon     string
	Runs     []Run
	// File is the name of a file or picture paragraph; URL is an embed's link.
	File string
	URL  string
	// Table is kept as its records, for a table paragraph.
	Table map[string]any
}

// Namer says the words of a mention: a page's name, a date, a file's name.
type Namer func(mention map[string]any) string

// FromRecord reads a paragraph record (its "content" in stored form).
func FromRecord(id string, value map[string]any) Block {
	b := Block{ID: id, Kind: str(value["kind"]), ParentID: str(value["parent_id"]), Order: num(value["order"]),
		Checked: value["checked"] == true, Language: str(value["language"]), Icon: str(value["icon"]),
		Runs: runsFromStored(value["content"]), URL: embedURL(value["embed"])}
	if file, ok := value["file"].(map[string]any); ok {
		b.File = str(file["name"])
	}
	if table, ok := value["table"].(map[string]any); ok {
		b.Table = table
	}
	return b
}

// ToRecord writes a paragraph as the record the vault keeps.
func (b Block) ToRecord() map[string]any {
	r := map[string]any{"id": b.ID, "kind": b.Kind, "order": b.Order, "content": runsToStored(b.Runs)}
	if b.ParentID != "" {
		r["parent_id"] = b.ParentID
	}
	if b.Kind == "todo" {
		r["checked"] = b.Checked
	}
	if b.Kind == "code" && b.Language != "" {
		r["language"] = b.Language
	}
	if b.Kind == "callout" && b.Icon != "" {
		r["icon"] = b.Icon
	}
	if b.Table != nil {
		r["table"] = b.Table
	}
	if b.URL != "" {
		r["embed"] = map[string]any{"url": b.URL}
	}
	return r
}

// HasMentionsOrFiles says whether a paragraph holds something the Markdown
// cannot carry back: a picture, a file, a table, a card or a link to a page.
func (b Block) HasMentionsOrFiles() bool {
	if b.Kind == "file" || b.Kind == "table" || b.Kind == "embed" {
		return true
	}
	for _, run := range b.Runs {
		if run.Mention != nil {
			return true
		}
	}
	return false
}

// ToMarkdown writes the paragraphs of a page in reading order: each under its
// parent, indented two spaces per level, numbered as the editor numbers them.
func ToMarkdown(blocks []Block, name Namer) string {
	byParent := map[string][]Block{}
	for _, b := range blocks {
		byParent[b.ParentID] = append(byParent[b.ParentID], b)
	}
	for parent := range byParent {
		list := byParent[parent]
		sort.SliceStable(list, func(i, j int) bool { return list[i].Order < list[j].Order })
	}
	var lines []string
	var visit func(parent string, depth int)
	visit = func(parent string, depth int) {
		number := 0
		for _, b := range byParent[parent] {
			if b.Kind == "numbered" {
				number++
			} else {
				number = 0
			}
			indent := strings.Repeat("  ", depth)
			for _, line := range blockLines(b, number, name) {
				if line == "" {
					lines = append(lines, "")
				} else {
					lines = append(lines, indent+line)
				}
			}
			visit(b.ID, depth+1)
		}
	}
	visit("", 0)
	return strings.Join(lines, "\n")
}

func blockLines(b Block, number int, name Namer) []string {
	text := inlineMarkdown(b.Runs, name)
	switch b.Kind {
	case "heading1":
		return []string{"# " + text}
	case "heading2":
		return []string{"## " + text}
	case "heading3":
		return []string{"### " + text}
	case "todo":
		box := " "
		if b.Checked {
			box = "x"
		}
		return []string{"- [" + box + "] " + text}
	case "bullet", "toggle":
		return []string{"- " + text}
	case "numbered":
		return []string{strconv.Itoa(number) + ". " + text}
	case "quote", "callout":
		var out []string
		for _, line := range strings.Split(text, "\n") {
			out = append(out, "> "+line)
		}
		return out
	case "divider":
		return []string{"---"}
	case "code":
		out := []string{"```" + b.Language}
		out = append(out, strings.Split(plainText(b.Runs), "\n")...)
		return append(out, "```")
	case "file":
		return []string{"[" + b.File + "](" + b.File + ")"}
	case "embed":
		return []string{b.URL}
	case "table":
		return tableLines(b.Table, name)
	case "page":
		return nil
	default:
		if text == "" {
			return []string{""}
		}
		return strings.Split(text, "\n")
	}
}

// tableLines writes a simple table: its rows in order, the first as a header.
func tableLines(table map[string]any, name Namer) []string {
	columns, _ := table["columns"].([]any)
	rows, _ := table["rows"].(map[string]any)
	if len(columns) == 0 || len(rows) == 0 {
		return nil
	}
	type row struct {
		order float64
		cells map[string]any
	}
	var list []row
	for _, value := range rows {
		r, _ := value.(map[string]any)
		cells, _ := r["cells"].(map[string]any)
		list = append(list, row{order: num(r["order"]), cells: cells})
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].order < list[j].order })
	var out []string
	for i, r := range list {
		var cells []string
		for _, c := range columns {
			col, _ := c.(map[string]any)
			cells = append(cells, strings.ReplaceAll(inlineMarkdown(runsFromStored(r.cells[str(col["id"])]), name), "|", `\|`))
		}
		out = append(out, "| "+strings.Join(cells, " | ")+" |")
		if i == 0 {
			seps := make([]string, len(columns))
			for k := range seps {
				seps[k] = "---"
			}
			out = append(out, "| "+strings.Join(seps, " | ")+" |")
		}
	}
	return out
}

func inlineMarkdown(runs []Run, name Namer) string {
	var b strings.Builder
	for _, run := range runs {
		if run.Mention != nil {
			switch str(run.Mention["kind"]) {
			case "page":
				b.WriteString("[[" + name(run.Mention) + "]]")
			case "image":
				file := str(run.Mention["file"].(map[string]any)["name"])
				b.WriteString("![" + file + "](" + file + ")")
			default:
				b.WriteString(name(run.Mention))
			}
			continue
		}
		text := run.Text
		switch {
		case run.Code:
			text = "`" + text + "`"
		case run.Link != "":
			text = "[" + text + "](" + run.Link + ")"
		}
		if run.Bold {
			text = "**" + text + "**"
		}
		if run.Italic {
			text = "*" + text + "*"
		}
		if run.Strike {
			text = "~~" + text + "~~"
		}
		b.WriteString(text)
	}
	return b.String()
}

func plainText(runs []Run) string {
	var b strings.Builder
	for _, run := range runs {
		b.WriteString(run.Text)
	}
	return b.String()
}

// FromMarkdown reads Markdown into paragraphs. Nesting follows the indentation
// (two spaces a level), each paragraph gets an id from newID, and the order of
// the paragraphs under one parent is the order they are written in.
func FromMarkdown(src string, newID func() string) []Block {
	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	type open struct {
		depth int
		id    string
	}
	var stack []open
	var out []Block
	counts := map[string]int{}
	add := func(depth int, b Block) {
		for len(stack) > 0 && stack[len(stack)-1].depth >= depth {
			stack = stack[:len(stack)-1]
		}
		if len(stack) > 0 {
			b.ParentID = stack[len(stack)-1].id
		}
		counts[b.ParentID]++
		b.Order = float64(1024 * counts[b.ParentID])
		b.ID = newID()
		out = append(out, b)
		stack = append(stack, open{depth: depth, id: b.ID})
	}
	for i := 0; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], " \t\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		depth := indent / 2
		body := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(body, "```") {
			language := strings.TrimSpace(strings.TrimPrefix(body, "```"))
			var code []string
			for i+1 < len(lines) && strings.TrimSpace(lines[i+1]) != "```" {
				i++
				code = append(code, strings.TrimPrefix(lines[i], strings.Repeat("  ", depth)))
			}
			if i+1 < len(lines) {
				i++ // the closing fence
			}
			add(depth, Block{Kind: "code", Language: language, Runs: []Run{{Text: strings.Join(code, "\n")}}})
			continue
		}
		if reDivider.MatchString(body) {
			add(depth, Block{Kind: "divider"})
			continue
		}
		if m := reHeading.FindStringSubmatch(body); m != nil {
			add(depth, Block{Kind: "heading" + strconv.Itoa(len(m[1])), Runs: inline(m[2])})
			continue
		}
		if m := reTodo.FindStringSubmatch(body); m != nil {
			add(depth, Block{Kind: "todo", Checked: m[1] == "x" || m[1] == "X", Runs: inline(m[2])})
			continue
		}
		if m := reBullet.FindStringSubmatch(body); m != nil {
			add(depth, Block{Kind: "bullet", Runs: inline(m[1])})
			continue
		}
		if m := reNumbered.FindStringSubmatch(body); m != nil {
			add(depth, Block{Kind: "numbered", Runs: inline(m[1])})
			continue
		}
		if m := reQuote.FindStringSubmatch(body); m != nil {
			text := m[1]
			for i+1 < len(lines) && reQuote.MatchString(strings.TrimSpace(lines[i+1])) {
				i++
				text += "\n" + reQuote.FindStringSubmatch(strings.TrimSpace(lines[i]))[1]
			}
			add(depth, Block{Kind: "quote", Runs: inline(text)})
			continue
		}
		add(depth, Block{Kind: "text", Runs: inline(body)})
	}
	return out
}

var (
	reHeading  = regexp.MustCompile(`^(#{1,3}) (.*)$`)
	reTodo     = regexp.MustCompile(`^[-*+] \[( |x|X)\] ?(.*)$`)
	reBullet   = regexp.MustCompile(`^[-*+] ?(.*)$`)
	reNumbered = regexp.MustCompile(`^\d+[.)] ?(.*)$`)
	reDivider  = regexp.MustCompile(`^(---|\*\*\*|___)$`)
	reQuote    = regexp.MustCompile(`^> ?(.*)$`)
)

// inline reads the marks of a line: `code`, **bold**, *italic*, ~~strike~~ and
// [text](link). Marks do not nest; a run inside a bold or italic keeps its mark.
func inline(s string) []Run {
	var runs []Run
	var plain strings.Builder
	flush := func() {
		if plain.Len() > 0 {
			runs = append(runs, Run{Text: plain.String()})
			plain.Reset()
		}
	}
	for i := 0; i < len(s); {
		rest := s[i:]
		switch {
		case strings.HasPrefix(rest, "`"):
			if end := strings.Index(rest[1:], "`"); end >= 0 {
				flush()
				runs = append(runs, Run{Text: rest[1 : 1+end], Code: true})
				i += end + 2
				continue
			}
		case strings.HasPrefix(rest, "**"):
			if end := strings.Index(rest[2:], "**"); end > 0 {
				flush()
				for _, r := range inline(rest[2 : 2+end]) {
					r.Bold = true
					runs = append(runs, r)
				}
				i += end + 4
				continue
			}
		case strings.HasPrefix(rest, "~~"):
			if end := strings.Index(rest[2:], "~~"); end > 0 {
				flush()
				for _, r := range inline(rest[2 : 2+end]) {
					r.Strike = true
					runs = append(runs, r)
				}
				i += end + 4
				continue
			}
		case strings.HasPrefix(rest, "*") && !strings.HasPrefix(rest, "**"):
			if end := strings.Index(rest[1:], "*"); end > 0 {
				flush()
				for _, r := range inline(rest[1 : 1+end]) {
					r.Italic = true
					runs = append(runs, r)
				}
				i += end + 2
				continue
			}
		case strings.HasPrefix(rest, "["):
			if m := reLink.FindStringSubmatch(rest); m != nil {
				flush()
				runs = append(runs, Run{Text: m[1], Link: m[2]})
				i += len(m[0])
				continue
			}
		}
		plain.WriteByte(s[i])
		i++
	}
	flush()
	return runs
}

var reLink = regexp.MustCompile(`^\[([^\]]+)\]\(([^)\s]+)\)`)

// runsFromStored reads the runs of a record's rich text, as the vault keeps them.
func runsFromStored(value any) []Run {
	list, _ := value.([]any)
	var runs []Run
	for _, item := range list {
		m, _ := item.(map[string]any)
		if m == nil {
			continue
		}
		marks, _ := m["marks"].(map[string]any)
		run := Run{Bold: marks["bold"] == true, Italic: marks["italic"] == true, Underline: marks["underline"] == true,
			Strike: marks["strike"] == true, Code: marks["code"] == true, Link: str(marks["link"]), Color: str(marks["color"])}
		if embed, ok := m["embed"].(map[string]any); ok {
			run.Mention, _ = embed["mention"].(map[string]any)
			runs = append(runs, run)
			continue
		}
		run.Text = str(m["text"])
		runs = append(runs, run)
	}
	return runs
}

// runsToStored writes runs as the vault keeps them.
func runsToStored(runs []Run) []any {
	out := make([]any, 0, len(runs))
	for _, run := range runs {
		var marks map[string]any
		set := func(key string) {
			if marks == nil {
				marks = map[string]any{}
			}
			marks[key] = true
		}
		if run.Bold {
			set("bold")
		}
		if run.Italic {
			set("italic")
		}
		if run.Underline {
			set("underline")
		}
		if run.Strike {
			set("strike")
		}
		if run.Code {
			set("code")
		}
		if run.Link != "" {
			if marks == nil {
				marks = map[string]any{}
			}
			marks["link"] = run.Link
		}
		if run.Color != "" {
			if marks == nil {
				marks = map[string]any{}
			}
			marks["color"] = run.Color
		}
		item := map[string]any{}
		if run.Mention != nil {
			item["embed"] = map[string]any{"mention": run.Mention}
		} else {
			item["text"] = run.Text
		}
		if marks != nil {
			item["marks"] = marks
		}
		out = append(out, item)
	}
	return out
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

// num reads a number as the vault keeps it: decoded from JSON, it is a
// json.Number.
func num(v any) float64 {
	switch n := v.(type) {
	case json.Number:
		f, _ := n.Float64()
		return f
	case float64:
		return n
	case int:
		return float64(n)
	case string:
		f, _ := strconv.ParseFloat(n, 64)
		return f
	}
	return 0
}

func embedURL(v any) string {
	m, _ := v.(map[string]any)
	return str(m["url"])
}

// Describe is a short text for debugging a paragraph.
func Describe(b Block) string {
	return fmt.Sprintf("%s %q", b.Kind, plainText(b.Runs))
}

// StoredText is the plain words of a record's rich text (a page's title, say).
func StoredText(value any) string {
	return plainText(runsFromStored(value))
}

// StoredRuns writes plain text as the stored rich text of a record.
func StoredRuns(text string) []any {
	return runsToStored([]Run{{Text: text}})
}

// WebRuns converts stored rich text to the form the web interface keeps in
// memory: a mention is a character with the mention beside it.
func WebRuns(value any) []any {
	list, _ := value.([]any)
	out := make([]any, 0, len(list))
	for _, item := range list {
		m, _ := item.(map[string]any)
		if embed, ok := m["embed"].(map[string]any); ok {
			run := map[string]any{"text": "⁣", "mention": embed["mention"]}
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
