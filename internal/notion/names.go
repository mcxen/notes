// Package notion reads a Notion export (the Markdown & CSV zip) and writes it
// into a vault: pages under pages, folders as parents, databases from their
// CSV files, links between pages resolved, pictures and files kept as the
// vault keeps them.
package notion

import (
	"path"
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// Notion ends the name of every page and folder it exports with the page's
// id: 32 hex digits, or a uuid.
var idSuffix = regexp.MustCompile(`(?i)[\s_-]*(?:[0-9a-f]{32}|[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12})$`)

var idInText = regexp.MustCompile(`(?i)([0-9a-f]{32}|[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12})`)

// stripID removes the id from the end of a name.
func stripID(name string) string {
	return strings.TrimSpace(idSuffix.ReplaceAllString(name, ""))
}

// idOf is the id a name ends with, as 32 lower-case hex digits, or "".
func idOf(name string) string {
	m := idSuffix.FindString(name)
	if m == "" {
		return ""
	}
	return normalizeID(idInText.FindString(m))
}

// idIn is the last id found anywhere in a text, such as a notion.so address.
func idIn(text string) string {
	all := idInText.FindAllString(text, -1)
	if len(all) == 0 {
		return ""
	}
	return normalizeID(all[len(all)-1])
}

func normalizeID(id string) string {
	return strings.ToLower(strings.ReplaceAll(id, "-", ""))
}

// stem is a file's name without its extension.
func stem(name string) string {
	return strings.TrimSuffix(name, path.Ext(name))
}

// clean is a path as the export's own: slashes, composed Unicode (macOS
// writes names decomposed, Notion links write them composed).
func clean(p string) string {
	return norm.NFC.String(path.Clean(strings.ReplaceAll(p, `\`, "/")))
}
