package app

import (
	"time"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/controlpb"
)

// Operation is the command boundary consumed by app execution code.
//
// The alias keeps app code at its existing boundary while the concrete type
// lives in internal/command.
type Operation = command.Operation
type commandOptions = command.Options

func buildRunCommand(op Operation) (*controlpb.RunCommand, commandOptions, error) {
	return command.BuildRunCommand(op)
}

func timeoutFor(cmd *controlpb.RunCommand) time.Duration {
	return command.TimeoutFor(cmd)
}
