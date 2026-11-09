package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/mi2428/dropcheck-agent/controller/internal/tuirun"
)

func TestInspectorSnapshotScrollRefreshAndResize(t *testing.T) {
	m, _ := syntheticTUIProgram(t)
	m.focus = focusPassingChecks
	m.openDetailForPanel(focusPassingChecks)
	before, identity := m.inspector.GetContent(), m.detailPassingKey
	if !strings.Contains(before, "Samples: 20") || !strings.Contains(before, "Logs") {
		t.Fatal("snapshot lost summary or related logs")
	}
	m = updateKey(t, m, tea.Key{Code: tea.KeyPgDown})
	offset := m.inspector.YOffset()
	if offset == 0 {
		t.Fatal("long snapshot did not scroll")
	}
	item := m.filteredPassingCheckSummaries()[m.passingCheckCursor]
	m.apply(tuirun.Event{Kind: tuirun.EventStepFinished, Time: m.Now.Add(time.Second), Round: 2,
		Agent: item.Agent, Target: item.Target, Step: item.Step, Status: "ok"})
	if m.inspector.GetContent() != before || m.inspector.YOffset() != offset || m.detailPassingKey != identity {
		t.Fatal("live event moved the frozen snapshot or its identity")
	}
	for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 24}, {Width: 160, Height: 48}} {
		updated, _ := m.Update(size)
		m = updated.(model)
		if m.inspector.GetContent() != before {
			t.Fatal("resize refreshed frozen text")
		}
		frame := m.View().Content
		if lipgloss.Width(frame) > size.Width || lipgloss.Height(frame) > size.Height {
			t.Fatalf("inspector frame escaped %dx%d", size.Width, size.Height)
		}
	}
	m = updateKey(t, m, tea.Key{Code: 'f', Text: "f"})
	if !m.inspectorFull || m.inspector.GetContent() != before {
		t.Fatal("full screen changed snapshot")
	}
	m = updateKey(t, m, tea.Key{Code: 'r', Text: "r"})
	if !strings.Contains(m.inspector.GetContent(), "Samples: 21") || m.inspector.YOffset() != 0 {
		t.Fatal("explicit refresh did not update snapshot and reset scroll")
	}
	m = updateKey(t, m, tea.Key{Code: tea.KeyEscape})
	if m.detailOpen {
		t.Fatal("Esc did not close inspector")
	}
}

func TestInspectorRetainsAllSummaryFieldsAndVisibleHistoryLimit(t *testing.T) {
	content := detailContent{Fields: []detailField{{Key: "device", Value: "端末東京"}, {Key: "message", Value: strings.Repeat("long message ", 30)}}}
	text := content.inspectorText(40)
	if !strings.Contains(text, "Device: 端末東京") || !strings.Contains(text, strings.Repeat("long message ", 30)) {
		t.Fatal("snapshot clipped or lost a summary value")
	}
	m := frameHistoryModel(16)
	item := m.FailedChecks[0]
	m.FailedChecks = nil
	for i := range detailHistoryLimit + 5 {
		item.When = m.Now.Add(time.Duration(i) * time.Millisecond)
		m.FailedChecks = append(m.FailedChecks, item)
	}
	rows := m.failedCheckDetailRows(m.FailedCheckSummaries()[0], 120)
	if len(rows) != detailHistoryLimit+2 || !strings.Contains(rows[len(rows)-1], "--jsonl") {
		t.Fatalf("history window must disclose its limit, rows=%d", len(rows))
	}
	if len(m.FailedChecks) != detailHistoryLimit+5 {
		t.Fatal("display limit changed retained data")
	}
}

func TestInspectorSummaryUsesSharedSafeProjection(t *testing.T) {
	content := detailContent{Fields: []detailField{
		{Key: "ssid", Value: "password=literal-network-name"},
		{Key: "message", Value: "https://example.test/?token=private-token\n\x1b[2Jnext"},
	}}
	text := content.inspectorText(80)
	if !strings.Contains(text, "SSID: password=literal-network-name") || strings.Contains(text, "private-token") || strings.ContainsAny(text, "\x1b\r") {
		t.Fatalf("unsafe projection or lost literal SSID: %q", text)
	}
	if content.Fields[1].Value != "https://example.test/?token=private-token\n\x1b[2Jnext" {
		t.Fatal("projection mutated retained evidence")
	}
}

func TestSearchPasteAndEveryPanelFocusCycle(t *testing.T) {
	m := frameHistoryModel(16)
	m.focus = focusPassingChecks
	m = updateKey(t, m, tea.Key{Code: '/', Text: "/"})
	updated, _ := m.Update(tea.PasteMsg{Content: "ping-0\n\x1b"})
	m = updated.(model)
	if m.passingSearchQuery != "ping-0" || len(m.filteredPassingCheckSummaries()) != 1 {
		t.Fatal("paste search was not applied safely")
	}
	m = updateKey(t, m, tea.Key{Code: tea.KeyEscape})
	start := m.currentFocusSlot()
	seen := map[focusSlot]bool{}
	for range len(m.focusSlots()) {
		seen[m.currentFocusSlot()] = true
		m = updateKey(t, m, tea.Key{Code: tea.KeyTab})
	}
	if m.currentFocusSlot() != start || len(seen) != len(m.focusSlots()) {
		t.Fatal("Tab did not make one complete panel cycle")
	}
}

func TestResizeDiscardsAnUnconfirmedControl(t *testing.T) {
	m := frameHistoryModel(16)
	m.menu = &actionMenu{confirmation: "cancel", scope: tuirun.Scope{Kind: tuirun.ScopeRun}}
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 39, Height: 13})
	m = updated.(model)
	if m.menu != nil || m.controlBusy {
		t.Fatal("resize retained a hidden destructive confirmation")
	}
	updated, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: 'y', Text: "y"}))
	if cmd != nil || updated.(model).controlBusy {
		t.Fatal("confirmation survived a too-small terminal")
	}
}
