package render

import (
	"strings"
	"unicode"
)

type displayTableColumn struct {
	header   string
	maxWidth int
}

func writeDisplayTable(b *strings.Builder, columns []displayTableColumn, rows [][]string) {
	if len(columns) == 0 {
		return
	}
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
			prepared[i] = fitDisplayCell(value, column.maxWidth)
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

	writeDisplayTableRow(b, preparedHeaders, widths)
	for _, row := range preparedRows {
		writeDisplayTableRow(b, row, widths)
	}
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

	suffix := "..."
	targetWidth := maxWidth - displayWidth(suffix)
	var out strings.Builder
	width := 0
	for _, r := range cleaned {
		runeWidth := runeDisplayWidth(r)
		if width+runeWidth > targetWidth {
			break
		}
		out.WriteRune(r)
		width += runeWidth
	}
	out.WriteString(suffix)
	return out.String()
}

func cleanDisplayCell(value string) string {
	value = strings.ReplaceAll(value, "\t", " ")
	value = strings.ReplaceAll(value, "\r", " ")
	return strings.ReplaceAll(value, "\n", " ")
}

func padDisplayEnd(value string, width int) string {
	padding := width - displayWidth(value)
	if padding <= 0 {
		return value
	}
	return value + strings.Repeat(" ", padding)
}

func displayWidth(value string) int {
	width := 0
	for _, r := range value {
		width += runeDisplayWidth(r)
	}
	return width
}

func runeDisplayWidth(r rune) int {
	if unicode.IsControl(r) || unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Mc, r) {
		return 0
	}
	if isWideRune(r) {
		return 2
	}
	return 1
}

func isWideRune(r rune) bool {
	return r >= 0x1100 && (r <= 0x115f ||
		r == 0x2329 ||
		r == 0x232a ||
		(r >= 0x2e80 && r <= 0xa4cf && r != 0x303f) ||
		(r >= 0xac00 && r <= 0xd7a3) ||
		(r >= 0xf900 && r <= 0xfaff) ||
		(r >= 0xfe10 && r <= 0xfe19) ||
		(r >= 0xfe30 && r <= 0xfe6f) ||
		(r >= 0xff00 && r <= 0xff60) ||
		(r >= 0xffe0 && r <= 0xffe6))
}
