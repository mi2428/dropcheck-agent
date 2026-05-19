package tui

import (
	"fmt"
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
		"Run Queue: pixel-a",
		"Run Queue: pixel-b",
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

func TestPassingCheckPanelShowsDeviceColumn(t *testing.T) {
	events := make(chan watch.Event)
	agents := []watch.AgentSnapshot{
		{ID: "agent-a", Name: "35251JEHN00258", ADBSerial: "35251JEHN00258", DeviceModel: "Pixel 7a"},
		{ID: "agent-b", Name: "45240DLAQ007HG", ADBSerial: "45240DLAQ007HG", DeviceModel: "Pixel 9"},
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
	if len(rows) != 2 || rows[0].Count != 1 || rows[1].Count != 1 {
		t.Fatalf("passingChecks should keep device-labelled rows separate, got %#v", rows)
	}
	table := stripANSI(m.passingChecksView(96, 8))
	for _, want := range []string{"Device", "Pixel 7a", "Pixel 9"} {
		if !strings.Contains(table, want) {
			t.Fatalf("passingChecks table should render %q:\n%s", want, table)
		}
	}
	if strings.Contains(table, "35251JEHN00258") || strings.Contains(table, "45240DLAQ007HG") {
		t.Fatalf("passingChecks table should use device names without serials:\n%s", table)
	}
}

func TestFailedCheckPanelShowsDeviceColumn(t *testing.T) {
	events := make(chan watch.Event)
	agents := []watch.AgentSnapshot{
		{ID: "agent-a", Name: "35251JEHN00258", ADBSerial: "35251JEHN00258", DeviceModel: "Pixel 7a"},
		{ID: "agent-b", Name: "45240DLAQ007HG", ADBSerial: "45240DLAQ007HG", DeviceModel: "Pixel 9"},
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
				Metric:   "status",
				Observed: "failed",
				Expected: "== ok",
				Message:  "ping failed",
			},
		})
	}

	rows := m.failedCheckSummaries()
	if len(rows) != 2 || rows[0].Count != 1 || rows[1].Count != 1 {
		t.Fatalf("failedChecks should keep device-labelled rows separate, got %#v", rows)
	}
	table := stripANSI(m.failedChecksView(112, 8))
	for _, want := range []string{"Device", "Pixel 7a", "Pixel 9"} {
		if !strings.Contains(table, want) {
			t.Fatalf("failedChecks table should render %q:\n%s", want, table)
		}
	}
	if strings.Contains(table, "35251JEHN00258") || strings.Contains(table, "45240DLAQ007HG") {
		t.Fatalf("failedChecks table should use device names without serials:\n%s", table)
	}
}

func TestCheckStatusShowsCurrentRoundPendingBeforeHistory(t *testing.T) {
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
	if !strings.Contains(checkStatus, "WAIT") || strings.Contains(checkStatus, "PASS") {
		t.Fatalf("new round should show pending current-round check instead of stale history:\n%s", checkStatus)
	}
	cell := m.checkStatusTargetCell("connect", target, []watch.AgentSnapshot{{}})
	if cell.Status != "pending" || cell.Stale {
		t.Fatalf("new round checkStatus cell = %#v, want current pending", cell)
	}
}

func TestCheckStatusFutureTargetsDoNotShowHistoricalPassDuringRound(t *testing.T) {
	events := make(chan watch.Event)
	targets := []watch.Target{
		{Name: "cs21(5G)", ShortName: "C21_5", SSID: "Lab"},
		{Name: "ub1(5G)", ShortName: "U1_5", SSID: "Lab"},
	}
	m := newModelWithChecks("shownet-watch", targets, []watch.Check{{Name: "connect", Type: "connect"}}, events)
	future := watch.TargetSnapshot{Name: "ub1(5G)", ShortName: "U1_5", SSID: "Lab"}
	m.apply(watch.Event{
		Time:     time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC),
		Kind:     watch.EventStepFinished,
		Round:    1,
		Target:   future,
		Step:     watch.StepSnapshot{Name: "connect", Type: "connect", Status: "ok"},
		Status:   "ok",
		Duration: 42,
	})
	m.apply(watch.Event{Kind: watch.EventRoundStarted, Round: 2, Status: "running"})
	m.apply(watch.Event{
		Kind:   watch.EventStepStarted,
		Round:  2,
		Target: watch.TargetSnapshot{Name: "cs21(5G)", ShortName: "C21_5", SSID: "Lab"},
		Step:   watch.StepSnapshot{Name: "connect", Type: "connect", Status: "running"},
		Status: "running",
	})

	cell := m.checkStatusTargetCell("connect", future, []watch.AgentSnapshot{{}})
	if cell.Status != "pending" || cell.Stale {
		t.Fatalf("future current-round target should stay pending instead of showing stale pass: %#v", cell)
	}
	checkStatus := stripANSI(m.checkStatusView(72, 3))
	if strings.Contains(checkStatus, "PASS") {
		t.Fatalf("future current-round target should not render historical PASS:\n%s", checkStatus)
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

func TestOperatorSkippedStepRendersPendingInCheckStatus(t *testing.T) {
	events := make(chan watch.Event)
	target := watch.TargetSnapshot{Name: "u7-5ghz", SSID: "SHIZK RADIO"}
	m := newModelWithChecks("shownet-watch", []watch.Target{{Name: "u7-5ghz", SSID: "SHIZK RADIO"}}, []watch.Check{{Name: "ping cloudflare", Type: "ping"}}, events)
	m.apply(watch.Event{
		Kind:   watch.EventTargetStarted,
		Round:  1,
		Target: target,
		Status: "running",
	})
	m.apply(watch.Event{
		Kind:   watch.EventStepStarted,
		Round:  1,
		Target: target,
		Step:   watch.StepSnapshot{Name: "ping cloudflare", Type: "ping", Status: "running"},
		Status: "running",
	})
	m.apply(watch.Event{
		Kind:   watch.EventStepFinished,
		Round:  1,
		Target: target,
		Step:   watch.StepSnapshot{Name: "ping cloudflare", Type: "ping", Status: "pending", Skipped: true, Message: "skipped by operator"},
		Status: "skipped",
	})
	m.apply(watch.Event{
		Kind:   watch.EventTargetFinished,
		Round:  1,
		Target: target,
		Status: "skipped",
	})

	checkStatus := stripANSI(m.checkStatusView(72, 6))
	if strings.Contains(checkStatus, "SKIP") {
		t.Fatalf("operator-skipped check should not render SKIP in Latest Check Results:\n%s", checkStatus)
	}
	if !strings.Contains(checkStatus, "WAIT") {
		t.Fatalf("operator-skipped check should render as pending in Latest Check Results:\n%s", checkStatus)
	}
}

func TestCheckStatusShortNameModeCompactsHeadersAndTokens(t *testing.T) {
	events := make(chan watch.Event)
	agents := []watch.AgentSnapshot{
		{ID: "agent-a", Name: "pixel-a"},
		{ID: "agent-b", Name: "pixel-b"},
	}
	targets := []watch.Target{
		{Name: "cs1(5G)", ShortName: "C1_5", SSID: "SHIZK RADIO"},
		{Name: "cs2(5G)", ShortName: "C2_5", SSID: "SHIZK RADIO"},
		{Name: "cs3(5G)", ShortName: "C3_5", SSID: "SHIZK RADIO"},
		{Name: "cs4(5G)", ShortName: "C4_5", SSID: "SHIZK RADIO"},
		{Name: "cs5(5G)", ShortName: "C5_5", SSID: "SHIZK RADIO"},
		{Name: "cs6(5G)", ShortName: "C6_5", SSID: "SHIZK RADIO"},
	}
	m := newModelWithChecks("shownet-watch", targets, []watch.Check{{Name: "connect", Type: "connect"}}, events, agents)
	m.apply(watch.Event{
		Kind:     watch.EventStepFinished,
		Agent:    agents[0],
		Round:    1,
		Target:   watch.TargetSnapshot{Name: "cs1(5G)", ShortName: "C1_5", SSID: "SHIZK RADIO"},
		Step:     watch.StepSnapshot{Name: "connect", Type: "connect", Status: "ok"},
		Status:   "ok",
		Duration: 10,
	})
	m.apply(watch.Event{
		Kind:   watch.EventFinding,
		Agent:  agents[0],
		Round:  1,
		Target: watch.TargetSnapshot{Name: "cs2(5G)", ShortName: "C2_5", SSID: "SHIZK RADIO"},
		Step:   watch.StepSnapshot{Name: "connect", Type: "connect", Status: "failed"},
		Status: "failed",
		Finding: &watch.Finding{
			Target: "cs2(5G)",
			Check:  "connect",
			Metric: "status",
		},
	})
	m.apply(watch.Event{
		Kind:   watch.EventStepStarted,
		Agent:  agents[0],
		Round:  1,
		Target: watch.TargetSnapshot{Name: "cs3(5G)", ShortName: "C3_5", SSID: "SHIZK RADIO"},
		Step:   watch.StepSnapshot{Name: "connect", Type: "connect", Status: "running"},
		Status: "running",
	})

	checkStatus := stripANSI(m.checkStatusView(58, 3))
	for _, want := range []string{"C1_5", "C2_5", "C3_5", "C4_5", "C5_5", "C6_5", "OK1/2", "NG1/2", "RUN1/2"} {
		if !strings.Contains(checkStatus, want) {
			t.Fatalf("short check status missing %q:\n%s", want, checkStatus)
		}
	}
	for _, notWant := range []string{"cs1(5G)", "PASS", "FAIL", "RUN("} {
		if strings.Contains(checkStatus, notWant) {
			t.Fatalf("short check status should not contain %q:\n%s", notWant, checkStatus)
		}
	}
}

func TestCheckStatusCompactTokenUsesPlannedAgentProgress(t *testing.T) {
	tests := []struct {
		name string
		cell checkStatusAggregate
		want string
	}{
		{name: "assigned pass is complete", cell: checkStatusAggregate{Status: "ok", Count: 1, Total: 1}, want: "PASS"},
		{name: "assigned fail is complete", cell: checkStatusAggregate{Status: "failed", Count: 1, Failed: 1, Total: 1}, want: "FAIL"},
		{name: "partial pass is intermediate", cell: checkStatusAggregate{Status: "ok", Count: 1, Total: 2}, want: "OK1/2"},
		{name: "partial fail is intermediate", cell: checkStatusAggregate{Status: "failed", Count: 1, Failed: 1, Total: 2}, want: "NG1/2"},
		{name: "all pass is complete", cell: checkStatusAggregate{Status: "ok", Count: 2, Total: 2}, want: "PASS"},
		{name: "second agent reached running step", cell: checkStatusAggregate{Status: "running", Count: 2, Total: 2}, want: "RUN2/2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := checkStatusAggregateToken(tt.cell, true, true)
			if token != tt.want {
				t.Fatalf("token = %q, want %q", token, tt.want)
			}
		})
	}
}

func TestCheckStatusUsesContiguousInitialWindow(t *testing.T) {
	events := make(chan watch.Event)
	agents := []watch.AgentSnapshot{{ID: "agent-a", Name: "pixel-a"}}
	disconnectAfter := false
	targets := []watch.Target{
		{Name: "ap1(5G)", ShortName: "A1", SSID: "Lab", DisconnectAfter: &disconnectAfter},
		{Name: "ap2(5G)", ShortName: "A2", SSID: "Lab", DisconnectAfter: &disconnectAfter},
		{Name: "ap3(5G)", ShortName: "A3", SSID: "Lab", DisconnectAfter: &disconnectAfter},
		{Name: "ap4(5G)", ShortName: "A4", SSID: "Lab", DisconnectAfter: &disconnectAfter},
		{Name: "ap5(5G)", ShortName: "A5", SSID: "Lab", DisconnectAfter: &disconnectAfter},
		{Name: "ap6(5G)", ShortName: "A6", SSID: "Lab", DisconnectAfter: &disconnectAfter},
		{Name: "ap7(5G)", ShortName: "A7", SSID: "Lab", DisconnectAfter: &disconnectAfter},
		{Name: "ap8(5G)", ShortName: "A8", SSID: "Lab", DisconnectAfter: &disconnectAfter},
	}
	m := newModelWithChecks("shownet-watch", targets, []watch.Check{}, events, agents)
	for i := 0; i < 6; i++ {
		target := watch.TargetSnapshot{Name: targets[i].Name, ShortName: targets[i].ShortName, SSID: targets[i].SSID}
		for _, step := range []string{"connect", "wait_connected"} {
			m.apply(watch.Event{
				Kind:     watch.EventStepFinished,
				Agent:    agents[0],
				Round:    1,
				Target:   target,
				Step:     watch.StepSnapshot{Name: step, Type: step, Status: "ok"},
				Status:   "ok",
				Duration: 10,
			})
		}
	}

	checkStatus := stripANSI(m.checkStatusView(52, 3))
	for _, want := range []string{"A1", "A2", "A3", "A4", "A5", "A6", "A7"} {
		if !strings.Contains(checkStatus, want) {
			t.Fatalf("clipped check status should keep a contiguous prefix window %q:\n%s", want, checkStatus)
		}
	}
	if strings.Contains(checkStatus, "A8") {
		t.Fatalf("clipped check status should not splice waiting targets into the visible window:\n%s", checkStatus)
	}
}

func TestCheckStatusAutoWindowTracksRunningTargetContiguously(t *testing.T) {
	events := make(chan watch.Event)
	agents := []watch.AgentSnapshot{{ID: "agent-a", Name: "pixel-a"}}
	disconnectAfter := false
	targets := []watch.Target{
		{Name: "ap1(5G)", ShortName: "A1", SSID: "Lab", DisconnectAfter: &disconnectAfter},
		{Name: "ap2(5G)", ShortName: "A2", SSID: "Lab", DisconnectAfter: &disconnectAfter},
		{Name: "ap3(5G)", ShortName: "A3", SSID: "Lab", DisconnectAfter: &disconnectAfter},
		{Name: "ap4(5G)", ShortName: "A4", SSID: "Lab", DisconnectAfter: &disconnectAfter},
		{Name: "ap5(5G)", ShortName: "A5", SSID: "Lab", DisconnectAfter: &disconnectAfter},
		{Name: "ap6(5G)", ShortName: "A6", SSID: "Lab", DisconnectAfter: &disconnectAfter},
	}
	m := newModelWithChecks("shownet-watch", targets, []watch.Check{}, events, agents)
	for i := 0; i < 4; i++ {
		target := watch.TargetSnapshot{Name: targets[i].Name, ShortName: targets[i].ShortName, SSID: targets[i].SSID}
		for _, step := range []string{"connect", "wait_connected"} {
			m.apply(watch.Event{
				Kind:     watch.EventStepFinished,
				Agent:    agents[0],
				Round:    1,
				Target:   target,
				Step:     watch.StepSnapshot{Name: step, Type: step, Status: "ok"},
				Status:   "ok",
				Duration: 10,
			})
		}
	}
	m.apply(watch.Event{
		Kind:   watch.EventStepStarted,
		Agent:  agents[0],
		Round:  1,
		Target: watch.TargetSnapshot{Name: targets[4].Name, ShortName: targets[4].ShortName, SSID: targets[4].SSID},
		Step:   watch.StepSnapshot{Name: "connect", Type: "connect", Status: "running"},
		Status: "running",
	})
	m.width = 28

	checkStatus := stripANSI(m.checkStatusView(panelContentWidth(m.width), 3))
	for _, want := range []string{"A4", "A5", "A6", "RUN", "WAIT"} {
		if !strings.Contains(checkStatus, want) {
			t.Fatalf("auto check status should keep a contiguous window around the running target %q:\n%s", want, checkStatus)
		}
	}
	for _, notWant := range []string{"A1", "A2", "A3"} {
		if strings.Contains(checkStatus, notWant) {
			t.Fatalf("auto check status should not splice a leading prefix before the tracked window %q:\n%s", notWant, checkStatus)
		}
	}
	if strings.Index(checkStatus, "A5") < strings.Index(checkStatus, "A4") || strings.Index(checkStatus, "A6") < strings.Index(checkStatus, "A5") {
		t.Fatalf("tracked target window should stay in configured order:\n%s", checkStatus)
	}
}

func TestCheckStatusAutoWindowTracksFailedTargetContiguously(t *testing.T) {
	events := make(chan watch.Event)
	agents := []watch.AgentSnapshot{{ID: "agent-a", Name: "pixel-a"}}
	disconnectAfter := false
	targets := []watch.Target{
		{Name: "ap1(5G)", ShortName: "A1", SSID: "Lab", DisconnectAfter: &disconnectAfter},
		{Name: "ap2(5G)", ShortName: "A2", SSID: "Lab", DisconnectAfter: &disconnectAfter},
		{Name: "ap3(5G)", ShortName: "A3", SSID: "Lab", DisconnectAfter: &disconnectAfter},
		{Name: "ap4(5G)", ShortName: "A4", SSID: "Lab", DisconnectAfter: &disconnectAfter},
		{Name: "ap5(5G)", ShortName: "A5", SSID: "Lab", DisconnectAfter: &disconnectAfter},
		{Name: "ap6(5G)", ShortName: "A6", SSID: "Lab", DisconnectAfter: &disconnectAfter},
	}
	m := newModelWithChecks("shownet-watch", targets, []watch.Check{}, events, agents)
	for i := 0; i < 4; i++ {
		target := watch.TargetSnapshot{Name: targets[i].Name, ShortName: targets[i].ShortName, SSID: targets[i].SSID}
		for _, step := range []string{"connect", "wait_connected"} {
			m.apply(watch.Event{
				Kind:     watch.EventStepFinished,
				Agent:    agents[0],
				Round:    1,
				Target:   target,
				Step:     watch.StepSnapshot{Name: step, Type: step, Status: "ok"},
				Status:   "ok",
				Duration: 10,
			})
		}
	}
	m.apply(watch.Event{
		Kind:   watch.EventFinding,
		Agent:  agents[0],
		Round:  1,
		Target: watch.TargetSnapshot{Name: targets[4].Name, ShortName: targets[4].ShortName, SSID: targets[4].SSID},
		Step:   watch.StepSnapshot{Name: "connect", Type: "connect", Status: "failed"},
		Status: "failed",
		Finding: &watch.Finding{
			Target: targets[4].Name,
			Check:  "connect",
			Metric: "status",
		},
	})
	m.width = 28

	checkStatus := stripANSI(m.checkStatusView(panelContentWidth(m.width), 3))
	for _, want := range []string{"A4", "A5", "A6", "FAIL", "WAIT"} {
		if !strings.Contains(checkStatus, want) {
			t.Fatalf("auto check status should keep a contiguous window around the failed target %q:\n%s", want, checkStatus)
		}
	}
	for _, notWant := range []string{"A1", "A2", "A3"} {
		if strings.Contains(checkStatus, notWant) {
			t.Fatalf("auto check status should not splice a leading prefix before the tracked failure %q:\n%s", notWant, checkStatus)
		}
	}
	if strings.Index(checkStatus, "A5") < strings.Index(checkStatus, "A4") || strings.Index(checkStatus, "A6") < strings.Index(checkStatus, "A5") {
		t.Fatalf("tracked failure window should stay in configured order:\n%s", checkStatus)
	}
}

func TestCheckStatusHorizontalScrollPinsVisibleWindow(t *testing.T) {
	events := make(chan watch.Event)
	agents := []watch.AgentSnapshot{{ID: "agent-a", Name: "pixel-a"}}
	disconnectAfter := false
	targets := []watch.Target{
		{Name: "ap1(5G)", ShortName: "A1", SSID: "Lab", DisconnectAfter: &disconnectAfter},
		{Name: "ap2(5G)", ShortName: "A2", SSID: "Lab", DisconnectAfter: &disconnectAfter},
		{Name: "ap3(5G)", ShortName: "A3", SSID: "Lab", DisconnectAfter: &disconnectAfter},
		{Name: "ap4(5G)", ShortName: "A4", SSID: "Lab", DisconnectAfter: &disconnectAfter},
		{Name: "ap5(5G)", ShortName: "A5", SSID: "Lab", DisconnectAfter: &disconnectAfter},
		{Name: "ap6(5G)", ShortName: "A6", SSID: "Lab", DisconnectAfter: &disconnectAfter},
	}
	m := newModelWithChecks("shownet-watch", targets, []watch.Check{}, events, agents)
	m.width = 28
	m.moveCheckStatusHorizontal(2)

	checkStatus := stripANSI(m.checkStatusView(panelContentWidth(m.width), 3))
	for _, want := range []string{"A3", "A4", "A5"} {
		if !strings.Contains(checkStatus, want) {
			t.Fatalf("horizontally scrolled check status missing %q:\n%s", want, checkStatus)
		}
	}
	for _, notWant := range []string{"A1", "A2", "A6"} {
		if strings.Contains(checkStatus, notWant) {
			t.Fatalf("horizontally scrolled check status should not contain %q:\n%s", notWant, checkStatus)
		}
	}

	m.apply(watch.Event{
		Kind:     watch.EventStepFinished,
		Agent:    agents[0],
		Round:    1,
		Target:   watch.TargetSnapshot{Name: "ap1(5G)", ShortName: "A1", SSID: "Lab"},
		Step:     watch.StepSnapshot{Name: "connect", Type: "connect", Status: "ok"},
		Status:   "ok",
		Duration: 10,
	})
	checkStatus = stripANSI(m.checkStatusView(panelContentWidth(m.width), 3))
	for _, want := range []string{"A3", "A4", "A5"} {
		if !strings.Contains(checkStatus, want) {
			t.Fatalf("check status update should keep horizontal window %q:\n%s", want, checkStatus)
		}
	}
	if strings.Contains(checkStatus, "A1") {
		t.Fatalf("check status update should not auto-slide to updated hidden target:\n%s", checkStatus)
	}

	m.moveCheckStatusHorizontal(-2)
	if !m.checkStatusPinned {
		t.Fatalf("check status should keep manual horizontal window pinned at the left edge")
	}
	checkStatus = stripANSI(m.checkStatusView(panelContentWidth(m.width), 3))
	for _, want := range []string{"A1", "A2", "A3"} {
		if !strings.Contains(checkStatus, want) {
			t.Fatalf("left-edge pinned check status missing %q:\n%s", want, checkStatus)
		}
	}
	for _, notWant := range []string{"A4", "A5", "A6"} {
		if strings.Contains(checkStatus, notWant) {
			t.Fatalf("left-edge pinned check status should show the first contiguous window, not %q:\n%s", notWant, checkStatus)
		}
	}
}

func TestCheckStatusLeftScrollFromAutoWindowPinsPreviousWindow(t *testing.T) {
	events := make(chan watch.Event)
	agents := []watch.AgentSnapshot{{ID: "agent-a", Name: "pixel-a"}}
	disconnectAfter := false
	targets := []watch.Target{
		{Name: "ap1(5G)", ShortName: "A1", SSID: "Lab", DisconnectAfter: &disconnectAfter},
		{Name: "ap2(5G)", ShortName: "A2", SSID: "Lab", DisconnectAfter: &disconnectAfter},
		{Name: "ap3(5G)", ShortName: "A3", SSID: "Lab", DisconnectAfter: &disconnectAfter},
		{Name: "ap4(5G)", ShortName: "A4", SSID: "Lab", DisconnectAfter: &disconnectAfter},
		{Name: "ap5(5G)", ShortName: "A5", SSID: "Lab", DisconnectAfter: &disconnectAfter},
		{Name: "ap6(5G)", ShortName: "A6", SSID: "Lab", DisconnectAfter: &disconnectAfter},
	}
	m := newModelWithChecks("shownet-watch", targets, []watch.Check{}, events, agents)
	for i := 0; i < 4; i++ {
		target := watch.TargetSnapshot{Name: targets[i].Name, ShortName: targets[i].ShortName, SSID: targets[i].SSID}
		for _, step := range []string{"connect", "wait_connected"} {
			m.apply(watch.Event{
				Kind:     watch.EventStepFinished,
				Agent:    agents[0],
				Round:    1,
				Target:   target,
				Step:     watch.StepSnapshot{Name: step, Type: step, Status: "ok"},
				Status:   "ok",
				Duration: 10,
			})
		}
	}
	m.width = 28
	m.apply(watch.Event{
		Kind:   watch.EventStepStarted,
		Agent:  agents[0],
		Round:  1,
		Target: watch.TargetSnapshot{Name: targets[4].Name, ShortName: targets[4].ShortName, SSID: targets[4].SSID},
		Step:   watch.StepSnapshot{Name: "connect", Type: "connect", Status: "running"},
		Status: "running",
	})

	auto := stripANSI(m.checkStatusView(panelContentWidth(m.width), 3))
	for _, want := range []string{"A4", "A5", "A6"} {
		if !strings.Contains(auto, want) {
			t.Fatalf("auto check status should track the running target before manual scroll %q:\n%s", want, auto)
		}
	}
	m.moveCheckStatusHorizontal(-1)
	if !m.checkStatusPinned {
		t.Fatalf("left scroll from auto should pin a contiguous target window")
	}
	checkStatus := stripANSI(m.checkStatusView(panelContentWidth(m.width), 3))
	for _, want := range []string{"A3", "A4", "A5"} {
		if !strings.Contains(checkStatus, want) {
			t.Fatalf("left-scrolled check status missing %q:\n%s", want, checkStatus)
		}
	}
	for _, notWant := range []string{"A1", "A2", "A6"} {
		if strings.Contains(checkStatus, notWant) {
			t.Fatalf("left-scrolled check status should show the previous contiguous window, not %q:\n%s", notWant, checkStatus)
		}
	}
}

func TestCheckStatusAutoWindowStaysStableBetweenStepTransitions(t *testing.T) {
	events := make(chan watch.Event)
	agents := []watch.AgentSnapshot{{ID: "agent-a", Name: "pixel-a"}}
	targets := []watch.Target{
		{Name: "ap1(5G)", ShortName: "A1", SSID: "Lab"},
		{Name: "ap2(5G)", ShortName: "A2", SSID: "Lab"},
		{Name: "ap3(5G)", ShortName: "A3", SSID: "Lab"},
		{Name: "ap4(5G)", ShortName: "A4", SSID: "Lab"},
		{Name: "ap5(5G)", ShortName: "A5", SSID: "Lab"},
		{Name: "ap6(5G)", ShortName: "A6", SSID: "Lab"},
	}
	m := newModelWithChecks("shownet-watch", targets, []watch.Check{}, events, agents)
	m.width = 28
	target := watch.TargetSnapshot{Name: targets[4].Name, ShortName: targets[4].ShortName, SSID: targets[4].SSID}
	m.apply(watch.Event{
		Kind:   watch.EventStepStarted,
		Agent:  agents[0],
		Round:  1,
		Target: target,
		Step:   watch.StepSnapshot{Name: "connect", Type: "connect", Status: "running"},
		Status: "running",
	})
	before := stripANSI(m.checkStatusView(panelContentWidth(m.width), 3))
	m.apply(watch.Event{
		Kind:     watch.EventStepFinished,
		Agent:    agents[0],
		Round:    1,
		Target:   target,
		Step:     watch.StepSnapshot{Name: "connect", Type: "connect", Status: "ok"},
		Status:   "ok",
		Duration: 10,
	})
	between := stripANSI(m.checkStatusView(panelContentWidth(m.width), 3))
	m.apply(watch.Event{
		Kind:   watch.EventStepStarted,
		Agent:  agents[0],
		Round:  1,
		Target: target,
		Step:   watch.StepSnapshot{Name: "wait_connected", Type: "wait_connected", Status: "running"},
		Status: "running",
	})
	after := stripANSI(m.checkStatusView(panelContentWidth(m.width), 3))

	for _, view := range []string{before, between, after} {
		for _, want := range []string{"A4", "A5", "A6"} {
			if !strings.Contains(view, want) {
				t.Fatalf("auto check status should keep the same target window across step transitions, missing %q:\nbefore:\n%s\nbetween:\n%s\nafter:\n%s", want, before, between, after)
			}
		}
		for _, notWant := range []string{"A1", "A2", "A3"} {
			if strings.Contains(view, notWant) {
				t.Fatalf("auto check status should not swap back to the leading window between step events, found %q:\nbefore:\n%s\nbetween:\n%s\nafter:\n%s", notWant, before, between, after)
			}
		}
	}
	if !strings.Contains(between, "PASS") {
		t.Fatalf("step-finished frame should preserve the completed check instead of turning the tracked window into all WAIT:\n%s", between)
	}
}

func TestCheckStatusAutoWindowRepositionsAfterResize(t *testing.T) {
	events := make(chan watch.Event)
	agents := []watch.AgentSnapshot{
		{ID: "agent-a", Name: "pixel-a"},
		{ID: "agent-b", Name: "pixel-b"},
	}
	targets := make([]watch.Target, 20)
	for i := range targets {
		targets[i] = watch.Target{
			Name:      fmt.Sprintf("ap%02d(5G)", i+1),
			ShortName: fmt.Sprintf("T%02d", i+1),
			SSID:      "Lab",
		}
	}
	m := newModelWithChecks("shownet-watch", targets, []watch.Check{{Name: "connect", Type: "connect"}}, events, agents)
	for _, run := range []struct {
		agent watch.AgentSnapshot
		index int
	}{
		{agent: agents[0], index: 3},
		{agent: agents[1], index: 17},
	} {
		target := watch.TargetSnapshot{
			Name:      targets[run.index].Name,
			ShortName: targets[run.index].ShortName,
			SSID:      targets[run.index].SSID,
		}
		m.apply(watch.Event{
			Kind:   watch.EventStepStarted,
			Agent:  run.agent,
			Round:  1,
			Target: target,
			Step:   watch.StepSnapshot{Name: "connect", Type: "connect", Status: "running"},
			Status: "running",
		})
	}

	checkStatusTargets := m.checkStatusTargets()
	if got := m.checkStatusActiveTargetIndex(checkStatusTargets); got != 17 {
		t.Fatalf("auto check status should track the rightmost running target, got index %d", got)
	}

	m.width = 80
	checkStatusTargets, layout, maxOffset := m.checkStatusWindowMetrics(panelContentWidth(m.width))
	if maxOffset <= 0 {
		t.Fatalf("test requires horizontally scrollable check status, maxOffset=%d layout=%#v", maxOffset, layout)
	}
	m.checkStatusOffset = maxOffset
	m.checkStatusPinned = false
	m.normalizeCheckStatusOffset()

	visible := min(layout.VisibleTargets, len(checkStatusTargets))
	wantOffset := clamp(17-max(0, visible-2), 0, maxOffset)
	if m.checkStatusOffset != wantOffset {
		t.Fatalf("auto resize should align the latest running target with one following column, offset=%d want=%d layout=%#v", m.checkStatusOffset, wantOffset, layout)
	}
	window := m.checkStatusVisibleTargets(checkStatusTargets, layout)
	if len(window) < 2 || window[len(window)-2].ShortName != "T18" || window[len(window)-1].ShortName != "T19" {
		t.Fatalf("auto resize should show the latest running target plus one following column, got window %#v", window)
	}
}

func TestCheckStatusManualScrollUnpinsWhenFocusLeaves(t *testing.T) {
	events := make(chan watch.Event)
	agents := []watch.AgentSnapshot{{ID: "agent-a", Name: "pixel-a"}}
	targets := []watch.Target{
		{Name: "ap1(5G)", ShortName: "A1", SSID: "Lab"},
		{Name: "ap2(5G)", ShortName: "A2", SSID: "Lab"},
		{Name: "ap3(5G)", ShortName: "A3", SSID: "Lab"},
		{Name: "ap4(5G)", ShortName: "A4", SSID: "Lab"},
		{Name: "ap5(5G)", ShortName: "A5", SSID: "Lab"},
		{Name: "ap6(5G)", ShortName: "A6", SSID: "Lab"},
	}
	m := newModelWithChecks("shownet-watch", targets, []watch.Check{}, events, agents)
	m.width = 28
	m.focus = focusCheckStatus
	m.moveCheckStatusHorizontal(1)
	if !m.checkStatusPinned || m.checkStatusOffset == 0 {
		t.Fatalf("check status horizontal scroll should pin a manual window, pinned=%v offset=%d", m.checkStatusPinned, m.checkStatusOffset)
	}

	m.setFocusSlot(focusSlot{Panel: focusRunQueue})
	if m.checkStatusPinned {
		t.Fatalf("leaving check status should unpin manual horizontal window")
	}
}

func TestAssignedTargetsUseAssignedAgentAsCheckStatusDenominator(t *testing.T) {
	events := make(chan watch.Event)
	agents := []watch.AgentSnapshot{
		{ID: "agent-a", Name: "35251JEHN00258", ADBSerial: "35251JEHN00258", DeviceModel: "Pixel 7a"},
		{ID: "agent-b", Name: "45240DLAQ007HG", ADBSerial: "45240DLAQ007HG", DeviceModel: "Pixel 9"},
	}
	targets := []watch.Target{
		{Name: "ap-5g", ShortName: "A5", Agent: "35251JEHN00258", SSID: "Lab"},
		{Name: "ap-6g", ShortName: "A6", Agent: "45240DLAQ007HG", SSID: "Lab"},
	}
	m := newModelWithChecks("shownet-watch", targets, []watch.Check{{Name: "connect", Type: "connect"}}, events, agents)
	m.apply(watch.Event{
		Kind:     watch.EventStepFinished,
		Agent:    agents[0],
		Round:    1,
		Target:   watch.TargetSnapshot{Name: "ap-5g", ShortName: "A5", Agent: "35251JEHN00258", SSID: "Lab"},
		Step:     watch.StepSnapshot{Name: "connect", Type: "connect", Status: "ok"},
		Status:   "ok",
		Duration: 10,
	})
	m.apply(watch.Event{
		Kind:   watch.EventFinding,
		Agent:  agents[1],
		Round:  1,
		Target: watch.TargetSnapshot{Name: "ap-6g", ShortName: "A6", Agent: "45240DLAQ007HG", SSID: "Lab"},
		Step:   watch.StepSnapshot{Name: "connect", Type: "connect", Status: "failed"},
		Status: "failed",
		Finding: &watch.Finding{
			Target: "ap-6g",
			Check:  "connect",
			Metric: "status",
		},
	})

	checkStatus := stripANSI(m.checkStatusView(80, 3))
	if !strings.Contains(checkStatus, "PASS") || !strings.Contains(checkStatus, "FAIL") ||
		strings.Contains(checkStatus, "PASS(50%)") || strings.Contains(checkStatus, "FAIL(50%)") {
		t.Fatalf("assigned target should use one-agent denominator:\n%s", checkStatus)
	}
	runQueue := stripANSI(m.runQueuePanelsView(80, 8))
	if !strings.Contains(runQueue, "ap-5g") || !strings.Contains(runQueue, "ap-6g") {
		t.Fatalf("assigned run queue should keep both assigned targets:\n%s", runQueue)
	}

	buckets, _, _ := m.targetRoundHistory(watch.TargetSnapshot{Name: "ap-5g", ShortName: "A5", Agent: "35251JEHN00258", SSID: "Lab"}, 1)
	if buckets[0].ConnectFailed {
		t.Fatalf("passing assigned target should not render connect failure: %#v", buckets[0])
	}
	buckets, _, _ = m.targetRoundHistory(watch.TargetSnapshot{Name: "ap-6g", ShortName: "A6", Agent: "45240DLAQ007HG", SSID: "Lab"}, 1)
	if !buckets[0].ConnectFailed {
		t.Fatalf("failed assigned target should render connect failure with one assigned agent: %#v", buckets[0])
	}
}

func TestCheckStatusShowsPendingInsteadOfPreviousFailureOnNextRound(t *testing.T) {
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
	if !strings.Contains(checkStatus, "WAIT") || strings.Contains(checkStatus, "FAIL") {
		t.Fatalf("new round should show pending current-round check instead of stale failure:\n%s", checkStatus)
	}
	cell := m.checkStatusTargetCell("ping cloudflare", target, []watch.AgentSnapshot{{}})
	if cell.Status != "pending" || cell.Stale {
		t.Fatalf("new round checkStatus cell = %#v, want current pending", cell)
	}
}
