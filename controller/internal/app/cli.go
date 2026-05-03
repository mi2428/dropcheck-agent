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

	state := &shellState{server: controlSession.Server}
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
	case linuxcli.Config:
		agents, err := state.commandTargets()
		if err != nil {
			return err
		}
		return runConfigForAgents(ctx, state, agents, command.ConfigScope, commandOutputOptions{format: cliOpts.Format, strict: true})
	case linuxcli.StandaloneSync:
		return syncStandaloneRuns(ctx, state, standaloneSyncOptions{
			OutputDir:  command.StandaloneSyncOutput,
			Limit:      command.StandaloneSyncLimit,
			MarkSynced: command.StandaloneSyncMark,
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
