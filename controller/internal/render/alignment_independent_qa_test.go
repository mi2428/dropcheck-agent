package render

import (
	"strings"
	"testing"
	"unicode"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/harness"
	"dropcheck/controller/internal/pipeline"
)

func independentCompact(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, value)
}

func TestIndependentAlignmentCanceledPartDoesNotLeakReason(t *testing.T) {
	result := harness.OperationResult{Parts: []harness.OperationRecord{{
		Name: "passive partial acquisition",
		Raw:  &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_CANCELED, Message: "password=independent-secret-marker"},
	}}}
	for _, width := range []int{8, 24, 40, 80, 120} {
		out, err := OperationResult("self", result, pipeline.FormatText, Presentation{Interactive: true, Width: width})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(independentCompact(out), "independent-secret-marker") {
			t.Errorf("width=%d canceled-part Reason leaked a synthetic credential", width)
		}
	}
}

func TestIndependentAlignmentTracerouteRetainsTypedDetail(t *testing.T) {
	result := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_FAILED, Payload: &controlpb.CommandResult_Traceroute{Traceroute: &controlpb.TracerouteResult{
		Host: "2001:db8::1", MaxHops: 29, SizeBytes: 1259, ExitCode: 73, ReachedTarget: new(false), ObservationError: "typed observation unavailable",
	}}}
	out, err := CommandResult("self", result, command.Options{Detail: true}, pipeline.FormatText, Presentation{Interactive: true, Detail: true, Width: 120})
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"29", "1259", "73"} {
		if !strings.Contains(out, value) {
			t.Errorf("interactive detail discarded typed traceroute value %s", value)
		}
	}
}
