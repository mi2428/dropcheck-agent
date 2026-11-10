package render

import (
	"strings"

	"github.com/mi2428/dropcheck-agent/controller/internal/safevalue"

	"github.com/charmbracelet/x/ansi"
)

type displayTableColumn struct {
	header  string
	numeric bool
}

func writeDisplayTable(b *strings.Builder, columns []displayTableColumn, rows [][]string, availableWidth ...int) {
	if len(columns) == 0 {
		return
	}
	writeTableBoundary(b)
	preparedHeaders := make([]string, len(columns))
	for i, column := range columns {
		preparedHeaders[i] = cleanDisplayCell(column.header)
	}
	preparedRows := make([][]string, 0, len(rows))
	for _, row := range rows {
		prepared := make([]string, len(columns))
		for i := range columns {
			value := ""
			if i < len(row) {
				value = row[i]
			}
			prepared[i] = cleanDisplayCell(value)
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

func cleanDisplayCell(value string) string {
	return safevalue.Normalize(value)
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
