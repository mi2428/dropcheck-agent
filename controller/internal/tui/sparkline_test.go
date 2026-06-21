package tui

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"dropcheck/controller/internal/watch"
)

func TestRoundTimelineShowsRoundProgressGauge(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel([]watch.Target{}, events)
	m.Round = 7
	m.Targets = []targetState{
		{Target: targetSnapshot("ok"), Status: "ok"},
		{Target: targetSnapshot("failed"), Status: "failed"},
		{Target: targetSnapshot("running"), Status: "running"},
		{Target: targetSnapshot("pending"), Status: "pending"},
	}

	text := stripANSI(m.roundProgressView(80))
	for _, want := range []string{"round=7", "progress=", "50%", "(2/4)", "run=1", "fail=1"} {
		if !strings.Contains(text, want) {
			t.Fatalf("progress gauge missing %q:\n%s", want, text)
		}
	}
	if strings.ContainsAny(text, "░▁▂▃▄▅▆▇") {
		t.Fatalf("progress gauge should avoid fragile shade/lower-block glyphs:\n%s", text)
	}
	if strings.Contains(text, "█") {
		t.Fatalf("progress gauge should render a line gauge, not a block bar:\n%s", text)
	}
	if !strings.Contains(text, "-") {
		t.Fatalf("progress gauge should render line progress:\n%s", text)
	}
	header := stripANSI(m.roundTimelineHeaderView(80, 1, 1, 1, 1))
	if !strings.Contains(header, "progress=50% (2/4)") {
		t.Fatalf("round timeline header should group done/total in parentheses:\n%s", header)
	}
}

func TestSummarySparklineIsPinnedToBottom(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel([]watch.Target{}, events)
	at := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	m.Now = at.Add(4 * time.Second)
	for i := range 4 {
		m.apply(watch.Event{
			Time:     at.Add(time.Duration(i) * time.Second),
			Kind:     watch.EventStepFinished,
			Round:    1,
			Target:   watch.TargetSnapshot{Name: "SHIZK RADIO", SSID: "SHIZK RADIO"},
			Step:     watch.StepSnapshot{Name: "connect", Type: "connect", Status: "ok"},
			Status:   "ok",
			Duration: 12,
		})
	}

	lines := strings.Split(stripANSI(m.passingChecksView(80, 8)), "\n")
	if len(lines) != 8 {
		t.Fatalf("passingChecks table should fill the requested height, got %d lines:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	if strings.TrimSpace(lines[len(lines)-5]) != "" {
		t.Fatalf("sparkline header should have a spacer row above it:\n%s", strings.Join(lines, "\n"))
	}
	if !strings.Contains(lines[len(lines)-4], "timeline window=last=30m") {
		t.Fatalf("sparkline header should be pinned above the bottom graph:\n%s", strings.Join(lines, "\n"))
	}
	if strings.TrimSpace(lines[len(lines)-3]) != "" {
		t.Fatalf("sparkline header should have a spacer row below it:\n%s", strings.Join(lines, "\n"))
	}
	graph := strings.Join(lines[len(lines)-2:], "\n")
	if strings.ContainsAny(graph, "░▁▂▃▄▅▆▇─") {
		t.Fatalf("sparkline should avoid fragile partial-block glyphs:\n%s", graph)
	}
	if strings.ContainsAny(graph, "|.:=+*#@") {
		t.Fatalf("sparkline should not render vertical bars or punctuation noise:\n%s", graph)
	}
	if !strings.Contains(graph, "█") || !strings.Contains(graph, "30m ago") || !strings.Contains(graph, "now") {
		t.Fatalf("sparkline should render a block time histogram:\n%s", graph)
	}
}

func TestRecentEventHistogramScrollsWithoutReshaping(t *testing.T) {
	base := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	times := []time.Time{
		base,
		base.Add(3 * time.Second),
		base.Add(6 * time.Second),
	}
	before := recentEventHistogram(times, 10, 10*time.Second, base.Add(8*time.Second))
	after := recentEventHistogram(times, 10, 10*time.Second, base.Add(9*time.Second))
	if before.Max != after.Max || before.Count != after.Count {
		t.Fatalf("histogram stats changed while no event entered or expired: before=%#v after=%#v", before, after)
	}
	if got, want := after.Counts[:9], before.Counts[1:]; !reflect.DeepEqual(got, want) {
		t.Fatalf("histogram should scroll left one bucket without reshaping:\nbefore=%v\nafter =%v", before.Counts, after.Counts)
	}
	if after.Counts[9] != 0 {
		t.Fatalf("new rightmost bucket should be empty without new events: %v", after.Counts)
	}
}

func TestRecentEventHistogramDoesNotRebucketWithinBucketWidth(t *testing.T) {
	base := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	times := []time.Time{
		base.Add(1 * time.Second),
		base.Add(3 * time.Second),
		base.Add(8 * time.Second),
	}
	before := recentEventHistogram(times, 5, 10*time.Second, base.Add(8*time.Second+500*time.Millisecond))
	after := recentEventHistogram(times, 5, 10*time.Second, base.Add(9*time.Second+500*time.Millisecond))
	if !reflect.DeepEqual(after.Counts, before.Counts) {
		t.Fatalf("histogram should keep stable bucket boundaries until the next bucket:\nbefore=%v\nafter =%v", before.Counts, after.Counts)
	}
}

func TestRecentEventHistogramStacksOnlyCurrentBucketForNewEvents(t *testing.T) {
	base := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	before := recentEventHistogram([]time.Time{
		base.Add(1 * time.Second),
		base.Add(3 * time.Second),
	}, 5, 10*time.Second, base.Add(8*time.Second+500*time.Millisecond))
	after := recentEventHistogram([]time.Time{
		base.Add(1 * time.Second),
		base.Add(3 * time.Second),
		base.Add(9 * time.Second),
	}, 5, 10*time.Second, base.Add(9*time.Second+500*time.Millisecond))
	if !reflect.DeepEqual(after.Counts[:4], before.Counts[:4]) {
		t.Fatalf("new live event should not stack into past buckets:\nbefore=%v\nafter =%v", before.Counts, after.Counts)
	}
	if got, want := after.Counts[4], before.Counts[4]+1; got != want {
		t.Fatalf("new live event should stack only in the current bucket: got %d want %d counts=%v", got, want, after.Counts)
	}
}

func TestSummarySparklineScalesThirtyMinuteWindowToPanelWidth(t *testing.T) {
	now := time.Date(2026, 5, 16, 9, 35, 0, 0, time.UTC)
	reference := recentEventHistogram(nil, 480, summarySparklineWindow, now)
	times := []time.Time{
		reference.First.Add(reference.BucketWidth / 2),
		reference.First.Add(summarySparklineWindow / 2),
		reference.Last.Add(-reference.BucketWidth / 2),
	}
	histogram := recentEventHistogram(times, 480, summarySparklineWindow, now)
	graph := stripANSI(strings.Join(renderSparkline(histogram.Counts, histogram.Max, 480, 3, okGraphStyle), "\n"))
	lines := strings.Split(graph, "\n")
	if len(lines) != 3 {
		t.Fatalf("sparkline graph should keep requested height, got %d:\n%s", len(lines), graph)
	}
	for i, line := range lines {
		if got := runeLen(line); got != 480 {
			t.Fatalf("sparkline line %d width = %d, want 480", i, got)
		}
	}
	containsInColumns := func(start int, end int) bool {
		for _, line := range lines {
			runes := []rune(line)
			if strings.Contains(string(runes[start:end]), "█") {
				return true
			}
		}
		return false
	}
	if !containsInColumns(0, 120) {
		t.Fatalf("old in-window events should render in the left quarter of the full-width graph:\n%s", graph)
	}
	if !containsInColumns(180, 300) {
		t.Fatalf("middle in-window events should render near the center of the full-width graph:\n%s", graph)
	}
	if !containsInColumns(360, 480) {
		t.Fatalf("recent in-window events should render in the right quarter of the full-width graph:\n%s", graph)
	}
}

func TestPassingSparklineRetainsThirtyMinuteWindowBeyondFourHundredEvents(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel([]watch.Target{}, events)
	now := time.Date(2026, 5, 16, 9, 35, 0, 0, time.UTC)
	m.Now = now
	for i := range 401 {
		offset := time.Duration(i) * 29 * time.Minute / 400
		m.recordPassingCheck(passingCheckState{
			When:     now.Add(-29 * time.Minute).Add(offset),
			Target:   watch.TargetSnapshot{Name: "lab"},
			Step:     watch.StepSnapshot{Name: "connect", Type: "connect", Status: "ok"},
			Duration: 10,
		})
	}

	times := m.passingCheckEventTimes()
	if got, want := len(times), 401; got != want {
		t.Fatalf("passing check history count = %d, want %d", got, want)
	}
	histogram := recentEventHistogram(times, 120, summarySparklineWindow, now)
	if got, want := histogram.Count, 401; got != want {
		t.Fatalf("passing histogram count = %d, want %d", got, want)
	}
	if maxInt(histogram.Counts[:30]) == 0 {
		t.Fatalf("old in-window passing events should still occupy the left side, counts=%v", histogram.Counts)
	}
	if maxInt(histogram.Counts[90:]) == 0 {
		t.Fatalf("recent passing events should occupy the right side, counts=%v", histogram.Counts)
	}
	view := stripANSI(m.summarySparklineView("passing checks", times, 120, 5, okGraphStyle))
	if !strings.Contains(view, "count=401") {
		t.Fatalf("passing sparkline should report all in-window events:\n%s", view)
	}
}

func TestSummarySparklineWindowIsThirtyMinutes(t *testing.T) {
	if summarySparklineWindow != 30*time.Minute {
		t.Fatalf("summarySparklineWindow = %s, want 30m", summarySparklineWindow)
	}
	now := time.Date(2026, 5, 16, 9, 35, 0, 0, time.UTC)
	histogram := recentEventHistogram(nil, 480, summarySparklineWindow, now)
	if got := histogram.Last.Sub(histogram.First); got != 30*time.Minute {
		t.Fatalf("histogram span = %s, want 30m", got)
	}
	if got, want := histogram.BucketWidth, 3750*time.Millisecond; got != want {
		t.Fatalf("480-column bucket width = %s, want %s", got, want)
	}
	wide := recentEventHistogram(nil, 1800, summarySparklineWindow, now)
	if got, want := wide.BucketWidth, time.Second; got != want {
		t.Fatalf("1800-column bucket width = %s, want %s", got, want)
	}
}

func TestSummarySparklineUsesNiceAbsoluteScale(t *testing.T) {
	if got := sparklineEventsPerRow(14, 3); got != 5 {
		t.Fatalf("sparklineEventsPerRow(14, 3) = %d, want 5", got)
	}
	graph := stripANSI(strings.Join(renderSparkline([]int{1, 5, 6, 10, 11, 14}, 14, 6, 3, okGraphStyle), "\n"))
	lines := strings.Split(graph, "\n")
	if len(lines) != 3 {
		t.Fatalf("unexpected graph height:\n%s", graph)
	}
	top := []rune(lines[0])
	middle := []rune(lines[1])
	bottom := []rune(lines[2])
	if strings.Contains(string(top[:4]), "█") {
		t.Fatalf("nice absolute scale should not pin every non-trivial bucket to the top:\n%s", graph)
	}
	if !strings.Contains(string(top[4:]), "█") {
		t.Fatalf("highest buckets should still reach the top row:\n%s", graph)
	}
	if !strings.Contains(string(middle[2:4]), "█") || !strings.Contains(string(bottom[:2]), "█") {
		t.Fatalf("buckets should step through 5-event Rows:\n%s", graph)
	}
}

func TestSummarySparklineHeaderShowsScaleWhenCompressed(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel([]watch.Target{}, events)
	at := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	m.Now = at.Add(10 * time.Second)
	for range 14 {
		m.PassingChecks = append(m.PassingChecks, passingCheckState{
			When:   at.Add(5 * time.Second),
			Target: watch.TargetSnapshot{Name: "lab"},
			Step:   watch.StepSnapshot{Name: "connect"},
		})
	}
	view := stripANSI(m.summarySparklineView("passing checks", m.passingCheckEventTimes(), 120, 6, okGraphStyle))
	if !strings.Contains(view, "peak=14 scale=10/row") {
		t.Fatalf("compressed sparkline header should expose the absolute y scale:\n%s", view)
	}
}
