package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/harness"
	"dropcheck/controller/internal/harness/ping"
	"dropcheck/controller/internal/runner"

	tea "charm.land/bubbletea/v2"
)

const workflowTestSecret = "synthetic-private-psk"

type workflowFake struct {
	mu               sync.Mutex
	operations       []string
	pingCalls        int
	transportFailure bool
	blockPing        bool
	blockAfter       int
	cleanupFailure   bool
	lostAgent        bool
	started          chan string
	canceled         chan string
	releasePing      chan struct{}
}

func (f *workflowFake) Run(ctx context.Context, agent control.AgentInfo, op command.Operation) (runner.Result, error) {
	f.mu.Lock()
	f.operations = append(f.operations, agent.ID+":"+op.Name)
	failure, block, cleanup, lost := false, f.blockPing, f.cleanupFailure, f.lostAgent
	if op.Name == "ping" {
		f.pingCalls++
		failure = f.transportFailure && f.pingCalls == 1
		block = block || f.blockAfter > 0 && f.pingCalls >= f.blockAfter
	}
	f.mu.Unlock()
	if lost {
		return runner.Result{}, fmt.Errorf("selected agent disconnected")
	}
	if op.Name == "ping" {
		if f.started != nil {
			f.started <- agent.ID
		}
		if block {
			select {
			case <-ctx.Done():
				if f.canceled != nil {
					f.canceled <- agent.ID
				}
				return runner.Result{}, ctx.Err()
			case <-f.releasePing:
			}
		}
		if failure {
			return runner.Result{}, fmt.Errorf("synthetic transport failure %s", workflowTestSecret)
		}
	}
	raw := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK, Message: "synthetic result " + workflowTestSecret}
	switch op.Name {
	case "wifi.connect":
		raw.Payload = &controlpb.CommandResult_ConnectWifi{ConnectWifi: &controlpb.ConnectWifiResult{Ssid: "Lab", Connected: true}}
	case "wifi.wait":
		raw.Payload = &controlpb.CommandResult_WifiAssert{WifiAssert: &controlpb.WifiAssertResult{Passed: true}}
	case "ping":
		raw.Payload = &controlpb.CommandResult_Ping{Ping: &controlpb.PingResult{Host: "8.8.8.8", Count: 5, Transmitted: 5, Received: 5}}
	case "ip.status":
		raw.Payload = &controlpb.CommandResult_IpStatus{IpStatus: &controlpb.IpStatus{Validated: true, Internet: true}}
	case "wifi.forget", "wifi.disconnect":
		raw.Payload = &controlpb.CommandResult_WifiOperation{WifiOperation: &controlpb.WifiOperationResult{}}
		if cleanup {
			return runner.Result{}, fmt.Errorf("synthetic cleanup failure")
		}
	}
	return runner.Result{Operation: op, Result: raw}, nil
}

func workflowAgents() []control.AgentInfo {
	var agents []control.AgentInfo
	for _, id := range []string{"agent-a", "agent-b"} {
		agents = append(agents, control.AgentInfo{ID: id, Hello: &controlpb.AgentHello{AdbSerial: "sim-" + id, Device: &controlpb.DeviceInfo{Model: "Synthetic"}, Capabilities: []string{"wifi.connect", "wifi.wait", "wifi.forget", "wifi.disconnect", "ping", "ip.status", "wifi.capabilities"}}})
	}
	return agents
}

func TestWorkflowSinkRespectsDeliveryDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	sink := workflowSink{uiCtx: context.Background(), events: make(chan harness.Event)}
	if err := sink.Emit(ctx, harness.Event{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked UI delivery error = %v, want deadline", err)
	}
}

func TestWorkflowFailedRerunDoesNotBroadenAgentCheckPairs(t *testing.T) {
	fake := &workflowFake{}
	plan := workflowPlan()
	plan.Checks = append(plan.Checks, harness.IPStatus())
	m := workflowFixture(t, plan, fake, workflowAgents())
	m.phase = workflowReview
	m.runs = []retainedRun{{report: harness.Report{Steps: []harness.StepReport{
		{Scope: harness.Scope{Kind: harness.ScopeCheck, AgentID: "agent-a", TargetID: "target/0", CheckID: "check/0"}, Outcome: harness.FailOutcome},
		{Scope: harness.Scope{Kind: harness.ScopeCheck, AgentID: "agent-b", TargetID: "target/0", CheckID: "check/1"}, Outcome: harness.MissingOutcome},
	}}, compiled: m.compiled}}
	m.prepareRerun(true)
	if m.phase != workflowPreview || len(m.selection.Scopes) != 2 {
		t.Fatalf("exact failed rerun not previewed: phase=%s message=%s selection=%+v", m.phase, m.message, m.selection)
	}
	if err := m.preflight(); err != nil {
		t.Fatal(err)
	}
	report, err := harness.Execute(context.Background(), m.selected, fake, harness.ExecuteOptions{Rounds: 1})
	if err != nil || !report.Passed() {
		t.Fatalf("exact rerun failed: %v (%+v)", err, report.Outcome)
	}
	fake.mu.Lock()
	operations := slices.Clone(fake.operations)
	fake.mu.Unlock()
	for _, forbidden := range []string{"agent-a:ip.status", "agent-b:ping"} {
		if slices.Contains(operations, forbidden) {
			t.Fatalf("failed rerun expanded to previously passed work %q: %v", forbidden, operations)
		}
	}
	for _, required := range []string{"agent-a:ping", "agent-b:ip.status"} {
		if !slices.Contains(operations, required) {
			t.Fatalf("failed rerun omitted %q: %v", required, operations)
		}
	}
}

func workflowPlan() harness.Plan {
	return harness.Plan{Name: "日本語 scenario " + workflowTestSecret, Networks: []harness.Network{harness.WiFi("lab").SSID("Lab").PSK(workflowTestSecret).ForgetAfter(true)}, Checks: []harness.Check{harness.Ping("8.8.8.8").Count(5).Expect(ping.Received().Eq(5))}}
}

func workflowFixture(t *testing.T, plan harness.Plan, fake *workflowFake, agents []control.AgentInfo) workflowModel {
	t.Helper()
	compiled, err := harness.Compile(plan, agents)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	lifetime := &workflowLifetime{}
	t.Cleanup(func() { cancel(); lifetime.stop() })
	return newWorkflow(ctx, WorkflowOptions{Compiled: compiled, Runner: fake}, lifetime)
}

func workflowKey(t *testing.T, m workflowModel, key tea.Key) workflowModel {
	t.Helper()
	updated, _ := m.Update(tea.KeyPressMsg(key))
	return updated.(workflowModel)
}

func pumpWorkflow(t *testing.T, m *workflowModel, done func(workflowModel) bool) {
	t.Helper()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for !done(*m) {
		if m.active == nil {
			t.Fatalf("workflow became inactive before condition: phase=%s message=%s", m.phase, m.message)
		}
		run := m.active
		select {
		case event, ok := <-run.events:
			var msg tea.Msg
			if ok {
				msg = workflowEvent{run, event}
			} else {
				msg = <-run.result
			}
			updated, _ := m.Update(msg)
			*m = updated.(workflowModel)
		case <-deadline.C:
			t.Fatal("workflow fake execution did not reach its condition")
		}
	}
}

func TestWorkflowSelectionPreviewFiniteTransportHistoryAndRerun(t *testing.T) {
	fake := &workflowFake{transportFailure: true}
	m := workflowFixture(t, workflowPlan(), fake, workflowAgents()[:1])
	if len(fake.operations) != 0 {
		t.Fatal("startup performed operations")
	}
	m = workflowKey(t, m, tea.Key{Code: tea.KeyEnter})
	if m.phase != workflowPreview || len(fake.operations) != 0 {
		t.Fatalf("preview mutated operations: phase=%s", m.phase)
	}
	frame := m.renderWorkflow()
	if strings.Contains(frame, workflowTestSecret) {
		t.Fatal("preview leaked credentials")
	}
	if !strings.Contains(strings.Join(workflowPreviewLines(m.selected.Preview()), "\n"), "expected=") {
		t.Fatal("preview omitted core expectations")
	}
	m = workflowKey(t, m, tea.Key{Code: tea.KeyEnter})
	pumpWorkflow(t, &m, func(m workflowModel) bool { return m.phase == workflowReview })
	if len(m.runs) != 1 || m.active != nil {
		t.Fatal("finite completion did not retain review")
	}
	first := m.runs[0].report
	if first.Counts.Failed == 0 || len(m.runs[0].dashboard.FailedChecks) == 0 {
		t.Fatalf("transport failure missing from report/reducer history: %+v", first.Counts)
	}
	snapshot, _ := json.Marshal(first)
	m = workflowKey(t, m, tea.Key{Code: 'f', Text: "f"})
	if m.phase != workflowPreview {
		t.Fatalf("failed rerun not previewed: %s %s", m.phase, m.message)
	}
	m = workflowKey(t, m, tea.Key{Code: tea.KeyEnter})
	pumpWorkflow(t, &m, func(m workflowModel) bool { return m.phase == workflowReview })
	if len(m.runs) != 2 || first.RunID == m.runs[1].report.RunID || !m.runs[1].report.Passed() {
		t.Fatal("rerun did not produce an independent passing report")
	}
	after, _ := json.Marshal(m.runs[0].report)
	if string(snapshot) != string(after) {
		t.Fatal("rerun mutated old report")
	}
	fake.mu.Lock()
	operations := slices.Clone(fake.operations)
	fake.mu.Unlock()
	want := []string{"agent-a:wifi.connect", "agent-a:wifi.wait", "agent-a:ping", "agent-a:wifi.disconnect", "agent-a:wifi.forget", "agent-a:wifi.connect", "agent-a:wifi.wait", "agent-a:ping", "agent-a:wifi.disconnect", "agent-a:wifi.forget"}
	if !slices.Equal(operations, want) {
		t.Fatalf("prerequisite/cleanup sequence=%v want=%v", operations, want)
	}
	for i, step := range m.runs[1].report.Steps {
		if step.Scope.CheckID == "check/0" {
			m.stepCursor = i
		}
	}
	m.inspect, m.detail = true, true
	text := strings.Join(workflowReportLines(m.currentReport(), m.stepCursor, true), "\n")
	for _, want := range []string{"observed=", "expected=", "attempt=1", "Cleanup"} {
		if !strings.Contains(text, want) {
			t.Fatalf("success inspect missing %q", want)
		}
	}
	if strings.Contains(text, workflowTestSecret) {
		t.Fatal("report/typed payload leaked credential")
	}
	m = workflowKey(t, m, tea.Key{Code: 'r', Text: "r"})
	if m.phase != workflowPreview {
		t.Fatalf("selected rerun not prerequisite-safe: %s", m.message)
	}
}

func TestWorkflowInvalidSelectionAndEmptySecretLoadNeverStart(t *testing.T) {
	fake := &workflowFake{}
	m := workflowFixture(t, workflowPlan(), fake, workflowAgents()[:1])
	m.selection.AgentIDs = nil
	m = workflowKey(t, m, tea.Key{Code: tea.KeyEnter})
	if m.phase != workflowSelect || m.message == "" || len(fake.operations) != 0 {
		t.Fatal("invalid selection performed work")
	}
	updated, _ := m.Update(workflowLoaded{err: fmt.Errorf("secret unavailable")})
	m = updated.(workflowModel)
	if len(fake.operations) != 0 || !strings.Contains(m.message, "secret unavailable") {
		t.Fatal("invalid Plan load started work")
	}
}

func TestWorkflowSelectedScopeCancelsExecutingContextNotOtherAgent(t *testing.T) {
	for _, stroke := range []string{"x", "s"} {
		t.Run(stroke, func(t *testing.T) {
			fake := &workflowFake{blockPing: true, started: make(chan string, 2), canceled: make(chan string, 2)}
			m := workflowFixture(t, workflowPlan(), fake, workflowAgents())
			m = workflowKey(t, m, tea.Key{Code: tea.KeyEnter})
			m = workflowKey(t, m, tea.Key{Code: tea.KeyEnter})
			scope := harness.Scope{Kind: harness.ScopeCheck, AgentID: "agent-a", TargetID: "target/0", CheckID: "check/0"}
			pumpWorkflow(t, &m, func(m workflowModel) bool { return slices.Contains(m.scopes, scope) })
			for i := 0; i < 2; i++ {
				select {
				case <-fake.started:
				case <-time.After(5 * time.Second):
					t.Fatal("both fake operations did not start")
				}
			}
			// Ordinary dashboard focus/navigation does not grant a control scope.
			m = workflowKey(t, m, tea.Key{Code: tea.KeyTab})
			m = workflowKey(t, m, tea.Key{Code: rune(stroke[0]), Text: stroke})
			select {
			case <-fake.canceled:
				t.Fatal("focus alone canceled an operation")
			default:
			}
			m = workflowKey(t, m, tea.Key{Code: 'v', Text: "v"})
			m.scopeIndex = slices.Index(m.scopes, scope)
			if m.scopeIndex < 0 {
				t.Fatal("explicit check scope unavailable")
			}
			m = workflowKey(t, m, tea.Key{Code: tea.KeyEnter})
			m = workflowKey(t, m, tea.Key{Code: rune(stroke[0]), Text: stroke})
			select {
			case id := <-fake.canceled:
				if id != "agent-a" {
					t.Fatalf("wrong executing context canceled: %s", id)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("selected cancel did not reach executing context")
			}
			select {
			case id := <-fake.canceled:
				t.Fatalf("other agent was canceled: %s", id)
			default:
			}
			m = workflowKey(t, m, tea.Key{Code: 'c', Mod: tea.ModCtrl})
			if m.phase != workflowStopping {
				t.Fatal("run cancellation quit instead of safe cleanup")
			}
			pumpWorkflow(t, &m, func(m workflowModel) bool { return m.phase == workflowReview })
			if len(m.runs[0].report.Cleanup) != 4 {
				t.Fatalf("multi-agent cleanup=%d", len(m.runs[0].report.Cleanup))
			}
		})
	}
}

func TestWorkflowFiniteNRecoveryCleanupAndNarrowSanitation(t *testing.T) {
	fake := &workflowFake{transportFailure: true, cleanupFailure: true}
	plan := workflowPlan()
	plan.Checks = []harness.Check{harness.Ping("8.8.8.8").Count(5).Retry(2, 0).Expect(ping.Received().Eq(5))}
	m := workflowFixture(t, plan, fake, workflowAgents()[:1])
	m.count = "2"
	m = workflowKey(t, m, tea.Key{Code: tea.KeyEnter})
	m = workflowKey(t, m, tea.Key{Code: tea.KeyEnter})
	pumpWorkflow(t, &m, func(m workflowModel) bool { return m.phase == workflowReview })
	if len(m.runs[0].report.Problems) == 0 {
		t.Fatal("cleanup failure disappeared")
	}
	for i, step := range m.currentReport().Steps {
		if len(step.Attempts) > 1 {
			m.stepCursor = i
			break
		}
	}
	text := strings.Join(workflowReportLines(m.currentReport(), m.stepCursor, true), "\n")
	if !strings.Contains(text, "Recovered:") || !strings.Contains(text, "synthetic cleanup failure") {
		t.Fatal("recovery/cleanup not inspectable")
	}
	for _, size := range []tea.WindowSizeMsg{{Width: 40, Height: 8}, {Width: 12, Height: 3}, {Width: 80, Height: 16}} {
		updated, _ := m.Update(size)
		m = updated.(workflowModel)
		m.inspect, m.detail = true, true
		frame := m.renderWorkflow()
		if len(strings.Split(frame, "\n")) > size.Height {
			t.Fatal("short viewport overflow")
		}
		for _, line := range strings.Split(frame, "\n") {
			if runeLen(line) > size.Width {
				t.Fatal("narrow viewport overflow")
			}
		}
		if strings.Contains(frame, "\x1b[2J") || strings.Contains(frame, workflowTestSecret) {
			t.Fatal("control/secret escaped sanitation")
		}
	}
}

func TestWorkflowLoopStopReviewAndLostAgentNoReplacement(t *testing.T) {
	fake := &workflowFake{blockPing: true, started: make(chan string, 1)}
	m := workflowFixture(t, workflowPlan(), fake, workflowAgents()[:1])
	m.loop = true
	m = workflowKey(t, m, tea.Key{Code: tea.KeyEnter})
	m = workflowKey(t, m, tea.Key{Code: tea.KeyEnter})
	pumpWorkflow(t, &m, func(m workflowModel) bool { return len(m.scopes) >= 3 })
	select {
	case <-fake.started:
	case <-time.After(5 * time.Second):
		t.Fatal("loop did not execute")
	}
	m = workflowKey(t, m, tea.Key{Code: 'q', Text: "q"})
	pumpWorkflow(t, &m, func(m workflowModel) bool { return m.phase == workflowReview })
	fake.mu.Lock()
	fake.blockPing = false
	fake.lostAgent = true
	fake.mu.Unlock()
	m.inspect = true
	for i, step := range m.currentReport().Steps {
		if step.Scope.CheckID == "check/0" {
			m.stepCursor = i
			break
		}
	}
	m = workflowKey(t, m, tea.Key{Code: 'r', Text: "r"})
	m = workflowKey(t, m, tea.Key{Code: tea.KeyEnter})
	pumpWorkflow(t, &m, func(m workflowModel) bool { return m.phase == workflowReview })
	for _, step := range m.runs[1].report.Steps {
		if step.Scope.AgentID != "agent-a" {
			t.Fatal("disconnected agent silently replaced")
		}
	}
	if m.runs[1].report.Passed() {
		t.Fatal("lost agent reported as ready/pass")
	}
}

func TestWorkflowFiniteNTwoAgentCoreRounds(t *testing.T) {
	fake := &workflowFake{}
	m := workflowFixture(t, workflowPlan(), fake, workflowAgents())
	m.count = "2"
	m = workflowKey(t, m, tea.Key{Code: tea.KeyEnter})
	m = workflowKey(t, m, tea.Key{Code: tea.KeyEnter})
	pumpWorkflow(t, &m, func(m workflowModel) bool { return m.phase == workflowReview })
	if len(m.currentReport().Agents) != 2 {
		t.Fatal("multi-agent progress collapsed into latest agent")
	}
	for _, agent := range m.currentReport().Agents {
		if agent.Round != 2 {
			t.Fatalf("finite rounds: agent=%s round=%d", agent.Agent.ID, agent.Round)
		}
	}
	fake.mu.Lock()
	calls := fake.pingCalls
	fake.mu.Unlock()
	if calls != 4 {
		t.Fatalf("shared core N rounds dispatched %d pings, want 4", calls)
	}
}

func TestWorkflowScopedPauseReachesCoreBoundaryAndResumes(t *testing.T) {
	fake := &workflowFake{blockPing: true, releasePing: make(chan struct{}, 1), started: make(chan string, 1)}
	plan := workflowPlan()
	plan.Checks = append(plan.Checks, harness.IPStatus())
	m := workflowFixture(t, plan, fake, workflowAgents()[:1])
	m = workflowKey(t, m, tea.Key{Code: tea.KeyEnter})
	m = workflowKey(t, m, tea.Key{Code: tea.KeyEnter})
	scope := harness.Scope{Kind: harness.ScopeTarget, AgentID: "agent-a", TargetID: "target/0"}
	pumpWorkflow(t, &m, func(m workflowModel) bool { return slices.Contains(m.scopes, scope) })
	select {
	case <-fake.started:
	case <-time.After(5 * time.Second):
		t.Fatal("fake ping not executing")
	}
	m = workflowKey(t, m, tea.Key{Code: 'v', Text: "v"})
	m.scopeIndex = slices.Index(m.scopes, scope)
	m = workflowKey(t, m, tea.Key{Code: tea.KeyEnter})
	m = workflowKey(t, m, tea.Key{Code: 'p', Text: "p"})
	fake.releasePing <- struct{}{}
	pumpWorkflow(t, &m, func(m workflowModel) bool { return m.pauseAcks["agent-a"] })
	fake.mu.Lock()
	queued := slices.Contains(fake.operations, "agent-a:ip.status")
	fake.mu.Unlock()
	if queued {
		t.Fatal("selected pause did not stop the next real core operation boundary")
	}
	m = workflowKey(t, m, tea.Key{Code: 'p', Text: "p"})
	pumpWorkflow(t, &m, func(m workflowModel) bool { return m.phase == workflowReview })
	if !m.currentReport().Passed() {
		t.Fatal("selected resume did not finish core work")
	}
}

func TestWorkflowKeyboardSelectsAgentsTargetsAndChecksWithoutOperations(t *testing.T) {
	fake := &workflowFake{}
	plan := workflowPlan()
	plan.Networks = append(plan.Networks, harness.WiFi("second").SSID("Other Lab").PSK(workflowTestSecret))
	plan.Checks = append(plan.Checks, harness.IPStatus())
	m := workflowFixture(t, plan, fake, workflowAgents())
	m = workflowKey(t, m, tea.Key{Code: 'j', Text: "j"})
	m = workflowKey(t, m, tea.Key{Code: tea.KeySpace, Text: " "})
	m = workflowKey(t, m, tea.Key{Code: tea.KeyTab})
	m = workflowKey(t, m, tea.Key{Code: tea.KeySpace, Text: " "})
	m = workflowKey(t, m, tea.Key{Code: tea.KeyTab})
	m = workflowKey(t, m, tea.Key{Code: 'j', Text: "j"})
	m = workflowKey(t, m, tea.Key{Code: tea.KeySpace, Text: " "})
	m = workflowKey(t, m, tea.Key{Code: tea.KeyEnter})
	if m.phase != workflowPreview {
		t.Fatalf("selected preview disabled: %s", m.message)
	}
	view := m.selected.Preview()
	if len(view.Agents) != 1 || len(view.Targets) != 1 || len(view.Checks) != 1 || view.Agents[0].ID != "agent-a" || view.Targets[0].ID != "target/1" || view.Checks[0].ID != "check/0" {
		t.Fatalf("selected IDs wrong: agents=%d targets=%d checks=%d", len(view.Agents), len(view.Targets), len(view.Checks))
	}
	if len(fake.operations) != 0 {
		t.Fatal("selection/preview performed a measurement")
	}
}
