package app

import (
	"bytes"
	"context"
	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/control"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"github.com/mi2428/dropcheck-agent/controller/internal/runner"
	"github.com/mi2428/dropcheck-agent/controller/internal/watchrun"
	"github.com/mi2428/dropcheck-agent/controller/internal/watchstate"
	"testing"
	"time"
)

type repeatReviewRunner struct{ ping int }

func (r *repeatReviewRunner) Run(_ context.Context, _ control.AgentInfo, op command.Operation) (runner.Result, error) {
	raw := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK}
	switch op.Name {
	case "wifi.connect":
		raw.Payload = &controlpb.CommandResult_ConnectWifi{ConnectWifi: &controlpb.ConnectWifiResult{Connected: true}}
	case "wifi.wait":
		raw.Payload = &controlpb.CommandResult_WifiAssert{WifiAssert: &controlpb.WifiAssertResult{Passed: true}}
	case "wifi.disconnect":
		raw.Payload = &controlpb.CommandResult_WifiOperation{WifiOperation: &controlpb.WifiOperationResult{Ok: true}}
	case "ping":
		received := uint32(0)
		if r.ping > 0 {
			received = 5
		}
		r.ping++
		raw.Payload = &controlpb.CommandResult_Ping{Ping: &controlpb.PingResult{Received: new(received)}}
	}
	return runner.Result{Result: raw}, nil
}

type repeatReviewSink func(watchrun.Event)

func (f repeatReviewSink) Emit(_ context.Context, e watchrun.Event) error { f(e); return nil }

func TestReviewRepeatFirstFailureReachesReducerAndNoTUI(t *testing.T) {
	expect, err := watchrun.CompileExpectations(map[string]any{"received": 5})
	if err != nil {
		t.Fatal(err)
	}
	op, _ := command.PingOperation(command.PingOptions{Host: "example.test"})
	p, err := watchrun.Compile(watchrun.Plan{Networks: []watchrun.Network{{Name: "lab", SSID: "Lab", PSK: watchrun.SecretValue("synthetic-passphrase"), WaitConnected: true, RequireIP: true, DisconnectAfter: true}}, Checks: []watchrun.Check{{Name: "ping", Operation: op, Policy: watchrun.Policy{Attempts: 1, Repeat: 2}, Expectations: expect}}}, []control.AgentInfo{{ID: "a"}})
	if err != nil {
		t.Fatal(err)
	}
	preview := p.Preview()
	state := watchstate.New(preview.Targets, preview.Checks, preview.Agents, time.Now())
	var out bytes.Buffer
	report, err := watchrun.Execute(context.Background(), p, &repeatReviewRunner{}, watchrun.ExecuteOptions{Rounds: 1, Sinks: []watchrun.Sink{repeatReviewSink(func(e watchrun.Event) { state.Apply(e); printWatchNoTUIEvent(&out, e, false) })}})
	if err != nil {
		t.Fatal(err)
	}
	if report.Outcome != watchrun.FailOutcome || len(state.FailedChecks) == 0 || out.Len() == 0 {
		t.Fatalf("failure disappeared: report=%s history=%d noTUI=%q", report.Outcome, len(state.FailedChecks), out.String())
	}
}
