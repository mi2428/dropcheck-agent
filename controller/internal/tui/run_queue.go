package tui

import (
	"fmt"
	"strings"

	"dropcheck/controller/internal/watch"

	"charm.land/lipgloss/v2"
)

func (m model) runQueueTreeView(width int, height int) string {
	return m.runQueueTreeViewScoped(watch.AgentSnapshot{}, m.MultiAgent, width, height, m.runQueueOffset, m.runQueueCursor)
}

func (m model) runQueueTreeViewScoped(agent watch.AgentSnapshot, includeAgentLabel bool, width int, height int, offset int, cursor int) string {
	lines := m.runQueueTreeLinesForAgent(width, agent, includeAgentLabel)
	if len(lines) == 0 || height <= 0 {
		return ""
	}
	if len(lines) > height {
		if compact, ok := m.compactRunQueueTreeLinesForAgent(width, height, agent, includeAgentLabel); ok {
			return renderRunQueueLines(compact, width)
		}
	}
	current, ok := m.currentRunQueueLineIndexForAgent(agent)
	if !ok {
		current = cursor
	}
	if m.focus == focusRunQueue && agentKey(agent) == "" {
		current = cursor
		ok = true
	}
	current = clamp(current, 0, len(lines)-1)
	start := stableOffset(current, offset, height, len(lines))
	end := min(len(lines), start+height)
	if ok && m.focus == focusRunQueue && agentKey(agent) == "" {
		for i := start; i < end; i++ {
			lines[i].Current = i == current
		}
	}
	return renderRunQueueLines(lines[start:end], width)
}

func renderRunQueueLines(lines []runQueueLine, width int) string {
	var b strings.Builder
	for _, line := range lines {
		b.WriteString(renderRunQueueLine(line, width))
		b.WriteByte('\n')
	}
	return b.String()
}

func (m model) runQueuePanelsView(width int, height int) string {
	if m.focus == focusRunQueue {
		return renderPanelFocused("Run Queue", width, height, m.runQueueTreeView(panelContentWidth(width), height-2), true)
	}
	if !m.MultiAgent || height < 6 {
		return renderPanel("Run Queue", width, height, m.runQueueTreeView(panelContentWidth(width), height-2))
	}
	agents := m.runQueueAgents()
	if len(agents) <= 1 || height < len(agents)*3 {
		return renderPanel("Run Queue", width, height, m.runQueueTreeView(panelContentWidth(width), height-2))
	}
	heights := splitHeights(height, len(agents))
	panels := make([]string, 0, len(agents))
	for i, agent := range agents {
		title := "Run Queue " + compactTargetLabel(agentLabel(agent), max(4, width-8))
		panelHeight := heights[i]
		panels = append(panels, renderPanel(title, width, panelHeight, m.runQueueTreeViewForAgent(agent, panelContentWidth(width), panelHeight-2)))
	}
	return lipgloss.JoinVertical(lipgloss.Left, panels...)
}

func (m model) runQueueAgents() []watch.AgentSnapshot {
	return m.outcomeAgents(m.outcomeEvents())
}

func splitHeights(total int, count int) []int {
	if count <= 0 {
		return nil
	}
	heights := make([]int, count)
	base := total / count
	remainder := total % count
	for i := range heights {
		heights[i] = base
		if i < remainder {
			heights[i]++
		}
	}
	return heights
}

func (m model) runQueueTreeViewForAgent(agent watch.AgentSnapshot, width int, height int) string {
	return m.runQueueTreeViewScoped(agent, false, width, height, 0, 0)
}

func renderRunQueueLine(line runQueueLine, width int) string {
	style := runQueueRowStyle(line.Status)
	if line.Current {
		return selectedStyle.Render(padVisible(fitANSI(line.Text, width), width))
	}
	return style.Render(fitANSI(line.Text, width))
}

func (m model) runQueueTreeLines(width int) []runQueueLine {
	return m.runQueueTreeLinesForAgent(width, watch.AgentSnapshot{}, m.MultiAgent)
}

func (m model) runQueueTreeLinesForAgent(width int, agent watch.AgentSnapshot, includeAgentLabel bool) []runQueueLine {
	blocks := m.runQueueTreeBlocksForAgent(width, agent, includeAgentLabel)
	lines := make([]runQueueLine, 0, len(m.Targets)*2)
	for _, block := range blocks {
		lines = append(lines, block.Target)
		lines = append(lines, block.Steps...)
	}
	return lines
}

type runQueueTargetBlock struct {
	Target runQueueLine
	Steps  []runQueueLine
	Status string
	Active bool
}

func (m model) runQueueTreeBlocksForAgent(width int, agent watch.AgentSnapshot, includeAgentLabel bool) []runQueueTargetBlock {
	blocks := make([]runQueueTargetBlock, 0, len(m.Targets))
	for _, target := range m.Targets {
		if agentKey(agent) != "" && !sameAgent(target.Agent, agent) {
			continue
		}
		status := runQueueTargetStatus(target)
		active := normalizeStatus(status) == "running"
		failedChecks := m.targetFailedCheckCount(target.Agent, target.Target.Name)
		suffix := ""
		if failedChecks > 0 {
			suffix = fmt.Sprintf(" fail=%d", failedChecks)
		}
		currentStepName := runQueueCurrentStepName(target)
		line := fmt.Sprintf("%s %s%s", statusToken(status), m.runQueueTargetLabel(target, includeAgentLabel), suffix)
		block := runQueueTargetBlock{
			Target: runQueueLine{Text: fitANSI(line, width), Status: status, Current: active && currentStepName == ""},
			Status: status,
			Active: active,
		}
		steps := runQueueVisibleSteps(target)
		if len(steps) == 0 {
			blocks = append(blocks, block)
			continue
		}
		for i, step := range steps {
			branch := "├──"
			if i == len(steps)-1 {
				branch = "└──"
			}
			stepCurrent := currentStepName == step.Name
			status := step.Status
			if stepCurrent && status == "" {
				status = "running"
			} else if status == "" {
				status = "pending"
			}
			line := fmt.Sprintf("  %s %s %s", branch, statusToken(status), displayCheckName(step.Name))
			block.Steps = append(block.Steps, runQueueLine{Text: fitANSI(line, width), Status: status, Current: stepCurrent})
		}
		blocks = append(blocks, block)
	}
	return blocks
}

func (m model) compactRunQueueTreeLinesForAgent(width int, height int, agent watch.AgentSnapshot, includeAgentLabel bool) ([]runQueueLine, bool) {
	if height <= 0 {
		return nil, false
	}
	blocks := m.runQueueTreeBlocksForAgent(width, agent, includeAgentLabel)
	active := -1
	for i, block := range blocks {
		if block.Active {
			active = i
			break
		}
	}
	if active < 0 {
		return nil, false
	}
	activeLines := compactActiveRunQueueBlock(blocks[active], height)
	if len(activeLines) >= height {
		return activeLines[:min(len(activeLines), height)], true
	}
	lines := make([]runQueueLine, 0, height)
	if previous, ok := previousCompletedRunQueueTarget(blocks, active); ok && len(activeLines)+1 <= height {
		lines = append(lines, previous)
	}
	lines = append(lines, activeLines...)
	for i := active + 1; i < len(blocks) && len(lines) < height; i++ {
		lines = append(lines, blocks[i].Target)
	}
	return lines, true
}

func compactActiveRunQueueBlock(block runQueueTargetBlock, height int) []runQueueLine {
	if height <= 0 {
		return nil
	}
	if len(block.Steps)+1 <= height {
		lines := make([]runQueueLine, 0, len(block.Steps)+1)
		lines = append(lines, block.Target)
		lines = append(lines, block.Steps...)
		return lines
	}
	childHeight := max(0, height-1)
	lines := []runQueueLine{block.Target}
	if childHeight == 0 || len(block.Steps) == 0 {
		return lines
	}
	current := 0
	for i, step := range block.Steps {
		if step.Current {
			current = i
			break
		}
	}
	start := stableOffset(current, 0, childHeight, len(block.Steps))
	end := min(len(block.Steps), start+childHeight)
	lines = append(lines, block.Steps[start:end]...)
	return lines
}

func previousCompletedRunQueueTarget(blocks []runQueueTargetBlock, active int) (runQueueLine, bool) {
	for i := active - 1; i >= 0; i-- {
		switch normalizeStatus(blocks[i].Status) {
		case "ok", "failed", "skipped":
			return blocks[i].Target, true
		}
	}
	return runQueueLine{}, false
}

func runQueueVisibleSteps(target targetState) []stepState {
	if normalizeStatus(runQueueTargetStatus(target)) != "running" {
		return nil
	}
	steps := mergeRunQueueSteps(target.PlannedSteps, target.Steps)
	if len(steps) == 0 {
		return target.Steps
	}
	return steps
}

func runQueueTargetStatus(target targetState) string {
	active := target.CurrentStep != "" || normalizeStatus(target.Status) == "running"
	if active && normalizeStatus(target.Status) != "failed" {
		return "running"
	}
	return target.Status
}

func runQueueCurrentStepName(target targetState) string {
	if strings.TrimSpace(target.CurrentStep) != "" {
		return target.CurrentStep
	}
	if normalizeStatus(runQueueTargetStatus(target)) != "running" {
		return ""
	}
	steps := runQueueVisibleSteps(target)
	for i := len(steps) - 1; i >= 0; i-- {
		if normalizeStatus(steps[i].Status) != "pending" {
			return steps[i].Name
		}
	}
	return ""
}

func mergeRunQueueSteps(planned []stepState, actual []stepState) []stepState {
	steps := make([]stepState, 0, len(planned)+len(actual))
	index := make(map[string]int, len(planned)+len(actual))
	add := func(step stepState, replace bool) {
		name := strings.TrimSpace(step.Name)
		if name == "" {
			name = strings.TrimSpace(step.Type)
			step.Name = name
		}
		if name == "" {
			return
		}
		key := strings.ToLower(name)
		if pos, ok := index[key]; ok {
			if replace {
				steps[pos] = step
			}
			return
		}
		index[key] = len(steps)
		steps = append(steps, step)
	}
	for _, step := range planned {
		add(step, false)
	}
	for _, step := range actual {
		add(step, true)
	}
	return steps
}

func (m model) targetOutcomeStrip(agent watch.AgentSnapshot, target watch.TargetSnapshot, width int, fallbackStatus string) string {
	if width <= 0 {
		return ""
	}
	events := filterOutcomeEvents(m.outcomeEvents(), agent, target)
	if len(events) == 0 && normalizeStatus(fallbackStatus) == "pending" {
		return strings.Repeat("-", width)
	}
	return plainRecentOutcomeStrip(events, width, fallbackStatus)
}

func plainRecentOutcomeStrip(events []outcomeEvent, width int, fallbackStatus string) string {
	if width <= 0 {
		return ""
	}
	if len(events) == 0 {
		switch normalizeStatus(fallbackStatus) {
		case "running":
			return "▌" + strings.Repeat(" ", max(0, width-1))
		case "ok":
			return strings.Repeat("▁", width)
		case "failed":
			return strings.Repeat("█", width)
		default:
			return strings.Repeat("-", width)
		}
	}
	start := max(0, len(events)-width)
	events = events[start:]
	var b strings.Builder
	if leftPad := width - len(events); leftPad > 0 {
		b.WriteString(strings.Repeat("-", leftPad))
	}
	for _, event := range events {
		if event.Status == "failed" {
			b.WriteString("█")
		} else {
			b.WriteString("▁")
		}
	}
	return b.String()
}

func runQueueStripWidth(width int) int {
	switch {
	case width >= 44:
		return 10
	case width >= 34:
		return 8
	case width >= 28:
		return 6
	default:
		return 0
	}
}

func lineWithRightSuffix(line string, suffix string, width int) string {
	if width <= 0 || suffix == "" {
		return line
	}
	suffixWidth := lipgloss.Width(suffix)
	if suffixWidth+2 >= width {
		return line
	}
	lineWidth := lipgloss.Width(line)
	available := width - suffixWidth - 1
	if lineWidth > available {
		line = fit(line, available)
		lineWidth = lipgloss.Width(line)
	}
	return line + strings.Repeat(" ", max(1, available-lineWidth+1)) + suffix
}

// updateRunQueueCursor keeps the queue anchored to the active target or step
// while preserving the existing scroll position whenever the active row remains
// visible. Completed steps can disappear or collapse out of the tree, so the
// fallback clamps the previous cursor into the new line set instead of jumping
// back to the top of the panel.
func (m *model) updateRunQueueCursor() {
	if m.focus == focusRunQueue && m.runQueuePinned {
		m.runQueueCursor = clamp(m.runQueueCursor, 0, max(0, m.runQueueLineCount()-1))
		m.runQueueOffset = clamp(m.runQueueOffset, 0, max(0, m.runQueueLineCount()-m.runQueueViewportHeight()))
		return
	}
	if index, ok := m.currentRunQueueLineIndex(); ok {
		m.runQueueCursor = index
		m.runQueueOffset = stableOffset(index, m.runQueueOffset, m.runQueueViewportHeight(), m.runQueueLineCount())
		return
	}
	m.runQueueCursor = clamp(m.runQueueCursor, 0, max(0, m.runQueueLineCount()-1))
	m.runQueueOffset = clamp(m.runQueueOffset, 0, max(0, m.runQueueLineCount()-m.runQueueViewportHeight()))
}

func (m model) runQueueViewportHeight() int {
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
	lowerHeight := max(1, bodyHeight-checkStatusHeight-roundTimelineHeight)
	return max(1, lowerHeight-2)
}

func (m model) currentRunQueueLineIndex() (int, bool) {
	return m.currentRunQueueLineIndexForAgent(watch.AgentSnapshot{})
}

func (m model) currentRunQueueLineIndexForAgent(agent watch.AgentSnapshot) (int, bool) {
	index := 0
	for _, target := range m.Targets {
		if agentKey(agent) != "" && !sameAgent(target.Agent, agent) {
			continue
		}
		active := normalizeStatus(runQueueTargetStatus(target)) == "running"
		currentStepName := runQueueCurrentStepName(target)
		if active && currentStepName == "" {
			return index, true
		}
		index++
		for _, step := range runQueueVisibleSteps(target) {
			if currentStepName == step.Name {
				return index, true
			}
			index++
		}
	}
	return 0, false
}

func (m *model) moveRunQueueCursor(delta int) {
	count := m.runQueueLineCount()
	if count == 0 {
		m.runQueueCursor = 0
		m.runQueueOffset = 0
		return
	}
	m.runQueuePinned = true
	m.runQueueCursor = clamp(m.runQueueCursor+delta, 0, count-1)
	m.runQueueOffset = stableOffset(m.runQueueCursor, m.runQueueOffset, m.runQueueViewportHeight(), count)
}

func (m model) runQueueLineCount() int {
	count := 0
	for _, target := range m.Targets {
		count++
		count += len(runQueueVisibleSteps(target))
	}
	return count
}
