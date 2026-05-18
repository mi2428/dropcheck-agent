package tui

import (
	"strings"
	"testing"

	"dropcheck/controller/internal/watch"

	"charm.land/lipgloss/v2"
)

func TestBarListLineAlignsColumns(t *testing.T) {
	layout := newBarListLayout(70, [][]string{
		{"Agent", "Target", "Check"},
		{"a", "short", "connect"},
		{"agent-long", "much longer target", "ping cloudflare v6"},
	}, [][]string{
		{"Cnt", "Dur", "Last"},
		{"1", "4.3s", "09:30:01"},
		{"12", "80ms", "09:30:02"},
	})
	short := barListLine([]string{"a", "short", "connect"}, []string{"1", "4.3s", "09:30:01"}, 1, 4, layout)
	long := barListLine([]string{"agent-long", "much longer target", "ping cloudflare v6"}, []string{"12", "80ms", "09:30:02"}, 4, 4, layout)

	if strings.Index(short, "█") != strings.Index(long, "█") {
		t.Fatalf("bar column should align:\nshort=%q\nlong=%q", short, long)
	}
	shortBefore, _, shortOK := strings.Cut(short, "09:30:01")
	longBefore, _, longOK := strings.Cut(long, "09:30:02")
	if !shortOK || !longOK || lipgloss.Width(shortBefore) != lipgloss.Width(longBefore) {
		t.Fatalf("right column should align:\nshort=%q\nlong=%q", short, long)
	}
	if strings.Contains(short, " / ") || strings.Contains(long, " / ") {
		t.Fatalf("bar rows should not use slash separators:\nshort=%q\nlong=%q", short, long)
	}
}

func TestPlainListLayoutFillsPanelWidth(t *testing.T) {
	layout := newPlainListLayout(70, [][]string{
		{"Target", "Check", "Metric"},
		{"radio-u7", "connect", "status"},
	}, [][]string{
		{"Cnt", "Last"},
		{"1", "20:11:27"},
	})
	header := barListHeader([]string{"Target", "Check", "Metric"}, []string{"Cnt", "Last"}, layout)
	if got := lipgloss.Width(header); got != 70 {
		t.Fatalf("plain list header width = %d, want 70: %q", got, header)
	}
	targetIndex := strings.Index(header, "Target")
	checkIndex := strings.Index(header, "Check")
	metricIndex := strings.Index(header, "Metric")
	countIndex := strings.Index(header, "Cnt")
	if targetIndex < 0 || checkIndex < 0 || metricIndex < 0 || countIndex < 0 {
		t.Fatalf("plain list header missing columns: %q", header)
	}
	if checkIndex-targetIndex < 12 || metricIndex-checkIndex < 12 || countIndex-metricIndex < 12 {
		t.Fatalf("plain list columns should spread across the panel width: %q", header)
	}
	if strings.Contains(header, "Target Check") || strings.Contains(header, "Check Metric") || strings.Contains(header, "Cnt Last") {
		t.Fatalf("plain list columns should be separated by at least two spaces: %q", header)
	}
}

func TestEmptyFailedCheckHeaderDistributesColumns(t *testing.T) {
	layout := failedCheckBarListLayout(nil, 96, true)
	header := barListHeader(failedCheckListHeaderColumns(true), failedCheckListRightHeaderColumns(), layout)
	targetIndex := strings.Index(header, "Target")
	checkIndex := strings.Index(header, "Check")
	metricIndex := strings.Index(header, "Metric")
	countIndex := strings.Index(header, "Cnt")
	if targetIndex < 0 || checkIndex < 0 || metricIndex < 0 || countIndex < 0 {
		t.Fatalf("empty failed header missing expected columns: %q", header)
	}
	if checkIndex-targetIndex < 16 || metricIndex-checkIndex < 16 {
		t.Fatalf("empty failed header should distribute left columns evenly: %q", header)
	}
	if countIndex-metricIndex < 16 {
		t.Fatalf("empty failed header should keep metric separated from right metrics: %q", header)
	}
}

func TestCheckStatusHeaderFillsPanelWidth(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", []watch.Target{{Name: "ub1(5G)", SSID: "SHIZK RADIO"}}, events)
	m.Targets[0].Steps = []stepState{{Name: "connect", Status: "ok"}}

	view := m.checkStatusView(64, 3)
	lines := strings.Split(view, "\n")
	if got := lipgloss.Width(lines[0]); got != 64 {
		t.Fatalf("checkStatus header width = %d, want full panel content width 64: %q", got, lines[0])
	}
	if !strings.Contains(lines[0], "\x1b[") {
		t.Fatalf("checkStatus header should be styled: %q", lines[0])
	}
}

func TestInitialRenderWithoutMeasurementsIsStable(t *testing.T) {
	events := make(chan watch.Event)
	agents := []watch.AgentSnapshot{
		{ID: "agent-a", Name: "pixel-a", ADBSerial: "pixel-a"},
		{ID: "agent-b", Name: "pixel-b", ADBSerial: "pixel-b"},
	}
	m := newModel("shownet-watch", []watch.Target{
		{Name: "radio-u6-5ghz", SSID: "SHIZK RADIO", Band: "5ghz"},
		{Name: "radio-any-2ghz", SSID: "SHIZK RADIO", Band: "2.4ghz"},
	}, events, agents)
	m.width = 180
	m.height = 34
	m.apply(watch.Event{Kind: watch.EventWatchStarted, Message: "watch started"})
	m.apply(watch.Event{Kind: watch.EventRoundStarted, Round: 1, Status: "running"})
	m.apply(watch.Event{
		Kind:   watch.EventTargetStarted,
		Round:  1,
		Agent:  agents[0],
		Target: watch.TargetSnapshot{Name: "radio-u6-5ghz", SSID: "SHIZK RADIO", Band: "5ghz"},
		Status: "running",
	})

	frame := stripANSI(m.render())
	lines := strings.Split(frame, "\n")
	if len(lines) != m.height {
		t.Fatalf("initial frame line count = %d, want %d:\n%s", len(lines), m.height, frame)
	}
	for i, line := range lines {
		if got := runeLen(line); got > m.width {
			t.Fatalf("initial frame line %d width = %d, want <= %d: %q", i, got, m.width, line)
		}
	}
	for _, want := range []string{
		"no passing checks",
		"no failed checks",
		"passing checks events last=30m count=0",
		"failed checks events last=30m count=0",
	} {
		if !strings.Contains(frame, want) {
			t.Fatalf("initial frame missing %q:\n%s", want, frame)
		}
	}
	failedHeader := ""
	for line := range strings.SplitSeq(frame, "\n") {
		if strings.Contains(line, "Fail%") && strings.Contains(line, "Strk") {
			failedHeader = line
			break
		}
	}
	if !strings.Contains(failedHeader, "Cnt") || !strings.Contains(failedHeader, "Fail%") || !strings.Contains(failedHeader, "Strk") {
		t.Fatalf("initial failed header missing expected columns: %q\n%s", failedHeader, frame)
	}
}
