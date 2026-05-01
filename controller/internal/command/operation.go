package command

import (
	"fmt"

	"dropcheck/controller/internal/controlpb"
	"google.golang.org/protobuf/proto"
)

// Operation is the typed command plan shared by the shell and Linux-style CLI.
//
// It carries the fully built agent command plus controller-local rendering or
// validation options. Operation intentionally avoids argv-shaped fields: once a
// frontend parser has recognized a command, execution should not need to
// reparse a synthetic argument list.
type Operation struct {
	// Name is a stable dotted operation identifier used by callers and tests.
	Name string
	// Command is the protocol-buffer command sent to an Android agent.
	Command *controlpb.RunCommand
	// Options contains controller-local behavior that is not sent to the agent.
	Options Options
}

// NewOperation creates an immutable Operation boundary by cloning cmd.
//
// Callers may safely reuse or mutate the original RunCommand after this call;
// BuildRunCommand also returns a clone before execution.
func NewOperation(name string, cmd *controlpb.RunCommand, options Options) Operation {
	return Operation{
		Name:    name,
		Command: cloneRunCommand(cmd),
		Options: options,
	}
}

// BuildRunCommand returns the executable command and local options for op.
//
// The returned RunCommand is cloned so execution-specific changes cannot mutate
// the stored Operation. A missing command is treated as a programmer error in
// the frontend parser that produced the operation.
func BuildRunCommand(op Operation) (*controlpb.RunCommand, Options, error) {
	if op.Command == nil {
		return nil, Options{}, fmt.Errorf("operation %q has no command adapter", op.Name)
	}
	return cloneRunCommand(op.Command), op.Options, nil
}

func cloneRunCommand(cmd *controlpb.RunCommand) *controlpb.RunCommand {
	if cmd == nil {
		return nil
	}
	return proto.Clone(cmd).(*controlpb.RunCommand)
}
