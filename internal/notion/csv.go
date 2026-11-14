package notion

import (
	"bytes"
	"encoding/csv"
	"strings"
	"time"
)

// table is a database's CSV: its header, and a row of cells for each page.
type csvTable struct {
	header []string
	rows   [][]string
}

func readCSV(data []byte) (csvTable, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	records, err := r.ReadAll()
	if err != nil {
		return csvTable{}, err
	}
	if len(records) == 0 {
		return csvTable{}, nil
	}
	t := csvTable{header: records[0]}
	for _, row := range records[1:] {
		if len(strings.Join(row, "")) == 0 {
			continue
		}
		for len(row) < len(t.header) {
			row = append(row, "")
		}
		t.rows = append(t.rows, row)
	}
	return t, nil
}

// A property's kind is not in the CSV: it is read from the values. A column
// of days is a Date; a column with a few values that repeat is a Select;
// anything else is Text.
const (
	maxOptions = 12
	minRows    = 3
)

var dateLayouts = []string{
	"January 2, 2006 3:04 PM", "January 2, 2006 15:04", "January 2, 2006",
	"Jan 2, 2006 3:04 PM", "Jan 2, 2006",
	"2006-01-02 15:04", "2006-01-02T15:04:05Z07:00", "2006-01-02", "2006/01/02", "2006/1/2 15:04", "2006/1/2",
	"2006年1月2日 15:04", "2006年1月2日",
	"02/01/2006", "1/2/2006",
}

// day reads a date as Notion writes it ("November 3, 2025 10:30 AM", a range
// "November 3, 2025 → November 5, 2025") and returns its first day as
// 2006-01-02.
func day(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if first, _, found := strings.Cut(value, "→"); found {
		value = strings.TrimSpace(first)
	}
	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, value); err == nil {
			return t.Format("2006-01-02"), true
		}
	}
	return "", false
}

// kindOf says what a column holds, and its options when it is a Select.
func kindOf(rows [][]string, col int) (kind string, options []string) {
	var values []string
	for _, row := range rows {
		if v := strings.TrimSpace(row[col]); v != "" {
			values = append(values, v)
		}
	}
	if len(values) == 0 {
		return "text", nil
	}
	dates := true
	for _, v := range values {
		if _, ok := day(v); !ok {
			dates = false
			break
		}
	}
	if dates {
		return "date", nil
	}
	seen := map[string]bool{}
	for _, v := range values {
		if strings.ContainsAny(v, ",\n") || len([]rune(v)) > 40 {
			return "text", nil
		}
		if !seen[v] {
			seen[v] = true
			options = append(options, v)
		}
	}
	if len(rows) >= minRows && len(options) <= maxOptions && len(options) < len(values) {
		return "select", options
	}
	return "text", nil
}
