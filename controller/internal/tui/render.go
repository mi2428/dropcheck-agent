package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

func (m model) render() string {
	width := m.width
	if width <= 0 {
		width = 120
	}
	height := m.height
	if height <= 0 {
		height = 32
	}
	help := m.helpBar(width)
	status := m.statusBar(width)
	bodyHeight := max(4, height-2)
	runQueueWidth := dashboardRunQueueWidth(width)
	leftWidth := dashboardMainWidth(width)
	roundTimelineHeight, checkStatusHeight, summaryHeight, eventLogHeight := m.dashboardPanelHeights(bodyHeight, panelContentWidth(width))
	lowerHeight := max(0, bodyHeight-checkStatusHeight-roundTimelineHeight)
	summaryGap := 1
	if leftWidth < 24 {
		summaryGap = 0
	}
	showHotspots := m.failureHotspotsVisible()
	passingChecksWidth := 0
	failedChecksWidth := 0
	hotspotsWidth := 0
	eventLogWidth := 0
	if showHotspots {
		summaryWidth := max(3, leftWidth-summaryGap*2)
		base := summaryWidth / 3
		remainder := summaryWidth % 3
		passingChecksWidth = base
		failedChecksWidth = base
		hotspotsWidth = base
		if remainder > 0 {
			passingChecksWidth++
		}
		if remainder > 1 {
			failedChecksWidth++
		}
		eventLogWidth = passingChecksWidth + summaryGap + failedChecksWidth
	} else {
		summaryWidth := max(2, leftWidth-summaryGap)
		passingChecksWidth = summaryWidth / 2
		failedChecksWidth = summaryWidth - passingChecksWidth
		eventLogWidth = passingChecksWidth + summaryGap + failedChecksWidth
	}

	checkStatus := ""
	if checkStatusHeight > 0 {
		checkStatus = renderPanelFocused("Latest Check Results", width, checkStatusHeight, m.checkStatusView(panelContentWidth(width), checkStatusHeight-2), m.focus == focusCheckStatus)
	}
	roundTimeline := ""
	if roundTimelineHeight > 0 {
		roundTimeline = renderPanel("Failure Events by Round", width, roundTimelineHeight, m.roundTimelineView(panelContentWidth(width), roundTimelineHeight-2))
	}
	passingChecks := renderPanelFocused("Passing Checks", passingChecksWidth, summaryHeight, m.passingChecksView(panelContentWidth(passingChecksWidth), summaryHeight-2), m.focus == focusPassingChecks)
	failedChecks := renderPanelFocused("Failed Checks", failedChecksWidth, summaryHeight, m.failedChecksView(panelContentWidth(failedChecksWidth), summaryHeight-2), m.focus == focusFailedChecks)
	summaries := lipgloss.JoinHorizontal(lipgloss.Top, passingChecks, horizontalSpacer(summaryGap, summaryHeight), failedChecks)
	eventLog := renderPanel("Event Log", eventLogWidth, eventLogHeight, m.eventLogView(panelContentWidth(eventLogWidth), eventLogHeight-2))
	left := lipgloss.JoinVertical(lipgloss.Left, summaries, eventLog)
	if showHotspots {
		hotspots := m.failureHotspotPanelsView(hotspotsWidth, lowerHeight)
		left = lipgloss.JoinHorizontal(lipgloss.Top, left, horizontalSpacer(summaryGap, lowerHeight), hotspots)
	}
	runQueue := m.runQueuePanelsView(runQueueWidth, lowerHeight)
	lower := lipgloss.JoinHorizontal(lipgloss.Top, left, verticalSpacer(lowerHeight), runQueue)
	body := lower
	if roundTimeline != "" {
		body = lipgloss.JoinVertical(lipgloss.Left, roundTimeline, body)
	}
	if checkStatus != "" {
		body = lipgloss.JoinVertical(lipgloss.Left, checkStatus, body)
	}
	content := lipgloss.JoinVertical(lipgloss.Left, help, body, status)
	frame := appStyle.Width(width).Height(height).Render(content)
	if m.detailOpen {
		frame = overlayModal(frame, width, height, m.detailModal(width, height))
	}
	return frame
}

func (m model) failureHotspotsVisible() bool {
	return dashboardMainWidth(m.width) >= 126
}

func dashboardRunQueueWidth(width int) int {
	if width <= 0 {
		width = 120
	}
	runQueueWidth := clamp(width*25/100, 32, 48)
	if width-runQueueWidth-1 < 50 {
		runQueueWidth = max(24, width-51)
	}
	if width < 80 {
		runQueueWidth = max(20, width/3)
	}
	return runQueueWidth
}

func dashboardMainWidth(width int) int {
	if width <= 0 {
		width = 120
	}
	return max(20, width-dashboardRunQueueWidth(width)-1)
}

func (m model) dashboardPanelHeights(bodyHeight int, roundTimelineWidths ...int) (roundTimelineHeight int, checkStatusHeight int, summaryHeight int, eventLogHeight int) {
	if bodyHeight <= 0 {
		return 0, 0, 0, 0
	}
	if bodyHeight < 14 {
		summaryHeight, eventLogHeight = summaryAndEventLogHeights(bodyHeight)
		return 0, 0, summaryHeight, eventLogHeight
	}
	roundTimelineWidth := 0
	if len(roundTimelineWidths) > 0 {
		roundTimelineWidth = roundTimelineWidths[0]
	} else {
		defaultWidth := m.width
		if defaultWidth <= 0 {
			defaultWidth = 120
		}
		roundTimelineWidth = panelContentWidth(max(20, defaultWidth))
	}
	eventLogHeight = clamp(bodyHeight/6, 4, 7)
	roundTimelineHeight = m.roundTimelinePanelHeight(roundTimelineWidth)
	checkStatusHeight = m.checkStatusPanelHeight()
	summaryHeight = bodyHeight - roundTimelineHeight - checkStatusHeight - eventLogHeight
	for summaryHeight < 8 && roundTimelineHeight > 4 {
		roundTimelineHeight--
		summaryHeight++
	}
	for summaryHeight < 8 && eventLogHeight > 3 {
		eventLogHeight--
		summaryHeight++
	}
	for summaryHeight < 4 && checkStatusHeight > 3 {
		checkStatusHeight--
		summaryHeight++
	}
	if summaryHeight < 4 {
		summaryHeight = max(0, bodyHeight-roundTimelineHeight-checkStatusHeight-eventLogHeight)
	}
	return roundTimelineHeight, checkStatusHeight, summaryHeight, eventLogHeight
}

func (m model) roundTimelinePanelHeight(width int) int {
	contentHeight := 1
	targets := m.checkStatusTargets()
	if len(targets) == 0 {
		contentHeight++ // empty-state line
	} else {
		contentHeight += roundTimelineGrid(width, len(targets)).Rows
	}
	return max(4, contentHeight+2)
}

func (m model) checkStatusPanelHeight() int {
	contentHeight := 1
	if len(m.checkStatusTargets()) == 0 || len(m.checkStatusChecks()) == 0 {
		contentHeight = 1
	} else {
		contentHeight += len(m.checkStatusChecks())
	}
	if len(m.connectStatusFooterItems()) > 0 {
		contentHeight++
	}
	return max(3, contentHeight+2)
}

func summaryAndEventLogHeights(bodyHeight int) (summaryHeight int, eventLogHeight int) {
	if bodyHeight <= 0 {
		return 0, 0
	}
	oldFailedChecksHeight := clamp(bodyHeight*63/100, 7, max(3, bodyHeight-6))
	oldEventLogHeight := bodyHeight - oldFailedChecksHeight
	if oldEventLogHeight < 4 && bodyHeight >= 8 {
		oldEventLogHeight = 4
	}
	eventLogHeight = max(2, (oldEventLogHeight*60+50)/100)
	if bodyHeight >= 12 {
		eventLogHeight = max(4, eventLogHeight)
	}
	if bodyHeight >= 8 {
		eventLogHeight = min(eventLogHeight, bodyHeight-4)
	} else {
		eventLogHeight = min(eventLogHeight, max(1, bodyHeight/3))
	}
	summaryHeight = max(0, bodyHeight-eventLogHeight)
	return summaryHeight, eventLogHeight
}

func (m model) helpBar(width int) string {
	now := m.currentTime().Format("15:04:05")
	label := " Keys:"
	parts := []string{"Tab=Focus", "h/j/k/l=Scroll"}
	if m.focus == focusPassingChecks && len(m.filteredPassingCheckSummaries()) > 0 ||
		m.focus == focusFailedChecks && len(m.filteredFailedCheckSummaries()) > 0 ||
		m.focus == focusFailureHotspots && len(m.focusedFailureHotspotRows()) > 0 {
		parts = append(parts, "Enter=Details")
	}
	if m.focus == focusFailureHotspots {
		parts = append(parts, "m=Mode")
	}
	parts = append(parts, "/=Filter")
	parts = append(parts, "Ctrl-N=Skip")
	if m.paused {
		parts = append(parts, "Esc=Resume")
	} else {
		parts = append(parts, "Ctrl-Z=Pause")
		if m.focusedScrollPinned() {
			parts = append(parts, "Esc=Latest")
		} else if m.hasSearchFilter() {
			parts = append(parts, "Esc=Clear")
		}
	}
	parts = append(parts, "Ctrl-D=PageDown", "Ctrl-U=PageUp", "Ctrl-C=Quit")
	keys := " " + strings.Join(parts, " ")
	if m.searchEditing {
		keys = " filter: type text Enter=Apply Esc=Done Backspace=Delete Ctrl-U=Clear Ctrl-C=Quit"
	}
	right := "Now=" + now
	spaces := width - runeLen(label) - runeLen(keys) - runeLen(right) - 1
	if spaces < 1 {
		return valueStyle.Render(fit(label+keys+" "+right, width))
	}
	return keyStyle.Render(label) +
		valueStyle.Render(keys) +
		valueStyle.Render(strings.Repeat(" ", spaces)) +
		keyStyle.Render("Now=") +
		valueStyle.Render(now) +
		valueStyle.Render(" ")
}

func (m model) currentTime() time.Time {
	return m.State.CurrentTime()
}

func (m model) statusBar(width int) string {
	current := m.currentTargetName()
	leftFields := [][2]string{
		{"status", firstNonEmpty(m.RoundStatus, "starting")},
		{"plan", firstNonEmpty(m.Title, "watch")},
		{"round", fmt.Sprint(m.Round)},
		{"focus", m.focusName()},
		{"current", current},
		{"targets", fmt.Sprint(len(m.Targets))},
		{"ok", fmt.Sprint(m.targetCount("ok"))},
		{"failed", fmt.Sprint(m.targetCount("failed"))},
		{"failed_checks", fmt.Sprint(len(m.FailedChecks))},
	}
	rightFields := [][2]string{}
	if m.paused {
		rightFields = append(rightFields, [2]string{"Paused", ""})
	}
	if m.searchEditing || m.hasSearchFilter() {
		rightFields = append(rightFields, [2]string{"filter", "/" + m.activeSearchQuery()})
	}
	leftPlain := statusPlain(leftFields)
	rightPlain := statusRightPlain(rightFields)
	plainWidth := runeLen(leftPlain)
	if rightPlain != "" {
		plainWidth += 1 + runeLen(rightPlain)
	}
	if plainWidth > width {
		if rightPlain == "" {
			return valueStyle.Render(fit(leftPlain, width))
		}
		rightWidth := runeLen(rightPlain)
		if rightWidth >= width {
			return renderStatusFields(rightFields, width, true)
		}
		leftBudget := max(0, width-rightWidth-1)
		return renderStatusFields(leftFields, leftBudget, false) + valueStyle.Render(" ") + renderStatusFields(rightFields, rightWidth, true)
	}
	left := renderStatusFields(leftFields, runeLen(leftPlain), false)
	if rightPlain == "" {
		return left + valueStyle.Render(strings.Repeat(" ", max(0, width-runeLen(leftPlain))))
	}
	right := renderStatusFields(rightFields, runeLen(rightPlain), true)
	spaces := width - runeLen(leftPlain) - runeLen(rightPlain)
	return left + valueStyle.Render(strings.Repeat(" ", max(1, spaces))) + right
}

func statusBarValueStyle(key string, value string) lipgloss.Style {
	switch key {
	case "status":
		return checkStatusStyle(value)
	case "Paused":
		return runningStatusStyle
	default:
		return valueStyle
	}
}

func statusPlain(fields [][2]string) string {
	var b strings.Builder
	b.WriteString(" ")
	for i, field := range fields {
		if i > 0 {
			b.WriteString(" ")
		}
		b.WriteString(field[0])
		b.WriteString("=")
		b.WriteString(field[1])
	}
	return b.String()
}

func statusRightPlain(fields [][2]string) string {
	if len(fields) == 0 {
		return ""
	}
	var b strings.Builder
	for i, field := range fields {
		if i > 0 {
			b.WriteString(" ")
		}
		if field[1] == "" {
			b.WriteString(field[0])
			continue
		}
		b.WriteString(field[0])
		b.WriteString("=")
		b.WriteString(field[1])
	}
	return b.String()
}

func renderStatusFields(fields [][2]string, width int, right bool) string {
	if width <= 0 {
		return ""
	}
	plain := statusPlain(fields)
	if right {
		plain = statusRightPlain(fields)
	}
	if runeLen(plain) > width {
		return valueStyle.Render(fit(plain, width))
	}
	var b strings.Builder
	if !right {
		b.WriteString(valueStyle.Render(" "))
	}
	for i, field := range fields {
		if i > 0 {
			b.WriteString(valueStyle.Render(" "))
		}
		if field[1] == "" {
			b.WriteString(statusBarValueStyle(field[0], field[1]).Render(field[0]))
			continue
		}
		b.WriteString(keyStyle.Render(field[0] + "="))
		b.WriteString(statusBarValueStyle(field[0], field[1]).Render(field[1]))
	}
	return b.String()
}

func (m model) focusName() string {
	switch m.focus {
	case focusPassingChecks:
		return "passing_checks"
	case focusCheckStatus:
		return "check_status"
	case focusRunQueue:
		if m.runQueuePanelsSplit() {
			key := m.currentRunQueueAgentKey()
			for _, agent := range m.runQueueAgents() {
				if roundAgentKey(agent) == key {
					return "run_queue:" + compactTargetLabel(agentLabel(agent), 24)
				}
			}
		}
		return "run_queue"
	case focusFailureHotspots:
		name := "failure_hotspots"
		if m.failureHotspotMode == failureHotspotModeCauses {
			name = "failure_causes"
		}
		if m.failureHotspotPanelsSplit() {
			key := m.currentHotspotAgentKey()
			for _, agent := range m.failureHotspotAgents() {
				if roundAgentKey(agent) == key {
					return name + ":" + compactTargetLabel(agentLabel(agent), 24)
				}
			}
		}
		return name
	default:
		return "failed_checks"
	}
}

func (m model) eventLogView(width int, height int) string {
	var b strings.Builder
	used := 0
	targetName, stepName, last := m.eventLogSummary()
	if targetName != "" && height > 0 {
		b.WriteString(keyStyle.Render("target="))
		targetText := fitText(targetName, max(1, width/4))
		b.WriteString(valueStyle.Render(targetText))
		b.WriteString(keyStyle.Render("  step="))
		stepText := fitText(firstNonEmpty(stepName, "-"), max(1, width/4))
		b.WriteString(valueStyle.Render(stepText))
		if last != "" {
			b.WriteString(keyStyle.Render("  last="))
			usedWidth := lipgloss.Width("target=") + lipgloss.Width(targetText) + lipgloss.Width("  step=") + lipgloss.Width(stepText) + lipgloss.Width("  last=")
			b.WriteString(valueStyle.Render(fitText(last, max(1, width-usedWidth))))
		}
		b.WriteByte('\n')
		used++
	}
	visibleLogs := max(0, height-used)
	start := len(m.Logs) - visibleLogs
	if start < 0 {
		start = 0
	}
	for _, line := range m.Logs[start:] {
		b.WriteString(logStyle.Render(fitText(line, max(1, width))))
		b.WriteByte('\n')
	}
	return b.String()
}

func (m model) eventLogSummary() (string, string, string) {
	if m.EventLogTarget != "" || m.EventLogStep != "" || m.EventLogLast != "" {
		return m.EventLogTarget, m.EventLogStep, m.EventLogLast
	}
	if target, ok := m.currentTarget(); ok {
		last := ""
		if step, ok := currentStepState(target); ok {
			last = step.Name + " " + firstNonEmpty(step.Message, step.Status)
		}
		return m.targetLabel(target), firstNonEmpty(target.CurrentStep, target.Status), last
	}
	return "", "", ""
}
