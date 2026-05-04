package mcpserver

import (
	"context"
	"time"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/controlpb"
)

// Backend is the execution boundary used by MCP tools.
//
// RealBackend owns a dropcheck controller session. Tests can provide a fake
// backend to exercise MCP protocol behavior without ADB or Android devices.
type Backend interface {
	Start(context.Context, SessionStartOptions) (SessionInfo, error)
	Stop(context.Context) error
	Agents(context.Context) ([]Agent, error)
	Run(context.Context, string, command.Operation) (Execution, error)
	Close() error
}

// SessionStartOptions configures controller-session startup.
type SessionStartOptions struct {
	ADBPath     string `json:"adb_path,omitempty" jsonschema:"adb executable path; defaults to adb"`
	Serial      string `json:"serial,omitempty" jsonschema:"adb serial to select; defaults to ADB_SERIAL or all connected devices"`
	PackageName string `json:"package_name,omitempty" jsonschema:"Android package containing .AgentService"`
	ListenAddr  string `json:"listen_addr,omitempty" jsonschema:"local controller gRPC listen address; defaults to 127.0.0.1:0"`
}

// Empty reports whether opts leaves every session option unchanged.
func (opts SessionStartOptions) Empty() bool {
	return opts.ADBPath == "" && opts.Serial == "" && opts.PackageName == "" && opts.ListenAddr == ""
}

// SessionInfo describes the active controller session.
type SessionInfo struct {
	Started    bool      `json:"started"`
	ListenAddr string    `json:"listen_addr,omitempty"`
	AgentCount int       `json:"agent_count"`
	Agents     []Agent   `json:"agents,omitempty"`
	StartedAt  time.Time `json:"started_at"`
}

// Agent is the MCP-facing view of one connected Android agent.
type Agent struct {
	Number       int       `json:"number,omitempty"`
	ID           string    `json:"id"`
	ADBSerial    string    `json:"adb_serial,omitempty"`
	SessionID    string    `json:"session_id,omitempty"`
	AppVersion   string    `json:"app_version,omitempty"`
	Manufacturer string    `json:"manufacturer,omitempty"`
	Model        string    `json:"model,omitempty"`
	Device       string    `json:"device,omitempty"`
	SDK          int32     `json:"sdk,omitempty"`
	Release      string    `json:"release,omitempty"`
	Connected    time.Time `json:"connected"`
}

// Execution is the result of one dropcheck operation against one agent.
type Execution struct {
	Agent        Agent
	CommandID    string
	Operation    string
	CommandLabel string
	Result       *controlpb.CommandResult
}
