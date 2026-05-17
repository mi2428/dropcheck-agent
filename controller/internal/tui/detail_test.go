package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"dropcheck/controller/internal/watch"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func TestEnterShowsFailedCheckDetail(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", []watch.Target{
		{Name: "SHIZK RADIO", SSID: "SHIZK RADIO", Band: "5ghz"},
	}, events)
	m.width = 150
	m.height = 50
	m.Now = time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
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
	m.Now = at.Add(40 * time.Second)
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
		t.Fatalf("tab should keep modal open and move focus/detail Panel: open=%v focus=%v detail=%v", m.detailOpen, m.focus, m.detailPanel)
	}
}

func TestDetailModalCursorFollowsOpenedItemAcrossUpdates(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", []watch.Target{}, events)
	m.focus = focusPassingChecks
	at := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	m.Now = at.Add(time.Minute)
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
	if got := m.filteredPassingCheckSummaries()[m.passingCheckCursor].Target.Name; got != "target-b" {
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
	if got := rows[m.passingCheckCursor].Target.Name; got != "target-b" {
		t.Fatalf("cursor should follow opened item after re-sort, got %q rows=%#v", got, rows)
	}
	view := stripANSI(m.passingCheckDetailView(100, 12))
	if !strings.Contains(view, "target=target-b") {
		t.Fatalf("detail modal should keep showing opened Item:\n%s", view)
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
	m.Now = at.Add(60 * time.Second)
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
	m.Now = at.Add(40 * time.Second)
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
	for i := range visibleEventLogLimit + 25 {
		m.apply(watch.Event{
			Time:    at.Add(time.Duration(i+1) * time.Second),
			Kind:    watch.EventLog,
			Message: fmt.Sprintf("unrelated log %03d", i),
		})
	}
	m.Now = at.Add(time.Duration(visibleEventLogLimit+25) * time.Second)
	if strings.Contains(strings.Join(m.Logs, "\n"), "kind=finding") {
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
	for i := range 30 {
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
