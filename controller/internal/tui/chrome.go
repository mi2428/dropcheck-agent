package tui

import (
	"strings"

	"dropcheck/controller/internal/watchstate"

	"charm.land/lipgloss/v2"
)

func renderPanel(title string, width int, height int, body string) string {
	return renderPanelFocused(title, width, height, body, false)
}

func renderPanelFocused(title string, width int, height int, body string, focused bool) string {
	width = max(2, width)
	height = max(2, height)
	contentWidth := panelContentWidth(width)
	contentHeight := max(0, height-2)
	lines := panelBodyLines(body)
	border := borderStyle
	titleSty := titleStyle
	if focused {
		border = focusBorderStyle
		titleSty = focusTitleStyle
	}

	var b strings.Builder
	b.WriteString(panelTop(title, width, border, titleSty))
	for i := range contentHeight {
		b.WriteByte('\n')
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		line = fitANSI(line, contentWidth)
		anchor := panelRowAnchorStyle(i)
		b.WriteString(border.Render("│"))
		b.WriteString(anchor.Render(" "))
		b.WriteString(line)
		b.WriteString(anchor.Render(strings.Repeat(" ", max(0, contentWidth-lipgloss.Width(line)))))
		b.WriteString(anchor.Render(" "))
		b.WriteString(border.Render("│"))
	}
	b.WriteByte('\n')
	b.WriteString(border.Render("└" + strings.Repeat("─", max(0, width-2)) + "┘"))
	return b.String()
}

func panelRowAnchorStyle(row int) lipgloss.Style {
	return panelRowAnchorStyles[row%len(panelRowAnchorStyles)]
}

func panelTop(title string, width int, border lipgloss.Style, titleStyle lipgloss.Style) string {
	innerWidth := max(0, width-2)
	title = fit(title, innerWidth)
	if title == "" {
		return border.Render("┌" + strings.Repeat("─", innerWidth) + "┐")
	}
	fill := max(0, innerWidth-runeLen(title))
	return border.Render("┌") + titleStyle.Render(title) + border.Render(strings.Repeat("─", fill)+"┐")
}

func panelBodyLines(body string) []string {
	body = strings.TrimRight(body, "\n")
	if body == "" {
		return nil
	}
	return strings.Split(body, "\n")
}

func panelContentWidth(width int) int {
	return max(0, width-4)
}

func verticalSpacer(height int) string {
	return horizontalSpacer(1, height)
}

func horizontalSpacer(width int, height int) string {
	if height <= 0 {
		return ""
	}
	width = max(0, width)
	lines := make([]string, height)
	for i := range lines {
		lines[i] = valueStyle.Render(strings.Repeat(" ", width))
	}
	return strings.Join(lines, "\n")
}

func statusToken(status string) string {
	switch normalizeStatus(status) {
	case "ok":
		return "OK  "
	case "running":
		return "RUN "
	case "failed":
		return "FAIL"
	case "skipped":
		return "SKIP"
	default:
		return "WAIT"
	}
}

func normalizeStatus(status string) string {
	return watchstate.NormalizeStatus(status)
}

func checkStatusStyle(status string) lipgloss.Style {
	switch normalizeStatus(status) {
	case "failed":
		return failedStatusStyle
	case "running":
		return runningStatusStyle
	case "ok":
		return okStatusStyle
	case "skipped":
		return skippedStatusStyle
	default:
		return waitStatusStyle
	}
}

func staleStatusStyle(status string) lipgloss.Style {
	switch normalizeStatus(status) {
	case "failed":
		return staleFailedStatusStyle
	case "ok":
		return staleOKStatusStyle
	case "skipped":
		return staleSkippedStatusStyle
	default:
		return dimStyle
	}
}
