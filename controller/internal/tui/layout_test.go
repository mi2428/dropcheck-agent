package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"dropcheck/controller/internal/watch"

	tea "charm.land/bubbletea/v2"
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
	if !strings.Contains(frame, "└── RUN  Connect") {
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
