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

// Run executes the dropcheck controller application for args.
//
// args must not include argv[0]. Run handles top-level flags, chooses shell or
// one-shot CLI mode, starts the required control session, and returns any
// user-facing error to the executable wrapper.
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
	return errors.New("usage: dropcheck [--adb adb] [--serial SERIAL] [--package PACKAGE] [--listen ADDR] [--no-adb] shell | <command>")
}

type shellOptions = session.Options

func parseTopLevelArgs(args []string) (shellOptions, []string, error) {
	opts := shellOptions{
		ADBPath:     "adb",
		Serial:      os.Getenv("ADB_SERIAL"),
		PackageName: session.DefaultPackageName,
		ListenAddr:  "127.0.0.1:0",
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
		case "--listen":
			if !hasValue {
				if i+1 >= len(args) {
					return opts, nil, fmt.Errorf("%s requires a value", name)
				}
				i++
				value = args[i]
			}
			opts.ListenAddr = value
		case "--no-adb":
			if hasValue {
				return opts, nil, fmt.Errorf("%s does not take a value", name)
			}
			opts.NoADB = true
		default:
			return opts, append([]string(nil), args[i:]...), nil
		}
	}
	return opts, nil, nil
}

func runShell(ctx context.Context, opts shellOptions) error {
	controlSession, err := startControlSession(ctx, opts)
	if err != nil {
		return err
	}
	defer controlSession.Close()

	state := &shellState{server: controlSession.Server}
	state.controllerToken = controlSession.Token
	if len(controlSession.Agents) > 0 {
		state.setSelectedAgent(controlSession.Agents[0])
		fmt.Fprintf(os.Stderr, "dropcheck: selected agent=%s\n", agentDisplayName(controlSession.Agents[0]))
	}
	return repl(ctx, state)
}

type shellState struct {
	server          *control.Server
	controllerToken string
	selected        string
	selectedLabel   string
	targetAll       bool
	requestMode     bool
}

func (s *shellState) setSelectedAgent(info control.AgentInfo) {
	s.selected = info.ID
	s.selectedLabel = agentDisplayName(info)
}
