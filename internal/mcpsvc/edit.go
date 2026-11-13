package mcpsvc

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"notes/internal/markdown"
	"notes/internal/vault"
)

// Changing a part of a page. notes_edit_page finds old_text in the Markdown of
// one paragraph, as notes_fetch writes it, and puts new_text in its place. The
// paragraph stays the paragraph it was: its id, its place, its colour and what
// is under it; the pictures, dates and links to pages among its words that
// the change leaves alone are still what they were. Only that paragraph is
// written, so a page with pictures or tables, which a whole new body would
// lose, can still have a word put right.

type editIn struct {
	Vault   string `json:"vault,omitempty" jsonschema:"the vault's name or folder; omit for the default"`
	PageID  string `json:"page_id" jsonschema:"the page's id"`
	OldText string `json:"old_text" jsonschema:"the text to change, as notes_fetch shows it: within one paragraph (a line, a code block or a quote)"`
	NewText string `json:"new_text" jsonschema:"what it becomes, as Markdown: more lines than one add paragraphs after it; empty takes it out"`
	Nth     int    `json:"nth,omitempty" jsonschema:"which place to change, counting from 1, when old_text is on the page several times"`
}

// kept is a mention among a paragraph's words, and what the Markdown writes
// for it.
type kept struct {
	text string
	run  markdown.Run
}

// markBase is the first of the characters that stand for the mentions of a
// paragraph while its changed Markdown is read: private ones, which no page
// has among its words.
const markBase = 0xF0000

func isMark(r rune) bool { return r >= markBase && r < markBase+0xFFFD }

// fixed says a paragraph that is not words: a table, a file or a card, which
// Markdown cannot write back.
func fixed(b markdown.Block) bool {
	return b.Kind == "table" || b.Kind == "file" || b.Kind == "embed"
}

// unindent takes the indentation of old's first line off every line of old
// and fresh: a paragraph is found by its own words, however deep it is nested
// in what notes_fetch showed.
func unindent(old, fresh string) (string, string) {
	old = strings.ReplaceAll(old, "\r\n", "\n")
	fresh = strings.ReplaceAll(fresh, "\r\n", "\n")
	depth := len(old) - len(strings.TrimLeft(old, " \t"))
	if depth == 0 {
		return old, fresh
	}
	strip := func(text string) string {
		lines := strings.Split(text, "\n")
		for i, line := range lines {
			lead := len(line) - len(strings.TrimLeft(line, " \t"))
			lines[i] = line[min(lead, depth):]
		}
		return strings.Join(lines, "\n")
	}
	return strip(old), strip(fresh)
}

func (s *server) edit(_ context.Context, _ *mcp.CallToolRequest, in editIn) (*mcp.CallToolResult, pageOut, error) {
	var out pageOut
	if s.host.ReadOnly() {
		return nil, out, errors.New(readOnlyError)
	}
	old, fresh := unindent(in.OldText, in.NewText)
	if strings.TrimSpace(old) == "" {
		return nil, out, errors.New("old_text is empty: to add to a page, use notes_update_page")
	}
	if in.Nth < 0 {
		return nil, out, errors.New("nth counts from 1")
	}
	err := s.within(in.Vault, func(v *vault.Vault, access *Access) error {
		lib := load(v)
		page, ok := lib.pages[in.PageID]
		if !ok {
			return fmt.Errorf("no page %q in this vault", in.PageID)
		}
		if lib.trashed(in.PageID) {
			return errors.New("the page is in the Trash: restore it first (notes_restore_page)")
		}
		blocks := lib.blocks[in.PageID]

		// Every place old_text stands, in reading order.
		type place struct {
			block markdown.Block
			text  string
			at    int
		}
		var places []place
		held := 0
		for _, entry := range markdown.Outline(blocks, lib.name) {
			text := entry.Text()
			for from := 0; ; {
				i := strings.Index(text[from:], old)
				if i < 0 {
					break
				}
				if fixed(entry.Block) {
					held++
				} else {
					places = append(places, place{block: entry.Block, text: text, at: from + i})
				}
				from += i + len(old)
			}
		}
		switch {
		case len(places) == 0 && held > 0:
			return errors.New("old_text is in a table, a file or a card, which is changed in Notes")
		case len(places) == 0 && strings.Contains(old, "\n"):
			return errors.New("old_text is not within one paragraph: give the text of one line (or of one code block or quote) as notes_fetch shows it, and change one paragraph at a time")
		case len(places) == 0:
			return errors.New("old_text is not on the page: give it exactly as notes_fetch shows it")
		case in.Nth > len(places):
			return fmt.Errorf("old_text is on the page %d times: nth is 1 to %d", len(places), len(places))
		case len(places) > 1 && in.Nth == 0:
			return fmt.Errorf("old_text is on the page %d times: give more of the paragraph, or pass nth (1 to %d) for the place to change", len(places), len(places))
		}
		at := places[max(in.Nth, 1)-1]
		b := at.block
		changed := at.text[:at.at] + fresh + at.text[at.at+len(old):]

		// The mentions the change leaves alone are read back as what they were.
		var mentions []kept
		for _, run := range b.Runs {
			if run.Mention != nil {
				mentions = append(mentions, kept{text: markdown.MentionText(run.Mention, lib.name), run: run})
			}
		}
		parsed := markdown.FromMarkdown(protect(changed, mentions), func() string { return newID("block") })
		unprotect(parsed, mentions)
		parsed = markdown.LinkPages(parsed, lib.resolve)
		parsed = rejoin(b, at.text, fresh, parsed)

		now := stamp()
		var writes []vault.Write
		cp, err := checkpoint(v, in.PageID, page, lib.stored[in.PageID], now)
		if err != nil {
			return err
		}
		writes = append(writes, cp...)
		recordID := vault.BlockRecordID(in.PageID, b.ID)
		if len(parsed) == 0 {
			for _, other := range blocks {
				if other.ParentID == b.ID {
					return errors.New("the paragraph has others under it: change those first, or leave it some words")
				}
			}
			writes = append(writes, vault.Write{Action: "delete", Collection: vault.Blocks, ID: recordID})
			out.Note = "removed a paragraph"
		} else {
			first := parsed[0]
			kind := first.Kind
			// A callout is written as a quote and a toggle as a bullet: read
			// back as one, it is still what it was.
			if (b.Kind == "callout" && kind == "quote") || (b.Kind == "toggle" && kind == "bullet") {
				kind = b.Kind
			}
			patch := map[string]any{"kind": kind, "content": first.ToRecord()["content"], "checked": nil, "language": nil}
			if kind == "todo" {
				patch["checked"] = first.Checked
			}
			if kind == "code" && first.Language != "" {
				patch["language"] = first.Language
			}
			writes = append(writes, vault.Write{Action: "patch", Collection: vault.Blocks, ID: recordID, Patch: patch})

			// The lines after the first: those written under it go under the
			// paragraph, after what it holds; the others follow it, before
			// the paragraph that came next.
			next, after := 0.0, 0
			for _, other := range blocks {
				if other.ParentID == b.ParentID && other.Order > b.Order && (next == 0 || other.Order < next) {
					next = other.Order
				}
			}
			for _, added := range parsed[1:] {
				if added.ParentID == "" {
					after++
				}
			}
			under, followed := lastOrder(blocks, b.ID), 0
			for _, added := range parsed[1:] {
				switch added.ParentID {
				case "":
					followed++
					added.ParentID = b.ParentID
					if next > 0 {
						added.Order = b.Order + (next-b.Order)*float64(followed)/float64(after+1)
					} else {
						added.Order = b.Order + 1024*float64(followed)
					}
				case first.ID:
					under += 1024
					added.ParentID = b.ID
					added.Order = under
				}
				writes = append(writes, vault.Write{Action: "put", Collection: vault.Blocks,
					ID: vault.BlockRecordID(in.PageID, added.ID), Value: added.ToRecord()})
			}
			out.Note = "changed a paragraph"
			if n := len(parsed) - 1; n > 0 {
				out.Note = fmt.Sprintf("changed a paragraph and added %d after it", n)
			}
		}
		writes = append(writes, vault.Write{Action: "patch", Collection: vault.Pages, ID: in.PageID,
			Patch: map[string]any{"updated_at": now, "updated_by": authorName}})
		changes, err := apply(v, writes)
		if err != nil {
			return err
		}
		access.Changed(vault.Changes{Changes: changes})
		out.ID, out.Title = in.PageID, lib.title(in.PageID)
		return nil
	})
	return nil, out, err
}

// protect writes a character of its own in place of each mention still in the
// changed Markdown, in the order they stood: read as Markdown, a picture would
// be a link and a date only words.
func protect(changed string, mentions []kept) string {
	var out strings.Builder
	rest := changed
	for i, mention := range mentions {
		if mention.text == "" {
			continue
		}
		at := strings.Index(rest, mention.text)
		if at < 0 {
			continue // taken out, or changed into something else
		}
		out.WriteString(rest[:at])
		out.WriteRune(rune(markBase + i))
		rest = rest[at+len(mention.text):]
	}
	out.WriteString(rest)
	return out.String()
}

// unprotect puts the mentions back where their characters stand. In code,
// which holds only letters, a mention is the words written for it.
func unprotect(blocks []markdown.Block, mentions []kept) {
	of := func(r rune) (kept, bool) {
		if i := int(r - markBase); isMark(r) && i < len(mentions) {
			return mentions[i], true
		}
		return kept{}, false
	}
	for i := range blocks {
		var runs []markdown.Run
		for _, run := range blocks[i].Runs {
			if run.Mention != nil || !strings.ContainsFunc(run.Text, isMark) {
				runs = append(runs, run)
				continue
			}
			var words strings.Builder
			flush := func() {
				if words.Len() > 0 {
					part := run
					part.Text = words.String()
					runs = append(runs, part)
					words.Reset()
				}
			}
			for _, r := range run.Text {
				mention, ok := of(r)
				switch {
				case !ok:
					words.WriteRune(r)
				case run.Code || blocks[i].Kind == "code":
					words.WriteString(mention.text)
				default:
					flush()
					runs = append(runs, mention.run)
				}
			}
			flush()
		}
		blocks[i].Runs = runs
	}
}

// rejoin makes one paragraph again of a paragraph whose words run over several
// lines (a line break typed inside it), when the change added no line: read
// as Markdown, each of its lines is a paragraph.
func rejoin(b markdown.Block, before, fresh string, parsed []markdown.Block) []markdown.Block {
	lines := strings.Count(before, "\n")
	if lines == 0 || b.Kind == "code" || b.Kind == "quote" || b.Kind == "callout" || strings.Contains(fresh, "\n") || len(parsed) != lines+1 {
		return parsed
	}
	for _, line := range parsed[1:] {
		if line.Kind != "text" || line.ParentID != "" {
			return parsed
		}
	}
	first := parsed[0]
	for _, line := range parsed[1:] {
		first.Runs = append(first.Runs, markdown.Run{Text: "\n"})
		first.Runs = append(first.Runs, line.Runs...)
	}
	return []markdown.Block{first}
}
