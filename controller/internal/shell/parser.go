package shell

import (
	"fmt"
	"strings"

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
	Profile
	Profiles
	LastReport
)

type Command struct {
	Kind                             CommandKind
	Operation                        command.Operation
	ADBDiagnosticsKind               string
	HelpTopic                        string
	Pipeline                         pipeline.Pipeline
	ProfileName, SSID, Family, BSSID string
	Detail                           bool
	Use                              bool
	Rejection                        string
}

func ParseLine(line string) (Command, error) {
	rejectCheck := func(err error) (Command, error) {
		if strings.HasPrefix(strings.TrimSpace(line), "check ") {
			return Command{Kind: Profile, ProfileName: "link", Rejection: "invalid check syntax"}, nil
		}
		return Command{}, err
	}
	parts, err := pipeline.Split(line)
	if err != nil {
		return rejectCheck(err)
	}
	pipe, err := pipeline.Parse(parts[1:])
	if err != nil {
		return rejectCheck(err)
	}
	args, err := command.SplitArgs(parts[0])
	if err != nil {
		return rejectCheck(err)
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
	result := Command{Kind: AgentCommand, Operation: parsed.Operation, ADBDiagnosticsKind: parsed.ADBKind, HelpTopic: parsed.Topic, Pipeline: pipe, ProfileName: parsed.Profile, SSID: parsed.SSID, Family: parsed.Family, BSSID: parsed.BSSID, Detail: parsed.Detail, Use: parsed.Path == "use", Rejection: parsed.Rejection}
	switch parsed.Path {
	case "check":
		result.Kind = Profile
	case "show checks":
		result.Kind = Profiles
	case "show check last":
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
