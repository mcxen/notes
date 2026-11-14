package notion

import (
	"html"
	"regexp"
	"strconv"
	"strings"

	"notes/internal/markdown"
)

// A page's table holds at most this many columns and rows, and its record
// stays well under the 128 KiB a record may take (web/src/core/simple-table.ts).
const (
	maxColumns   = 20
	maxRows      = 200
	maxTableSize = 96 * 1024
)

// grid is a table's cells, as words, row by row.
type grid [][]string

// pipeTable reads a Markdown table: the rows after its header row.
func (b *builder) pipeTable(parent string, rows []string) {
	var cells grid
	for _, row := range rows {
		cells = append(cells, splitPipeRow(row))
	}
	b.table(parent, cells, true, strings.Join(rows, "\n"))
}

// splitPipeRow splits a row at its bars, leaving out the ones written \|.
func splitPipeRow(row string) []string {
	row = strings.TrimSpace(row)
	row = strings.TrimPrefix(row, "|")
	row = strings.TrimSuffix(row, "|")
	var out []string
	var cur strings.Builder
	for i := 0; i < len(row); i++ {
		if row[i] == '\\' && i+1 < len(row) && row[i+1] == '|' {
			cur.WriteByte('|')
			i++
			continue
		}
		if row[i] == '|' {
			out = append(out, strings.TrimSpace(cur.String()))
			cur.Reset()
			continue
		}
		cur.WriteByte(row[i])
	}
	return append(out, strings.TrimSpace(cur.String()))
}

var (
	reRow    = regexp.MustCompile(`(?is)<tr[^>]*>(.*?)</tr>`)
	reCell   = regexp.MustCompile(`(?is)<t([dh])[^>]*>(.*?)</t[dh]>`)
	reHeadEl = regexp.MustCompile(`(?i)<thead|<th[\s>]`)
	reBreak  = regexp.MustCompile(`(?i)<br\s*/?>`)
)

// htmlTable reads an HTML table, which is how Notion writes a simple table.
func (b *builder) htmlTable(parent string, inner []string) {
	text := strings.Join(inner, "\n")
	var cells grid
	for _, row := range reRow.FindAllStringSubmatch(text, -1) {
		var line []string
		for _, cell := range reCell.FindAllStringSubmatch(row[1], -1) {
			words := reBreak.ReplaceAllString(cell[2], " ")
			words = html.UnescapeString(reTag.ReplaceAllString(words, ""))
			line = append(line, strings.TrimSpace(strings.Join(strings.Fields(words), " ")))
		}
		cells = append(cells, line)
	}
	b.table(parent, cells, reHeadEl.MatchString(text), text)
}

// table makes a table paragraph of the cells, or, for one the page cannot
// hold, a code paragraph with what the export wrote.
func (b *builder) table(parent string, cells grid, header bool, raw string) {
	columns := 0
	for _, row := range cells {
		columns = max(columns, len(row))
	}
	if columns == 0 || len(cells) == 0 {
		return
	}
	if columns > maxColumns || len(cells) > maxRows {
		b.c.warn("a table is too large for a page (more than 20 columns or 200 rows) and was kept as text")
		b.add(parent, markdown.Block{Kind: "code", Runs: []markdown.Run{{Text: raw}}})
		return
	}
	cols := make([]any, columns)
	for i := range cols {
		cols[i] = map[string]any{"id": columnID(i)}
	}
	rows := map[string]any{}
	size := 0
	for r, row := range cells {
		stored := map[string]any{}
		for c := 0; c < columns; c++ {
			text := ""
			if c < len(row) {
				text = row[c]
			}
			size += len(text) + 40
			stored[columnID(c)] = markdown.RunsToStored(b.runs(text))
		}
		rows["row"+strconv.Itoa(r+1)] = map[string]any{"order": float64(1024 * (r + 1)), "cells": stored}
	}
	if size > maxTableSize {
		b.c.warn("a table is too large for a page and was kept as text")
		b.add(parent, markdown.Block{Kind: "code", Runs: []markdown.Run{{Text: raw}}})
		return
	}
	b.add(parent, markdown.Block{Kind: "table", Table: map[string]any{"columns": cols, "rows": rows, "header_row": header}})
}

func columnID(i int) string { return "col" + strconv.Itoa(i+1) }
