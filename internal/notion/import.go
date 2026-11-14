package notion

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"mime"
	"net/http"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"notes/internal/markdown"
	"notes/internal/vault"
)

// Options are what the person chose and the words the import writes in the
// language shown.
type Options struct {
	// Parent is the page the import goes under; empty puts it at the top.
	Parent string
	// Title names the page that holds everything the import makes.
	Title string
	// Untitled names a page the export gives no name; ReportTitle the page
	// that lists what could not be carried over; ViewName a database's view.
	Untitled, ReportTitle, ViewName string
	// Limits for the zip; zero is DefaultLimits.
	Limits Limits
	// Progress is told how many pages are written of how many.
	Progress func(done, total int)
	// Now is the clock; nil is time.Now.
	Now func() time.Time
}

// Report says what an import made.
type Report struct {
	// RootID is the page that holds the import.
	RootID string
	// Pages counts every page, databases and their rows included.
	Pages, Databases, Rows int
	// Files counts the pictures and files kept on pages.
	Files int
	// Links counts the links between pages that were carried over.
	Links int
	// Warnings say what was left out or could not be carried over.
	Warnings []string
}

// node is a page of the export: a page, a database, or a row of one.
type node struct {
	kind   string // "page", "database", "row"
	rel    string // its file in the export, "" for a folder that had no page
	id     string // Notion's id for it
	title  string
	newID  string
	parent *node
	body   string
	dir    string // the folder of the files that belong under it

	// A database's columns, and the property ids made for them.
	csv   *csvTable
	props []property
	// A row's values by property id, and its place among the rows.
	values map[string]string
	order  int

	files  []pending
	linked int
}

type property struct {
	id, name, kind string
	options        []string
}

// pending is a file of the export to be stored with a page.
type pending struct {
	rel, path string
}

const author = "Local notebook"

// Import reads the Notion export at zipPath and writes it into v, under one
// page of its own. It returns what it wrote, and the changes to tell the
// window of.
func Import(ctx context.Context, v *vault.Vault, zipPath string, opt Options) (Report, []vault.Change, error) {
	var rep Report
	limits := opt.Limits
	if limits == (Limits{}) {
		limits = DefaultLimits
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	tmp, err := os.MkdirTemp("", "notes-notion-*")
	if err != nil {
		return rep, nil, err
	}
	defer os.RemoveAll(tmp)
	exp, err := Unpack(zipPath, tmp, limits)
	if err != nil {
		return rep, nil, err
	}
	rep.Warnings = append(rep.Warnings, exp.Warnings...)
	p := &plan{exp: exp, opt: opt, rep: &rep, byRel: map[string]*node{}, byID: map[string]*node{}, byDir: map[string]*node{}, files: map[string]bool{}}
	if err := p.build(); err != nil {
		return rep, nil, err
	}
	changes, err := p.write(ctx, v)
	return rep, changes, err
}

type plan struct {
	exp   *Export
	opt   Options
	rep   *Report
	nodes []*node
	root  *node

	byRel map[string]*node // a page's or database's file
	byID  map[string]*node
	byDir map[string]*node // the folder that holds a page's children
	files map[string]bool  // the files of the export that are not pages
}

func newID(prefix string) string {
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Sprintf("%s_%016x", prefix, time.Now().UnixNano())
	}
	return prefix + "_" + hex.EncodeToString(raw)
}

func isPageFile(rel string) bool {
	e := strings.ToLower(path.Ext(rel))
	return e == ".md" || e == ".markdown"
}

func (p *plan) warn(text string) { p.rep.Warnings = append(p.rep.Warnings, text) }

// build reads the export into a tree of pages.
func (p *plan) build() error {
	p.root = &node{kind: "page", title: p.opt.Title, newID: newID("page")}
	csvs := map[string]bool{}
	for _, rel := range p.exp.Files {
		if strings.EqualFold(path.Ext(rel), ".csv") {
			csvs[rel] = true
		}
	}
	for _, rel := range p.exp.Files {
		switch {
		case isPageFile(rel):
			data, err := p.exp.Read(rel)
			if err != nil {
				return err
			}
			n := &node{kind: "page", rel: rel, id: idOf(stem(path.Base(rel))), newID: newID("page"), dir: stem(rel), body: string(data)}
			n.title, n.body = titleOf(n, p.opt.Untitled)
			p.register(n)
		case csvs[rel]:
			// Notion writes a database twice: its CSV, and "_all.csv" with the
			// columns its view hid. The first is the one that is read.
			if base := strings.TrimSuffix(rel, "_all.csv"); base != rel && csvs[base+".csv"] {
				continue
			}
			data, err := p.exp.Read(rel)
			if err != nil {
				return err
			}
			t, err := readCSV(data)
			if err != nil {
				p.warn("a database could not be read: " + rel)
				continue
			}
			base := strings.TrimSuffix(path.Base(rel), "_all.csv")
			if base == path.Base(rel) {
				base = stem(path.Base(rel))
			}
			n := &node{kind: "database", rel: rel, id: idOf(base), newID: newID("page"), dir: path.Join(path.Dir(rel), base), csv: &t}
			if n.dir == "" || strings.HasPrefix(n.dir, "./") {
				n.dir = strings.TrimPrefix(n.dir, "./")
			}
			n.title = stripID(base)
			if n.title == "" {
				n.title = p.opt.Untitled
			}
			p.register(n)
		default:
			p.files[rel] = true
		}
	}
	sort.SliceStable(p.nodes, func(i, j int) bool { return p.nodes[i].rel < p.nodes[j].rel })
	for _, n := range append([]*node(nil), p.nodes...) {
		n.parent = p.parentOf(n)
	}
	for _, n := range p.nodes {
		if n.kind == "page" && n.parent != nil && n.parent.kind == "database" {
			n.kind = "row"
		}
	}
	for _, n := range p.nodes {
		if n.kind == "database" {
			p.fillDatabase(n)
		}
	}
	for _, n := range p.nodes {
		switch n.kind {
		case "database":
			p.rep.Databases++
		case "row":
			p.rep.Rows++
		}
	}
	p.rep.Pages = len(p.nodes)
	return nil
}

func (p *plan) register(n *node) {
	p.nodes = append(p.nodes, n)
	if n.rel != "" {
		p.byRel[n.rel] = n
	}
	if n.id != "" {
		if _, taken := p.byID[n.id]; !taken || n.kind == "page" {
			p.byID[n.id] = n
		}
	}
	// A page's folder is where its children are; a page wins it over a database
	// of the same name.
	if n.dir != "" {
		if old, taken := p.byDir[n.dir]; !taken || old.kind == "database" && n.kind == "page" {
			p.byDir[n.dir] = n
		}
	}
}

// parentOf finds the page a page is under: the page of the folder it is in. A
// folder with no page of its own gets one made for it.
func (p *plan) parentOf(n *node) *node {
	dir := path.Dir(n.rel)
	if n.rel == "" {
		dir = path.Dir(n.dir)
	}
	return p.folder(dir)
}

func (p *plan) folder(dir string) *node {
	if dir == "." || dir == "" || dir == "/" {
		return nil
	}
	if parent, ok := p.byDir[dir]; ok {
		return parent
	}
	// Notion truncates a long name in a folder and not in its page: match on
	// the name without its id when the exact folder is not found.
	made := &node{kind: "page", title: stripID(path.Base(dir)), id: idOf(path.Base(dir)), newID: newID("page"), dir: dir}
	if made.title == "" {
		made.title = p.opt.Untitled
	}
	p.byDir[dir] = made
	p.nodes = append(p.nodes, made)
	made.parent = p.folder(path.Dir(dir))
	return made
}

// titleOf is a page's name: the heading that opens its file, else its file's
// name. The heading is taken out of the body, as are the lines of values that
// follow it in a row.
func titleOf(n *node, untitled string) (string, string) {
	lines := splitLines(n.body)
	i := 0
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	title := stripID(stem(path.Base(n.rel)))
	if i < len(lines) && strings.HasPrefix(lines[i], "# ") {
		if h := strings.TrimSpace(lines[i][2:]); h != "" {
			title = h
		}
		i++
	}
	if title == "" {
		title = untitled
	}
	return title, strings.Join(lines[i:], "\n")
}

var reValueLine = regexp.MustCompile(`^[^:\n]{1,80}:(?: .*)?$`)

// dropValues takes the lines of a database row's values from the start of its
// body: the CSV holds them already.
func dropValues(body string) string {
	lines := splitLines(body)
	i := 0
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	start := i
	for i < len(lines) && reValueLine.MatchString(lines[i]) {
		i++
	}
	if i == start {
		return body
	}
	return strings.Join(lines[i:], "\n")
}

// fillDatabase reads a database's columns and rows, and matches its rows to
// the pages of its folder by name.
func (p *plan) fillDatabase(db *node) {
	t := db.csv
	if len(t.header) == 0 {
		return
	}
	for col := 1; col < len(t.header); col++ {
		kind, options := kindOf(t.rows, col)
		name := strings.TrimSpace(t.header[col])
		if name == "" {
			name = fmt.Sprintf("Column %d", col)
		}
		db.props = append(db.props, property{id: newID("prop"), name: name, kind: kind, options: options})
	}
	byTitle := map[string][]*node{}
	for _, n := range p.nodes {
		if n.parent == db && n.kind == "row" {
			byTitle[clean(strings.TrimSpace(n.title))] = append(byTitle[clean(strings.TrimSpace(n.title))], n)
			n.body = dropValues(n.body)
		}
	}
	for i, row := range t.rows {
		key := clean(strings.TrimSpace(row[0]))
		var n *node
		if list := byTitle[key]; len(list) > 0 {
			n, byTitle[key] = list[0], list[1:]
		} else {
			title := strings.TrimSpace(row[0])
			if title == "" {
				title = p.opt.Untitled
			}
			n = &node{kind: "row", title: title, newID: newID("page"), parent: db}
			p.nodes = append(p.nodes, n)
		}
		n.order = i
		n.values = map[string]string{}
		for c, prop := range db.props {
			v := strings.TrimSpace(row[c+1])
			if v == "" {
				continue
			}
			if prop.kind == "date" {
				if d, ok := day(v); ok {
					v = d
				}
			}
			n.values[prop.id] = v
		}
	}
}

// converterFor is what the reading of a page's body needs: the links it
// holds are resolved against the export, and the files it names are kept with it.
func (p *plan) converterFor(n *node) *converter {
	c := &converter{dir: path.Dir(n.rel), newID: func() string { return newID("block") }, warn: p.warn}
	if n.rel == "" {
		c.dir = ""
	}
	kept := map[string]map[string]any{}
	c.page = func(target string) (string, bool) {
		var found *node
		if isNotionAddress(target) {
			found = p.byID[idIn(target)]
		} else {
			rel := c.resolveLocal(target)
			found = p.byRel[rel]
			if found == nil {
				found = p.byDir[strings.TrimSuffix(rel, "/")]
			}
			if found == nil && (isPageFile(rel) || strings.EqualFold(path.Ext(rel), ".csv")) {
				found = p.byID[idOf(stem(path.Base(rel)))]
			}
		}
		if found == nil {
			return "", false
		}
		n.linked++
		return found.newID, true
	}
	c.file = func(target string) (map[string]any, bool) {
		rel := c.resolveLocal(target)
		if !p.files[rel] {
			return nil, false
		}
		if rec, ok := kept[rel]; ok {
			return rec, true
		}
		rec := p.fileRecord(n, rel)
		if rec == nil {
			return nil, false
		}
		kept[rel] = rec
		return rec, true
	}
	return c
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// safeName is a file's name as the attachments folder takes it: letters,
// digits, dots, dashes and underscores; a name in another script keeps its
// extension and is shown by its own name.
func safeName(name string) string {
	ext := strings.Trim(unsafeName.ReplaceAllString(path.Ext(name), "_"), "._")
	if len(ext) > 12 {
		ext = ""
	}
	stem := strings.Trim(unsafeName.ReplaceAllString(strings.TrimSuffix(name, path.Ext(name)), "_"), "._")
	if stem == "" {
		stem = "file"
	}
	if len(stem) > 100 {
		stem = stem[:100]
	}
	if ext == "" {
		return stem
	}
	return stem + "." + ext
}

func shownName(name string) string {
	name = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, path.Base(name)))
	if chars := []rune(name); len(chars) > 255 {
		name = string(chars[:255])
	}
	if name == "" || name == "." {
		return "file"
	}
	return name
}

var pageDir = regexp.MustCompile(`[^\w-]`)

// fileRecord is the record a page keeps of a file of the export, which is
// stored when the page is written.
func (p *plan) fileRecord(n *node, rel string) map[string]any {
	size := p.exp.Size(rel)
	name := path.Base(rel)
	stored := "files/" + pageDir.ReplaceAllString(n.newID, "") + "/" + newID("file") + "/" + safeName(name)
	kind := mediaTypeOf(p.exp, rel)
	rec := map[string]any{"path": stored, "owner": "local-user", "name": shownName(name), "size": float64(size), "type": kind}
	if strings.HasPrefix(kind, "image/") {
		if f, err := p.exp.Open(rel); err == nil {
			if cfg, _, err := image.DecodeConfig(f); err == nil && cfg.Width > 0 && cfg.Height > 0 {
				rec["width"], rec["height"] = float64(cfg.Width), float64(cfg.Height)
			}
			f.Close()
		}
	}
	n.files = append(n.files, pending{rel: rel, path: stored})
	return rec
}

func mediaTypeOf(e *Export, rel string) string {
	if byName := mime.TypeByExtension(strings.ToLower(path.Ext(rel))); byName != "" {
		return strings.ToLower(strings.SplitN(byName, ";", 2)[0])
	}
	if f, err := e.Open(rel); err == nil {
		defer f.Close()
		head := make([]byte, 512)
		n, _ := f.Read(head)
		return strings.ToLower(strings.SplitN(http.DetectContentType(head[:n]), ";", 2)[0])
	}
	return "application/octet-stream"
}

func stamp(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

// write stores the pages: the page that holds the import first, then each page
// after the page it is under. A page's files go in before the page that names them.
func (p *plan) write(ctx context.Context, v *vault.Vault) (changes []vault.Change, err error) {
	now := p.opt.Now()
	put := func(c vault.Collection, id string, value vault.Record) error {
		ack, err := v.Write(vault.Write{Action: "put", Collection: c, ID: id, Value: value})
		if err != nil {
			return err
		}
		changes = append(changes, vault.Change{Collection: c, ID: id, Value: ack.Value, Deleted: ack.Value == nil})
		return nil
	}
	page := func(n *node, kind string) vault.Record {
		rec := vault.Record{"id": n.newID, "kind": kind, "icon": "", "title_content": markdown.StoredRuns(n.title),
			"created_at": stamp(now), "updated_at": stamp(now), "created_by": author, "updated_by": author}
		parent := ""
		switch {
		case n.parent != nil:
			parent = n.parent.newID
		case n != p.root:
			parent = p.root.newID
		default:
			parent = p.opt.Parent
		}
		if parent != "" {
			rec["parent_id"] = parent
		}
		return rec
	}
	if err := put(vault.Pages, p.root.newID, page(p.root, "document")); err != nil {
		return changes, err
	}
	// Parents before children: a folder's page is made before what is in it.
	ordered := p.nodes
	sort.SliceStable(ordered, func(i, j int) bool { return depth(ordered[i]) < depth(ordered[j]) })
	for i, n := range ordered {
		if err := ctx.Err(); err != nil {
			return changes, err
		}
		kind := "document"
		if n.kind == "database" {
			kind = "database"
		}
		rec := page(n, kind)
		if n.kind == "database" {
			props := make([]any, 0, len(n.props))
			for _, pr := range n.props {
				def := map[string]any{"id": pr.id, "name": pr.name, "type": pr.kind}
				if pr.kind == "select" {
					opts := make([]any, len(pr.options))
					for k, o := range pr.options {
						opts[k] = o
					}
					def["options"] = opts
				}
				props = append(props, def)
			}
			view := map[string]any{"id": newID("view"), "name": p.opt.ViewName, "kind": "table"}
			rec["database"] = map[string]any{"properties": props, "views": []any{view}, "title_name": strings.TrimSpace(n.csv.header[0])}
		}
		if n.kind == "row" {
			values := map[string]any{}
			for k, val := range n.values {
				values[k] = val
			}
			if len(values) > 0 {
				rec["properties"] = values
			}
			rec["row_order"] = float64(now.UnixMilli() + int64(n.order))
		}
		var blocks []markdown.Block
		if n.body != "" {
			blocks = convertBody(n.body, p.converterFor(n))
		}
		for _, f := range n.files {
			data, err := p.exp.Read(f.rel)
			if err != nil {
				p.warn("a file could not be read: " + f.rel)
				continue
			}
			if err := v.WriteFile(f.path, data); err != nil {
				return changes, err
			}
			p.rep.Files++
		}
		p.rep.Links += n.linked
		if err := put(vault.Pages, n.newID, rec); err != nil {
			return changes, err
		}
		for _, b := range blocks {
			if err := put(vault.Blocks, vault.BlockRecordID(n.newID, b.ID), vault.Record(b.ToRecord())); err != nil {
				return changes, err
			}
		}
		if i%50 == 49 {
			if err := v.Flush(); err != nil {
				return changes, err
			}
		}
		if p.opt.Progress != nil {
			p.opt.Progress(i+1, len(ordered))
		}
	}
	p.rep.RootID = p.root.newID
	if len(p.rep.Warnings) > 0 {
		if err := p.writeReport(put, now); err != nil {
			return changes, err
		}
	}
	return changes, v.Flush()
}

// depth is how many pages lie above a page.
func depth(n *node) int {
	d := 0
	for ; n.parent != nil; n = n.parent {
		d++
	}
	return d
}

// writeReport makes the page that lists what could not be carried over.
func (p *plan) writeReport(put func(vault.Collection, string, vault.Record) error, now time.Time) error {
	id := newID("page")
	rec := vault.Record{"id": id, "kind": "document", "icon": "", "title_content": markdown.StoredRuns(p.opt.ReportTitle),
		"created_at": stamp(now), "updated_at": stamp(now), "created_by": author, "updated_by": author, "parent_id": p.root.newID}
	if err := put(vault.Pages, id, rec); err != nil {
		return err
	}
	seen := map[string]bool{}
	i := 0
	for _, w := range p.rep.Warnings {
		if seen[w] {
			continue
		}
		seen[w] = true
		i++
		b := markdown.Block{ID: newID("block"), Kind: "bullet", Order: float64(1024 * i), Runs: []markdown.Run{{Text: w}}}
		if err := put(vault.Blocks, vault.BlockRecordID(id, b.ID), vault.Record(b.ToRecord())); err != nil {
			return err
		}
	}
	return nil
}
