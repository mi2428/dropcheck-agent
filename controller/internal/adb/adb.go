package adb

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	Path    string
	Serial  string
	Timeout time.Duration
}

type Device struct {
	Serial  string
	State   string
	Details map[string]string
	Raw     string
}

func (c Client) Reverse(ctx context.Context, port int) error {
	spec := "tcp:" + strconv.Itoa(port)
	_, err := c.Output(ctx, "reverse", spec, spec)
	return err
}

func (c Client) RemoveReverse(ctx context.Context, port int) error {
	spec := "tcp:" + strconv.Itoa(port)
	_, err := c.Output(ctx, "reverse", "--remove", spec)
	return err
}

func (c Client) StartAgentSession(ctx context.Context, packageName string, port int, token string, agentID string, adbSerial string) (string, error) {
	args := []string{
		"shell", "am", "start-foreground-service",
		"-n", packageName + "/.AgentService",
		"-a", "io.dropcheck.agent.action.GRPC_SESSION",
		"--es", "grpc_host", "127.0.0.1",
		"--ei", "grpc_port", strconv.Itoa(port),
		"--es", "grpc_token", token,
	}
	if agentID != "" {
		args = append(args, "--es", "agent_id", agentID)
	}
	if adbSerial != "" {
		args = append(args, "--es", "adb_serial", adbSerial)
	}
	return c.Output(ctx, args...)
}

func (c Client) ListDevices(ctx context.Context) ([]Device, error) {
	out, err := Client{Path: c.Path, Timeout: c.Timeout}.Output(ctx, "devices", "-l")
	if err != nil {
		return nil, err
	}
	var devices []Device
	for raw := range strings.SplitSeq(out, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "List of devices") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		device := Device{
			Serial:  fields[0],
			State:   fields[1],
			Details: make(map[string]string),
			Raw:     line,
		}
		for _, field := range fields[2:] {
			key, value, ok := strings.Cut(field, ":")
			if ok {
				device.Details[key] = value
			}
		}
		devices = append(devices, device)
	}
	return devices, nil
}

func (c Client) Output(ctx context.Context, args ...string) (string, error) {
	if c.Path == "" {
		c.Path = "adb"
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	full := make([]string, 0, len(args)+2)
	if c.Serial != "" {
		full = append(full, "-s", c.Serial)
	}
	full = append(full, args...)

	cmd := exec.CommandContext(cmdCtx, c.Path, full...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := stdout.String() + stderr.String()
	if cmdCtx.Err() != nil {
		return out, cmdCtx.Err()
	}
	if err != nil {
		msg := strings.TrimSpace(out)
		if msg == "" {
			msg = err.Error()
		}
		return out, fmt.Errorf("adb %s: %s", strings.Join(full, " "), msg)
	}
	return out, nil
}
