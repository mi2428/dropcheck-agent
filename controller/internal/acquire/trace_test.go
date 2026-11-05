package acquire

import (
	"context"
	"testing"

	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/control"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"github.com/mi2428/dropcheck-agent/controller/internal/runner"
)

func TestRequiredHopsUseTypedAddressesAndHostnamesNotStdout(t *testing.T) {
	op, err := command.TracerouteOperation(command.TracerouteOptions{Host: "example.test", Via: []string{"192.0.2.1", "router.example.test"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name          string
		trace         *controlpb.TracerouteResult
		pass, missing bool
	}{
		{"typed hops", &controlpb.TracerouteResult{ReachedTarget: new(true), Hops: []*controlpb.TracerouteHop{{Index: 1, Addresses: []string{"192.0.2.1"}, Hostnames: []string{"router.example.test"}}}}, true, false},
		{"stdout only", &controlpb.TracerouteResult{ReachedTarget: new(false), Output: "192.0.2.1 router.example.test"}, false, false},
		{"unavailable observations", &controlpb.TracerouteResult{Output: "192.0.2.1 router.example.test"}, false, true},
		{"failed observations", &controlpb.TracerouteResult{ReachedTarget: new(false), ObservationError: "not collected"}, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK, Payload: &controlpb.CommandResult_Traceroute{Traceroute: tc.trace}}
			result, err := Execute(context.Background(), runnerFunc(func(context.Context, control.AgentInfo, command.Operation) (runner.Result, error) {
				return runner.Result{Result: raw}, nil
			}), control.AgentInfo{ID: "a"}, op)
			if err != nil || len(result.Findings) != 2 || len(result.Parts) != 1 || result.Parts[0].AgentID != "a" {
				t.Fatalf("trace acquisition lost attribution: %+v err=%v", result, err)
			}
			for _, f := range result.Findings {
				if f.Passed != tc.pass || f.Missing != tc.missing {
					t.Fatalf("stdout replaced typed hops: %+v", f)
				}
			}
		})
	}
}
