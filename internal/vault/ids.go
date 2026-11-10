package vault

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// The ids of a page's records are `<length>_<page id>_<rest>`, length
// prefixed so a page id and what follows it never run into each other. It is
// the same scheme as web/src/platform/record-ids.ts, so both hosts name the
// same record the same way.

var (
	safeIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)
	partSuffix    = regexp.MustCompile(`^(.*)\.part\.(\d+)$`)
)

// ValidID reports whether id may name a page, a version or a file. Ids are
// used in file names, so only letters, digits, underscores and dashes pass.
func ValidID(id string) bool { return safeIDPattern.MatchString(id) }

func pageScopedID(pageID, rest string) string {
	return fmt.Sprintf("%d_%s_%s", len(pageID), pageID, rest)
}

// parsePageScopedID splits `<length>_<page id>_<rest>`. The page id must be
// exactly as long as its prefix says and the rest must not be empty.
func parsePageScopedID(id string) (pageID, rest string, ok bool) {
	under := strings.IndexByte(id, '_')
	if under <= 0 {
		return "", "", false
	}
	length := id[:under]
	for _, c := range length {
		if c < '0' || c > '9' {
			return "", "", false
		}
	}
	n, err := strconv.Atoi(length)
	if err != nil {
		return "", "", false
	}
	start := under + 1
	end := start + n
	if end+1 >= len(id) || id[end] != '_' {
		return "", "", false
	}
	return id[start:end], id[end+1:], true
}

// BlockRecordID names a paragraph's record.
func BlockRecordID(pageID, blockID string) string { return pageScopedID(pageID, blockID) }

// ParseBlockRecordID returns the page and block a paragraph's record names.
func ParseBlockRecordID(id string) (pageID, blockID string, ok bool) {
	pageID, blockID, ok = parsePageScopedID(id)
	return pageID, blockID, ok && blockID != ""
}

// HistoryRecordID names a part of a page's checkpoint. Part 0 has no suffix.
func HistoryRecordID(pageID, versionID string, part int) string {
	suffix := ""
	if part > 0 {
		suffix = fmt.Sprintf(".part.%d", part)
	}
	return pageScopedID(pageID, versionID+suffix)
}

// ParseHistoryRecordID returns the page, checkpoint and part a history
// record's id names.
func ParseHistoryRecordID(id string) (pageID, versionID string, part int, ok bool) {
	pageID, rest, ok := parsePageScopedID(id)
	if !ok {
		return "", "", 0, false
	}
	versionID = rest
	if match := partSuffix.FindStringSubmatch(rest); match != nil {
		versionID = match[1]
		part, _ = strconv.Atoi(match[2])
	}
	if versionID == "" {
		return "", "", 0, false
	}
	return pageID, versionID, part, true
}
