package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/session"
)

func Run(args []string) error {
	opts, rest, err := parseTopLevelArgs(args)
	if err != nil {
		return usage()
	}
	if len(rest) == 1 && rest[0] == "shell" {
		return runShell(context.Background(), opts)
	}
	if len(rest) == 0 || rest[0] == "shell" {
		return usage()
	}
	return runCLI(context.Background(), opts, rest)
}

func usage() error {
	return errors.New("usage: dropcheck [--adb adb] [--serial SERIAL] [--package PACKAGE] shell | <command>")
}

type shellOptions = session.Options

func parseTopLevelArgs(args []string) (shellOptions, []string, error) {
	opts := shellOptions{
		ADBPath:     "adb",
		Serial:      os.Getenv("ADB_SERIAL"),
		PackageName: session.DefaultPackageName,
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return opts, append([]string(nil), args[i+1:]...), nil
		}
		if !strings.HasPrefix(arg, "-") {
			return opts, append([]string(nil), args[i:]...), nil
		}
		name, value, hasValue := strings.Cut(arg, "=")
		switch name {
		case "--help", "-h":
			return opts, nil, fmt.Errorf("help requested")
		case "--adb", "-adb":
			if !hasValue {
				if i+1 >= len(args) {
					return opts, nil, fmt.Errorf("%s requires a value", name)
				}
				i++
				value = args[i]
			}
			opts.ADBPath = value
		case "--serial", "-serial":
			if !hasValue {
				if i+1 >= len(args) {
					return opts, nil, fmt.Errorf("%s requires a value", name)
				}
				i++
				value = args[i]
			}
			opts.Serial = value
		case "--package", "-package":
			if !hasValue {
				if i+1 >= len(args) {
					return opts, nil, fmt.Errorf("%s requires a value", name)
				}
				i++
				value = args[i]
			}
			opts.PackageName = value
		default:
			return opts, append([]string(nil), args[i:]...), nil
		}
	}
	return opts, nil, nil
}

func runShell(ctx context.Context, opts shellOptions) error {
	controlSession, err := session.Start(ctx, opts)
	if err != nil {
		return err
	}
	defer controlSession.Close()

	state := &shellState{server: controlSession.Server}
	if len(controlSession.Agents) > 0 {
		state.setSelectedAgent(controlSession.Agents[0])
		fmt.Fprintf(os.Stderr, "dropcheck: selected agent=%s\n", agentDisplayName(controlSession.Agents[0]))
	}
	return repl(ctx, state)
}

type shellState struct {
	server        *control.Server
	selected      string
	selectedLabel string
	targetAll     bool
}

func (s *shellState) setSelectedAgent(info control.AgentInfo) {
	s.selected = info.ID
	s.selectedLabel = agentDisplayName(info)
}
