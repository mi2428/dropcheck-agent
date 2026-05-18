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
	histogram := recentEventHistogram(m.passingCheckOccurrences(item.Agent, item.Target, item.Step), width, summarySparklineWindow, m.currentTime())
	targetLine := keyStyle.Render("target=") + valueStyle.Render(item.Target.Name) +
		keyStyle.Render("  ssid=") + valueStyle.Render(firstNonEmpty(item.Target.SSID, "-")) +
		keyStyle.Render("  step=") + valueStyle.Render(item.Step.Name)
	if m.MultiAgent && agentKey(item.Agent) != "" {
		targetLine = keyStyle.Render("agent=") + valueStyle.Render(agentLabel(item.Agent)) +
			keyStyle.Render("  ") + targetLine
	}
	lines := []string{
		targetLine,
		keyStyle.Render("status=") + valueStyle.Render(firstNonEmpty(item.Step.Status, "ok")) +
			keyStyle.Render("  type=") + valueStyle.Render(firstNonEmpty(item.Step.Type, "-")) +
			keyStyle.Render("  op=") + valueStyle.Render(firstNonEmpty(item.Step.Operation, "-")) +
			keyStyle.Render("  duration=") + valueStyle.Render(durationLabel(item.Duration)),
		keyStyle.Render("count=") + valueStyle.Render(fmt.Sprint(item.Count)) +
			keyStyle.Render("  last=") + valueStyle.Render(item.Last.Format("15:04:05")),
	}
	if histogram.Count > 0 {
		lines[2] += detailHistogramSummary(histogram, detailCompactGraphHeight(height, len(lines), 2))
	}
	sections := []detailSection{
		{Title: "recent passes", Rows: m.passingCheckDetailRows(item)},
		{Title: "logs", Rows: m.passingCheckRelatedLogLines(item, detailModalLogLimit)},
	}
	return denseDetailView(lines, histogram, okGraphStyle, sections, width, height)
}

func (m model) failedCheckDetailView(width int, height int) string {
	rows := m.filteredFailedCheckSummaries()
	if len(rows) == 0 || height <= 0 {
		return dimStyle.Render("no failed check selected")
	}
	selected := clamp(m.failedCheckCursor, 0, len(rows)-1)
	item := rows[selected]
	finding := item.Finding
	histogram := recentEventHistogram(m.failedCheckOccurrences(item.Agent, item.Target, finding), width, summarySparklineWindow, m.currentTime())
	targetLine := keyStyle.Render("target=") + valueStyle.Render(finding.Target) +
		keyStyle.Render("  check=") + valueStyle.Render(finding.Check) +
		keyStyle.Render("  metric=") + valueStyle.Render(finding.Metric)
	if m.MultiAgent && agentKey(item.Agent) != "" {
		targetLine = keyStyle.Render("agent=") + valueStyle.Render(agentLabel(item.Agent)) +
			keyStyle.Render("  ") + targetLine
	}
	lines := []string{
		targetLine,
		keyStyle.Render("observed=") + valueStyle.Render(finding.Observed) +
			keyStyle.Render("  expected=") + valueStyle.Render(detailValue(finding.Expected)) +
			keyStyle.Render("  message=") + valueStyle.Render(finding.Message),
		keyStyle.Render("count=") + valueStyle.Render(fmt.Sprint(item.Count)) +
			keyStyle.Render("  last=") + valueStyle.Render(item.Last.Format("15:04:05")),
	}
	if histogram.Count > 0 {
		lines[2] += detailHistogramSummary(histogram, detailCompactGraphHeight(height, len(lines), 2))
	}
	sections := []detailSection{
		{Title: "recent failures", Rows: m.failedCheckDetailRows(item)},
		{Title: "logs", Rows: m.failedCheckRelatedLogLines(item, detailModalLogLimit)},
	}
	return denseDetailView(lines, histogram, failGraphStyle, sections, width, height)
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
	finding := item.LatestFinding
	histogram := recentEventHistogram(m.failureHotspotOccurrences(item), width, summarySparklineWindow, m.currentTime())
	rate := 0
	if item.RunCount > 0 {
		rate = item.FailRunCount * 100 / item.RunCount
	}
	targetLine := keyStyle.Render("target=") + valueStyle.Render(checkStatusTargetLabel(item.Target)) +
		keyStyle.Render("  ssid=") + valueStyle.Render(firstNonEmpty(item.Target.SSID, "-")) +
		keyStyle.Render("  band=") + valueStyle.Render(firstNonEmpty(item.Target.Band, "-"))
	if m.MultiAgent && agentKey(item.Agent) != "" {
		targetLine = keyStyle.Render("agent=") + valueStyle.Render(agentLabel(item.Agent)) +
			keyStyle.Render("  ") + targetLine
	}
	lines := []string{
		targetLine,
		keyStyle.Render("cause=") + valueStyle.Render(firstNonEmpty(item.LatestCause, "-")),
		keyStyle.Render("fail_rate=") + valueStyle.Render(fmt.Sprintf("%d%%", rate)) +
			keyStyle.Render("  fail_runs=") + valueStyle.Render(fmt.Sprintf("%d/%d", item.FailRunCount, item.RunCount)) +
			keyStyle.Render("  failures=") + valueStyle.Render(fmt.Sprint(item.FailCount)) +
			keyStyle.Render("  streak=") + valueStyle.Render(fmt.Sprint(item.FailStreak)) +
			keyStyle.Render("  last=") + valueStyle.Render(item.Last.Format("15:04:05")),
		keyStyle.Render("check=") + valueStyle.Render(firstNonEmpty(finding.Check, "-")) +
			keyStyle.Render("  metric=") + valueStyle.Render(firstNonEmpty(finding.Metric, "-")) +
			keyStyle.Render("  observed=") + valueStyle.Render(firstNonEmpty(finding.Observed, "-")) +
			keyStyle.Render("  expected=") + valueStyle.Render(detailValue(finding.Expected)),
	}
	if histogram.Count > 0 {
		lines[2] += detailHistogramSummary(histogram, detailCompactGraphHeight(height, len(lines), 3))
	}
	sections := []detailSection{
		{Title: "causes", Rows: m.failureHotspotCauseRows(item)},
		{Title: "recent failures", Rows: m.failureHotspotDetailRows(item)},
		{Title: "logs", Rows: m.failureHotspotRelatedLogLines(item, detailModalLogLimit)},
	}
	return denseDetailView(lines, histogram, failGraphStyle, sections, width, height)
}

type detailSection struct {
	Title string
	Rows  []string
}

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
	graphHeight := detailCompactGraphHeight(height, len(lines), len(sections))
	if histogram.Count > 0 && graphHeight > 0 && len(lines)+graphHeight <= height {
		lines = append(lines, renderDetailHistogram(histogram, width, graphHeight, graphStyle)...)
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
		for _, row := range rows[:min(len(rows), rowLimit)] {
			if len(lines) >= height {
				break
			}
			lines = append(lines, fitANSI(row, width))
		}
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func detailCompactGraphHeight(height int, used int, sectionCount int) int {
	remaining := height - used
	if remaining < sectionCount*2+4 {
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

func (m model) passingCheckDetailRows(summary passingCheckSummary) []string {
	items := make([]passingCheckState, 0, summary.Count)
	for _, item := range m.PassingChecks {
		if passingCheckSummaryKey(item.Agent, item.Target, item.Step) == passingCheckSummaryKey(summary.Agent, summary.Target, summary.Step) {
			items = append(items, item)
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].When.After(items[j].When)
	})
	rows := []string{summaryKeyStyle.Render("  time      round  status  dur     op/type")}
	for _, item := range items {
		op := firstNonEmpty(item.Step.Operation, item.Step.Type, "-")
		rows = append(rows, valueStyle.Render(fmt.Sprintf("  %s  %-5s  %-6s  %-6s  %s",
			item.When.Format("15:04:05"),
			roundLabel(item.Round),
			firstNonEmpty(item.Step.Status, "ok"),
			durationLabel(item.Duration),
			op,
		)))
	}
	return rows
}

func (m model) failedCheckDetailRows(summary failedCheckSummary) []string {
	items := make([]failedCheckState, 0, summary.Count)
	for _, item := range m.FailedChecks {
		if failedCheckSummaryKey(item.Target, item.Finding) == failedCheckSummaryKey(summary.Target, summary.Finding) {
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
			rows = append(rows, valueStyle.Render("  "+entry.Line))
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

func detailHistogramSummary(histogram occurrenceHistogram, graphHeight int) string {
	plotHeight := max(1, graphHeight-1)
	eventsPerRow := sparklineEventsPerRow(histogram.Max, plotHeight)
	out := keyStyle.Render("  events=") +
		valueStyle.Render("last="+formatBucketDuration(summarySparklineWindow)) +
		keyStyle.Render(" count=") +
		valueStyle.Render(fmt.Sprint(histogram.Count)) +
		keyStyle.Render(" peak=") +
		valueStyle.Render(fmt.Sprint(histogram.Max))
	if eventsPerRow > 1 {
		out += keyStyle.Render(" scale=") + valueStyle.Render(fmt.Sprintf("%d/row", eventsPerRow))
	}
	return out
}

func renderDetailHistogram(histogram occurrenceHistogram, width int, height int, style lipgloss.Style) []string {
	if width <= 0 || height <= 0 || len(histogram.Counts) == 0 || histogram.Max <= 0 {
		return nil
	}
	if height == 1 {
		return renderSparkline(histogram.Counts, histogram.Max, width, 1, style)
	}
	lines := renderSparkline(histogram.Counts, histogram.Max, width, max(1, height-1), style)
	lines = append(lines, summarySparklineAxis(width, summarySparklineWindow))
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
	modalWidth := max(2, width*90/100)
	modalHeight := detailModalHeight(height)
	return renderPanel("Passing Check Detail", modalWidth, modalHeight, m.passingCheckDetailView(panelContentWidth(modalWidth), modalHeight-2))
}

func (m model) failedCheckDetailModal(width int, height int) string {
	modalWidth := max(2, width*90/100)
	modalHeight := detailModalHeight(height)
	return renderPanel("Failed Check Detail", modalWidth, modalHeight, m.failedCheckDetailView(panelContentWidth(modalWidth), modalHeight-2))
}

func (m model) failureHotspotDetailModal(width int, height int) string {
	modalWidth := max(2, width*90/100)
	modalHeight := detailModalHeight(height)
	return renderPanel("Failure Hotspot Detail", modalWidth, modalHeight, m.failureHotspotDetailView(panelContentWidth(modalWidth), modalHeight-2))
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
