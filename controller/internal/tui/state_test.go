package tui

import (
	"strings"
	"testing"
	"time"

	"dropcheck/controller/internal/watch"
)

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
	if rows := m.passingCheckSummaries(); len(rows) != 1 || rows[0].Count != 1 {
		t.Fatalf("passingChecks should aggregate successful agents into count, got %#v", rows)
	}
	if rows := m.failedCheckSummaries(); len(rows) != 1 || rows[0].Count != 1 {
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
	if len(rows) != 1 || rows[0].Count != 2 {
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
		t.Fatalf("previous round setup should record a passing check First:\n%s", checkStatus)
	}

	m.apply(watch.Event{Kind: watch.EventRoundStarted, Round: 2, Status: "running"})
	checkStatus := stripANSI(m.checkStatusView(72, 4))
	if !strings.Contains(checkStatus, "PASS") || strings.Contains(checkStatus, "WAIT") {
		t.Fatalf("new round should keep previous result token instead of replacing it with WAIT:\n%s", checkStatus)
	}
	cell := m.checkStatusTargetCell("connect", target, []watch.AgentSnapshot{{}})
	if cell.Status != "ok" || !cell.Stale {
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
	if cell.Status != "failed" || !cell.Stale {
		t.Fatalf("new round checkStatus cell = %#v, want stale failed", cell)
	}
}
