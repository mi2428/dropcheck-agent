package app

import (
	"context"
	"fmt"

	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/linuxcli"
)

func runCLI(ctx context.Context, opts shellOptions, rawArgs []string) error {
	cliOpts, args, err := linuxcli.ExtractOptions(rawArgs)
	if err != nil {
		return err
	}
	if cliOpts.Format == "" {
		cliOpts.Format = outputText
	}
	command, err := linuxcli.Parse(args)
	if err != nil {
		return err
	}

	controlSession, err := startControlSession(ctx, opts)
	if err != nil {
		return err
	}
	defer controlSession.Close()

	state := &shellState{server: controlSession.Server, controllerToken: controlSession.Token}
	if len(controlSession.Agents) > 0 {
		state.setSelectedAgent(controlSession.Agents[0])
	}
	if cliOpts.All {
		state.targetAll = true
	}
	if cliOpts.Target != "" {
		info, err := resolveShellAgent(state, cliOpts.Target)
		if err != nil {
			return err
		}
		state.setSelectedAgent(info)
		state.targetAll = false
	}

	switch command.Kind {
	case linuxcli.Devices:
		out, err := renderAgents(agentListView(state), cliOpts.Format)
		if err != nil {
			return err
		}
		fmt.Print(out)
		return nil
	case linuxcli.Target:
		out, err := renderTarget(targetView(state), cliOpts.Format)
		if err != nil {
			return err
		}
		fmt.Print(out)
		return nil
	case linuxcli.FestivalSync:
		return syncFestivalRuns(ctx, state, festivalSyncOptions{
			OutputDir:  command.FestivalSyncOutput,
			Limit:      command.FestivalSyncLimit,
			MarkSynced: command.FestivalSyncMark,
		})
	default:
		agents, err := state.commandTargets()
		if err != nil {
			return err
		}
		return runOperationForAgents(ctx, state, agents, command.Operation, commandOutputOptions{format: cliOpts.Format, strict: true})
	}
}

func (s *shellState) commandTargets() ([]control.AgentInfo, error) {
	if s.targetAll {
		agents := s.server.Agents()
		if len(agents) == 0 {
			return nil, fmt.Errorf("no Android agents connected")
		}
		return agents, nil
	}
	info, err := selectedAgent(s)
	if err != nil {
		return nil, err
	}
	return []control.AgentInfo{info}, nil
}

type cliOptions struct {
	format outputFormat
	target string
	all    bool
}

type cliCommandKind int

const (
	cliAgentCommand cliCommandKind = iota
	cliDevices
	cliTarget
)

type cliCommand struct {
	kind      cliCommandKind
	operation Operation
}

func extractCLIOptions(args []string) (cliOptions, []string, error) {
	opts, rest, err := linuxcli.ExtractOptions(args)
	return cliOptions{format: opts.Format, target: opts.Target, all: opts.All}, rest, err
}

func parseLinuxCommand(args []string) (cliCommand, error) {
	parsed, err := linuxcli.Parse(args)
	return cliCommand{
		kind:      cliCommandKind(parsed.Kind),
		operation: parsed.Operation,
	}, err
}
