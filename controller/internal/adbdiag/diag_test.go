package adbdiag

import (
	"strings"
	"testing"

	"dropcheck/controller/internal/pipeline"
)

func TestSpecsFullIncludesRawWifiAndConnectivityCommands(t *testing.T) {
	specs, err := Specs(KindFull)
	if err != nil {
		t.Fatalf("Specs(full) error = %v", err)
	}
	var names []string
	for _, spec := range specs {
		names = append(names, spec.Name)
	}
	joined := strings.Join(names, "\n")
	for _, want := range []string{"cmd wifi status", "dumpsys wifi", "dumpsys connectivity", "dumpsys connectivity --diag", "dumpsys connectivity trafficcontroller"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("Specs(full) names = %q, missing %q", joined, want)
		}
	}
}

func TestRenderTextIncludesCommandOutput(t *testing.T) {
	out, err := Render(Bundle{
		Agent:  "agent-1",
		Serial: "R5CT12345",
		Kind:   KindCmdWifiStatus,
		Commands: []CommandResult{{
			Name:      "cmd wifi status",
			Command:   "adb -s R5CT12345 shell cmd wifi status",
			Stdout:    "Wifi is enabled\n",
			ExitCode:  0,
			ElapsedMs: 12,
		}},
	}, pipeline.FormatText)
	if err != nil {
		t.Fatalf("Render(text) error = %v", err)
	}
	for _, want := range []string{"ADB diagnostics: serial=R5CT12345", "--- cmd wifi status ---", "Wifi is enabled"} {
		if !strings.Contains(out, want) {
			t.Fatalf("Render(text) = %q, missing %q", out, want)
		}
	}
}
