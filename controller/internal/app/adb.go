package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"dropcheck/controller/internal/adb"
	"dropcheck/controller/internal/session"
)

func startControlSession(ctx context.Context, opts shellOptions) (*session.Session, error) {
	adbPath := opts.ADBPath
	if adbPath == "" {
		adbPath = "adb"
	}
	targets, err := discoverADBTargets(ctx, adb.Client{Path: adbPath}, opts.Serial)
	if err != nil {
		return nil, err
	}
	return session.Start(ctx, opts, targets)
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
