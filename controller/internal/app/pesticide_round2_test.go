package app

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/mi2428/dropcheck-agent/controller/internal/adbdiag"
	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/control"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"github.com/mi2428/dropcheck-agent/controller/internal/pipeline"
)

func TestRound2UntrustedCredentialEncodings(t *testing.T) {
	const secret = "round2-synthetic-secret"
	for _, tc := range []struct{ name, value string }{
		{"json", `{"password":"` + secret + `"}`},
		{"ansi-key", "pass\x1b[31mword=" + secret},
		{"encoded-url-key", "https://example.test/?%70assword=" + secret},
		{"url-user-info", "https://user:" + secret + "@example.test/"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := adbdiag.Render(adbdiag.Bundle{Commands: []adbdiag.CommandResult{{Stdout: tc.value}}})
			if err != nil {
				t.Fatal(err)
			}
			p, _ := pipeline.Parse([]string{"display json"})
			out, err = shellOutput(p, out, true)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(out, secret) {
				t.Error("ADB safe JSON retains an encoded credential")
			}
		})
	}
}

func TestRound2InvalidPipelineHasNoSessionSideEffects(t *testing.T) {
	state := &shellState{}
	const command = "set cli default-passphrase new-synthetic-value"
	_, err := runReplLine(t.Context(), state, command)
	if err != nil || state.defaultPassphrase != "new-synthetic-value" {
		t.Fatal("positive control did not set the credential", err)
	}
	state.defaultPassphrase = "prior-synthetic-value"
	_, err = runReplLine(t.Context(), state, command+" | display json | count")
	if err != nil {
		t.Fatal(err)
	}
	if state.defaultPassphrase != "prior-synthetic-value" {
		t.Fatal("invalid pipeline changed the session credential")
	}
}

func TestRound2MultiTargetUseFailsBeforeStateChangeOrDispatch(t *testing.T) {
	state := &shellState{}
	op, err := command.WifiConnectOperation(command.WifiConnectOptions{SSID: "round2", Passphrase: "round2-synthetic-secret"})
	if err != nil {
		t.Fatal(err)
	}
	err = runOperationForAgents(t.Context(), state, []control.AgentInfo{{ID: "a"}, {ID: "b"}}, op, commandOutputOptions{use: true})
	if err == nil || state.failedUse {
		t.Fatal("unsafe broadcast use was not rejected before state change")
	}
}

func TestRound2WrongNetworkIsNotSuccessfulShellJSON(t *testing.T) {
	state, stream, cleanup := connectedShellStateWithStream(t)
	defer cleanup()
	agent, err := selectedAgent(state)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for frame := range stream.sent {
			if frame.GetRunCommand() == nil {
				continue
			}
			stream.recv <- &controlpb.AgentFrame{CommandId: frame.GetCommandId(), Body: &controlpb.AgentFrame_Result{Result: &controlpb.CommandResult{
				Status: controlpb.CommandResult_STATUS_OK, Payload: &controlpb.CommandResult_NetworkSnapshot{NetworkSnapshot: &controlpb.NetworkSnapshot{
					Networks: []*controlpb.NetworkDiagnostics{{NetworkId: "43", IpStatus: &controlpb.IpStatus{NetworkId: "43"}}},
				}},
			}}}
			return
		}
	}()
	op, err := command.NetworkOperation("42")
	if err != nil {
		t.Fatal(err)
	}
	p, _ := pipeline.Parse([]string{"display json"})
	out, err := captureStdout(t, func() error {
		var mu sync.Mutex
		printed := false
		return runCommandForAgent(t.Context(), state, agent, op, commandOutputOptions{pipeline: p}, &mu, &printed)
	})
	if err != nil {
		return
	}
	var envelope struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.OK {
		t.Error("request Network 42, response Network 43: Shell JSON still says ok=true")
	}
}

func TestPTYRound2NativeHistory(t *testing.T) {
	if os.Getenv("DROPCHECK_SHELL_PTY") != "1" {
		t.Skip("requires an interactive PTY driver")
	}
	if !useLineEditor() {
		t.Fatal("native history regression requires a real PTY")
	}
	state := &shellState{server: control.NewServer("", nil)}
	if err := replLineEditor(t.Context(), state); err != nil {
		t.Fatal(err)
	}
	if state.defaultPassphrase != "" {
		t.Fatal("Shell exit did not clear the session credential")
	}
}
