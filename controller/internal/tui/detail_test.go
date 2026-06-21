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
	m := newModel([]watch.Target{
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

	m.focus = focusFailedChecks
	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m = updated.(model)
	frame := stripANSI(m.render())
	for _, want := range []string{
		"Failed Check Detail",
		"Event Log",
		"Run Queue",
		"Target       Check            Metric    Failures",
		"SHIZK RADIO  ping cloudflare  received  1",
		"Fail Rate  Streak",
		"100%       1",
		"Last      Observed  Expected",
		"09:30:00  0         \"== 5\"",
		"Message",
		"constraint failed",
		"Failure History:",
		"Logs:",
	} {
		if !strings.Contains(frame, want) {
			t.Fatalf("detail view missing %q:\n%s", want, frame)
		}
	}
}

func TestFailedRequiredStepDetailShowsWifiAssertFailurePoint(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel([]watch.Target{
		{Name: "ub2(6G)", SSID: "SHIZK RADIO", BSSID: "70:a7:41:a0:9a:6f", Band: "5ghz"},
	}, events)
	m.width = 180
	m.height = 50
	at := time.Date(2026, 5, 18, 10, 27, 26, 0, time.UTC)
	m.Now = at
	target := watch.TargetSnapshot{Name: "ub2(6G)", SSID: "SHIZK RADIO", BSSID: "70:a7:41:a0:9a:6f", Band: "5ghz"}
	message := "wifi condition timeout: last_pass=band failed=ip(actual=absent expected=present),validated(actual=false expected=true) assert_elapsed_ms=30000"
	m.apply(watch.Event{
		Time:   at,
		Kind:   watch.EventStepFinished,
		Round:  1,
		Target: target,
		Step:   watch.StepSnapshot{Name: "wait_connected", Type: "wait_connected", Operation: "wifi.wait", Status: "failed", Message: message},
		Status: "failed",
	})
	m.apply(watch.Event{
		Time:    at.Add(time.Second),
		Kind:    watch.EventLog,
		Round:   1,
		Target:  target,
		Step:    watch.StepSnapshot{Name: "wait_connected", Type: "wait_connected", Operation: "wifi.wait", Status: "failed"},
		Status:  "warn",
		Message: `wifi failure cause: disconnected reason=6 locally_generated=false bssid=70:a7:41:a0:9a:6f ssid="SHIZK RADIO"`,
	})

	view := stripANSI(m.failedCheckDetailView(180, 18))
	for _, want := range []string{
		"Target   Check           Metric  Failures  Fail Rate  Streak  Last      Observed  Expected",
		"ub2(6G)  Wait Connected  status  1         100%       1       10:27:26  failed    \"== ok\"",
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
	events := make(chan watch.Event)
	m := newModel([]watch.Target{
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
		"Target       Check    SSID         Band  Op            Last",
		"SHIZK RADIO  Connect  SHIZK RADIO  5ghz  wifi.connect  09:30:40",
		"Duration  Avg    Max    Samples",
		"120ms     120ms  120ms  3",
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

func TestTabKeepsModalOpenAndMovesPanelFocus(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel([]watch.Target{}, events)
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

	m = updateKey(t, m, tea.Key{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.detailOpen || m.focus != focusCheckStatus {
		t.Fatalf("shift-tab to a non-detail panel should close modal and focus check status: open=%v focus=%v detail=%v", m.detailOpen, m.focus, m.detailPanel)
	}
}

func TestDetailModalCursorFollowsOpenedItemAcrossUpdates(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel([]watch.Target{}, events)
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
	if !strings.Contains(view, "target-b") {
		t.Fatalf("detail modal should keep showing opened Item:\n%s", view)
	}
}

func TestFailedCheckDetailOverlaysExistingTUI(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel([]watch.Target{
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

func TestFailedCheckDetailModalKeepsInvestigationRowsWhenCompact(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel([]watch.Target{
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
		"Failures  Fail Rate  Streak",
		"7         100%       1",
		"Last      Observed  Expected",
		"09:31:00  0         \"== 5\"",
		"Failure History:",
		"Logs:",
	} {
		if !strings.Contains(frame, want) {
			t.Fatalf("compact detail view missing %q:\n%s", want, frame)
		}
	}
	if strings.Contains(frame, "回数") || strings.Contains(frame, "時刻") || strings.Contains(frame, "·") {
		t.Fatalf("detail histogram should not render Japanese labels or dotted zero buckets:\n%s", frame)
	}
}

func TestFailedCheckDetailUsesDenseInvestigationLayout(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel([]watch.Target{}, events)
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

	view := stripANSI(m.failedCheckDetailView(80, 19))
	lines := strings.Split(view, "\n")
	if got, want := len(lines), 19; got != want {
		t.Fatalf("detail line count = %d, want %d:\n%s", got, want, view)
	}
	for _, want := range []string{
		"Target       Check            Metric    Failures  Fail Rate  Streak",
		"SHIZK RADIO  ping cloudflare  received  3         100%       3",
		"Last      Observed  Expected",
		"09:30:40  0         \"== 5\"",
		"window=last=90m count=",
		"peak=2",
		"scale=",
		"Message",
		"constraint failed",
		"90m ago",
		"now",
		"Failure History:",
		"Time      Round  Check",
		"09:30:40",
		"Logs:",
		"kind=finding",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("dense detail view missing %q:\n%s", want, view)
		}
	}
	axis := lineIndex(view, "90m ago")
	section := lineIndex(view, "Failure History:")
	if axis <= 0 || section <= axis {
		t.Fatalf("detail view missing expected graph/section order:\n%s", view)
	}
	timeline := lineIndex(view, "window=last=90m")
	if timeline <= 0 || lines[timeline-1] != "" {
		t.Fatalf("detail graph should have one blank line above it, got line before timeline = %q:\n%s", lines[intMax(0, timeline-1)], view)
	}
	if timeline+1 >= len(lines) || lines[timeline+1] != "" {
		t.Fatalf("detail graph should have one blank line below its timeline header, got line after timeline = %q:\n%s", lines[intMin(len(lines)-1, timeline+1)], view)
	}
	if lines[section-1] != "" {
		t.Fatalf("detail graph should have one blank line below it, got line before section = %q:\n%s", lines[section-1], view)
	}
	logs := lineIndex(view, "Logs:")
	if logs <= section || lines[logs-1] != "" {
		t.Fatalf("detail sections should be separated by one blank line before Logs, got line before Logs = %q:\n%s", lines[intMax(0, logs-1)], view)
	}
}

func TestFailureHotspotDetailSectionsUseTitledSpacing(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel([]watch.Target{}, events)
	at := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	m.Now = at.Add(2 * time.Minute)
	target := watch.TargetSnapshot{Name: "radio-a", SSID: "SHIZK RADIO", Band: "5ghz"}
	for round, when := range []time.Time{at, at.Add(time.Minute)} {
		m.apply(watch.Event{
			Time:   when,
			Kind:   watch.EventFinding,
			Round:  uint64(round + 1),
			Target: target,
			Step:   watch.StepSnapshot{Name: "public ipv6", Type: "global_ip", Status: "failed"},
			Status: "failed",
			Finding: &watch.Finding{
				Target:   target.Name,
				Check:    "public ipv6",
				Metric:   "ipv6_global_ips",
				Observed: "-",
				Expected: "cidr ::/0 mode=at_least",
				Message:  "constraint failed",
			},
		})
	}

	view := stripANSI(m.failureHotspotDetailView(120, 25))
	for _, want := range []string{
		"Causes:",
		"2  09:31:00  constraint failed",
		"Failure History:",
		"09:31:00  2      public ipv6",
		"Logs:",
		"kind=finding",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("hotspot detail view missing %q:\n%s", want, view)
		}
	}
	lines := strings.Split(view, "\n")
	for _, marker := range []string{"Latest Cause", "Fail Rate", "Causes:", "Failure History:", "Logs:"} {
		index := lineIndex(view, marker)
		if index <= 0 || lines[index-1] != "" {
			t.Fatalf("%s should have one blank line above it, got %q:\n%s", marker, lines[intMax(0, index-1)], view)
		}
	}
}

func TestFailedCheckDetailLogsAreScopedToSelectedCheck(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel([]watch.Target{}, events)
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
	for _, want := range []string{"Logs:", "kind=finding", "step=\"public ipv6\"", "metric=ipv6_global_ips"} {
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
	m := newModel([]watch.Target{}, events)
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
	for _, want := range []string{"Logs:", "kind=finding", "step=\"public ipv6\""} {
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

func TestDetailSummaryFieldsRenderAdaptiveTable(t *testing.T) {
	fields := []detailField{
		{Key: "device", Value: "Pixel 7a (35251JEHN00258)"},
		{Key: "target", Value: "hp1(5G)"},
		{Key: "check", Value: "Wi-Fi Link"},
		{Key: "ssid", Value: "SHIZK RADIO"},
		{Key: "bssid", Value: "22:0B:8B:B6:2C:E1"},
		{Key: "band", Value: "5ghz"},
		{Key: "op", Value: "wifi.status"},
		{Key: "type", Value: "wifi_status"},
		{Key: "last", Value: "00:31:23"},
		{Key: "duration", Value: "466ms"},
	}

	wideLines := strings.Split(stripANSI(strings.Join(detailSummaryTableLines(fields, 160), "\n")), "\n")
	if len(wideLines) < 2 {
		t.Fatalf("summary table should render header/value rows: %#v", wideLines)
	}
	if !strings.Contains(wideLines[0], "Device") || !strings.Contains(wideLines[0], "Target") || !strings.Contains(wideLines[0], "Check") {
		t.Fatalf("summary header row missing expected columns:\n%s", strings.Join(wideLines, "\n"))
	}
	if !strings.Contains(wideLines[1], "Pixel 7a") || !strings.Contains(wideLines[1], "hp1(5G)") || !strings.Contains(wideLines[1], "Wi-Fi Link") {
		t.Fatalf("summary value row missing expected values:\n%s", strings.Join(wideLines, "\n"))
	}
	if strings.Contains(strings.Join(wideLines, "\n"), "device=") || strings.Contains(strings.Join(wideLines, "\n"), "target=") {
		t.Fatalf("summary should render table columns, not inline key=value pairs:\n%s", strings.Join(wideLines, "\n"))
	}

	narrow := stripANSI(strings.Join(detailSummaryTableLines(fields, 44), "\n"))
	for i, line := range strings.Split(narrow, "\n") {
		if got := lipgloss.Width(line); got > 44 {
			t.Fatalf("narrow summary line %d width = %d, want <= 44:\n%s", i, got, narrow)
		}
	}
	if got, wide := len(strings.Split(narrow, "\n")), len(wideLines); got <= wide {
		t.Fatalf("narrow summary should split into more rows: narrow=%d wide=%d\n%s", got, wide, narrow)
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

func TestOccurrenceGraphHeightUsesHalfOfDetailContent(t *testing.T) {
	if got, want := occurrenceGraphHeight(18), 9; got != want {
		t.Fatalf("occurrenceGraphHeight(18) = %d, want %d", got, want)
	}
	if got, want := occurrenceGraphHeight(6), 3; got != want {
		t.Fatalf("occurrenceGraphHeight(6) = %d, want %d", got, want)
	}
}

func TestDenseDetailTimelineSeparatesHeaderAndGraph(t *testing.T) {
	now := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	histogram := recentEventHistogram([]time.Time{now.Add(-time.Minute)}, 80, detailTimelineWindow, now)
	view := stripANSI(denseDetailView([]string{"Device  Pixel 9"}, histogram, failGraphStyle, nil, 80, 8))
	lines := strings.Split(view, "\n")
	header := -1
	for i, line := range lines {
		if strings.Contains(line, "window=last=90m") {
			header = i
			break
		}
	}
	if header < 0 {
		t.Fatalf("detail timeline header missing:\n%s", view)
	}
	if header+1 >= len(lines) || strings.TrimSpace(lines[header+1]) != "" {
		t.Fatalf("detail timeline header should have a spacer row below it:\n%s", view)
	}
	axis := lineIndex(view, "90m ago")
	if axis <= header+2 {
		t.Fatalf("detail timeline graph should start after the spacer row:\n%s", view)
	}
	graph := strings.Join(lines[header+2:axis], "\n")
	if !strings.Contains(graph, "█") {
		t.Fatalf("detail timeline graph should render after the spacer row:\n%s", view)
	}
}

func TestFailureCauseHistoryTableAlignsWithLongCheckNames(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel([]watch.Target{}, events)
	at := time.Date(2026, 5, 16, 13, 41, 52, 0, time.UTC)
	m.Now = at.Add(time.Minute)
	target := watch.TargetSnapshot{Name: "debug-a1(5G)", SSID: "SHIZK RADIO"}
	m.apply(watch.Event{
		Time:   at,
		Kind:   watch.EventFinding,
		Round:  2,
		Target: target,
		Step:   watch.StepSnapshot{Name: "Debug Ping Blackhole", Type: "ping", Status: "failed"},
		Status: "failed",
		Finding: &watch.Finding{
			Target:   target.Name,
			Check:    "Debug Ping Blackhole",
			Metric:   "status",
			Observed: "failed",
			Expected: "== ok",
			Message:  "ping failed",
		},
	})
	causes := m.failureCauses()
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
	events := make(chan watch.Event)
	m := newModel([]watch.Target{}, events)
	at := time.Date(2026, 5, 16, 14, 6, 35, 0, time.UTC)
	m.Now = at.Add(time.Minute)
	target := watch.TargetSnapshot{Name: "debug-a1(5G)", SSID: "SHIZK RADIO"}
	m.apply(watch.Event{
		Time:   at,
		Kind:   watch.EventFinding,
		Round:  1,
		Target: target,
		Step:   watch.StepSnapshot{Name: "Debug HTTP Wrong Status", Type: "http", Status: "failed"},
		Status: "failed",
		Finding: &watch.Finding{
			Target:   target.Name,
			Check:    "Debug HTTP Wrong Status",
			Metric:   "elapsed_ms",
			Observed: "45",
			Expected: "<= 1",
			Message:  "constraint failed",
		},
	})
	hotspots := m.failureHotspots()
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

func TestDetailSectionAllocationsPrioritizeHistoryAndUseLogFloor(t *testing.T) {
	sections := []detailSection{
		{Title: "causes", Rows: []string{"  Count  Last      Cause", "      2  14:29:36  constraint failed", "      2  14:29:35  ping failed"}},
		{Title: "failure history", Rows: detailTestRows(17, "  14:29:36  2  Debug HTTP Wrong Status  status  failed  \"== ok\"")},
		{Title: "logs", Rows: detailTestRows(80, "  14:30:37  kind=step_started round=3 status=running target=debug-a2(5G)"), WrapLogs: true},
	}

	allocations := detailSectionAllocations(sections, 40, 160)

	if got, want := allocations[0], detailSectionNaturalAllocation(sections[0], 160); got != want {
		t.Fatalf("causes allocation = %d, want full %d", got, want)
	}
	if got, want := allocations[1], detailSectionNaturalAllocation(sections[1], 160); got != want {
		t.Fatalf("failure history allocation = %d, want full %d", got, want)
	}
	if got, floor := allocations[2], detailLogMinimumAllocation(40); got < floor {
		t.Fatalf("logs allocation = %d, want at least %d", got, floor)
	}
	if got := allocations[0] + allocations[1] + allocations[2]; got != 40 {
		t.Fatalf("allocations should use all section height, got %d: %#v", got, allocations)
	}
}

func TestDetailSectionAllocationsKeepLogsAtFloorWhenHistoryOverflows(t *testing.T) {
	sections := []detailSection{
		{Title: "failure history", Rows: detailTestRows(80, "  14:29:36  2  Debug HTTP Wrong Status  status  failed  \"== ok\"")},
		{Title: "logs", Rows: detailTestRows(80, "  14:30:37  kind=step_started round=3 status=running target=debug-a2(5G)"), WrapLogs: true},
	}

	allocations := detailSectionAllocations(sections, 40, 160)
	logFloor := detailLogMinimumAllocation(40)

	if got := allocations[1]; got != logFloor {
		t.Fatalf("logs should stay at floor while history is overflowing: got=%d floor=%d allocations=%#v", got, logFloor, allocations)
	}
	if got := allocations[0]; got <= allocations[1] {
		t.Fatalf("failure history should receive the remaining height before extra logs: allocations=%#v", allocations)
	}
	if got := allocations[0] + allocations[1]; got != 40 {
		t.Fatalf("allocations should use all section height, got %d: %#v", got, allocations)
	}
}

func detailTestRows(count int, value string) []string {
	rows := make([]string, count)
	for i := range rows {
		rows[i] = value
	}
	return rows
}

func TestFailedCheckDetailModalUsesFixedAppRatio(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel([]watch.Target{}, events)
	modal := m.failedCheckDetailModal(150, 32)

	if got, want := lipgloss.Width(modal), 81; got != want {
		t.Fatalf("modal width = %d, want %d", got, want)
	}
	if got, want := lipgloss.Height(modal), 10; got != want {
		t.Fatalf("modal height = %d, want %d", got, want)
	}
}

func TestDetailModalShowsExpandedLogRows(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel([]watch.Target{}, events)
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

	modalFrame := m.passingCheckDetailModal(150, 60)
	if got, base := lipgloss.Height(modalFrame), detailModalHeight(60); got <= base {
		t.Fatalf("modal should grow beyond base height when logs overflow: got=%d base=%d\n%s", got, base, stripANSI(modalFrame))
	}
	if got, want := lipgloss.Height(modalFrame), detailModalMaxHeight(60); got != want {
		t.Fatalf("overflowing modal height = %d, want cap %d", got, want)
	}
	modal := stripANSI(modalFrame)
	if got := strings.Count(modal, "kind=step_finished"); got < 4 {
		t.Fatalf("expanded modal should show multiple related log rows, got %d:\n%s", got, modal)
	}
	if !strings.Contains(modal, "          band=5ghz") && !strings.Contains(modal, "            band=5ghz") {
		t.Fatalf("expanded modal should wrap log bodies under the body column:\n%s", modal)
	}
}
