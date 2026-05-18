package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"dropcheck/controller/internal/watch"

	"charm.land/lipgloss/v2"
)

func TestRenderShowsRoundTimelineAndCheckStatus(t *testing.T) {
	events := make(chan watch.Event)
	agents := []watch.AgentSnapshot{
		{ID: "agent-a", Name: "pixel-a", ADBSerial: "pixel-a"},
		{ID: "agent-b", Name: "pixel-b", ADBSerial: "pixel-b"},
	}
	m := newModel("shownet-watch", []watch.Target{
		{Name: "u7-5ghz", SSID: "SHIZK RADIO", Band: "5ghz"},
		{Name: "u6-5ghz", SSID: "SHIZK RADIO", Band: "5ghz"},
	}, events, agents)
	m.width = 180
	m.height = 42
	at := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	m.Now = at.Add(2 * time.Second)
	m.apply(watch.Event{
		Time:     at,
		Kind:     watch.EventStepFinished,
		Agent:    agents[0],
		Round:    1,
		Target:   watch.TargetSnapshot{Name: "u7-5ghz", SSID: "SHIZK RADIO"},
		Step:     watch.StepSnapshot{Name: "connect", Type: "connect", Status: "ok"},
		Status:   "ok",
		Duration: 42,
	})
	m.apply(watch.Event{
		Time:   at.Add(time.Second),
		Kind:   watch.EventFinding,
		Agent:  agents[1],
		Round:  1,
		Target: watch.TargetSnapshot{Name: "u6-5ghz", SSID: "SHIZK RADIO"},
		Step:   watch.StepSnapshot{Name: "ping cloudflare", Type: "ping", Status: "failed"},
		Status: "failed",
		Finding: &watch.Finding{
			Target:   "u6-5ghz",
			Check:    "ping cloudflare",
			Metric:   "received",
			Observed: "0",
			Expected: "== 5",
			Message:  "constraint failed",
		},
	})

	frame := stripANSI(m.render())
	for _, want := range []string{
		"Round Timeline",
		"Check Status",
		"progress=",
		"span=1..1",
		"ok=1",
		"fail=1",
		"pixel-a",
		"pixel-b",
		"u7-5ghz",
		"u6-5ghz",
		"Connect",
		"ping cloudflare",
		"FAIL(50%)",
		"█",
	} {
		if !strings.Contains(frame, want) {
			t.Fatalf("dashboard graph frame missing %q:\n%s", want, frame)
		}
	}
	checkStatus := stripANSI(m.checkStatusView(72, 4))
	if strings.Contains(checkStatus, "▁") || strings.Contains(checkStatus, "█") {
		t.Fatalf("checkStatus should render PASS/FAIL cells instead of history bars:\n%s", checkStatus)
	}
	if strings.Contains(checkStatus, "Agent") || !strings.Contains(checkStatus, "Check") {
		t.Fatalf("checkStatus should use checks as Rows:\n%s", checkStatus)
	}
	timeline := stripANSI(m.roundTimelineView(120, 5))
	timelineLines := strings.Split(timeline, "\n")
	if len(timelineLines) == 0 ||
		!strings.Contains(timelineLines[0], "round=") ||
		!strings.Contains(timelineLines[0], "span=") ||
		!strings.Contains(timelineLines[0], "progress=") {
		t.Fatalf("round timeline header should keep round/span and right progress on one Line:\n%s", timeline)
	}
	if strings.Contains(strings.Join(timelineLines[1:], "\n"), "span=") {
		t.Fatalf("round timeline should not render span on a separate second header Line:\n%s", timeline)
	}
	for _, want := range []string{"u7-5ghz", "u6-5ghz"} {
		if !strings.Contains(timeline, want) {
			t.Fatalf("target timeline missing %q:\n%s", want, timeline)
		}
	}
	if strings.Contains(timeline, "f=") {
		t.Fatalf("target timeline should not render per-tile failure counters:\n%s", timeline)
	}
	if strings.Contains(timeline, "pixel-a") || strings.Contains(timeline, "pixel-b") {
		t.Fatalf("round timeline should group by target, not Agent:\n%s", timeline)
	}
}

func TestRoundTimelineRendersTargetFailureDensity(t *testing.T) {
	events := make(chan watch.Event)
	targets := []watch.Target{
		{Name: "u7-5ghz", SSID: "SHIZK RADIO"},
		{Name: "u6-5ghz", SSID: "SHIZK RADIO"},
		{Name: "any-5ghz", SSID: "SHIZK RADIO"},
		{Name: "u7-2.4ghz", SSID: "SHIZK RADIO"},
	}
	m := newModel("shownet-watch", targets, events)
	now := time.Date(2026, 5, 16, 9, 30, 30, 0, time.UTC)
	m.Now = now
	m.Round = 4
	target := watch.TargetSnapshot{Name: "u7-5ghz", SSID: "SHIZK RADIO"}
	m.FailedChecks = []failedCheckState{
		{Round: 3, When: now.Add(-3 * time.Second), Target: target, Finding: watch.Finding{Target: "u7-5ghz", Check: "dns a"}},
		{Round: 3, When: now.Add(-3 * time.Second), Target: target, Finding: watch.Finding{Target: "u7-5ghz", Check: "ping v4"}},
		{Round: 2, When: now.Add(-20 * time.Second), Target: target, Finding: watch.Finding{Target: "u7-5ghz", Check: "connect"}},
	}

	buckets, total, peak := m.targetRoundHistory(target, 20)
	if total != 3 || peak != 2 {
		t.Fatalf("target round history stats total=%d peak=%d buckets=%#v", total, peak, buckets)
	}
	if got := stripANSI(renderTargetRoundHistory([]targetRoundBucket{{Seen: true, Failed: 1, ConnectFailed: true}}, 8, 1)); got != "X" {
		t.Fatalf("connection failure should render as a red X, got %q", got)
	}
	if got := stripANSI(renderTargetRoundHistory([]targetRoundBucket{{Seen: true, Failed: 1}}, 8, 1)); got != "▂" {
		t.Fatalf("one failed check should render at the low absolute height, got %q", got)
	}
	if got := stripANSI(renderTargetRoundHistory([]targetRoundBucket{{Seen: true, Failed: 8}}, 8, 1)); got != "█" {
		t.Fatalf("all checks failed should render at the maximum absolute height, got %q", got)
	}
	timeline := stripANSI(m.roundTimelineView(200, 6))
	firstTargetRow := lineIndex(timeline, "u7-5ghz")
	if firstTargetRow < 0 {
		t.Fatalf("timeline missing target row:\n%s", timeline)
	}
	row := strings.Split(timeline, "\n")[firstTargetRow]
	for _, want := range []string{"u7-5ghz", "u6-5ghz", "any-5ghz", "u7-2.4ghz"} {
		if !strings.Contains(row, want) {
			t.Fatalf("first timeline row should contain four target tiles and is missing %q:\n%s", want, timeline)
		}
	}
	later := m
	later.Now = now.Add(time.Hour)
	if stripANSI(later.roundTimelineView(200, 6)) != timeline {
		t.Fatalf("round history graph should not shift just because wall-clock time advanced:\nbefore:\n%s\nafter:\n%s", timeline, stripANSI(later.roundTimelineView(200, 6)))
	}
	if strings.Contains(timeline, "█") {
		t.Fatalf("partial failures should not render as max-height when check count is higher than bucket failures:\n%s", timeline)
	}
}

func TestRoundTimelineConnectFailureXRequiresAllAgents(t *testing.T) {
	events := make(chan watch.Event)
	agents := []watch.AgentSnapshot{
		{ID: "agent-a", Name: "pixel-a"},
		{ID: "agent-b", Name: "pixel-b"},
	}
	target := watch.TargetSnapshot{Name: "u6-2.4ghz", SSID: "SHIZK RADIO"}
	m := newModel("shownet-watch", []watch.Target{{Name: "u6-2.4ghz", SSID: "SHIZK RADIO"}}, events, agents)
	m.Round = 1
	m.PassingChecks = []passingCheckState{{
		Round:  1,
		Agent:  agents[0],
		Target: target,
		Step:   watch.StepSnapshot{Name: "connect", Type: "connect", Status: "ok"},
	}}
	m.FailedChecks = []failedCheckState{{
		Round:   1,
		Agent:   agents[1],
		Target:  target,
		Finding: watch.Finding{Target: "u6-2.4ghz", Check: "connect"},
	}}

	buckets, _, _ := m.targetRoundHistory(target, 1)
	if buckets[0].ConnectFailed {
		t.Fatalf("single-agent connect failure should not mark the target bucket as total connection failure: %#v", buckets[0])
	}
	if got := stripANSI(renderTargetRoundHistory(buckets, 8, 1)); got == "X" {
		t.Fatalf("partial multi-agent connect failure should not render as X")
	}

	m.PassingChecks = nil
	m.FailedChecks = []failedCheckState{
		{Round: 1, Agent: agents[0], Target: target, Finding: watch.Finding{Target: "u6-2.4ghz", Check: "connect"}},
		{Round: 1, Agent: agents[1], Target: target, Finding: watch.Finding{Target: "u6-2.4ghz", Check: "connect"}},
	}
	buckets, _, _ = m.targetRoundHistory(target, 1)
	if !buckets[0].ConnectFailed {
		t.Fatalf("all-agent connect failure should mark the target bucket as total connection failure: %#v", buckets[0])
	}
}

func TestRoundTimelinePacksColumnsWhileKeepingTenRounds(t *testing.T) {
	events := make(chan watch.Event)
	targets := []watch.Target{
		{Name: "t0", SSID: "SHIZK RADIO"},
		{Name: "t1", SSID: "SHIZK RADIO"},
		{Name: "t2", SSID: "SHIZK RADIO"},
		{Name: "t3", SSID: "SHIZK RADIO"},
		{Name: "t4", SSID: "SHIZK RADIO"},
		{Name: "t5", SSID: "SHIZK RADIO"},
	}
	m := newModel("shownet-watch", targets, events)
	m.Round = 20

	wide := stripANSI(m.roundTimelineView(140, 4))
	wideLines := strings.Split(wide, "\n")
	if len(wideLines) < 2 {
		t.Fatalf("wide timeline missing target row:\n%s", wide)
	}
	for _, want := range []string{"t0", "t1", "t2", "t3", "t4", "t5"} {
		if !strings.Contains(wideLines[1], want) {
			t.Fatalf("wide timeline should pack all targets into one row, missing %q:\n%s", want, wide)
		}
	}
	start, end := m.roundTimelineRoundSpan(140)
	if got := int(end - start + 1); got < 10 {
		t.Fatalf("wide timeline visible rounds = %d, want at least 10", got)
	}

	narrow := stripANSI(m.roundTimelineView(100, 5))
	narrowLines := strings.Split(narrow, "\n")
	if len(narrowLines) < 3 {
		t.Fatalf("narrow timeline should wrap Targets:\n%s", narrow)
	}
	for _, want := range []string{"t0", "t2", "t4"} {
		if !strings.Contains(narrowLines[1], want) {
			t.Fatalf("narrow first row should show the top of each column, missing %q:\n%s", want, narrow)
		}
	}
	for _, want := range []string{"t1", "t3", "t5"} {
		if strings.Contains(narrowLines[1], want) || !strings.Contains(narrowLines[2], want) {
			t.Fatalf("narrow timeline should fill columns from top to bottom, target %q placed incorrectly:\n%s", want, narrow)
		}
	}
	start, end = m.roundTimelineRoundSpan(100)
	if got := int(end - start + 1); got < 10 {
		t.Fatalf("narrow timeline visible rounds = %d, want at least 10", got)
	}
}

func TestRoundTimelineDoesNotPadSingleRoundGraph(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", []watch.Target{
		{Name: "hp1(5G)", SSID: "Lab"},
		{Name: "hp6(5G)", SSID: "Lab"},
	}, events)
	at := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	for _, target := range []watch.TargetSnapshot{{Name: "hp1(5G)", SSID: "Lab"}, {Name: "hp6(5G)", SSID: "Lab"}} {
		m.apply(watch.Event{
			Time:   at,
			Kind:   watch.EventStepFinished,
			Round:  1,
			Target: target,
			Step:   watch.StepSnapshot{Name: "Connect", Type: "connect", Status: "ok"},
			Status: "ok",
		})
	}

	view := stripANSI(m.roundTimelineView(80, 3))
	lines := strings.Split(view, "\n")
	if len(lines) < 2 {
		t.Fatalf("round timeline missing target row:\n%s", view)
	}
	first := strings.Index(lines[1], "hp1(5G) ▁")
	second := strings.Index(lines[1], "hp6(5G) ▁")
	if first < 0 || second < 0 {
		t.Fatalf("single-round timeline should render graph next to each label:\n%s", view)
	}
	if second-first <= len("hp1(5G) ▁ ")+1 {
		t.Fatalf("single-round timeline should keep target columns padded:\n%s", view)
	}
}

func TestRoundTimelineAlignsGraphStartWithinColumn(t *testing.T) {
	events := make(chan watch.Event)
	targets := []watch.Target{
		{Name: "hp1(5G)", SSID: "Lab"},
		{Name: "hp10(5G)", SSID: "Lab"},
		{Name: "hp2(5G)", SSID: "Lab"},
	}
	m := newModel("shownet-watch", targets, events)
	at := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	for _, target := range []watch.TargetSnapshot{
		{Name: "hp1(5G)", SSID: "Lab"},
		{Name: "hp10(5G)", SSID: "Lab"},
		{Name: "hp2(5G)", SSID: "Lab"},
	} {
		m.apply(watch.Event{
			Time:   at,
			Kind:   watch.EventStepFinished,
			Round:  1,
			Target: target,
			Step:   watch.StepSnapshot{Name: "Connect", Type: "connect", Status: "ok"},
			Status: "ok",
		})
	}

	view := stripANSI(m.roundTimelineView(35, 5))
	lines := strings.Split(view, "\n")
	if len(lines) < 4 {
		t.Fatalf("round timeline should wrap targets into one column:\n%s", view)
	}
	offsets := make([]int, 0, len(targets))
	for i, target := range targets {
		line := lines[i+1]
		labelStart := strings.Index(line, target.Name)
		graphStart := strings.Index(line, "▁")
		if labelStart < 0 || graphStart < 0 {
			t.Fatalf("round timeline row missing label or graph for %q:\n%s", target.Name, view)
		}
		offsets = append(offsets, graphStart-labelStart)
	}
	for _, offset := range offsets[1:] {
		if offset != offsets[0] {
			t.Fatalf("graph starts should align within the wrapped target column, got offsets %v:\n%s", offsets, view)
		}
	}
	if offsets[0] >= roundTimelineTargetLabelRunes+1 {
		t.Fatalf("graph alignment should use visible label width instead of fixed label padding, got offset %d:\n%s", offsets[0], view)
	}
}

func TestRoundTimelineKeepsTenTargetRunesAndTenRounds(t *testing.T) {
	labelWidth, plotWidth := roundTimelineTileLayout(roundTimelineMinTileWidth())
	if labelWidth != roundTimelineTargetLabelRunes {
		t.Fatalf("round timeline label width = %d, want %d", labelWidth, roundTimelineTargetLabelRunes)
	}
	if plotWidth != roundTimelineMinVisibleRounds {
		t.Fatalf("round timeline plot width = %d, want %d visible rounds", plotWidth, roundTimelineMinVisibleRounds)
	}
	if got := roundTimelineTargetLabel(watch.TargetSnapshot{Name: "cs20(5G)-extra"}, labelWidth); got != "cs20(5G)-~" {
		t.Fatalf("round timeline target label = %q, want width-10 truncation", got)
	}
	if got := roundTimelineColumnCount(35, 2); got != 1 {
		t.Fatalf("round timeline columns = %d, want wrap before reducing labels below ten runes or rounds below ten", got)
	}
}

func TestRoundTimelinePanelHeightExpandsForWrappedTargetRows(t *testing.T) {
	events := make(chan watch.Event)
	targets := make([]watch.Target, 0, 24)
	for i := range 24 {
		targets = append(targets, watch.Target{Name: fmt.Sprintf("target-%02d", i), SSID: "SHIZK RADIO"})
	}
	m := newModel("shownet-watch", targets, events)
	grid := roundTimelineGrid(100, len(targets))
	height := m.roundTimelinePanelHeight(100)
	if grid.Rows < 6 {
		t.Fatalf("round timeline grid rows = %d, want wrapped rows", grid.Rows)
	}
	if want := grid.Rows + 3; height != want {
		t.Fatalf("round timeline panel height = %d, want %d for header plus wrapped rows", height, want)
	}
}

func TestCheckStatusPanelHeightFitsAllChecks(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", []watch.Target{{Name: "u7-5ghz", SSID: "SHIZK RADIO"}}, events)
	checks := []string{
		"connect",
		"wait_connected",
		"wifi link",
		"ip provisioning",
		"ping v4",
		"ping v6",
		"dns a",
		"dns aaaa",
		"http v4",
		"download v6",
		"public ipv4",
		"public ipv6",
		"traceroute v4",
		"path mtu v6",
	}
	for _, check := range checks {
		m.Targets[0].Steps = append(m.Targets[0].Steps, stepState{Name: check, Status: "ok"})
	}

	wantHeight := len(checks) + 3 // border + header + check rows + border
	if got := m.checkStatusPanelHeight(); got != wantHeight {
		t.Fatalf("checkStatus height = %d, want %d for all checks", got, wantHeight)
	}
	_, checkStatusHeight, _, _ := m.dashboardPanelHeights(60)
	if checkStatusHeight != wantHeight {
		t.Fatalf("dashboard checkStatus height = %d, want %d for all checks", checkStatusHeight, wantHeight)
	}
	checkStatus := stripANSI(m.checkStatusView(160, wantHeight-2))
	for _, check := range checks {
		label := displayCheckName(check)
		if !strings.Contains(checkStatus, label) {
			t.Fatalf("checkStatus missing check %q:\n%s", label, checkStatus)
		}
	}
}

func TestCheckStatusSpansFullWidthAboveRoundTimelineAndRunQueue(t *testing.T) {
	events := make(chan watch.Event)
	agents := []watch.AgentSnapshot{
		{ID: "agent-a", Name: "pixel-a"},
		{ID: "agent-b", Name: "pixel-b"},
	}
	m := newModel("shownet-watch", []watch.Target{{Name: "u7-5ghz", SSID: "SHIZK RADIO"}}, events, agents)
	m.width = 140
	m.height = 34
	m.apply(watch.Event{
		Kind:     watch.EventStepStarted,
		Agent:    agents[0],
		Round:    1,
		Target:   watch.TargetSnapshot{Name: "u7-5ghz", SSID: "SHIZK RADIO"},
		Step:     watch.StepSnapshot{Name: "connect", Type: "connect", Status: "running"},
		Status:   "running",
		Duration: 42,
	})

	frame := stripANSI(m.render())
	checkStatusRow := lineIndex(frame, "┌Check Status")
	roundTimelineRow := lineIndex(frame, "┌Round Timeline")
	runQueueRow := lineIndex(frame, "┌Run Queue")
	if checkStatusRow < 0 || roundTimelineRow < 0 || runQueueRow < 0 {
		t.Fatalf("missing expected panels:\n%s", frame)
	}
	if checkStatusRow >= roundTimelineRow {
		t.Fatalf("Check Status should render above Round Timeline: checkStatus=%d roundTimeline=%d\n%s", checkStatusRow, roundTimelineRow, frame)
	}
	roundTimelineHeight, _, _, _ := m.dashboardPanelHeights(max(4, m.height-2), panelContentWidth(m.width))
	if runQueueRow != roundTimelineRow+roundTimelineHeight {
		t.Fatalf("Run Queue should start below full-width Round Timeline: runQueue=%d roundTimeline=%d roundTimelineHeight=%d\n%s", runQueueRow, roundTimelineRow, roundTimelineHeight, frame)
	}
	checkStatusLine := strings.Split(frame, "\n")[checkStatusRow]
	if lipgloss.Width(checkStatusLine) != m.width {
		t.Fatalf("Check Status panel should span full app width, got %d want %d: %q", lipgloss.Width(checkStatusLine), m.width, checkStatusLine)
	}
	roundTimelineLine := strings.Split(frame, "\n")[roundTimelineRow]
	if lipgloss.Width(roundTimelineLine) != m.width {
		t.Fatalf("Round Timeline panel should span full app width, got %d want %d: %q", lipgloss.Width(roundTimelineLine), m.width, roundTimelineLine)
	}
}

func TestCheckStatusFooterShowsLiveConnectStateByAgent(t *testing.T) {
	events := make(chan watch.Event)
	agents := []watch.AgentSnapshot{
		{ID: "agent-a", Name: "35251JEHN00258", ADBSerial: "35251JEHN00258", DeviceModel: "Pixel 7a"},
		{ID: "agent-b", Name: "45240DLAQ007HG", ADBSerial: "45240DLAQ007HG", DeviceModel: "Pixel 9"},
		{ID: "agent-c", Name: "extra-agent"},
	}
	m := newModelWithChecks("shownet-watch", []watch.Target{{Name: "ub1(5G)", SSID: "SHIZK RADIO"}}, []watch.Check{{Name: "connect", Type: "connect"}}, events, agents)
	m.apply(watch.Event{
		Time:    time.Date(2026, 5, 18, 11, 0, 0, 0, time.UTC),
		Kind:    watch.EventLog,
		Agent:   agents[0],
		Message: "wifi connect state: supplicant=FOUR_WAY_HANDSHAKE bssid=70:a7:41:a0:9a:6f",
	})
	m.apply(watch.Event{
		Time:    time.Date(2026, 5, 18, 11, 0, 0, 0, time.UTC),
		Kind:    watch.EventLog,
		Agent:   agents[1],
		Message: "wifi connect state: supplicant=COMPLETED bssid=22:0b:8b:b6:2c:e1",
	})
	m.apply(watch.Event{
		Time:    time.Date(2026, 5, 18, 11, 0, 0, 0, time.UTC),
		Kind:    watch.EventLog,
		Agent:   agents[2],
		Message: "wifi connect state: supplicant=ASSOCIATED bssid=22:0b:8b:b6:2c:e2",
	})

	view := stripANSI(m.checkStatusView(140, 5))
	lines := strings.Split(view, "\n")
	if got := lines[len(lines)-1]; !strings.Contains(got, "Pixel 7a (") ||
		!strings.Contains(got, "35251") ||
		!strings.Contains(got, "FOUR_WAY_HANDSHAKE") ||
		!strings.Contains(got, "Pixel 9 (") ||
		!strings.Contains(got, "45240") ||
		!strings.Contains(got, "COMPLETED") ||
		!strings.Contains(got, "extra-agent=") ||
		!strings.Contains(got, "ASSOCIATED") {
		t.Fatalf("connect footer missing live states:\n%s", view)
	}
	if strings.Count(lines[len(lines)-1], " ") < 2 {
		t.Fatalf("connect footer should use single-space separated agent items:\n%s", view)
	}
}

func TestAgentPanelTitlesUseDeviceModelAndSerial(t *testing.T) {
	events := make(chan watch.Event)
	agents := []watch.AgentSnapshot{
		{ID: "agent-a", Name: "35251JEHN00258", ADBSerial: "35251JEHN00258", DeviceModel: "Pixel 7a"},
		{ID: "agent-b", Name: "45240DLAQ007HG", ADBSerial: "45240DLAQ007HG", DeviceModel: "Pixel 9"},
	}
	m := newModelWithChecks("shownet-watch", []watch.Target{{Name: "ub1(5G)", SSID: "SHIZK RADIO"}}, []watch.Check{{Name: "connect", Type: "connect"}}, events, agents)
	m.width = 220
	m.height = 60

	text := stripANSI(m.failureHotspotPanelsView(100, 12) + "\n" + m.runQueuePanelsView(100, 12))
	for _, want := range []string{
		"Failure Hotspots Pixel 7a (35251JEHN00258)",
		"Failure Hotspots Pixel 9 (45240DLAQ007HG)",
		"Run Queue Pixel 7a (35251JEHN00258)",
		"Run Queue Pixel 9 (45240DLAQ007HG)",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("agent panel title missing %q:\n%s", want, text)
		}
	}
}

func TestHotspotsAndRunQueueStartBelowFullWidthRoundTimeline(t *testing.T) {
	events := make(chan watch.Event)
	agents := []watch.AgentSnapshot{
		{ID: "agent-a", Name: "pixel-a"},
		{ID: "agent-b", Name: "pixel-b"},
	}
	m := newModel("shownet-watch", []watch.Target{{Name: "u7-5ghz", SSID: "SHIZK RADIO"}}, events, agents)
	m.width = 220
	m.height = 42
	at := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	for i, agent := range agents {
		m.apply(watch.Event{
			Time:   at.Add(time.Duration(i) * time.Second),
			Kind:   watch.EventFinding,
			Round:  uint64(i + 1),
			Agent:  agent,
			Target: watch.TargetSnapshot{Name: "u7-5ghz", SSID: "SHIZK RADIO"},
			Step:   watch.StepSnapshot{Name: "connect", Type: "connect", Status: "failed"},
			Status: "failed",
			Finding: &watch.Finding{
				Target:   "u7-5ghz",
				Check:    "connect",
				Metric:   "status",
				Observed: "failed",
				Expected: "== ok",
				Message:  agent.Name + " timeout",
			},
		})
	}

	frame := stripANSI(m.render())
	roundTimelineRow := lineIndex(frame, "┌Round Timeline")
	passingRow := lineIndex(frame, "┌Passing Checks")
	failedRow := lineIndex(frame, "┌Failed Checks")
	hotspotsRow := lineIndex(frame, "┌Failure Hotspots")
	runQueueRow := lineIndex(frame, "┌Run Queue")
	eventLogRow := lineIndex(frame, "┌Event Log")
	if roundTimelineRow < 0 || passingRow < 0 || failedRow < 0 || hotspotsRow < 0 || runQueueRow < 0 || eventLogRow < 0 {
		t.Fatalf("missing expected panels:\n%s", frame)
	}
	if passingRow != hotspotsRow || failedRow != hotspotsRow || runQueueRow != hotspotsRow {
		t.Fatalf("Passing/Failed/Hotspots/RunQueue should start on the same lower row: passing=%d failed=%d hotspots=%d runQueue=%d\n%s", passingRow, failedRow, hotspotsRow, runQueueRow, frame)
	}
	if eventLogRow <= passingRow || eventLogRow <= hotspotsRow {
		t.Fatalf("Event Log should sit below Passing/Failed while Hotspots continues vertically: eventLog=%d passing=%d hotspots=%d\n%s", eventLogRow, passingRow, hotspotsRow, frame)
	}
	roundTimelineLine := strings.Split(frame, "\n")[roundTimelineRow]
	if lipgloss.Width(roundTimelineLine) != m.width {
		t.Fatalf("Round Timeline panel should span full app width, got %d want %d: %q", lipgloss.Width(roundTimelineLine), m.width, roundTimelineLine)
	}
	eventLogWidth := panelTopWidth(frame, "Event Log")
	passingWidth := panelTopWidth(frame, "Passing Checks")
	failedWidth := panelTopWidth(frame, "Failed Checks")
	if eventLogWidth != passingWidth+1+failedWidth {
		t.Fatalf("Event Log width should match Passing+Failed columns, got eventLog=%d passing=%d failed=%d\n%s", eventLogWidth, passingWidth, failedWidth, frame)
	}
}
