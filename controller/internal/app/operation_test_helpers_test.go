package app

import (
	"slices"
	"testing"

	"dropcheck/controller/internal/controlpb"
)

func operationCommand(t *testing.T, op Operation) (*controlpb.RunCommand, commandOptions) {
	t.Helper()
	cmd, options, err := buildRunCommand(op)
	if err != nil {
		t.Fatalf("buildRunCommand() error = %v", err)
	}
	return cmd, options
}

func assertOperationLabel(t *testing.T, op Operation, label string) {
	t.Helper()
	cmd, _ := operationCommand(t, op)
	if cmd.GetLabel() != label {
		t.Fatalf("operation label = %q, want %q", cmd.GetLabel(), label)
	}
}

func assertTracerouteHops(t *testing.T, op Operation, hops []string) {
	t.Helper()
	_, options := operationCommand(t, op)
	if !slices.Equal(options.TracerouteRequiredHops, hops) {
		t.Fatalf("traceroute required hops = %#v, want %#v", options.TracerouteRequiredHops, hops)
	}
}
