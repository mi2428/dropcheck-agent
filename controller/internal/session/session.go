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

const DefaultPackageName = "io.dropcheck.agent"

type Options struct {
	ADBPath     string
	Serial      string
	PackageName string
}

type Session struct {
	Server *control.Server
	Agents []control.AgentInfo

	grpcServer *grpc.Server
	serveDone  chan error
	reversed   []adb.Client
	port       int
	closeOnce  sync.Once
}

func Start(ctx context.Context, opts Options, targets []adb.Device) (*Session, error) {
	if opts.ADBPath == "" {
		opts.ADBPath = "adb"
	}
	if opts.PackageName == "" {
		opts.PackageName = DefaultPackageName
	}
	token, err := control.RandomHex(24)
	if err != nil {
		return nil, fmt.Errorf("create session token: %w", err)
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
	session := &Session{
		Server:     controlServer,
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

	fmt.Fprintf(os.Stderr, "dropcheck: grpc=127.0.0.1:%d adb_reverse=tcp:%d package=%s devices=%d\n", port, port, opts.PackageName, len(targets))
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
