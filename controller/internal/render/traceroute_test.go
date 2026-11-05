package render

import (
	"slices"
	"strings"
	"testing"

	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
)

func TestTracerouteUsesTypedHopsNotNativeText(t *testing.T) {
	result := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK, Payload: &controlpb.CommandResult_Traceroute{Traceroute: &controlpb.TracerouteResult{
		Host: "8.8.8.8", ReachedTarget: new(false),
		Output: "1 raw-only (203.0.113.254) 1.234 ms\n2 dns.google (8.8.8.8) 18.100 ms\n",
		Hops: []*controlpb.TracerouteHop{
			{Index: 1, Hostnames: []string{"router.local"}, Addresses: []string{"192.168.23.254"}, RttMs: []float64{1.234, 1.456}},
			{Index: 2, TimedOut: true},
		},
	}}}
	text, err := CommandResult("target", result, command.Options{TracerouteRequiredHops: []string{"192.168.23.254", "203.0.113.254"}})
	if err != nil {
		t.Fatal(err)
	}
	for label, want := range map[string]string{"Matched hops": "192.168.23.254", "Missing hops": "203.0.113.254", "Reached target": "false"} {
		if !slices.Equal(metadataQAValues(text, label), []string{want}) {
			t.Errorf("typed %s not preserved in %s", label, text)
		}
	}
	for _, want := range []string{"router.local", "1.234,1.456", "true", "Native output (reference; not a separate verdict)", "raw-only"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %s", want, text)
		}
	}
	if strings.Contains(text, "Reached target: true") || strings.Contains(text, "status=failed") {
		t.Fatal("presentation inferred a verdict from native text")
	}
}
