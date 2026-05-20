package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"dropcheck/controller/internal/watch"

	"charm.land/lipgloss/v2"
)

func (m model) passingChecksView(width int, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	rows := m.filteredPassingCheckSummaries()
	maxCount := maxPassingCheckSummaryCount(rows)
	layout := passingCheckBarListLayout(rows, max(1, width-2), m.MultiAgent)
	selected := clamp(m.passingCheckCursor, 0, max(0, len(rows)-1))
	now := m.currentTime()
	sparkHeight := summarySparklineHeight(height)
	if sparkHeight > 0 {
		sparkHeight = min(height, sparkHeight+1)
	}
	tableHeight := max(0, height-sparkHeight)
	lines := make([]tableLine, 0, len(rows)*2+1)
	selectedLine := -1
	lines = append(lines, tableLine{Text: "  " + barListHeader(passingCheckListHeaderColumns(m.MultiAgent), passingCheckListRightHeaderColumns(), layout), Style: summaryTableHeaderStyle, Fill: true})
	for index, item := range rows {
		line := barListLine(passingCheckListColumns(item, m.MultiAgent), passingCheckListRightColumns(item), item.Count, maxCount, layout)
		style := orangeSummaryPanelRowStyle(item.Last, now)
		fill := false
		if m.focus == focusPassingChecks && index == selected {
			style = selectedStyle
			fill = true
			selectedLine = len(lines)
		}
		lines = append(lines, tableLine{Text: "  " + line, Style: style, Fill: fill})
	}
	if len(rows) == 0 {
		lines = append(lines, tableLine{Text: "  " + m.emptyPanelText(focusPassingChecks, "passing checks"), Style: summaryStaleRowStyle})
	}
	table := renderTableLines(lines, width, tableHeight, selectedLine)
	sparkline := m.summarySparklineView("passing checks", m.passingCheckEventTimes(), width, sparkHeight, summaryGraphStyle)
	return pinBottom(table, sparkline, width, height)
}

func (m model) failedChecksView(width int, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	rows := m.filteredFailedCheckSummaries()
	maxCount := maxFailedCheckSummaryCount(rows)
	layout := failedCheckBarListLayout(rows, max(1, width-2), m.MultiAgent)
	selected := clamp(m.failedCheckCursor, 0, max(0, len(rows)-1))
	now := m.currentTime()
	sparkHeight := summarySparklineHeight(height)
	if sparkHeight > 0 {
		sparkHeight = min(height, sparkHeight+1)
	}
	tableHeight := max(0, height-sparkHeight)
	lines := make([]tableLine, 0, len(rows)*2+1)
	selectedLine := -1
	lines = append(lines, tableLine{Text: "  " + barListHeader(failedCheckListHeaderColumns(m.MultiAgent), failedCheckListRightHeaderColumns(), layout), Style: summaryTableHeaderStyle, Fill: true})
	for index, item := range rows {
		line := barListLine(failedCheckListColumns(item, m.MultiAgent), failedCheckListRightColumns(item), item.Count, maxCount, layout)
		style := orangeSummaryPanelRowStyle(item.Last, now)
		fill := false
		if m.focus == focusFailedChecks && index == selected {
			style = selectedStyle
			fill = true
			selectedLine = len(lines)
		}
		lines = append(lines, tableLine{Text: "  " + line, Style: style, Fill: fill})
	}
	if len(rows) == 0 {
		lines = append(lines, tableLine{Text: "  " + m.emptyPanelText(focusFailedChecks, "failed checks"), Style: summaryStaleRowStyle})
	}
	table := renderTableLines(lines, width, tableHeight, selectedLine)
	sparkline := m.summarySparklineView("failed checks", m.failedCheckEventTimes(), width, sparkHeight, summaryGraphStyle)
	return pinBottom(table, sparkline, width, height)
}

func (m model) failureHotspotsView(width int, height int) string {
	if m.failureHotspotMode == failureHotspotModeCauses {
		return m.failureCausesView(width, height)
	}
	if width <= 0 || height <= 0 {
		return ""
	}
	rows := m.filteredFailureHotspotRows()
	layout := failureHotspotListLayout(rows, max(1, width-2))
	selected := clamp(m.failureHotspotCursor, 0, max(0, len(rows)-1))
	lines := make([]tableLine, 0, len(rows)+1)
	selectedLine := -1
	lines = append(lines, tableLine{Text: "  " + barListHeader(failureHotspotListHeaderColumns(), failureHotspotListRightHeaderColumns(), layout), Style: summaryTableHeaderStyle, Fill: true})
	for index, item := range rows {
		line := barListLine(failureHotspotListColumns(item), failureHotspotListRightColumns(item), item.FailCount, 1, layout)
		style := failureSummaryPanelRowStyle(item.Last, m.currentTime())
		fill := false
		if m.focus == focusFailureHotspots && index == selected {
			style = selectedStyle
			fill = true
			selectedLine = len(lines)
		}
		lines = append(lines, tableLine{Text: "  " + line, Style: style, Fill: fill})
	}
	if len(rows) == 0 {
		lines = append(lines, tableLine{Text: "  " + m.emptyPanelText(focusFailureHotspots, "failure hotspots"), Style: summaryStaleRowStyle})
	}
	return renderTableLines(lines, width, height, selectedLine)
}

func (m model) failureCausesView(width int, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	rows := m.filteredFailureCauseRows()
	layout := failureCauseListLayout(rows, max(1, width-2))
	selected := clamp(m.failureHotspotCursor, 0, max(0, len(rows)-1))
	lines := make([]tableLine, 0, len(rows)+1)
	selectedLine := -1
	lines = append(lines, tableLine{Text: "  " + barListHeader(failureCauseListHeaderColumns(), failureCauseListRightHeaderColumns(), layout), Style: summaryTableHeaderStyle, Fill: true})
	for index, item := range rows {
		line := barListLine(failureCauseListColumns(item, failureCauseTargetColumnWidth(layout)), failureCauseListRightColumns(item), item.TargetCount, 1, layout)
		style := failureSummaryPanelRowStyle(item.Last, m.currentTime())
		fill := false
		if m.focus == focusFailureHotspots && index == selected {
			style = selectedStyle
			fill = true
			selectedLine = len(lines)
		}
		lines = append(lines, tableLine{Text: "  " + line, Style: style, Fill: fill})
	}
	if len(rows) == 0 {
		lines = append(lines, tableLine{Text: "  " + m.emptyPanelText(focusFailureHotspots, "failure causes"), Style: summaryStaleRowStyle})
	}
	return renderTableLines(lines, width, height, selectedLine)
}

func (m model) failureHotspotPanelsView(width int, height int) string {
	if !m.failureHotspotPanelsSplit() {
		return renderPanelFocused(m.failureHotspotPanelTitle(), width, height, m.failureHotspotsView(panelContentWidth(width), height-2), m.focus == focusFailureHotspots)
	}
	agents := m.failureHotspotAgents()
	heights := splitHeights(height, len(agents))
	panels := make([]string, 0, len(agents))
	for i, agent := range agents {
		title := panelTitleWithLabel(m.failureHotspotPanelTitle(), agentLabel(agent), width)
		panelHeight := heights[i]
		focused := m.failureHotspotPanelHasFocus(agent)
		panels = append(panels, renderPanelFocused(title, width, panelHeight, m.failureHotspotsViewForAgent(agent, panelContentWidth(width), panelHeight-2), focused))
	}
	return lipgloss.JoinVertical(lipgloss.Left, panels...)
}

func (m model) failureHotspotPanelTitle() string {
	if m.failureHotspotMode == failureHotspotModeTargets {
		return "Failure Hotspots"
	}
	return "Failure Causes"
}

func (m model) failureHotspotPanelsSplit() bool {
	if !m.MultiAgent || !m.failureHotspotsVisible() {
		return false
	}
	agents := m.failureHotspotAgents()
	if len(agents) <= 1 {
		return false
	}
	height := m.height
	if height <= 0 {
		height = 32
	}
	width := m.width
	if width <= 0 {
		width = 120
	}
	bodyHeight := max(4, height-2)
	roundTimelineHeight, checkStatusHeight, _, _ := m.dashboardPanelHeights(bodyHeight, panelContentWidth(width))
	lowerHeight := max(0, bodyHeight-checkStatusHeight-roundTimelineHeight)
	return lowerHeight >= 6 && lowerHeight >= len(agents)*3
}

func (m model) failureHotspotsViewForAgent(agent watch.AgentSnapshot, width int, height int) string {
	if m.failureHotspotMode == failureHotspotModeCauses {
		return m.failureCausesViewForAgent(agent, width, height)
	}
	if width <= 0 || height <= 0 {
		return ""
	}
	rows := m.filteredFailureHotspotRowsForAgent(agent)
	items := make([]failureHotspotSummary, 0, len(rows))
	for _, row := range rows {
		items = append(items, row.Item)
	}
	layout := failureHotspotListLayout(items, max(1, width-2))
	lines := make([]tableLine, 0, len(rows)+1)
	selectedLine := -1
	lines = append(lines, tableLine{Text: "  " + barListHeader(failureHotspotListHeaderColumns(), failureHotspotListRightHeaderColumns(), layout), Style: summaryTableHeaderStyle, Fill: true})
	for _, row := range rows {
		item := row.Item
		line := barListLine(failureHotspotListColumns(item), failureHotspotListRightColumns(item), item.FailCount, 1, layout)
		style := failureSummaryPanelRowStyle(item.Last, m.currentTime())
		fill := false
		if m.failureHotspotPanelHasFocus(agent) && row.Index == m.failureHotspotCursor {
			style = selectedStyle
			fill = true
			selectedLine = len(lines)
		}
		lines = append(lines, tableLine{Text: "  " + line, Style: style, Fill: fill})
	}
	if len(rows) == 0 {
		lines = append(lines, tableLine{Text: "  " + m.emptyPanelText(focusFailureHotspots, "failure hotspots"), Style: summaryStaleRowStyle})
	}
	return renderTableLines(lines, width, height, selectedLine)
}

func (m model) failureCausesViewForAgent(agent watch.AgentSnapshot, width int, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	rows := m.filteredFailureCauseRowsForAgent(agent)
	items := make([]failureCauseSummary, 0, len(rows))
	for _, row := range rows {
		items = append(items, row.Item)
	}
	layout := failureCauseListLayout(items, max(1, width-2))
	lines := make([]tableLine, 0, len(rows)+1)
	selectedLine := -1
	lines = append(lines, tableLine{Text: "  " + barListHeader(failureCauseListHeaderColumns(), failureCauseListRightHeaderColumns(), layout), Style: summaryTableHeaderStyle, Fill: true})
	for _, row := range rows {
		item := row.Item
		line := barListLine(failureCauseListColumns(item, failureCauseTargetColumnWidth(layout)), failureCauseListRightColumns(item), item.TargetCount, 1, layout)
		style := failureSummaryPanelRowStyle(item.Last, m.currentTime())
		fill := false
		if m.failureHotspotPanelHasFocus(agent) && row.Index == m.failureHotspotCursor {
			style = selectedStyle
			fill = true
			selectedLine = len(lines)
		}
		lines = append(lines, tableLine{Text: "  " + line, Style: style, Fill: fill})
	}
	if len(rows) == 0 {
		lines = append(lines, tableLine{Text: "  " + m.emptyPanelText(focusFailureHotspots, "failure causes"), Style: summaryStaleRowStyle})
	}
	return renderTableLines(lines, width, height, selectedLine)
}

func (m model) failureHotspotPanelHasFocus(agent watch.AgentSnapshot) bool {
	if m.focus != focusFailureHotspots || !m.failureHotspotPanelsSplit() {
		return false
	}
	return roundAgentKey(agent) == m.currentHotspotAgentKey()
}

func (m model) failureHotspotAgents() []watch.AgentSnapshot {
	return m.outcomeAgents(m.outcomeEvents())
}

func (m model) emptyPanelText(panel focusPanel, noun string) string {
	if strings.TrimSpace(m.panelFilterQuery(panel)) != "" {
		return "no " + noun + " match"
	}
	return "no " + noun
}

func summarySparklineHeight(height int) int {
	if height >= 10 {
		return summarySparklineRows
	}
	if height >= 8 {
		return 4
	}
	if height >= 6 {
		return 3
	}
	return 0
}

func (m model) summarySparklineView(_ string, times []time.Time, width int, height int, style lipgloss.Style) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	histogram := recentEventHistogram(times, width, summarySparklineWindow, m.currentTime())
	plotHeight := summarySparklinePlotHeight(height)
	eventsPerRow := sparklineEventsPerRow(histogram.Max, plotHeight)
	scaleText := ""
	if eventsPerRow > 1 {
		scaleText = fmt.Sprintf(" scale=%d/row", eventsPerRow)
	}
	headerPlain := fmt.Sprintf("timeline window=last=%s count=%d peak=%d%s", formatBucketDuration(summarySparklineWindow), histogram.Count, histogram.Max, scaleText)
	header := summarySparklineLabelStyle.Render("timeline ") +
		summarySparklineLabelStyle.Render("window=") +
		summaryValueStyle.Render("last="+formatBucketDuration(summarySparklineWindow)) +
		summarySparklineLabelStyle.Render(" count=") +
		summaryValueStyle.Render(fmt.Sprint(histogram.Count)) +
		summarySparklineLabelStyle.Render(" peak=") +
		summaryValueStyle.Render(fmt.Sprint(histogram.Max))
	if scaleText != "" {
		header += summarySparklineLabelStyle.Render(" scale=") + summaryValueStyle.Render(fmt.Sprintf("%d/row", eventsPerRow))
	}
	if height == 1 {
		return fitANSI(header, width)
	}
	if lipgloss.Width(headerPlain) > width {
		header = summaryValueStyle.Render(fit(headerPlain, width))
	}
	lines := make([]string, 0, height)
	if height >= 5 {
		lines = append(lines, "")
	}
	lines = append(lines, fitANSI(header, width))
	if height >= 4 {
		lines = append(lines, "")
	}
	lines = append(lines, renderSparkline(histogram.Counts, histogram.Max, width, plotHeight, style)...)
	lines = append(lines, summarySparklineAxis(width, summarySparklineWindow))
	return strings.Join(lines[:min(len(lines), height)], "\n")
}

func summarySparklinePlotHeight(height int) int {
	if height <= 2 {
		return 1
	}
	reserved := 2 // header + axis
	if height >= 5 {
		reserved++ // spacer above header
	}
	if height >= 4 {
		reserved++ // spacer between header and graph
	}
	return max(1, height-reserved)
}

func (m model) passingCheckEventTimes() []time.Time {
	times := make([]time.Time, 0, len(m.PassingChecks))
	for _, passingCheck := range m.PassingChecks {
		times = append(times, passingCheck.When)
	}
	sort.SliceStable(times, func(i, j int) bool {
		return times[i].Before(times[j])
	})
	return times
}

func (m model) failedCheckEventTimes() []time.Time {
	times := make([]time.Time, 0, len(m.FailedChecks))
	for _, failedCheck := range m.FailedChecks {
		times = append(times, failedCheck.When)
	}
	sort.SliceStable(times, func(i, j int) bool {
		return times[i].Before(times[j])
	})
	return times
}

func pinBottom(top string, bottom string, width int, height int) string {
	if height <= 0 {
		return ""
	}
	topLines := panelBodyLines(top)
	bottomLines := panelBodyLines(bottom)
	if len(bottomLines) > height {
		bottomLines = bottomLines[len(bottomLines)-height:]
	}
	maxTop := max(0, height-len(bottomLines))
	if len(topLines) > maxTop {
		topLines = topLines[:maxTop]
	}
	lines := make([]string, 0, height)
	lines = append(lines, topLines...)
	for len(lines) < maxTop {
		lines = append(lines, "")
	}
	lines = append(lines, bottomLines...)
	for len(lines) < height {
		lines = append(lines, "")
	}
	for i := range lines {
		lines[i] = fitANSI(lines[i], width)
	}
	return strings.Join(lines, "\n")
}

type tableLine struct {
	Text  string
	Style lipgloss.Style
	Fill  bool
}

type barListLayout struct {
	ColumnWidths []int
	BarWidth     int
	RightWidths  []int
	RightGap     int
}

const listColumnGap = 2

func orangeSummaryPanelRowStyle(last time.Time, now time.Time) lipgloss.Style {
	return recencyPanelRowStyle(last, now, summaryFreshRowStyle, summaryWarmRowStyle, summaryStaleRowStyle)
}

func failureSummaryPanelRowStyle(last time.Time, now time.Time) lipgloss.Style {
	return recencyPanelRowStyle(last, now, summaryFailureRowStyle, summaryFailureRowStyle, staleFailedStatusStyle)
}

func recencyPanelRowStyle(last time.Time, now time.Time, fresh lipgloss.Style, warm lipgloss.Style, stale lipgloss.Style) lipgloss.Style {
	if last.IsZero() {
		return summaryStaleRowStyle
	}
	if now.IsZero() {
		now = time.Now()
	}
	age := now.Sub(last)
	if age < 0 {
		age = 0
	}
	switch {
	case age <= recencyFreshWindow:
		return fresh
	case age <= recencyWarmWindow:
		return warm
	default:
		return stale
	}
}

func passingCheckBarListLayout(rows []passingCheckSummary, width int, multiAgent bool) barListLayout {
	columns := [][]string{passingCheckListHeaderColumns(multiAgent)}
	rights := [][]string{passingCheckListRightHeaderColumns()}
	for _, row := range rows {
		columns = append(columns, passingCheckListColumns(row, multiAgent))
		rights = append(rights, passingCheckListRightColumns(row))
	}
	return newPlainListLayout(width, columns, rights)
}

func failedCheckBarListLayout(rows []failedCheckSummary, width int, multiAgent bool) barListLayout {
	columns := [][]string{failedCheckListHeaderColumns(multiAgent)}
	rights := [][]string{failedCheckListRightHeaderColumns()}
	for _, row := range rows {
		columns = append(columns, failedCheckListColumns(row, multiAgent))
		rights = append(rights, failedCheckListRightColumns(row))
	}
	return newPlainListLayout(width, columns, rights)
}

func failureHotspotListLayout(rows []failureHotspotSummary, width int) barListLayout {
	columns := [][]string{failureHotspotListHeaderColumns()}
	rights := [][]string{failureHotspotListRightHeaderColumns()}
	for _, row := range rows {
		columns = append(columns, failureHotspotListColumns(row))
		rights = append(rights, failureHotspotListRightColumns(row))
	}
	return newPlainListLayout(width, columns, rights)
}

func failureCauseListLayout(rows []failureCauseSummary, width int) barListLayout {
	columns := [][]string{failureCauseListHeaderColumns()}
	rights := [][]string{failureCauseListRightHeaderColumns()}
	for _, row := range rows {
		columns = append(columns, failureCauseListColumns(row, 0))
		rights = append(rights, failureCauseListRightColumns(row))
	}
	return newPlainListLayout(width, columns, rights)
}

func newPlainListLayout(width int, columns [][]string, rights [][]string) barListLayout {
	return newListLayout(width, columns, rights, false)
}

func newBarListLayout(width int, columns [][]string, rights [][]string) barListLayout {
	return newListLayout(width, columns, rights, true)
}

func newListLayout(width int, columns [][]string, rights [][]string, includeBar bool) barListLayout {
	if width <= 0 {
		return barListLayout{}
	}
	emptyRows := len(columns) <= 1
	columnWidths := maxColumnWidths(columns)
	rightWidths := maxColumnWidths(rights)
	rightWidth := joinedWidths(rightWidths)
	barWidth := 0
	if includeBar {
		barWidth = countBarWidth(width)
		barWidth = min(barWidth, max(4, width-joinedWidths(columnWidths)-rightWidth-listColumnGap*2))
	}
	gapWidth := listColumnGap
	if includeBar {
		gapWidth = listColumnGap*2 + barWidth
	}
	leftBudget := max(4, width-rightWidth-gapWidth)
	columnWidths = shrinkWidths(columnWidths, leftBudget, 4)
	if emptyRows {
		columnWidths = expandWidthsEvenly(columnWidths, leftBudget)
	}
	if includeBar {
		if joinedWidths(columnWidths)+barWidth+rightWidth+listColumnGap*2 > width {
			barWidth = max(4, width-joinedWidths(columnWidths)-rightWidth-listColumnGap*2)
		}
		if joinedWidths(columnWidths)+rightWidth+listColumnGap*2 < width {
			barWidth = max(4, width-joinedWidths(columnWidths)-rightWidth-listColumnGap*2)
		}
	} else {
		budget := max(1, width-joinedWidths(rightWidths)-listColumnGap)
		columnWidths = shrinkWidths(columnWidths, budget, 4)
		if emptyRows {
			columnWidths = expandWidthsEvenly(columnWidths, budget)
		}
	}
	rightGap := listColumnGap
	if !includeBar {
		rightGap = max(listColumnGap, width-joinedWidths(columnWidths)-joinedWidths(rightWidths))
	}
	return barListLayout{ColumnWidths: columnWidths, BarWidth: barWidth, RightWidths: rightWidths, RightGap: rightGap}
}

func expandWidthsEvenly(widths []int, budget int) []int {
	out := append([]int(nil), widths...)
	if len(out) == 0 || budget <= 0 || joinedWidths(out) >= budget {
		return out
	}
	available := max(0, budget-listColumnGap*(len(out)-1))
	base := available / len(out)
	remainder := available % len(out)
	for i := range out {
		target := base
		if i < remainder {
			target++
		}
		if target > out[i] {
			out[i] = target
		}
	}
	for joinedWidths(out) < budget {
		for i := range out {
			if joinedWidths(out) >= budget {
				break
			}
			out[i]++
		}
	}
	if joinedWidths(out) > budget {
		out = shrinkWidths(out, budget, 1)
	}
	return out
}

func maxColumnWidths(rows [][]string) []int {
	count := 0
	for _, row := range rows {
		count = max(count, len(row))
	}
	widths := make([]int, count)
	for _, row := range rows {
		for i, value := range row {
			widths[i] = max(widths[i], lipgloss.Width(value))
		}
	}
	return widths
}

func shrinkWidths(widths []int, budget int, minWidth int) []int {
	out := append([]int(nil), widths...)
	for len(out) > 0 && joinedWidths(out) > budget {
		index := 0
		for i := range out {
			if out[i] > out[index] {
				index = i
			}
		}
		if out[index] <= minWidth {
			break
		}
		out[index]--
	}
	return out
}

func joinedWidths(widths []int) int {
	total := 0
	for _, width := range widths {
		total += width
	}
	if len(widths) > 1 {
		total += listColumnGap * (len(widths) - 1)
	}
	return total
}

func barListHeader(columns []string, rightColumns []string, layout barListLayout) string {
	if len(layout.ColumnWidths) == 0 {
		return ""
	}
	left := renderBarListColumns(columns, layout.ColumnWidths, false)
	right := renderBarListColumns(rightColumns, layout.RightWidths, true)
	if layout.BarWidth <= 0 {
		return joinListColumns(left, right, layout.RightGap)
	}
	return left + strings.Repeat(" ", listColumnGap) + strings.Repeat(" ", layout.BarWidth) + strings.Repeat(" ", listColumnGap) + right
}

func barListLine(columns []string, rightColumns []string, count int, maxCount int, layout barListLayout) string {
	if len(layout.ColumnWidths) == 0 {
		return ""
	}
	left := renderBarListColumns(columns, layout.ColumnWidths, false)
	right := renderBarListColumns(rightColumns, layout.RightWidths, true)
	if layout.BarWidth <= 0 {
		return joinListColumns(left, right, layout.RightGap)
	}
	return left + strings.Repeat(" ", listColumnGap) + countBar(count, maxCount, layout.BarWidth) + strings.Repeat(" ", listColumnGap) + right
}

func joinListColumns(left string, right string, gap int) string {
	if right == "" {
		return left
	}
	if left == "" {
		return right
	}
	return left + strings.Repeat(" ", max(listColumnGap, gap)) + right
}

func renderBarListColumns(values []string, widths []int, rightAlign bool) string {
	parts := make([]string, 0, len(widths))
	for i, width := range widths {
		value := ""
		if i < len(values) {
			value = values[i]
		}
		if rightAlign {
			parts = append(parts, padLeftVisible(value, width))
		} else {
			parts = append(parts, padVisible(value, width))
		}
	}
	return strings.Join(parts, strings.Repeat(" ", listColumnGap))
}

func countBarWidth(width int) int {
	return clamp(width/8, 4, 18)
}

func passingCheckListHeaderColumns(multiAgent bool) []string {
	if multiAgent {
		return []string{"Device", "Target", "Check"}
	}
	return []string{"Check"}
}

func passingCheckListColumns(item passingCheckSummary, multiAgent bool) []string {
	device := agentDeviceLabel(item.Agent)
	target := compactTargetLabel(firstNonEmpty(item.Target.Name, item.Target.SSID, item.Target.BSSID, "-"), 18)
	step := displayCheckName(firstNonEmpty(item.Step.Name, item.Step.Type, "step"))
	if multiAgent {
		return []string{device, target, step}
	}
	return []string{step}
}

func passingCheckListRightHeaderColumns() []string {
	return []string{"Cnt", "Avg", "Max", "Last"}
}

func passingCheckListRightColumns(item passingCheckSummary) []string {
	return []string{fmt.Sprint(item.Count), durationLabel(item.AvgDuration()), durationLabel(item.MaxDuration), item.Last.Format("15:04:05")}
}

func failedCheckListHeaderColumns(multiAgent bool) []string {
	if multiAgent {
		return []string{"Device", "Target", "Check", "Metric"}
	}
	return []string{"Check", "Metric"}
}

func failedCheckListColumns(item failedCheckSummary, multiAgent bool) []string {
	device := agentDeviceLabel(item.Agent)
	target := compactTargetLabel(firstNonEmpty(item.Finding.Target, item.Target.Name, item.Target.SSID, item.Target.BSSID, "-"), 18)
	check := displayCheckName(firstNonEmpty(item.Finding.Check, "check"))
	metric := firstNonEmpty(item.Finding.Metric, "status")
	if multiAgent {
		return []string{device, target, check, metric}
	}
	return []string{check, metric}
}

func failedCheckListRightHeaderColumns() []string {
	return []string{"Cnt", "Fail%", "Strk", "Last"}
}

func failedCheckListRightColumns(item failedCheckSummary) []string {
	return []string{fmt.Sprint(item.Count), fmt.Sprintf("%d%%", item.FailPercent), fmt.Sprint(item.FailStreak), item.Last.Format("15:04:05")}
}

func failureHotspotListHeaderColumns() []string {
	return []string{"Target", "Cause"}
}

func failureHotspotListColumns(item failureHotspotSummary) []string {
	target := compactTargetLabel(firstNonEmpty(item.Target.Name, item.Target.SSID, item.Target.BSSID, item.LatestFinding.Target, "-"), 18)
	return []string{target, item.LatestCause}
}

func failureHotspotListRightHeaderColumns() []string {
	return []string{"Fail%", "Fail/Run", "Last"}
}

func failureHotspotListRightColumns(item failureHotspotSummary) []string {
	rate := 0
	if item.RunCount > 0 {
		rate = item.FailRunCount * 100 / item.RunCount
	}
	return []string{
		fmt.Sprintf("%d%%", rate),
		fmt.Sprintf("%d/%d", item.FailCount, item.RunCount),
		item.Last.Format("15:04:05"),
	}
}

func failureCauseListHeaderColumns() []string {
	return []string{"Cause", "Targets"}
}

func failureCauseListColumns(item failureCauseSummary, targetWidth int) []string {
	return []string{item.Cause, failureCauseTargetsLabel(item, targetWidth)}
}

func failureCauseListRightHeaderColumns() []string {
	return []string{"Hits", "Fail%", "Fail/Run", "Last"}
}

func failureCauseListRightColumns(item failureCauseSummary) []string {
	rate := 0
	if item.RunCount > 0 {
		rate = item.FailRunCount * 100 / item.RunCount
	}
	return []string{
		fmt.Sprint(item.TargetCount),
		fmt.Sprintf("%d%%", rate),
		fmt.Sprintf("%d/%d", item.FailCount, item.RunCount),
		item.Last.Format("15:04:05"),
	}
}

func failureCauseTargetColumnWidth(layout barListLayout) int {
	if len(layout.ColumnWidths) < 2 {
		return 0
	}
	return layout.ColumnWidths[1]
}

func failureCauseTargetsLabel(item failureCauseSummary, width int) string {
	labels := failureCauseTargetLabels(item)
	if len(labels) == 0 {
		return "-"
	}
	full := strings.Join(labels, ", ")
	if width <= 0 || lipgloss.Width(full) <= width {
		return full
	}
	for shown := len(labels) - 1; shown >= 0; shown-- {
		omitted := len(labels) - shown
		suffix := fmt.Sprintf("...(%d)", omitted)
		candidate := suffix
		if shown > 0 {
			candidate = strings.Join(labels[:shown], ", ") + " " + suffix
		}
		if lipgloss.Width(candidate) <= width {
			return candidate
		}
	}
	return fitANSI(fmt.Sprintf("...(%d)", len(labels)), width)
}

func failureCauseTargetLabels(item failureCauseSummary) []string {
	labels := make([]string, 0, len(item.Targets))
	for _, target := range item.Targets {
		label := firstNonEmpty(target.Target.Name, target.Target.SSID, target.Target.BSSID, target.LatestFinding.Target, "-")
		labels = append(labels, compactTargetLabel(label, 18))
	}
	return labels
}

func countBar(count int, maxCount int, width int) string {
	if width <= 0 {
		return ""
	}
	if count <= 0 || maxCount <= 0 {
		return strings.Repeat(" ", width)
	}
	filled := max(1, (count*width+maxCount-1)/maxCount)
	filled = clamp(filled, 1, width)
	return strings.Repeat("█", filled) + strings.Repeat(" ", width-filled)
}

func maxPassingCheckSummaryCount(rows []passingCheckSummary) int {
	maxCount := 1
	for _, row := range rows {
		if row.Count > maxCount {
			maxCount = row.Count
		}
	}
	return maxCount
}

func maxFailedCheckSummaryCount(rows []failedCheckSummary) int {
	maxCount := 1
	for _, row := range rows {
		if row.Count > maxCount {
			maxCount = row.Count
		}
	}
	return maxCount
}

func renderTableLines(lines []tableLine, width int, visible int, selectedLine int) string {
	if visible <= 0 || len(lines) == 0 {
		return ""
	}
	start := 0
	if selectedLine >= 0 {
		start = correctedOffset(selectedLine, visible, len(lines))
	}
	end := min(len(lines), start+visible)
	var b strings.Builder
	for _, line := range lines[start:end] {
		text := fit(line.Text, width)
		if line.Fill {
			text = padToWidth(text, width)
		}
		b.WriteString(line.Style.Render(text))
		b.WriteByte('\n')
	}
	return b.String()
}
