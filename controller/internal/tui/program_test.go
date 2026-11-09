package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/control"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"github.com/mi2428/dropcheck-agent/controller/internal/runner"
	"github.com/mi2428/dropcheck-agent/controller/internal/tuirun"
)

type programCheckpoint struct {
	Kind         string                 `json:"kind"`
	Active       bool                   `json:"active"`
	Width        int                    `json:"width"`
	Height       int                    `json:"height"`
	Round        uint64                 `json:"round"`
	Progress     []tuirun.AgentProgress `json:"progress"`
	Menu         bool                   `json:"menu"`
	MenuScope    tuirun.Scope           `json:"menu_scope"`
	Scope        tuirun.Scope           `json:"scope"`
	Busy         bool                   `json:"busy"`
	Applied      bool                   `json:"applied"`
	RunPaused    bool                   `json:"run_paused"`
	Phase        string                 `json:"phase"`
	Detail       bool                   `json:"detail"`
	Offset       int                    `json:"offset"`
	Inspector    string                 `json:"inspector"`
	Frame        string                 `json:"frame"`
	Confirmation string                 `json:"confirmation"`
}

type programProbe struct{ reply chan programCheckpoint }
type programReadyMsg struct{}

type checkedProgram struct {
	inner model
	out   *json.Encoder
}

func (m checkedProgram) Init() tea.Cmd {
	return tea.Batch(m.inner.Init(), func() tea.Msg { return programReadyMsg{} })
}
func (m checkedProgram) View() tea.View {
	// The race check isolates event scheduling; the PTY child covers real drawing.
	if m.out == nil {
		return tea.NewView("")
	}
	return m.inner.View()
}
func (m checkedProgram) checkpoint() programCheckpoint {
	result := programCheckpoint{Kind: "checkpoint", Menu: m.inner.menu != nil, Scope: m.inner.controlScope,
		Progress: append([]tuirun.AgentProgress(nil), m.inner.AgentProgress...),
		Active:   m.inner.controls.HasScope(tuirun.Scope{Kind: tuirun.ScopeRun}), Width: m.inner.width, Height: m.inner.height, Round: m.inner.Round,
		Busy: m.inner.controlBusy, Applied: m.inner.controlApplied, Phase: m.inner.Phase,
		RunPaused: m.inner.controls.Paused(tuirun.Scope{Kind: tuirun.ScopeRun}), Detail: m.inner.detailOpen,
		Offset: m.inner.inspector.YOffset(), Inspector: m.inner.inspector.GetContent()}
	if m.out != nil || m.inner.menu != nil {
		result.Frame = m.inner.View().Content
	}
	if m.inner.menu != nil {
		result.MenuScope, result.Confirmation = m.inner.menu.scope, m.inner.menu.confirmation
	}
	return result
}
func (m checkedProgram) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(programReadyMsg); ok {
		if m.out != nil {
			if err := m.out.Encode(programCheckpoint{Kind: "ready"}); err != nil {
				return m, tea.Quit
			}
		}
		return m, nil
	}
	if probe, ok := msg.(programProbe); ok {
		probe.reply <- m.checkpoint()
		return m, nil
	}
	if key, ok := msg.(tea.KeyPressMsg); ok && key.Code == tea.KeyF12 && m.out != nil {
		if err := m.out.Encode(m.checkpoint()); err != nil {
			return m, tea.Quit
		}
		return m, nil
	}
	next, cmd := m.inner.Update(msg)
	m.inner = next.(model)
	return m, cmd
}

type programRunner struct{}

func (programRunner) Run(ctx context.Context, _ control.AgentInfo, op command.Operation) (runner.Result, error) {
	if op.Name != "ping" {
		return runner.Result{}, fmt.Errorf("unexpected synthetic operation %q", op.Name)
	}
	select {
	case <-ctx.Done():
		return runner.Result{}, ctx.Err()
	case <-time.After(20 * time.Millisecond):
	}
	count := op.Command.GetPing().GetCount()
	return runner.Result{Result: &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK,
		Payload: &controlpb.CommandResult_Ping{Ping: &controlpb.PingResult{Host: op.Command.GetPing().GetHost(), Count: count, Transmitted: new(count), Received: new(count)}}}}, nil
}

func syntheticTUIProgram(t *testing.T) (model, func(context.Context) error) {
	t.Helper()
	op, err := command.PingOperation(command.PingOptions{Host: "example.test"})
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := tuirun.Compile(tuirun.Plan{
		Name:     "synthetic-tui",
		Networks: []tuirun.Network{{Name: "synthetic-target", SSID: "synthetic", ReadOnly: true, ActiveProbes: true}},
		Checks:   []tuirun.Check{tuirun.NewCheck("ping", "", op, tuirun.Policy{Repeat: 2, Timeout: 20 * time.Second}, false)},
	}, []control.AgentInfo{{ID: "qa-agent-a"}, {ID: "qa-agent-b"}})
	if err != nil {
		t.Fatal(err)
	}
	preview := compiled.Preview()
	events := make(chan tuirun.Event, 1)
	controls := tuirun.NewControls()
	m := newModelWithChecks(preview.Name, preview.Targets, preview.Checks, events, preview.Agents)
	m.controls, m.width, m.height = controls, 160, 48
	for _, target := range m.Targets {
		step := target.PlannedSteps[0]
		for i := range 20 {
			at := m.Now.Add(-time.Duration(100-i) * time.Second)
			m.RecordPassingCheck(passingCheckState{When: at, Round: 1, Agent: target.Agent, Target: target.Target,
				Step: tuirun.StepSnapshot{ID: step.ID, Name: step.Name, Type: step.Type, Status: "ok"}})
		}
		for i := range 120 {
			m.PushEventLog(tuirun.Event{Kind: tuirun.EventLog, Time: m.Now.Add(-time.Duration(120-i) * time.Second),
				Agent: target.Agent, Target: target.Target, Step: tuirun.StepSnapshot{ID: step.ID, Name: step.Name, Type: step.Type},
				Message: fmt.Sprintf("synthetic related log %03d; no device operation", i)})
		}
	}
	return m, func(ctx context.Context) error {
		defer close(events)
		_, err := tuirun.Execute(ctx, compiled, programRunner{}, tuirun.ExecuteOptions{Loop: true, RoundInterval: 100 * time.Millisecond,
			Controls: controls, Sinks: []tuirun.Sink{tuirun.ChannelSink{C: events, Done: ctx.Done()}}})
		return err
	}
}

func TestTUIProgramConsumesEventsWhileControlling(t *testing.T) {
	m, execute := syntheticTUIProgram(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	program := tea.NewProgram(checkedProgram{inner: m}, tea.WithContext(ctx), tea.WithInput(nil), tea.WithOutput(io.Discard), tea.WithWindowSize(160, 48), tea.WithoutSignalHandler())
	programDone, runtimeDone := make(chan error, 1), make(chan error, 1)
	go func() { _, err := program.Run(); programDone <- err }()
	go func() { runtimeDone <- execute(ctx) }()
	t.Cleanup(func() {
		cancel()
		program.Kill()
		select {
		case <-programDone:
		case <-time.After(time.Second):
			t.Error("program did not stop")
		}
		select {
		case <-runtimeDone:
		case <-time.After(time.Second):
			t.Error("runtime did not stop")
		}
	})
	probe := func(want func(programCheckpoint) bool) programCheckpoint {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		var last programCheckpoint
		for time.Now().Before(deadline) {
			reply := make(chan programCheckpoint, 1)
			program.Send(programProbe{reply: reply})
			select {
			case last = <-reply:
				if want(last) {
					return last
				}
			case <-ctx.Done():
				t.Fatal("program checkpoint timed out")
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("program did not reach expected state: phase=%q busy=%v paused=%v applied=%v round=%d progress=%+v", last.Phase, last.Busy, last.RunPaused, last.Applied, last.Round, last.Progress)
		return last
	}
	probe(func(programCheckpoint) bool { return m.controls.HasScope(tuirun.Scope{Kind: tuirun.ScopeRun}) })
	press := func(key tea.Key) { program.Send(tea.KeyPressMsg(key)) }
	press(tea.Key{Code: 'z', Mod: tea.ModCtrl})
	probe(func(s programCheckpoint) bool { return !s.Busy && s.Applied && s.RunPaused && s.Phase == "paused" })
	press(tea.Key{Code: tea.KeyEscape})
	probe(func(s programCheckpoint) bool { return s.RunPaused })
	press(tea.Key{Code: 'a', Text: "a"})
	snapshot := probe(func(s programCheckpoint) bool { return s.Menu })
	if !strings.Contains(snapshot.Frame, "Select control scope") {
		t.Fatal("scope picker was not rendered")
	}
	press(tea.Key{Code: tea.KeyEscape})
	probe(func(s programCheckpoint) bool { return !s.Menu && s.RunPaused })
	press(tea.Key{Code: 'z', Mod: tea.ModCtrl})
	probe(func(s programCheckpoint) bool { return !s.Busy && !s.RunPaused && s.Applied })
}

// Interactive PTY test entry; every runner result is synthetic.
func TestTUIPTYChild(t *testing.T) {
	if os.Getenv("DROPCHECK_TUI_PTY_CHILD") != "1" {
		t.Skip("PTY child only")
	}
	fd, err := strconv.Atoi(os.Getenv("DROPCHECK_TUI_QA_FD"))
	if err != nil || fd < 3 {
		t.Fatal("missing QA checkpoint descriptor")
	}
	output := os.NewFile(uintptr(fd), "tui-checkpoints")
	defer output.Close()
	encoder := json.NewEncoder(output)
	m, execute := syntheticTUIProgram(t)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	runtimeDone := make(chan error, 1)
	go func() { runtimeDone <- execute(ctx) }()
	_, programErr := tea.NewProgram(checkedProgram{inner: m, out: encoder}, tea.WithContext(ctx)).Run()
	cancel()
	select {
	case err := <-runtimeDone:
		if err != nil {
			t.Fatalf("synthetic runtime failed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("synthetic runtime did not stop")
	}
	if programErr != nil {
		t.Fatal(programErr)
	}
	if err := encoder.Encode(programCheckpoint{Kind: "done"}); err != nil {
		t.Fatal(err)
	}
}
