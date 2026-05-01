package app

import (
	"slices"
	"testing"

	"google.golang.org/protobuf/proto"
)

func assertOperationMatchesArgs(t *testing.T, op Operation, args []string) {
	t.Helper()
	gotCmd, gotOptions, err := buildRunCommand(op)
	if err != nil {
		t.Fatalf("buildRunCommand() error = %v", err)
	}
	wantCmd, wantOptions, err := buildCommandWithOptions(args)
	if err != nil {
		t.Fatalf("buildCommandWithOptions(%#v) error = %v", args, err)
	}
	if !proto.Equal(gotCmd, wantCmd) {
		t.Fatalf("operation command = %#v, want %#v", gotCmd, wantCmd)
	}
	if !slices.Equal(gotOptions.TracerouteRequiredHops, wantOptions.TracerouteRequiredHops) {
		t.Fatalf("operation options = %#v, want %#v", gotOptions, wantOptions)
	}
}
