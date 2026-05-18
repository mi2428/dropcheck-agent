package tui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"dropcheck/controller/internal/watch"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

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
	m = updateKey(t, m, tea.Key{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.focus != focusFailedChecks {
		t.Fatalf("shift-tab should move counter-clockwise back to failed checks, got %v", m.focus)
	}
	m = updateKey(t, m, tea.Key{Code: tea.KeyTab})
	if m.focus != focusPassingChecks {
		t.Fatalf("tab should return clockwise to passing checks, got %v", m.focus)
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
	if m.focus != focusCheckStatus {
		t.Fatalf("tab from passing checks should focus check status, got %v", m.focus)
	}
	m.checkStatusOffset = 0
	m = updateKey(t, m, tea.Key{Code: 'l', Text: "l"})
	if m.focus != focusCheckStatus {
		t.Fatalf("l should keep check status focused, got %v", m.focus)
	}
	m = updateKey(t, m, tea.Key{Code: tea.KeyTab})
	if m.focus != focusRunQueue {
		t.Fatalf("tab from check status should focus run queue, got %v", m.focus)
	}
	m = updateKey(t, m, tea.Key{Code: tea.KeyTab})
	m = updateKey(t, m, tea.Key{Code: 'k', Text: "k"})
	if m.focus != focusFailedChecks || m.failedCheckCursor != 0 {
		t.Fatalf("k should move failed checks cursor after focus returns: focus=%v failed=%d", m.focus, m.failedCheckCursor)
	}
}

func TestRunQueueFocusScrollsVertically(t *testing.T) {
	events := make(chan watch.Event)
	m := newModel("shownet-watch", []watch.Target{}, events)
	m.width = 120
	m.height = 16
	for i := range 20 {
		m.Targets = append(m.Targets, targetState{
			Target: targetSnapshot(fmt.Sprintf("target-%02d", i)),
			Status: "pending",
		})
	}
	m.focus = focusRunQueue
	m.updateRunQueueCursor()

	for range 8 {
		m = updateKey(t, m, tea.Key{Code: 'j', Text: "j"})
	}
	if m.runQueueCursor != 8 || m.runQueueOffset == 0 || !m.runQueuePinned {
		t.Fatalf("run queue scroll state cursor=%d offset=%d pinned=%v", m.runQueueCursor, m.runQueueOffset, m.runQueuePinned)
	}
	view := stripANSI(m.runQueuePanelsView(40, 8))
	if !strings.Contains(view, "target-08") {
		t.Fatalf("focused run queue should reveal scrolled cursor:\n%s", view)
	}
}

func TestPauseResumeAndRightAlignedStatusItems(t *testing.T) {
	events := make(chan watch.Event)
	control := watch.NewPauseController()
	m := newModel("shownet-watch", []watch.Target{}, events)
	m.pauseControl = control
	m.width = 120
	m.searchQuery = "hop2"

	m = updateKey(t, m, tea.Key{Code: 'w', Text: "w"})
	if !m.paused || !control.Paused() {
		t.Fatalf("w should pause model and controller: model=%v controller=%v", m.paused, control.Paused())
	}
	status := stripANSI(m.statusBar(120))
	if !strings.HasSuffix(strings.TrimRight(status, " "), "Paused filter=/hop2") {
		t.Fatalf("paused/filter status should be right aligned at end:\n%q", status)
	}
	help := stripANSI(m.helpBar(120))
	if !strings.Contains(help, "Esc=Resume") || strings.Contains(help, "Esc=Clear") {
		t.Fatalf("paused help should prioritize resume:\n%s", help)
	}

	m = updateKey(t, m, tea.Key{Code: tea.KeyEscape})
	if m.paused || control.Paused() {
		t.Fatalf("esc should resume model and controller: model=%v controller=%v", m.paused, control.Paused())
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
		t.Fatalf("anchors should not change visible Text:\n%q\n%q", stripANSI(lines[1]), stripANSI(lines[2]))
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
	m.Now = at
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
	if !m.Now.Equal(at.Add(recencyWarmWindow + time.Second)) {
		t.Fatalf("tick should update model clock, got %s", m.Now)
	}
	rows := m.passingCheckSummaries()
	if len(rows) != 1 {
		t.Fatalf("passing check summaries = %d, want 1", len(rows))
	}
	assertStyleRGB(t, summaryPanelRowStyle(rows[0].Last, m.currentTime()), 0xb4, 0x65, 0x22)
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
		texts = append(texts, line.Text)
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
	for line := range strings.SplitSeq(text, "\n") {
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
