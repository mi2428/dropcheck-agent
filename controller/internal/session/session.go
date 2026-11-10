package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/mi2428/dropcheck-agent/controller/internal/adb"
	"github.com/mi2428/dropcheck-agent/controller/internal/control"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
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
	// OnLog receives every log level and DEBUG command acknowledgements inline
	// on stream goroutines. Calls may overlap across agents. Handlers must return
	// promptly and must not synchronously close the session. Nil filters DEBUG/INFO
	// and writes other levels to stderr. Discovery/startup diagnostics always use
	// stderr independently.
	OnLog func(control.LogEvent)
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
	closeErr   error
}

// Open discovers ready ADB devices (or uses Serial explicitly), then starts the
// authenticated session. It observes cancellation before invoking ADB.
func Open(ctx context.Context, opts Options) (*Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	targets, err := discoverADBTargets(ctx, adb.Client{Path: opts.ADBPath}, opts.Serial)
	if err != nil {
		return nil, err
	}
	return Start(ctx, opts, targets)
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
		} else {
			skipped = append(skipped, fmt.Sprintf("%s(%s)", device.Serial, device.State))
		}
	}
	if len(skipped) > 0 {
		fmt.Fprintf(os.Stderr, "dropcheck: skipped adb devices %s\n", strings.Join(skipped, ", "))
	}
	if len(targets) == 0 {
		return nil, errors.New("no connected adb devices; connect a device or pass --serial")
	}
	return targets, nil
}

// Start opens a local gRPC server, starts agents on targets, and waits for them
// to connect.
//
// targets must be the explicit adb devices selected by the app package. On
// startup failure, Start cleans up any gRPC server and adb reverse rules it
// already created.
func Start(ctx context.Context, opts Options, targets []adb.Device) (*Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
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
		var cleanupErr *startupCleanupError
		if attempt == attempts || errors.As(err, &cleanupErr) || !isADBReversePortCollision(err) {
			return nil, err
		}
		fmt.Fprintf(os.Stderr, "dropcheck: adb reverse port collision; retrying with a new local port attempt=%d/%d\n", attempt+1, attempts)
	}
	return nil, lastErr
}

func startOnce(ctx context.Context, opts Options, targets []adb.Device, token string) (_ *Session, err error) {
	listener, err := net.Listen("tcp", opts.ListenAddr)
	if err != nil {
		return nil, fmt.Errorf("listen grpc control: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listenAddr := listener.Addr().String()

	server := grpc.NewServer()
	controlServer := control.NewServer(token, sessionLogHandler(opts))
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
			if cleanupErr := session.Close(); cleanupErr != nil {
				err = &startupCleanupError{errors.Join(err, cleanupErr)}
			}
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

func sessionLogHandler(opts Options) func(control.LogEvent) {
	if opts.OnLog != nil {
		return opts.OnLog
	}
	return func(event control.LogEvent) {
		writeSessionLog(os.Stderr, event)
	}
}

func writeSessionLog(w io.Writer, event control.LogEvent) {
	if event.Level == controlpb.CommandLog_LEVEL_DEBUG || event.Level == controlpb.CommandLog_LEVEL_INFO {
		return
	}
	prefix := levelName(event.Level)
	if event.CommandID != "" {
		_, _ = fmt.Fprintf(w, "[%s agent=%s command=%s] %s\n", prefix, empty(event.AgentID, "unknown"), event.CommandID, event.Message)
		return
	}
	_, _ = fmt.Fprintf(w, "[%s agent=%s] %s\n", prefix, empty(event.AgentID, "unknown"), event.Message)
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
// Close is concurrent-safe and idempotent: every call returns the same aggregate
// cleanup error. Reverse removals start concurrently with a shared five-second
// deadline (plus up to one second to reap processes). It never changes Wi-Fi.
func (s *Session) Close() error {
	if s == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		if s.grpcServer != nil {
			s.grpcServer.Stop()
		}
		if s.serveDone != nil {
			select {
			case err := <-s.serveDone:
				if err != nil && !errors.Is(err, grpc.ErrServerStopped) {
					s.closeErr = errors.Join(s.closeErr, fmt.Errorf("serve grpc control: %w", err))
				}
			case <-time.After(time.Second):
				s.closeErr = errors.Join(s.closeErr, errors.New("grpc control shutdown timed out"))
			}
		}
		removeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		errs := make([]error, len(s.reversed))
		var wg sync.WaitGroup
		for i, client := range s.reversed {
			wg.Go(func() {
				if err := client.RemoveReverse(removeCtx, s.port); err != nil {
					errs[i] = fmt.Errorf("remove adb reverse serial=%s: %w", client.Serial, err)
				}
			})
		}
		wg.Wait()
		s.closeErr = errors.Join(append([]error{s.closeErr}, errs...)...)
	})
	return s.closeErr
}

type startupCleanupError struct{ error }

func (e *startupCleanupError) Unwrap() error { return e.error }

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
