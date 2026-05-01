package command

import (
	"fmt"

	"dropcheck/controller/internal/controlpb"
	"google.golang.org/protobuf/proto"
)

type Operation struct {
	Name    string
	Command *controlpb.RunCommand
	Options Options
}

func NewOperation(name string, cmd *controlpb.RunCommand, options Options) Operation {
	return Operation{
		Name:    name,
		Command: cloneRunCommand(cmd),
		Options: options,
	}
}

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
