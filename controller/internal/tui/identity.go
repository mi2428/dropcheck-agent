package tui

import (
	"strings"

	"dropcheck/controller/internal/watch"
	"dropcheck/controller/internal/watchstate"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func failedCheckKey(finding watch.Finding) string { return watchstate.FailedCheckKey(finding) }
func failedCheckSummaryKey(agent watch.AgentSnapshot, target watch.TargetSnapshot, finding watch.Finding) string {
	return watchstate.FailedCheckSummaryKey(agent, target, finding)
}
func failedCheckSummaryIdentity(item failedCheckSummary) string {
	return watchstate.FailedCheckSummaryIdentity(item)
}
func failedCheckSummaryIndexByIdentity(rows []failedCheckSummary, key string) int {
	return watchstate.FailedCheckSummaryIndexByIdentity(rows, key)
}
func failureHotspotSummaryIdentity(item failureHotspotSummary) string {
	return watchstate.FailureHotspotSummaryIdentity(item)
}
func failureCauseSummaryIdentity(item failureCauseSummary) string {
	return watchstate.FailureCauseSummaryIdentity(item)
}
func failureCauseIdentity(agent watch.AgentSnapshot, finding watch.Finding) string {
	return watchstate.FailureCauseIdentity(agent, watchstate.FailureHotspotCause(finding))
}
func failureHotspotIdentity(agent watch.AgentSnapshot, target watch.TargetSnapshot, findingTargets ...string) string {
	return watchstate.FailureHotspotIdentity(agent, target, findingTargets...)
}
func agentKey(agent watch.AgentSnapshot) string                   { return watchstate.AgentKey(agent) }
func agentLabel(agent watch.AgentSnapshot) string                 { return watchstate.AgentLabel(agent) }
func sameAgent(a watch.AgentSnapshot, b watch.AgentSnapshot) bool { return watchstate.SameAgent(a, b) }

func (m model) targetLabel(target targetState) string {
	return m.runQueueTargetLabel(target, m.MultiAgent)
}

func (m model) runQueueTargetLabel(target targetState, includeAgentLabel bool) string {
	label := target.Target.Name
	if !includeAgentLabel {
		return label
	}
	return agentLabel(target.Agent) + " " + label
}

func passingCheckSummaryIdentity(item passingCheckSummary) string {
	return watchstate.PassingCheckSummaryIdentity(item)
}
func passingCheckSummaryIndexByIdentity(rows []passingCheckSummary, key string) int {
	return watchstate.PassingCheckSummaryIndexByIdentity(rows, key)
}
func durationLabel(duration int64) string { return watchstate.DurationLabel(duration) }

func displayCheckName(name string) string { return watchstate.DisplayCheckName(name) }

func agentDeviceLabel(agent watch.AgentSnapshot) string {
	if agent.DeviceModel != "" {
		return agent.DeviceModel
	}
	for _, value := range []string{agent.Name, agent.ID, agent.SessionID} {
		if value != "" && value != agent.ADBSerial {
			return value
		}
	}
	return "-"
}

func fit(value string, width int) string {
	runes := []rune(value)
	if width <= 0 || len(runes) <= width {
		return value
	}
	if width == 1 {
		return "~"
	}
	return string(runes[:width-1]) + "~"
}

func fitText(value string, width int) string { return fitANSI(sanitizeLogText(value), width) }

func fitANSI(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= width {
		return value
	}
	return ansi.Truncate(value, width, "~")
}

func padToWidth(value string, width int) string {
	if width <= 0 {
		return ""
	}
	value = fit(value, width)
	return value + strings.Repeat(" ", max(0, width-runeLen(value)))
}

func runeLen(value string) int                { return len([]rune(value)) }
func (m model) targetCount(status string) int { return m.TargetCount(status) }

func runQueueRowStyle(status string) lipgloss.Style {
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

func firstNonEmpty(values ...string) string { return watchstate.FirstNonEmpty(values...) }
func max(a, b int) int                      { return watchstate.Max(a, b) }
func min(a, b int) int                      { return watchstate.Min(a, b) }
func clamp(value, low, high int) int        { return watchstate.Clamp(value, low, high) }
func correctedOffset(selected int, visibleRows int, totalRows int) int {
	if totalRows <= 0 {
		return 0
	}
	visibleRows = max(1, visibleRows)
	selected = clamp(selected, 0, totalRows-1)
	if totalRows <= visibleRows {
		return 0
	}
	start := selected - visibleRows/2
	return clamp(start, 0, totalRows-visibleRows)
}
func stableOffset(selected int, currentOffset int, visibleRows int, totalRows int) int {
	if totalRows <= 0 {
		return 0
	}
	visibleRows = max(1, visibleRows)
	selected = clamp(selected, 0, totalRows-1)
	currentOffset = clamp(currentOffset, 0, max(0, totalRows-visibleRows))
	if totalRows <= visibleRows {
		return 0
	}
	if selected < currentOffset {
		return selected
	}
	if selected >= currentOffset+visibleRows {
		return selected - visibleRows + 1
	}
	return currentOffset
}
