package app

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/render"
)

func connectShell(ctx context.Context, state *shellState, parsed shellCommand) error {
	agents, err := state.commandTargets()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return nil
	}
	if state.targetAll || len(agents) != 1 || parsed.connectOptions == nil {
		fmt.Fprintln(os.Stderr, "request wifi connect requires one pinned agent")
		return nil
	}
	opts := *parsed.connectOptions
	if opts.Passphrase == "" {
		opts.Passphrase = state.defaultPassphrase
	}
	if !command.ValidPassphrase(opts.Passphrase) {
		fmt.Fprintln(os.Stderr, "connect requires a valid explicit or session credential")
		return nil
	}
	op, err := command.WifiConnectOperation(opts)
	if err != nil {
		fmt.Fprintln(os.Stderr, "invalid connection arguments")
		return nil
	}
	return runOperationForAgents(ctx, state, agents, op, commandOutputOptions{pipeline: parsed.pipeline, use: true, singleTarget: true})
}

func renderShellSession(state *shellState) string {
	credential, target := "unset", state.selected
	if state.defaultPassphrase != "" {
		credential = "present"
	}
	if state.targetAll {
		target = "all"
	} else if target == "" {
		target = "unselected"
	}
	return render.Fields([][2]string{{"Owner", "Controller"}, {"Target", target}, {"Broadcast", fmt.Sprint(state.targetAll)}, {"Layout width", fmt.Sprint(terminalPresentation().Width)}, {"History", "all commands; session-only"}, {"Default passphrase", credential}}, render.Presentation{})
}

func prepareWifiTest(state *shellState, parsed shellCommand) bool {
	state.lastWifiTest = nil
	state.lastWifiTestAgent = ""
	state.lastTestAt = time.Now()
	state.lastProfile, state.lastSSID = "wifi", ""
	reason := parsed.rejection
	if reason == "" {
		reason = "test has not returned a valid result"
	}
	report := profileReport("wifi", reason, state.selected)
	state.lastReport = &report
	if parsed.rejection != "" {
		fmt.Fprintln(os.Stderr, "test wifi: "+parsed.rejection)
		return false
	}
	return true
}

func renderLastShellTest(state *shellState, view render.Presentation) (string, error) {
	if state.lastWifiTest != nil {
		target := state.lastWifiTestAgent
		if target == "" {
			target = "? (agent metadata unavailable)"
		}
		out, err := render.CommandResult(target, state.lastWifiTest, command.Options{Detail: true}, outputText, view)
		return fmt.Sprintf("Historical test wifi at %s (not current readiness)\n%s", state.lastTestAt.Format(time.RFC3339Nano), out), err
	}
	return renderProfileReport(state, true, view)
}
