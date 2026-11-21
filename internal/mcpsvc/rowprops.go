package mcpsvc

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// A database is a page of kind "database" whose columns are database.properties.
// Its rows are the pages under it; a row's values are page.properties, by the
// column's id. A status or a select is kept as the option's name. This file
// reads and writes those values by the column's name, as the page does
// (web/src/features/database/database-model.ts).

// maxTextValue is the longest text a text column takes from here.
const maxTextValue = 2000

type statusOption struct{ id, name, group string }

// column is one column of a database.
type column struct {
	id, name, kind string
	options        []string // a select's
	statuses       []statusOption
	defaultStatus  string // the id of the option a row reads as until one is set
}

// Notion's three, which a status has until it has options of its own.
var defaultStatuses = []statusOption{
	{"not-started", "Not started", "todo"},
	{"in-progress", "In progress", "progress"},
	{"done", "Done", "complete"},
}

var groupRank = map[string]int{"todo": 0, "progress": 1, "complete": 2}

// isDatabase says whether a page is a database.
func (lib library) isDatabase(id string) bool {
	return str(lib.pages[id]["kind"]) == "database"
}

// databaseOf is the database a row is in: the page above it, when that is one.
func (lib library) databaseOf(id string) (string, bool) {
	parent := str(lib.pages[id]["parent_id"])
	if parent != "" && lib.pages[parent] != nil && lib.isDatabase(parent) {
		return parent, true
	}
	return "", false
}

// columns are a database's columns, in the order it keeps them.
func (lib library) columns(db string) []column {
	config, _ := lib.pages[db]["database"].(map[string]any)
	list, _ := config["properties"].([]any)
	var out []column
	for _, item := range list {
		p, _ := item.(map[string]any)
		if p == nil || str(p["id"]) == "" {
			continue
		}
		c := column{id: str(p["id"]), name: str(p["name"]), kind: str(p["type"]), defaultStatus: str(p["default_status"])}
		if opts, ok := p["options"].([]any); ok {
			for _, o := range opts {
				if name := str(o); name != "" {
					c.options = append(c.options, name)
				}
			}
		}
		if c.kind == "status" {
			if opts, ok := p["status_options"].([]any); ok {
				for _, o := range opts {
					m, _ := o.(map[string]any)
					if m != nil && str(m["name"]) != "" {
						c.statuses = append(c.statuses, statusOption{str(m["id"]), str(m["name"]), str(m["group"])})
					}
				}
			}
			if len(c.statuses) == 0 {
				c.statuses = defaultStatuses
			}
			// The options as the database keeps them: To-do first, then In
			// progress, then Complete.
			sort.SliceStable(c.statuses, func(i, j int) bool { return groupRank[c.statuses[i].group] < groupRank[c.statuses[j].group] })
		}
		out = append(out, c)
	}
	return out
}

// readsAs is what a row reads as in a column: a status with no stored value
// reads as the column's default option, and a stored value that names an
// option (spaces aside) as that option; anything else is shown as stored.
func (c column) readsAs(stored string) string {
	if c.kind != "status" {
		return strings.TrimSpace(stored)
	}
	for _, o := range c.statuses {
		if o.name == stored || o.name == strings.TrimSpace(stored) {
			return o.name
		}
	}
	for _, o := range c.statuses {
		if o.id == c.defaultStatus {
			return o.name
		}
	}
	for _, o := range c.statuses {
		if o.group == "todo" {
			return o.name
		}
	}
	return c.statuses[0].name
}

type value struct{ name, text string }

// rowValues are a row's values in its database's column order: the ones that
// have a value, a status always.
func (lib library) rowValues(id string) []value {
	db, ok := lib.databaseOf(id)
	if !ok {
		return nil
	}
	stored, _ := lib.pages[id]["properties"].(map[string]any)
	var out []value
	for _, c := range lib.columns(db) {
		if c.kind == "image" {
			files, _ := lib.pages[id]["property_files"].(map[string]any)
			if list, _ := files[c.id].([]any); len(list) > 0 {
				out = append(out, value{c.name, fmt.Sprintf("%d file(s)", len(list))})
			}
			continue
		}
		text := c.readsAs(str(stored[c.id]))
		if text != "" {
			out = append(out, value{c.name, text})
		}
	}
	return out
}

// rowMap is a row's values by column name, for a list.
func (lib library) rowMap(id string) map[string]string {
	values := lib.rowValues(id)
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for _, v := range values {
		out[v.name] = v.text
	}
	return out
}

// rowLines are the lines notes_fetch adds for a row: its database, then its values.
func (lib library) rowLines(id string) string {
	db, ok := lib.databaseOf(id)
	if !ok {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Database: %s (%s)\n", lib.shown(db), db)
	var parts []string
	for _, v := range lib.rowValues(id) {
		parts = append(parts, v.name+": "+oneLine(v.text))
	}
	if len(parts) > 0 {
		b.WriteString("Properties: " + strings.Join(parts, "; ") + "\n")
	}
	return b.String()
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// databaseLines are the lines notes_fetch adds for a database: its columns, and
// its views.
func (lib library) databaseLines(id string) string {
	cols := lib.columns(id)
	var b strings.Builder
	if len(cols) > 0 {
		b.WriteString("Properties:\n")
		for _, c := range cols {
			switch c.kind {
			case "status":
				var names []string
				for _, o := range c.statuses {
					names = append(names, o.name)
				}
				fmt.Fprintf(&b, "- %s (status): %s\n", c.name, strings.Join(names, ", "))
			case "select":
				fmt.Fprintf(&b, "- %s (select): %s\n", c.name, strings.Join(c.options, ", "))
			case "date":
				fmt.Fprintf(&b, "- %s (date, YYYY-MM-DD)\n", c.name)
			default:
				fmt.Fprintf(&b, "- %s (%s)\n", c.name, c.kind)
			}
		}
	}
	config, _ := lib.pages[id]["database"].(map[string]any)
	views, _ := config["views"].([]any)
	var viewLabels []string
	var boardLines []string
	for _, item := range views {
		v, _ := item.(map[string]any)
		if v == nil {
			continue
		}
		vName := str(v["name"])
		vKind := str(v["kind"])
		if vName != "" && vKind != "" {
			viewLabels = append(viewLabels, fmt.Sprintf("%s (%s)", vName, vKind))
		}
		if vKind != "board" {
			continue
		}
		for _, c := range cols {
			if c.id == str(v["group_by"]) {
				boardLines = append(boardLines, fmt.Sprintf("Board view %q groups by %s\n", vName, c.name))
				break
			}
		}
	}
	if len(viewLabels) > 0 {
		fmt.Fprintf(&b, "Views: %s\n", strings.Join(viewLabels, ", "))
	}
	for _, line := range boardLines {
		b.WriteString(line)
	}
	return b.String()
}

// columnLookup finds a database column by name: exact trimmed match first, else
// a unique case-insensitive match.
func columnLookup(cols []column, name string) (column, error) {
	name = strings.TrimSpace(name)
	var exact []column
	var fold []column
	for _, c := range cols {
		trimmed := strings.TrimSpace(c.name)
		if trimmed == name {
			exact = append(exact, c)
		}
		if strings.EqualFold(trimmed, name) {
			fold = append(fold, c)
		}
	}
	found := exact
	if len(found) == 0 {
		found = fold
	}
	switch len(found) {
	case 1:
		return found[0], nil
	case 0:
		var names []string
		for _, c := range cols {
			names = append(names, c.name)
		}
		return column{}, fmt.Errorf("the database has no property %q; its properties are: %s", name, strings.Join(names, ", "))
	}
	return column{}, fmt.Errorf("the database has several properties called %q: rename one of them in Notes first", name)
}

// columnNamed finds the column a name means.
func columnNamed(cols []column, name string) (column, error) {
	return columnLookup(cols, name)
}

// checked is a value as it is kept for a column, or an error saying what it
// may be.
func (c column) checked(value string) (string, error) {
	value = strings.TrimSpace(value)
	switch c.kind {
	case "status":
		for _, o := range c.statuses {
			if strings.EqualFold(o.name, value) {
				return o.name, nil
			}
		}
		var names []string
		for _, o := range c.statuses {
			names = append(names, o.name)
		}
		return "", fmt.Errorf("%q is not an option of %s; the options are: %s", value, c.name, strings.Join(names, ", "))
	case "select":
		for _, o := range c.options {
			if strings.EqualFold(strings.TrimSpace(o), value) {
				return o, nil
			}
		}
		return "", fmt.Errorf("%q is not an option of %s; the options are: %s", value, c.name, strings.Join(c.options, ", "))
	case "date":
		if t, err := time.Parse("2006-01-02", value); err != nil || t.Format("2006-01-02") != value {
			return "", fmt.Errorf("%s takes a date written YYYY-MM-DD, not %q", c.name, value)
		}
		return value, nil
	case "text":
		if len([]rune(value)) > maxTextValue {
			return "", fmt.Errorf("%s takes at most %d characters", c.name, maxTextValue)
		}
		return value, nil
	}
	return "", fmt.Errorf("%s is a %s property, which cannot be set here", c.name, c.kind)
}

// propertyChanges are the values to put in a row, by the column's id: the new
// value, or nil for one that is cleared (a JSON merge patch, which leaves every
// other value as it is). Nothing is returned when any value is wrong. db is the
// database the row is in, "" for a page that is not a row.
func (lib library) propertyChanges(db string, given map[string]string) (map[string]any, error) {
	if len(given) == 0 {
		return nil, nil
	}
	if db == "" {
		return nil, errors.New("properties can only be set on a page that is a row of a database")
	}
	cols := lib.columns(db)
	out := map[string]any{}
	names := make([]string, 0, len(given))
	for name := range given {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		c, err := columnNamed(cols, name)
		if err != nil {
			return nil, err
		}
		if c.kind == "image" || c.kind == "person" {
			return nil, fmt.Errorf("%s is a %s property, which cannot be set here", c.name, c.kind)
		}
		if strings.TrimSpace(given[name]) == "" {
			out[c.id] = nil
			continue
		}
		v, err := c.checked(given[name])
		if err != nil {
			return nil, err
		}
		out[c.id] = v
	}
	return out, nil
}

// setValues are the changes without the cleared ones: what a new row holds.
func setValues(changes map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range changes {
		if v != nil {
			out[k] = v
		}
	}
	return out
}
