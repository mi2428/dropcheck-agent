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
		return runShell(context.Background(), opts, nil)
	}
	if len(rest) > 0 && rest[0] == "shell" {
		return runShell(context.Background(), opts, rest[1:])
	}
	if len(rest) == 0 {
		return usage()
	}
	return runCLI(context.Background(), opts, rest)
}

func usage() error {
	return errors.New("usage: dropcheck [--adb adb] [--serial SERIAL] [--package PACKAGE] [--listen ADDR] shell [--target TARGET] | <command>")
}

type shellOptions = session.Options

type shellTargetOptions struct {
	target string
}

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
		default:
			return opts, append([]string(nil), args[i:]...), nil
		}
	}
	return opts, nil, nil
}

func runShell(ctx context.Context, opts shellOptions, args []string) error {
	targetOpts, err := parseShellTargetOptions(args)
	if err != nil {
		return err
	}
	controlSession, err := startControlSession(ctx, opts)
	if err != nil {
		return err
	}
	defer controlSession.Close()

	state := &shellState{server: controlSession.Server, adbPath: opts.ADBPath}
	if err := selectShellStartupTarget(state, controlSession.Agents, targetOpts.target); err != nil {
		return err
	}
	return repl(ctx, state)
}

func parseShellTargetOptions(args []string) (shellTargetOptions, error) {
	var opts shellTargetOptions
	for i := 0; i < len(args); i++ {
		name, value, hasValue := strings.Cut(args[i], "=")
		switch name {
		case "--target":
			if !hasValue {
				if i+1 >= len(args) {
					return opts, fmt.Errorf("--target requires a value")
				}
				i++
				value = args[i]
			}
			if opts.target != "" {
				return opts, fmt.Errorf("--target specified twice")
			}
			opts.target = value
		default:
			return opts, fmt.Errorf("unknown shell option %q", args[i])
		}
	}
	return opts, nil
}

func selectShellStartupTarget(state *shellState, agents []control.AgentInfo, target string) error {
	if target == "all" {
		state.targetAll = true
		fmt.Fprintln(os.Stderr, "dropcheck: selected agent=all")
		return nil
	}
	if target != "" {
		info, err := resolveShellAgent(state, target)
		if err != nil {
			return err
		}
		state.setSelectedAgent(info)
		fmt.Fprintf(os.Stderr, "dropcheck: selected agent=%s\n", agentDisplayName(info))
		return nil
	}
	switch len(agents) {
	case 0:
		return nil
	case 1:
		state.setSelectedAgent(agents[0])
		fmt.Fprintf(os.Stderr, "dropcheck: selected agent=%s\n", agentDisplayName(agents[0]))
		return nil
	default:
		return fmt.Errorf("multiple agents connected; start shell with --target <agent|serial|number|all>")
	}
}

type shellMode int

const (
	shellModeOperational shellMode = iota
	shellModeConfigure
	shellModeRequest
)

type shellState struct {
	server        *control.Server
	adbPath       string
	selected      string
	selectedLabel string
	targetAll     bool
	mode          shellMode
}

func (s *shellState) setSelectedAgent(info control.AgentInfo) {
	s.selected = info.ID
	s.selectedLabel = agentDisplayName(info)
}
