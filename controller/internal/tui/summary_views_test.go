package tui

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"dropcheck/controller/internal/watch"

	tea "charm.land/bubbletea/v2"
)

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
	m.Now = at.Add(5 * time.Second)
	target := watch.TargetSnapshot{Name: "SHIZK RADIO", SSID: "SHIZK RADIO"}
	for i := range 3 {
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
		t.Fatalf("summary panels should use grouped bar lists, not a Bar column:\npassing Checks:\n%s\nfailed Checks:\n%s", passingChecks, failedChecks)
	}
	if strings.Contains(passingChecks, " / ") || strings.Contains(failedChecks, " / ") {
		t.Fatalf("summary panels should use aligned columns, not slash-delimited labels:\npassing Checks:\n%s\nfailed Checks:\n%s", passingChecks, failedChecks)
	}
	if strings.Contains(passingTable, "█") || strings.Contains(failedTable, "█") {
		t.Fatalf("summary tables should not spend columns on horizontal count bars:\npassing Checks:\n%s\nfailed Checks:\n%s", passingChecks, failedChecks)
	}
	if strings.Contains(passingTable, "▏") || strings.Contains(failedTable, "▏") {
		t.Fatalf("summary tables should not render ambiguous tick glyphs:\npassing Checks:\n%s\nfailed Checks:\n%s", passingChecks, failedChecks)
	}
	if !strings.Contains(passingChecks, "passing checks events last=30m") || !strings.Contains(failedChecks, "failed checks events last=30m") {
		t.Fatalf("summary panels should pin event sparklines at the bottom:\npassing Checks:\n%s\nfailed Checks:\n%s", passingChecks, failedChecks)
	}
	if !strings.Contains(failedChecks, "09:30:04") {
		t.Fatalf("failedChecks should keep the last-seen time:\n%s", failedChecks)
	}
}

func TestFailureHotspotsRankTargetsByStreakRateCount(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", []watch.Target{}, events)
	at := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	m.Now = at.Add(5 * time.Minute)
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
	if got := []string{rows[0].Target.Name, rows[1].Target.Name, rows[2].Target.Name}; !reflect.DeepEqual(got, []string{"radio-a", "radio-c", "radio-b"}) {
		t.Fatalf("hotspots sorted incorrectly: %#v rows=%#v", got, rows)
	}
	if rows[0].FailStreak != 2 || rows[0].FailCount != 2 || rows[0].RunCount != 2 || rows[0].FailRunCount != 2 {
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
	m.Now = at.Add(time.Minute)
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
		t.Fatalf("same target failures from different agents should stay separate, got %d Rows: %#v", got, rows)
	}
	for _, row := range rows {
		if row.FailCount != 1 || row.RunCount != 1 || agentKey(row.Agent) == "" {
			t.Fatalf("hotspot row should retain per-agent stats: %#v", row)
		}
	}
	view := stripANSI(m.failureHotspotPanelsView(96, 14))
	agentATitle := lineIndex(view, "Failure Hotspots agent-a")
	agentBTitle := lineIndex(view, "Failure Hotspots agent-b")
	agentACause := lineIndex(view, "agent-a timeout")
	agentBCause := lineIndex(view, "agent-b timeout")
	if agentATitle < 0 || agentBTitle < 0 || agentBTitle <= agentATitle {
		t.Fatalf("hotspot panels should be stacked by Agent:\n%s", view)
	}
	if agentACause <= agentATitle || agentACause >= agentBTitle {
		t.Fatalf("agent-a hotspot should render inside agent-a Panel: title=%d cause=%d next=%d\n%s", agentATitle, agentACause, agentBTitle, view)
	}
	if agentBCause <= agentBTitle {
		t.Fatalf("agent-b hotspot should render inside agent-b Panel: title=%d cause=%d\n%s", agentBTitle, agentBCause, view)
	}

	m.focus = focusFailedChecks
	m = updateKey(t, m, tea.Key{Code: tea.KeyTab})
	if m.focus != focusFailureHotspots || m.focusHotspotAgentKey != roundAgentKey(agents[0]) {
		t.Fatalf("tab from failed checks should focus first hotspot agent panel: focus=%v hotspot=%q", m.focus, m.focusHotspotAgentKey)
	}
	m = updateKey(t, m, tea.Key{Code: tea.KeyTab})
	if m.focus != focusFailureHotspots || m.focusHotspotAgentKey != roundAgentKey(agents[1]) {
		t.Fatalf("second tab should focus second hotspot agent panel: focus=%v hotspot=%q", m.focus, m.focusHotspotAgentKey)
	}
	m = updateKey(t, m, tea.Key{Code: tea.KeyTab})
	if m.focus != focusRunQueue || m.focusRunQueueAgentKey != roundAgentKey(agents[0]) {
		t.Fatalf("third tab should focus first run queue agent panel: focus=%v runQueue=%q", m.focus, m.focusRunQueueAgentKey)
	}
	m = updateKey(t, m, tea.Key{Code: tea.KeyTab})
	if m.focus != focusRunQueue || m.focusRunQueueAgentKey != roundAgentKey(agents[1]) {
		t.Fatalf("fourth tab should focus second run queue agent panel: focus=%v runQueue=%q", m.focus, m.focusRunQueueAgentKey)
	}
	m = updateKey(t, m, tea.Key{Code: tea.KeyTab})
	if m.focus != focusCheckStatus {
		t.Fatalf("fifth tab should focus check status, got %v", m.focus)
	}
	m = updateKey(t, m, tea.Key{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.focus != focusRunQueue || m.focusRunQueueAgentKey != roundAgentKey(agents[1]) {
		t.Fatalf("shift-tab should reverse to second run queue agent panel: focus=%v runQueue=%q", m.focus, m.focusRunQueueAgentKey)
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
	m.Now = at.Add(time.Minute)
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

	m.focus = focusFailedChecks
	m = updateKey(t, m, tea.Key{Code: tea.KeyTab})
	if m.focusHotspotAgentKey != roundAgentKey(agents[0]) {
		t.Fatalf("expected first hotspot agent focus, got %q", m.focusHotspotAgentKey)
	}
	if got := m.filteredFailureHotspots()[m.failureHotspotCursor].Target.Name; got != "radio-a2" {
		t.Fatalf("first focused agent cursor = %q, want radio-a2", got)
	}
	m = updateKey(t, m, tea.Key{Code: 'j', Text: "j"})
	if got := m.filteredFailureHotspots()[m.failureHotspotCursor].Target.Name; got != "radio-a1" {
		t.Fatalf("j should stay within agent-a hotspot rows, got %q", got)
	}
	m = updateKey(t, m, tea.Key{Code: tea.KeyTab})
	if m.focusHotspotAgentKey != roundAgentKey(agents[1]) {
		t.Fatalf("expected second hotspot agent focus, got %q", m.focusHotspotAgentKey)
	}
	if got := m.filteredFailureHotspots()[m.failureHotspotCursor].Target.Name; got != "radio-b1" {
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
	if m.focus != focusRunQueue {
		t.Fatalf("tab from failure hotspots should focus run queue, got %v", m.focus)
	}
	m = updateKey(t, m, tea.Key{Code: tea.KeyTab})
	if m.focus != focusCheckStatus {
		t.Fatalf("tab from run queue should focus check status, got %v", m.focus)
	}
	m = updateKey(t, m, tea.Key{Code: tea.KeyTab})
	if m.focus != focusPassingChecks {
		t.Fatalf("tab from check status should focus passing checks, got %v", m.focus)
	}
	m = updateKey(t, m, tea.Key{Code: tea.KeyTab})
	if m.focus != focusFailedChecks {
		t.Fatalf("tab from passing checks should focus failed checks, got %v", m.focus)
	}
	m = updateKey(t, m, tea.Key{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.focus != focusPassingChecks {
		t.Fatalf("shift-tab from failed checks should focus passing checks, got %v", m.focus)
	}
}

func TestEnterShowsFailureHotspotDetail(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", []watch.Target{}, events)
	m.width = 220
	m.height = 50
	at := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	m.Now = at.Add(2 * time.Minute)
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

	m.focus = focusRunQueue
	m = updateKey(t, m, tea.Key{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.focus != focusFailureHotspots {
		t.Fatalf("shift-tab from run queue should focus hotspot panel before opening detail, got %v", m.focus)
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
	m.Now = at.Add(2 * time.Minute)
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
	if got := m.filteredFailureHotspots()[m.failureHotspotCursor].Target.Name; got != "radio-b" {
		t.Fatalf("opened hotspot row = %q, want radio-b", got)
	}

	addFail(3, at.Add(time.Minute), targetA)

	rows := m.filteredFailureHotspots()
	if got := rows[m.failureHotspotCursor].Target.Name; got != "radio-b" {
		t.Fatalf("hotspot cursor should follow opened item after re-sort, got %q rows=%#v", got, rows)
	}
	view := stripANSI(m.failureHotspotDetailView(100, 12))
	if !strings.Contains(view, "target=radio-b") {
		t.Fatalf("hotspot detail modal should keep showing opened Item:\n%s", view)
	}
}
