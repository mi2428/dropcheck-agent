package watchrun

import (
	"context"
	"errors"
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

func TestWatchAdapterIsLosslessWithoutAddingPolicy(t *testing.T) {
	op, err := command.TracerouteOperation(command.TracerouteOptions{Host: "example.test", Via: []string{"router.example.test"}})
	if err != nil {
		t.Fatal(err)
	}
	agent := control.AgentInfo{ID: "agent", SessionID: "original", Connected: time.Unix(123, 0)}
	for _, runErr := range []error{nil, errors.New("synthetic acquisition failure")} {
		r := agentRunnerFunc(func(context.Context, control.AgentInfo, command.Operation) (runner.Result, error) {
			return runner.Result{CommandID: "original-command", Result: &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK, Payload: &controlpb.CommandResult_Traceroute{Traceroute: &controlpb.TracerouteResult{ReachedTarget: new(false)}}}}, runErr
		})
		neutral, wantErr := acquire.Execute(t.Context(), r, agent, op)
		watch, gotErr := executeOperation(t.Context(), r, agent, op)
		if gotErr != wantErr || !proto.Equal(neutral.Raw, watch.Raw) || !reflect.DeepEqual(neutral.Options, watch.Options) || len(neutral.Parts) != len(watch.Parts) || len(neutral.Findings) != len(watch.Findings) {
			t.Fatal("watch adapter changed acquisition")
		}
		for i, part := range watch.Parts {
			original := neutral.Parts[i]
			if part.Scope != (Scope{Kind: ScopeAgent, AgentID: original.AgentID}) || part.Round != 0 || part.CommandID != original.CommandID || part.Name != original.Name || part.Error != original.Error || !proto.Equal(part.Raw, original.Raw) {
				t.Fatal("watch adapter changed part identity/payload")
			}
		}
		for i, f := range watch.Findings {
			original := neutral.Findings[i]
			want := Finding{Metric: original.Metric, Observed: original.Observed, Expected: original.Expected, Passed: original.Passed, Missing: original.Missing, ObservedValue: original.ObservedValue, ExpectedValue: original.ExpectedValue, Message: original.Message}
			if !reflect.DeepEqual(f, want) {
				t.Fatal("watch adapter added/lost policy attribution or evidence")
			}
		}
	}
}
