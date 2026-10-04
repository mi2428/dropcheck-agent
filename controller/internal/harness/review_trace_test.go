package harness_test

import (
	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/harness"
	"dropcheck/controller/internal/harness/trace"
	"testing"
)

func TestReviewTypedYAMLTracePresenceParity(t *testing.T) {
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
			input := harness.Result{Run: harness.RunResult{Raw: &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK, Payload: &controlpb.CommandResult_Traceroute{Traceroute: tc.raw}}}}
			typed := trace.HopCount().Eq(tc.count).Evaluate(input)
			metric, err := harness.CompileExpectations(map[string]any{"hop_count": tc.count})
			if err != nil {
				t.Fatal(err)
			}
			yaml := metric[0].Evaluate(input)
			if len(typed) != 1 || len(yaml) != 1 || typed[0].Passed != yaml[0].Passed || typed[0].Missing != yaml[0].Missing || yaml[0].Missing != tc.missing {
				t.Fatalf("different judgement: typed=%+v YAML=%+v", typed, yaml)
			}
			if tc.missing {
				for _, family := range []string{"ipv4_hop_ips", "ipv6_hop_ips"} {
					expect, err := harness.CompileExpectations(map[string]any{family: map[string]any{"exact": []any{}}})
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
