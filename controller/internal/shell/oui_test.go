package shell

import (
	"strings"
	"testing"
)

func TestOUICommandsAreLocalAndKeepPipelines(t *testing.T) {
	for _, tc := range []struct {
		line string
		kind CommandKind
	}{{"sh oui", ShowOUI}, {"req oui up", UpdateOUI}} {
		parsed, err := ParseLine(tc.line + " | count")
		if err != nil || parsed.Kind != tc.kind || parsed.Effect != "local" || parsed.Pipeline.StageCount() != 1 || parsed.Operation.Command != nil {
			t.Fatalf("local OUI command changed target or pipeline: %+v %v", parsed, err)
		}
	}
	if got := strings.Join(CompleteLine("request oui "), ","); !strings.Contains(got, "request oui update") {
		t.Fatal(got)
	}
}
