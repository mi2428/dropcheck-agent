package runner

import (
	"context"
	"fmt"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/controlpb"
)

// Result is the complete controller-side result of one operation execution.
type Result struct {
	// CommandID is the unique ID assigned by the controller for this execution.
	CommandID string
	// Operation is the typed operation requested by the caller.
	Operation command.Operation
	// Command is the cloned protobuf command sent to the agent.
	Command *controlpb.RunCommand
	// Options are controller-local options returned by command.BuildRunCommand.
	Options command.Options
	// Result is the terminal result returned by the Android agent.
	Result *controlpb.CommandResult
}

// Runner executes operations through a control server.
type Runner struct {
	server *control.Server
}

// New creates a Runner backed by server.
func New(server *control.Server) Runner {
	return Runner{server: server}
}

// Run executes op on agent and waits for the terminal agent result.
func (r Runner) Run(ctx context.Context, agent control.AgentInfo, op command.Operation) (Result, error) {
	if r.server == nil {
		return Result{}, fmt.Errorf("runner has no control server")
	}
	cmd, options, err := command.BuildRunCommand(op)
	if err != nil {
		return Result{}, err
	}
	commandID, err := control.RandomHex(8)
	if err != nil {
		return Result{}, err
	}
	runCtx, cancel := context.WithTimeout(ctx, command.TimeoutFor(cmd))
	defer cancel()
	result, err := r.server.Run(runCtx, agent.ID, commandID, cmd)
	if err != nil {
		return Result{CommandID: commandID, Operation: op, Command: cmd, Options: options}, err
	}
	return Result{CommandID: commandID, Operation: op, Command: cmd, Options: options, Result: result}, nil
}
