package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/mi2428/dropcheck-agent/controller/internal/tuirun"
)

// actionHarness starts the synthetic TUI runtime and drains its one-slot event
// channel so a control command never blocks on event delivery.
type actionHarness struct {
	t           *testing.T
	m           model
	cancel      context.CancelFunc
	runtimeDone chan error
	drainStop   chan struct{}
	drainDone   chan struct{}
	stopped     bool
}

func newActionHarness(t *testing.T) *actionHarness {
	t.Helper()
	m, execute := syntheticTUIProgram(t)
	ctx, cancel := context.WithCancel(context.Background())
	h := &actionHarness{
		t: t, m: m, cancel: cancel,
		runtimeDone: make(chan error, 1),
		drainStop:   make(chan struct{}),
		drainDone:   make(chan struct{}),
	}
	events := m.events
	go func() { h.runtimeDone <- execute(ctx) }()
	go func() {
		defer close(h.drainDone)
		for {
			select {
			case _, ok := <-events:
				if !ok {
					return
				}
			case <-h.drainStop:
				return
			}
		}
	}()
	// Register cleanup before any fatal path so the runtime and drainer are
	// always collected when the bind wait fails.
	t.Cleanup(h.stop)
	deadline := time.Now().Add(2 * time.Second)
	for !h.m.controls.HasScope(tuirun.Scope{Kind: tuirun.ScopeRun}) {
		if time.Now().After(deadline) {
			t.Fatal("synthetic TUI runtime did not bind controls")
		}
		time.Sleep(2 * time.Millisecond)
	}
	return h
}

func (h *actionHarness) stop() {
	if h.stopped {
		return
	}
	h.stopped = true
	close(h.drainStop)
	h.cancel()
	select {
	case <-h.runtimeDone:
	case <-time.After(2 * time.Second):
		h.t.Error("synthetic TUI runtime did not stop")
	}
	select {
	case <-h.drainDone:
	case <-time.After(time.Second):
		h.t.Error("event drainer did not stop")
	}
}

func (h *actionHarness) press(key tea.Key) tea.Cmd {
	h.t.Helper()
	updated, cmd := h.m.Update(tea.KeyPressMsg(key))
	next, ok := updated.(model)
	if !ok {
		h.t.Fatalf("Update returned %T, want tui.model", updated)
	}
	h.m = next
	return cmd
}

func (h *actionHarness) applyResult(result controlResultMsg) {
	h.t.Helper()
	updated, _ := h.m.Update(result)
	next, ok := updated.(model)
	if !ok {
		h.t.Fatalf("Update returned %T, want tui.model", updated)
	}
	h.m = next
}

// runCmd executes a control command and returns its result. The harness drains
// events concurrently, so a blocking emit inside the command still completes.
func (h *actionHarness) runCmd(cmd tea.Cmd) controlResultMsg {
	h.t.Helper()
	if cmd == nil {
		h.t.Fatal("expected an asynchronous control command, got nil")
	}
	msg := cmd()
	result, ok := msg.(controlResultMsg)
	if !ok {
		h.t.Fatalf("control command returned %T, want controlResultMsg", msg)
	}
	h.applyResult(result)
	return result
}

// itemScopeIndex returns the index of the picker item that selects scope.
func itemScopeIndex(items []list.Item, scope tuirun.Scope) int {
	for i, item := range items {
		if action, ok := item.(actionItem); ok && action.scope == scope {
			return i
		}
	}
	return -1
}

func checkScopes(t *testing.T, m model) (main, peer tuirun.Scope) {
	t.Helper()
	if len(m.Targets) == 0 {
		t.Fatal("synthetic fixture has no targets")
	}
	first := m.Targets[0]
	if first.Agent.ID == "" || first.Target.ID == "" || len(first.PlannedSteps) == 0 {
		t.Fatalf("synthetic target lacks identity: agent=%q target=%q steps=%d", first.Agent.ID, first.Target.ID, len(first.PlannedSteps))
	}
	main = tuirun.Scope{Kind: tuirun.ScopeCheck, AgentID: first.Agent.ID, TargetID: first.Target.ID, CheckID: first.PlannedSteps[0].ID}
	for _, target := range m.Targets[1:] {
		if target.Target.ID != main.TargetID || target.Agent.ID == main.AgentID || len(target.PlannedSteps) == 0 {
			continue
		}
		peer = tuirun.Scope{Kind: tuirun.ScopeCheck, AgentID: target.Agent.ID, TargetID: target.Target.ID, CheckID: target.PlannedSteps[0].ID}
		return main, peer
	}
	t.Fatal("synthetic fixture did not bind the same check to a second agent")
	return main, peer
}

func TestScopePickerSeparatesIdenticalChecksByStableIdentity(t *testing.T) {
	h := newActionHarness(t)
	mainCheck, peerCheck := checkScopes(t, h.m)
	if mainCheck.CheckID != peerCheck.CheckID {
		t.Fatalf("fixture check IDs differ: %q vs %q", mainCheck.CheckID, peerCheck.CheckID)
	}

	runScope := tuirun.Scope{Kind: tuirun.ScopeRun}
	agentScope := tuirun.Scope{Kind: tuirun.ScopeAgent, AgentID: mainCheck.AgentID}
	targetScope := tuirun.Scope{Kind: tuirun.ScopeTarget, AgentID: mainCheck.AgentID, TargetID: mainCheck.TargetID}

	// Every scope level must be selectable and must carry the exact identity.
	for _, scope := range []tuirun.Scope{runScope, agentScope, targetScope, mainCheck, peerCheck} {
		if cmd := h.press(tea.Key{Code: 'a', Text: "a"}); cmd != nil {
			t.Fatalf("opening the scope picker returned a command for %v", scope)
		}
		if h.m.menu == nil {
			t.Fatalf("scope picker did not open for %v", scope)
		}
		index := itemScopeIndex(h.m.scopeItems(), scope)
		if index < 0 {
			t.Fatalf("scope picker is missing %v", scope)
		}
		h.m.menu.list.Select(index)
		if cmd := h.press(tea.Key{Code: tea.KeyEnter}); cmd != nil {
			t.Fatalf("selecting scope %v returned a command", scope)
		}
		if h.m.menu == nil {
			t.Fatalf("selecting scope %v closed the picker", scope)
		}
		if h.m.menu.scope != scope {
			t.Fatalf("picker selected %v, want %v", h.m.menu.scope, scope)
		}
		h.press(tea.Key{Code: tea.KeyEscape})
		if h.m.menu != nil {
			t.Fatalf("Esc did not close the picker for %v", scope)
		}
	}

	// Cancel confirmation: Enter, Esc and N never execute; only Y does.
	openCancelConfirmation := func() {
		t.Helper()
		h.press(tea.Key{Code: 'a', Text: "a"})
		if h.m.menu == nil {
			t.Fatal("scope picker did not open")
		}
		index := itemScopeIndex(h.m.scopeItems(), mainCheck)
		if index < 0 {
			t.Fatal("check scope missing from picker")
		}
		h.m.menu.list.Select(index)
		h.press(tea.Key{Code: tea.KeyEnter})
		cancelIndex := -1
		for i, item := range h.m.menu.list.Items() {
			if action, ok := item.(actionItem); ok && action.action == "cancel" {
				cancelIndex = i
			}
		}
		if cancelIndex < 0 {
			t.Fatal("cancel action missing from action list")
		}
		h.m.menu.list.Select(cancelIndex)
		h.press(tea.Key{Code: tea.KeyEnter})
		if h.m.menu == nil || h.m.menu.confirmation != "cancel" {
			t.Fatalf("cancel confirmation not shown: menu=%v", h.m.menu)
		}
	}

	openCancelConfirmation()
	if cmd := h.press(tea.Key{Code: tea.KeyEnter}); cmd != nil {
		t.Fatal("Enter must not execute a pending confirmation")
	}
	if h.m.menu == nil || h.m.menu.confirmation != "cancel" {
		t.Fatal("Enter must keep the confirmation open")
	}
	if h.m.controls.Canceled(mainCheck) {
		t.Fatal("Enter executed the cancel despite no confirmation")
	}

	h.press(tea.Key{Code: tea.KeyEscape})
	if h.m.menu != nil {
		t.Fatal("Esc did not close the confirmation")
	}
	if h.m.controls.Canceled(mainCheck) || h.m.controls.Canceled(peerCheck) {
		t.Fatal("closing the confirmation canceled a scope")
	}

	openCancelConfirmation()
	if cmd := h.press(tea.Key{Code: 'n', Text: "n"}); cmd != nil {
		t.Fatal("N must not execute a pending confirmation")
	}
	if h.m.menu != nil {
		t.Fatal("N did not close the confirmation")
	}
	if h.m.controls.Canceled(mainCheck) {
		t.Fatal("N executed the cancel")
	}

	openCancelConfirmation()
	result := h.runCmd(h.press(tea.Key{Code: 'y', Text: "y"}))
	if result.err != nil {
		t.Fatalf("cancel command failed: %v", result.err)
	}
	if result.action != "cancel" || result.scope != mainCheck {
		t.Fatalf("cancel command result = %q/%v, want cancel/%v", result.action, result.scope, mainCheck)
	}
	if !h.m.controls.Canceled(mainCheck) {
		t.Fatal("Y did not cancel the selected check scope")
	}
	if h.m.controls.Canceled(peerCheck) {
		t.Fatal("canceling one agent's check leaked into the same check on another agent")
	}
}

func TestControlRejectsChildResumeUnderPausedParentAndDeadScopes(t *testing.T) {
	h := newActionHarness(t)
	checkScope, otherCheck := checkScopes(t, h.m)
	runScope := tuirun.Scope{Kind: tuirun.ScopeRun}

	pauseResult := h.runCmd(h.m.requestControl("pause", runScope, "All agents"))
	if pauseResult.err != nil {
		t.Fatalf("run pause failed: %v", pauseResult.err)
	}
	if !h.m.controls.Paused(runScope) {
		t.Fatal("run pause was not recorded")
	}

	logsBefore := len(h.m.Logs)
	if cmd := h.m.requestControl("resume", checkScope, "check"); cmd != nil {
		t.Fatal("child resume must not be requested while the run parent is paused")
	}
	if len(h.m.Logs) == logsBefore || !strings.Contains(strings.Join(h.m.Logs[logsBefore:], "\n"), "Resume rejected") {
		t.Fatalf("child resume rejection was not reported: %v", h.m.Logs[logsBefore:])
	}
	if !h.m.controls.Paused(runScope) {
		t.Fatal("rejected child resume cleared the parent pause")
	}

	if err := h.m.controls.Cancel(checkScope); err != nil {
		t.Fatalf("cancel check scope: %v", err)
	}
	if cmd := h.m.requestControl("pause", checkScope, "check"); cmd != nil {
		t.Fatal("a canceled scope must not accept a new control")
	}
	if cmd := h.m.requestControl("pause", tuirun.Scope{Kind: tuirun.ScopeCheck, AgentID: "ghost", TargetID: checkScope.TargetID, CheckID: checkScope.CheckID}, "ghost"); cmd != nil {
		t.Fatal("an unknown scope must not accept a control")
	}

	// Once the run finishes, even a scope that was valid earlier fails closed.
	h.stop()
	if h.m.controls.HasScope(runScope) {
		t.Fatal("finished controls still accept the run scope")
	}
	if cmd := h.m.requestControl("pause", otherCheck, "check"); cmd != nil {
		t.Fatal("a finished run must not accept a control")
	}
}

func TestSkipConfirmationRequiresExplicitYes(t *testing.T) {
	h := newActionHarness(t)
	checkScope, _ := checkScopes(t, h.m)

	openSkip := func() {
		t.Helper()
		if cmd := h.press(tea.Key{Code: 'n', Mod: tea.ModCtrl}); cmd != nil {
			t.Fatal("Ctrl-N returned a command before confirmation")
		}
		if h.m.menu == nil || h.m.menu.intent != "skip" {
			t.Fatalf("Ctrl-N did not open the skip scope picker: %+v", h.m.menu)
		}
		index := itemScopeIndex(h.m.scopeItems(), checkScope)
		if index < 0 {
			t.Fatal("check scope missing from skip picker")
		}
		h.m.menu.list.Select(index)
		h.press(tea.Key{Code: tea.KeyEnter})
		if h.m.menu == nil || h.m.menu.confirmation != "skip" {
			t.Fatalf("skip confirmation not shown: %+v", h.m.menu)
		}
	}

	openSkip()
	if cmd := h.press(tea.Key{Code: tea.KeyEnter}); cmd != nil {
		t.Fatal("Enter must not execute a pending skip")
	}
	if h.m.menu == nil || h.m.menu.confirmation != "skip" {
		t.Fatal("Enter must keep the skip confirmation open")
	}
	if h.m.controlAction == "skip" || h.m.controlBusy {
		t.Fatal("Enter executed the skip")
	}

	h.press(tea.Key{Code: tea.KeyEscape})
	if h.m.menu != nil {
		t.Fatal("Esc did not close the skip confirmation")
	}
	if h.m.controlAction == "skip" || h.m.controlBusy {
		t.Fatal("Esc executed the skip")
	}

	openSkip()
	if cmd := h.press(tea.Key{Code: 'n', Text: "n"}); cmd != nil {
		t.Fatal("N must not execute a pending skip")
	}
	if h.m.menu != nil {
		t.Fatal("N did not close the skip confirmation")
	}
	if h.m.controlAction == "skip" || h.m.controlBusy {
		t.Fatal("N executed the skip")
	}

	openSkip()
	result := h.runCmd(h.press(tea.Key{Code: 'y', Text: "y"}))
	if result.err != nil {
		t.Fatalf("skip command failed: %v", result.err)
	}
	if result.action != "skip" || result.scope != checkScope {
		t.Fatalf("skip command result = %q/%v, want skip/%v", result.action, result.scope, checkScope)
	}
}

func TestCtrlZUpdateReturnsWhileTheEventChannelIsFull(t *testing.T) {
	m, execute := syntheticTUIProgram(t)
	ctx, cancel := context.WithCancel(context.Background())
	runtimeDone := make(chan error, 1)
	go func() { runtimeDone <- execute(ctx) }()
	defer func() {
		cancel()
		select {
		case <-runtimeDone:
		case <-time.After(2 * time.Second):
			t.Error("synthetic TUI runtime did not stop")
		}
	}()

	events := m.events
	deadline := time.Now().Add(2 * time.Second)
	for !m.controls.HasScope(tuirun.Scope{Kind: tuirun.ScopeRun}) {
		if time.Now().After(deadline) {
			t.Fatal("synthetic TUI runtime did not bind controls")
		}
		time.Sleep(2 * time.Millisecond)
	}

	// The fixture channel has capacity 1. Wait until it is genuinely full so the
	// producer is blocked, then prove Update still returns promptly without doing
	// any channel I/O. The bound is well below the 1s delivery timeout used below.
	fullDeadline := time.Now().Add(2 * time.Second)
	for len(events) != cap(events) || cap(events) == 0 {
		if time.Now().After(fullDeadline) {
			t.Fatal("synthetic event channel never became full")
		}
		time.Sleep(time.Millisecond)
	}
	start := time.Now()
	updated, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: 'z', Mod: tea.ModCtrl}))
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("Update blocked for %s on a full event channel", elapsed)
	}
	next, ok := updated.(model)
	if !ok {
		t.Fatalf("Update returned %T, want tui.model", updated)
	}
	m = next
	if cmd == nil {
		t.Fatal("Ctrl-Z returned no control command")
	}

	// Run the command on its own goroutine while draining events, mirroring the
	// real program loop. Delivery must succeed and the run must stay alive.
	result := make(chan tea.Msg, 1)
	go func() { result <- cmd() }()
	drainStop := make(chan struct{})
	drainDone := make(chan struct{})
	go func() {
		defer close(drainDone)
		for {
			select {
			case _, ok := <-events:
				if !ok {
					return
				}
			case <-drainStop:
				return
			}
		}
	}()
	defer func() {
		close(drainStop)
		select {
		case <-drainDone:
		case <-time.After(time.Second):
			t.Error("event drainer did not stop")
		}
	}()

	select {
	case msg := <-result:
		controlResult, ok := msg.(controlResultMsg)
		if !ok {
			t.Fatalf("control command returned %T, want controlResultMsg", msg)
		}
		if controlResult.err != nil {
			t.Fatalf("Ctrl-Z pause failed: %v", controlResult.err)
		}
		if controlResult.action != "pause" {
			t.Fatalf("Ctrl-Z action = %q, want pause", controlResult.action)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("control command was not delivered while events flowed")
	}

	select {
	case err := <-runtimeDone:
		t.Fatalf("run finished instead of staying alive: %v", err)
	default:
	}
	if !m.controls.Paused(tuirun.Scope{Kind: tuirun.ScopeRun}) {
		t.Fatal("the run was not paused after a delivered Ctrl-Z command")
	}
}
