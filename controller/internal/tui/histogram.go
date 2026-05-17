package tui

import (
	"strings"
	"time"

	"dropcheck/controller/internal/watch"
	"dropcheck/controller/internal/watchstate"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func recentEventHistogram(times []time.Time, width int, window time.Duration, now time.Time) occurrenceHistogram {
	return watchstate.RecentEventHistogram(times, width, window, now)
}

func renderSparkline(counts []int, maxCount int, width int, height int, style lipgloss.Style) []string {
	if width <= 0 || height <= 0 {
		return nil
	}
	counts = resampleSparklineCounts(counts, width)
	maxCount = max(maxCount, maxInt(counts))
	eventsPerRow := sparklineEventsPerRow(maxCount, height)
	if len(counts) == 0 || maxCount <= 0 {
		lines := make([]string, height)
		for i := range lines {
			lines[i] = dimStyle.Render(strings.Repeat(" ", width))
		}
		return lines
	}
	columnHeights := make([]int, width)
	for i := 0; i < width; i++ {
		count := 0
		if i < len(counts) {
			count = counts[i]
		}
		if count > 0 {
			columnHeights[i] = clamp((count+eventsPerRow-1)/eventsPerRow, 1, height)
		}
	}
	lines := make([]string, 0, height)
	for row := height; row >= 1; row-- {
		var b strings.Builder
		for _, columnHeight := range columnHeights {
			if columnHeight >= row {
				b.WriteString(style.Render("█"))
			} else {
				b.WriteString(dimStyle.Render(" "))
			}
		}
		lines = append(lines, b.String())
	}
	return lines
}

func sparklineEventsPerRow(maxCount int, height int) int {
	return watchstate.SparklineEventsPerRow(maxCount, height)
}
func niceSparklineUnit(needed int) int { return watchstate.NiceSparklineUnit(needed) }
func resampleSparklineCounts(counts []int, width int) []int {
	return watchstate.ResampleSparklineCounts(counts, width)
}
func maxInt(values []int) int { return watchstate.MaxInt(values) }

func summarySparklineAxis(width int, window time.Duration) string {
	if width <= 0 {
		return ""
	}
	left := formatBucketDuration(window) + " ago"
	right := "now"
	if width < len(left)+len(right)+1 {
		return dimStyle.Render(strings.Repeat("-", width))
	}
	mid := strings.Repeat("-", max(1, width-len(left)-len(right)))
	return dimStyle.Render(left + mid + right)
}

func (m model) passingCheckOccurrences(agent watch.AgentSnapshot, target watch.TargetSnapshot, step watch.StepSnapshot) []time.Time {
	return m.State.PassingCheckOccurrences(agent, target, step)
}

func (m model) failedCheckOccurrences(agent watch.AgentSnapshot, target watch.TargetSnapshot, finding watch.Finding) []time.Time {
	return m.State.FailedCheckOccurrences(agent, target, finding)
}

func (m model) failureHotspotOccurrences(item failureHotspotSummary) []time.Time {
	return m.State.FailureHotspotOccurrences(item)
}

func formatBucketDuration(duration time.Duration) string {
	return watchstate.FormatBucketDuration(duration)
}
func occurrenceGraphHeight(contentHeight int) int {
	return watchstate.OccurrenceGraphHeight(contentHeight)
}

func padVisible(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) > width {
		value = ansi.Truncate(value, width, "~")
	}
	return value + strings.Repeat(" ", max(0, width-lipgloss.Width(value)))
}

func padLeftVisible(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) > width {
		value = ansi.Truncate(value, width, "~")
	}
	return strings.Repeat(" ", max(0, width-lipgloss.Width(value))) + value
}
