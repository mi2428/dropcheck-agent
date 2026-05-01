package command

import (
	"strings"

	"dropcheck/controller/internal/controlpb"
)

type Options struct {
	TracerouteRequiredHops []string
}

func BuildCommandWithOptions(args []string) (*controlpb.RunCommand, Options, error) {
	args, err := NormalizeAgentCommandArgs(args)
	if err != nil {
		return nil, Options{}, err
	}
	cmd, err := BuildCommand(args)
	if err != nil {
		return nil, Options{}, err
	}
	return cmd, localCommandOptions(args), nil
}

func localCommandOptions(args []string) Options {
	if len(args) == 0 || args[0] != "traceroute" {
		return Options{}
	}
	return Options{TracerouteRequiredHops: collectTracerouteRequiredHops(args[1:])}
}

func collectTracerouteRequiredHops(args []string) []string {
	if len(args) == 0 {
		return nil
	}
	rest := args[1:]
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "--") {
		rest = rest[1:]
	}
	var required []string
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "--via":
			if i+1 < len(rest) {
				required = append(required, rest[i+1])
				i++
			}
		case "--size", "--timeout":
			if i+1 < len(rest) {
				i++
			}
		}
	}
	return required
}
