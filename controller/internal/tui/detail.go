package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"dropcheck/controller/internal/watch"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func (m model) passingCheckDetailView(width int, height int) string {
	rows := m.filteredPassingCheckSummaries()
	if len(rows) == 0 || height <= 0 {
		return dimStyle.Render("no passing check selected")
	}
	selected := clamp(m.passingCheckCursor, 0, len(rows)-1)
	item := rows[selected]
	histogram := recentEventHistogram(m.passingCheckOccurrences(item.Agent, item.Target, item.Step), width, detailTimelineWindow, m.currentTime())
	fields := detailPassingSummaryFields(item, m.MultiAgent)
	sections := []detailSection{
		{Title: "logs", Rows: m.passingCheckRelatedLogLines(item, detailModalLogLimit), WrapLogs: true},
	}
	return denseDetailView(detailSummaryTableLines(fields, width), histogram, okGraphStyle, detailTimelineLabel(histogram), sections, width, height)
}

func (m model) failedCheckDetailView(width int, height int) string {
	rows := m.filteredFailedCheckSummaries()
	if len(rows) == 0 || height <= 0 {
		return dimStyle.Render("no failed check selected")
	}
	selected := clamp(m.failedCheckCursor, 0, len(rows)-1)
	item := rows[selected]
	finding := item.Finding
	histogram := recentEventHistogram(m.failedCheckOccurrences(item.Agent, item.Target, finding), width, detailTimelineWindow, m.currentTime())
	fields := detailFailedSummaryFields(item, m.MultiAgent)
	sections := []detailSection{
		{Title: "failure history", Rows: m.failedCheckDetailRows(item)},
		{Title: "logs", Rows: m.failedCheckRelatedLogLines(item, detailModalLogLimit), WrapLogs: true},
	}
	return denseDetailView(detailSummaryTableLines(fields, width), histogram, failGraphStyle, detailTimelineLabel(histogram), sections, width, height)
}

func (m model) failureHotspotDetailView(width int, height int) string {
	rows := m.focusedFailureHotspotRows()
	if len(rows) == 0 || height <= 0 {
		return dimStyle.Render("no failure hotspot selected")
	}
	selected := 0
	for i, row := range rows {
		if row.Index == m.failureHotspotCursor {
			selected = i
			break
		}
	}
	item := rows[selected].Item
	histogram := recentEventHistogram(m.failureHotspotOccurrences(item), width, detailTimelineWindow, m.currentTime())
	fields := detailHotspotSummaryFields(item, m.MultiAgent)
	sections := []detailSection{
		{Title: "causes", Rows: m.failureHotspotCauseRows(item)},
		{Title: "failure history", Rows: m.failureHotspotDetailRows(item)},
		{Title: "logs", Rows: m.failureHotspotRelatedLogLines(item, detailModalLogLimit), WrapLogs: true},
	}
	return denseDetailView(detailSummaryTableLines(fields, width), histogram, failGraphStyle, detailTimelineLabel(histogram), sections, width, height)
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
	detailTimelineHeaderHeight = 1
	detailSummaryColumnGap     = 2
	detailSummaryMinCellWidth  = 4
	detailSummaryMaxCellWidth  = 30
	detailSummaryTargetCell    = 12
)

func denseDetailView(summary []string, histogram occurrenceHistogram, graphStyle lipgloss.Style, timeline string, sections []detailSection, width int, height int) string {
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
	graphHeight := detailCompactGraphHeight(height, len(lines), len(sections))
	if histogram.Count > 0 && graphHeight > 0 && len(lines)+graphHeight+detailGraphVerticalPadding+detailTimelineHeaderHeight <= height {
		lines = append(lines, "")
		lines = append(lines, fitANSI(timeline, width))
		lines = append(lines, renderDetailHistogram(histogram, width, graphHeight, graphStyle)...)
		lines = append(lines, "")
	}
	remaining := height - len(lines)
	if remaining <= 0 || len(sections) == 0 {
		return strings.Join(lines[:min(len(lines), height)], "\n")
	}
	allocations := detailSectionAllocations(sections, remaining)
	for i, section := range sections {
		if len(lines) >= height || allocations[i] <= 0 {
			continue
		}
		lines = append(lines, fitANSI(summaryKeyStyle.Render(section.Title), width))
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

func detailSummaryTableLines(fields []detailField, width int) []string {
	if width <= 0 {
		return nil
	}
	lines := make([]string, 0, max(1, len(fields)/3)*2)
	pending := make([]detailField, 0, len(fields))
	flush := func() {
		if len(pending) == 0 {
			return
		}
		lines = append(lines, detailSummaryFieldTableLines(pending, width)...)
		pending = pending[:0]
	}
	for _, field := range fields {
		if field.Wide {
			flush()
			lines = append(lines, detailSummaryRenderFieldRow([]detailField{field}, []int{width})...)
			continue
		}
		pending = append(pending, field)
	}
	flush()
	return lines
}

func detailSummaryFieldTableLines(fields []detailField, width int) []string {
	lines := make([]string, 0, max(1, len(fields)/3)*2)
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
	return 1, []int{max(1, width)}
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
		keyWidth := lipgloss.Width(field.Key)
		valueWidth := min(lipgloss.Width(firstNonEmpty(field.Value, "-")), detailSummaryTargetCell)
		widths[i] = max(detailSummaryMinCellWidth, max(keyWidth, valueWidth))
	}
	return widths
}

func detailSummaryPreferredWidth(field detailField) int {
	value := firstNonEmpty(field.Value, "-")
	preferred := max(lipgloss.Width(field.Key), lipgloss.Width(value))
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
		headers = append(headers, detailSummaryCell(keyStyle, field.Key, width))
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

func detailTimelineLabel(histogram occurrenceHistogram) string {
	label := keyStyle.Render("timeline ") +
		summaryKeyStyle.Render("window=") + valueStyle.Render("last="+detailTimelineWindowLabel()) +
		summaryKeyStyle.Render("  peak=") + valueStyle.Render(fmt.Sprint(histogram.Max))
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
	mid := strings.Repeat("-", max(1, width-len(left)-len(right)))
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
	bodyWidth := max(1, width-lipgloss.Width(prefix))
	wrapped := strings.Split(ansi.Wrap(body, bodyWidth, " "), "\n")
	lines := make([]string, 0, max(1, len(wrapped)))
	if len(wrapped) == 0 {
		wrapped = []string{""}
	}
	for i, part := range wrapped {
		linePrefix := prefix
		if i > 0 {
			linePrefix = continuationPrefix
		}
		lines = append(lines, valueStyle.Render(linePrefix+fitANSI(strings.TrimSpace(part), bodyWidth)))
	}
	return lines
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

func detailCompactGraphHeight(height int, used int, sectionCount int) int {
	remaining := height - used
	if remaining < sectionCount*2+4+detailGraphVerticalPadding+detailTimelineHeaderHeight {
		return 0
	}
	if remaining >= 12 {
		return 4
	}
	return 3
}

func detailSectionAllocations(sections []detailSection, available int) []int {
	allocations := make([]int, len(sections))
	if len(sections) == 0 || available <= 0 {
		return allocations
	}
	weights := make([]int, len(sections))
	totalWeight := 0
	for i, section := range sections {
		weight := 1
		if section.Title == "logs" {
			weight = 2
		}
		weights[i] = weight
		totalWeight += weight
	}
	remaining := available
	for i := range sections {
		value := max(1, available*weights[i]/totalWeight)
		allocations[i] = min(value, remaining)
		remaining -= allocations[i]
	}
	for remaining > 0 {
		changed := false
		for i := range allocations {
			if remaining <= 0 {
				break
			}
			allocations[i]++
			remaining--
			changed = true
		}
		if !changed {
			break
		}
	}
	return allocations
}

func (m model) failedCheckDetailRows(summary failedCheckSummary) []string {
	items := make([]failedCheckState, 0, summary.Count)
	for _, item := range m.FailedChecks {
		if failedCheckSummaryKey(item.Agent, item.Target, item.Finding) == failedCheckSummaryKey(summary.Agent, summary.Target, summary.Finding) {
			items = append(items, item)
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].When.After(items[j].When)
	})
	rows := []string{summaryKeyStyle.Render("  time      round  check             metric      observed  expected  message")}
	for _, item := range items {
		finding := item.Finding
		rows = append(rows, valueStyle.Render(fmt.Sprintf("  %s  %-5s  %-16s  %-10s  %-8s  %-8s  %s",
			item.When.Format("15:04:05"),
			roundLabel(item.Round),
			firstNonEmpty(finding.Check, "-"),
			firstNonEmpty(finding.Metric, "-"),
			firstNonEmpty(finding.Observed, "-"),
			detailValue(finding.Expected),
			firstNonEmpty(finding.Message, "-"),
		)))
	}
	return rows
}

func (m model) failureHotspotDetailRows(summary failureHotspotSummary) []string {
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
	rows := []string{summaryKeyStyle.Render("  time      round  check             metric      observed  expected  cause")}
	for _, item := range items {
		finding := item.Finding
		rows = append(rows, valueStyle.Render(fmt.Sprintf("  %s  %-5s  %-16s  %-10s  %-8s  %-8s  %s",
			item.When.Format("15:04:05"),
			roundLabel(item.Round),
			firstNonEmpty(finding.Check, "-"),
			firstNonEmpty(finding.Metric, "-"),
			firstNonEmpty(finding.Observed, "-"),
			detailValue(finding.Expected),
			failureHotspotCause(finding),
		)))
	}
	return rows
}

type failureHotspotCauseSummary struct {
	Cause string
	Count int
	Last  time.Time
}

func (m model) failureHotspotCauseRows(summary failureHotspotSummary) []string {
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
	out := []string{summaryKeyStyle.Render("  count  last      cause")}
	for _, row := range rows {
		out = append(out, valueStyle.Render(fmt.Sprintf("  %-5d  %-8s  %s", row.Count, row.Last.Format("15:04:05"), row.Cause)))
	}
	return out
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
	lines := renderSparkline(histogram.Counts, histogram.Max, width, max(1, height-1), style)
	lines = append(lines, detailTimelineAxis(width))
	return lines[:min(len(lines), height)]
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
	modalHeight := detailModalHeight(height)
	return renderPanel("Passing Check Detail", modalWidth, modalHeight, m.passingCheckDetailView(panelContentWidth(modalWidth), modalHeight-2))
}

func (m model) failedCheckDetailModal(width int, height int) string {
	modalWidth := detailModalWidth(width)
	modalHeight := detailModalHeight(height)
	return renderPanel("Failed Check Detail", modalWidth, modalHeight, m.failedCheckDetailView(panelContentWidth(modalWidth), modalHeight-2))
}

func (m model) failureHotspotDetailModal(width int, height int) string {
	modalWidth := detailModalWidth(width)
	modalHeight := detailModalHeight(height)
	return renderPanel("Failure Hotspot Detail", modalWidth, modalHeight, m.failureHotspotDetailView(panelContentWidth(modalWidth), modalHeight-2))
}

func detailModalWidth(width int) int {
	return max(2, width*detailModalWidthPercent/100)
}

func detailModalHeight(height int) int {
	return max(2, height*detailModalHeightPercent/100)
}

func overlayModal(frame string, width int, height int, modal string) string {
	modalWidth := lipgloss.Width(modal)
	modalHeight := lipgloss.Height(modal)
	if modalWidth <= 0 || modalHeight <= 0 {
		return frame
	}
	x := max(0, (width-modalWidth)/2)
	y := max(0, (height-modalHeight)/2)
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
