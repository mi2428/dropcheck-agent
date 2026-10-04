package app

import (
	"context"
	"fmt"
	"os"

	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/linuxcli"
	"dropcheck/controller/internal/version"
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
	if command.Kind == linuxcli.Version {
		fmt.Println(version.Version)
		return nil
	}
	if command.Kind == linuxcli.Help {
		writeCommandHelp(os.Stdout, command.HelpTopic)
		return nil
	}
	if command.Kind == linuxcli.Profiles {
		out, err := showProfiles(cliOpts.Format)
		if err == nil {
			fmt.Print(out)
		}
		return err
	}
	if command.Kind == linuxcli.LastReport {
		fmt.Println("No report in this process; show check last is Shell historical state only.")
		return nil
	}
	if command.Kind == linuxcli.Profile && command.ProfileName == "" {
		fmt.Println(profileCandidates())
		return nil
	}
	if command.Kind == linuxcli.Profile && command.Rejection != "" {
		return (&shellState{}).runCheck(ctx, command.ProfileName, command.SSID, command.Family, command.BSSID, command.Rejection, cliOpts.Format, pipePipeline{}, true)
	}

	controlSession, err := startControlSession(ctx, opts)
	if err != nil {
		return err
	}
	defer controlSession.Close()

	state := &shellState{server: controlSession.Server, adbPath: opts.ADBPath}
	if len(controlSession.Agents) == 1 {
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
	case linuxcli.Profile:
		return state.runCheck(ctx, command.ProfileName, command.SSID, command.Family, command.BSSID, command.Rejection, cliOpts.Format, pipePipeline{}, true)
	case linuxcli.Devices:
		out, err := renderAgents(agentListView(state), cliOpts.Format)
		if err != nil {
			return err
		}
		fmt.Print(out)
		return nil
	case linuxcli.ADBDiagnostics:
		agents, err := state.commandTargets()
		if err != nil {
			return err
		}
		return runADBDiagnosticsForAgents(ctx, state, agents, command.ADBKind, commandOutputOptions{format: cliOpts.Format, strict: true})
	default:
		agents, err := state.commandTargets()
		if err != nil {
			return err
		}
		return runOperationForAgents(ctx, state, agents, command.Operation, commandOutputOptions{format: cliOpts.Format, strict: true, use: command.Use})
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
