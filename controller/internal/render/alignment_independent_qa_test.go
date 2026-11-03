package render

import (
	"strings"
	"testing"
	"unicode"

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
