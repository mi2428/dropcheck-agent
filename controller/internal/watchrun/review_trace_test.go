package watchrun_test

import (
	"testing"

	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"github.com/mi2428/dropcheck-agent/controller/internal/watchrun"
)

func TestTypedTraceObservationsRemainAvailableToYAML(t *testing.T) {
	for _, tc := range []struct {
		name    string
		raw     *controlpb.TracerouteResult
		count   int
		missing bool
	}{
		{"timeout hop included", &controlpb.TracerouteResult{ReachedTarget: new(true), Hops: []*controlpb.TracerouteHop{{Index: 1, Addresses: []string{"192.0.2.1"}}, {Index: 2, Addresses: []string{"2001:db8::1"}}, {Index: 3, TimedOut: true}}}, 3, false},
		{"known zero", &controlpb.TracerouteResult{ReachedTarget: new(false)}, 0, false},
		{"presence missing", &controlpb.TracerouteResult{}, 0, true},
		{"observation failed", &controlpb.TracerouteResult{ReachedTarget: new(false), ObservationError: "not collected"}, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := watchrun.Result{Run: watchrun.RunResult{Raw: &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK, Payload: &controlpb.CommandResult_Traceroute{Traceroute: tc.raw}}}}
			metric, err := watchrun.CompileExpectations(map[string]any{"hop_count": tc.count})
			if err != nil {
				t.Fatal(err)
			}
			yaml := metric[0].Evaluate(input)
			if len(yaml) != 1 || yaml[0].Missing != tc.missing || yaml[0].Passed == tc.missing {
				t.Fatalf("unexpected typed observation: YAML=%+v", yaml)
			}
			if tc.missing {
				for _, family := range []string{"ipv4_hop_ips", "ipv6_hop_ips"} {
					expect, err := watchrun.CompileExpectations(map[string]any{family: map[string]any{"exact": []any{}}})
					if err != nil {
						t.Fatal(err)
					}
					if !expect[0].Evaluate(input)[0].Missing {
						t.Fatalf("unknown %s became known empty", family)
					}
				}
			}
		})
	}
}
