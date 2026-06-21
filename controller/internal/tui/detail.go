package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"dropcheck/controller/internal/watch"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func (m model) passingCheckDetailView(width int, height int) string {
	content, ok := m.passingCheckDetailContent(width)
	if !ok {
		return dimStyle.Render("no passing check selected")
	}
	return content.render(width, height)
}

func (m model) passingCheckDetailContent(width int) (detailContent, bool) {
	rows := m.filteredPassingCheckSummaries()
	if len(rows) == 0 {
		return detailContent{}, false
	}
	selected := clamp(m.passingCheckCursor, 0, len(rows)-1)
	item := rows[selected]
	histogram := recentEventHistogram(m.passingCheckOccurrences(item.Agent, item.Target, item.Step), width, detailTimelineWindow, m.currentTime())
	fields := detailPassingSummaryFields(item, m.MultiAgent)
	sections := []detailSection{
		{Title: "logs", Rows: m.passingCheckRelatedLogLines(item, detailModalLogLimit), WrapLogs: true},
	}
	return detailContent{
		Summary:    detailSummaryTableLines(fields, width),
		Histogram:  histogram,
		GraphStyle: summaryGraphStyle,
		Sections:   sections,
	}, true
}

func (m model) failedCheckDetailView(width int, height int) string {
	content, ok := m.failedCheckDetailContent(width)
	if !ok {
		return dimStyle.Render("no failed check selected")
	}
	return content.render(width, height)
}

func (m model) failedCheckDetailContent(width int) (detailContent, bool) {
	rows := m.filteredFailedCheckSummaries()
	if len(rows) == 0 {
		return detailContent{}, false
	}
	selected := clamp(m.failedCheckCursor, 0, len(rows)-1)
	item := rows[selected]
	finding := item.Finding
	histogram := recentEventHistogram(m.failedCheckOccurrences(item.Agent, item.Target, finding), width, detailTimelineWindow, m.currentTime())
	fields := detailFailedSummaryFields(item, m.MultiAgent)
	sections := []detailSection{
		{Title: "failure history", Rows: m.failedCheckDetailRows(item, width)},
		{Title: "logs", Rows: m.failedCheckRelatedLogLines(item, detailModalLogLimit), WrapLogs: true},
	}
	return detailContent{
		Summary:    detailSummaryTableLines(fields, width),
		Histogram:  histogram,
		GraphStyle: summaryGraphStyle,
		Sections:   sections,
	}, true
}

func (m model) failureHotspotDetailView(width int, height int) string {
	content, ok := m.failureHotspotDetailContent(width)
	if !ok {
		return dimStyle.Render("no failure hotspot selected")
	}
	return content.render(width, height)
}

func (m model) failureHotspotDetailContent(width int) (detailContent, bool) {
	mode := m.failureHotspotMode
	if m.detailOpen && m.detailPanel == focusFailureHotspots {
		mode = m.detailHotspotMode
	}
	rows := m.focusedFailureAnalysisRowsForMode(mode)
	if len(rows) == 0 {
		return detailContent{}, false
	}
	selected := 0
	for i, row := range rows {
		if row.Index == m.failureHotspotCursor {
			selected = i
			break
		}
	}
	row := rows[selected]
	if row.Mode == failureHotspotModeCauses {
		return m.failureCauseDetailContent(row.Cause, width), true
	}
	item := row.Hotspot
	histogram := recentEventHistogram(m.failureHotspotOccurrences(item), width, detailTimelineWindow, m.currentTime())
	fields := detailHotspotSummaryFields(item, m.MultiAgent)
	sections := []detailSection{
		{Title: "causes", Rows: m.failureHotspotCauseRows(item, width)},
		{Title: "failure history", Rows: m.failureHotspotDetailRows(item, width)},
		{Title: "logs", Rows: m.failureHotspotRelatedLogLines(item, detailModalLogLimit), WrapLogs: true},
	}
	return detailContent{
		Summary:    detailSummaryTableLines(fields, width),
		Histogram:  histogram,
		GraphStyle: summaryGraphStyle,
		Sections:   sections,
	}, true
}

func (m model) failureCauseDetailContent(item failureCauseSummary, width int) detailContent {
	histogram := recentEventHistogram(m.failureCauseOccurrences(item), width, detailTimelineWindow, m.currentTime())
	fields := detailCauseSummaryFields(item, m.MultiAgent, width)
	sections := []detailSection{
		{Title: "targets", Rows: failureCauseTargetRows(item, width)},
		{Title: "failure history", Rows: m.failureCauseDetailRows(item, width)},
		{Title: "logs", Rows: m.failureCauseRelatedLogLines(item, detailModalLogLimit), WrapLogs: true},
	}
	return detailContent{
		Summary:    detailSummaryTableLines(fields, width),
		Histogram:  histogram,
		GraphStyle: summaryGraphStyle,
		Sections:   sections,
	}
}

type detailContent struct {
	Summary    []string
	Histogram  occurrenceHistogram
	GraphStyle lipgloss.Style
	Sections   []detailSection
}

func (content detailContent) render(width int, height int) string {
	return denseDetailView(content.Summary, content.Histogram, content.GraphStyle, content.Sections, width, height)
}

func (content detailContent) naturalHeight(width int) int {
	height := len(content.Summary)
	trailingBlank := height > 0 && detailLineBlank(content.Summary[height-1])
	if content.Histogram.Count > 0 {
		if height > 0 && !trailingBlank {
			height++
		}
		height += detailTimelineHeaderHeight
		height += detailNaturalGraphHeight
		height++
		trailingBlank = true
	}
	for _, section := range content.Sections {
		if height > 0 && !trailingBlank {
			height++
		}
		height++
		for _, row := range detailSectionNaturalRows(section) {
			height += len(detailSectionRowLines(section, row, width))
		}
		trailingBlank = false
	}
	return height
}

func detailSectionNaturalRows(section detailSection) []string {
	rows := section.Rows
	if len(rows) == 0 {
		return []string{dimStyle.Render("  no matching entries")}
	}
	return rows
}

type detailSection struct {
	Title    string
	Rows     []string
	WrapLogs bool
}

type detailField struct {
	Key   string
	Value string
	Wide  bool
}

func detailPassingSummaryFields(item passingCheckSummary, multiAgent bool) []detailField {
	fields := make([]detailField, 0, 12)
	detailAppendAgentField(&fields, item.Agent, multiAgent)
	fields = append(fields,
		detailField{Key: "target", Value: firstNonEmpty(item.Target.Name, item.Target.SSID, item.Target.BSSID, "-")},
		detailField{Key: "check", Value: displayCheckName(firstNonEmpty(item.Step.Name, item.Step.Type, "check"))},
	)
	detailAppendTargetFields(&fields, item.Target)
	detailAppendOperationFields(&fields, item.Step)
	fields = append(fields,
		detailField{Key: "last", Value: item.Last.Format("15:04:05")},
		detailField{Key: "duration", Value: durationLabel(item.Duration)},
		detailField{Key: "avg", Value: durationLabel(item.AvgDuration())},
		detailField{Key: "max", Value: durationLabel(item.MaxDuration)},
		detailField{Key: "samples", Value: fmt.Sprint(item.Count)},
	)
	return fields
}

func detailFailedSummaryFields(item failedCheckSummary, multiAgent bool) []detailField {
	finding := item.Finding
	fields := make([]detailField, 0, 12)
	detailAppendAgentField(&fields, item.Agent, multiAgent)
	fields = append(fields,
		detailField{Key: "target", Value: firstNonEmpty(finding.Target, item.Target.Name, item.Target.SSID, item.Target.BSSID, "-")},
		detailField{Key: "check", Value: displayCheckName(firstNonEmpty(finding.Check, "check"))},
		detailField{Key: "metric", Value: firstNonEmpty(finding.Metric, "-")},
		detailField{Key: "failures", Value: fmt.Sprint(item.Count)},
		detailField{Key: "fail_rate", Value: fmt.Sprintf("%d%%", item.FailPercent)},
		detailField{Key: "streak", Value: fmt.Sprint(item.FailStreak)},
		detailField{Key: "last", Value: item.Last.Format("15:04:05")},
		detailField{Key: "observed", Value: firstNonEmpty(finding.Observed, "-")},
		detailField{Key: "expected", Value: detailValue(finding.Expected)},
	)
	if finding.Message != "" {
		fields = append(fields, detailField{Key: "message", Value: finding.Message, Wide: true})
	}
	return fields
}

func detailHotspotSummaryFields(item failureHotspotSummary, multiAgent bool) []detailField {
	finding := item.LatestFinding
	rate := 0
	if item.RunCount > 0 {
		rate = item.FailRunCount * 100 / item.RunCount
	}
	fields := make([]detailField, 0, 16)
	detailAppendAgentField(&fields, item.Agent, multiAgent)
	fields = append(fields, detailField{Key: "target", Value: firstNonEmpty(checkStatusTargetLabel(item.Target), finding.Target, "-")})
	detailAppendTargetFields(&fields, item.Target)
	fields = append(fields,
		detailField{Key: "latest_cause", Value: firstNonEmpty(item.LatestCause, "-"), Wide: true},
		detailField{Key: "fail_rate", Value: fmt.Sprintf("%d%%", rate)},
		detailField{Key: "failed_runs", Value: fmt.Sprintf("%d/%d", item.FailRunCount, item.RunCount)},
		detailField{Key: "failures", Value: fmt.Sprint(item.FailCount)},
		detailField{Key: "streak", Value: fmt.Sprint(item.FailStreak)},
		detailField{Key: "last", Value: item.Last.Format("15:04:05")},
		detailField{Key: "latest_check", Value: displayCheckName(firstNonEmpty(finding.Check, "check"))},
		detailField{Key: "metric", Value: firstNonEmpty(finding.Metric, "-")},
		detailField{Key: "observed", Value: firstNonEmpty(finding.Observed, "-")},
		detailField{Key: "expected", Value: detailValue(finding.Expected)},
	)
	return fields
}

func detailCauseSummaryFields(item failureCauseSummary, multiAgent bool, width int) []detailField {
	rate := 0
	if item.RunCount > 0 {
		rate = item.FailRunCount * 100 / item.RunCount
	}
	fields := make([]detailField, 0, 12)
	detailAppendAgentField(&fields, item.Agent, multiAgent)
	fields = append(fields,
		detailField{Key: "cause", Value: firstNonEmpty(item.Cause, "-"), Wide: true},
		detailField{Key: "targets", Value: failureCauseTargetsLabel(item, width), Wide: true},
		detailField{Key: "target_count", Value: fmt.Sprint(item.TargetCount)},
		detailField{Key: "fail_rate", Value: fmt.Sprintf("%d%%", rate)},
		detailField{Key: "failed_runs", Value: fmt.Sprintf("%d/%d", item.FailRunCount, item.RunCount)},
		detailField{Key: "failures", Value: fmt.Sprint(item.FailCount)},
		detailField{Key: "last", Value: item.Last.Format("15:04:05")},
	)
	return fields
}

func detailAppendAgentField(fields *[]detailField, agent watch.AgentSnapshot, multiAgent bool) {
	if multiAgent && agentKey(agent) != "" {
		*fields = append(*fields, detailField{Key: "device", Value: agentLabel(agent)})
	}
}

func detailAppendTargetFields(fields *[]detailField, target watch.TargetSnapshot) {
	for _, field := range []detailField{
		{Key: "ssid", Value: target.SSID},
		{Key: "bssid", Value: target.BSSID},
		{Key: "band", Value: target.Band},
	} {
		if field.Value != "" {
			*fields = append(*fields, field)
		}
	}
}

func detailAppendOperationFields(fields *[]detailField, step watch.StepSnapshot) {
	display := normalizedScopeToken(displayCheckName(firstNonEmpty(step.Name, step.Type)))
	for _, field := range []detailField{
		{Key: "op", Value: step.Operation},
		{Key: "type", Value: step.Type},
	} {
		if field.Value == "" || normalizedScopeToken(field.Value) == display {
			continue
		}
		*fields = append(*fields, field)
	}
}

const (
	detailGraphVerticalPadding = 2
	detailTimelineHeaderHeight = 2
	detailNaturalGraphHeight   = 4
	detailSummaryColumnGap     = 2
	detailSummaryMinCellWidth  = 4
	detailSummaryMaxCellWidth  = 30
	detailSummaryTargetCell    = 12
)

func denseDetailView(summary []string, histogram occurrenceHistogram, graphStyle lipgloss.Style, sections []detailSection, width int, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	lines := make([]string, 0, height)
	for _, line := range summary {
		if len(lines) >= height {
			return strings.Join(lines, "\n")
		}
		lines = append(lines, fitANSI(line, width))
	}
	graphHeight := detailCompactGraphHeight(height, len(lines), sections)
	if histogram.Count > 0 && graphHeight > 0 && len(lines)+graphHeight+detailGraphVerticalPadding+detailTimelineHeaderHeight <= height {
		lines = append(lines, "")
		timeline := detailTimelineLabel(histogram, graphHeight)
		lines = append(lines, fitANSI(timeline, width))
		lines = append(lines, "")
		lines = append(lines, renderDetailHistogram(histogram, width, graphHeight, graphStyle)...)
		lines = append(lines, "")
	}
	remaining := height - len(lines)
	if remaining <= 0 || len(sections) == 0 {
		return strings.Join(lines[:intMin(len(lines), height)], "\n")
	}
	allocations := detailSectionAllocations(sections, intMax(0, remaining-detailSectionGapCount(lines, len(sections))), width)
	for i, section := range sections {
		if len(lines) >= height || allocations[i] <= 0 {
			continue
		}
		appendDetailGap(&lines, height)
		if len(lines) >= height {
			break
		}
		lines = append(lines, fitANSI(summaryKeyStyle.Render(detailSectionTitle(section)), width))
		rowLimit := allocations[i] - 1
		rows := section.Rows
		if len(rows) == 0 {
			rows = []string{dimStyle.Render("  no matching entries")}
		}
		rendered := 0
		for _, row := range rows {
			if len(lines) >= height || rendered >= rowLimit {
				break
			}
			rowLines := detailSectionRowLines(section, row, width)
			for _, rowLine := range rowLines {
				if len(lines) >= height || rendered >= rowLimit {
					break
				}
				lines = append(lines, fitANSI(rowLine, width))
				rendered++
			}
		}
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func appendDetailGap(lines *[]string, height int) {
	if len(*lines) == 0 || len(*lines) >= height {
		return
	}
	if detailLineBlank((*lines)[len(*lines)-1]) {
		return
	}
	*lines = append(*lines, "")
}

func detailSectionGapCount(lines []string, sectionCount int) int {
	if sectionCount <= 0 {
		return 0
	}
	gaps := sectionCount
	if len(lines) == 0 || detailLineBlank(lines[len(lines)-1]) {
		gaps--
	}
	return intMax(0, gaps)
}

func detailLineBlank(line string) bool {
	return strings.TrimSpace(ansi.Strip(line)) == ""
}

func detailSummaryTableLines(fields []detailField, width int) []string {
	if width <= 0 {
		return nil
	}
	lines := make([]string, 0, intMax(1, len(fields)/3)*2)
	pending := make([]detailField, 0, len(fields))
	appendGroup := func(group []string) {
		if len(group) == 0 {
			return
		}
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, group...)
	}
	flush := func() {
		if len(pending) == 0 {
			return
		}
		appendGroup(detailSummaryFieldTableLines(pending, width))
		pending = pending[:0]
	}
	for _, field := range fields {
		if field.Wide {
			flush()
			appendGroup(detailSummaryRenderFieldRow([]detailField{field}, []int{width}))
			continue
		}
		pending = append(pending, field)
	}
	flush()
	return lines
}

func detailSummaryFieldTableLines(fields []detailField, width int) []string {
	lines := make([]string, 0, intMax(1, len(fields)/3)*2)
	for len(fields) > 0 {
		count, widths := detailSummaryRowShape(fields, width)
		lines = append(lines, detailSummaryRenderFieldRow(fields[:count], widths)...)
		fields = fields[count:]
	}
	return lines
}

func detailSummaryRowShape(fields []detailField, width int) (int, []int) {
	maxColumns := clamp(width/detailSummaryTargetCell, 1, len(fields))
	for count := maxColumns; count >= 1; count-- {
		widths := detailSummaryMinWidths(fields[:count])
		if detailSummaryWidthSum(widths) <= width {
			return count, detailSummaryColumnWidths(fields[:count], width)
		}
	}
	return 1, []int{intMax(1, width)}
}

func detailSummaryColumnWidths(fields []detailField, width int) []int {
	if len(fields) == 0 {
		return nil
	}
	minWidths := detailSummaryMinWidths(fields)
	preferred := make([]int, len(fields))
	for i, field := range fields {
		preferred[i] = detailSummaryPreferredWidth(field)
	}
	if detailSummaryWidthSum(preferred) <= width {
		return preferred
	}
	widths := append([]int(nil), minWidths...)
	remaining := width - detailSummaryWidthSum(widths)
	for remaining > 0 {
		best := -1
		bestNeed := 0
		for i := range widths {
			need := preferred[i] - widths[i]
			if need > bestNeed {
				best = i
				bestNeed = need
			}
		}
		if best < 0 {
			break
		}
		widths[best]++
		remaining--
	}
	return widths
}

func detailSummaryMinWidths(fields []detailField) []int {
	widths := make([]int, len(fields))
	for i, field := range fields {
		keyWidth := lipgloss.Width(detailFieldHeader(field.Key))
		valueWidth := intMin(lipgloss.Width(firstNonEmpty(field.Value, "-")), detailSummaryTargetCell)
		widths[i] = intMax(detailSummaryMinCellWidth, intMax(keyWidth, valueWidth))
	}
	return widths
}

func detailSummaryPreferredWidth(field detailField) int {
	value := firstNonEmpty(field.Value, "-")
	preferred := intMax(lipgloss.Width(detailFieldHeader(field.Key)), lipgloss.Width(value))
	return clamp(preferred, detailSummaryMinCellWidth, detailSummaryMaxCellWidth)
}

func detailSummaryWidthSum(widths []int) int {
	if len(widths) == 0 {
		return 0
	}
	sum := detailSummaryColumnGap * (len(widths) - 1)
	for _, width := range widths {
		sum += width
	}
	return sum
}

func detailSummaryRenderFieldRow(fields []detailField, widths []int) []string {
	if len(fields) == 0 {
		return nil
	}
	headers := make([]string, 0, len(fields))
	values := make([]string, 0, len(fields))
	for i, field := range fields {
		width := widths[i]
		headers = append(headers, detailSummaryCell(keyStyle, detailFieldHeader(field.Key), width))
		values = append(values, detailSummaryCell(valueStyle, firstNonEmpty(field.Value, "-"), width))
	}
	gap := valueStyle.Render(strings.Repeat(" ", detailSummaryColumnGap))
	return []string{
		strings.Join(headers, gap),
		strings.Join(values, gap),
	}
}

func detailSummaryCell(style lipgloss.Style, value string, width int) string {
	if width <= 0 {
		return ""
	}
	return style.Render(padVisible(fitText(value, width), width))
}

func detailFieldHeader(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	parts := strings.Split(key, "_")
	for i, part := range parts {
		parts[i] = detailFieldHeaderWord(part)
	}
	return strings.Join(parts, " ")
}

func detailFieldHeaderWord(word string) string {
	switch strings.ToLower(strings.TrimSpace(word)) {
	case "ssid":
		return "SSID"
	case "bssid":
		return "BSSID"
	case "ip":
		return "IP"
	case "dns":
		return "DNS"
	case "http":
		return "HTTP"
	case "op":
		return "Op"
	default:
		if word == "" {
			return ""
		}
		lower := strings.ToLower(word)
		return strings.ToUpper(lower[:1]) + lower[1:]
	}
}

func detailTimelineLabel(histogram occurrenceHistogram, graphHeight int) string {
	plotHeight := graphHeight
	if graphHeight > 1 {
		plotHeight = graphHeight - 1
	}
	scale := sparklineEventsPerRow(histogram.Max, intMax(1, plotHeight))
	label := summarySparklineLabelStyle.Render("window=") + summaryValueStyle.Render("last="+detailTimelineWindowLabel()) +
		summarySparklineLabelStyle.Render(" count=") + summaryValueStyle.Render(fmt.Sprint(histogram.Count)) +
		summarySparklineLabelStyle.Render(" peak=") + summaryValueStyle.Render(fmt.Sprint(histogram.Max)) +
		summarySparklineLabelStyle.Render(" scale=") + summaryValueStyle.Render(fmt.Sprintf("%d/row", scale))
	return label
}

func detailTimelineWindowLabel() string {
	return fmt.Sprintf("%dm", int(detailTimelineWindow/time.Minute))
}

func detailTimelineAxis(width int) string {
	if width <= 0 {
		return ""
	}
	left := detailTimelineWindowLabel() + " ago"
	right := "now"
	if width < len(left)+len(right)+1 {
		return dimStyle.Render(strings.Repeat("-", width))
	}
	mid := strings.Repeat("-", intMax(1, width-len(left)-len(right)))
	return dimStyle.Render(left + mid + right)
}

func detailSectionRowLines(section detailSection, row string, width int) []string {
	if !section.WrapLogs {
		return []string{row}
	}
	return detailLogRowLines(row, width)
}

func detailLogRowLines(row string, width int) []string {
	if width <= 0 {
		return nil
	}
	row = strings.TrimSpace(sanitizeLogText(ansi.Strip(row)))
	if row == "" {
		return nil
	}
	timestamp, body, ok := detailSplitLogTimestamp(row)
	if !ok {
		body = row
	}
	prefix := "  "
	continuationPrefix := "  "
	if ok {
		prefix += timestamp + "  "
		continuationPrefix += strings.Repeat(" ", lipgloss.Width(timestamp)) + "  "
	}
	bodyWidth := intMax(1, width-lipgloss.Width(prefix))
	wrapped := detailWrapLogBody(body, bodyWidth)
	lines := make([]string, 0, intMax(1, len(wrapped)))
	if len(wrapped) == 0 {
		wrapped = []string{""}
	}
	for i, part := range wrapped {
		linePrefix := prefix
		if i > 0 {
			linePrefix = continuationPrefix
		}
		lines = append(lines, detailLogLine(linePrefix, part, bodyWidth))
	}
	return lines
}

func detailLogLine(prefix string, body string, bodyWidth int) string {
	return valueStyle.Render(prefix) + valueStyle.Render(fitANSI(strings.TrimSpace(body), bodyWidth))
}

func detailWrapLogBody(body string, width int) []string {
	body = strings.TrimSpace(body)
	if body == "" {
		return []string{""}
	}
	if width <= 0 {
		return []string{body}
	}
	lines := make([]string, 0, 2)
	for lipgloss.Width(body) > width {
		cut := detailLogWrapCut(body, width)
		if cut <= 0 || cut > len(body) {
			break
		}
		part := strings.TrimSpace(body[:cut])
		if part != "" {
			lines = append(lines, part)
		}
		body = strings.TrimSpace(body[cut:])
	}
	if body != "" || len(lines) == 0 {
		lines = append(lines, body)
	}
	return lines
}

func detailLogWrapCut(value string, width int) int {
	if width <= 0 {
		return len(value)
	}
	visible := 0
	lastSpace := -1
	for i, r := range value {
		runeWidth := lipgloss.Width(string(r))
		if visible > 0 && unicode.IsSpace(r) {
			lastSpace = i
		}
		if visible+runeWidth > width {
			if lastSpace > 0 {
				return lastSpace
			}
			if i == 0 {
				size := utf8.RuneLen(r)
				if size <= 0 {
					return len(string(r))
				}
				return size
			}
			return i
		}
		visible += runeWidth
	}
	return len(value)
}

func detailSplitLogTimestamp(row string) (timestamp string, body string, ok bool) {
	if len(row) < len("15:04:05 ") {
		return "", "", false
	}
	if !asciiDigit(row[0]) || !asciiDigit(row[1]) || row[2] != ':' ||
		!asciiDigit(row[3]) || !asciiDigit(row[4]) || row[5] != ':' ||
		!asciiDigit(row[6]) || !asciiDigit(row[7]) || row[8] != ' ' {
		return "", "", false
	}
	return row[:8], strings.TrimSpace(row[9:]), true
}

func asciiDigit(ch byte) bool {
	return ch >= '0' && ch <= '9'
}

func detailCompactGraphHeight(height int, used int, sections []detailSection) int {
	remaining := height - used
	availableForGraph := remaining - detailSectionsMinimumHeight(sections) - detailGraphVerticalPadding - detailTimelineHeaderHeight
	if availableForGraph < 2 {
		return 0
	}
	return intMin(4, availableForGraph)
}

func detailSectionsMinimumHeight(sections []detailSection) int {
	total := 0
	for i, section := range sections {
		if i > 0 {
			total++
		}
		total += detailSectionMinAllocation(section)
	}
	return total
}

func detailSectionAllocations(sections []detailSection, available int, width int) []int {
	allocations := make([]int, len(sections))
	if len(sections) == 0 || available <= 0 {
		return allocations
	}
	naturals := make([]int, len(sections))
	minimums := make([]int, len(sections))
	minimumTotal := 0
	for i, section := range sections {
		naturals[i] = detailSectionNaturalAllocation(section, width)
		minimums[i] = detailSectionMinAllocation(section)
		if naturals[i] > 0 {
			minimums[i] = intMin(minimums[i], naturals[i])
		}
		minimumTotal += minimums[i]
	}
	if available <= minimumTotal {
		remaining := available
		for i, minimum := range minimums {
			if remaining <= 0 {
				break
			}
			allocations[i] = intMin(minimum, remaining)
			remaining -= allocations[i]
		}
		return allocations
	}
	copy(allocations, minimums)
	remaining := available - minimumTotal

	grant := func(index int, target int) {
		if remaining <= 0 || index < 0 || index >= len(allocations) {
			return
		}
		target = intMin(target, naturals[index])
		if target <= allocations[index] {
			return
		}
		extra := intMin(target-allocations[index], remaining)
		allocations[index] += extra
		remaining -= extra
	}

	for i := range sections {
		if detailSectionIsLogs(sections[i]) {
			grant(i, detailLogMinimumAllocation(available))
		}
	}
	for i := range sections {
		if detailSectionIsFailureHistory(sections[i]) {
			grant(i, naturals[i])
		}
	}
	for i := range sections {
		if !detailSectionIsLogs(sections[i]) && !detailSectionIsFailureHistory(sections[i]) {
			grant(i, naturals[i])
		}
	}
	for i := range sections {
		if detailSectionIsLogs(sections[i]) {
			grant(i, naturals[i])
		}
	}
	return allocations
}

func detailSectionNaturalAllocation(section detailSection, width int) int {
	total := 1
	for _, row := range detailSectionNaturalRows(section) {
		total += len(detailSectionRowLines(section, row, width))
	}
	return total
}

func detailLogMinimumAllocation(available int) int {
	return intMax(2, (available+3)/4)
}

func detailSectionMinAllocation(section detailSection) int {
	rows := len(section.Rows)
	if rows == 0 {
		return 2
	}
	if detailSectionIsLogs(section) {
		return 2
	}
	return 1 + intMin(2, rows)
}

func detailSectionTitle(section detailSection) string {
	switch detailSectionTitleKey(section) {
	case "logs":
		return "Logs:"
	case "causes":
		return "Causes:"
	case "failure history":
		return "Failure History:"
	default:
		title := strings.TrimSpace(strings.TrimSuffix(section.Title, ":"))
		if title == "" {
			return ""
		}
		return detailFieldHeader(strings.ReplaceAll(title, " ", "_")) + ":"
	}
}

func detailSectionIsLogs(section detailSection) bool {
	return detailSectionTitleKey(section) == "logs"
}

func detailSectionIsFailureHistory(section detailSection) bool {
	return detailSectionTitleKey(section) == "failure history"
}

func detailSectionTitleKey(section detailSection) string {
	title := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(section.Title)), ":")
	return strings.Join(strings.Fields(title), " ")
}

func (m model) failedCheckDetailRows(summary failedCheckSummary, width int) []string {
	items := make([]failedCheckState, 0, summary.Count)
	for _, item := range m.FailedChecks {
		if failedCheckSummaryKey(item.Agent, item.Target, item.Finding) == failedCheckSummaryKey(summary.Agent, summary.Target, summary.Finding) {
			items = append(items, item)
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].When.After(items[j].When)
	})
	tableRows := make([][]string, 0, len(items))
	for _, item := range items {
		finding := item.Finding
		tableRows = append(tableRows, []string{
			item.When.Format("15:04:05"),
			roundLabel(item.Round),
			firstNonEmpty(finding.Check, "-"),
			firstNonEmpty(finding.Metric, "-"),
			firstNonEmpty(finding.Observed, "-"),
			detailValue(finding.Expected),
			firstNonEmpty(finding.Message, "-"),
		})
	}
	return detailTableRows([]string{"Time", "Round", "Check", "Metric", "Observed", "Expected", "Message"}, tableRows, width, nil)
}

func (m model) failureHotspotDetailRows(summary failureHotspotSummary, width int) []string {
	key := failureHotspotSummaryIdentity(summary)
	items := make([]failedCheckState, 0, summary.FailCount)
	for _, item := range m.FailedChecks {
		if failureHotspotIdentity(item.Agent, item.Target, item.Finding.Target) == key {
			items = append(items, item)
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].When.After(items[j].When)
	})
	tableRows := make([][]string, 0, len(items))
	for _, item := range items {
		finding := item.Finding
		tableRows = append(tableRows, []string{
			item.When.Format("15:04:05"),
			roundLabel(item.Round),
			firstNonEmpty(finding.Check, "-"),
			firstNonEmpty(finding.Metric, "-"),
			firstNonEmpty(finding.Observed, "-"),
			detailValue(finding.Expected),
			failureHotspotCause(finding),
		})
	}
	return detailTableRows([]string{"Time", "Round", "Check", "Metric", "Observed", "Expected", "Cause"}, tableRows, width, nil)
}

type failureHotspotCauseSummary struct {
	Cause string
	Count int
	Last  time.Time
}

func (m model) failureHotspotCauseRows(summary failureHotspotSummary, width int) []string {
	key := failureHotspotSummaryIdentity(summary)
	index := map[string]int{}
	rows := make([]failureHotspotCauseSummary, 0)
	for _, item := range m.FailedChecks {
		if failureHotspotIdentity(item.Agent, item.Target, item.Finding.Target) != key {
			continue
		}
		cause := failureHotspotCause(item.Finding)
		pos, ok := index[cause]
		if !ok {
			pos = len(rows)
			index[cause] = pos
			rows = append(rows, failureHotspotCauseSummary{Cause: cause})
		}
		rows[pos].Count++
		if item.When.After(rows[pos].Last) || rows[pos].Last.IsZero() {
			rows[pos].Last = item.When
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Count != rows[j].Count {
			return rows[i].Count > rows[j].Count
		}
		return rows[i].Last.After(rows[j].Last)
	})
	tableRows := make([][]string, 0, len(rows))
	for _, row := range rows {
		tableRows = append(tableRows, []string{fmt.Sprint(row.Count), row.Last.Format("15:04:05"), row.Cause})
	}
	return detailTableRows([]string{"Count", "Last", "Cause"}, tableRows, width, map[int]bool{0: true})
}

func failureCauseTargetRows(summary failureCauseSummary, width int) []string {
	tableRows := make([][]string, 0, len(summary.Targets))
	for _, target := range summary.Targets {
		rate := 0
		if target.RunCount > 0 {
			rate = target.FailRunCount * 100 / target.RunCount
		}
		tableRows = append(tableRows, []string{
			firstNonEmpty(target.Target.Name, target.Target.SSID, target.Target.BSSID, target.LatestFinding.Target, "-"),
			fmt.Sprintf("%d%%", rate),
			fmt.Sprintf("%d/%d", target.FailRunCount, target.RunCount),
			fmt.Sprint(target.FailCount),
			target.Last.Format("15:04:05"),
			firstNonEmpty(target.LatestFinding.Check, "-"),
		})
	}
	return detailTableRows([]string{"Target", "Fail%", "Fail/Run", "Failures", "Last", "Latest"}, tableRows, width, map[int]bool{1: true, 2: true, 3: true})
}

func (m model) failureCauseDetailRows(summary failureCauseSummary, width int) []string {
	key := failureCauseSummaryIdentity(summary)
	items := make([]failedCheckState, 0, summary.FailCount)
	for _, item := range m.FailedChecks {
		if failureCauseIdentity(item.Agent, item.Finding) == key {
			items = append(items, item)
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].When.After(items[j].When)
	})
	tableRows := make([][]string, 0, len(items))
	for _, item := range items {
		finding := item.Finding
		tableRows = append(tableRows, []string{
			item.When.Format("15:04:05"),
			roundLabel(item.Round),
			firstNonEmpty(item.Target.Name, item.Target.SSID, item.Target.BSSID, finding.Target, "-"),
			firstNonEmpty(finding.Check, "-"),
			firstNonEmpty(finding.Metric, "-"),
			firstNonEmpty(finding.Observed, "-"),
			detailValue(finding.Expected),
		})
	}
	return detailTableRows([]string{"Time", "Round", "Target", "Check", "Metric", "Observed", "Expected"}, tableRows, width, nil)
}

func detailTableRows(headers []string, rows [][]string, width int, rightAlign map[int]bool) []string {
	if width <= 0 || len(headers) == 0 {
		return nil
	}
	allRows := make([][]string, 0, len(rows)+1)
	allRows = append(allRows, headers)
	allRows = append(allRows, rows...)
	widths := maxColumnWidths(allRows)
	widths = shrinkWidths(widths, intMax(1, width-2), 1)
	out := make([]string, 0, len(rows)+1)
	out = append(out, summaryKeyStyle.Render("  "+detailTableLine(headers, widths, rightAlign)))
	for _, row := range rows {
		out = append(out, valueStyle.Render("  "+detailTableLine(row, widths, rightAlign)))
	}
	return out
}

func detailTableLine(values []string, widths []int, rightAlign map[int]bool) string {
	parts := make([]string, 0, len(widths))
	for i, width := range widths {
		value := ""
		if i < len(values) {
			value = values[i]
		}
		if rightAlign != nil && rightAlign[i] {
			parts = append(parts, padLeftVisible(value, width))
		} else {
			parts = append(parts, padVisible(value, width))
		}
	}
	return strings.Join(parts, strings.Repeat(" ", listColumnGap))
}

func (m model) passingCheckRelatedLogLines(item passingCheckSummary, limit int) []string {
	return m.scopedLogLines(limit, func(entry eventLogEntry) bool {
		event := entry.Event
		if event.Kind == "" || !scopedEventAgentMatches(event.Agent, item.Agent) || !scopedEventTargetMatches(event, item.Target) {
			return false
		}
		if targetSummaryStep(item.Step) {
			return event.Kind == watch.EventTargetStarted || event.Kind == watch.EventTargetFinished
		}
		if !eventMatchesStep(event, item.Step) {
			return false
		}
		return event.Kind == watch.EventStepStarted || event.Kind == watch.EventStepFinished || event.Kind == watch.EventLog
	})
}

func (m model) failedCheckRelatedLogLines(item failedCheckSummary, limit int) []string {
	return m.scopedLogLines(limit, func(entry eventLogEntry) bool {
		event := entry.Event
		if event.Kind == "" || !scopedEventAgentMatches(event.Agent, item.Agent) || !scopedEventTargetMatches(event, item.Target, item.Finding.Target) {
			return false
		}
		if event.Finding != nil && failedCheckKey(*event.Finding) == failedCheckKey(item.Finding) {
			return true
		}
		if !eventMatchesFinding(event, item.Finding) {
			return false
		}
		return event.Kind == watch.EventStepStarted || event.Kind == watch.EventStepFinished || event.Kind == watch.EventLog
	})
}

func (m model) failureHotspotRelatedLogLines(item failureHotspotSummary, limit int) []string {
	return m.scopedLogLines(limit, func(entry eventLogEntry) bool {
		event := entry.Event
		if event.Kind == "" || !scopedEventAgentMatches(event.Agent, item.Agent) || !scopedEventTargetMatches(event, item.Target, item.LatestFinding.Target) {
			return false
		}
		switch event.Kind {
		case watch.EventFinding:
			return true
		case watch.EventStepFinished:
			return eventFailureLike(event)
		case watch.EventStepStarted:
			return eventMatchesFinding(event, item.LatestFinding)
		case watch.EventLog:
			return eventFailureLike(event)
		default:
			return false
		}
	})
}

func (m model) failureCauseRelatedLogLines(item failureCauseSummary, limit int) []string {
	return m.scopedLogLines(limit, func(entry eventLogEntry) bool {
		event := entry.Event
		if event.Kind == "" || !scopedEventAgentMatches(event.Agent, item.Agent) {
			return false
		}
		if event.Finding != nil {
			return failureCauseIdentity(event.Agent, *event.Finding) == failureCauseSummaryIdentity(item)
		}
		if event.Kind != watch.EventStepFinished && event.Kind != watch.EventLog {
			return false
		}
		for _, target := range item.Targets {
			if scopedEventTargetMatches(event, target.Target, target.LatestFinding.Target) && eventFailureLike(event) {
				return true
			}
		}
		return false
	})
}

func (m model) scopedLogLines(limit int, match func(eventLogEntry) bool) []string {
	if limit <= 0 {
		return nil
	}
	rows := make([]string, 0, limit)
	for i := len(m.EventLogEntries) - 1; i >= 0 && len(rows) < limit; i-- {
		entry := m.EventLogEntries[i]
		if match(entry) {
			rows = append(rows, entry.Line)
		}
	}
	return rows
}

func scopedEventAgentMatches(eventAgent watch.AgentSnapshot, selected watch.AgentSnapshot) bool {
	if agentKey(selected) == "" || agentKey(eventAgent) == "" {
		return true
	}
	return sameAgent(eventAgent, selected)
}

func scopedEventTargetMatches(event watch.Event, target watch.TargetSnapshot, aliases ...string) bool {
	expected := scopedTargetKey(target, aliases...)
	actual := scopedEventTargetKey(event)
	return expected != "" && actual != "" && expected == actual
}

func scopedEventTargetKey(event watch.Event) string {
	findingTarget := ""
	if event.Finding != nil {
		findingTarget = event.Finding.Target
	}
	return scopedTargetKey(event.Target, findingTarget)
}

func scopedTargetKey(target watch.TargetSnapshot, aliases ...string) string {
	values := []string{target.Name}
	values = append(values, aliases...)
	values = append(values, target.SSID, target.BSSID)
	return normalizedScopeToken(firstNonEmpty(values...))
}

func eventMatchesFinding(event watch.Event, finding watch.Finding) bool {
	if event.Finding != nil && failedCheckKey(*event.Finding) == failedCheckKey(finding) {
		return true
	}
	return eventMatchesStep(event, watch.StepSnapshot{Name: finding.Check, Type: finding.Check})
}

func eventMatchesStep(event watch.Event, step watch.StepSnapshot) bool {
	expected := scopeTokenSet(step.Name, step.Type, step.Operation)
	if len(expected) == 0 {
		return false
	}
	actual := scopeTokenList(event.Step.Name, event.Step.Type, event.Step.Operation)
	if event.Finding != nil {
		actual = append(actual, scopeTokenList(event.Finding.Check)...)
	}
	for _, token := range actual {
		if _, ok := expected[token]; ok {
			return true
		}
	}
	return false
}

func scopeTokenSet(values ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, token := range scopeTokenList(values...) {
		out[token] = struct{}{}
	}
	return out
}

func scopeTokenList(values ...string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		token := normalizedScopeToken(value)
		if token == "" || token == "-" {
			continue
		}
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		out = append(out, token)
	}
	return out
}

func normalizedScopeToken(value string) string {
	return strings.ToLower(strings.TrimSpace(sanitizeLogText(value)))
}

func targetSummaryStep(step watch.StepSnapshot) bool {
	return normalizedScopeToken(firstNonEmpty(step.Name, step.Type)) == "target"
}

func eventFailureLike(event watch.Event) bool {
	status := normalizedScopeToken(firstNonEmpty(event.Status, event.Step.Status))
	if status == "failed" || status == "fail" || status == "warn" || status == "warning" || status == "error" {
		return true
	}
	message := normalizedScopeToken(firstNonEmpty(event.Message, event.Step.Message, event.Step.Error))
	return strings.Contains(message, "fail") ||
		strings.Contains(message, "reject") ||
		strings.Contains(message, "declined") ||
		strings.Contains(message, "timeout") ||
		strings.Contains(message, "not available") ||
		strings.Contains(message, "constraint")
}

func roundLabel(round uint64) string {
	if round == 0 {
		return "-"
	}
	return fmt.Sprint(round)
}

func renderDetailHistogram(histogram occurrenceHistogram, width int, height int, style lipgloss.Style) []string {
	if width <= 0 || height <= 0 || len(histogram.Counts) == 0 || histogram.Max <= 0 {
		return nil
	}
	if height == 1 {
		return renderSparkline(histogram.Counts, histogram.Max, width, 1, style)
	}
	lines := renderSparkline(histogram.Counts, histogram.Max, width, intMax(1, height-1), style)
	lines = append(lines, detailTimelineAxis(width))
	return lines[:intMin(len(lines), height)]
}

func (m model) detailModal(width int, height int) string {
	switch m.detailPanel {
	case focusPassingChecks:
		return m.passingCheckDetailModal(width, height)
	case focusFailureHotspots:
		return m.failureHotspotDetailModal(width, height)
	default:
		return m.failedCheckDetailModal(width, height)
	}
}

func (m model) passingCheckDetailModal(width int, height int) string {
	modalWidth := detailModalWidth(width)
	contentWidth := panelContentWidth(modalWidth)
	content, ok := m.passingCheckDetailContent(contentWidth)
	modalHeight := detailModalHeight(height)
	body := dimStyle.Render("no passing check selected")
	if ok {
		modalHeight = detailModalHeightForContent(height, content.naturalHeight(contentWidth))
		body = content.render(contentWidth, modalHeight-2)
	}
	return renderPanel("Passing Check Detail", modalWidth, modalHeight, body)
}

func (m model) failedCheckDetailModal(width int, height int) string {
	modalWidth := detailModalWidth(width)
	contentWidth := panelContentWidth(modalWidth)
	content, ok := m.failedCheckDetailContent(contentWidth)
	modalHeight := detailModalHeight(height)
	body := dimStyle.Render("no failed check selected")
	if ok {
		modalHeight = detailModalHeightForContent(height, content.naturalHeight(contentWidth))
		body = content.render(contentWidth, modalHeight-2)
	}
	return renderPanel("Failed Check Detail", modalWidth, modalHeight, body)
}

func (m model) failureHotspotDetailModal(width int, height int) string {
	modalWidth := detailModalWidth(width)
	contentWidth := panelContentWidth(modalWidth)
	content, ok := m.failureHotspotDetailContent(contentWidth)
	modalHeight := detailModalHeight(height)
	title := "Failure Cause Detail"
	if m.detailHotspotMode == failureHotspotModeTargets {
		title = "Failure Hotspot Detail"
	}
	body := dimStyle.Render("no failure hotspot selected")
	if ok {
		modalHeight = detailModalHeightForContent(height, content.naturalHeight(contentWidth))
		body = content.render(contentWidth, modalHeight-2)
	}
	return renderPanel(title, modalWidth, modalHeight, body)
}

func detailModalWidth(width int) int {
	return intMax(2, width*detailModalWidthPercent/100)
}

func detailModalHeight(height int) int {
	return intMax(2, height*detailModalHeightPercent/100)
}

func detailModalMaxHeight(height int) int {
	return intMax(2, height*detailModalMaxHeightPercent/100)
}

func detailModalHeightForContent(appHeight int, contentHeight int) int {
	baseHeight := detailModalHeight(appHeight)
	baseContentHeight := intMax(0, baseHeight-2)
	if contentHeight <= baseContentHeight {
		return baseHeight
	}
	return clamp(contentHeight+2, baseHeight, detailModalMaxHeight(appHeight))
}

func overlayModal(frame string, width int, height int, modal string) string {
	modalWidth := lipgloss.Width(modal)
	modalHeight := lipgloss.Height(modal)
	if modalWidth <= 0 || modalHeight <= 0 {
		return frame
	}
	x := intMax(0, (width-modalWidth)/2)
	y := intMax(0, (height-modalHeight)/2)
	frameLines := strings.Split(frame, "\n")
	for len(frameLines) < height {
		frameLines = append(frameLines, valueStyle.Render(strings.Repeat(" ", width)))
	}
	modalLines := strings.Split(modal, "\n")
	for i, modalLine := range modalLines {
		row := y + i
		if row < 0 || row >= len(frameLines) {
			continue
		}
		line := frameLines[row]
		left := ansi.Cut(line, 0, x)
		right := ansi.Cut(line, x+modalWidth, width)
		frameLines[row] = left + modalLine + right
	}
	return strings.Join(frameLines, "\n")
}
