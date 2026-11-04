package render

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

type displayTableColumn struct {
	header   string
	maxWidth int
	numeric  bool
}

func (column displayTableColumn) fitValue(value string) string {
	if column.header == "SSID" {
		return fitDisplayCell(value, column.maxWidth)
	}
	return cleanDisplayCell(value)
}

func writeDisplayTable(b *strings.Builder, columns []displayTableColumn, rows [][]string, availableWidth ...int) {
	if len(columns) == 0 {
		return
	}
	writeTableBoundary(b)
	preparedHeaders := make([]string, len(columns))
	for i, column := range columns {
		preparedHeaders[i] = fitDisplayCell(column.header, column.maxWidth)
	}
	preparedRows := make([][]string, 0, len(rows))
	for _, row := range rows {
		prepared := make([]string, len(columns))
		for i, column := range columns {
			value := ""
			if i < len(row) {
				value = row[i]
			}
			prepared[i] = column.fitValue(value)
		}
		preparedRows = append(preparedRows, prepared)
	}

	widths := make([]int, len(columns))
	for i := range columns {
		widths[i] = displayWidth(preparedHeaders[i])
		for _, row := range preparedRows {
			if width := displayWidth(row[i]); width > widths[i] {
				widths[i] = width
			}
		}
	}
	if len(availableWidth) > 0 && availableWidth[0] > 0 {
		total := 2 * (len(columns) - 1)
		for _, width := range widths {
			total += width
		}
		if total > availableWidth[0] {
			if len(preparedRows) == 0 {
				for _, header := range preparedHeaders {
					b.WriteString(header + "\n")
				}
			}
			for _, row := range preparedRows {
				for i, column := range columns {
					writeRecord(b, column.header, row[i])
				}
				writeBlankLine(b)
			}
			writeTableBoundary(b)
			return
		}
	}

	writeDisplayTableRow(b, preparedHeaders, widths)
	for _, row := range preparedRows {
		for i, column := range columns {
			if column.numeric {
				row[i] = strings.Repeat(" ", widths[i]-displayWidth(row[i])) + row[i]
			}
		}
		writeDisplayTableRow(b, row, widths)
	}
	writeTableBoundary(b)
}

func writeTableBoundary(b *strings.Builder) {
	text := strings.TrimRight(b.String(), "\n")
	if text == "" {
		b.Reset()
		b.WriteByte('\n')
		return
	}
	if b.Len() > len(text)+2 {
		b.Reset()
		b.WriteString(text)
	}
	writeBlankLine(b)
}

func writeDisplayTableRow(b *strings.Builder, row []string, widths []int) {
	for i, value := range row {
		if i > 0 {
			b.WriteString("  ")
		}
		if i == len(row)-1 {
			b.WriteString(value)
			continue
		}
		b.WriteString(padDisplayEnd(value, widths[i]))
	}
	b.WriteByte('\n')
}

func fitDisplayCell(value string, maxWidth int) string {
	cleaned := cleanDisplayCell(value)
	if maxWidth <= 0 || displayWidth(cleaned) <= maxWidth {
		return cleaned
	}
	if maxWidth <= 3 {
		return strings.Repeat(".", maxWidth)
	}

	return ansi.Truncate(cleaned, maxWidth, "...")
}

func cleanDisplayCell(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '\u061c' || r == '\u200b' || r == '\u200e' || r == '\u200f' || r == '\ufeff' || r == '\u2028' || r == '\u2029' || (r >= '\u202a' && r <= '\u202e') || (r >= '\u2060' && r <= '\u2069') {
			return ' '
		}
		return r
	}, ansi.Strip(value))
}

func padDisplayEnd(value string, width int) string {
	padding := width - displayWidth(value)
	if padding <= 0 {
		return value
	}
	return value + strings.Repeat(" ", padding)
}

func displayWidth(value string) int {
	return ansi.StringWidth(value)
}
