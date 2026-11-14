package notion

import (
	"fmt"
	"html"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"notes/internal/markdown"
)

// A page's body is Notion's Markdown: the usual lines, indented four spaces a
// level, and HTML for what Markdown has no word for (callouts, toggles,
// tables). It is read line by line into paragraphs.

// converter is what reading one page's body needs from the import.
type converter struct {
	// dir is the folder of the page's file in the export: links are relative to it.
	dir string
	// page resolves a link to a page of the export to that page's id.
	page func(target string) (id string, ok bool)
	// file resolves a link to a file of the export that is not a page, and
	// stores it with the page: the file's record.
	file func(target string) (record map[string]any, ok bool)
	// newID makes a paragraph's id.
	newID func() string
	warn  func(string)
}

type builder struct {
	c      *converter
	blocks []markdown.Block
	counts map[string]int
	// after are paragraphs for files linked among words, put after the one
	// that links them.
	after []markdown.Block
}

func newBuilder(c *converter) *builder {
	return &builder{c: c, counts: map[string]int{}}
}

func (b *builder) add(parent string, blk markdown.Block) string {
	blk.ID = b.c.newID()
	blk.ParentID = parent
	b.counts[parent]++
	blk.Order = float64(1024 * b.counts[parent])
	b.blocks = append(b.blocks, blk)
	return blk.ID
}

// convertBody reads a page's Markdown into paragraphs.
func convertBody(src string, c *converter) []markdown.Block {
	b := newBuilder(c)
	b.parse(splitLines(src), "")
	return b.blocks
}

func splitLines(src string) []string {
	src = strings.ReplaceAll(strings.ReplaceAll(src, "\r\n", "\n"), "\r", "\n")
	src = strings.ReplaceAll(src, "\t", "    ")
	return strings.Split(src, "\n")
}

var (
	reHeading  = regexp.MustCompile(`^(#{1,6}) +(.*)$`)
	reTodo     = regexp.MustCompile(`^[-*+] \[( |x|X)\] ?(.*)$`)
	reBullet   = regexp.MustCompile(`^[-*+](?: +(.*))?$`)
	reNumbered = regexp.MustCompile(`^\d+[.)](?: +(.*))?$`)
	reDivider  = regexp.MustCompile(`^(?:---+|\*\*\*+|___+)$`)
	reQuote    = regexp.MustCompile(`^> ?(.*)$`)
	reSepRow   = regexp.MustCompile(`^\|?\s*:?-{1,}:?\s*(\|\s*:?-{1,}:?\s*)*\|?$`)
	reImage    = regexp.MustCompile(`!\[([^\]]*)\]\(((?:[^()\s]|\([^)]*\))+)(?:\s+"[^"]*")?\)`)
	reLink     = regexp.MustCompile(`\[((?:[^\[\]]|\[[^\]]*\])*)\]\(((?:[^()\s]|\([^)]*\))+)(?:\s+"[^"]*")?\)`)
	reTag      = regexp.MustCompile(`</?[A-Za-z][^>]*>`)
)

// parse reads lines into paragraphs under parent. Nesting follows the
// indentation of the lines that are items of a list.
func (b *builder) parse(lines []string, parent string) {
	type level struct {
		indent int
		id     string
	}
	var stack []level
	owner := func(indent int) string {
		for len(stack) > 0 && stack[len(stack)-1].indent >= indent {
			stack = stack[:len(stack)-1]
		}
		if len(stack) > 0 {
			return stack[len(stack)-1].id
		}
		return parent
	}
	for i := 0; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], " ")
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		body := strings.TrimLeft(line, " ")
		under := owner(indent)
		push := func(id string) { stack = append(stack, level{indent, id}) }

		switch {
		case strings.HasPrefix(body, "```"):
			language := strings.TrimSpace(strings.TrimPrefix(body, "```"))
			var code []string
			for i+1 < len(lines) && strings.TrimSpace(lines[i+1]) != "```" {
				i++
				code = append(code, strings.TrimPrefix(lines[i], strings.Repeat(" ", min(indent, len(lines[i])-len(strings.TrimLeft(lines[i], " "))))))
			}
			if i+1 < len(lines) {
				i++
			}
			b.add(under, markdown.Block{Kind: "code", Language: language, Runs: []markdown.Run{{Text: strings.Join(code, "\n")}}})
		case strings.HasPrefix(body, "$$"):
			text := strings.TrimPrefix(body, "$$")
			if end := strings.LastIndex(text, "$$"); end >= 0 {
				text = text[:end]
			} else {
				var formula []string
				if strings.TrimSpace(text) != "" {
					formula = append(formula, strings.TrimSpace(text))
				}
				for i+1 < len(lines) {
					i++
					if end := strings.Index(lines[i], "$$"); end >= 0 {
						if rest := strings.TrimSpace(lines[i][:end]); rest != "" {
							formula = append(formula, rest)
						}
						break
					}
					formula = append(formula, strings.TrimSpace(lines[i]))
				}
				text = strings.Join(formula, "\n")
			}
			b.add(under, markdown.Block{Kind: "math", Runs: []markdown.Run{{Text: strings.TrimSpace(text)}}})
		case strings.HasPrefix(body, "<aside"):
			inner, next := untilClosed(lines, i, "aside")
			i = next
			b.callout(under, inner)
		case strings.HasPrefix(body, "<details"):
			inner, next := untilClosed(lines, i, "details")
			i = next
			b.toggle(under, inner)
		case strings.HasPrefix(body, "<table"):
			inner, next := untilClosed(lines, i, "table")
			i = next
			b.htmlTable(under, inner)
		case strings.HasPrefix(body, "|") && i+1 < len(lines) && reSepRow.MatchString(strings.TrimSpace(lines[i+1])) && strings.Contains(lines[i+1], "-"):
			rows := []string{body}
			i++ // the separator
			for i+1 < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i+1]), "|") {
				i++
				rows = append(rows, strings.TrimSpace(lines[i]))
			}
			b.pipeTable(under, rows)
		case reDivider.MatchString(body):
			b.add(under, markdown.Block{Kind: "divider"})
		case reHeading.MatchString(body):
			m := reHeading.FindStringSubmatch(body)
			b.add(under, markdown.Block{Kind: "heading" + strconv.Itoa(min(len(m[1]), 3)), Runs: b.runs(m[2])})
		case reTodo.MatchString(body):
			m := reTodo.FindStringSubmatch(body)
			push(b.add(under, markdown.Block{Kind: "todo", Checked: m[1] != " ", Runs: b.runs(m[2])}))
		case reBullet.MatchString(body):
			m := reBullet.FindStringSubmatch(body)
			push(b.add(under, markdown.Block{Kind: "bullet", Runs: b.runs(m[1])}))
		case reNumbered.MatchString(body):
			m := reNumbered.FindStringSubmatch(body)
			push(b.add(under, markdown.Block{Kind: "numbered", Runs: b.runs(m[1])}))
		case reQuote.MatchString(body):
			text := reQuote.FindStringSubmatch(body)[1]
			for i+1 < len(lines) && reQuote.MatchString(strings.TrimSpace(lines[i+1])) {
				i++
				text += "\n" + reQuote.FindStringSubmatch(strings.TrimSpace(lines[i]))[1]
			}
			b.add(under, markdown.Block{Kind: "quote", Runs: b.runs(text)})
		default:
			b.paragraph(under, body)
		}
		for _, blk := range b.after {
			b.add(under, blk)
		}
		b.after = nil
	}
}

// untilClosed collects the lines from the one at i, which opens the HTML
// element tag, to the line that closes it, nested elements of the same tag
// included. It returns what is between, and the index of the closing line.
func untilClosed(lines []string, i int, tag string) ([]string, int) {
	open := regexp.MustCompile(`(?i)<` + tag + `[\s>]|<` + tag + `$`)
	closer := "</" + tag + ">"
	depth := 0
	var inner []string
	for j := i; j < len(lines); j++ {
		line := lines[j]
		depth += len(open.FindAllString(line, -1))
		depth -= strings.Count(strings.ToLower(line), closer)
		if j == i {
			// Content on the opening line itself, after its tag.
			if k := strings.Index(line, ">"); k >= 0 && strings.TrimSpace(line[k+1:]) != "" {
				rest := line[k+1:]
				if depth <= 0 {
					rest = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(rest), closer))
				}
				if rest != "" {
					inner = append(inner, rest)
				}
			}
			if depth <= 0 {
				return inner, j
			}
			continue
		}
		if depth <= 0 {
			if k := strings.Index(strings.ToLower(line), closer); k > 0 && strings.TrimSpace(line[:k]) != "" {
				inner = append(inner, line[:k])
			}
			return inner, j
		}
		inner = append(inner, line)
	}
	return inner, len(lines) - 1
}

// callout reads an <aside>: an emoji on a line of its own is its icon, the
// first paragraph its words, the rest what it holds.
func (b *builder) callout(parent string, inner []string) {
	icon := ""
	var rest []string
	for _, line := range inner {
		if strings.TrimSpace(line) == "" && len(rest) == 0 {
			continue
		}
		rest = append(rest, line)
	}
	if len(rest) > 0 {
		first := strings.TrimSpace(rest[0])
		switch {
		case isEmoji(first):
			icon, rest = first, rest[1:]
		case strings.HasPrefix(first, "<img"):
			// An icon of Notion's own, drawn as a picture: the default stays.
			rest = rest[1:]
		}
	}
	for len(rest) > 0 && strings.TrimSpace(rest[0]) == "" {
		rest = rest[1:]
	}
	var words string
	if len(rest) > 0 {
		words, rest = strings.TrimSpace(rest[0]), rest[1:]
	}
	id := b.add(parent, markdown.Block{Kind: "callout", Icon: icon, Runs: b.runs(words)})
	b.parse(rest, id)
}

// isEmoji says whether a short line is an emoji or a few of them: no letters or digits.
func isEmoji(s string) bool {
	if s == "" || len([]rune(s)) > 12 {
		return false
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || (r < 0x80 && !unicode.IsSpace(r)) {
			return false
		}
	}
	return true
}

var reSummary = regexp.MustCompile(`(?is)<summary>(.*?)</summary>`)

// toggle reads a <details>: its <summary> is the toggle's words, the rest
// what it holds.
func (b *builder) toggle(parent string, inner []string) {
	text := strings.Join(inner, "\n")
	title := ""
	if m := reSummary.FindStringSubmatchIndex(text); m != nil {
		title = strings.TrimSpace(reTag.ReplaceAllString(text[m[2]:m[3]], ""))
		text = text[:m[0]] + text[m[1]:]
	}
	id := b.add(parent, markdown.Block{Kind: "toggle", Runs: b.runs(title)})
	b.parse(splitLines(text), id)
}

// paragraph reads a line of words: a picture or a file alone on its line
// becomes a paragraph of its own.
func (b *builder) paragraph(parent, body string) {
	if m := reImage.FindStringSubmatch(body); m != nil && strings.TrimSpace(reImage.ReplaceAllString(body, "")) == "" {
		runs := b.runs(body)
		b.add(parent, markdown.Block{Kind: "text", Runs: runs})
		return
	}
	if m := reLink.FindStringSubmatch(body); m != nil && strings.TrimSpace(strings.Replace(body, m[0], "", 1)) == "" {
		target := localTarget(m[2])
		if _, isPage := b.c.page(target); !isPage && target != "" {
			if rec, ok := b.c.file(target); ok {
				b.add(parent, markdown.Block{Kind: "file", FileRef: rec, Runs: b.runs(m[1])})
				return
			}
		}
	}
	b.add(parent, markdown.Block{Kind: "text", Runs: b.runs(body)})
}

// localTarget is a link's address as a path of the export: decoded, without
// a fragment; "" for an address on the web.
func localTarget(target string) string {
	target = strings.TrimSpace(target)
	target = strings.TrimSuffix(strings.TrimPrefix(target, "<"), ">")
	if target == "" || strings.HasPrefix(target, "#") || regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:`).MatchString(target) {
		return ""
	}
	if k := strings.IndexAny(target, "#?"); k >= 0 {
		target = target[:k]
	}
	if decoded, err := url.PathUnescape(target); err == nil {
		target = decoded
	}
	return target
}

// resolveLocal is a link's path joined to the folder of the page it is on.
func (c *converter) resolveLocal(target string) string {
	if strings.HasPrefix(target, "/") {
		return clean(strings.TrimPrefix(target, "/"))
	}
	return clean(path.Join(c.dir, target))
}

// runs reads the words of a line: marks, links between pages, pictures.
func (b *builder) runs(s string) []markdown.Run {
	s = strings.ReplaceAll(s, "<br>", " ")
	s = strings.ReplaceAll(s, "<br/>", " ")
	s = strings.ReplaceAll(s, "<br />", " ")
	var out []markdown.Run
	push := func(text string) {
		for _, r := range markWords(text) {
			out = append(out, r)
		}
	}
	for len(s) > 0 {
		img := reImage.FindStringSubmatchIndex(s)
		lnk := reLink.FindStringSubmatchIndex(s)
		var at []int
		isImage := false
		switch {
		case img != nil && (lnk == nil || img[0] <= lnk[0]):
			at, isImage = img, true
		case lnk != nil:
			at = lnk
		}
		if at == nil {
			push(s)
			break
		}
		push(s[:at[0]])
		label, target := s[at[2]:at[3]], s[at[4]:at[5]]
		if isImage {
			out = append(out, b.picture(label, target)...)
		} else {
			out = append(out, b.link(label, target)...)
		}
		s = s[at[1]:]
	}
	return out
}

// picture is a picture of the export drawn among the words, or the words of
// one on the web.
func (b *builder) picture(alt, target string) []markdown.Run {
	local := localTarget(target)
	if local == "" {
		if strings.HasPrefix(target, "http") {
			text := alt
			if text == "" {
				text = target
			}
			return []markdown.Run{{Text: text, Link: strings.Trim(target, "<>")}}
		}
		return nil
	}
	rec, ok := b.c.file(local)
	if !ok {
		b.c.warn(fmt.Sprintf("a picture was not found in the export: %s", local))
		if alt != "" {
			return []markdown.Run{{Text: alt}}
		}
		return nil
	}
	return []markdown.Run{{Mention: map[string]any{"kind": "image", "file": rec}}}
}

// link is a link to a page of the export, a file of it, or an address.
func (b *builder) link(label, target string) []markdown.Run {
	local := localTarget(target)
	if local == "" {
		address := strings.Trim(strings.TrimSpace(target), "<>")
		// A link to another Notion page: its id may name a page of the export.
		if isNotionAddress(address) {
			if id, ok := b.c.page(address); ok {
				return []markdown.Run{{Mention: map[string]any{"kind": "page", "page_id": id}}}
			}
		}
		if strings.HasPrefix(address, "#") {
			return markWords(label)
		}
		runs := markWords(label)
		for i := range runs {
			runs[i].Link = address
		}
		return runs
	}
	if id, ok := b.c.page(local); ok {
		return []markdown.Run{{Mention: map[string]any{"kind": "page", "page_id": id}}}
	}
	if rec, ok := b.c.file(local); ok {
		// A file among words is kept on the page as a paragraph after it: the
		// words stay words, as the export wrote them.
		b.after = append(b.after, markdown.Block{Kind: "file", FileRef: rec, Runs: markWords(label)})
		return markWords(label)
	}
	b.c.warn("a link was not found in the export: " + local)
	return markWords(label)
}

func isNotionAddress(address string) bool {
	return regexp.MustCompile(`(?i)^https?://(?:www\.)?notion\.(?:so|site|com)/`).MatchString(address)
}

// escapes are the characters Notion writes after a backslash.
var escapes = strings.NewReplacer(`\*`, "", `\_`, "", `\[`, "", `\]`, "", "\\`", "", `\~`, "", `\\`, "", `\<`, "", `\>`, "", `\#`, "", `\|`, "", `\(`, "", `\)`, "")

var unescapes = strings.NewReplacer("", "*", "", "_", "", "[", "", "]", "", "`", "", "~", "", `\`, "", "<", "", ">", "", "#", "", "|", "", "(", "", ")")

var reUnderline = regexp.MustCompile(`(?is)<u>(.*?)</u>`)

// markWords reads **bold**, *italic*, ~~strike~~ and `code` in words that have
// no links, and the underline Notion writes as <u>.
func markWords(s string) []markdown.Run {
	if s == "" {
		return nil
	}
	var out []markdown.Run
	for len(s) > 0 {
		m := reUnderline.FindStringSubmatchIndex(s)
		if m == nil {
			out = append(out, readMarks(s, false)...)
			break
		}
		out = append(out, readMarks(s[:m[0]], false)...)
		out = append(out, readMarks(s[m[2]:m[3]], true)...)
		s = s[m[1]:]
	}
	return out
}

func readMarks(s string, underline bool) []markdown.Run {
	s = reTag.ReplaceAllString(s, "")
	s = html.UnescapeString(escapes.Replace(s))
	runs := markdown.Inline(s)
	for i := range runs {
		runs[i].Text = unescapes.Replace(runs[i].Text)
		runs[i].Underline = runs[i].Underline || underline
	}
	return runs
}
