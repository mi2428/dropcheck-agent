package session

import (
	"context"
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

// DefaultPackageName is the Android application ID used when the user does not
// pass --package.
const DefaultPackageName = "io.dropcheck.agent"

const adbReversePortCollisionAttempts = 5

// Options configures control-session startup.
type Options struct {
	// ADBPath is the adb executable path. Empty uses "adb".
	ADBPath string
	// Serial optionally selects one adb device for commands that need it.
	Serial string
	// PackageName is the Android app package that contains .AgentService.
	PackageName string
	// ListenAddr is the controller gRPC listen address. Empty uses 127.0.0.1:0.
	ListenAddr string
}

// Session represents a running controller session and its cleanup handles.
type Session struct {
	// Server dispatches commands to the connected Android agents.
	Server *control.Server
	// Agents are the agents that connected during startup.
	Agents []control.AgentInfo
	// Token is the current controller session token accepted by the gRPC server.
	Token string
	// ListenAddr is the concrete local address reported by the listener.
	ListenAddr string

	grpcServer *grpc.Server
	serveDone  chan error
	reversed   []adb.Client
	port       int
	closeOnce  sync.Once
}

// Start opens a local gRPC server, starts agents on targets, and waits for them
// to connect.
//
// targets must be the explicit adb devices selected by the app package. On
// startup failure, Start cleans up any gRPC server and adb reverse rules it
// already created.
func Start(ctx context.Context, opts Options, targets []adb.Device) (*Session, error) {
	if len(targets) == 0 {
		return nil, fmt.Errorf("no adb devices selected")
	}
	if opts.ADBPath == "" {
		opts.ADBPath = "adb"
	}
	if opts.PackageName == "" {
		opts.PackageName = DefaultPackageName
	}
	if opts.ListenAddr == "" {
		opts.ListenAddr = "127.0.0.1:0"
	}
	token, err := control.RandomHex(24)
	if err != nil {
		return nil, fmt.Errorf("create session token: %w", err)
	}

	attempts := 1
	if listenAddrUsesEphemeralPort(opts.ListenAddr) {
		attempts = adbReversePortCollisionAttempts
	}
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		session, err := startOnce(ctx, opts, targets, token)
		if err == nil {
			return session, nil
		}
		lastErr = err
		if attempt == attempts || !isADBReversePortCollision(err) {
			return nil, err
		}
		fmt.Fprintf(os.Stderr, "dropcheck: adb reverse port collision; retrying with a new local port attempt=%d/%d\n", attempt+1, attempts)
	}
	return nil, lastErr
}

func startOnce(ctx context.Context, opts Options, targets []adb.Device, token string) (*Session, error) {
	listener, err := net.Listen("tcp", opts.ListenAddr)
	if err != nil {
		return nil, fmt.Errorf("listen grpc control: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listenAddr := listener.Addr().String()

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
	session := &Session{
		Server:     controlServer,
		Token:      token,
		ListenAddr: listenAddr,
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

	fmt.Fprintf(os.Stderr, "dropcheck: grpc=%s adb_reverse=tcp:%d package=%s devices=%d\n", listenAddr, port, opts.PackageName, len(targets))
	for _, target := range targets {
		client := adb.Client{Path: opts.ADBPath, Serial: target.Serial}
		if err := client.Reverse(ctx, port); err != nil {
			return nil, fmt.Errorf("configure adb reverse serial=%s: %w", target.Serial, err)
		}
		session.reversed = append(session.reversed, client)
		fmt.Fprintf(os.Stderr, "dropcheck: starting agent serial=%s\n", target.Serial)
		if out, err := client.StartAgentSession(ctx, opts.PackageName, port, token, target.Serial, target.Serial); err != nil {
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
	session.Agents = infos
	cleanupOnError = false
	return session, nil
}

func listenAddrUsesEphemeralPort(addr string) bool {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	return port == "0"
}

func isADBReversePortCollision(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "cannot bind listener") ||
		strings.Contains(message, "Address already in use")
}

// Close stops the gRPC server and removes adb reverse rules.
//
// Close is idempotent and may be called after a partially failed Start.
func (s *Session) Close() {
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

func levelName(level controlpb.CommandLog_Level) string {
	switch level {
	case controlpb.CommandLog_LEVEL_DEBUG:
		return "debug"
	case controlpb.CommandLog_LEVEL_WARN:
		return "warn"
	case controlpb.CommandLog_LEVEL_ERROR:
		return "error"
	default:
		return "info"
	}
}

func empty(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
