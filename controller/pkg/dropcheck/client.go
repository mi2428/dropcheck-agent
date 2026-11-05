// Package dropcheck exposes basic Android agent operations without a CLI or test DSL.
package dropcheck

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/mi2428/dropcheck-agent/controller/internal/adb"
	"github.com/mi2428/dropcheck-agent/controller/internal/control"
	"github.com/mi2428/dropcheck-agent/controller/internal/runner"
	"github.com/mi2428/dropcheck-agent/controller/internal/session"
	"github.com/mi2428/dropcheck-agent/controller/internal/watchrun"
)

const DefaultPackageName = session.DefaultPackageName

type Options = session.Options
type Agent = control.AgentInfo
type LogEvent = control.LogEvent
type Result = watchrun.OperationResult
type Device = adb.Device

// Devices lists the full ADB inventory without starting any agent session.
func Devices(ctx context.Context, adbPath string) ([]Device, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return (adb.Client{Path: adbPath}).ListDevices(ctx)
}

// Client owns one ADB-backed agent session. Close releases its transport resources.
type Client struct{ *session.Session }

// Open discovers ready devices unless Serial explicitly selects one device.
// It starts the same authenticated transport used by Shell and watch.
func Open(ctx context.Context, opts Options) (*Client, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	adbPath := opts.ADBPath
	if adbPath == "" {
		adbPath = "adb"
	}
	targets, err := discoverADBTargets(ctx, adb.Client{Path: adbPath}, opts.Serial)
	if err != nil {
		return nil, err
	}
	s, err := session.Start(ctx, opts, targets)
	if err != nil {
		return nil, err
	}
	return &Client{Session: s}, nil
}

// Run executes one typed operation, including compound acquisition when requested.
// Command outcomes remain in Result.Raw and Result.Findings, not rendered text.
// Cancel ctx to cancel the operation without closing the client.
func (c *Client) Run(ctx context.Context, agent Agent, op Operation) (Result, error) {
	if c == nil || c.Session == nil || c.Server == nil {
		return Result{}, fmt.Errorf("client has no control session")
	}
	return watchrun.ExecuteOperation(ctx, runner.New(c.Server), agent, op)
}

func discoverADBTargets(ctx context.Context, client adb.Client, serial string) ([]adb.Device, error) {
	if serial != "" {
		// An explicit serial is treated as an operator override. This avoids
		// rejecting a valid target just because "adb devices" is temporarily
		// slow, noisy, or unable to report the device before the service start.
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
		// Keep non-ready devices visible in stderr instead of silently ignoring
		// them; it is often the only clue that authorization or USB state is the
		// real startup problem.
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
