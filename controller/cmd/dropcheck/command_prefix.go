package main

import (
	"fmt"
	"strings"
)

var localShellCommands = []string{
	"exit",
	"quit",
	"help",
	"devices",
	"use",
	"all",
}

var agentCommands = []string{
	"wifi",
	"ip",
	"ping",
	"traceroute",
	"path-mtu",
	"global-ip",
	"download",
	"dns",
	"http",
}

var shellCommands = append(append([]string{}, localShellCommands...), agentCommands...)

var wifiCommands = []string{
	"status",
	"diagnostics",
	"scan",
	"capabilities",
	"connect",
	"disconnect",
	"forget",
	"wait",
	"assert",
	"watch",
	"monitor",
	"reconnect",
	"cycle",
}

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
	if len(args) == 0 {
		return nil, fmt.Errorf("empty command")
	}
	normalized := append([]string(nil), args...)
	command, err := resolveUniquePrefix("command", normalized[0], agentCommands)
	if err != nil {
		return nil, err
	}
	normalized[0] = command
	if command != "wifi" || len(normalized) < 2 {
		return normalized, nil
	}
	wifiCommand, err := resolveUniquePrefix("wifi command", normalized[1], wifiCommands)
	if err != nil {
		return nil, err
	}
	normalized[1] = wifiCommand
	switch wifiCommand {
	case "scan":
		normalizeOptionalSubcommand(normalized, 2, "wifi scan command", []string{"fresh", "detail"})
	case "wait":
		if len(normalized) >= 3 {
			value, err := resolveUniquePrefix("wifi wait command", normalized[2], []string{"connected"})
			if err != nil {
				return nil, err
			}
			normalized[2] = value
		}
	}
	return normalized, nil
}

func normalizeOptionalSubcommand(args []string, index int, kind string, candidates []string) {
	if len(args) <= index || strings.HasPrefix(args[index], "--") {
		return
	}
	value, err := resolveUniquePrefix(kind, args[index], candidates)
	if err == nil {
		args[index] = value
	}
}

func resolveUniquePrefix(kind string, value string, candidates []string) (string, error) {
	if value == "" {
		return "", fmt.Errorf("empty %s", kind)
	}
	for _, candidate := range candidates {
		if value == candidate {
			return candidate, nil
		}
	}
	var matches []string
	for _, candidate := range candidates {
		if strings.HasPrefix(candidate, value) {
			matches = append(matches, candidate)
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("unknown %s %q", kind, value)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("ambiguous %s %q (matches %s)", kind, value, strings.Join(matches, ", "))
	}
}

func isCommand(value string, commands []string) bool {
	for _, command := range commands {
		if value == command {
			return true
		}
	}
	return false
}
