package notion

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"notes/internal/markdown"
	"notes/internal/vault"
)

const (
	idHome   = "1a2b3c4d5e6f47a8b9c0d1e2f3a4b5c6"
	idSub    = "2b3c4d5e6f7a48b9c0d1e2f3a4b5c6d7"
	idDB     = "3c4d5e6f7a8b49c0d1e2f3a4b5c6d7e8"
	idRowA   = "4d5e6f7a8b9c40d1e2f3a4b5c6d7e8f9"
	idRowB   = "5e6f7a8b9c0d41e2f3a4b5c6d7e8f9a0"
	idGone   = "7a8b9c0d1e2f43a4b5c6d7e8f9a0b1c2"
	idEmpty  = "6f7a8b9c0d1e42f3a4b5c6d7e8f9a0b1"
	pngBytes = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
)

// export is a Notion Markdown & CSV export, as the files of a zip.
func export() map[string]string {
	png, _ := base64.StdEncoding.DecodeString(pngBytes)
	return map[string]string{
		"Home " + idHome + ".md": `# Home

Welcome to **Notes** and *Notion*. See [Sub page](Home%20` + idHome + `/Sub%20page%20` + idSub + `.md) and [the tasks](Home%20` + idHome + `/Tasks%20` + idDB + `.csv).

- [ ] write it
- [x] done
    - nested item
1. first
2. second

> a quote
> over two lines

<aside>
💡

A callout with words

More under it

</aside>

<details>
<summary>A toggle</summary>

Hidden words

</details>

| Name | Qty |
| --- | --- |
| Apple | 3 |
| Pear | 5 |

$$
E = mc^2
$$

![a picture](Home%20` + idHome + `/pic.png)

[the contract](Home%20` + idHome + `/contract.pdf)

[a stranger](https://www.notion.so/Elsewhere-` + idGone + `)

[outside](https://example.com/x)

` + "```go\nfmt.Println(1)\n```\n",
		"Home " + idHome + "/Sub page " + idSub + ".md":                        "# Sub page\n\nBack to [Home](../Home%20" + idHome + ".md).\n\n<table>\n<tr><th>A</th><th>B</th></tr>\n<tr><td>1</td><td>x<br>y</td></tr>\n</table>\n",
		"Home " + idHome + "/pic.png":                                          string(png),
		"Home " + idHome + "/contract.pdf":                                     "%PDF-1.4 fake",
		"Home " + idHome + "/Tasks " + idDB + ".csv":                           "Name,Status,Due,Notes\nWrite code,Doing,\"January 5, 2026\",first\nWrite tests,Done,\"January 6, 2026\",\nShip it,Doing,\"January 9, 2026 10:30 AM\",\"a, b\"\nExtra row,Done,,\n",
		"Home " + idHome + "/Tasks " + idDB + "_all.csv":                       "Name,Status,Due,Notes,Hidden\nWrite code,Doing,,,\n",
		"Home " + idHome + "/Tasks " + idDB + "/Write code " + idRowA + ".md":  "# Write code\n\nStatus: Doing\nDue: January 5, 2026\n\nThe body of the first task.\n",
		"Home " + idHome + "/Tasks " + idDB + "/Write tests " + idRowB + ".md": "# Write tests\n\nStatus: Done\n\nTests body.\n",
		"Loose folder/Inside " + idEmpty + ".md":                               "# Inside\n\nA page in a folder that has no page.\n",
	}
}

func zipOf(t *testing.T, files map[string]string, wrapper string) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		if wrapper != "" {
			name = wrapper + "/" + name
		}
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(content))
	}
	zw.Close()
	path := filepath.Join(t.TempDir(), "export.zip")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func openVault(t *testing.T) *vault.Vault {
	t.Helper()
	v, err := vault.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { v.Close() })
	return v
}

// library is what the vault holds, by page title.
type library struct {
	pages  map[string]vault.Record
	byID   map[string]vault.Record
	blocks map[string][]markdown.Block
}

func load(v *vault.Vault) library {
	lib := library{pages: map[string]vault.Record{}, byID: map[string]vault.Record{}, blocks: map[string][]markdown.Block{}}
	for _, row := range v.Snapshot().Records {
		switch row.Collection {
		case vault.Pages:
			lib.byID[row.ID] = row.Value
			lib.pages[markdown.StoredText(row.Value["title_content"])] = row.Value
		case vault.Blocks:
			page, block, _ := vault.ParseBlockRecordID(row.ID)
			lib.blocks[page] = append(lib.blocks[page], markdown.FromRecord(block, row.Value))
		}
	}
	return lib
}

func (l library) text(title string) []markdown.Block {
	id, _ := l.pages[title]["id"].(string)
	blocks := append([]markdown.Block(nil), l.blocks[id]...)
	for i := range blocks {
		for j := i + 1; j < len(blocks); j++ {
			if blocks[j].Order < blocks[i].Order {
				blocks[i], blocks[j] = blocks[j], blocks[i]
			}
		}
	}
	return blocks
}

func opts() Options {
	return Options{Title: "Notion import", Untitled: "Untitled", ReportTitle: "Import report", ViewName: "Table"}
}

func importExport(t *testing.T, files map[string]string, wrapper string) (library, Report, *vault.Vault) {
	t.Helper()
	v := openVault(t)
	rep, _, err := Import(context.Background(), v, zipOf(t, files, wrapper), opts())
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	return load(v), rep, v
}

func TestAnExportBecomesPagesUnderOnePage(t *testing.T) {
	lib, rep, _ := importExport(t, export(), "Export-9f8e7d6c")
	root := lib.pages["Notion import"]
	if root == nil || rep.RootID != root["id"] {
		t.Fatalf("no page holds the import: %+v", rep)
	}
	for _, title := range []string{"Home", "Sub page", "Tasks", "Write code", "Write tests", "Loose folder", "Inside"} {
		if lib.pages[title] == nil {
			t.Fatalf("page %q is missing; have %v", title, keys(lib.pages))
		}
	}
	parent := func(title string) string { p, _ := lib.pages[title]["parent_id"].(string); return p }
	id := func(title string) string { s, _ := lib.pages[title]["id"].(string); return s }
	if parent("Home") != id("Notion import") || parent("Loose folder") != id("Notion import") {
		t.Fatalf("top pages are under the import page: Home %q, Loose %q", parent("Home"), parent("Loose folder"))
	}
	if parent("Sub page") != id("Home") || parent("Tasks") != id("Home") || parent("Inside") != id("Loose folder") {
		t.Fatal("pages are under the page of their folder")
	}
	if parent("Write code") != id("Tasks") {
		t.Fatal("rows are under their database")
	}
	if rep.Pages != 9 || rep.Databases != 1 || rep.Rows != 4 {
		t.Fatalf("report = %+v", rep)
	}
	if lib.pages["Tasks"]["kind"] != "database" {
		t.Fatalf("Tasks is %v", lib.pages["Tasks"]["kind"])
	}
}

// nameless names no mention: the test pages have none that matter.
func nameless(map[string]any) string { return "" }

func keys(m map[string]vault.Record) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestABodyKeepsItsBlocks(t *testing.T) {
	lib, rep, _ := importExport(t, export(), "")
	kinds := map[string]int{}
	var all []markdown.Block
	for _, b := range lib.text("Home") {
		kinds[b.Kind]++
		all = append(all, b)
	}
	want := map[string]int{"text": 6, "todo": 2, "bullet": 1, "numbered": 2, "quote": 1, "callout": 1, "toggle": 1, "table": 1, "code": 1, "math": 1, "file": 0}
	for kind, n := range want {
		if kind == "file" {
			continue
		}
		if kinds[kind] < n {
			t.Errorf("%s paragraphs: got %d, want at least %d (all %v)", kind, kinds[kind], n, kinds)
		}
	}
	// The title heading is the page's name, not a paragraph.
	for _, b := range all {
		if strings.HasPrefix(markdown.StoredText(markdown.RunsToStored(b.Runs)), "Home") && b.Kind == "heading1" {
			t.Error("the page's own heading stays in its body")
		}
	}
	// A callout has its icon and words, the rest under it; a toggle its title and what it hides.
	var callout, toggle markdown.Block
	for _, b := range all {
		switch b.Kind {
		case "callout":
			callout = b
		case "toggle":
			toggle = b
		}
	}
	if callout.Icon != "💡" || markdown.Words(callout, nameless) != "A callout with words" {
		t.Errorf("callout = %q %q", callout.Icon, markdown.Words(callout, nameless))
	}
	if toggle.Runs[0].Text != "A toggle" {
		t.Errorf("toggle = %+v", toggle.Runs)
	}
	children := func(parent string) []string {
		var out []string
		for _, b := range all {
			if b.ParentID == parent {
				out = append(out, markdown.Words(b, nameless))
			}
		}
		return out
	}
	if got := children(callout.ID); len(got) != 1 || got[0] != "More under it" {
		t.Errorf("under the callout: %v", got)
	}
	if got := children(toggle.ID); len(got) != 1 || got[0] != "Hidden words" {
		t.Errorf("under the toggle: %v", got)
	}
	// Nesting by four spaces.
	var nested markdown.Block
	for _, b := range all {
		if markdown.Words(b, nameless) == "nested item" {
			nested = b
		}
	}
	var done markdown.Block
	for _, b := range all {
		if markdown.Words(b, nameless) == "done" {
			done = b
		}
	}
	if nested.ParentID != done.ID || done.ID == "" || !done.Checked {
		t.Errorf("nested item under %q, done is %q checked %v", nested.ParentID, done.ID, done.Checked)
	}
	// A formula is an equation block, its source the TeX.
	var formula string
	for _, b := range all {
		if b.Kind == "math" {
			formula = markdown.Words(b, nameless)
		}
	}
	if formula != "E = mc^2" {
		t.Errorf("formula = %q", formula)
	}
	if rep.Links < 3 {
		t.Errorf("links carried over: %d", rep.Links)
	}
}

func TestLinksBetweenPagesAreResolved(t *testing.T) {
	lib, _, _ := importExport(t, export(), "")
	home, sub, tasks := lib.pages["Home"]["id"], lib.pages["Sub page"]["id"], lib.pages["Tasks"]["id"]
	mentions := func(title string) map[any]bool {
		out := map[any]bool{}
		for _, b := range lib.text(title) {
			for _, r := range b.Runs {
				if r.Mention != nil && r.Mention["kind"] == "page" {
					out[r.Mention["page_id"]] = true
				}
			}
		}
		return out
	}
	if m := mentions("Home"); !m[sub] || !m[tasks] {
		t.Errorf("Home links to %v, want the sub page %v and the database %v", m, sub, tasks)
	}
	if m := mentions("Sub page"); !m[home] {
		t.Errorf("the sub page links back to %v, want %v", m, home)
	}
	// A page of Notion that is not in the export stays a link to Notion.
	var external bool
	for _, b := range lib.text("Home") {
		for _, r := range b.Runs {
			if strings.Contains(r.Link, "notion.so") && r.Text == "a stranger" {
				external = true
			}
		}
	}
	if !external {
		t.Error("a link to a page that was not exported is kept as a link")
	}
}

func TestPicturesAndFilesAreKept(t *testing.T) {
	lib, rep, v := importExport(t, export(), "")
	if rep.Files != 2 {
		t.Fatalf("files kept = %d", rep.Files)
	}
	var picture, file map[string]any
	for _, b := range lib.text("Home") {
		for _, r := range b.Runs {
			if r.Mention != nil && r.Mention["kind"] == "image" {
				picture, _ = r.Mention["file"].(map[string]any)
			}
		}
		if b.Kind == "file" {
			file = b.FileRef
		}
	}
	if picture == nil || picture["type"] != "image/png" || picture["width"] != float64(1) {
		t.Fatalf("picture = %+v", picture)
	}
	if file == nil || file["name"] != "contract.pdf" {
		t.Fatalf("file = %+v", file)
	}
	data, err := v.ReadFile(picture["path"].(string))
	if err != nil || len(data) == 0 {
		t.Fatalf("the picture's bytes are not in the vault: %v", err)
	}
	if got, _ := v.ReadFile(file["path"].(string)); string(got) != "%PDF-1.4 fake" {
		t.Fatalf("file bytes = %q", got)
	}
}

func TestTablesFromMarkdownAndHTML(t *testing.T) {
	lib, _, _ := importExport(t, export(), "")
	cell := func(table map[string]any, row, col string) string {
		rows := table["rows"].(map[string]any)
		cells := rows[row].(map[string]any)["cells"].(map[string]any)
		return markdown.StoredText(cells[col])
	}
	var pipe, html map[string]any
	for _, b := range lib.text("Home") {
		if b.Kind == "table" {
			pipe = b.Table
		}
	}
	for _, b := range lib.text("Sub page") {
		if b.Kind == "table" {
			html = b.Table
		}
	}
	if pipe == nil || cell(pipe, "row1", "col1") != "Name" || cell(pipe, "row3", "col2") != "5" || pipe["header_row"] != true {
		t.Fatalf("markdown table = %+v", pipe)
	}
	if html == nil || cell(html, "row1", "col2") != "B" || cell(html, "row2", "col2") != "x y" || html["header_row"] != true {
		t.Fatalf("html table = %+v", html)
	}
}

func TestADatabaseKeepsItsColumnsRowsAndValues(t *testing.T) {
	lib, _, _ := importExport(t, export(), "")
	db := lib.pages["Tasks"]
	cfg := db["database"].(map[string]any)
	byName := map[string]map[string]any{}
	for _, p := range cfg["properties"].([]any) {
		m := p.(map[string]any)
		byName[m["name"].(string)] = m
	}
	if byName["Status"]["type"] != "select" || byName["Due"]["type"] != "date" || byName["Notes"]["type"] != "text" || byName["Hidden"] != nil {
		t.Fatalf("properties = %+v", byName)
	}
	if opts := byName["Status"]["options"].([]any); len(opts) != 2 || opts[0] != "Doing" || opts[1] != "Done" {
		t.Fatalf("status options = %v", opts)
	}
	if cfg["title_name"] != "Name" {
		t.Fatalf("title column = %v", cfg["title_name"])
	}
	if views := cfg["views"].([]any); len(views) != 1 || views[0].(map[string]any)["kind"] != "table" {
		t.Fatalf("views = %v", views)
	}
	row := lib.pages["Write code"]
	props := row["properties"].(vault.Record)
	if props[byName["Status"]["id"].(string)] != "Doing" || props[byName["Due"]["id"].(string)] != "2026-01-05" {
		t.Fatalf("row values = %v", props)
	}
	// A time of day is dropped from a date; a row of the CSV with no page of
	// its own gets one; a row's body has the values lines taken out.
	if got := lib.pages["Ship it"]["properties"].(vault.Record)[byName["Due"]["id"].(string)]; got != "2026-01-09" {
		t.Fatalf("Ship it due %v", got)
	}
	if lib.pages["Extra row"] == nil {
		t.Fatal("a row with no page file is not lost")
	}
	body := lib.text("Write code")
	if len(body) != 1 || markdown.Words(body[0], nameless) != "The body of the first task." {
		t.Fatalf("row body = %v", body)
	}
}

func TestTheWrapperAndNestedZipsAreOpened(t *testing.T) {
	inner := zipOf(t, export(), "Export-9f8e7d6c")
	data, _ := os.ReadFile(inner)
	outer := zipOf(t, map[string]string{"Export-9f8e7d6c-Part-1.zip": string(data)}, "")
	v := openVault(t)
	if _, _, err := Import(context.Background(), v, outer, opts()); err != nil {
		t.Fatal(err)
	}
	if lib := load(v); lib.pages["Sub page"] == nil || lib.pages["Export-9f8e7d6c"] != nil {
		t.Fatalf("pages = %v", keys(lib.pages))
	}
}

func TestAnUnsafeZipIsRefused(t *testing.T) {
	path := zipOf(t, map[string]string{"../evil.md": "# x", "ok " + idHome + ".md": "# ok"}, "")
	v := openVault(t)
	rep, _, err := Import(context.Background(), v, path, opts())
	if err != nil {
		t.Fatal(err)
	}
	if lib := load(v); lib.pages["ok"] == nil || lib.pages["x"] != nil {
		t.Fatalf("pages = %v", keys(lib.pages))
	}
	if !strings.Contains(strings.Join(rep.Warnings, "\n"), "unsafe") {
		t.Fatalf("warnings = %v", rep.Warnings)
	}
	small := Limits{MaxEntries: 10, MaxFile: 4, MaxTotal: 100, MaxDepth: 1, MaxNested: 1}
	if _, _, err := Import(context.Background(), openVault(t), zipOf(t, map[string]string{"a.md": "too big for the limit"}, ""), Options{Title: "t", Limits: small}); err == nil {
		t.Fatal("a file over the limit should refuse the import")
	}
	notZip := filepath.Join(t.TempDir(), "x.zip")
	os.WriteFile(notZip, []byte("not a zip"), 0o644)
	if _, _, err := Import(context.Background(), openVault(t), notZip, opts()); err == nil {
		t.Fatal("a file that is not a zip should be refused")
	}
}

func TestWhatCouldNotBeCarriedOverIsReported(t *testing.T) {
	files := map[string]string{"A " + idHome + ".md": "# A\n\nSee [gone](Missing%20" + idSub + ".md) and ![lost](nothing.png).\n"}
	lib, rep, _ := importExport(t, files, "")
	if len(rep.Warnings) < 2 || lib.pages["Import report"] == nil {
		t.Fatalf("warnings = %v, pages = %v", rep.Warnings, keys(lib.pages))
	}
	if blocks := lib.text("Import report"); len(blocks) < 2 {
		t.Fatalf("the report page lists %d warnings", len(blocks))
	}
	// An export that loses nothing leaves no report.
	lib, rep, _ = importExport(t, map[string]string{"B.md": "# B\n\nfine\n"}, "")
	if lib.pages["Import report"] != nil || len(rep.Warnings) != 0 {
		t.Fatalf("a clean import has no report: %v", rep.Warnings)
	}
}

func TestNamesLoseTheirIDs(t *testing.T) {
	for in, want := range map[string]string{
		"Home " + idHome: "Home",
		"My page 1a2b3c4d-5e6f-47a8-b9c0-d1e2f3a4b5c6": "My page",
		"No id here": "No id here",
		"2026 plan":  "2026 plan",
	} {
		if got := stripID(in); got != want {
			t.Errorf("stripID(%q) = %q, want %q", in, got, want)
		}
	}
	if idOf("Home "+idHome) != idHome || idOf("Home") != "" {
		t.Error("idOf")
	}
	if idIn("https://www.notion.so/Home-"+idHome+"?pvs=4") != idHome {
		t.Error("idIn")
	}
}

func TestNamesWrittenDecomposedStillMatchTheirLinks(t *testing.T) {
	// macOS writes file names decomposed (é as e + accent); Notion's links are composed.
	decomposed := "Café " + idHome + ".md"
	files := map[string]string{
		decomposed:               "# Café\n\nBack to [Other](Other%20" + idSub + ".md) and [notion](https://www.notion.so/Other-" + idSub + "?pvs=4).\n",
		"Other " + idSub + ".md": "# Other\n\nTo [Café](Caf%C3%A9%20" + idHome + ".md)\n",
	}
	lib, rep, _ := importExport(t, files, "")
	var linked []any
	for _, b := range lib.text("Café") {
		for _, r := range b.Runs {
			if r.Mention != nil {
				linked = append(linked, r.Mention["page_id"])
			}
		}
	}
	if len(linked) != 2 || linked[0] != lib.pages["Other"]["id"] || linked[1] != lib.pages["Other"]["id"] {
		t.Fatalf("links from Café = %v (warnings %v)", linked, rep.Warnings)
	}
	if blocks := lib.text("Other"); len(blocks) == 0 || blocks[0].Runs[len(blocks[0].Runs)-1].Mention["page_id"] != lib.pages["Café"]["id"] {
		t.Fatalf("the composed link did not find the decomposed file: %+v", blocks)
	}
}

func TestAnExportOfManyPagesImportsQuickly(t *testing.T) {
	files := map[string]string{}
	for i := 0; i < 1500; i++ {
		id := strings.Repeat("a", 24) + strings.Repeat("0", 8-len(itoa(i))) + itoa(i)
		files["Page "+itoa(i)+" "+id+".md"] = "# Page " + itoa(i) + "\n\n" + strings.Repeat("Some words in a paragraph.\n\n- item\n- item\n\n", 10)
	}
	start := nowMillis()
	_, rep, _ := importExport(t, files, "")
	if rep.Pages != 1500 {
		t.Fatalf("pages = %d", rep.Pages)
	}
	if took := nowMillis() - start; took > 60_000 {
		t.Fatalf("1500 pages took %d ms", took)
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

func nowMillis() int64 { return time.Now().UnixMilli() }
