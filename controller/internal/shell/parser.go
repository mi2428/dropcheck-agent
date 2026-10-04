package shell

import (
	"fmt"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/pipeline"
)

type CommandKind int

const (
	Noop CommandKind = iota
	Exit
	Help
	ShowDevices
	AgentCommand
	ADBDiagnostics
	Version
)

type Command struct {
	Kind               CommandKind
	Operation          command.Operation
	ADBDiagnosticsKind string
	HelpTopic          string
	Pipeline           pipeline.Pipeline
	RawCommand         string
}

func ParseLine(line string) (Command, error) {
	parts, err := pipeline.Split(line)
	if err != nil {
		return Command{}, err
	}
	pipe, err := pipeline.Parse(parts[1:])
	if err != nil {
		return Command{}, err
	}
	args, err := command.SplitArgs(parts[0])
	if err != nil {
		return Command{}, err
	}
	if len(args) == 0 {
		return Command{Kind: Noop}, nil
	}
	if args[0] == "exit" || args[0] == "quit" {
		if len(args) != 1 {
			return Command{}, fmt.Errorf("usage: %s", args[0])
		}
		return Command{Kind: Exit}, nil
	}
	parsed, err := command.ParseTokens(args)
	if err != nil {
		return Command{}, err
	}
	result := Command{Kind: AgentCommand, Operation: parsed.Operation, ADBDiagnosticsKind: parsed.ADBKind, HelpTopic: parsed.Topic, Pipeline: pipe, RawCommand: parts[0]}
	switch parsed.Path {
	case "help":
		result.Kind = Help
	case "show devices":
		result.Kind = ShowDevices
	case "show version":
		result.Kind = Version
	default:
		if parsed.ADBKind != "" {
			result.Kind = ADBDiagnostics
		}
	}
	return result, nil
}
