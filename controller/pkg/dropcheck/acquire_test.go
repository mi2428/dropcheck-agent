package dropcheck

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/mi2428/dropcheck-agent/controller/internal/acquire"
	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/control"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"github.com/mi2428/dropcheck-agent/controller/internal/runner"
	"google.golang.org/protobuf/proto"
)

type projectionRunner struct{ raw *controlpb.CommandResult }

func (r projectionRunner) Run(context.Context, control.AgentInfo, command.Operation) (runner.Result, error) {
	return runner.Result{CommandID: "acquired-command", Result: r.raw}, nil
}

func TestPublicResultProjectsNeutralAcquisitionWithoutWatchTypes(t *testing.T) {
	op, err := Traceroute(TracerouteOptions{Host: "example.test", Via: []string{"router.example.test"}})
	if err != nil {
		t.Fatal(err)
	}
	agent := control.AgentInfo{ID: "agent", SessionID: "original", Connected: time.Unix(123, 0)}
	value, err := acquire.Execute(t.Context(), projectionRunner{&controlpb.CommandResult{Status: StatusOK, Payload: &TraceroutePayload{Traceroute: &TracerouteResult{ReachedTarget: new(false)}}}}, agent, op.impl)
	if err != nil {
		t.Fatal(err)
	}
	result := publicResult(value)
	if !proto.Equal(result.Raw, value.Raw) || len(result.Parts) != 1 || len(result.Findings) != 1 {
		t.Fatal("public projection changed acquisition")
	}
	p, part := result.Parts[0], value.Parts[0]
	if p.AgentID != part.AgentID || p.SessionID != part.SessionID || p.CommandID != part.CommandID || p.Name != part.Name || p.Started != part.Started || p.Ended != part.Ended || p.Error != part.Error || !proto.Equal(p.Raw, part.Raw) {
		t.Fatal("public projection lost pinned identity/timing")
	}
	f := value.Findings[0]
	if !reflect.DeepEqual(result.Findings[0], Finding{Metric: f.Metric, Observed: f.Observed, Expected: f.Expected, Passed: f.Passed, Missing: f.Missing, Message: f.Message}) {
		t.Fatal("public projection lost typed findings")
	}
	result.Raw.GetTraceroute().Host = "edited"
	if result.Parts[0].Raw.GetTraceroute().Host == "edited" {
		t.Fatal("public Raw aliases an acquisition part")
	}
}
