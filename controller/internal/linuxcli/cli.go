package linuxcli

import (
	"fmt"
	"strings"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/pipeline"
)

type Options struct {
	Format pipeline.Format
	Target string
	All    bool
}

type Kind int

const (
	AgentCommand Kind = iota
	Devices
	Version
	Help
	ADBDiagnostics
	Profile
	Profiles
	LastReport
)

type Command struct {
	Kind                             Kind
	Operation                        command.Operation
	ADBKind                          string
	HelpTopic                        string
	ProfileName, SSID, Family, BSSID string
	Detail                           bool
	Use                              bool
	Rejection                        string
}

// Host options are recognized only before the first network command token.
// A positional HOST/SSID/URL equal to --target or --format remains literal.
func ExtractOptions(args []string) (Options, []string, error) {
	var opts Options
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return opts, args[i+1:], nil
		}
		name, value, inline := strings.Cut(arg, "=")
		if name != "--format" && name != "--target" && name != "--all" {
			if opts.All && opts.Target != "" {
				return opts, nil, fmt.Errorf("--all and --target cannot be combined")
			}
			return opts, args[i:], nil
		}
		if name == "--all" {
			if inline || opts.All {
				return opts, nil, fmt.Errorf("invalid --all")
			}
			opts.All = true
			continue
		}
		if !inline {
			i++
			if i >= len(args) {
				return opts, nil, fmt.Errorf("%s requires a value", name)
			}
			value = args[i]
		}
		if name == "--target" {
			if opts.Target != "" || value == "" {
				return opts, nil, fmt.Errorf("invalid --target")
			}
			opts.Target = value
		} else {
			if opts.Format != "" || value != "text" && value != "json" {
				return opts, nil, fmt.Errorf("invalid --format")
			}
			opts.Format = pipeline.Format(value)
		}
	}
	if opts.All && opts.Target != "" {
		return opts, nil, fmt.Errorf("--all and --target cannot be combined")
	}
	return opts, nil, nil
}

func Parse(args []string) (Command, error) {
	parsed, err := command.ParseTokens(args)
	if err != nil {
		return Command{}, err
	}
	result := Command{Kind: AgentCommand, Operation: parsed.Operation, ADBKind: parsed.ADBKind, HelpTopic: parsed.Topic, ProfileName: parsed.Profile, SSID: parsed.SSID, Family: parsed.Family, BSSID: parsed.BSSID, Detail: parsed.Detail, Use: parsed.Path == "use", Rejection: parsed.Rejection}
	switch parsed.Path {
	case "check":
		result.Kind = Profile
	case "show checks":
		result.Kind = Profiles
	case "show check last":
		result.Kind = LastReport
	case "show devices":
		result.Kind = Devices
	case "show version":
		result.Kind = Version
	case "help":
		result.Kind = Help
	default:
		if parsed.ADBKind != "" {
			result.Kind = ADBDiagnostics
		}
	}
	return result, nil
}
