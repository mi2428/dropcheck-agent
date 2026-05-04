package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"dropcheck/controller/internal/adb"
	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/runner"
	"dropcheck/controller/internal/session"
)

// RealBackend starts Android agents over ADB and executes operations through
// the controller gRPC session.
type RealBackend struct {
	mu        sync.Mutex
	opts      SessionStartOptions
	session   *session.Session
	startedAt time.Time
	closed    bool
}

// NewRealBackend creates a backend that lazily starts a dropcheck session on
// first use.
func NewRealBackend(opts SessionStartOptions) *RealBackend {
	return &RealBackend{opts: defaultSessionStartOptions(opts)}
}

// Start starts or restarts the controller session.
func (b *RealBackend) Start(ctx context.Context, opts SessionStartOptions) (SessionInfo, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return SessionInfo{}, errors.New("dropcheck MCP backend is closed")
	}
	effective := b.opts
	if !opts.Empty() {
		effective = mergeSessionStartOptions(defaultSessionStartOptions(b.opts), opts)
	}
	if b.session != nil {
		if opts.Empty() || sameSessionOptions(effective, b.opts) {
			return b.sessionInfoLocked(), nil
		}
		b.session.Close()
		b.session = nil
	}
	targets, err := discoverADBTargets(ctx, effective)
	if err != nil {
		return SessionInfo{}, err
	}
	controlSession, err := session.Start(ctx, session.Options{
		ADBPath:     effective.ADBPath,
		Serial:      effective.Serial,
		PackageName: effective.PackageName,
		ListenAddr:  effective.ListenAddr,
	}, targets)
	if err != nil {
		return SessionInfo{}, err
	}
	b.session = controlSession
	b.opts = effective
	b.startedAt = time.Now()
	return b.sessionInfoLocked(), nil
}

// Stop stops the active controller session, if any.
func (b *RealBackend) Stop(context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.session != nil {
		b.session.Close()
		b.session = nil
	}
	b.startedAt = time.Time{}
	return nil
}

// Agents returns connected Android agents, starting a session when needed.
func (b *RealBackend) Agents(ctx context.Context) ([]Agent, error) {
	if _, err := b.ensureStartedLocked(ctx); err != nil {
		return nil, err
	}
	defer b.mu.Unlock()
	return agentsFromInfos(b.session.Agents), nil
}

// Run executes op on target. Empty target auto-selects the only connected
// agent; numeric targets select the displayed agent number.
func (b *RealBackend) Run(ctx context.Context, target string, op command.Operation) (Execution, error) {
	if _, err := b.ensureStartedLocked(ctx); err != nil {
		return Execution{}, err
	}
	defer b.mu.Unlock()
	info, err := resolveAgentLocked(b.session, target)
	if err != nil {
		return Execution{}, err
	}
	result, err := runner.New(b.session.Server).Run(ctx, info, op)
	exec := Execution{
		Agent:        agentFromInfo(0, info),
		CommandID:    result.CommandID,
		Operation:    op.Name,
		CommandLabel: "",
		Result:       result.Result,
	}
	if result.Command != nil {
		exec.CommandLabel = result.Command.GetLabel()
	}
	if err != nil {
		return exec, err
	}
	return exec, nil
}

// Close releases backend resources.
func (b *RealBackend) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	if b.session != nil {
		b.session.Close()
		b.session = nil
	}
	return nil
}

func (b *RealBackend) ensureStartedLocked(ctx context.Context) (SessionInfo, error) {
	b.mu.Lock()
	if b.session != nil {
		return b.sessionInfoLocked(), nil
	}
	if b.closed {
		b.mu.Unlock()
		return SessionInfo{}, errors.New("dropcheck MCP backend is closed")
	}
	b.mu.Unlock()
	info, err := b.Start(ctx, SessionStartOptions{})
	if err != nil {
		return SessionInfo{}, err
	}
	b.mu.Lock()
	return info, nil
}

func (b *RealBackend) sessionInfoLocked() SessionInfo {
	if b.session == nil {
		return SessionInfo{}
	}
	agents := agentsFromInfos(b.session.Agents)
	return SessionInfo{
		Started:    true,
		ListenAddr: b.session.ListenAddr,
		AgentCount: len(agents),
		Agents:     agents,
		StartedAt:  b.startedAt,
	}
}

func defaultSessionStartOptions(opts SessionStartOptions) SessionStartOptions {
	if opts.ADBPath == "" {
		opts.ADBPath = "adb"
	}
	if opts.Serial == "" {
		opts.Serial = os.Getenv("ADB_SERIAL")
	}
	if opts.PackageName == "" {
		opts.PackageName = session.DefaultPackageName
	}
	if opts.ListenAddr == "" {
		opts.ListenAddr = "127.0.0.1:0"
	}
	return opts
}

func mergeSessionStartOptions(base, override SessionStartOptions) SessionStartOptions {
	if override.ADBPath != "" {
		base.ADBPath = override.ADBPath
	}
	if override.Serial != "" {
		base.Serial = override.Serial
	}
	if override.PackageName != "" {
		base.PackageName = override.PackageName
	}
	if override.ListenAddr != "" {
		base.ListenAddr = override.ListenAddr
	}
	return base
}

func sameSessionOptions(a, b SessionStartOptions) bool {
	return a.ADBPath == b.ADBPath &&
		a.Serial == b.Serial &&
		a.PackageName == b.PackageName &&
		a.ListenAddr == b.ListenAddr
}

func discoverADBTargets(ctx context.Context, opts SessionStartOptions) ([]adb.Device, error) {
	if opts.Serial != "" {
		return []adb.Device{{Serial: opts.Serial, State: "device"}}, nil
	}
	devices, err := adb.Client{Path: opts.ADBPath}.ListDevices(ctx)
	if err != nil {
		return nil, fmt.Errorf("list adb devices: %w", err)
	}
	var targets []adb.Device
	for _, device := range devices {
		if device.State == "device" {
			targets = append(targets, device)
		}
	}
	if len(targets) == 0 {
		return nil, errors.New("no connected adb devices; connect a device or pass serial")
	}
	return targets, nil
}

func resolveAgentLocked(controlSession *session.Session, target string) (control.AgentInfo, error) {
	if controlSession == nil || controlSession.Server == nil {
		return control.AgentInfo{}, errors.New("dropcheck session is not started")
	}
	target = strings.TrimSpace(target)
	if target == "" {
		agents := controlSession.Server.Agents()
		switch len(agents) {
		case 0:
			return control.AgentInfo{}, errors.New("no Android agents connected")
		case 1:
			return agents[0], nil
		default:
			return control.AgentInfo{}, errors.New("multiple Android agents connected; specify target")
		}
	}
	if index, err := strconv.Atoi(target); err == nil {
		agents := controlSession.Server.Agents()
		if index < 1 || index > len(agents) {
			return control.AgentInfo{}, fmt.Errorf("agent number %d is out of range", index)
		}
		return agents[index-1], nil
	}
	return controlSession.Server.ResolveAgent(target)
}

func agentsFromInfos(infos []control.AgentInfo) []Agent {
	agents := make([]Agent, 0, len(infos))
	for i, info := range infos {
		agents = append(agents, agentFromInfo(i+1, info))
	}
	return agents
}

func agentFromInfo(number int, info control.AgentInfo) Agent {
	hello := info.Hello
	device := hello.GetDevice()
	return Agent{
		Number:       number,
		ID:           info.ID,
		ADBSerial:    hello.GetAdbSerial(),
		SessionID:    info.SessionID,
		AppVersion:   hello.GetAppVersion(),
		Manufacturer: device.GetManufacturer(),
		Model:        device.GetModel(),
		Device:       device.GetDevice(),
		SDK:          device.GetSdk(),
		Release:      device.GetRelease(),
		Connected:    info.Connected,
	}
}
