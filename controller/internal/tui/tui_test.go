package tui

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"dropcheck/controller/internal/watch"
)

func TestRenderUsesDropcheckLayout(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", []watch.Target{
		{Name: "shownet-6g-ap1", SSID: "ShowNet", BSSID: "aa:bb:cc:dd:ee:ff", Band: "6ghz"},
		{Name: "shownet-5g-any", SSID: "ShowNet", Band: "5ghz"},
	}, events)
	m.width = 180
	m.height = 30
	m.apply(watch.Event{Kind: watch.EventWatchStarted, Message: "watch started"})
	m.apply(watch.Event{Kind: watch.EventRoundStarted, Round: 1})
	m.apply(watch.Event{
		Kind:   watch.EventTargetStarted,
		Round:  1,
		Target: watch.TargetSnapshot{Name: "shownet-6g-ap1", SSID: "ShowNet", BSSID: "aa:bb:cc:dd:ee:ff", Band: "6ghz"},
	})
	m.apply(watch.Event{
		Kind:   watch.EventStepStarted,
		Round:  1,
		Target: watch.TargetSnapshot{Name: "shownet-6g-ap1", SSID: "ShowNet", BSSID: "aa:bb:cc:dd:ee:ff", Band: "6ghz"},
		Step:   watch.StepSnapshot{Name: "connect", Type: "connect", Status: "running"},
	})

	frame := stripANSI(m.render())
	for _, want := range []string{
		"Keys:",
		"Now=",
		"status=running plan=shownet-watch",
		"Passing Checks",
		"Failed Checks",
		"Failure Hotspots",
		"Event Log",
		"Run Queue",
		"shownet-6g-ap1",
		"connect",
	} {
		if !strings.Contains(frame, want) {
			t.Fatalf("rendered frame missing %q:\n%s", want, frame)
		}
	}
	firstRow := strings.Split(frame, "\n")[0]
	if strings.Contains(firstRow, "Passing Checks") || strings.Contains(firstRow, "Failed Checks") || strings.Contains(firstRow, "Run Queue") || strings.Contains(firstRow, "Event Log") {
		t.Fatalf("top keyboard bar should be separate from panels: %q", firstRow)
	}
	if !strings.Contains(frame, "┌Passing Checks") || !strings.Contains(frame, "┌Failed Checks") || !strings.Contains(frame, "┌Failure Hotspots") || !strings.Contains(frame, "┌Event Log") || !strings.Contains(frame, "┌Run Queue") {
		t.Fatalf("panel titles should be embedded in top borders:\n%s", frame)
	}
	if strings.Contains(frame, "│ Passing Checks") || strings.Contains(frame, "│ Failed Checks") || strings.Contains(frame, "│ Failure Hotspots") || strings.Contains(frame, "│ Event Log") || strings.Contains(frame, "│ Run Queue") {
		t.Fatalf("panel titles should not consume an interior content row:\n%s", frame)
	}
	if !strings.Contains(frame, "└── RUN  connect") {
		t.Fatalf("run queue panel should render the current target as an expanded tree:\n%s", frame)
	}
	if strings.Contains(frame, "> RUN") || strings.Contains(frame, "> OK") || strings.Contains(frame, "> FAIL") || strings.Contains(frame, "> WAIT") {
		t.Fatalf("run queue panel should rely on row highlighting instead of a text cursor:\n%s", frame)
	}
	if strings.Contains(frame, "▶") || strings.Contains(frame, "✓") || strings.Contains(frame, "✕") || strings.Contains(frame, "●") {
		t.Fatalf("run queue/status markers should use fixed ASCII tokens:\n%s", frame)
	}
	lines := strings.Split(frame, "\n")
	lastLine := lines[len(lines)-1]
	if !strings.Contains(lastLine, "status=running plan=shownet-watch") {
		t.Fatalf("status line should be rendered at bottom, got: %q\n%s", lastLine, frame)
	}
	if strings.Contains(frame, "Legend:") {
		t.Fatalf("legend should not be rendered:\n%s", frame)
	}
	passingChecksRow := lineIndex(frame, "┌Passing Checks")
	failedChecksRow := lineIndex(frame, "┌Failed Checks")
	hotspotsRow := lineIndex(frame, "┌Failure Hotspots")
	if passingChecksRow < 0 || failedChecksRow < 0 || hotspotsRow < 0 || passingChecksRow != failedChecksRow || failedChecksRow != hotspotsRow {
		t.Fatalf("summary panels should be rendered side by side: passingChecks=%d failedChecks=%d hotspots=%d\n%s", passingChecksRow, failedChecksRow, hotspotsRow, frame)
	}
	passingWidth := panelTopWidth(frame, "Passing Checks")
	failedWidth := panelTopWidth(frame, "Failed Checks")
	hotspotsWidth := panelTopWidth(frame, "Failure Hotspots")
	if max(passingWidth, max(failedWidth, hotspotsWidth))-min(passingWidth, min(failedWidth, hotspotsWidth)) > 1 {
		t.Fatalf("summary panels should be evenly split: passing=%d failed=%d hotspots=%d\n%s", passingWidth, failedWidth, hotspotsWidth, frame)
	}
}

func TestInitStartsEventAndClockCommands(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", nil, events)
	msg := m.Init()()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("Init msg = %T, want tea.BatchMsg", msg)
	}
	if len(batch) != 2 {
		t.Fatalf("Init batch size = %d, want 2", len(batch))
	}
}

func TestWindowResizeRequestsFullRepaint(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", nil, events)
	updated, cmd := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	next := updated.(model)
	if next.width != 120 || next.height != 40 {
		t.Fatalf("resize should update model dimensions, got %dx%d", next.width, next.height)
	}
	if cmd == nil {
		t.Fatalf("resize should request a full repaint")
	}
	if got := fmt.Sprintf("%T", cmd()); got != "tea.clearScreenMsg" {
		t.Fatalf("resize command = %s, want tea.clearScreenMsg", got)
	}
}

func TestQKeyDoesNotQuit(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", nil, events)
	_, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: 'q', Text: "q"}))
	if cmd != nil {
		t.Fatalf("q should not be bound to quit")
	}
	help := stripANSI(m.helpBar(120))
	if strings.Contains(help, "q=Quit") {
		t.Fatalf("help should not advertise q quit: %q", help)
	}
}

func TestDashboardPanelHeightsFollowRoundTimelineAndCheckStatusContent(t *testing.T) {
	events := make(chan watch.Event)
	agents := []watch.AgentSnapshot{
		{ID: "agent-a", Name: "pixel-a"},
		{ID: "agent-b", Name: "pixel-b"},
	}
	m := newModel("shownet-watch", []watch.Target{
		{Name: "u7-5ghz", SSID: "SHIZK RADIO"},
		{Name: "u6-5ghz", SSID: "SHIZK RADIO"},
	}, events, agents)
	at := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	m.apply(watch.Event{
		Time:   at,
		Kind:   watch.EventFinding,
		Agent:  agents[0],
		Target: watch.TargetSnapshot{Name: "u7-5ghz", SSID: "SHIZK RADIO"},
		Step:   watch.StepSnapshot{Name: "connect", Type: "connect", Status: "failed"},
		Finding: &watch.Finding{
			Target: "u7-5ghz",
			Check:  "connect",
			Metric: "status",
		},
	})
	m.apply(watch.Event{
		Time:     at.Add(time.Second),
		Kind:     watch.EventStepFinished,
		Agent:    agents[1],
		Target:   watch.TargetSnapshot{Name: "u6-5ghz", SSID: "SHIZK RADIO"},
		Step:     watch.StepSnapshot{Name: "ping cloudflare", Type: "ping", Status: "ok"},
		Status:   "ok",
		Duration: 42,
	})

	roundTimeline, checkStatus, summary, eventLog := m.dashboardPanelHeights(40)
	if roundTimeline != 4 {
		t.Fatalf("roundTimeline height = %d, want 4", roundTimeline)
	}
	if checkStatus != 5 {
		t.Fatalf("checkStatus height = %d, want 5", checkStatus)
	}
	if summary != 40-roundTimeline-checkStatus-eventLog {
		t.Fatalf("summary height = %d, want remaining buffer", summary)
	}
}

func TestRunQueueTreeExpandsOnlyRunningTargets(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", []watch.Target{}, events)
	m.targets = []targetState{
		{
			target: targetSnapshot("done-target"),
			status: "ok",
			steps:  []stepState{{name: "connect", status: "ok"}},
			plannedSteps: []stepState{
				{name: "connect", typ: "connect"},
				{name: "wait_connected", typ: "wait_connected"},
			},
		},
		{
			target:      targetSnapshot("running-target"),
			status:      "running",
			currentStep: "ping cloudflare",
			plannedSteps: []stepState{
				{name: "connect", typ: "connect"},
				{name: "wait_connected", typ: "wait_connected"},
				{name: "ping cloudflare", typ: "ping"},
				{name: "download", typ: "download"},
			},
			steps: []stepState{
				{name: "connect", status: "ok"},
				{name: "ping cloudflare", status: "running"},
			},
		},
		{
			target: targetSnapshot("waiting-target"),
			status: "pending",
			plannedSteps: []stepState{
				{name: "connect", typ: "connect"},
				{name: "wait_connected", typ: "wait_connected"},
			},
		},
	}

	lines := m.runQueueTreeLines(80)
	text := stripANSI(strings.Join(runQueueTexts(lines), "\n"))
	for _, want := range []string{
		"OK   done-target",
		"RUN  running-target",
		"  ├── OK   connect",
		"  ├── WAIT wait_connected",
		"  ├── RUN  ping cloudflare",
		"  └── WAIT download",
		"WAIT waiting-target",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("run queue tree missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "done-target\n  ") {
		t.Fatalf("completed target should not be expanded:\n%s", text)
	}
	if strings.Contains(text, "WAIT waiting-target\n  ") {
		t.Fatalf("waiting target should not be expanded:\n%s", text)
	}
}

func TestRunQueueCursorStaysOnChildBetweenStepFinishAndNextStart(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", []watch.Target{}, events)
	m.targets = []targetState{{
		target:      targetSnapshot("running-target"),
		status:      "running",
		currentStep: "ping cloudflare",
		plannedSteps: []stepState{
			{name: "connect", typ: "connect"},
			{name: "ping cloudflare", typ: "ping"},
			{name: "disconnect", typ: "cleanup"},
		},
		steps: []stepState{
			{name: "connect", status: "ok"},
			{name: "ping cloudflare", status: "running"},
		},
	}}
	m.updateRunQueueCursor()
	before := m.runQueueCursor

	m.targets[0].currentStep = ""
	m.targets[0].steps[1].status = "ok"
	m.updateRunQueueCursor()

	if before != 2 || m.runQueueCursor != 2 {
		t.Fatalf("cursor should stay on the just-finished child row, before=%d after=%d", before, m.runQueueCursor)
	}
	lines := m.runQueueTreeLines(80)
	if lines[0].current {
		t.Fatalf("parent row should not become current between step finish and next start: %#v", lines)
	}
	if !lines[2].current || !strings.Contains(stripANSI(lines[2].text), "OK   ping cloudflare") {
		t.Fatalf("just-finished child row should remain current: %#v", lines)
	}
}

func TestRunQueueTreeOmitsTargetOutcomeStrip(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", []watch.Target{{Name: "u7-5ghz", SSID: "SHIZK RADIO"}}, events)
	at := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	m.apply(watch.Event{
		Time:     at,
		Kind:     watch.EventStepFinished,
		Round:    1,
		Target:   watch.TargetSnapshot{Name: "u7-5ghz", SSID: "SHIZK RADIO"},
		Step:     watch.StepSnapshot{Name: "connect", Type: "connect", Status: "ok"},
		Status:   "ok",
		Duration: 42,
	})
	m.apply(watch.Event{
		Time:   at.Add(time.Second),
		Kind:   watch.EventFinding,
		Round:  1,
		Target: watch.TargetSnapshot{Name: "u7-5ghz", SSID: "SHIZK RADIO"},
		Step:   watch.StepSnapshot{Name: "ping cloudflare", Type: "ping", Status: "failed"},
		Status: "failed",
		Finding: &watch.Finding{
			Target:   "u7-5ghz",
			Check:    "ping cloudflare",
			Metric:   "received",
			Observed: "0",
			Expected: "== 5",
			Message:  "constraint failed",
		},
	})

	text := stripANSI(m.runQueueTreeView(48, 6))
	for _, want := range []string{"u7-5ghz", "fail=1"} {
		if !strings.Contains(text, want) {
			t.Fatalf("run queue tree missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "▁") || strings.Contains(text, "█") || strings.Contains(text, "▌") {
		t.Fatalf("run queue tree should not render target outcome bars:\n%s", text)
	}
}

func TestRunQueuePanelsSplitByAgent(t *testing.T) {
	events := make(chan watch.Event)
	agents := []watch.AgentSnapshot{
		{ID: "agent-a", Name: "pixel-a"},
		{ID: "agent-b", Name: "pixel-b"},
	}
	m := newModelWithChecks("shownet-watch", []watch.Target{{Name: "u7-5ghz", SSID: "SHIZK RADIO"}}, []watch.Check{{Name: "wifi link", Type: "wifi_status"}}, events, agents)
	m.targets[0].status = "running"
	m.targets[0].currentStep = "connect"
	m.targets[0].steps = []stepState{{name: "connect", status: "running"}}
	m.targets[1].status = "pending"

	text := stripANSI(m.runQueuePanelsView(48, 16))
	for _, want := range []string{"┌Run Queue pixel-a", "┌Run Queue pixel-b", "RUN  u7-5ghz", "├── RUN  connect", "WAIT u7-5ghz", "└── WAIT disconnect"} {
		if !strings.Contains(text, want) {
			t.Fatalf("split run queue panel missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "pixel-a u7-5ghz") || strings.Contains(text, "pixel-b u7-5ghz") {
		t.Fatalf("agent-scoped run queue panel should not repeat agent names on target rows:\n%s", text)
	}
	if strings.Contains(text, "WAIT connect") {
		t.Fatalf("waiting agent target should not expand child rows:\n%s", text)
	}
	if strings.Contains(text, "▁") || strings.Contains(text, "█") || strings.Contains(text, "▌") {
		t.Fatalf("split run queue panel should not render outcome bars:\n%s", text)
	}
}

func TestRunQueueTreeKeepsScrollAnchorWhenNoTargetIsActive(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", []watch.Target{}, events)
	m.height = 9
	for i := 0; i < 16; i++ {
		status := "pending"
		if i == 12 {
			status = "running"
		}
		m.targets = append(m.targets, targetState{
			target: targetSnapshot(fmt.Sprintf("target-%02d", i)),
			status: status,
			steps:  []stepState{{name: "connect", status: "ok"}},
		})
	}
	m.updateRunQueueCursor()
	running := stripANSI(m.runQueueTreeView(80, 5))
	if !strings.Contains(running, "target-12") {
		t.Fatalf("running target should be visible:\n%s", running)
	}

	m.targets[12].status = "ok"
	m.updateRunQueueCursor()
	idle := stripANSI(m.runQueueTreeView(80, 5))
	if !strings.Contains(idle, "target-12") {
		t.Fatalf("run queue should keep the previous scroll anchor after active target finishes:\n%s", idle)
	}
	if strings.Contains(idle, "target-00") {
		t.Fatalf("run queue should not jump back to the top when no target is active:\n%s", idle)
	}
}

func TestRunQueueTreeUsesStableOffsetInsteadOfRecentering(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", []watch.Target{}, events)
	m.height = 9
	for i := 0; i < 12; i++ {
		m.targets = append(m.targets, targetState{
			target: targetSnapshot(fmt.Sprintf("target-%02d", i)),
			status: "pending",
		})
	}
	m.targets[4].status = "running"
	m.updateRunQueueCursor()
	if m.runQueueOffset != 0 {
		t.Fatalf("visible active row should not recenter run queue offset, got %d", m.runQueueOffset)
	}

	m.targets[4].status = "ok"
	m.targets[6].status = "running"
	m.updateRunQueueCursor()
	if m.runQueueOffset != 2 {
		t.Fatalf("offset should move only enough to reveal active row, got %d", m.runQueueOffset)
	}
}

func TestRenderShowsPassingRows(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", []watch.Target{
		{Name: "SHIZK RADIO", SSID: "SHIZK RADIO", Band: "5ghz"},
	}, events)
	m.width = 150
	m.height = 34
	at := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	target := watch.TargetSnapshot{Name: "SHIZK RADIO", SSID: "SHIZK RADIO", Band: "5ghz"}
	m.apply(watch.Event{
		Time:     at,
		Kind:     watch.EventStepFinished,
		Round:    3,
		Target:   target,
		Step:     watch.StepSnapshot{Name: "ping cloudflare", Type: "ping", Status: "ok"},
		Status:   "ok",
		Duration: 42,
	})
	m.apply(watch.Event{
		Time:     at.Add(time.Second),
		Kind:     watch.EventTargetFinished,
		Round:    3,
		Target:   target,
		Status:   "ok",
		Duration: 220,
	})

	frame := stripANSI(m.render())
	for _, want := range []string{"Passing Checks", "09:30:01", "SHIZK RADIO", "ping cloudflare", "target", "42ms", "220ms"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("passing checks panel missing %q:\n%s", want, frame)
		}
	}
}

func TestPassingAndFailedChecksRenderCompactTables(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", []watch.Target{}, events)
	at := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	m.now = at.Add(5 * time.Second)
	target := watch.TargetSnapshot{Name: "SHIZK RADIO", SSID: "SHIZK RADIO"}
	for i := 0; i < 3; i++ {
		m.apply(watch.Event{
			Time:     at.Add(time.Duration(i) * time.Second),
			Kind:     watch.EventStepFinished,
			Round:    uint64(i + 1),
			Target:   target,
			Step:     watch.StepSnapshot{Name: "connect", Type: "connect", Status: "ok"},
			Status:   "ok",
			Duration: 12,
		})
	}
	m.apply(watch.Event{
		Time:   at.Add(4 * time.Second),
		Kind:   watch.EventFinding,
		Round:  4,
		Target: target,
		Step:   watch.StepSnapshot{Name: "ping cloudflare", Type: "ping", Status: "failed"},
		Status: "failed",
		Finding: &watch.Finding{
			Target:   "SHIZK RADIO",
			Check:    "ping cloudflare",
			Metric:   "received",
			Observed: "0",
			Expected: "== 5",
			Message:  "constraint failed",
		},
	})

	passingChecks := stripANSI(m.passingChecksView(96, 8))
	failedChecks := stripANSI(m.failedChecksView(96, 8))
	passingTable := strings.Join(strings.Split(passingChecks, "\n")[:4], "\n")
	failedTable := strings.Join(strings.Split(failedChecks, "\n")[:4], "\n")
	for _, want := range []string{"Cnt", "Avg", "Max", "12ms"} {
		if !strings.Contains(passingChecks, want) {
			t.Fatalf("passing checks should render %q:\n%s", want, passingChecks)
		}
	}
	for _, want := range []string{"Cnt", "Fail%", "Strk", "100%", "1"} {
		if !strings.Contains(failedChecks, want) {
			t.Fatalf("failed checks should render %q:\n%s", want, failedChecks)
		}
	}
	if strings.Contains(passingChecks, "Bar") || strings.Contains(failedChecks, "Bar") {
		t.Fatalf("summary panels should use grouped bar lists, not a Bar column:\npassing checks:\n%s\nfailed checks:\n%s", passingChecks, failedChecks)
	}
	if strings.Contains(passingChecks, " / ") || strings.Contains(failedChecks, " / ") {
		t.Fatalf("summary panels should use aligned columns, not slash-delimited labels:\npassing checks:\n%s\nfailed checks:\n%s", passingChecks, failedChecks)
	}
	if strings.Contains(passingTable, "█") || strings.Contains(failedTable, "█") {
		t.Fatalf("summary tables should not spend columns on horizontal count bars:\npassing checks:\n%s\nfailed checks:\n%s", passingChecks, failedChecks)
	}
	if strings.Contains(passingTable, "▏") || strings.Contains(failedTable, "▏") {
		t.Fatalf("summary tables should not render ambiguous tick glyphs:\npassing checks:\n%s\nfailed checks:\n%s", passingChecks, failedChecks)
	}
	if !strings.Contains(passingChecks, "passing checks events last=30m") || !strings.Contains(failedChecks, "failed checks events last=30m") {
		t.Fatalf("summary panels should pin event sparklines at the bottom:\npassing checks:\n%s\nfailed checks:\n%s", passingChecks, failedChecks)
	}
	if !strings.Contains(failedChecks, "09:30:04") {
		t.Fatalf("failedChecks should keep the last-seen time:\n%s", failedChecks)
	}
}

func TestFailureHotspotsRankTargetsByStreakRateCount(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", []watch.Target{}, events)
	at := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	m.now = at.Add(5 * time.Minute)
	targetA := watch.TargetSnapshot{Name: "radio-a", SSID: "SHIZK RADIO"}
	targetB := watch.TargetSnapshot{Name: "radio-b", SSID: "SHIZK RADIO"}
	targetC := watch.TargetSnapshot{Name: "radio-c", SSID: "SHIZK RADIO"}
	addPass := func(round uint64, at time.Time, target watch.TargetSnapshot) {
		m.apply(watch.Event{
			Time:     at,
			Kind:     watch.EventStepFinished,
			Round:    round,
			Target:   target,
			Step:     watch.StepSnapshot{Name: "connect", Type: "connect", Status: "ok"},
			Status:   "ok",
			Duration: 10,
		})
	}
	addFail := func(round uint64, at time.Time, target watch.TargetSnapshot, message string) {
		m.apply(watch.Event{
			Time:   at,
			Kind:   watch.EventFinding,
			Round:  round,
			Target: target,
			Step:   watch.StepSnapshot{Name: "connect", Type: "connect", Status: "failed"},
			Status: "failed",
			Finding: &watch.Finding{
				Target:   target.Name,
				Check:    "connect",
				Metric:   "status",
				Observed: "failed",
				Expected: "== ok",
				Message:  message,
			},
		})
	}

	addFail(1, at, targetA, "assoc timeout")
	addFail(2, at.Add(time.Minute), targetA, "REQUEST_DECLINED")
	addFail(3, at.Add(2*time.Minute), targetB, "dhcp timeout")
	addPass(4, at.Add(3*time.Minute), targetB)
	addFail(5, at.Add(4*time.Minute), targetC, "wait_connected timeout")
	addFail(6, at.Add(-31*time.Minute), watch.TargetSnapshot{Name: "old-radio"}, "old failure")

	rows := m.failureHotspots()
	if got, want := len(rows), 3; got != want {
		t.Fatalf("hotspot rows = %d, want %d: %#v", got, want, rows)
	}
	if got := []string{rows[0].target.Name, rows[1].target.Name, rows[2].target.Name}; !reflect.DeepEqual(got, []string{"radio-a", "radio-c", "radio-b"}) {
		t.Fatalf("hotspots sorted incorrectly: %#v rows=%#v", got, rows)
	}
	if rows[0].failStreak != 2 || rows[0].failCount != 2 || rows[0].runCount != 2 || rows[0].failRunCount != 2 {
		t.Fatalf("radio-a stats = %#v", rows[0])
	}
	view := stripANSI(m.failureHotspotsView(96, 8))
	for _, want := range []string{"Target", "Cause", "Fail%", "Fail/Run", "radio-a", "100%", "2/2", "REQUEST_DECLINED"} {
		if !strings.Contains(view, want) {
			t.Fatalf("failure hotspots view missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "old-radio") {
		t.Fatalf("failure hotspots should ignore events outside the 30m window:\n%s", view)
	}
}

func TestFailureHotspotsPanelSplitsByAgent(t *testing.T) {
	events := make(chan watch.Event)
	agents := []watch.AgentSnapshot{
		{ID: "agent-a", Name: "agent-a"},
		{ID: "agent-b", Name: "agent-b"},
	}
	m := newModel("shownet-watch", []watch.Target{
		{Name: "same-radio", SSID: "SHIZK RADIO"},
	}, events, agents)
	m.width = 220
	m.height = 50
	at := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	m.now = at.Add(time.Minute)
	target := watch.TargetSnapshot{Name: "same-radio", SSID: "SHIZK RADIO"}
	for i, agent := range agents {
		m.apply(watch.Event{
			Time:   at.Add(time.Duration(i) * time.Second),
			Kind:   watch.EventFinding,
			Round:  uint64(i + 1),
			Agent:  agent,
			Target: target,
			Step:   watch.StepSnapshot{Name: "connect", Type: "connect", Status: "failed"},
			Status: "failed",
			Finding: &watch.Finding{
				Target:   target.Name,
				Check:    "connect",
				Metric:   "status",
				Observed: "failed",
				Expected: "== ok",
				Message:  agent.Name + " timeout",
			},
		})
	}

	rows := m.failureHotspots()
	if got, want := len(rows), 2; got != want {
		t.Fatalf("same target failures from different agents should stay separate, got %d rows: %#v", got, rows)
	}
	for _, row := range rows {
		if row.failCount != 1 || row.runCount != 1 || agentKey(row.agent) == "" {
			t.Fatalf("hotspot row should retain per-agent stats: %#v", row)
		}
	}
	view := stripANSI(m.failureHotspotPanelsView(96, 14))
	agentATitle := lineIndex(view, "Failure Hotspots agent-a")
	agentBTitle := lineIndex(view, "Failure Hotspots agent-b")
	agentACause := lineIndex(view, "agent-a timeout")
	agentBCause := lineIndex(view, "agent-b timeout")
	if agentATitle < 0 || agentBTitle < 0 || agentBTitle <= agentATitle {
		t.Fatalf("hotspot panels should be stacked by agent:\n%s", view)
	}
	if agentACause <= agentATitle || agentACause >= agentBTitle {
		t.Fatalf("agent-a hotspot should render inside agent-a panel: title=%d cause=%d next=%d\n%s", agentATitle, agentACause, agentBTitle, view)
	}
	if agentBCause <= agentBTitle {
		t.Fatalf("agent-b hotspot should render inside agent-b panel: title=%d cause=%d\n%s", agentBTitle, agentBCause, view)
	}

	m = updateKey(t, m, tea.Key{Code: tea.KeyTab})
	if m.focus != focusFailureHotspots || m.focusHotspotAgentKey != roundAgentKey(agents[0]) {
		t.Fatalf("first tab from failed checks should focus first hotspot agent panel: focus=%v hotspot=%q", m.focus, m.focusHotspotAgentKey)
	}
	m = updateKey(t, m, tea.Key{Code: tea.KeyTab})
	if m.focus != focusFailureHotspots || m.focusHotspotAgentKey != roundAgentKey(agents[1]) {
		t.Fatalf("second tab should focus second hotspot agent panel: focus=%v hotspot=%q", m.focus, m.focusHotspotAgentKey)
	}
	m = updateKey(t, m, tea.Key{Code: tea.KeyTab})
	if m.focus != focusPassingChecks {
		t.Fatalf("third tab should wrap to passing checks, got %v", m.focus)
	}
}

func TestFailureHotspotNavigationStaysWithinFocusedAgent(t *testing.T) {
	events := make(chan watch.Event)
	agents := []watch.AgentSnapshot{
		{ID: "agent-a", Name: "agent-a"},
		{ID: "agent-b", Name: "agent-b"},
	}
	m := newModel("shownet-watch", []watch.Target{
		{Name: "radio-a1", SSID: "SHIZK RADIO"},
		{Name: "radio-a2", SSID: "SHIZK RADIO"},
		{Name: "radio-b1", SSID: "SHIZK RADIO"},
	}, events, agents)
	m.width = 220
	m.height = 50
	at := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	m.now = at.Add(time.Minute)
	addFail := func(agent watch.AgentSnapshot, target string, offset time.Duration) {
		m.apply(watch.Event{
			Time:   at.Add(offset),
			Kind:   watch.EventFinding,
			Round:  uint64(offset/time.Second) + 1,
			Agent:  agent,
			Target: watch.TargetSnapshot{Name: target, SSID: "SHIZK RADIO"},
			Step:   watch.StepSnapshot{Name: "connect", Type: "connect", Status: "failed"},
			Status: "failed",
			Finding: &watch.Finding{
				Target:   target,
				Check:    "connect",
				Metric:   "status",
				Observed: "failed",
				Expected: "== ok",
				Message:  target + " timeout",
			},
		})
	}
	addFail(agents[0], "radio-a1", 0)
	addFail(agents[0], "radio-a2", time.Second)
	addFail(agents[1], "radio-b1", 2*time.Second)

	m = updateKey(t, m, tea.Key{Code: tea.KeyTab})
	if m.focusHotspotAgentKey != roundAgentKey(agents[0]) {
		t.Fatalf("expected first hotspot agent focus, got %q", m.focusHotspotAgentKey)
	}
	if got := m.filteredFailureHotspots()[m.failureHotspotCursor].target.Name; got != "radio-a2" {
		t.Fatalf("first focused agent cursor = %q, want radio-a2", got)
	}
	m = updateKey(t, m, tea.Key{Code: 'j', Text: "j"})
	if got := m.filteredFailureHotspots()[m.failureHotspotCursor].target.Name; got != "radio-a1" {
		t.Fatalf("j should stay within agent-a hotspot rows, got %q", got)
	}
	m = updateKey(t, m, tea.Key{Code: tea.KeyTab})
	if m.focusHotspotAgentKey != roundAgentKey(agents[1]) {
		t.Fatalf("expected second hotspot agent focus, got %q", m.focusHotspotAgentKey)
	}
	if got := m.filteredFailureHotspots()[m.failureHotspotCursor].target.Name; got != "radio-b1" {
		t.Fatalf("tab to agent-b should move cursor into agent-b rows, got %q", got)
	}
	m = updateKey(t, m, tea.Key{Code: tea.KeyEnter})
	frame := stripANSI(m.render())
	for _, want := range []string{"Failure Hotspot Detail", "agent=agent-b  target=radio-b1"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("agent-b hotspot detail missing %q:\n%s", want, frame)
		}
	}
}

func TestTabCyclesThroughFailureHotspotsWhenVisible(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", []watch.Target{}, events)
	m.width = 220
	m.height = 50

	if !m.failureHotspotsVisible() {
		t.Fatalf("wide frame should expose failure hotspots panel")
	}
	m = updateKey(t, m, tea.Key{Code: tea.KeyTab})
	if m.focus != focusFailureHotspots {
		t.Fatalf("tab from failed checks should focus failure hotspots, got %v", m.focus)
	}
	m = updateKey(t, m, tea.Key{Code: tea.KeyTab})
	if m.focus != focusPassingChecks {
		t.Fatalf("tab from failure hotspots should focus passing checks, got %v", m.focus)
	}
	m = updateKey(t, m, tea.Key{Code: tea.KeyTab})
	if m.focus != focusFailedChecks {
		t.Fatalf("tab from passing checks should focus failed checks, got %v", m.focus)
	}
}

func TestEnterShowsFailureHotspotDetail(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", []watch.Target{}, events)
	m.width = 220
	m.height = 50
	at := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	m.now = at.Add(2 * time.Minute)
	target := watch.TargetSnapshot{Name: "radio-a", SSID: "SHIZK RADIO", Band: "2.4ghz"}
	for round, at := range []time.Time{at, at.Add(time.Minute)} {
		m.apply(watch.Event{
			Time:   at,
			Kind:   watch.EventFinding,
			Round:  uint64(round + 1),
			Target: target,
			Step:   watch.StepSnapshot{Name: "connect", Type: "connect", Status: "failed"},
			Status: "failed",
			Finding: &watch.Finding{
				Target:   target.Name,
				Check:    "connect",
				Metric:   "status",
				Observed: "failed",
				Expected: "== ok",
				Message:  "REQUEST_DECLINED",
			},
		})
	}

	m = updateKey(t, m, tea.Key{Code: tea.KeyTab})
	if m.focus != focusFailureHotspots {
		t.Fatalf("tab should focus hotspot panel before opening detail, got %v", m.focus)
	}
	m = updateKey(t, m, tea.Key{Code: tea.KeyEnter})
	frame := stripANSI(m.render())
	for _, want := range []string{
		"Failure Hotspot Detail",
		"target=radio-a  ssid=SHIZK RADIO  band=2.4ghz",
		"cause=REQUEST_DECLINED",
		"fail_rate=100%  fail_runs=2/2  failures=2  streak=2  last=09:31:00  events=last=30m count=2 peak=1",
		"check=connect  metric=status  observed=failed  expected=\"== ok\"",
		"30m ago",
		"now",
		"█",
	} {
		if !strings.Contains(frame, want) {
			t.Fatalf("failure hotspot detail view missing %q:\n%s", want, frame)
		}
	}
}

func TestFailureHotspotDetailCursorFollowsOpenedItemAcrossUpdates(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", []watch.Target{}, events)
	m.width = 220
	m.height = 50
	m.focus = focusFailureHotspots
	at := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	m.now = at.Add(2 * time.Minute)
	targetA := watch.TargetSnapshot{Name: "radio-a", SSID: "SHIZK RADIO"}
	targetB := watch.TargetSnapshot{Name: "radio-b", SSID: "SHIZK RADIO"}
	addFail := func(round uint64, at time.Time, target watch.TargetSnapshot) {
		m.apply(watch.Event{
			Time:   at,
			Kind:   watch.EventFinding,
			Round:  round,
			Target: target,
			Step:   watch.StepSnapshot{Name: "connect", Type: "connect", Status: "failed"},
			Status: "failed",
			Finding: &watch.Finding{
				Target:   target.Name,
				Check:    "connect",
				Metric:   "status",
				Observed: "failed",
				Expected: "== ok",
				Message:  "REQUEST_DECLINED",
			},
		})
	}
	addFail(1, at.Add(-time.Minute), targetA)
	addFail(2, at, targetB)
	m.failureHotspotCursor = 0
	m.openDetailForPanel(focusFailureHotspots)
	if got := m.filteredFailureHotspots()[m.failureHotspotCursor].target.Name; got != "radio-b" {
		t.Fatalf("opened hotspot row = %q, want radio-b", got)
	}

	addFail(3, at.Add(time.Minute), targetA)

	rows := m.filteredFailureHotspots()
	if got := rows[m.failureHotspotCursor].target.Name; got != "radio-b" {
		t.Fatalf("hotspot cursor should follow opened item after re-sort, got %q rows=%#v", got, rows)
	}
	view := stripANSI(m.failureHotspotDetailView(100, 12))
	if !strings.Contains(view, "target=radio-b") {
		t.Fatalf("hotspot detail modal should keep showing opened item:\n%s", view)
	}
}

func TestRoundTimelineShowsRoundProgressGauge(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", []watch.Target{}, events)
	m.round = 7
	m.targets = []targetState{
		{target: targetSnapshot("ok"), status: "ok"},
		{target: targetSnapshot("failed"), status: "failed"},
		{target: targetSnapshot("running"), status: "running"},
		{target: targetSnapshot("pending"), status: "pending"},
	}

	text := stripANSI(m.roundProgressView(80))
	for _, want := range []string{"round=7", "progress=", "50%", "2/4", "run=1", "fail=1"} {
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
}

func TestSummarySparklineIsPinnedToBottom(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", []watch.Target{}, events)
	at := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	m.now = at.Add(4 * time.Second)
	for i := 0; i < 4; i++ {
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
	if !strings.Contains(lines[len(lines)-4], "passing checks events last=30m") {
		t.Fatalf("sparkline header should be pinned above the bottom graph:\n%s", strings.Join(lines, "\n"))
	}
	graph := strings.Join(lines[len(lines)-3:], "\n")
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
	if before.max != after.max || before.count != after.count {
		t.Fatalf("histogram stats changed while no event entered or expired: before=%#v after=%#v", before, after)
	}
	if got, want := after.counts[:9], before.counts[1:]; !reflect.DeepEqual(got, want) {
		t.Fatalf("histogram should scroll left one bucket without reshaping:\nbefore=%v\nafter =%v", before.counts, after.counts)
	}
	if after.counts[9] != 0 {
		t.Fatalf("new rightmost bucket should be empty without new events: %v", after.counts)
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
	if !reflect.DeepEqual(after.counts, before.counts) {
		t.Fatalf("histogram should keep stable bucket boundaries until the next bucket:\nbefore=%v\nafter =%v", before.counts, after.counts)
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
	if !reflect.DeepEqual(after.counts[:4], before.counts[:4]) {
		t.Fatalf("new live event should not stack into past buckets:\nbefore=%v\nafter =%v", before.counts, after.counts)
	}
	if got, want := after.counts[4], before.counts[4]+1; got != want {
		t.Fatalf("new live event should stack only in the current bucket: got %d want %d counts=%v", got, want, after.counts)
	}
}

func TestSummarySparklineScalesThirtyMinuteWindowToPanelWidth(t *testing.T) {
	now := time.Date(2026, 5, 16, 9, 35, 0, 0, time.UTC)
	reference := recentEventHistogram(nil, 480, summarySparklineWindow, now)
	times := []time.Time{
		reference.first.Add(reference.bucketWidth / 2),
		reference.first.Add(summarySparklineWindow / 2),
		reference.last.Add(-reference.bucketWidth / 2),
	}
	histogram := recentEventHistogram(times, 480, summarySparklineWindow, now)
	graph := stripANSI(strings.Join(renderSparkline(histogram.counts, histogram.max, 480, 3, okGraphStyle), "\n"))
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
	m := newModel("shownet-watch", []watch.Target{}, events)
	now := time.Date(2026, 5, 16, 9, 35, 0, 0, time.UTC)
	m.now = now
	for i := 0; i < 401; i++ {
		offset := time.Duration(i) * 29 * time.Minute / 400
		m.recordPassingCheck(passingCheckState{
			when:     now.Add(-29 * time.Minute).Add(offset),
			target:   watch.TargetSnapshot{Name: "lab"},
			step:     watch.StepSnapshot{Name: "connect", Type: "connect", Status: "ok"},
			duration: 10,
		})
	}

	times := m.passingCheckEventTimes()
	if got, want := len(times), 401; got != want {
		t.Fatalf("passing check history count = %d, want %d", got, want)
	}
	histogram := recentEventHistogram(times, 120, summarySparklineWindow, now)
	if got, want := histogram.count, 401; got != want {
		t.Fatalf("passing histogram count = %d, want %d", got, want)
	}
	if maxInt(histogram.counts[:30]) == 0 {
		t.Fatalf("old in-window passing events should still occupy the left side, counts=%v", histogram.counts)
	}
	if maxInt(histogram.counts[90:]) == 0 {
		t.Fatalf("recent passing events should occupy the right side, counts=%v", histogram.counts)
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
	if got := histogram.last.Sub(histogram.first); got != 30*time.Minute {
		t.Fatalf("histogram span = %s, want 30m", got)
	}
	if got, want := histogram.bucketWidth, 3750*time.Millisecond; got != want {
		t.Fatalf("480-column bucket width = %s, want %s", got, want)
	}
	wide := recentEventHistogram(nil, 1800, summarySparklineWindow, now)
	if got, want := wide.bucketWidth, time.Second; got != want {
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
		t.Fatalf("buckets should step through 5-event rows:\n%s", graph)
	}
}

func TestSummarySparklineHeaderShowsScaleWhenCompressed(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", []watch.Target{}, events)
	at := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	m.now = at.Add(10 * time.Second)
	for i := 0; i < 14; i++ {
		m.passingChecks = append(m.passingChecks, passingCheckState{
			when:   at.Add(5 * time.Second),
			target: watch.TargetSnapshot{Name: "lab"},
			step:   watch.StepSnapshot{Name: "connect"},
		})
	}
	view := stripANSI(m.summarySparklineView("passing checks", m.passingCheckEventTimes(), 120, 5, okGraphStyle))
	if !strings.Contains(view, "peak=14 scale=5/row") {
		t.Fatalf("compressed sparkline header should expose the absolute y scale:\n%s", view)
	}
}

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
	shortRight := strings.Index(short, "09:30:01")
	longRight := strings.Index(long, "09:30:02")
	if shortRight < 0 || longRight < 0 || lipgloss.Width(short[:shortRight]) != lipgloss.Width(long[:longRight]) {
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
	for _, line := range strings.Split(frame, "\n") {
		if strings.Contains(line, "Fail%") && strings.Contains(line, "Strk") {
			failedHeader = line
			break
		}
	}
	if !strings.Contains(failedHeader, "Cnt") || !strings.Contains(failedHeader, "Fail%") || !strings.Contains(failedHeader, "Strk") {
		t.Fatalf("initial failed header missing expected columns: %q\n%s", failedHeader, frame)
	}
}

func TestFailedCheckRemovesSameRoundPassingCheck(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", nil, events)
	at := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	target := watch.TargetSnapshot{Name: "SHIZK RADIO", SSID: "SHIZK RADIO"}
	step := watch.StepSnapshot{Name: "ping cloudflare", Type: "ping", Status: "ok"}
	m.apply(watch.Event{
		Time:     at,
		Kind:     watch.EventStepFinished,
		Round:    3,
		Target:   target,
		Step:     step,
		Status:   "ok",
		Duration: 42,
	})
	step.Status = "failed"
	m.apply(watch.Event{
		Time:   at.Add(time.Second),
		Kind:   watch.EventFinding,
		Round:  3,
		Target: target,
		Step:   step,
		Status: "failed",
		Finding: &watch.Finding{
			Target:   "SHIZK RADIO",
			Check:    "ping cloudflare",
			Metric:   "received",
			Observed: "0",
			Expected: "== 5",
			Message:  "constraint failed",
		},
	})

	if rows := m.passingCheckSummaries(); len(rows) != 0 {
		t.Fatalf("same-round failed check should remove the prior passing check, got %#v", rows)
	}
	frame := stripANSI(m.render())
	if !strings.Contains(frame, "no passing checks") {
		t.Fatalf("passing checks panel should be empty after matching failed check:\n%s", frame)
	}
}

func TestMultiAgentResultsStaySeparated(t *testing.T) {
	events := make(chan watch.Event)
	agents := []watch.AgentSnapshot{
		{ID: "agent-a", Name: "pixel-a", ADBSerial: "pixel-a"},
		{ID: "agent-b", Name: "pixel-b", ADBSerial: "pixel-b"},
	}
	m := newModel("shownet-watch", []watch.Target{{Name: "SHIZK RADIO", SSID: "SHIZK RADIO"}}, events, agents)
	m.width = 180
	m.height = 36
	at := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	target := watch.TargetSnapshot{Name: "SHIZK RADIO", SSID: "SHIZK RADIO"}
	m.apply(watch.Event{
		Time:     at,
		Kind:     watch.EventStepFinished,
		Agent:    agents[0],
		Round:    1,
		Target:   target,
		Step:     watch.StepSnapshot{Name: "connect", Type: "connect", Status: "ok"},
		Status:   "ok",
		Duration: 42,
	})
	m.apply(watch.Event{
		Time:   at.Add(time.Second),
		Kind:   watch.EventFinding,
		Agent:  agents[1],
		Round:  1,
		Target: target,
		Step:   watch.StepSnapshot{Name: "connect", Type: "connect", Status: "failed"},
		Status: "failed",
		Finding: &watch.Finding{
			Target:   "SHIZK RADIO",
			Check:    "connect",
			Metric:   "status",
			Observed: "failed",
			Expected: "== ok",
			Message:  "connect failed",
		},
	})

	frame := stripANSI(m.render())
	for _, want := range []string{
		"pixel-a",
		"pixel-b",
		"Run Queue pixel-a",
		"Run Queue pixel-b",
		"FAIL SHIZK RADIO fail=1",
		"connect failed",
	} {
		if !strings.Contains(frame, want) {
			t.Fatalf("multi-agent frame missing %q:\n%s", want, frame)
		}
	}
	if strings.Contains(frame, "   Agent") {
		t.Fatalf("summary panels should aggregate agents into Cnt instead of rendering an Agent column:\n%s", frame)
	}
	if rows := m.passingCheckSummaries(); len(rows) != 1 || rows[0].count != 1 {
		t.Fatalf("passingChecks should aggregate successful agents into count, got %#v", rows)
	}
	if rows := m.failedCheckSummaries(); len(rows) != 1 || rows[0].count != 1 {
		t.Fatalf("failedChecks should aggregate failing agents into count, got %#v", rows)
	}
}

func TestSummaryPanelsAggregateAgentsIntoCounts(t *testing.T) {
	events := make(chan watch.Event)
	agents := []watch.AgentSnapshot{
		{ID: "agent-a", Name: "pixel-a"},
		{ID: "agent-b", Name: "pixel-b"},
	}
	m := newModel("shownet-watch", []watch.Target{{Name: "SHIZK RADIO", SSID: "SHIZK RADIO"}}, events, agents)
	at := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	for i, agent := range agents {
		m.apply(watch.Event{
			Time:     at.Add(time.Duration(i) * time.Second),
			Kind:     watch.EventStepFinished,
			Agent:    agent,
			Round:    1,
			Target:   watch.TargetSnapshot{Name: "SHIZK RADIO", SSID: "SHIZK RADIO"},
			Step:     watch.StepSnapshot{Name: "connect", Type: "connect", Status: "ok"},
			Status:   "ok",
			Duration: 42,
		})
	}

	rows := m.passingCheckSummaries()
	if len(rows) != 1 || rows[0].count != 2 {
		t.Fatalf("passingChecks should aggregate matching agent rows into count +2, got %#v", rows)
	}
	table := stripANSI(m.passingChecksView(96, 8))
	if strings.Contains(table, "Agent") {
		t.Fatalf("passingChecks table should not render Agent column:\n%s", table)
	}
	if !strings.Contains(table, " 2 ") {
		t.Fatalf("passingChecks table should render aggregated count 2:\n%s", table)
	}
}

func TestCheckStatusUsesCurrentRoundStateBeforeHistory(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", []watch.Target{{Name: "SHIZK RADIO", SSID: "SHIZK RADIO"}}, events)
	target := watch.TargetSnapshot{Name: "SHIZK RADIO", SSID: "SHIZK RADIO"}
	m.apply(watch.Event{
		Time:     time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC),
		Kind:     watch.EventStepFinished,
		Round:    1,
		Target:   target,
		Step:     watch.StepSnapshot{Name: "connect", Type: "connect", Status: "ok"},
		Status:   "ok",
		Duration: 42,
	})
	if checkStatus := stripANSI(m.checkStatusView(72, 4)); !strings.Contains(checkStatus, "PASS") {
		t.Fatalf("previous round setup should record a passing check first:\n%s", checkStatus)
	}

	m.apply(watch.Event{Kind: watch.EventRoundStarted, Round: 2, Status: "running"})
	checkStatus := stripANSI(m.checkStatusView(72, 4))
	if !strings.Contains(checkStatus, "PASS") || strings.Contains(checkStatus, "WAIT") {
		t.Fatalf("new round should keep previous result token instead of replacing it with WAIT:\n%s", checkStatus)
	}
	cell := m.checkStatusTargetCell("connect", target, []watch.AgentSnapshot{{}})
	if cell.status != "ok" || !cell.stale {
		t.Fatalf("new round checkStatus cell = %#v, want stale ok", cell)
	}
}

func TestCheckStatusOmitsFullFailedCheckPercent(t *testing.T) {
	events := make(chan watch.Event)
	agents := []watch.AgentSnapshot{
		{ID: "agent-a", Name: "pixel-a"},
		{ID: "agent-b", Name: "pixel-b"},
	}
	m := newModel("shownet-watch", []watch.Target{{Name: "SHIZK RADIO", SSID: "SHIZK RADIO"}}, events, agents)
	at := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	for i, agent := range agents {
		m.apply(watch.Event{
			Time:   at.Add(time.Duration(i) * time.Second),
			Kind:   watch.EventFinding,
			Agent:  agent,
			Round:  1,
			Target: watch.TargetSnapshot{Name: "SHIZK RADIO", SSID: "SHIZK RADIO"},
			Step:   watch.StepSnapshot{Name: "ping cloudflare", Type: "ping", Status: "failed"},
			Status: "failed",
			Finding: &watch.Finding{
				Target:   "SHIZK RADIO",
				Check:    "ping cloudflare",
				Metric:   "received",
				Observed: "0",
				Expected: "== 5",
				Message:  "constraint failed",
			},
		})
	}

	checkStatus := stripANSI(m.checkStatusView(72, 4))
	if strings.Contains(checkStatus, "FAIL(100%)") || !strings.Contains(checkStatus, "FAIL") {
		t.Fatalf("all-agent failed check should render FAIL without a 100%% suffix:\n%s", checkStatus)
	}
}

func TestCheckStatusRendersPartialNonFailedStatusPercent(t *testing.T) {
	events := make(chan watch.Event)
	agents := []watch.AgentSnapshot{
		{ID: "agent-a", Name: "pixel-a"},
		{ID: "agent-b", Name: "pixel-b"},
	}
	target := watch.TargetSnapshot{Name: "u7-5ghz", SSID: "SHIZK RADIO"}
	m := newModel("shownet-watch", []watch.Target{{Name: "u7-5ghz", SSID: "SHIZK RADIO"}}, events, agents)
	m.apply(watch.Event{
		Kind:   watch.EventStepFinished,
		Agent:  agents[0],
		Round:  1,
		Target: target,
		Step:   watch.StepSnapshot{Name: "wifi link", Type: "wifi_status", Status: "skipped", Skipped: true},
		Status: "skipped",
	})

	checkStatus := stripANSI(m.checkStatusView(72, 4))
	if !strings.Contains(checkStatus, "SKIP(50%)") {
		t.Fatalf("partial skipped check should render percentage:\n%s", checkStatus)
	}

	m.apply(watch.Event{
		Kind:     watch.EventStepFinished,
		Agent:    agents[1],
		Round:    1,
		Target:   target,
		Step:     watch.StepSnapshot{Name: "wifi link", Type: "wifi_status", Status: "ok"},
		Status:   "ok",
		Duration: 10,
	})
	checkStatus = stripANSI(m.checkStatusView(72, 4))
	if !strings.Contains(checkStatus, "PASS(50%)") {
		t.Fatalf("mixed ok/skipped check should show partial pass instead of plain skip:\n%s", checkStatus)
	}
}

func TestCheckStatusKeepsPreviousFailedCheckDimmedOnNextRound(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", []watch.Target{{Name: "SHIZK RADIO", SSID: "SHIZK RADIO"}}, events)
	target := watch.TargetSnapshot{Name: "SHIZK RADIO", SSID: "SHIZK RADIO"}
	m.apply(watch.Event{
		Time:   time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC),
		Kind:   watch.EventFinding,
		Round:  1,
		Target: target,
		Step:   watch.StepSnapshot{Name: "ping cloudflare", Type: "ping", Status: "failed"},
		Status: "failed",
		Finding: &watch.Finding{
			Target:   "SHIZK RADIO",
			Check:    "ping cloudflare",
			Metric:   "received",
			Observed: "0",
			Expected: "== 5",
			Message:  "constraint failed",
		},
	})

	m.apply(watch.Event{Kind: watch.EventRoundStarted, Round: 2, Status: "running"})
	checkStatus := stripANSI(m.checkStatusView(72, 4))
	if !strings.Contains(checkStatus, "FAIL") || strings.Contains(checkStatus, "WAIT") {
		t.Fatalf("new round should keep previous failed-check token instead of replacing it with WAIT:\n%s", checkStatus)
	}
	cell := m.checkStatusTargetCell("ping cloudflare", target, []watch.AgentSnapshot{{}})
	if cell.status != "failed" || !cell.stale {
		t.Fatalf("new round checkStatus cell = %#v, want stale failed", cell)
	}
}

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
	m.now = at.Add(2 * time.Second)
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
		"connect",
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
		t.Fatalf("checkStatus should use checks as rows:\n%s", checkStatus)
	}
	timeline := stripANSI(m.roundTimelineView(120, 5))
	timelineLines := strings.Split(timeline, "\n")
	if len(timelineLines) == 0 ||
		!strings.Contains(timelineLines[0], "round=") ||
		!strings.Contains(timelineLines[0], "span=") ||
		!strings.Contains(timelineLines[0], "progress=") {
		t.Fatalf("round timeline header should keep round/span and right progress on one line:\n%s", timeline)
	}
	if strings.Contains(strings.Join(timelineLines[1:], "\n"), "span=") {
		t.Fatalf("round timeline should not render span on a separate second header line:\n%s", timeline)
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
		t.Fatalf("round timeline should group by target, not agent:\n%s", timeline)
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
	m.now = now
	m.round = 4
	target := watch.TargetSnapshot{Name: "u7-5ghz", SSID: "SHIZK RADIO"}
	m.failedChecks = []failedCheckState{
		{round: 3, when: now.Add(-3 * time.Second), target: target, finding: watch.Finding{Target: "u7-5ghz", Check: "dns a"}},
		{round: 3, when: now.Add(-3 * time.Second), target: target, finding: watch.Finding{Target: "u7-5ghz", Check: "ping v4"}},
		{round: 2, when: now.Add(-20 * time.Second), target: target, finding: watch.Finding{Target: "u7-5ghz", Check: "connect"}},
	}

	buckets, total, peak := m.targetRoundHistory(target, 20)
	if total != 3 || peak != 2 {
		t.Fatalf("target round history stats total=%d peak=%d buckets=%#v", total, peak, buckets)
	}
	if got := stripANSI(renderTargetRoundHistory([]targetRoundBucket{{seen: true, failed: 1, connectFailed: true}}, 8, 1)); got != "X" {
		t.Fatalf("connection failure should render as a red X, got %q", got)
	}
	if got := stripANSI(renderTargetRoundHistory([]targetRoundBucket{{seen: true, failed: 1}}, 8, 1)); got != "▂" {
		t.Fatalf("one failed check should render at the low absolute height, got %q", got)
	}
	if got := stripANSI(renderTargetRoundHistory([]targetRoundBucket{{seen: true, failed: 8}}, 8, 1)); got != "█" {
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
	later.now = now.Add(time.Hour)
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
	m.round = 1
	m.passingChecks = []passingCheckState{{
		round:  1,
		agent:  agents[0],
		target: target,
		step:   watch.StepSnapshot{Name: "connect", Type: "connect", Status: "ok"},
	}}
	m.failedChecks = []failedCheckState{{
		round:   1,
		agent:   agents[1],
		target:  target,
		finding: watch.Finding{Target: "u6-2.4ghz", Check: "connect"},
	}}

	buckets, _, _ := m.targetRoundHistory(target, 1)
	if buckets[0].connectFailed {
		t.Fatalf("single-agent connect failure should not mark the target bucket as total connection failure: %#v", buckets[0])
	}
	if got := stripANSI(renderTargetRoundHistory(buckets, 8, 1)); got == "X" {
		t.Fatalf("partial multi-agent connect failure should not render as X")
	}

	m.passingChecks = nil
	m.failedChecks = []failedCheckState{
		{round: 1, agent: agents[0], target: target, finding: watch.Finding{Target: "u6-2.4ghz", Check: "connect"}},
		{round: 1, agent: agents[1], target: target, finding: watch.Finding{Target: "u6-2.4ghz", Check: "connect"}},
	}
	buckets, _, _ = m.targetRoundHistory(target, 1)
	if !buckets[0].connectFailed {
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
	m.round = 20

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
		t.Fatalf("narrow timeline should wrap targets:\n%s", narrow)
	}
	for _, want := range []string{"t0", "t1", "t2", "t3", "t4"} {
		if !strings.Contains(narrowLines[1], want) {
			t.Fatalf("narrow first row should use the maximum fitting columns, missing %q:\n%s", want, narrow)
		}
	}
	if strings.Contains(narrowLines[1], "t5") || !strings.Contains(narrowLines[2], "t5") {
		t.Fatalf("narrow timeline should wrap only after columns can no longer keep ten rounds:\n%s", narrow)
	}
	start, end = m.roundTimelineRoundSpan(100)
	if got := int(end - start + 1); got < 10 {
		t.Fatalf("narrow timeline visible rounds = %d, want at least 10", got)
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
		m.targets[0].steps = append(m.targets[0].steps, stepState{name: check, status: "ok"})
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
		if !strings.Contains(checkStatus, check) {
			t.Fatalf("checkStatus missing check %q:\n%s", check, checkStatus)
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

func TestConnectFailedCheckAppearsInFailedChecks(t *testing.T) {
	events := make(chan watch.Event)
	agent := watch.AgentSnapshot{ID: "agent-b", Name: "pixel-b", ADBSerial: "pixel-b"}
	m := newModel("shownet-watch", []watch.Target{{Name: "SHIZK RADIO", SSID: "SHIZK RADIO"}}, events, []watch.AgentSnapshot{agent})
	at := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	m.apply(watch.Event{
		Time:   at,
		Kind:   watch.EventStepFinished,
		Agent:  agent,
		Round:  1,
		Target: watch.TargetSnapshot{Name: "SHIZK RADIO", SSID: "SHIZK RADIO"},
		Step:   watch.StepSnapshot{Name: "connect", Type: "connect", Status: "failed", Message: "wifi connect failed"},
		Status: "failed",
	})

	rows := m.failedCheckSummaries()
	if len(rows) != 1 {
		t.Fatalf("failed check summary count = %d, want 1", len(rows))
	}
	finding := rows[0].finding
	if finding.Check != "connect" || finding.Metric != "status" || finding.Observed != "failed" || finding.Message != "wifi connect failed" {
		t.Fatalf("connect failed check = %#v", finding)
	}
}

func TestSummaryAndEventLogHeightsReduceEventLogAndKeepSummaryArea(t *testing.T) {
	summary, eventLog := summaryAndEventLogHeights(28)
	if summary+eventLog != 28 {
		t.Fatalf("panel heights do not fill body: summary=%d eventLog=%d", summary, eventLog)
	}
	if eventLog != 7 {
		t.Fatalf("eventLog height = %d, want 7", eventLog)
	}
	if summary != 21 {
		t.Fatalf("summary height = %d, want 21", summary)
	}
}

func TestRenderShowsFailedCheckRowsInMainTable(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", []watch.Target{
		{Name: "shownet-6g-ap1", SSID: "ShowNet", Band: "6ghz"},
	}, events)
	m.width = 140
	m.height = 30
	m.apply(watch.Event{Kind: watch.EventRoundStarted, Round: 3})
	m.apply(watch.Event{
		Time:   time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC),
		Kind:   watch.EventFinding,
		Round:  3,
		Target: watch.TargetSnapshot{Name: "shownet-6g-ap1", SSID: "ShowNet", Band: "6ghz"},
		Step:   watch.StepSnapshot{Name: "ping cloudflare", Type: "ping", Status: "failed"},
		Finding: &watch.Finding{
			Target:   "shownet-6g-ap1",
			Check:    "ping cloudflare",
			Metric:   "avg_latency_ms",
			Observed: "120",
			Expected: "<=50",
			Message:  "latency exceeded",
		},
	})

	frame := stripANSI(m.render())
	for _, want := range []string{"ping cl~", "avg_lat~", "100%", "1", "latency exceeded"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("failed checks table missing %q:\n%s", want, frame)
		}
	}
}

func TestFailedChecksCursorRowFillsPanelWidth(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", []watch.Target{}, events)
	m.apply(watch.Event{
		Time:   time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC),
		Kind:   watch.EventFinding,
		Target: watch.TargetSnapshot{Name: "SHIZK RADIO"},
		Step:   watch.StepSnapshot{Name: "ping cloudflare"},
		Finding: &watch.Finding{
			Target:   "SHIZK RADIO",
			Check:    "ping cloudflare",
			Metric:   "received",
			Observed: "0",
			Expected: "== 5",
			Message:  "constraint failed",
		},
	})
	table := stripANSI(m.failedChecksView(96, 6))
	lines := strings.Split(table, "\n")
	if len(lines) < 3 {
		t.Fatalf("failedChecks table rendered too few lines:\n%s", table)
	}
	selected := ""
	for _, line := range lines {
		if strings.Contains(line, "09:30:00") {
			selected = line
			break
		}
	}
	if selected == "" {
		t.Fatalf("selected failed check row not rendered:\n%s", table)
	}
	if got := runeLen(selected); got != 96 {
		t.Fatalf("selected failed check row width = %d, want 96: %q", got, selected)
	}
}

func TestPassingAndFailedChecksDoNotGroupBySSID(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", []watch.Target{}, events)
	at := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	m.apply(watch.Event{
		Time:     at,
		Kind:     watch.EventStepFinished,
		Round:    1,
		Target:   watch.TargetSnapshot{Name: "ap-1", SSID: "ShowNet"},
		Step:     watch.StepSnapshot{Name: "connect", Type: "connect", Status: "ok"},
		Status:   "ok",
		Duration: 12,
	})
	m.apply(watch.Event{
		Time:     at.Add(time.Second),
		Kind:     watch.EventStepFinished,
		Round:    1,
		Target:   watch.TargetSnapshot{Name: "lab-1", SSID: "LabNet"},
		Step:     watch.StepSnapshot{Name: "connect", Type: "connect", Status: "ok"},
		Status:   "ok",
		Duration: 15,
	})
	m.apply(watch.Event{
		Time:   at.Add(2 * time.Second),
		Kind:   watch.EventFinding,
		Round:  1,
		Target: watch.TargetSnapshot{Name: "ap-2", SSID: "ShowNet"},
		Step:   watch.StepSnapshot{Name: "ping cloudflare", Type: "ping", Status: "failed"},
		Finding: &watch.Finding{
			Target:   "ap-2",
			Check:    "ping cloudflare",
			Metric:   "received",
			Observed: "0",
			Expected: "== 5",
			Message:  "constraint failed",
		},
	})
	m.apply(watch.Event{
		Time:   at.Add(3 * time.Second),
		Kind:   watch.EventFinding,
		Round:  1,
		Target: watch.TargetSnapshot{Name: "lab-2", SSID: "LabNet"},
		Step:   watch.StepSnapshot{Name: "dns wide", Type: "dns", Status: "failed"},
		Finding: &watch.Finding{
			Target:   "lab-2",
			Check:    "dns wide",
			Metric:   "answers",
			Observed: "0",
			Expected: "> 0",
			Message:  "constraint failed",
		},
	})

	passingChecks := stripANSI(m.passingChecksView(100, 8))
	failedChecks := stripANSI(m.failedChecksView(100, 8))
	for _, frame := range []struct {
		name string
		text string
	}{
		{name: "passingChecks", text: passingChecks},
		{name: "failedChecks", text: failedChecks},
	} {
		for _, want := range []string{"09:30"} {
			if !strings.Contains(frame.text, want) {
				t.Fatalf("%s table missing %q:\n%s", frame.name, want, frame.text)
			}
		}
		if strings.Contains(frame.text, "SSID ") {
			t.Fatalf("%s table should not render SSID group rows:\n%s", frame.name, frame.text)
		}
	}
}

func TestEventLogPanelShowsRawWatchEvents(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", []watch.Target{
		{Name: "SHIZK RADIO", SSID: "SHIZK RADIO", Band: "5ghz"},
	}, events)
	m.width = 260
	m.height = 50
	at := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	target := watch.TargetSnapshot{Name: "SHIZK RADIO", SSID: "SHIZK RADIO", Band: "5ghz"}
	step := watch.StepSnapshot{Name: "ping cloudflare", Type: "ping", Operation: "request_ping", Status: "running"}
	m.apply(watch.Event{Time: at, Kind: watch.EventRoundStarted, Round: 9, Status: "running"})
	m.apply(watch.Event{Time: at, Kind: watch.EventTargetStarted, Round: 9, Target: target, Status: "running"})
	m.apply(watch.Event{Time: at, Kind: watch.EventStepStarted, Round: 9, Target: target, Step: step, Status: "running"})
	step.Status = "failed"
	step.Message = "constraint failed"
	m.apply(watch.Event{Time: at, Kind: watch.EventStepFinished, Round: 9, Target: target, Step: step, Status: "failed", Duration: 1234})
	m.apply(watch.Event{
		Time:   at,
		Kind:   watch.EventFinding,
		Round:  9,
		Target: target,
		Step:   step,
		Status: "failed",
		Finding: &watch.Finding{
			Target:   "SHIZK RADIO",
			Check:    "ping cloudflare",
			Metric:   "received",
			Observed: "0",
			Expected: "== 5",
			Message:  "constraint failed",
		},
	})

	joinedLogs := strings.Join(m.logs, "\n")
	for _, want := range []string{
		"kind=target_started round=9 status=running target=\"SHIZK RADIO\" ssid=\"SHIZK RADIO\" band=5ghz",
		"kind=step_started round=9 status=running target=\"SHIZK RADIO\" ssid=\"SHIZK RADIO\" band=5ghz step=\"ping cloudflare\" type=ping op=request_ping",
		"kind=step_finished round=9 status=failed target=\"SHIZK RADIO\" ssid=\"SHIZK RADIO\" band=5ghz step=\"ping cloudflare\" type=ping op=request_ping duration_ms=1234 msg=\"constraint failed\"",
		"kind=finding round=9 status=failed target=\"SHIZK RADIO\" ssid=\"SHIZK RADIO\" band=5ghz step=\"ping cloudflare\" type=ping op=request_ping check=\"ping cloudflare\" metric=received observed=0 expected=\"== 5\" finding_msg=\"constraint failed\"",
	} {
		if !strings.Contains(joinedLogs, want) {
			t.Fatalf("eventLog log buffer missing raw log %q:\n%s", want, joinedLogs)
		}
	}
	frame := stripANSI(m.render())
	for _, want := range []string{"target=SHIZK RADIO  step=ping cloudflare  last=ping cloudflare constraint failed", "kind=step_started", "kind=finding"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("eventLog panel missing raw log fragment %q:\n%s", want, frame)
		}
	}
}

func TestEventLogPanelSanitizesControlCharacters(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", []watch.Target{}, events)
	m.width = 180
	m.height = 18
	m.apply(watch.Event{
		Time:    time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC),
		Kind:    watch.EventLog,
		Message: "[warn agent=agent-a]\rtraceroute binary not available\nhost=1.1.1.1 \x1b]0;bad\a",
	})

	frame := stripANSI(m.render())
	for _, bad := range []string{"\r", "\n[warn", "\x1b", "\a"} {
		if strings.Contains(frame, bad) {
			t.Fatalf("frame contains unsanitized control sequence %q:\n%s", bad, frame)
		}
	}
	for _, want := range []string{
		`[warn agent=agent-a]\rtraceroute binary not available\nhost=1.1.1.1`,
		`Event Log`,
	} {
		if !strings.Contains(frame, want) {
			t.Fatalf("sanitized eventLog log missing %q:\n%s", want, frame)
		}
	}
	for i, line := range strings.Split(frame, "\n") {
		if got := lipgloss.Width(line); got > m.width {
			t.Fatalf("frame line %d width = %d, want <= %d: %q", i, got, m.width, line)
		}
	}
}

func TestEnterShowsFailedCheckDetail(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", []watch.Target{
		{Name: "SHIZK RADIO", SSID: "SHIZK RADIO", Band: "5ghz"},
	}, events)
	m.width = 150
	m.height = 50
	m.now = time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	m.apply(watch.Event{
		Time:   time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC),
		Kind:   watch.EventFinding,
		Round:  1,
		Target: watch.TargetSnapshot{Name: "SHIZK RADIO", SSID: "SHIZK RADIO", Band: "5ghz"},
		Step:   watch.StepSnapshot{Name: "ping cloudflare", Type: "ping", Status: "failed"},
		Status: "failed",
		Finding: &watch.Finding{
			Target:   "SHIZK RADIO",
			Check:    "ping cloudflare",
			Metric:   "received",
			Observed: "0",
			Expected: "== 5",
			Message:  "constraint failed",
		},
	})

	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m = updated.(model)
	frame := stripANSI(m.render())
	for _, want := range []string{
		"Failed Check Detail",
		"Event Log",
		"Run Queue",
		"target=SHIZK RADIO  check=ping cloudflare  metric=received",
		"observed=0  expected=\"== 5\"  message=constraint failed",
		"count=1  last=09:30:00  events=last=30m count=1 peak=1",
		"█",
		"30m ago",
		"now",
	} {
		if !strings.Contains(frame, want) {
			t.Fatalf("detail view missing %q:\n%s", want, frame)
		}
	}
}

func TestEnterShowsPassingCheckDetail(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", []watch.Target{
		{Name: "SHIZK RADIO", SSID: "SHIZK RADIO", Band: "5ghz"},
	}, events)
	m.width = 150
	m.height = 50
	at := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	m.now = at.Add(40 * time.Second)
	for _, second := range []int{0, 20, 40} {
		m.apply(watch.Event{
			Time:     at.Add(time.Duration(second) * time.Second),
			Kind:     watch.EventStepFinished,
			Round:    1,
			Target:   watch.TargetSnapshot{Name: "SHIZK RADIO", SSID: "SHIZK RADIO", Band: "5ghz"},
			Step:     watch.StepSnapshot{Name: "connect", Type: "connect", Operation: "wifi.connect", Status: "ok"},
			Status:   "ok",
			Duration: 120,
		})
	}

	m = updateKey(t, m, tea.Key{Code: tea.KeyTab})
	m = updateKey(t, m, tea.Key{Code: tea.KeyEnter})
	frame := stripANSI(m.render())
	for _, want := range []string{
		"Passing Check Detail",
		"Event Log",
		"Run Queue",
		"target=SHIZK RADIO  ssid=SHIZK RADIO  step=connect",
		"status=ok  type=connect  op=wifi.connect  duration=120ms",
		"count=3  last=09:30:40  events=last=30m count=3 peak=1",
		"█",
		"30m ago",
		"now",
	} {
		if !strings.Contains(frame, want) {
			t.Fatalf("passing check detail view missing %q:\n%s", want, frame)
		}
	}
}

func TestTabKeepsModalOpenAndMovesPanelFocus(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", []watch.Target{}, events)
	m.apply(watch.Event{
		Time:     time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC),
		Kind:     watch.EventStepFinished,
		Round:    1,
		Target:   watch.TargetSnapshot{Name: "SHIZK RADIO", SSID: "SHIZK RADIO"},
		Step:     watch.StepSnapshot{Name: "connect", Type: "connect", Status: "ok"},
		Status:   "ok",
		Duration: 10,
	})
	m = updateKey(t, m, tea.Key{Code: tea.KeyTab})
	m = updateKey(t, m, tea.Key{Code: tea.KeyEnter})
	if !m.detailOpen || m.detailPanel != focusPassingChecks {
		t.Fatalf("enter on passing checks should open passing check detail: open=%v panel=%v", m.detailOpen, m.detailPanel)
	}

	m = updateKey(t, m, tea.Key{Code: tea.KeyTab})
	if !m.detailOpen || m.focus != focusFailedChecks || m.detailPanel != focusFailedChecks {
		t.Fatalf("tab should keep modal open and move focus/detail panel: open=%v focus=%v detail=%v", m.detailOpen, m.focus, m.detailPanel)
	}
}

func TestDetailModalCursorFollowsOpenedItemAcrossUpdates(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", []watch.Target{}, events)
	m.focus = focusPassingChecks
	at := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	m.now = at.Add(time.Minute)
	targetA := watch.TargetSnapshot{Name: "target-a", SSID: "Lab"}
	targetB := watch.TargetSnapshot{Name: "target-b", SSID: "Lab"}
	m.apply(watch.Event{
		Time:     at.Add(-time.Minute),
		Kind:     watch.EventStepFinished,
		Round:    1,
		Target:   targetA,
		Step:     watch.StepSnapshot{Name: "connect", Type: "connect", Status: "ok"},
		Status:   "ok",
		Duration: 10,
	})
	m.apply(watch.Event{
		Time:     at,
		Kind:     watch.EventStepFinished,
		Round:    1,
		Target:   targetB,
		Step:     watch.StepSnapshot{Name: "connect", Type: "connect", Status: "ok"},
		Status:   "ok",
		Duration: 10,
	})
	m.passingCheckCursor = 0
	m.openDetailForPanel(focusPassingChecks)
	if got := m.filteredPassingCheckSummaries()[m.passingCheckCursor].target.Name; got != "target-b" {
		t.Fatalf("opened row = %q, want target-b", got)
	}

	m.apply(watch.Event{
		Time:     at.Add(time.Minute),
		Kind:     watch.EventStepFinished,
		Round:    2,
		Target:   targetA,
		Step:     watch.StepSnapshot{Name: "connect", Type: "connect", Status: "ok"},
		Status:   "ok",
		Duration: 12,
	})

	rows := m.filteredPassingCheckSummaries()
	if got := rows[m.passingCheckCursor].target.Name; got != "target-b" {
		t.Fatalf("cursor should follow opened item after re-sort, got %q rows=%#v", got, rows)
	}
	view := stripANSI(m.passingCheckDetailView(100, 12))
	if !strings.Contains(view, "target=target-b") {
		t.Fatalf("detail modal should keep showing opened item:\n%s", view)
	}
}

func TestFailedCheckDetailOverlaysExistingTUI(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", []watch.Target{
		{Name: "SHIZK RADIO", SSID: "SHIZK RADIO", Band: "5ghz"},
	}, events)
	m.width = 150
	m.height = 32
	m.apply(watch.Event{Kind: watch.EventWatchStarted, Message: "watch started"})
	m.apply(watch.Event{
		Time:   time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC),
		Kind:   watch.EventFinding,
		Round:  1,
		Target: watch.TargetSnapshot{Name: "SHIZK RADIO", SSID: "SHIZK RADIO", Band: "5ghz"},
		Step:   watch.StepSnapshot{Name: "ping cloudflare", Type: "ping", Status: "failed"},
		Status: "failed",
		Finding: &watch.Finding{
			Target:   "SHIZK RADIO",
			Check:    "ping cloudflare",
			Metric:   "received",
			Observed: "0",
			Expected: "== 5",
			Message:  "constraint failed",
		},
	})
	background := stripANSI(m.render())
	m.detailOpen = true
	overlaid := stripANSI(m.render())

	for _, want := range []string{"Event Log", "Failed Check Detail"} {
		if !strings.Contains(overlaid, want) {
			t.Fatalf("overlay frame missing %q:\n%s", want, overlaid)
		}
	}
	if lineIndex(background, "┌Failed Check Detail") >= 0 {
		t.Fatalf("background unexpectedly contains modal:\n%s", background)
	}
	if lineIndex(overlaid, "┌Failed Check Detail") < 0 {
		t.Fatalf("overlay does not contain modal:\n%s", overlaid)
	}
	if len(strings.Split(background, "\n")) != len(strings.Split(overlaid, "\n")) {
		t.Fatalf("overlay changed frame line count")
	}
}

func TestFailedCheckDetailShowsOccurrenceHistogram(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", []watch.Target{
		{Name: "SHIZK RADIO", SSID: "SHIZK RADIO", Band: "5ghz"},
	}, events)
	m.width = 160
	m.height = 50
	at := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	m.now = at.Add(60 * time.Second)
	for _, second := range []int{0, 0, 15, 30, 30, 30, 60} {
		m.apply(watch.Event{
			Time:   at.Add(time.Duration(second) * time.Second),
			Kind:   watch.EventFinding,
			Round:  1,
			Target: watch.TargetSnapshot{Name: "SHIZK RADIO", SSID: "SHIZK RADIO", Band: "5ghz"},
			Step:   watch.StepSnapshot{Name: "ping cloudflare", Type: "ping", Status: "failed"},
			Status: "failed",
			Finding: &watch.Finding{
				Target:   "SHIZK RADIO",
				Check:    "ping cloudflare",
				Metric:   "received",
				Observed: "0",
				Expected: "== 5",
				Message:  "constraint failed",
			},
		})
	}
	m.detailOpen = true

	frame := stripANSI(m.render())
	for _, want := range []string{
		"count=7  last=09:31:00  events=last=30m count=7 peak=3",
		"█",
		"30m ago",
		"now",
	} {
		if !strings.Contains(frame, want) {
			t.Fatalf("detail histogram missing %q:\n%s", want, frame)
		}
	}
	if strings.Contains(frame, "回数") || strings.Contains(frame, "時刻") || strings.Contains(frame, "·") {
		t.Fatalf("detail histogram should not render Japanese labels or dotted zero buckets:\n%s", frame)
	}
}

func TestFailedCheckDetailUsesDenseInvestigationLayout(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", []watch.Target{}, events)
	at := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	m.now = at.Add(40 * time.Second)
	for _, second := range []int{0, 20, 40} {
		m.apply(watch.Event{
			Time:   at.Add(time.Duration(second) * time.Second),
			Kind:   watch.EventFinding,
			Target: watch.TargetSnapshot{Name: "SHIZK RADIO"},
			Step:   watch.StepSnapshot{Name: "ping cloudflare"},
			Finding: &watch.Finding{
				Target:   "SHIZK RADIO",
				Check:    "ping cloudflare",
				Metric:   "received",
				Observed: "0",
				Expected: "== 5",
				Message:  "constraint failed",
			},
		})
	}

	view := stripANSI(m.failedCheckDetailView(80, 16))
	lines := strings.Split(view, "\n")
	if got, want := len(lines), 16; got != want {
		t.Fatalf("detail line count = %d, want %d:\n%s", got, want, view)
	}
	for _, want := range []string{
		"target=SHIZK RADIO  check=ping cloudflare  metric=received",
		"observed=0  expected=\"== 5\"  message=constraint failed",
		"count=3  last=09:30:40  events=last=30m count=3",
		"30m ago",
		"now",
		"recent failures",
		"time      round  check",
		"09:30:40",
		"logs",
		"kind=finding",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("dense detail view missing %q:\n%s", want, view)
		}
	}
}

func TestFailedCheckDetailLogsAreScopedToSelectedCheck(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", []watch.Target{}, events)
	at := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	target := watch.TargetSnapshot{Name: "radio-a", SSID: "Lab", Band: "5ghz"}
	m.apply(watch.Event{
		Time:   at,
		Kind:   watch.EventStepFinished,
		Round:  1,
		Target: target,
		Step:   watch.StepSnapshot{Name: "http cloudflare v6", Type: "http", Status: "ok"},
		Status: "ok",
	})
	m.apply(watch.Event{
		Time:   at.Add(time.Second),
		Kind:   watch.EventFinding,
		Round:  1,
		Target: target,
		Step:   watch.StepSnapshot{Name: "public ipv6", Type: "global_ip", Status: "failed"},
		Status: "failed",
		Finding: &watch.Finding{
			Target:   "radio-a",
			Check:    "public ipv6",
			Metric:   "ipv6_global_ips",
			Observed: "-",
			Expected: "cidr ::/0 mode=at_least",
			Message:  "constraint failed",
		},
	})
	m.apply(watch.Event{
		Time:   at.Add(2 * time.Second),
		Kind:   watch.EventStepFinished,
		Round:  1,
		Target: target,
		Step:   watch.StepSnapshot{Name: "disconnect", Type: "cleanup", Status: "ok"},
		Status: "ok",
	})

	view := stripANSI(m.failedCheckDetailView(120, 18))
	for _, want := range []string{"logs", "kind=finding", "step=\"public ipv6\"", "metric=ipv6_global_ips"} {
		if !strings.Contains(view, want) {
			t.Fatalf("selected check detail missing %q:\n%s", want, view)
		}
	}
	for _, unwanted := range []string{"http cloudflare v6", "disconnect"} {
		if strings.Contains(view, unwanted) {
			t.Fatalf("selected check detail should not include unrelated log %q:\n%s", unwanted, view)
		}
	}
}

func TestFailedCheckDetailLogsUseStructuredHistoryBeyondVisibleEventLog(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", []watch.Target{}, events)
	at := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	target := watch.TargetSnapshot{Name: "radio-a", SSID: "Lab", Band: "5ghz"}
	m.apply(watch.Event{
		Time:   at,
		Kind:   watch.EventFinding,
		Round:  1,
		Target: target,
		Step:   watch.StepSnapshot{Name: "public ipv6", Type: "global_ip", Status: "failed"},
		Status: "failed",
		Finding: &watch.Finding{
			Target:   "radio-a",
			Check:    "public ipv6",
			Metric:   "ipv6_global_ips",
			Observed: "-",
			Expected: "cidr ::/0 mode=at_least",
			Message:  "constraint failed",
		},
	})
	for i := 0; i < visibleEventLogLimit+25; i++ {
		m.apply(watch.Event{
			Time:    at.Add(time.Duration(i+1) * time.Second),
			Kind:    watch.EventLog,
			Message: fmt.Sprintf("unrelated log %03d", i),
		})
	}
	m.now = at.Add(time.Duration(visibleEventLogLimit+25) * time.Second)
	if strings.Contains(strings.Join(m.logs, "\n"), "kind=finding") {
		t.Fatalf("test setup expected selected finding to be outside the visible Event Log tail")
	}

	view := stripANSI(m.failedCheckDetailView(120, 18))
	for _, want := range []string{"logs", "kind=finding", "step=\"public ipv6\""} {
		if !strings.Contains(view, want) {
			t.Fatalf("detail should use structured history for %q:\n%s", want, view)
		}
	}
}

func TestDetailHistogramUsesSummarySparklineImplementation(t *testing.T) {
	now := time.Date(2026, 5, 16, 9, 35, 0, 0, time.UTC)
	histogram := recentEventHistogram([]time.Time{
		now.Add(-4 * time.Minute),
		now.Add(-2 * time.Minute),
		now.Add(-2 * time.Minute),
		now.Add(-10 * time.Second),
	}, 104, summarySparklineWindow, now)
	lines := renderDetailHistogram(histogram, 104, 8, failGraphStyle)

	if len(lines) != 8 {
		t.Fatalf("chart line count = %d, want 8", len(lines))
	}
	for i, line := range lines {
		stripped := stripANSI(line)
		if got := lipgloss.Width(stripped); got != 104 {
			t.Fatalf("chart line %d width = %d, want 104: %q", i, got, stripped)
		}
	}
	joined := stripANSI(strings.Join(lines, "\n"))
	for _, want := range []string{"30m ago", "now", "█"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("chart missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "09:") || strings.Contains(joined, "┬") || strings.Contains(joined, "回数") || strings.Contains(joined, "時刻") || strings.Contains(joined, "·") {
		t.Fatalf("detail chart should use the summary sparkline axis/rendering:\n%s", joined)
	}
}

func TestOccurrenceGraphHeightUsesHalfOfDetailContent(t *testing.T) {
	if got, want := occurrenceGraphHeight(18), 9; got != want {
		t.Fatalf("occurrenceGraphHeight(18) = %d, want %d", got, want)
	}
	if got, want := occurrenceGraphHeight(6), 3; got != want {
		t.Fatalf("occurrenceGraphHeight(6) = %d, want %d", got, want)
	}
}

func TestFailedCheckDetailModalUsesFixedAppRatio(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", []watch.Target{}, events)
	modal := m.failedCheckDetailModal(150, 32)

	if got, want := lipgloss.Width(modal), 135; got != want {
		t.Fatalf("modal width = %d, want %d", got, want)
	}
	if got, want := lipgloss.Height(modal), 17; got != want {
		t.Fatalf("modal height = %d, want %d", got, want)
	}
}

func TestDetailModalShowsExpandedLogRows(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", []watch.Target{}, events)
	at := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	target := watch.TargetSnapshot{Name: "radio-a", SSID: "Lab", Band: "5ghz"}
	for i := 0; i < 30; i++ {
		m.apply(watch.Event{
			Time:     at.Add(time.Duration(i) * time.Second),
			Kind:     watch.EventStepFinished,
			Round:    uint64(i + 1),
			Target:   target,
			Step:     watch.StepSnapshot{Name: "connect", Type: "connect", Operation: "wifi.connect", Status: "ok"},
			Status:   "ok",
			Duration: 100,
		})
	}
	m.focus = focusPassingChecks
	m.openDetailForPanel(focusPassingChecks)

	modal := stripANSI(m.passingCheckDetailModal(150, 60))
	if got := strings.Count(modal, "kind=step_finished"); got < 15 {
		t.Fatalf("expanded modal should show at least 15 related log rows, got %d:\n%s", got, modal)
	}
}

func TestTabFocusAndPanelLocalNavigation(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", []watch.Target{}, events)
	at := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	target := watch.TargetSnapshot{Name: "SHIZK RADIO", SSID: "SHIZK RADIO"}
	for i, step := range []string{"connect", "wait_connected"} {
		m.apply(watch.Event{
			Time:     at.Add(time.Duration(i) * time.Second),
			Kind:     watch.EventStepFinished,
			Round:    1,
			Target:   target,
			Step:     watch.StepSnapshot{Name: step, Type: step, Status: "ok"},
			Status:   "ok",
			Duration: 10,
		})
	}
	for i, check := range []string{"ping cloudflare", "dns cloudflare"} {
		m.apply(watch.Event{
			Time:   at.Add(time.Duration(i+2) * time.Second),
			Kind:   watch.EventFinding,
			Round:  1,
			Target: target,
			Step:   watch.StepSnapshot{Name: check, Type: "probe", Status: "failed"},
			Status: "failed",
			Finding: &watch.Finding{
				Target:   "SHIZK RADIO",
				Check:    check,
				Metric:   "status",
				Observed: "failed",
				Expected: "== ok",
				Message:  "constraint failed",
			},
		})
	}
	m.failedCheckCursor = 0

	m = updateKey(t, m, tea.Key{Code: 'j', Text: "j"})
	if m.focus != focusFailedChecks || m.failedCheckCursor != 1 || m.passingCheckCursor != 0 {
		t.Fatalf("j should move failed checks cursor only: focus=%v passing=%d failed=%d", m.focus, m.passingCheckCursor, m.failedCheckCursor)
	}
	m = updateKey(t, m, tea.Key{Code: tea.KeyTab})
	if m.focus != focusPassingChecks {
		t.Fatalf("tab should focus passing checks, got %v", m.focus)
	}
	m = updateKey(t, m, tea.Key{Code: 'j', Text: "j"})
	if m.passingCheckCursor != 1 || m.failedCheckCursor != 1 {
		t.Fatalf("j should move passing checks cursor only: passing=%d failed=%d", m.passingCheckCursor, m.failedCheckCursor)
	}
	m = updateKey(t, m, tea.Key{Code: 'u', Mod: tea.ModCtrl})
	if m.passingCheckCursor != 0 {
		t.Fatalf("ctrl-u should page passing checks cursor upward, got %d", m.passingCheckCursor)
	}
	m = updateKey(t, m, tea.Key{Code: tea.KeyTab})
	m = updateKey(t, m, tea.Key{Code: 'k', Text: "k"})
	if m.focus != focusFailedChecks || m.failedCheckCursor != 0 {
		t.Fatalf("k should move failed checks cursor after focus returns: focus=%v failed=%d", m.focus, m.failedCheckCursor)
	}
}

func TestSlashFilterAppliesToPassingAndFailedChecks(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", []watch.Target{}, events)
	m.width = 150
	m.height = 34
	at := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	target := watch.TargetSnapshot{Name: "SHIZK RADIO", SSID: "SHIZK RADIO"}
	m.apply(watch.Event{
		Time:     at,
		Kind:     watch.EventStepFinished,
		Round:    1,
		Target:   target,
		Step:     watch.StepSnapshot{Name: "connect", Type: "connect", Status: "ok"},
		Status:   "ok",
		Duration: 10,
	})
	m.apply(watch.Event{
		Time:   at.Add(time.Second),
		Kind:   watch.EventFinding,
		Round:  1,
		Target: target,
		Step:   watch.StepSnapshot{Name: "ping cloudflare", Type: "ping", Status: "failed"},
		Status: "failed",
		Finding: &watch.Finding{
			Target:   "SHIZK RADIO",
			Check:    "ping cloudflare",
			Metric:   "received",
			Observed: "0",
			Expected: "== 5",
			Message:  "constraint failed",
		},
	})

	m = updateKey(t, m, tea.Key{Code: '/', Text: "/"})
	for _, ch := range "ping" {
		m = updateKey(t, m, tea.Key{Code: ch, Text: string(ch)})
	}
	m = updateKey(t, m, tea.Key{Code: tea.KeyEnter})

	if m.searchEditing {
		t.Fatalf("enter should apply filter editing")
	}
	if got := len(m.filteredPassingCheckSummaries()); got != 0 {
		t.Fatalf("filtered passing check count = %d, want 0", got)
	}
	if got := len(m.filteredFailedCheckSummaries()); got != 1 {
		t.Fatalf("filtered failed check count = %d, want 1", got)
	}
	frame := stripANSI(m.render())
	for _, want := range []string{"filter=/ping", "Esc=Clear", "no passing checks match", "ping cloudflare"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("filtered frame missing %q:\n%s", want, frame)
		}
	}

	m = updateKey(t, m, tea.Key{Code: tea.KeyEscape})
	if m.hasSearchFilter() || len(m.filteredPassingCheckSummaries()) != 1 {
		t.Fatalf("esc should clear applied filter: query=%q passingChecks=%d", m.searchQuery, len(m.filteredPassingCheckSummaries()))
	}
}

func TestRenderLetsSummaryPanelsAbsorbRoundTimelineAndCheckStatusGrowth(t *testing.T) {
	fewSteps := renderWatchFrameWithSteps(t, []watch.StepSnapshot{
		{Name: "connect", Type: "connect", Status: "running"},
	})
	manySteps := renderWatchFrameWithSteps(t, []watch.StepSnapshot{
		{Name: "connect", Type: "connect", Status: "ok"},
		{Name: "wait_connected", Type: "wait_connected", Status: "ok"},
		{Name: "ip provisioning", Type: "ip_status", Status: "ok"},
		{Name: "ping cloudflare", Type: "ping", Status: "running"},
	})

	fewIndex := lineIndex(fewSteps, "┌Failed Checks")
	manyIndex := lineIndex(manySteps, "┌Failed Checks")
	if fewIndex < 0 || manyIndex < 0 {
		t.Fatalf("missing Failed Checks panel\nfew:\n%s\nmany:\n%s", fewSteps, manySteps)
	}
	if manyIndex <= fewIndex {
		t.Fatalf("Failed Checks panel should move down when Check Status grows: few=%d many=%d\nfew:\n%s\nmany:\n%s", fewIndex, manyIndex, fewSteps, manySteps)
	}
	fewEventLogIndex := lineIndex(fewSteps, "┌Event Log")
	manyEventLogIndex := lineIndex(manySteps, "┌Event Log")
	if fewEventLogIndex < 0 || manyEventLogIndex < 0 {
		t.Fatalf("missing Event Log panel\nfew:\n%s\nmany:\n%s", fewSteps, manySteps)
	}
	if fewEventLogIndex != manyEventLogIndex {
		t.Fatalf("Event Log panel should stay anchored while summaries absorb height changes: few=%d many=%d\nfew:\n%s\nmany:\n%s", fewEventLogIndex, manyEventLogIndex, fewSteps, manySteps)
	}
}

func TestRenderStylesSpacerRuns(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", []watch.Target{
		{Name: "shownet-6g-ap1", SSID: "ShowNet", Band: "6ghz"},
		{Name: "shownet-5g-any", SSID: "ShowNet", Band: "5ghz"},
	}, events)
	m.width = 140
	m.height = 30
	m.apply(watch.Event{Kind: watch.EventWatchStarted, Message: "watch started"})

	raw := m.render()
	if match := unstyledSpacerPattern.FindString(raw); match != "" {
		t.Fatalf("rendered frame contains an unstyled spacer run after SGR reset: %q", match)
	}
}

func TestPanelRowsCarryInvisibleAnchors(t *testing.T) {
	panel := renderPanel("Run Queue", 24, 5, "same\nsame\nsame")
	lines := strings.Split(panel, "\n")
	if len(lines) != 5 {
		t.Fatalf("panel line count = %d, want 5: %q", len(lines), panel)
	}
	if stripANSI(lines[1]) != stripANSI(lines[2]) {
		t.Fatalf("anchors should not change visible text:\n%q\n%q", stripANSI(lines[1]), stripANSI(lines[2]))
	}
	if lines[1] == lines[2] {
		t.Fatalf("adjacent identical visible rows should differ in raw ANSI to defeat hard-scroll matching:\n%s", panel)
	}
}

func TestVerticalSpacerStylesEveryRow(t *testing.T) {
	spacer := verticalSpacer(4)
	lines := strings.Split(spacer, "\n")
	if len(lines) != 4 {
		t.Fatalf("verticalSpacer line count = %d, want 4: %q", len(lines), spacer)
	}
	for i, line := range lines {
		if got := stripANSI(line); got != " " {
			t.Fatalf("verticalSpacer line %d visible content = %q, want one space", i, got)
		}
		if line == " " {
			t.Fatalf("verticalSpacer line %d is unstyled", i)
		}
	}
}

func TestSummaryPanelsUseOrangePalette(t *testing.T) {
	now := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	assertStyleRGB(t, summaryPanelRowStyle(now, now), 0xff, 0xaa, 0x44)
	assertStyleRGB(t, summaryPanelRowStyle(now.Add(-recencyFreshWindow-time.Second), now), 0xee, 0x88, 0x22)
	assertStyleRGB(t, summaryPanelRowStyle(now.Add(-recencyWarmWindow-time.Second), now), 0xb4, 0x65, 0x22)
	assertStyleRGB(t, summaryGraphStyle, 0xff, 0x9f, 0x1c)
	assertStyleRGB(t, summaryTableHeaderStyle, 0xff, 0xaa, 0x44)
}

func TestTickAdvancesSummaryRecencyClock(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", []watch.Target{}, events)
	at := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	m.now = at
	m.apply(watch.Event{
		Time:     at,
		Kind:     watch.EventStepFinished,
		Round:    1,
		Target:   watch.TargetSnapshot{Name: "SHIZK RADIO", SSID: "SHIZK RADIO"},
		Step:     watch.StepSnapshot{Name: "connect", Type: "connect", Status: "ok"},
		Status:   "ok",
		Duration: 42,
	})

	updated, _ := m.Update(tickMsg(at.Add(recencyWarmWindow + time.Second)))
	m = updated.(model)
	if !m.now.Equal(at.Add(recencyWarmWindow + time.Second)) {
		t.Fatalf("tick should update model clock, got %s", m.now)
	}
	rows := m.passingCheckSummaries()
	if len(rows) != 1 {
		t.Fatalf("passing check summaries = %d, want 1", len(rows))
	}
	assertStyleRGB(t, summaryPanelRowStyle(rows[0].last, m.currentTime()), 0xb4, 0x65, 0x22)
}

func TestRunQueueRowsUseStatusPalette(t *testing.T) {
	assertStyleRGB(t, runQueueRowStyle("failed"), 0xff, 0x6b, 0x4a)
	if !runQueueRowStyle("failed").GetBold() {
		t.Fatalf("failed run queue rows should be bold")
	}
	assertStyleRGB(t, runQueueRowStyle("running"), 0xff, 0xd1, 0x66)
	if !runQueueRowStyle("running").GetBold() {
		t.Fatalf("running run queue rows should be bold")
	}
	assertStyleRGB(t, runQueueRowStyle("ok"), 0x86, 0xc7, 0x79)
	if runQueueRowStyle("ok").GetBold() {
		t.Fatalf("ok run queue rows should use a calmer non-bold green")
	}
	assertStyleRGB(t, runQueueRowStyle("pending"), 0x9b, 0x6a, 0x43)
	assertStyleRGB(t, runQueueRowStyle("skipped"), 0xd2, 0x8a, 0x3c)
}

func TestHistoricalCheckStatusKeepsOutcomeHue(t *testing.T) {
	assertStyleRGB(t, staleStatusStyle("ok"), 0x3c, 0x9d, 0x62)
	assertStyleRGB(t, staleStatusStyle("failed"), 0xb4, 0x5a, 0x38)
	assertStyleRGB(t, staleStatusStyle("skipped"), 0x9c, 0x64, 0x36)
}

func renderWatchFrameWithSteps(t *testing.T, steps []watch.StepSnapshot) string {
	t.Helper()
	events := make(chan watch.Event)
	m := newModel("shownet-watch", []watch.Target{
		{Name: "shownet-6g-ap1", SSID: "ShowNet", Band: "6ghz"},
	}, events)
	m.width = 140
	m.height = 30
	m.apply(watch.Event{Kind: watch.EventRoundStarted, Round: 1})
	m.apply(watch.Event{
		Kind:   watch.EventTargetStarted,
		Round:  1,
		Target: watch.TargetSnapshot{Name: "shownet-6g-ap1", SSID: "ShowNet", Band: "6ghz"},
	})
	for _, step := range steps {
		step := step
		kind := watch.EventStepFinished
		if step.Status == "running" {
			kind = watch.EventStepStarted
		}
		m.apply(watch.Event{
			Kind:   kind,
			Round:  1,
			Target: watch.TargetSnapshot{Name: "shownet-6g-ap1", SSID: "ShowNet", Band: "6ghz"},
			Step:   step,
		})
	}
	return stripANSI(m.render())
}

func targetSnapshot(name string) watch.TargetSnapshot {
	return watch.TargetSnapshot{Name: name, SSID: name}
}

func runQueueTexts(lines []runQueueLine) []string {
	texts := make([]string, 0, len(lines))
	for _, line := range lines {
		texts = append(texts, line.text)
	}
	return texts
}

func assertStyleRGB(t *testing.T, style lipgloss.Style, r uint32, g uint32, b uint32) {
	t.Helper()
	gotR, gotG, gotB, _ := style.GetForeground().RGBA()
	if gotR>>8 != r || gotG>>8 != g || gotB>>8 != b {
		t.Fatalf("style fg = #%02x%02x%02x, want #%02x%02x%02x", gotR>>8, gotG>>8, gotB>>8, r, g, b)
	}
}

func updateKey(t *testing.T, m model, key tea.Key) model {
	t.Helper()
	updated, _ := m.Update(tea.KeyPressMsg(key))
	next, ok := updated.(model)
	if !ok {
		t.Fatalf("updated model type = %T, want tui.model", updated)
	}
	return next
}

func lineIndex(text string, needle string) int {
	for i, line := range strings.Split(text, "\n") {
		if strings.Contains(line, needle) {
			return i
		}
	}
	return -1
}

func panelTopWidth(text string, title string) int {
	for _, line := range strings.Split(text, "\n") {
		index := strings.Index(line, "┌"+title)
		if index < 0 {
			continue
		}
		rest := line[index:]
		end := strings.Index(rest, "┐")
		if end < 0 {
			return 0
		}
		return lipgloss.Width(rest[:end+len("┐")])
	}
	return 0
}

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)
var unstyledSpacerPattern = regexp.MustCompile(`\x1b\[m +(\x1b|\n|$)`)

func stripANSI(value string) string {
	return ansiPattern.ReplaceAllString(value, "")
}
