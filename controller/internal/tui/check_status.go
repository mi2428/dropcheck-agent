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
	checks, targets = m.filteredCheckStatusAxes(checks, targets)
	if len(checks) == 0 || len(targets) == 0 {
		return dimStyle.Render("no checks or targets match")
	}
	agents := m.outcomeAgents(m.outcomeEvents())
	layout := checkStatusTableLayout(width, checks, targets, agents)
	targets = m.checkStatusVisibleTargets(targets, layout)
	var lines []string
	var header strings.Builder
	header.WriteString(padVisible("Check", layout.LabelWidth))
	header.WriteString(" ")
	for i, target := range targets {
		if i > 0 {
			header.WriteString(" ")
		}
		header.WriteString(padVisible(checkStatusHeaderLabel(target, layout), layout.CellWidth))
	}
	lines = append(lines, tableHeaderStyle.Render(padVisible(header.String(), width)))
	footer := m.connectStatusFooter(width)
	footerHeight := 0
	if footer != "" && height > 1 {
		footerHeight = 1
	} else {
		footer = ""
	}
	visibleRows := max(0, height-1-footerHeight)
	for _, check := range checks {
		if visibleRows <= 0 {
			break
		}
		var b strings.Builder
		b.WriteString(valueStyle.Render(padVisible(displayCheckName(check), layout.LabelWidth)))
		b.WriteString(valueStyle.Render(" "))
		for i, target := range targets {
			if i > 0 {
				b.WriteString(valueStyle.Render(" "))
			}
			cell := m.checkStatusTargetCell(check, target, m.checkStatusAgentsForTarget(target, agents))
			b.WriteString(renderCheckStatusAggregateCell(cell, layout.CellWidth, m.MultiAgent, layout.ShortMode))
		}
		lines = append(lines, padCheckStatusLine(b.String(), width))
		visibleRows--
	}
	if footer != "" {
		for len(lines) < height-1 {
			lines = append(lines, "")
		}
		lines = append(lines, footer)
	}
	return strings.Join(lines, "\n")
}

type checkStatusLayout struct {
	LabelWidth     int
	CellWidth      int
	VisibleTargets int
	ShortMode      bool
}

func checkStatusTableLayout(width int, checks []string, targets []watch.TargetSnapshot, agents []watch.AgentSnapshot) checkStatusLayout {
	labelWidth := clamp(maxCheckStatusLabelWidth(checks), 5, min(27, max(5, width*3/8)))
	available := max(1, width-labelWidth-1)
	fullMinCellWidth := 9
	fullVisible := checkStatusVisibleTargetCount(available, len(targets), fullMinCellWidth)
	shortMode := fullVisible < len(targets) && allCheckStatusTargetsHaveShortName(targets)
	minCellWidth := fullMinCellWidth
	maxCellWidth := 12
	if shortMode {
		minCellWidth = maxCheckStatusShortCellWidth(targets, agents)
		maxCellWidth = max(minCellWidth, 10)
	}
	visibleTargets := checkStatusVisibleTargetCount(available, len(targets), minCellWidth)
	cellWidth := clamp((available-max(0, visibleTargets-1))/visibleTargets, minCellWidth, maxCellWidth)
	return checkStatusLayout{
		LabelWidth:     labelWidth,
		CellWidth:      cellWidth,
		VisibleTargets: visibleTargets,
		ShortMode:      shortMode,
	}
}

func checkStatusVisibleTargetCount(available int, targetCount int, minCellWidth int) int {
	if targetCount <= 0 {
		return 0
	}
	return min(targetCount, max(1, (available+1)/(minCellWidth+1)))
}

func maxCheckStatusShortCellWidth(targets []watch.TargetSnapshot, agents []watch.AgentSnapshot) int {
	width := compactCheckStatusTokenWidth(max(1, len(agents)))
	for _, target := range targets {
		width = max(width, lipgloss.Width(checkStatusTargetShortLabel(target)))
	}
	return max(1, width)
}

func allCheckStatusTargetsHaveShortName(targets []watch.TargetSnapshot) bool {
	if len(targets) == 0 {
		return false
	}
	for _, target := range targets {
		if strings.TrimSpace(target.ShortName) == "" {
			return false
		}
	}
	return true
}

func (m model) checkStatusVisibleTargets(targets []watch.TargetSnapshot, layout checkStatusLayout) []watch.TargetSnapshot {
	visible := min(layout.VisibleTargets, len(targets))
	if visible >= len(targets) {
		return targets
	}
	if visible <= 0 {
		return nil
	}
	offset := clamp(m.checkStatusOffset, 0, len(targets)-visible)
	return targets[offset : offset+visible]
}

func (m *model) moveCheckStatusHorizontal(delta int) {
	if delta == 0 {
		return
	}
	maxOffset := m.checkStatusMaxOffset(panelContentWidth(m.width))
	if maxOffset <= 0 {
		m.checkStatusOffset = 0
		m.checkStatusPinned = false
		return
	}
	m.checkStatusOffset = clamp(m.checkStatusOffset+delta, 0, maxOffset)
	m.checkStatusPinned = true
}

func padCheckStatusLine(line string, width int) string {
	line = fitANSI(line, width)
	return line + valueStyle.Render(strings.Repeat(" ", max(0, width-lipgloss.Width(line))))
}

func (m *model) normalizeCheckStatusOffset() {
	targets, layout, maxOffset := m.checkStatusWindowMetrics(panelContentWidth(m.width))
	if maxOffset <= 0 {
		m.checkStatusOffset = 0
		m.checkStatusPinned = false
		return
	}
	m.checkStatusOffset = clamp(m.checkStatusOffset, 0, maxOffset)
	if m.checkStatusPinned {
		return
	}
	active := m.checkStatusActiveTargetIndex(targets)
	if active < 0 {
		return
	}
	visible := min(layout.VisibleTargets, len(targets))
	if visible <= 0 {
		return
	}
	desiredPosition := max(0, visible-2)
	m.checkStatusOffset = clamp(active-desiredPosition, 0, maxOffset)
}

func (m model) checkStatusMaxOffset(width int) int {
	_, _, maxOffset := m.checkStatusWindowMetrics(width)
	return maxOffset
}

func (m model) checkStatusWindowMetrics(width int) ([]watch.TargetSnapshot, checkStatusLayout, int) {
	targets := m.checkStatusTargets()
	if len(targets) == 0 {
		return nil, checkStatusLayout{}, 0
	}
	checks := m.checkStatusChecks()
	if len(checks) == 0 {
		return targets, checkStatusLayout{}, 0
	}
	checks, targets = m.filteredCheckStatusAxes(checks, targets)
	if len(checks) == 0 || len(targets) == 0 {
		return nil, checkStatusLayout{}, 0
	}
	agents := m.outcomeAgents(m.outcomeEvents())
	layout := checkStatusTableLayout(width, checks, targets, agents)
	return targets, layout, max(0, len(targets)-layout.VisibleTargets)
}

func (m model) filteredCheckStatusAxes(checks []string, targets []watch.TargetSnapshot) ([]string, []watch.TargetSnapshot) {
	query := watchstate.NormalizedSearchQuery(m.panelFilterQuery(focusCheckStatus))
	if query == "" {
		return checks, targets
	}
	matchedChecks := make([]string, 0, len(checks))
	for _, check := range checks {
		if checkStatusCheckMatches(check, query) {
			matchedChecks = append(matchedChecks, check)
		}
	}
	matchedTargets := make([]watch.TargetSnapshot, 0, len(targets))
	for _, target := range targets {
		if checkStatusTargetMatches(target, query) {
			matchedTargets = append(matchedTargets, target)
		}
	}
	if len(matchedChecks) == 0 && len(matchedTargets) == 0 {
		return nil, nil
	}
	if len(matchedChecks) > 0 {
		checks = matchedChecks
	}
	if len(matchedTargets) > 0 {
		targets = matchedTargets
	}
	return checks, targets
}

func checkStatusCheckMatches(check string, query string) bool {
	return watchstate.FieldsContainQuery([]string{
		check,
		displayCheckName(check),
	}, query)
}

func checkStatusTargetMatches(target watch.TargetSnapshot, query string) bool {
	return watchstate.FieldsContainQuery([]string{
		target.Name,
		target.ShortName,
		target.Agent,
		target.SSID,
		target.BSSID,
		target.Band,
		checkStatusTargetLabel(target),
		checkStatusTargetShortLabel(target),
	}, query)
}

func (m model) checkStatusActiveTargetIndex(targets []watch.TargetSnapshot) int {
	for i := len(targets) - 1; i >= 0; i-- {
		if m.checkStatusTargetRunning(targets[i]) {
			return i
		}
	}
	for i := len(targets) - 1; i >= 0; i-- {
		if m.checkStatusTargetFailed(targets[i]) {
			return i
		}
	}
	return -1
}

func (m model) checkStatusTargetRunning(target watch.TargetSnapshot) bool {
	targetKey := checkStatusTargetKey(target)
	if targetKey == "" {
		return false
	}
	for _, state := range m.Targets {
		if checkStatusTargetKey(state.Target) != targetKey {
			continue
		}
		if normalizeStatus(state.Status) == "running" || strings.TrimSpace(state.CurrentStep) != "" {
			return true
		}
		for _, step := range state.Steps {
			if normalizeStatus(step.Status) == "running" {
				return true
			}
		}
	}
	return false
}

func (m model) checkStatusTargetFailed(target watch.TargetSnapshot) bool {
	targetKey := checkStatusTargetKey(target)
	if targetKey == "" {
		return false
	}
	for _, state := range m.Targets {
		if checkStatusTargetKey(state.Target) != targetKey {
			continue
		}
		if normalizeStatus(state.Status) == "failed" {
			return true
		}
		for _, step := range state.Steps {
			if normalizeStatus(step.Status) == "failed" {
				return true
			}
		}
	}
	return false
}

func checkStatusHeaderLabel(target watch.TargetSnapshot, layout checkStatusLayout) string {
	if layout.ShortMode {
		return fitANSI(checkStatusTargetShortLabel(target), layout.CellWidth)
	}
	return compactTargetLabel(checkStatusTargetLabel(target), layout.CellWidth)
}

func (m model) outcomeEvents() []outcomeEvent {
	return m.OutcomeEvents()
}

func (m model) outcomeAgents(events []outcomeEvent) []watch.AgentSnapshot {
	return m.OutcomeAgents(events)
}

func (m model) checkStatusTargets() []watch.TargetSnapshot {
	return m.CheckStatusTargets()
}

func (m model) checkStatusChecks() []string {
	return m.CheckStatusChecks()
}

func (m model) checkStatusAgentsForTarget(target watch.TargetSnapshot, fallback []watch.AgentSnapshot) []watch.AgentSnapshot {
	key := checkStatusTargetKey(target)
	seen := make(map[string]bool)
	var agents []watch.AgentSnapshot
	for _, state := range m.Targets {
		if checkStatusTargetKey(state.Target) != key {
			continue
		}
		agentKey := roundAgentKey(state.Agent)
		if seen[agentKey] {
			continue
		}
		seen[agentKey] = true
		agents = append(agents, state.Agent)
	}
	if len(agents) > 0 {
		return agents
	}
	return fallback
}

func (m model) connectStatusFooter(width int) string {
	items := m.connectStatusFooterItems()
	if len(items) == 0 || width <= 0 {
		return ""
	}
	rendered := make([]string, 0, len(items))
	for _, item := range items {
		rendered = append(rendered, renderConnectStatusFooterItem(item))
	}
	return fitANSI(strings.Join(rendered, valueStyle.Render(" ")), width)
}

func (m model) connectStatusFooterItems() []connectState {
	if len(m.ConnectStates) == 0 {
		return nil
	}
	agents := m.Agents
	if len(agents) == 0 {
		agents = m.outcomeAgents(m.outcomeEvents())
	}
	items := make([]connectState, 0, len(m.ConnectStates))
	if len(agents) > 0 {
		for _, agent := range agents {
			if state, ok := m.connectStatusForAgent(agent); ok {
				items = append(items, state)
			}
		}
	}
	for _, state := range m.ConnectStates {
		if connectStatusContainsAgent(items, state.Agent) {
			continue
		}
		items = append(items, state)
	}
	return items
}

func (m model) connectStatusForAgent(agent watch.AgentSnapshot) (connectState, bool) {
	for _, state := range m.ConnectStates {
		if sameAgent(state.Agent, agent) {
			return state, true
		}
	}
	return connectState{}, false
}

func connectStatusContainsAgent(items []connectState, agent watch.AgentSnapshot) bool {
	for _, item := range items {
		if sameAgent(item.Agent, agent) {
			return true
		}
	}
	return false
}

func renderConnectStatusFooterItem(item connectState) string {
	return keyStyle.Render(connectStatusAgentLabel(item)+"=") + connectStatusPhaseStyle(item.Supplicant).Render(firstNonEmpty(item.Supplicant, "-"))
}

func connectStatusAgentLabel(item connectState) string {
	return agentLabel(item.Agent)
}

func connectStatusPhaseStyle(phase string) lipgloss.Style {
	switch strings.ToUpper(strings.TrimSpace(phase)) {
	case "COMPLETED":
		return okStatusStyle
	case "ASSOCIATED", "ASSOCIATING", "FOUR_WAY_HANDSHAKE", "GROUP_HANDSHAKE":
		return runningStatusStyle
	case "DISCONNECTED", "INACTIVE", "INTERFACE_DISABLED", "UNINITIALIZED", "INVALID":
		return waitStatusStyle
	default:
		return valueStyle
	}
}

func maxCheckStatusLabelWidth(checks []string) int {
	width := 5
	for _, check := range checks {
		width = max(width, lipgloss.Width(displayCheckName(check)))
	}
	return width
}

func outcomeCounts(events []outcomeEvent) (ok int, failed int) {
	return watchstate.OutcomeCounts(events)
}

func renderCheckStatusAggregateCell(cell checkStatusAggregate, width int, multiAgent bool, compact bool) string {
	if width <= 0 {
		return ""
	}
	token := checkStatusAggregateToken(cell, multiAgent, compact)
	if cell.Stale {
		return staleStatusStyle(cell.Status).Render(padVisible(token, width))
	}
	return checkStatusStyle(cell.Status).Render(padVisible(token, width))
}

func checkStatusAggregateToken(cell checkStatusAggregate, multiAgent bool, compact bool) string {
	status := normalizeStatus(cell.Status)
	count := cell.Count
	if status == "failed" && count == 0 {
		count = cell.Failed
	}
	if multiAgent && cell.Total > 1 && count > 0 && (count < cell.Total || status == "running") {
		if compact {
			return fmt.Sprintf("%s%d/%d", compactCheckStatusToken(status), count, cell.Total)
		}
		return fmt.Sprintf("%s(%d%%)", checkStatusToken(status), 100*count/cell.Total)
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

func compactCheckStatusToken(status string) string {
	switch normalizeStatus(status) {
	case "ok":
		return "OK"
	case "failed":
		return "NG"
	case "running":
		return "RUN"
	case "skipped":
		return "SK"
	default:
		return "WAIT"
	}
}

func compactCheckStatusTokenWidth(total int) int {
	width := 4 // WAIT
	for _, status := range []string{"ok", "failed", "running", "skipped"} {
		width = max(width, lipgloss.Width(compactCheckStatusToken(status)))
	}
	if total > 1 {
		count := max(1, total-1)
		for _, status := range []string{"ok", "failed", "running", "skipped"} {
			width = max(width, lipgloss.Width(fmt.Sprintf("%s%d/%d", compactCheckStatusToken(status), count, total)))
		}
	}
	return width
}

func checkStatusTargetKey(target watch.TargetSnapshot) string {
	return watchstate.CheckStatusTargetKey(target)
}

func checkStatusTargetLabel(target watch.TargetSnapshot) string {
	return watchstate.CheckStatusTargetLabel(target)
}

func checkStatusTargetShortLabel(target watch.TargetSnapshot) string {
	return watchstate.CheckStatusTargetShortLabel(target)
}

func (m model) checkStatusTargetCell(check string, target watch.TargetSnapshot, agents []watch.AgentSnapshot) checkStatusAggregate {
	return m.CheckStatusTargetCell(check, target, agents)
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
