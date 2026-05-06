// Package adbdiag collects Android diagnostics through adb.
//
// Values named stdout or stderr in this package are raw ADB command streams.
// They are separate from protobuf raw fields emitted by the Android agent,
// which preserve framework object string snapshots such as WifiInfo.toString().
package adbdiag

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"dropcheck/controller/internal/adb"
	"dropcheck/controller/internal/pipeline"
)

const (
	KindCmdWifiStatus                  = "cmd-wifi-status"
	KindDumpsysWifi                    = "dumpsys-wifi"
	KindDumpsysConnectivity            = "dumpsys-connectivity"
	KindDumpsysConnectivityNetworks    = "dumpsys-connectivity-networks"
	KindDumpsysConnectivityRequests    = "dumpsys-connectivity-requests"
	KindDumpsysConnectivityDiagnostics = "dumpsys-connectivity-diagnostics"
	KindDumpsysConnectivityTraffic     = "dumpsys-connectivity-trafficcontroller"
	KindFull                           = "full"
	defaultCommandTimeout              = 90 * time.Second
)

// CommandSpec is one adb command in a diagnostics bundle.
type CommandSpec struct {
	Name string
	Args []string
}

// CommandResult is one adb command result in a diagnostics bundle.
type CommandResult struct {
	Name      string   `json:"name"`
	Args      []string `json:"args"`
	Command   string   `json:"command"`
	Stdout    string   `json:"stdout"`           // Stdout is the raw adb command stdout stream.
	Stderr    string   `json:"stderr,omitempty"` // Stderr is the raw adb command stderr stream.
	ExitCode  int      `json:"exit_code"`
	TimedOut  bool     `json:"timed_out,omitempty"`
	Error     string   `json:"error,omitempty"`
	ElapsedMs int64    `json:"elapsed_ms"`
}

// Bundle is an ADB diagnostics result for one Android agent/device.
//
// Bundle command streams are adb stdout/stderr captures; they are not the
// framework-object raw fields carried in agent protobuf responses.
type Bundle struct {
	Agent     string          `json:"agent,omitempty"`
	Serial    string          `json:"serial"`
	Kind      string          `json:"kind"`
	StartedAt string          `json:"started_at"`
	Commands  []CommandResult `json:"commands"`
}

// Specs returns the adb commands for a diagnostics kind.
func Specs(kind string) ([]CommandSpec, error) {
	switch kind {
	case KindCmdWifiStatus:
		return []CommandSpec{cmdWifiStatus()}, nil
	case KindDumpsysWifi:
		return []CommandSpec{dumpsysWifi()}, nil
	case KindDumpsysConnectivity:
		return []CommandSpec{dumpsysConnectivity()}, nil
	case KindDumpsysConnectivityNetworks:
		return []CommandSpec{{Name: "dumpsys connectivity networks", Args: []string{"shell", "dumpsys", "connectivity", "networks"}}}, nil
	case KindDumpsysConnectivityRequests:
		return []CommandSpec{{Name: "dumpsys connectivity requests", Args: []string{"shell", "dumpsys", "connectivity", "requests"}}}, nil
	case KindDumpsysConnectivityDiagnostics:
		return []CommandSpec{{Name: "dumpsys connectivity --diag", Args: []string{"shell", "dumpsys", "connectivity", "--diag"}}}, nil
	case KindDumpsysConnectivityTraffic:
		return []CommandSpec{{Name: "dumpsys connectivity trafficcontroller", Args: []string{"shell", "dumpsys", "connectivity", "trafficcontroller"}}}, nil
	case KindFull:
		return []CommandSpec{
			cmdWifiStatus(),
			dumpsysWifi(),
			{Name: "dumpsys wifi ipclient", Args: []string{"shell", "dumpsys", "wifi", "ipclient"}},
			{Name: "dumpsys wifi WifiScoreReport", Args: []string{"shell", "dumpsys", "wifi", "WifiScoreReport"}},
			{Name: "dumpsys wifi WifiScoreCard", Args: []string{"shell", "dumpsys", "wifi", "WifiScoreCard"}},
			dumpsysConnectivity(),
			{Name: "dumpsys connectivity --diag", Args: []string{"shell", "dumpsys", "connectivity", "--diag"}},
			{Name: "dumpsys connectivity trafficcontroller", Args: []string{"shell", "dumpsys", "connectivity", "trafficcontroller"}},
		}, nil
	default:
		return nil, fmt.Errorf("unknown adb diagnostics kind %q", kind)
	}
}

// Collect runs the diagnostics commands against one adb device.
func Collect(ctx context.Context, client adb.Client, agent string, kind string) (Bundle, error) {
	specs, err := Specs(kind)
	if err != nil {
		return Bundle{}, err
	}
	if client.Timeout <= 0 {
		client.Timeout = defaultCommandTimeout
	}
	bundle := Bundle{
		Agent:     agent,
		Serial:    client.Serial,
		Kind:      kind,
		StartedAt: time.Now().Format(time.RFC3339Nano),
		Commands:  make([]CommandResult, 0, len(specs)),
	}
	for _, spec := range specs {
		result, err := client.Run(ctx, spec.Args...)
		commandResult := CommandResult{
			Name:      spec.Name,
			Args:      result.Args,
			Command:   "adb " + strings.Join(result.Args, " "),
			Stdout:    result.Stdout,
			Stderr:    result.Stderr,
			ExitCode:  result.ExitCode,
			TimedOut:  result.TimedOut,
			ElapsedMs: result.Elapsed.Milliseconds(),
		}
		if err != nil {
			commandResult.Error = err.Error()
		}
		bundle.Commands = append(bundle.Commands, commandResult)
	}
	return bundle, nil
}

// Render renders one ADB diagnostics bundle as text or JSON.
func Render(bundle Bundle, format pipeline.Format) (string, error) {
	if format == pipeline.FormatJSON {
		data, err := json.MarshalIndent(bundle, "", "  ")
		if err != nil {
			return "", err
		}
		return string(data) + "\n", nil
	}
	var b strings.Builder
	rows := []kvRow{
		kv("serial", empty(bundle.Serial, "unknown")),
		kv("kind", bundle.Kind),
		kv("commands", len(bundle.Commands)),
	}
	if bundle.Agent != "" {
		rows = append(rows, kv("agent", bundle.Agent))
	}
	writeKVSection(&b, "ADB Diagnostics", rows...)
	for _, result := range bundle.Commands {
		writeKVSection(&b, result.Name,
			kv("command", result.Command),
			kv("exit", result.ExitCode),
			kv("elapsed", fmt.Sprintf("%dms", result.ElapsedMs)),
		)
		if result.TimedOut {
			writeKVSection(&b, "ADB Warning", kv("timed_out", "true"))
		}
		if result.Error != "" {
			writeKVSection(&b, "ADB Error", kv("message", result.Error))
		}
		if result.Stdout != "" {
			writeSection(&b, "stdout")
			b.WriteString(result.Stdout)
			if !strings.HasSuffix(result.Stdout, "\n") {
				b.WriteByte('\n')
			}
		}
		if result.Stderr != "" {
			writeSection(&b, "stderr")
			b.WriteString(result.Stderr)
			if !strings.HasSuffix(result.Stderr, "\n") {
				b.WriteByte('\n')
			}
		}
	}
	return b.String(), nil
}

func cmdWifiStatus() CommandSpec {
	return CommandSpec{Name: "cmd wifi status", Args: []string{"shell", "cmd", "wifi", "status"}}
}

func dumpsysWifi() CommandSpec {
	return CommandSpec{Name: "dumpsys wifi", Args: []string{"shell", "dumpsys", "wifi"}}
}

func dumpsysConnectivity() CommandSpec {
	return CommandSpec{Name: "dumpsys connectivity", Args: []string{"shell", "dumpsys", "connectivity"}}
}

func empty(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
