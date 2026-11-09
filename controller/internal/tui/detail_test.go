package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mi2428/dropcheck-agent/controller/internal/tuirun"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func renderSelectedDetail(t *testing.T, selectContent func(int) (detailContent, bool), width, height int) string {
	t.Helper()
	content, ok := selectContent(width)
	if !ok {
		t.Fatal("expected a selected detail row")
	}
	return content.inspectorText(width)
}

func TestEnterShowsFailedCheckDetail(t *testing.T) {
	events := make(chan tuirun.Event)
	m := newModel([]tuirun.Target{
		{Name: "Example Net", SSID: "Example Net", Band: "5ghz"},
	}, events)
	m.width = 150
	m.height = 50
	m.Now = time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	m.apply(tuirun.Event{
		Time:   time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC),
		Kind:   tuirun.EventFinding,
		Round:  1,
		Target: tuirun.TargetSnapshot{Name: "Example Net", SSID: "Example Net", Band: "5ghz"},
		Step:   tuirun.StepSnapshot{Name: "ping cloudflare", Type: "ping", Status: "failed"},
		Status: "failed",
		Finding: &tuirun.Finding{
			Target:   "Example Net",
			Check:    "ping cloudflare",
			Metric:   "received",
			Observed: "0",
			Expected: "== 5",
			Message:  "constraint failed",
		},
	})

	m.focus = focusFailedChecks
	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m = updated.(model)
	frame := stripANSI(m.render())
	for _, want := range []string{
		"Failed Check Detail",
		"Target: Example Net",
		"Check: ping cloudflare",
		"Metric: received",
		"Failures: 1",
		"Fail Rate: 100%",
		"Streak: 1",
		"Last: 09:30:00",
		"Observed: 0",
		"Expected: \"== 5\"",
		"Message: constraint failed",
		"Failure History",
		"Logs",
	} {
		if !strings.Contains(frame, want) {
			t.Fatalf("detail view missing %q:\n%s", want, frame)
		}
	}
}

func TestFailedRequiredStepDetailShowsWifiAssertFailurePoint(t *testing.T) {
	events := make(chan tuirun.Event)
	m := newModel([]tuirun.Target{
		{Name: "ub2(6G)", SSID: "Example Net", BSSID: "02:00:00:00:00:21", Band: "5ghz"},
	}, events)
	m.width = 180
	m.height = 50
	at := time.Date(2026, 5, 18, 10, 27, 26, 0, time.UTC)
	m.Now = at
	target := tuirun.TargetSnapshot{Name: "ub2(6G)", SSID: "Example Net", BSSID: "02:00:00:00:00:21", Band: "5ghz"}
	message := "wifi condition timeout: last_pass=band failed=ip(actual=absent expected=present),validated(actual=false expected=true) assert_elapsed_ms=30000"
	m.apply(tuirun.Event{
		Time:   at,
		Kind:   tuirun.EventStepFinished,
		Round:  1,
		Target: target,
		Step:   tuirun.StepSnapshot{Name: "wait_connected", Type: "wait_connected", Operation: "wifi.wait", Status: "failed", Message: message},
		Status: "failed",
	})
	m.apply(tuirun.Event{Time: at, Kind: tuirun.EventFinding, Round: 1, Target: target,
		Step:    tuirun.StepSnapshot{Name: "wait_connected", Type: "wait_connected", Operation: "wifi.wait", Status: "failed"},
		Finding: &tuirun.Finding{Target: target.Name, Check: "wait_connected", Metric: "status", Observed: "failed", Expected: "== ok", Message: message}})
	m.apply(tuirun.Event{
		Time:    at.Add(time.Second),
		Kind:    tuirun.EventLog,
		Round:   1,
		Target:  target,
		Step:    tuirun.StepSnapshot{Name: "wait_connected", Type: "wait_connected", Operation: "wifi.wait", Status: "failed"},
		Status:  "warn",
		Message: `wifi failure cause: disconnected reason=6 locally_generated=false bssid=02:00:00:00:00:21 ssid="Example Net"`,
	})

	view := stripANSI(renderSelectedDetail(t, m.failedCheckDetailContent, 180, 18))
	for _, want := range []string{
		"Target: ub2(6G)", "Check: Wait Connected", "Metric: status", "Failures: 1",
		"Fail Rate: 100%", "Streak: 1", "Last: 10:27:26", "Observed: failed", "Expected: \"== ok\"",
		"last_pass=band",
		"failed=ip(actual=absent expected=present),validated(actual=false expected=true)",
		"wifi failure cause: disconnected reason=6 locally_generated=false",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("failed required step detail missing %q:\n%s", want, view)
		}
	}
}

func TestEnterShowsPassingCheckDetail(t *testing.T) {
	events := make(chan tuirun.Event)
	m := newModel([]tuirun.Target{
		{Name: "Example Net", SSID: "Example Net", Band: "5ghz"},
	}, events)
	m.width = 150
	m.height = 50
	at := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	m.Now = at.Add(40 * time.Second)
	for _, second := range []int{0, 20, 40} {
		m.apply(tuirun.Event{
			Time:     at.Add(time.Duration(second) * time.Second),
			Kind:     tuirun.EventStepFinished,
			Round:    1,
			Target:   tuirun.TargetSnapshot{Name: "Example Net", SSID: "Example Net", Band: "5ghz"},
			Step:     tuirun.StepSnapshot{Name: "connect", Type: "connect", Operation: "wifi.connect", Status: "ok"},
			Status:   "ok",
			Duration: 120,
		})
	}

	m = updateKey(t, m, tea.Key{Code: tea.KeyTab})
	m = updateKey(t, m, tea.Key{Code: tea.KeyEnter})
	frame := stripANSI(m.render())
	for _, want := range []string{
		"Passing Check Detail",
		"Target: Example Net", "Check: Connect", "SSID: Example Net", "Band: 5ghz",
		"Op: wifi.connect", "Last: 09:30:40", "Duration: 120ms", "Avg: 120ms", "Max: 120ms", "Samples: 3",
		"window=last=90m count=",
		"peak=2",
		"scale=",
		"█",
		"90m ago",
		"now",
	} {
		if !strings.Contains(frame, want) {
			t.Fatalf("passing check detail view missing %q:\n%s", want, frame)
		}
	}
}

func TestInspectorKeepsFocusUntilExplicitlyClosed(t *testing.T) {
	events := make(chan tuirun.Event)
	m := newModel([]tuirun.Target{}, events)
	m.apply(tuirun.Event{
		Time:     time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC),
		Kind:     tuirun.EventStepFinished,
		Round:    1,
		Target:   tuirun.TargetSnapshot{Name: "Example Net", SSID: "Example Net"},
		Step:     tuirun.StepSnapshot{Name: "connect", Type: "connect", Status: "ok"},
		Status:   "ok",
		Duration: 10,
	})
	m = updateKey(t, m, tea.Key{Code: tea.KeyTab})
	m = updateKey(t, m, tea.Key{Code: tea.KeyEnter})
	if !m.detailOpen || m.detailPanel != focusPassingChecks {
		t.Fatalf("enter on passing checks should open passing check detail: open=%v panel=%v", m.detailOpen, m.detailPanel)
	}

	m = updateKey(t, m, tea.Key{Code: tea.KeyTab, Mod: tea.ModShift})
	if !m.detailOpen || m.focus != focusPassingChecks {
		t.Fatalf("inspector must retain its scope and focus: open=%v focus=%v", m.detailOpen, m.focus)
	}
	m = updateKey(t, m, tea.Key{Code: tea.KeyEscape})
	if m.detailOpen {
		t.Fatal("Esc did not close inspector")
	}
}

func TestInspectorCursorFollowsOpenedItemAcrossUpdates(t *testing.T) {
	events := make(chan tuirun.Event)
	m := newModel([]tuirun.Target{}, events)
	m.focus = focusPassingChecks
	at := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	m.Now = at.Add(time.Minute)
	targetA := tuirun.TargetSnapshot{Name: "target-a", SSID: "Lab"}
	targetB := tuirun.TargetSnapshot{Name: "target-b", SSID: "Lab"}
	m.apply(tuirun.Event{
		Time:     at.Add(-time.Minute),
		Kind:     tuirun.EventStepFinished,
		Round:    1,
		Target:   targetA,
		Step:     tuirun.StepSnapshot{Name: "connect", Type: "connect", Status: "ok"},
		Status:   "ok",
		Duration: 10,
	})
	m.apply(tuirun.Event{
		Time:     at,
		Kind:     tuirun.EventStepFinished,
		Round:    1,
		Target:   targetB,
		Step:     tuirun.StepSnapshot{Name: "connect", Type: "connect", Status: "ok"},
		Status:   "ok",
		Duration: 10,
	})
	m.passingCheckCursor = 0
	m.openDetailForPanel(focusPassingChecks)
	if got := m.filteredPassingCheckSummaries()[m.passingCheckCursor].Target.Name; got != "target-b" {
		t.Fatalf("opened row = %q, want target-b", got)
	}

	m.apply(tuirun.Event{
		Time:     at.Add(time.Minute),
		Kind:     tuirun.EventStepFinished,
		Round:    2,
		Target:   targetA,
		Step:     tuirun.StepSnapshot{Name: "connect", Type: "connect", Status: "ok"},
		Status:   "ok",
		Duration: 12,
	})

	rows := m.filteredPassingCheckSummaries()
	if got := rows[m.passingCheckCursor].Target.Name; got != "target-b" {
		t.Fatalf("cursor should follow opened item after re-sort, got %q rows=%#v", got, rows)
	}
	view := stripANSI(renderSelectedDetail(t, m.passingCheckDetailContent, 100, 12))
	if !strings.Contains(view, "target-b") {
		t.Fatalf("detail modal should keep showing opened Item:\n%s", view)
	}
}

func TestFailedCheckDetailOverlaysExistingTUI(t *testing.T) {
	events := make(chan tuirun.Event)
	m := newModel([]tuirun.Target{
		{Name: "Example Net", SSID: "Example Net", Band: "5ghz"},
	}, events)
	m.width = 150
	m.height = 32
	m.apply(tuirun.Event{Kind: tuirun.EventTUIStarted, Message: "tui started"})
	m.apply(tuirun.Event{
		Time:   time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC),
		Kind:   tuirun.EventFinding,
		Round:  1,
		Target: tuirun.TargetSnapshot{Name: "Example Net", SSID: "Example Net", Band: "5ghz"},
		Step:   tuirun.StepSnapshot{Name: "ping cloudflare", Type: "ping", Status: "failed"},
		Status: "failed",
		Finding: &tuirun.Finding{
			Target:   "Example Net",
			Check:    "ping cloudflare",
			Metric:   "received",
			Observed: "0",
			Expected: "== 5",
			Message:  "constraint failed",
		},
	})
	background := stripANSI(m.render())
	m.openDetailForPanel(focusFailedChecks)
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

func TestFailureHotspotDetailSectionsUseTitledSpacing(t *testing.T) {
	events := make(chan tuirun.Event)
	m := newModel([]tuirun.Target{}, events)
	at := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	m.Now = at.Add(2 * time.Minute)
	target := tuirun.TargetSnapshot{Name: "radio-a", SSID: "Example Net", Band: "5ghz"}
	for round, when := range []time.Time{at, at.Add(time.Minute)} {
		m.apply(tuirun.Event{
			Time:   when,
			Kind:   tuirun.EventFinding,
			Round:  uint64(round + 1),
			Target: target,
			Step:   tuirun.StepSnapshot{Name: "public ipv6", Type: "global_ip", Status: "failed"},
			Status: "failed",
			Finding: &tuirun.Finding{
				Target:   target.Name,
				Check:    "public ipv6",
				Metric:   "ipv6_global_ips",
				Observed: "-",
				Expected: "cidr ::/0 mode=at_least",
				Message:  "constraint failed",
			},
		})
	}

	view := stripANSI(renderSelectedDetail(t, m.failureHotspotDetailContent, 120, 25))
	for _, want := range []string{
		"Causes",
		"2  09:31:00  constraint failed",
		"Failure History",
		"09:31:00  2      public ipv6",
		"Logs",
		"kind=finding",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("hotspot detail view missing %q:\n%s", want, view)
		}
	}
	lines := strings.Split(view, "\n")
	for _, marker := range []string{"Causes", "Failure History", "Logs"} {
		index := lineIndex(view, marker)
		if index <= 0 || lines[index-1] != "" {
			t.Fatalf("%s should have one blank line above it, got %q:\n%s", marker, lines[intMax(0, index-1)], view)
		}
	}
}

func TestFailedCheckDetailLogsAreScopedToSelectedCheck(t *testing.T) {
	events := make(chan tuirun.Event)
	m := newModel([]tuirun.Target{}, events)
	at := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	target := tuirun.TargetSnapshot{Name: "radio-a", SSID: "Lab", Band: "5ghz"}
	m.apply(tuirun.Event{
		Time:   at,
		Kind:   tuirun.EventStepFinished,
		Round:  1,
		Target: target,
		Step:   tuirun.StepSnapshot{Name: "http cloudflare v6", Type: "http", Status: "ok"},
		Status: "ok",
	})
	m.apply(tuirun.Event{
		Time:   at.Add(time.Second),
		Kind:   tuirun.EventFinding,
		Round:  1,
		Target: target,
		Step:   tuirun.StepSnapshot{Name: "public ipv6", Type: "global_ip", Status: "failed"},
		Status: "failed",
		Finding: &tuirun.Finding{
			Target:   "radio-a",
			Check:    "public ipv6",
			Metric:   "ipv6_global_ips",
			Observed: "-",
			Expected: "cidr ::/0 mode=at_least",
			Message:  "constraint failed",
		},
	})
	m.apply(tuirun.Event{
		Time:   at.Add(2 * time.Second),
		Kind:   tuirun.EventStepFinished,
		Round:  1,
		Target: target,
		Step:   tuirun.StepSnapshot{Name: "disconnect", Type: "cleanup", Status: "ok"},
		Status: "ok",
	})

	view := stripANSI(renderSelectedDetail(t, m.failedCheckDetailContent, 120, 18))
	for _, want := range []string{"Logs", "kind=finding", "step=\"public ipv6\"", "metric=ipv6_global_ips"} {
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
	events := make(chan tuirun.Event)
	m := newModel([]tuirun.Target{}, events)
	at := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	target := tuirun.TargetSnapshot{Name: "radio-a", SSID: "Lab", Band: "5ghz"}
	m.apply(tuirun.Event{
		Time:   at,
		Kind:   tuirun.EventFinding,
		Round:  1,
		Target: target,
		Step:   tuirun.StepSnapshot{Name: "public ipv6", Type: "global_ip", Status: "failed"},
		Status: "failed",
		Finding: &tuirun.Finding{
			Target:   "radio-a",
			Check:    "public ipv6",
			Metric:   "ipv6_global_ips",
			Observed: "-",
			Expected: "cidr ::/0 mode=at_least",
			Message:  "constraint failed",
		},
	})
	for i := range visibleEventLogLimit + 25 {
		m.apply(tuirun.Event{
			Time:    at.Add(time.Duration(i+1) * time.Second),
			Kind:    tuirun.EventLog,
			Message: fmt.Sprintf("unrelated log %03d", i),
		})
	}
	m.Now = at.Add(time.Duration(visibleEventLogLimit+25) * time.Second)
	if strings.Contains(strings.Join(m.Logs, "\n"), "kind=finding") {
		t.Fatalf("test setup expected selected finding to be outside the visible Event Log tail")
	}

	view := stripANSI(renderSelectedDetail(t, m.failedCheckDetailContent, 120, 18))
	for _, want := range []string{"Logs", "kind=finding", "step=\"public ipv6\""} {
		if !strings.Contains(view, want) {
			t.Fatalf("detail should use structured history for %q:\n%s", want, view)
		}
	}
}

func TestDetailLogRowsWrapToViewWidth(t *testing.T) {
	width := 58
	lines := detailLogRowLines(`09:30:00 kind=finding round=1 status=failed target="radio-a" ssid="Lab" step="public ipv6" metric=ipv6_global_ips observed=- expected="cidr ::/0 mode=at_least"`, width)
	if len(lines) < 2 {
		t.Fatalf("long log should wrap into multiple rows: %#v", lines)
	}
	view := stripANSI(strings.Join(lines, "\n"))
	for i, line := range strings.Split(view, "\n") {
		if got := lipgloss.Width(line); got > width {
			t.Fatalf("detail line %d width = %d, want <= %d: %q\n%s", i, got, width, line, view)
		}
	}
	if !strings.HasPrefix(strings.Split(view, "\n")[1], "            ") {
		t.Fatalf("continuation row should keep timestamp column empty:\n%s", view)
	}
	if strings.Contains(view, "~") {
		t.Fatalf("wrapped log should not be truncated:\n%s", view)
	}

	hardWrapped := stripANSI(strings.Join(detailLogRowLines("09:30:00 "+strings.Repeat("x", 80), 24), "\n"))
	if got := strings.Count(hardWrapped, "\n") + 1; got < 4 {
		t.Fatalf("long log token should hard-wrap, got %d lines:\n%s", got, hardWrapped)
	}
	if strings.Contains(hardWrapped, "~") {
		t.Fatalf("hard-wrapped log should not be truncated:\n%s", hardWrapped)
	}
	for i, line := range strings.Split(hardWrapped, "\n") {
		if got := lipgloss.Width(line); got > 24 {
			t.Fatalf("hard-wrapped line %d width = %d, want <= 24:\n%s", i, got, hardWrapped)
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
	}, 104, detailTimelineWindow, now)
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
	for _, want := range []string{"90m ago", "now", "█"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("chart missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "09:") || strings.Contains(joined, "┬") || strings.Contains(joined, "回数") || strings.Contains(joined, "時刻") || strings.Contains(joined, "·") {
		t.Fatalf("detail chart should use the summary sparkline axis/rendering:\n%s", joined)
	}
}

func TestFailureCauseHistoryTableAlignsWithLongCheckNames(t *testing.T) {
	events := make(chan tuirun.Event)
	m := newModel([]tuirun.Target{}, events)
	at := time.Date(2026, 5, 16, 13, 41, 52, 0, time.UTC)
	m.Now = at.Add(time.Minute)
	target := tuirun.TargetSnapshot{Name: "debug-a1(5G)", SSID: "Example Net"}
	m.apply(tuirun.Event{
		Time:   at,
		Kind:   tuirun.EventFinding,
		Round:  2,
		Target: target,
		Step:   tuirun.StepSnapshot{Name: "Debug Ping Blackhole", Type: "ping", Status: "failed"},
		Status: "failed",
		Finding: &tuirun.Finding{
			Target:   target.Name,
			Check:    "Debug Ping Blackhole",
			Metric:   "status",
			Observed: "failed",
			Expected: "== ok",
			Message:  "ping failed",
		},
	})
	causes := m.FailureCauses()
	if len(causes) != 1 {
		t.Fatalf("failure causes = %d, want 1: %#v", len(causes), causes)
	}

	rows := m.failureCauseDetailRows(causes[0], 120)
	if len(rows) < 2 {
		t.Fatalf("failure cause history rows = %d, want header and data", len(rows))
	}
	assertDetailTableColumnAligned(t, rows[0], rows[1], [][2]string{
		{"Metric", "status"},
		{"Observed", "failed"},
		{"Expected", "\"== ok\""},
	})
}

func TestFailureHotspotHistoryTableAlignsWithLongCheckNames(t *testing.T) {
	events := make(chan tuirun.Event)
	m := newModel([]tuirun.Target{}, events)
	at := time.Date(2026, 5, 16, 14, 6, 35, 0, time.UTC)
	m.Now = at.Add(time.Minute)
	target := tuirun.TargetSnapshot{Name: "debug-a1(5G)", SSID: "Example Net"}
	m.apply(tuirun.Event{
		Time:   at,
		Kind:   tuirun.EventFinding,
		Round:  1,
		Target: target,
		Step:   tuirun.StepSnapshot{Name: "Debug HTTP Wrong Status", Type: "http", Status: "failed"},
		Status: "failed",
		Finding: &tuirun.Finding{
			Target:   target.Name,
			Check:    "Debug HTTP Wrong Status",
			Metric:   "elapsed_ms",
			Observed: "45",
			Expected: "<= 1",
			Message:  "constraint failed",
		},
	})
	hotspots := m.FailureHotspots()
	if len(hotspots) != 1 {
		t.Fatalf("failure hotspots = %d, want 1: %#v", len(hotspots), hotspots)
	}

	rows := m.failureHotspotDetailRows(hotspots[0], 120)
	if len(rows) < 2 {
		t.Fatalf("failure hotspot history rows = %d, want header and data", len(rows))
	}
	assertDetailTableColumnAligned(t, rows[0], rows[1], [][2]string{
		{"Metric", "elapsed_ms"},
		{"Observed", "45"},
		{"Expected", "\"<= 1\""},
		{"Cause", "constraint failed"},
	})
}

func assertDetailTableColumnAligned(t *testing.T, headerLine string, rowLine string, pairs [][2]string) {
	t.Helper()
	header := stripANSI(headerLine)
	row := stripANSI(rowLine)
	for _, pair := range pairs {
		want := strings.Index(header, pair[0])
		got := strings.Index(row, pair[1])
		if want < 0 || got < 0 {
			t.Fatalf("column pair not found: header %q index=%d row %q index=%d\nheader=%q\nrow=%q", pair[0], want, pair[1], got, header, row)
		}
		if got != want {
			t.Fatalf("column %q not aligned: header index=%d row index=%d\nheader=%q\nrow=%q", pair[0], want, got, header, row)
		}
	}
}
