package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"dropcheck/controller/internal/adb"
	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/controlpb"
	"google.golang.org/grpc"
)

const defaultPackageName = "io.dropcheck.agent"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
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

type shellOptions struct {
	adbPath     string
	serial      string
	packageName string
}

func parseTopLevelArgs(args []string) (shellOptions, []string, error) {
	opts := shellOptions{
		adbPath:     "adb",
		serial:      os.Getenv("ADB_SERIAL"),
		packageName: defaultPackageName,
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
			opts.adbPath = value
		case "--serial", "-serial":
			if !hasValue {
				if i+1 >= len(args) {
					return opts, nil, fmt.Errorf("%s requires a value", name)
				}
				i++
				value = args[i]
			}
			opts.serial = value
		case "--package", "-package":
			if !hasValue {
				if i+1 >= len(args) {
					return opts, nil, fmt.Errorf("%s requires a value", name)
				}
				i++
				value = args[i]
			}
			opts.packageName = value
		default:
			return opts, append([]string(nil), args[i:]...), nil
		}
	}
	return opts, nil, nil
}

type controlSession struct {
	server *control.Server
	agents []control.AgentInfo

	grpcServer *grpc.Server
	serveDone  chan error
	reversed   []adb.Client
	port       int
	closeOnce  sync.Once
}

func startControlSession(ctx context.Context, opts shellOptions) (*controlSession, error) {
	token, err := control.RandomHex(24)
	if err != nil {
		return nil, fmt.Errorf("create session token: %w", err)
	}
	targets, err := discoverADBTargets(ctx, adb.Client{Path: opts.adbPath}, opts.serial)
	if err != nil {
		return nil, err
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen grpc control: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port

	server := grpc.NewServer()
	controlServer := control.NewServer(token, func(event control.LogEvent) {
		if event.Level == controlpb.CommandLog_LEVEL_DEBUG || event.Level == controlpb.CommandLog_LEVEL_INFO {
			return
		}
		prefix := levelName(event.Level)
		if event.CommandID != "" {
			fmt.Fprintf(os.Stderr, "[%s agent=%s command=%s] %s\n", prefix, empty(event.AgentID, "unknown"), event.CommandID, event.Message)
			return
		}
		fmt.Fprintf(os.Stderr, "[%s agent=%s] %s\n", prefix, empty(event.AgentID, "unknown"), event.Message)
	})
	controlServer.Register(server)

	serveDone := make(chan error, 1)
	go func() {
		serveDone <- server.Serve(listener)
	}()
	session := &controlSession{
		server:     controlServer,
		grpcServer: server,
		serveDone:  serveDone,
		port:       port,
	}
	cleanupOnError := true
	defer func() {
		if cleanupOnError {
			session.Close()
		}
	}()

	fmt.Fprintf(os.Stderr, "dropcheck: grpc=127.0.0.1:%d adb_reverse=tcp:%d package=%s devices=%d\n", port, port, opts.packageName, len(targets))
	for _, target := range targets {
		client := adb.Client{Path: opts.adbPath, Serial: target.Serial}
		if err := client.Reverse(ctx, port); err != nil {
			return nil, fmt.Errorf("configure adb reverse serial=%s: %w", target.Serial, err)
		}
		session.reversed = append(session.reversed, client)
		fmt.Fprintf(os.Stderr, "dropcheck: starting agent serial=%s\n", target.Serial)
		if out, err := client.StartAgentSession(ctx, opts.packageName, port, token, target.Serial, target.Serial); err != nil {
			return nil, fmt.Errorf("start Android agent serial=%s: %w\n%s", target.Serial, err, strings.TrimSpace(out))
		}
	}

	waitCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	infos, err := controlServer.WaitAgents(waitCtx, len(targets))
	cancel()
	if err != nil {
		return nil, fmt.Errorf("wait for Android agents: connected=%d expected=%d: %w", len(infos), len(targets), err)
	}
	for _, info := range infos {
		printAgentConnected(info)
	}
	session.agents = infos
	cleanupOnError = false
	return session, nil
}

func (s *controlSession) Close() {
	if s == nil {
		return
	}
	s.closeOnce.Do(func() {
		if s.grpcServer != nil {
			s.grpcServer.Stop()
		}
		if s.serveDone != nil {
			select {
			case <-s.serveDone:
			case <-time.After(time.Second):
			}
		}
		removeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, client := range s.reversed {
			_ = client.RemoveReverse(removeCtx, s.port)
		}
	})
}

func runShell(ctx context.Context, opts shellOptions) error {
	session, err := startControlSession(ctx, opts)
	if err != nil {
		return err
	}
	defer session.Close()

	state := &shellState{server: session.server}
	if len(session.agents) > 0 {
		state.setSelectedAgent(session.agents[0])
		fmt.Fprintf(os.Stderr, "dropcheck: selected agent=%s\n", agentDisplayName(session.agents[0]))
	}
	return repl(ctx, state)
}

func discoverADBTargets(ctx context.Context, client adb.Client, serial string) ([]adb.Device, error) {
	if serial != "" {
		return []adb.Device{{Serial: serial, State: "device"}}, nil
	}
	devices, err := client.ListDevices(ctx)
	if err != nil {
		return nil, fmt.Errorf("list adb devices: %w", err)
	}
	var targets []adb.Device
	var skipped []string
	for _, device := range devices {
		if device.State == "device" {
			targets = append(targets, device)
			continue
		}
		skipped = append(skipped, fmt.Sprintf("%s(%s)", device.Serial, device.State))
	}
	if len(skipped) > 0 {
		fmt.Fprintf(os.Stderr, "dropcheck: skipped adb devices %s\n", strings.Join(skipped, ", "))
	}
	if len(targets) == 0 {
		return nil, errors.New("no connected adb devices; connect a device or pass --serial")
	}
	return targets, nil
}

func printAgentConnected(info control.AgentInfo) {
	hello := info.Hello
	device := hello.GetDevice()
	fmt.Fprintf(os.Stderr, "dropcheck: agent id=%s adb_serial=%s session=%s app=%s device=%s/%s sdk=%d\n",
		info.ID,
		empty(hello.GetAdbSerial(), "unknown"),
		info.SessionID,
		hello.AppVersion,
		device.GetManufacturer(),
		device.GetModel(),
		device.GetSdk(),
	)
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
