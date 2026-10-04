package app

import (
	"bytes"
	"context"
	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/harness"
	"dropcheck/controller/internal/runner"
	"dropcheck/controller/internal/watchstate"
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
		raw.Payload = &controlpb.CommandResult_Ping{Ping: &controlpb.PingResult{Received: received}}
	}
	return runner.Result{Result: raw}, nil
}

type repeatReviewSink func(harness.Event)

func (f repeatReviewSink) Emit(_ context.Context, e harness.Event) error { f(e); return nil }

func TestReviewRepeatFirstFailureReachesReducerAndNoTUI(t *testing.T) {
	expect, err := harness.CompileExpectations(map[string]any{"received": 5})
	if err != nil {
		t.Fatal(err)
	}
	op, _ := command.PingOperation(command.PingOptions{Host: "example.test"})
	p, err := harness.Compile(harness.Plan{Networks: []harness.Network{harness.WiFi("lab").SSID("Lab")}, Checks: []harness.Check{harness.NewCheck("ping", "", op, harness.Policy{Attempts: 1, Repeat: 2}, false, expect...)}}, []control.AgentInfo{{ID: "a"}})
	if err != nil {
		t.Fatal(err)
	}
	preview := p.Preview()
	state := watchstate.New(preview.Targets, preview.Checks, preview.Agents, time.Now())
	var out bytes.Buffer
	report, err := harness.Execute(context.Background(), p, &repeatReviewRunner{}, harness.ExecuteOptions{Rounds: 1, Sinks: []harness.Sink{repeatReviewSink(func(e harness.Event) { state.Apply(e); printWatchNoTUIEvent(&out, e, false) })}})
	if err != nil {
		t.Fatal(err)
	}
	if report.Outcome != harness.FailOutcome || len(state.FailedChecks) == 0 || out.Len() == 0 {
		t.Fatalf("failure disappeared: report=%s history=%d noTUI=%q", report.Outcome, len(state.FailedChecks), out.String())
	}
}
