package main

import (
	"fmt"
	"slices"
	"strings"

	"dropcheck/controller/internal/command"
)

var localShellCommands = []string{
	"exit",
	"quit",
	"help",
	"devices",
	"use",
	"all",
}

var agentCommands = command.AgentCommands()

var shellCommands = append(append([]string{}, localShellCommands...), agentCommands...)

func normalizeShellCommandArgs(args []string) ([]string, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("empty command")
	}
	if strings.HasPrefix(args[0], "@") {
		return append([]string(nil), args...), nil
	}
	normalized := append([]string(nil), args...)
	command, err := resolveUniquePrefix("command", normalized[0], shellCommands)
	if err != nil {
		return nil, err
	}
	normalized[0] = command
	if isCommand(command, agentCommands) {
		return normalizeAgentCommandArgs(normalized)
	}
	return normalized, nil
}

func normalizeAgentCommandArgs(args []string) ([]string, error) {
	return command.NormalizeAgentCommandArgs(args)
}

func resolveUniquePrefix(kind string, value string, candidates []string) (string, error) {
	return command.ResolveUniquePrefix(kind, value, candidates)
}

func isCommand(value string, commands []string) bool {
	return slices.Contains(commands, value)
}
