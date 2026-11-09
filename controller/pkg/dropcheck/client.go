// Package dropcheck provides typed Android agent operations.
// Clients own their transports; operations and agent snapshots do
// not expose authentication material. Raw results are unsanitized acquisition
// data: protect them before logging or exporting.
package dropcheck

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/mi2428/dropcheck-agent/controller/internal/acquire"
	"github.com/mi2428/dropcheck-agent/controller/internal/adb"
	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/control"
	"github.com/mi2428/dropcheck-agent/controller/internal/runner"
	"github.com/mi2428/dropcheck-agent/controller/internal/session"
)

// DefaultPackageName is the Android application ID used by Open by default.
const DefaultPackageName = session.DefaultPackageName

// ErrClientNotOpen indicates a nil or zero Client, which cannot run operations.
var ErrClientNotOpen = errors.New("client has no open session")

// ErrClientClosed indicates that Close has begun; create a new Client to run again.
var ErrClientClosed = errors.New("client is closed")

// Options configures Open. Values are copied and never modified by the client.
type Options struct {
	// ADBPath is the executable path; empty uses "adb", resolved via PATH.
	ADBPath string
	// Serial selects one device without discovery; empty discovers ready devices.
	Serial string
	// PackageName contains .AgentService; empty uses DefaultPackageName.
	PackageName string
	// ListenAddr is the local gRPC address; empty uses "127.0.0.1:0".
	ListenAddr string
	// OnLog receives all levels, including DEBUG accepted-command notifications,
	// inline on stream goroutines. Calls may overlap across agents; return promptly
	// or result delivery on that stream is delayed. Do not call Close synchronously
	// here. Panics are not recovered. Nil filters DEBUG/INFO and writes other
	// severities (including unspecified/unknown) to stderr. Discovery and startup
	// diagnostics always go to stderr, independently of this callback.
	OnLog func(LogEvent)
}

// LogLevel is an agent log severity. Unknown values can be received from peers.
type LogLevel int32

const (
	// LogUnspecified is an unspecified severity.
	LogUnspecified LogLevel = 0
	// LogDebug includes command acceptance notifications.
	LogDebug LogLevel = 1
	// LogInfo is informational severity.
	LogInfo LogLevel = 2
	// LogWarn is warning severity.
	LogWarn LogLevel = 3
	// LogError is error severity.
	LogError LogLevel = 4
)

// LogEvent is a copied agent log or command acknowledgement. Its strings may
// contain sensitive acquired data and are not presentation-sanitized.
type LogEvent struct {
	// AgentID and SessionID identify the originating connection.
	AgentID, SessionID string
	// CommandID is empty for logs not associated with a command.
	CommandID string
	// Level is the reported severity.
	Level LogLevel
	// Message is the unmodified agent message.
	Message string
	// Time is the peer timestamp, or controller receipt time if absent.
	Time time.Time
}

// Device is an ADB inventory row, including devices that are not ready.
// The caller owns Details and may modify it.
type Device struct {
	// Serial is the ADB transport identifier.
	Serial string
	// State is the ADB state, for example "device", "offline", or "unauthorized".
	State string
	// Details holds key:value attributes from adb devices -l.
	Details map[string]string
	// Raw is the original trimmed listing row, not sanitized presentation text.
	Raw string
}

// DeviceInfo is credential-free Android build metadata.
type DeviceInfo struct {
	// Manufacturer, Model and Device are Android build identifiers.
	Manufacturer, Model, Device string
	// SDK is the Android API level.
	SDK int32
	// Release is the Android release label.
	Release string
}

// Agent is an owned, credential-free snapshot returned by Agents. Editing public
// fields does not change routing or capabilities. A snapshot is bound to its
// originating Client and connection; Run never silently repins a stale snapshot.
type Agent struct {
	// ID and SessionID identify the observed agent and connection.
	ID, SessionID string
	// ADBSerial is the peer-reported ADB serial.
	ADBSerial string
	// PackageName and AppVersion describe the running application.
	PackageName, AppVersion string
	// Device is the Android build metadata.
	Device DeviceInfo
	// Capabilities is a caller-owned copy of the advertised capabilities.
	Capabilities []string
	// Connected is the controller's acceptance time for this connection.
	Connected time.Time
	identity  *agentIdentity
}

type agentIdentity struct {
	client      *Client
	id, session string
	connected   time.Time
}

// Client owns an ADB-backed session. It must not be copied after first use.
// Agents, Run and Close are safe to call concurrently. Open's context controls
// startup only; after Open returns, Close controls the client's lifetime.
type Client struct {
	mu        sync.Mutex
	session   *session.Session
	lifetime  context.Context
	cancel    context.CancelFunc
	closed    bool
	closeOnce sync.Once
	closeErr  error
}

// Devices lists the full inventory without starting services or reverse rules.
// Empty adbPath uses "adb". In-flight cancel/deadline causes support errors.Is.
func Devices(ctx context.Context, adbPath string) ([]Device, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rows, err := (adb.Client{Path: adbPath}).ListDevices(ctx)
	if err != nil {
		return nil, err
	}
	devices := make([]Device, len(rows))
	for i, row := range rows {
		devices[i] = Device{Serial: row.Serial, State: row.State, Details: row.Details, Raw: row.Raw}
	}
	return devices, nil
}

// Open discovers devices and starts an authenticated session. Startup failures
// include partial cleanup failures. It does not change Wi-Fi configuration.
func Open(ctx context.Context, opts Options) (*Client, error) {
	internal := session.Options{ADBPath: opts.ADBPath, Serial: opts.Serial, PackageName: opts.PackageName, ListenAddr: opts.ListenAddr}
	if opts.OnLog != nil {
		internal.OnLog = func(e control.LogEvent) {
			opts.OnLog(LogEvent{AgentID: e.AgentID, SessionID: e.SessionID, CommandID: e.CommandID, Level: LogLevel(e.Level), Message: e.Message, Time: e.Time})
		}
	}
	s, err := session.Open(ctx, internal)
	if err != nil {
		return nil, err
	}
	lifetime, cancel := context.WithCancel(context.Background())
	return &Client{session: s, lifetime: lifetime, cancel: cancel}, nil
}

// Agents returns fresh snapshots in stable display order. Each call owns its
// slice and capability slices. Nil, zero and closed clients return nil.
// Call again explicitly to select a replacement connection.
func (c *Client) Agents() []Agent {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.session == nil || c.session.Server == nil {
		return nil
	}
	infos := c.session.Server.Agents()
	agents := make([]Agent, len(infos))
	for i, info := range infos {
		h := info.Hello
		d := h.GetDevice()
		agents[i] = Agent{ID: info.ID, SessionID: info.SessionID, ADBSerial: h.GetAdbSerial(), PackageName: h.GetPackageName(), AppVersion: h.GetAppVersion(), Connected: info.Connected,
			Device:       DeviceInfo{Manufacturer: d.GetManufacturer(), Model: d.GetModel(), Device: d.GetDevice(), SDK: d.GetSdk(), Release: d.GetRelease()},
			Capabilities: append([]string(nil), h.GetCapabilities()...), identity: &agentIdentity{client: c, id: info.ID, session: info.SessionID, connected: info.Connected}}
	}
	return agents
}

// Run validates and executes an opaque operation on the snapshot's original
// connection. Caller-edited metadata never authorizes an operation. Cancel ctx
// to cancel this call; Close cancels all active calls. Transport/input errors are
// Go errors; inspect Raw.Status AND Findings for agent/acquisition failures.
// Partial acquisitions can be returned together with an error. The caller owns
// the returned result and may modify it without affecting future calls.
func (c *Client) Run(ctx context.Context, agent Agent, op Operation) (Result, error) {
	if c == nil {
		return Result{}, ErrClientNotOpen
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return Result{}, ErrClientClosed
	}
	s, lifetime := c.session, c.lifetime
	c.mu.Unlock()
	if s == nil || s.Server == nil || lifetime == nil {
		return Result{}, ErrClientNotOpen
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if err := command.ValidateOperation(op.impl); err != nil {
		return Result{}, err
	}
	identity := agent.identity
	if identity == nil || identity.client != c {
		return Result{}, errors.New("agent snapshot does not belong to this client")
	}
	var current control.AgentInfo
	for _, info := range s.Server.Agents() {
		if info.ID == identity.id && info.SessionID == identity.session && info.Connected.Equal(identity.connected) {
			current = info
			break
		}
	}
	if current.ID == "" {
		return Result{}, errors.New("selected agent connection is stale or disconnected; select it again")
	}
	runCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(lifetime, cancel)
	defer func() { stop(); cancel() }()
	if lifetime.Err() != nil {
		return Result{}, ErrClientClosed
	}
	value, err := acquire.Execute(runCtx, runner.New(s.Server), current, op.impl)
	if err != nil && lifetime.Err() != nil {
		err = errors.Join(ErrClientClosed, err)
	}
	return publicResult(value), err
}

// Close cancels active Run calls, stops transport and removes reverse rules.
// It never disconnects Wi-Fi or forgets profiles. Cleanup is bounded (normally
// at most seven seconds), and every concurrent/repeated call returns the same
// aggregate error. A failed cleanup is not retried. Nil/zero Client Close is safe.
func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		s, cancel := c.session, c.cancel
		c.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		c.closeErr = s.Close()
	})
	return c.closeErr
}
