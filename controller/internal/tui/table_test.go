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
	for i, want := range []int{len("radio-u7"), len("connect"), len("Metric")} {
		if got := layout.ColumnWidths[i]; got != want {
			t.Fatalf("plain list column width[%d] = %d, want data-fit width %d; layout=%#v", i, got, want, layout)
		}
	}
	targetIndex := strings.Index(header, "Target")
	checkIndex := strings.Index(header, "Check")
	metricIndex := strings.Index(header, "Metric")
	countIndex := strings.Index(header, "Cnt")
	if targetIndex < 0 || checkIndex < 0 || metricIndex < 0 || countIndex < 0 {
		t.Fatalf("plain list header missing columns: %q", header)
	}
	if checkIndex-targetIndex != layout.ColumnWidths[0]+listColumnGap || metricIndex-checkIndex != layout.ColumnWidths[1]+listColumnGap {
		t.Fatalf("plain list left columns should use data-fit widths before the flexible spacer: %q layout=%#v", header, layout)
	}
	if countIndex-metricIndex <= layout.ColumnWidths[2]+listColumnGap {
		t.Fatalf("plain list flexible spacer should sit before right metrics: %q layout=%#v", header, layout)
	}
	if strings.Contains(header, "Target Check") || strings.Contains(header, "Check Metric") || strings.Contains(header, "Cnt Last") {
		t.Fatalf("plain list columns should be separated by at least two spaces: %q", header)
	}
}

func TestFailedCheckLayoutMovesSpareWidthOutOfTargetColumn(t *testing.T) {
	row := failedCheckSummary{
		Count:       1,
		FailPercent: 50,
		FailStreak:  1,
		Target:      watch.TargetSnapshot{Name: "ub2(6G)"},
		Finding: watch.Finding{
			Target: "ub2(6G)",
			Check:  "wait_connected",
			Metric: "status",
		},
	}
	layout := failedCheckBarListLayout([]failedCheckSummary{row}, 76, true)
	if got, want := layout.ColumnWidths[1], lipgloss.Width("ub2(6G)"); got != want {
		t.Fatalf("target column width = %d, want data-fit width %d; layout=%#v", got, want, layout)
	}
	if got, want := layout.ColumnWidths[2], lipgloss.Width("Wait Connected"); got < want {
		t.Fatalf("check column width = %d, want at least %d to avoid truncation; layout=%#v", got, want, layout)
	}
	line := barListLine(failedCheckListColumns(row, true), failedCheckListRightColumns(row), row.Count, row.Count, layout)
	if got := lipgloss.Width(line); got != 76 {
		t.Fatalf("failed check row width = %d, want 76: %q", got, line)
	}
	if strings.Contains(line, "Wait Connec~") || !strings.Contains(line, "Wait Connected") {
		t.Fatalf("failed check row should use target slack for full check name: %q layout=%#v", line, layout)
	}
}

func TestEmptyFailedCheckHeaderDistributesColumns(t *testing.T) {
	layout := failedCheckBarListLayout(nil, 96, true)
	header := barListHeader(failedCheckListHeaderColumns(true), failedCheckListRightHeaderColumns(), layout)
	deviceIndex := strings.Index(header, "Device")
	targetIndex := strings.Index(header, "Target")
	checkIndex := strings.Index(header, "Check")
	metricIndex := strings.Index(header, "Metric")
	countIndex := strings.Index(header, "Cnt")
	if deviceIndex < 0 || targetIndex < 0 || checkIndex < 0 || metricIndex < 0 || countIndex < 0 {
		t.Fatalf("empty failed header missing expected columns: %q", header)
	}
	if targetIndex-deviceIndex < 12 || checkIndex-targetIndex < 12 || metricIndex-checkIndex < 12 {
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

func TestCheckStatusCheckColumnCanGrowByHalf(t *testing.T) {
	checks := []string{"Debug HTTP Wrong Status"}
	targets := []watch.TargetSnapshot{{Name: "debug-a1", ShortName: "DA1"}}

	layout := checkStatusTableLayout(64, checks, targets, nil)
	if got, want := layout.LabelWidth, lipgloss.Width("Debug HTTP Wrong Status"); got != want {
		t.Fatalf("check column should fit long check name when space allows: width=%d want=%d layout=%#v", got, want, layout)
	}

	layout = checkStatusTableLayout(200, []string{"this check label is intentionally much too long"}, targets, nil)
	if got, want := layout.LabelWidth, 27; got != want {
		t.Fatalf("check column max width = %d, want %d", got, want)
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
		"timeline window=last=30m",
		"timeline window=last=30m",
	} {
		if !strings.Contains(frame, want) {
			t.Fatalf("initial frame missing %q:\n%s", want, frame)
		}
	}
	failedHeader := ""
	for line := range strings.SplitSeq(frame, "\n") {
		if strings.Contains(line, "Fail%") {
			failedHeader = line
			break
		}
	}
	if !strings.Contains(failedHeader, "Dev") || !strings.Contains(failedHeader, "Cnt") || !strings.Contains(failedHeader, "Fail%") {
		t.Fatalf("initial failed header missing expected columns: %q\n%s", failedHeader, frame)
	}
}
