package app

import (
	"context"
	"encoding/json"
	"math/rand"
	"strings"
	"sync"
	"testing"

	"github.com/mi2428/dropcheck-agent/controller/internal/adbdiag"
	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/pipeline"
	"github.com/mi2428/dropcheck-agent/controller/internal/shell"
)

func TestAdversarialQARawADBSecretBoundary(t *testing.T) {
	raw, err := adbdiag.Render(adbdiag.Bundle{Commands: []adbdiag.CommandResult{{Stdout: "password=qa-synthetic-secret\n"}}})
	if err != nil {
		t.Fatal(err)
	}
	pipe, err := pipeline.Parse([]string{`match qa-synthetic-secret`})
	if err != nil {
		t.Fatal(err)
	}
	filtered, err := shellOutput(pipe, raw, true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(filtered, "qa-synthetic-secret") {
		t.Error("untrusted ADB credential reaches the filter/output boundary unredacted")
	}
	pipe, err = pipeline.Parse([]string{"display json"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := shellOutput(pipe, raw, true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "qa-synthetic-secret") {
		t.Error("safe presentation JSON contains the synthetic ADB credential")
	}
}

func TestAdversarialQAMidTokenCompletion(t *testing.T) {
	line := []rune("show wifi status")
	cursor := len([]rune("show wi"))
	values, _ := (shellReadlineCompleter{}).Do(line, cursor)
	if len(values) != 1 {
		t.Fatalf("expected one completion, got %d", len(values))
	}
	completed := string(line[:cursor]) + string(values[0]) + string(line[cursor:])
	if completed != "show wifi status" {
		t.Errorf("mid-token Tab corrupted input: %q", completed)
	}
}

func TestAdversarialQABroadcastJSONAgentIdentity(t *testing.T) {
	state, stream, cleanup := connectedShellStateWithStream(t)
	defer cleanup()
	agent, err := state.server.ResolveAgent("agent-a")
	if err != nil {
		t.Fatal(err)
	}
	respondToPingCommand(stream, 4)
	op, err := command.PingOperation(command.PingOptions{Host: "example.test", Count: "1"})
	if err != nil {
		t.Fatal(err)
	}
	pipe, err := pipeline.Parse([]string{"display json"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := captureStdout(t, func() error {
		var mu sync.Mutex
		printed := false
		return runCommandForAgent(context.Background(), state, agent, op, commandOutputOptions{includeAgentHeader: true, pipeline: pipe}, &mu, &printed)
	})
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Lines []string `json:"lines"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Lines) == 0 || !strings.HasPrefix(envelope.Lines[0], "Agent:") {
		t.Errorf("per-agent JSON identity was lost: first line=%q", envelope.Lines[0])
	}
	if strings.Contains(envelope.Lines[0], "Agent:") && !strings.Contains(envelope.Lines[0], agentDisplayName(agent)) {
		t.Error("agent label missing")
	}
}

func TestAdversarialQAFailedADBMustNotBeJSONSuccess(t *testing.T) {
	state, _, cleanup := connectedShellStateWithStream(t)
	defer cleanup()
	state.adbPath = fakeADB(t, "echo 'permission denied' >&2\nexit 7\n")
	agent, err := state.server.ResolveAgent("agent-a")
	if err != nil {
		t.Fatal(err)
	}
	pipe, err := pipeline.Parse([]string{"display json"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := captureStdout(t, func() error {
		var mu sync.Mutex
		printed := false
		return runADBDiagnosticsForAgent(context.Background(), state, agent, adbdiag.KindCmdWifiStatus,
			commandOutputOptions{pipeline: pipe}, &mu, &printed)
	})
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.OK {
		t.Error("all ADB acquisitions failed with exit 7, but presentation JSON says ok=true")
	}
}

func TestAdversarialQABareLabParity(t *testing.T) {
	parsed, err := shell.ParseLine("test lab")
	if err != nil {
		t.Fatal(err)
	}
	if parsed.ProfileName != "lab" {
		t.Errorf("bare lab changed profile to %q", parsed.ProfileName)
	}
}

func TestAdversarialQAMalformedCorpusDoesNotPanic(t *testing.T) {
	rng := rand.New(rand.NewSource(20261103))
	alphabet := []rune("show wifi network passphrase count ?|\\\"' \t\r\n[]()日本語")
	for range 2000 {
		runes := make([]rune, rng.Intn(150))
		for i := range runes {
			runes[i] = alphabet[rng.Intn(len(alphabet))]
		}
		input := string(runes)
		_, _ = shell.ParseLine(input)
		_ = shell.CompleteLine(input)
		var help strings.Builder
		shell.WriteContextHelp(&help, input+"?")
	}
}
