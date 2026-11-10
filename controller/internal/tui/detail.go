package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/mi2428/dropcheck-agent/controller/internal/tuirun"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

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
		{Title: "logs", Rows: m.passingCheckRelatedLogLines(item, detailLogLimit), WrapLogs: true},
	}
	return detailContent{
		Fields:     fields,
		Histogram:  histogram,
		GraphStyle: summaryGraphStyle,
		Sections:   sections,
	}, true
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
		{Title: "logs", Rows: m.failedCheckRelatedLogLines(item, detailLogLimit), WrapLogs: true},
	}
	return detailContent{
		Fields:     fields,
		Histogram:  histogram,
		GraphStyle: summaryGraphStyle,
		Sections:   sections,
	}, true
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
		{Title: "logs", Rows: m.failureHotspotRelatedLogLines(item, detailLogLimit), WrapLogs: true},
	}
	return detailContent{
		Fields:     fields,
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
		{Title: "logs", Rows: m.failureCauseRelatedLogLines(item, detailLogLimit), WrapLogs: true},
	}
	return detailContent{
		Fields:     fields,
		Histogram:  histogram,
		GraphStyle: summaryGraphStyle,
		Sections:   sections,
	}
}

type detailContent struct {
	Fields     []detailField
	Histogram  occurrenceHistogram
	GraphStyle lipgloss.Style
	Sections   []detailSection
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
		detailField{Key: "duration", Value: knownDurationLabel(item.Duration, item.DurationKnown)},
		detailField{Key: "avg", Value: knownDurationLabel(item.AvgDuration(), item.DurationCount > 0)},
		detailField{Key: "max", Value: knownDurationLabel(item.MaxDuration, item.DurationCount > 0)},
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
		fields = append(fields, detailField{Key: "message", Value: finding.Message})
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
		detailField{Key: "latest_cause", Value: firstNonEmpty(item.LatestCause, "-")},
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
		detailField{Key: "cause", Value: firstNonEmpty(item.Cause, "-")},
		detailField{Key: "targets", Value: failureCauseTargetsLabel(item, width)},
		detailField{Key: "target_count", Value: fmt.Sprint(item.TargetCount)},
		detailField{Key: "fail_rate", Value: fmt.Sprintf("%d%%", rate)},
		detailField{Key: "failed_runs", Value: fmt.Sprintf("%d/%d", item.FailRunCount, item.RunCount)},
		detailField{Key: "failures", Value: fmt.Sprint(item.FailCount)},
		detailField{Key: "last", Value: item.Last.Format("15:04:05")},
	)
	return fields
}

func detailAppendAgentField(fields *[]detailField, agent tuirun.AgentSnapshot, multiAgent bool) {
	if multiAgent && agentKey(agent) != "" {
		*fields = append(*fields, detailField{Key: "device", Value: agentLabel(agent)})
	}
}

func detailAppendTargetFields(fields *[]detailField, target tuirun.TargetSnapshot) {
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

func detailAppendOperationFields(fields *[]detailField, step tuirun.StepSnapshot) {
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

func detailSectionTitle(section detailSection) string {
	switch detailSectionTitleKey(section) {
	case "logs":
		return "Logs"
	case "causes":
		return "Causes"
	case "failure history":
		return "Failure History"
	default:
		title := strings.TrimSpace(strings.TrimSuffix(section.Title, ":"))
		if title == "" {
			return ""
		}
		return detailFieldHeader(strings.ReplaceAll(title, " ", "_"))
	}
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
	total := len(items)
	items = items[:intMin(len(items), detailHistoryLimit)]
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
	return retainedDetailRows(detailTableRows([]string{"Time", "Round", "Check", "Metric", "Observed", "Expected", "Message"}, tableRows, width, nil), len(items), total)
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
	total := len(items)
	items = items[:intMin(len(items), detailHistoryLimit)]
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
	return retainedDetailRows(detailTableRows([]string{"Time", "Round", "Check", "Metric", "Observed", "Expected", "Cause"}, tableRows, width, nil), len(items), total)
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
	total := len(items)
	items = items[:intMin(len(items), detailHistoryLimit)]
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
	return retainedDetailRows(detailTableRows([]string{"Time", "Round", "Target", "Check", "Metric", "Observed", "Expected"}, tableRows, width, nil), len(items), total)
}

func retainedDetailRows(rows []string, shown, total int) []string {
	if shown < total {
		rows = append(rows, dimStyle.Render(fmt.Sprintf("Showing newest %d of %d retained entries; use --jsonl when full-stream export is required", shown, total)))
	}
	return rows
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
			return event.Kind == tuirun.EventTargetStarted || event.Kind == tuirun.EventTargetFinished
		}
		if !eventMatchesStep(event, item.Step) {
			return false
		}
		return event.Kind == tuirun.EventStepStarted || event.Kind == tuirun.EventStepFinished || event.Kind == tuirun.EventLog
	})
}

func (m model) failedCheckRelatedLogLines(item failedCheckSummary, limit int) []string {
	return m.scopedLogLines(limit, func(entry eventLogEntry) bool {
		event := entry.Event
		if event.Kind == "" || !scopedEventAgentMatches(event.Agent, item.Agent) || !scopedEventTargetMatches(event, item.Target, item.Finding.Target) {
			return false
		}
		if event.Finding != nil && failedCheckKey(*event.Finding) == failedCheckKey(item.Finding) {
			return eventMatchesFinding(event, item.Finding)
		}
		if !eventMatchesFinding(event, item.Finding) {
			return false
		}
		return event.Kind == tuirun.EventStepStarted || event.Kind == tuirun.EventStepFinished || event.Kind == tuirun.EventLog
	})
}

func (m model) failureHotspotRelatedLogLines(item failureHotspotSummary, limit int) []string {
	return m.scopedLogLines(limit, func(entry eventLogEntry) bool {
		event := entry.Event
		if event.Kind == "" || !scopedEventAgentMatches(event.Agent, item.Agent) || !scopedEventTargetMatches(event, item.Target, item.LatestFinding.Target) {
			return false
		}
		switch event.Kind {
		case tuirun.EventFinding:
			return true
		case tuirun.EventStepFinished:
			return eventFailureLike(event)
		case tuirun.EventStepStarted:
			return eventMatchesFinding(event, item.LatestFinding)
		case tuirun.EventLog:
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
		if event.Kind != tuirun.EventStepFinished && event.Kind != tuirun.EventLog {
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

func scopedEventAgentMatches(eventAgent tuirun.AgentSnapshot, selected tuirun.AgentSnapshot) bool {
	return sameAgent(eventAgent, selected)
}

func scopedEventTargetMatches(event tuirun.Event, target tuirun.TargetSnapshot, aliases ...string) bool {
	ids := []string{event.Scope.TargetID, event.Target.ID}
	if event.Finding != nil {
		ids = append(ids, event.Finding.TargetID)
	}
	if matched, identified := scopeIDsMatch(target.ID, ids...); identified {
		return matched
	}
	expected := scopedTargetKey(target, aliases...)
	actual := scopedEventTargetKey(event)
	return expected != "" && actual != "" && expected == actual
}

func scopedEventTargetKey(event tuirun.Event) string {
	findingTarget := ""
	if event.Finding != nil {
		findingTarget = event.Finding.Target
	}
	return scopedTargetKey(event.Target, findingTarget)
}

func scopedTargetKey(target tuirun.TargetSnapshot, aliases ...string) string {
	values := []string{target.Name}
	values = append(values, aliases...)
	values = append(values, target.SSID, target.BSSID)
	return normalizedScopeToken(firstNonEmpty(values...))
}

func eventMatchesFinding(event tuirun.Event, finding tuirun.Finding) bool {
	return eventMatchesStep(event, tuirun.StepSnapshot{ID: finding.CheckID, Name: finding.Check, Type: finding.Check})
}

func eventMatchesStep(event tuirun.Event, step tuirun.StepSnapshot) bool {
	ids := []string{event.Scope.CheckID, event.Step.ID}
	if event.Finding != nil {
		ids = append(ids, event.Finding.CheckID)
	}
	if matched, identified := scopeIDsMatch(step.ID, ids...); identified {
		return matched
	}
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

// Any available stable ID is authoritative; inconsistent or absent IDs cannot
// be repaired by overlapping display names or operation types.
func scopeIDsMatch(expected string, actual ...string) (matched, identified bool) {
	identified = expected != ""
	seen := false
	for _, id := range actual {
		if id == "" {
			continue
		}
		identified, seen = true, true
		if id != expected {
			return false, true
		}
	}
	return expected != "" && seen, identified
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

func targetSummaryStep(step tuirun.StepSnapshot) bool {
	return normalizedScopeToken(firstNonEmpty(step.Name, step.Type)) == "target"
}

func eventFailureLike(event tuirun.Event) bool {
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
