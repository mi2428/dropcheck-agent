package adbdiag

import (
	"context"
	"fmt"
	"strings"
	"time"

	"dropcheck/controller/internal/adb"
)

// InteractiveSpecs is the bounded Shell union, not the independent one-shot full kind.
func InteractiveSpecs(kind string) ([]CommandSpec, error) {
	if kind != "shell-full" && kind != "shell-wifi" && kind != "shell-connectivity" {
		return Specs(kind)
	}
	specs, err := Specs(KindFull)
	if err != nil {
		return nil, err
	}
	for _, extra := range []string{KindDumpsysConnectivityNetworks, KindDumpsysConnectivityRequests} {
		part, err := Specs(extra)
		if err != nil {
			return nil, err
		}
		specs = append(specs, part...)
	}
	var selected []CommandSpec
	for _, spec := range specs {
		connectivity := strings.HasPrefix(spec.Name, "dumpsys connectivity")
		if kind == "shell-full" || kind == "shell-connectivity" && connectivity || kind == "shell-wifi" && !connectivity {
			selected = append(selected, spec)
		}
	}
	return selected, nil
}

// CollectInteractive preserves each independent source outcome under one deadline.
// Zero means the documented default; user-supplied zero is rejected by the parser.
func CollectInteractive(ctx context.Context, client adb.Client, agent, kind string, budget time.Duration, progress func(int, int, string)) (Bundle, error) {
	if client.Serial == "" || strings.ContainsAny(client.Serial, " \t\r\n\x00") {
		return Bundle{}, fmt.Errorf("interactive ADB diagnostics require one pinned serial")
	}
	if budget == 0 {
		budget = 90 * time.Second
	}
	if budget <= 0 || budget > 15*time.Minute {
		return Bundle{}, fmt.Errorf("ADB collection budget must be in 1..900000 ms")
	}
	specs, err := InteractiveSpecs(kind)
	if err != nil {
		return Bundle{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	if client.Timeout <= 0 || client.Timeout > defaultCommandTimeout {
		client.Timeout = defaultCommandTimeout
	}
	bundle := Bundle{Agent: agent, Serial: client.Serial, Kind: kind, StartedAt: time.Now().Format(time.RFC3339Nano)}
	for i, spec := range specs {
		entry := CommandResult{Name: spec.Name, Args: spec.Args, Command: "adb " + strings.Join(spec.Args, " ")}
		if err := ctx.Err(); err != nil {
			entry.NotCollected, entry.ExitCode, entry.Error = true, -1, err.Error()
		} else {
			if progress != nil {
				progress(i+1, len(specs), spec.Name)
			}
			entry.StartedAt = time.Now().Format(time.RFC3339Nano)
			captured, runErr := client.Run(ctx, spec.Args...)
			entry.Args, entry.Command = captured.Args, "adb "+strings.Join(captured.Args, " ")
			entry.Stdout, entry.Stderr, entry.ExitCode = captured.Stdout, captured.Stderr, captured.ExitCode
			entry.TimedOut, entry.ElapsedMs = captured.TimedOut, captured.Elapsed.Milliseconds()
			if runErr != nil {
				entry.Error = runErr.Error()
			}
		}
		bundle.Commands = append(bundle.Commands, entry)
	}
	return bundle, nil
}
