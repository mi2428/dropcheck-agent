package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"dropcheck/controller/internal/adb"
	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/linuxcli"
	"dropcheck/controller/internal/render"
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
		return (&shellState{}).runCheck(ctx, command.ProfileName, command.SSID, command.Family, command.BSSID, command.Rejection, cliOpts.Format, pipePipeline{}, true, command.Lab)
	}
	if command.Kind == linuxcli.Devices {
		if cliOpts.Target != "" || cliOpts.All {
			return fmt.Errorf("show devices lists the ADB inventory; --target and --all require an agent session")
		}
		devices, err := (adb.Client{Path: opts.ADBPath}).ListDevices(ctx)
		if err != nil {
			return err
		}
		if cliOpts.Format == outputJSON {
			type deviceRow struct {
				Serial string `json:"serial"`
				State  string `json:"state"`
				Model  string `json:"model,omitempty"`
			}
			rows := make([]deviceRow, 0, len(devices))
			for _, device := range devices {
				rows = append(rows, deviceRow{device.Serial, device.State, device.Details["model"]})
			}
			return json.NewEncoder(os.Stdout).Encode(struct {
				Devices []deviceRow `json:"devices"`
			}{rows})
		}
		rows := make([][]string, 0, len(devices))
		for _, device := range devices {
			rows = append(rows, []string{device.Serial, device.State, device.Details["model"]})
		}
		fmt.Print(render.Table([]string{"SERIAL", "STATE", "MODEL"}, rows, terminalPresentation()))
		return nil
	}
	if !cliOpts.All {
		targets, err := discoverADBTargets(ctx, adb.Client{Path: opts.ADBPath}, opts.Serial)
		if err != nil {
			return err
		}
		if cliOpts.Target == "" && len(targets) != 1 {
			return fmt.Errorf("multiple Android agents available; specify --target, --serial or --all")
		}
		selected := ""
		if cliOpts.Target == "" {
			selected = targets[0].Serial
		} else {
			for _, target := range targets {
				if target.Serial == cliOpts.Target {
					selected = target.Serial
					break
				}
				if strings.HasPrefix(target.Serial, cliOpts.Target) {
					if selected != "" {
						return fmt.Errorf("ambiguous target serial prefix")
					}
					selected = target.Serial
				}
			}
			if selected == "" {
				return fmt.Errorf("target serial not found")
			}
		}
		opts.Serial = selected
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
	switch command.Kind {
	case linuxcli.Profile:
		return state.runCheck(ctx, command.ProfileName, command.SSID, command.Family, command.BSSID, command.Rejection, cliOpts.Format, pipePipeline{}, true, command.Lab)
	case linuxcli.ADBDiagnostics:
		agents, err := state.commandTargets()
		if err != nil {
			return err
		}
		return runADBDiagnosticsForAgents(ctx, state, agents, command.ADBKind, commandOutputOptions{format: cliOpts.Format, strict: true, adbTimeoutMs: command.ADBTimeoutMs})
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
