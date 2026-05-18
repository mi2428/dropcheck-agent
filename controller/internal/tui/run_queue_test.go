package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"dropcheck/controller/internal/watch"
)

func TestRunQueueTreeExpandsOnlyRunningTargets(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", []watch.Target{}, events)
	m.Targets = []targetState{
		{
			Target: targetSnapshot("done-target"),
			Status: "ok",
			Steps:  []stepState{{Name: "connect", Status: "ok"}},
			PlannedSteps: []stepState{
				{Name: "connect", Type: "connect"},
				{Name: "wait_connected", Type: "wait_connected"},
			},
		},
		{
			Target:      targetSnapshot("running-target"),
			Status:      "running",
			CurrentStep: "ping cloudflare",
			PlannedSteps: []stepState{
				{Name: "connect", Type: "connect"},
				{Name: "wait_connected", Type: "wait_connected"},
				{Name: "ping cloudflare", Type: "ping"},
				{Name: "download", Type: "download"},
			},
			Steps: []stepState{
				{Name: "connect", Status: "ok"},
				{Name: "ping cloudflare", Status: "running"},
			},
		},
		{
			Target: targetSnapshot("waiting-target"),
			Status: "pending",
			PlannedSteps: []stepState{
				{Name: "connect", Type: "connect"},
				{Name: "wait_connected", Type: "wait_connected"},
			},
		},
	}

	lines := m.runQueueTreeLines(80)
	text := stripANSI(strings.Join(runQueueTexts(lines), "\n"))
	for _, want := range []string{
		"OK   done-target",
		"RUN  running-target",
		"  ├── OK   Connect",
		"  ├── WAIT Wait Connected",
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

func TestRunQueueTreePrioritizesActiveStepsWhenClipped(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", []watch.Target{}, events)
	for i := range 6 {
		m.Targets = append(m.Targets, targetState{
			Target: targetSnapshot(fmt.Sprintf("done-%02d", i)),
			Status: "ok",
		})
	}
	m.Targets = append(m.Targets, targetState{
		Target:      targetSnapshot("running-target"),
		Status:      "running",
		CurrentStep: "download",
		PlannedSteps: []stepState{
			{Name: "connect", Type: "connect"},
			{Name: "wait_connected", Type: "wait_connected"},
			{Name: "ip provisioning", Type: "ip_status"},
			{Name: "ping cf ipv4", Type: "ping"},
			{Name: "download", Type: "download"},
		},
		Steps: []stepState{
			{Name: "connect", Status: "ok"},
			{Name: "wait_connected", Status: "ok"},
			{Name: "ip provisioning", Status: "ok"},
			{Name: "ping cf ipv4", Status: "ok"},
			{Name: "download", Status: "running"},
		},
	})
	for i := range 3 {
		m.Targets = append(m.Targets, targetState{
			Target: targetSnapshot(fmt.Sprintf("waiting-%02d", i)),
			Status: "pending",
		})
	}

	text := stripANSI(m.runQueueTreeView(80, 7))
	for _, want := range []string{
		"OK   done-05",
		"RUN  running-target",
		"├── OK   Connect",
		"├── OK   Wait Connected",
		"├── OK   ip provisioning",
		"├── OK   ping cf ipv4",
		"└── RUN  download",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("compact run queue missing %q:\n%s", want, text)
		}
	}
	for _, unwanted := range []string{"done-00", "done-04", "waiting-00"} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("compact run queue should prioritize active child rows over %q:\n%s", unwanted, text)
		}
	}
}

func TestRunQueueCursorStaysOnChildBetweenStepFinishAndNextStart(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", []watch.Target{}, events)
	m.Targets = []targetState{{
		Target:      targetSnapshot("running-target"),
		Status:      "running",
		CurrentStep: "ping cloudflare",
		PlannedSteps: []stepState{
			{Name: "connect", Type: "connect"},
			{Name: "ping cloudflare", Type: "ping"},
			{Name: "disconnect", Type: "cleanup"},
		},
		Steps: []stepState{
			{Name: "connect", Status: "ok"},
			{Name: "ping cloudflare", Status: "running"},
		},
	}}
	m.updateRunQueueCursor()
	before := m.runQueueCursor

	m.Targets[0].CurrentStep = ""
	m.Targets[0].Steps[1].Status = "ok"
	m.updateRunQueueCursor()

	if before != 2 || m.runQueueCursor != 2 {
		t.Fatalf("cursor should stay on the just-finished child row, before=%d after=%d", before, m.runQueueCursor)
	}
	lines := m.runQueueTreeLines(80)
	if lines[0].Current {
		t.Fatalf("parent row should not become current between step finish and next start: %#v", lines)
	}
	if !lines[2].Current || !strings.Contains(stripANSI(lines[2].Text), "OK   ping cloudflare") {
		t.Fatalf("just-finished child row should remain Current: %#v", lines)
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
	m.width = 120
	m.height = 30
	m.Targets[0].Status = "running"
	m.Targets[0].CurrentStep = "connect"
	m.Targets[0].Steps = []stepState{{Name: "connect", Status: "running"}}
	m.Targets[1].Status = "pending"

	text := stripANSI(m.runQueuePanelsView(48, 16))
	for _, want := range []string{"┌Run Queue pixel-a", "┌Run Queue pixel-b", "RUN  u7-5ghz", "├── RUN  Connect", "WAIT u7-5ghz", "└── WAIT Disconnect"} {
		if !strings.Contains(text, want) {
			t.Fatalf("split run queue panel missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "pixel-a u7-5ghz") || strings.Contains(text, "pixel-b u7-5ghz") {
		t.Fatalf("agent-scoped run queue panel should not repeat agent names on target Rows:\n%s", text)
	}
	if strings.Contains(text, "WAIT Connect") {
		t.Fatalf("waiting agent target should not expand child Rows:\n%s", text)
	}
	if strings.Contains(text, "▁") || strings.Contains(text, "█") || strings.Contains(text, "▌") {
		t.Fatalf("split run queue panel should not render outcome bars:\n%s", text)
	}

	m.focus = focusRunQueue
	m.focusRunQueueAgentKey = roundAgentKey(agents[0])
	focusedText := stripANSI(m.runQueuePanelsView(48, 16))
	for _, want := range []string{"┌Run Queue pixel-a", "┌Run Queue pixel-b"} {
		if !strings.Contains(focusedText, want) {
			t.Fatalf("focused split run queue panel should keep agent panels, missing %q:\n%s", want, focusedText)
		}
	}
	if count := strings.Count(focusedText, "Run Queue pixel-"); count != 2 {
		t.Fatalf("focused run queue should not collapse split panels, got %d titles:\n%s", count, focusedText)
	}
}

func TestRunQueueTreeKeepsScrollAnchorWhenNoTargetIsActive(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", []watch.Target{}, events)
	m.height = 9
	for i := range 16 {
		status := "pending"
		if i == 12 {
			status = "running"
		}
		m.Targets = append(m.Targets, targetState{
			Target: targetSnapshot(fmt.Sprintf("target-%02d", i)),
			Status: status,
			Steps:  []stepState{{Name: "connect", Status: "ok"}},
		})
	}
	m.updateRunQueueCursor()
	running := stripANSI(m.runQueueTreeView(80, 5))
	if !strings.Contains(running, "target-12") {
		t.Fatalf("running target should be visible:\n%s", running)
	}

	m.Targets[12].Status = "ok"
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
	for i := range 12 {
		m.Targets = append(m.Targets, targetState{
			Target: targetSnapshot(fmt.Sprintf("target-%02d", i)),
			Status: "pending",
		})
	}
	m.Targets[4].Status = "running"
	m.updateRunQueueCursor()
	if m.runQueueOffset != 0 {
		t.Fatalf("visible active row should not recenter run queue offset, got %d", m.runQueueOffset)
	}

	m.Targets[4].Status = "ok"
	m.Targets[6].Status = "running"
	m.updateRunQueueCursor()
	if m.runQueueOffset != 2 {
		t.Fatalf("offset should move only enough to reveal active row, got %d", m.runQueueOffset)
	}
}
