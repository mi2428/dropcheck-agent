package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/session"
	"dropcheck/controller/internal/version"
)

var errHelpRequested = errors.New("help requested")

type helpRow struct {
	usage       string
	description string
}

// Run executes the dropcheck controller application for args.
//
// args must not include argv[0]. Run handles top-level flags, chooses shell or
// one-shot CLI mode, starts the required control session, and returns any
// user-facing error to the executable wrapper.
func Run(args []string) error {
	if len(args) == 1 && (args[0] == "--version" || args[0] == "-version") {
		fmt.Println(version.Version)
		return nil
	}
	opts, rest, err := parseTopLevelArgs(args)
	if err != nil {
		if errors.Is(err, errHelpRequested) {
			writeTopLevelHelp(os.Stdout)
			return nil
		}
		return fmt.Errorf("%w\n\nrun 'dropcheck --help' for usage", err)
	}
	if len(rest) == 1 && rest[0] == "help" {
		writeTopLevelHelp(os.Stdout)
		return nil
	}
	if len(rest) == 1 && rest[0] == "shell" {
		return runShell(context.Background(), opts, nil)
	}
	if len(rest) > 0 && rest[0] == "shell" {
		return runShell(context.Background(), opts, rest[1:])
	}
	if len(rest) == 0 {
		writeTopLevelHelp(os.Stdout)
		return nil
	}
	return runCLI(context.Background(), opts, rest)
}

func topLevelHelp() string {
	var b bytes.Buffer
	writeTopLevelHelp(&b)
	return b.String()
}

func writeTopLevelHelp(w io.Writer) {
	_, _ = fmt.Fprintln(w, "Dropcheck controller.")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Usage:")
	_, _ = fmt.Fprintln(w, "  dropcheck [flags] shell [--target TARGET]")
	_, _ = fmt.Fprintln(w, "  dropcheck [flags] [--format text|json] [--target TARGET|--all] <command>")
	_, _ = fmt.Fprintln(w, "  dropcheck --version")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Commands:")
	writeHelpRows(w, []helpRow{
		{"shell", "start the interactive controller shell"},
		{"show devices", "list connected Android agents"},
		{"show config [standalone]", "print agent configuration"},
		{"show wifi <topic>", "show Wi-Fi status and diagnostics"},
		{"show ip status", "show IP and routing status"},
		{"show standalone <topic>", "show standalone runs and status"},
		{"configure <set|delete> ...", "edit agent configuration"},
		{"clear standalone runs [synced|all]", "delete stored runs"},
		{"sync standalone runs [options]", "download stored standalone runs"},
		{"request <command> ...", "run a one-shot agent operation"},
	})
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Global flags:")
	writeHelpRows(w, []helpRow{
		{"-h, --help", "show this help"},
		{"--version", "print version and exit"},
		{"--adb PATH", `adb executable (default "adb")`},
		{"--serial SERIAL", "adb serial; defaults to ADB_SERIAL"},
		{"--package PACKAGE", fmt.Sprintf("Android package (default %q)", session.DefaultPackageName)},
		{"--listen ADDR", `gRPC listen address (default "127.0.0.1:0")`},
	})
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "CLI output and target flags:")
	writeHelpRows(w, []helpRow{
		{"--format text|json", "output format for one-shot commands"},
		{"--target TARGET", "select one agent by ID, prefix, or adb serial"},
		{"--all", "run agent commands on all connected agents"},
	})
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Common request commands:")
	writeHelpRows(w, []helpRow{
		{"request wifi scan fresh [options]", "run a fresh Wi-Fi scan"},
		{"request wifi connect <ssid>", "connect to Wi-Fi"},
		{"request ping <host> [options]", "run ICMP ping"},
		{"request traceroute <host>", "run traceroute"},
		{"request dns <name> [options]", "resolve DNS"},
		{"request http <url> [options]", "check HTTP status"},
	})
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, `Notes:
  Top-level flags accept either single or double dash, for example -serial or --serial.

Examples:
  dropcheck shell
  dropcheck --serial R5CT12345 shell
  dropcheck --format json show devices
  dropcheck request ping 1.1.1.1 --count 5
  dropcheck request wifi scan fresh --timeout 9000`)
}

func writeHelpRows(w io.Writer, rows []helpRow) {
	const usageWidth = 36
	for _, row := range rows {
		if row.description == "" {
			_, _ = fmt.Fprintf(w, "  %s\n", row.usage)
			continue
		}
		if len(row.usage) > usageWidth {
			_, _ = fmt.Fprintf(w, "  %s\n  %-*s  %s\n", row.usage, usageWidth, "", row.description)
			continue
		}
		_, _ = fmt.Fprintf(w, "  %-*s  %s\n", usageWidth, row.usage, row.description)
	}
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
		case "--help", "-help", "-h":
			return opts, nil, errHelpRequested
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
		case "--listen", "-listen":
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
