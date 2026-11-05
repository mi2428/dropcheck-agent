package shell

import (
	"fmt"

	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/pipeline"
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
	Profile
	Profiles
	LastReport
	SetDefaultPassphrase
	ClearDefaultPassphrase
	Connect
	CLI
	WifiTest
)

type Command struct {
	Kind                             CommandKind
	Operation                        command.Operation
	ADBDiagnosticsKind               string
	HelpTopic                        string
	Pipeline                         pipeline.Pipeline
	ProfileName, SSID, Family, BSSID string
	Lab                              command.LabConfig
	Detail                           bool
	Rejection                        string
	Passphrase                       string
	Path, Effect                     string
	ConnectOptions                   *command.WifiConnectOptions
	ADBTimeoutMs                     uint32
}

func ParseLine(line string) (Command, error) {
	rejectTest := func(err error) (Command, error) {
		lexed, _ := command.Lex(line, true)
		var args []string
		for _, token := range lexed.Tokens {
			if token.Pipe {
				break
			}
			args = append(args, token.Value)
		}
		if name := command.ShellTestProfile(args); name != "" {
			kind := Profile
			if name == "wifi" {
				kind = WifiTest
			}
			return Command{Kind: kind, Path: "test " + name, ProfileName: name, Rejection: "invalid test syntax"}, nil
		}
		return Command{}, err
	}
	parts, err := pipeline.Split(line)
	if err != nil {
		return rejectTest(err)
	}
	pipe, err := pipeline.Parse(parts[1:])
	if err != nil {
		return rejectTest(err)
	}
	args, err := command.SplitArgs(parts[0])
	if err != nil {
		return rejectTest(err)
	}
	if len(args) == 0 {
		return Command{Kind: Noop}, nil
	}
	if args[0] == "exit" {
		if len(args) != 1 || len(parts) > 1 {
			return Command{}, fmt.Errorf("usage: %s", args[0])
		}
		return Command{Kind: Exit}, nil
	}
	if len(args) == 1 && args[0] == "?" && len(parts) == 1 {
		return Command{Kind: Help}, nil
	}
	parsed, err := command.ParseShellTokens(args)
	if err != nil {
		return Command{}, err
	}
	result := Command{Kind: AgentCommand, Operation: parsed.Operation, ADBDiagnosticsKind: parsed.ADBKind, HelpTopic: parsed.Topic, Pipeline: pipe, ProfileName: parsed.Profile, SSID: parsed.SSID, Family: parsed.Family, BSSID: parsed.BSSID, Lab: parsed.Lab, Detail: parsed.Detail, Rejection: parsed.Rejection, Passphrase: parsed.Passphrase, Path: parsed.Path, Effect: parsed.Effect, ConnectOptions: parsed.Connect, ADBTimeoutMs: parsed.ADBTimeoutMs}
	if parsed.Rejection != "" && parsed.Profile != "" {
		result.Kind = Profile
		if parsed.Profile == "wifi" {
			result.Kind = WifiTest
		}
		return result, nil
	}
	switch parsed.Path {
	case "set cli default-passphrase":
		result.Kind = SetDefaultPassphrase
	case "clear cli default-passphrase":
		result.Kind = ClearDefaultPassphrase
	case "request wifi connect":
		result.Kind = Connect
	case "show cli":
		result.Kind = CLI
	case "test wifi":
		result.Kind = WifiTest
	case "test link", "test lab":
		result.Kind = Profile
	case "show test profiles":
		result.Kind = Profiles
	case "show test last":
		result.Kind = LastReport
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
