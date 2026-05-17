package tui

import (
	"fmt"
	"strings"

	"dropcheck/controller/internal/watch"
	"dropcheck/controller/internal/watchstate"

	"charm.land/lipgloss/v2"
)

func (m model) checkStatusView(width int, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	targets := m.checkStatusTargets()
	if len(targets) == 0 {
		return dimStyle.Render("no targets")
	}
	checks := m.checkStatusChecks()
	if len(checks) == 0 {
		return dimStyle.Render("no checks")
	}
	agents := m.outcomeAgents(m.outcomeEvents())
	labelWidth := clamp(maxCheckStatusLabelWidth(checks), 5, min(18, max(5, width/4)))
	available := max(1, width-labelWidth-1)
	minCellWidth := 9
	visibleTargets := min(len(targets), max(1, (available+1)/(minCellWidth+1)))
	cellWidth := clamp((available-max(0, visibleTargets-1))/visibleTargets, minCellWidth, 12)
	targets = targets[:visibleTargets]
	var lines []string
	var header strings.Builder
	header.WriteString(tableHeaderStyle.Render(padVisible("Check", labelWidth)))
	header.WriteString(tableHeaderStyle.Render(" "))
	for i, target := range targets {
		if i > 0 {
			header.WriteString(tableHeaderStyle.Render(" "))
		}
		header.WriteString(tableHeaderStyle.Render(padVisible(compactTargetLabel(checkStatusTargetLabel(target), cellWidth), cellWidth)))
	}
	lines = append(lines, header.String())
	visibleRows := max(0, height-1)
	for _, check := range checks {
		if visibleRows <= 0 {
			break
		}
		var b strings.Builder
		b.WriteString(valueStyle.Render(padVisible(check, labelWidth)))
		b.WriteString(valueStyle.Render(" "))
		for i, target := range targets {
			if i > 0 {
				b.WriteString(valueStyle.Render(" "))
			}
			cell := m.checkStatusTargetCell(check, target, agents)
			b.WriteString(renderCheckStatusAggregateCell(cell, cellWidth, m.MultiAgent))
		}
		lines = append(lines, b.String())
		visibleRows--
	}
	return strings.Join(lines, "\n")
}

func (m model) agentTargetCheckStatusView(width int, height int) string {
	return m.checkStatusView(width, height)
}

func (m model) outcomeEvents() []outcomeEvent {
	return m.State.OutcomeEvents()
}

func (m model) outcomeAgents(events []outcomeEvent) []watch.AgentSnapshot {
	return m.State.OutcomeAgents(events)
}

func (m model) checkStatusTargets() []watch.TargetSnapshot {
	return m.State.CheckStatusTargets()
}

func (m model) checkStatusChecks() []string {
	return m.State.CheckStatusChecks()
}

func maxCheckStatusLabelWidth(checks []string) int {
	width := 5
	for _, check := range checks {
		width = max(width, lipgloss.Width(check))
	}
	return width
}

func filterOutcomeEvents(events []outcomeEvent, agent watch.AgentSnapshot, target watch.TargetSnapshot) []outcomeEvent {
	return watchstate.FilterOutcomeEvents(events, agent, target)
}

func outcomeCounts(events []outcomeEvent) (ok int, failed int) {
	return watchstate.OutcomeCounts(events)
}

func renderOutcomeStrip(events []outcomeEvent, width int) string {
	if width <= 0 {
		return ""
	}
	if len(events) == 0 {
		return dimStyle.Render(strings.Repeat(" ", width))
	}
	buckets := outcomeBuckets(events, width)
	maxOK := 1
	maxFailed := 1
	for _, bucket := range buckets {
		if bucket.OK > maxOK {
			maxOK = bucket.OK
		}
		if bucket.Failed > maxFailed {
			maxFailed = bucket.Failed
		}
	}
	var b strings.Builder
	for _, bucket := range buckets {
		if bucket.OK+bucket.Failed == 0 {
			b.WriteString(dimStyle.Render(" "))
			continue
		}
		if bucket.Failed > 0 {
			block := intensityBlockWithFloor(bucket.Failed, maxFailed, 4)
			b.WriteString(failGraphStyle.Render(block))
		} else {
			block := intensityBlockWithFloor(bucket.OK, maxOK, 2)
			b.WriteString(okGraphStyle.Render(block))
		}
	}
	return b.String()
}

func outcomeBuckets(events []outcomeEvent, width int) []outcomeBucket {
	return watchstate.OutcomeBuckets(events, width)
}

func renderCheckStatusCell(status string, width int) string {
	if width <= 0 {
		return ""
	}
	status = normalizeStatus(status)
	token := checkStatusToken(status)
	return checkStatusStyle(status).Render(padVisible(token, width))
}

func renderCheckStatusAggregateCell(cell checkStatusAggregate, width int, multiAgent bool) string {
	if width <= 0 {
		return ""
	}
	token := checkStatusAggregateToken(cell, multiAgent)
	if cell.Stale {
		return staleStatusStyle(cell.Status).Render(padVisible(token, width))
	}
	return checkStatusStyle(cell.Status).Render(padVisible(token, width))
}

func checkStatusAggregateToken(cell checkStatusAggregate, multiAgent bool) string {
	status := normalizeStatus(cell.Status)
	count := cell.Count
	if status == "failed" && count == 0 {
		count = cell.Failed
	}
	if multiAgent && cell.Total > 1 && count > 0 && count < cell.Total {
		percent := 100 * count / cell.Total
		return fmt.Sprintf("%s(%d%%)", checkStatusToken(status), percent)
	}
	return checkStatusToken(status)
}

func checkStatusToken(status string) string {
	switch normalizeStatus(status) {
	case "ok":
		return "PASS"
	case "failed":
		return "FAIL"
	case "running":
		return "RUN"
	case "skipped":
		return "SKIP"
	default:
		return "WAIT"
	}
}

func renderRecentOutcomeStrip(events []outcomeEvent, width int, fallbackStatus string) string {
	if width <= 0 {
		return ""
	}
	if len(events) == 0 {
		switch normalizeStatus(fallbackStatus) {
		case "running":
			return warnStyle.Render("▌" + strings.Repeat(" ", max(0, width-1)))
		case "ok":
			return okGraphStyle.Render(strings.Repeat("▁", width))
		case "failed":
			return failGraphStyle.Render(strings.Repeat("█", width))
		default:
			return dimStyle.Render(strings.Repeat("-", width))
		}
	}
	start := max(0, len(events)-width)
	events = events[start:]
	leftPad := width - len(events)
	var b strings.Builder
	if leftPad > 0 {
		b.WriteString(dimStyle.Render(strings.Repeat("-", leftPad)))
	}
	for _, event := range events {
		if event.Status == "failed" {
			b.WriteString(failGraphStyle.Render("█"))
		} else {
			b.WriteString(okGraphStyle.Render("▁"))
		}
	}
	return b.String()
}

func intensityBlock(count int, maxCount int) string {
	return intensityBlockWithFloor(count, maxCount, 0)
}

func intensityBlockWithFloor(count int, maxCount int, floor int) string {
	if count <= 0 || maxCount <= 0 {
		return " "
	}
	blocks := []string{"▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"}
	index := (count*len(blocks) - 1) / maxCount
	index = clamp(index, floor, len(blocks)-1)
	return blocks[index]
}

func maxOutcomeAgentLabelWidth(agents []watch.AgentSnapshot) int {
	width := 4
	for _, agent := range agents {
		if label := outcomeAgentLabel(agent, true); lipgloss.Width(label) > width {
			width = lipgloss.Width(label)
		}
	}
	return width
}

func outcomeAgentLabel(agent watch.AgentSnapshot, multiAgent bool) string {
	if !multiAgent && agentKey(agent) == "" {
		return "all"
	}
	return firstNonEmpty(agentLabel(agent), "all")
}

func checkStatusTargetKey(target watch.TargetSnapshot) string {
	return watchstate.CheckStatusTargetKey(target)
}

func checkStatusTargetLabel(target watch.TargetSnapshot) string {
	return watchstate.CheckStatusTargetLabel(target)
}

func (m model) checkStatusForTarget(agent watch.AgentSnapshot, target watch.TargetSnapshot, events []outcomeEvent) string {
	return m.State.CheckStatusForTarget(agent, target, events)
}

func (m model) checkStatusTargetCell(check string, target watch.TargetSnapshot, agents []watch.AgentSnapshot) checkStatusAggregate {
	return m.State.CheckStatusTargetCell(check, target, agents)
}

func (m model) checkStatusAgentResult(agent watch.AgentSnapshot, target watch.TargetSnapshot, check string) checkStatusAgentResult {
	return m.State.CheckStatusAgentResult(agent, target, check)
}

func (m model) historicalCheckStatus(agent watch.AgentSnapshot, target watch.TargetSnapshot, check string) (string, bool) {
	return m.State.HistoricalCheckStatus(agent, target, check)
}

func (m model) currentCheckStatus(agent watch.AgentSnapshot, target watch.TargetSnapshot, check string) (string, bool) {
	return m.State.CurrentCheckStatus(agent, target, check)
}

func compactTargetLabel(label string, width int) string {
	label = strings.TrimSpace(label)
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(label) <= width {
		return label
	}
	parts := strings.Split(label, "-")
	if len(parts) > 1 {
		candidate := ""
		for i := len(parts) - 1; i >= 0; i-- {
			next := parts[i]
			if candidate != "" {
				next += "-" + candidate
			}
			if lipgloss.Width(next) > width {
				break
			}
			candidate = next
		}
		if candidate != "" {
			return candidate
		}
	}
	return fit(label, width)
}
