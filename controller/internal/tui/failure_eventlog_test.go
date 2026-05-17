package tui

import (
	"strings"
	"testing"
	"time"

	"dropcheck/controller/internal/watch"

	"charm.land/lipgloss/v2"
)

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
	finding := rows[0].Finding
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
		Name string
		Text string
	}{
		{Name: "passingChecks", Text: passingChecks},
		{Name: "failedChecks", Text: failedChecks},
	} {
		for _, want := range []string{"09:30"} {
			if !strings.Contains(frame.Text, want) {
				t.Fatalf("%s table missing %q:\n%s", frame.Name, want, frame.Text)
			}
		}
		if strings.Contains(frame.Text, "SSID ") {
			t.Fatalf("%s table should not render SSID group Rows:\n%s", frame.Name, frame.Text)
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

	joinedLogs := strings.Join(m.Logs, "\n")
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
