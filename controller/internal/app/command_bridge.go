package app

import "github.com/mi2428/dropcheck-agent/controller/internal/command"

// Operation is the command boundary consumed by app execution code.
//
// The alias keeps app code at its existing boundary while the concrete type
// lives in internal/command.
type Operation = command.Operation
type commandOptions = command.Options
