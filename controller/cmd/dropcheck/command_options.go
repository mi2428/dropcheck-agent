package main

import (
	"strings"

	"dropcheck/controller/internal/controlpb"
)

type commandOptions struct {
	tracerouteRequiredHops []string
}

func buildCommandWithOptions(args []string) (*controlpb.RunCommand, commandOptions, error) {
	args, err := normalizeAgentCommandArgs(args)
	if err != nil {
		return nil, commandOptions{}, err
	}
	cmd, err := buildCommand(args)
	if err != nil {
		return nil, commandOptions{}, err
	}
	return cmd, localCommandOptions(args), nil
}

func localCommandOptions(args []string) commandOptions {
	if len(args) == 0 || args[0] != "traceroute" {
		return commandOptions{}
	}
	return commandOptions{tracerouteRequiredHops: collectTracerouteRequiredHops(args[1:])}
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
