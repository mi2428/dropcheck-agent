package app

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/chzyer/readline"
	"github.com/mi2428/dropcheck-agent/controller/internal/pipeline"
)

func TestShellCredentialsAreSessionOnlyAndClearable(t *testing.T) {
	state := &shellState{}
	for _, line := range []string{"set cli default-passphrase test-only", "clear cli default-passphrase"} {
		if _, err := runReplLine(context.Background(), state, line); err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(line, "set") && state.defaultPassphrase != "test-only" {
			t.Fatal("default not retained in session")
		}
	}
	if state.defaultPassphrase != "" || (&shellState{}).defaultPassphrase != "" {
		t.Fatal("session credential was not cleared")
	}
}

func TestFilteredCheckStillReportsFailure(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stderr
	os.Stderr = file
	defer func() { os.Stderr = previous; _ = file.Close() }()
	state := &shellState{}
	out, err := captureStdout(t, func() error {
		_, err := runReplLine(context.Background(), state, "test link ssid Lab family invalid | match absent")
		return err
	})
	if err != nil || out != "" {
		t.Fatal(out, err)
	}
	data, err := os.ReadFile(file.Name())
	if err != nil || !strings.Contains(string(data), "check link:") {
		t.Fatalf("filtered failure disappeared: %s %v", data, err)
	}
}

func TestShellJSONPreservesLiteralIdentityAndFailure(t *testing.T) {
	p, err := pipeline.Parse([]string{"display json"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := wrapPipePipeline(p).shellOutput("SSID: password=Guest\n", false)
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		OK    bool
		Lines []string
	}
	if err := json.Unmarshal([]byte(out), &body); err != nil || body.OK || len(body.Lines) != 1 || body.Lines[0] != "SSID: password=Guest" {
		t.Fatalf("invalid safe envelope: %s %v", out, err)
	}
}

func TestShellCompletionReplacesTheWholeKeyword(t *testing.T) {
	for _, input := range []string{"show wi^fi status", "show w^i status", "show wi^x status", "sho^w wifi status", "show wifi st^atus", "show wifi status | match '東京' | co^unt"} {
		prefix, suffix, _ := strings.Cut(input, "^")
		line, pos := []rune(prefix+suffix), len([]rune(prefix))
		values, _ := (shellReadlineCompleter{}).Do(line, pos)
		if len(values) == 1 {
			line = append(append(append([]rune(nil), line[:pos]...), values[0]...), line[pos:]...)
			pos += len(values[0])
		}
		if replaced, cursor, ok := handleShellCompletionHintKey(io.Discard, line, pos, readline.CharTab, nil); ok {
			line, pos = replaced, cursor
		}
		want := "show wifi status"
		if strings.Contains(input, "| match") {
			want += " | match '東京' | count"
		}
		if string(line) != want || pos > len(line) {
			t.Fatalf("%q: %q at %d, want %q", input, line, pos, want)
		}
	}
	for _, input := range []string{"show wi^\"fi\" status", "show wifi status | match 'private^suffix'"} {
		prefix, suffix, _ := strings.Cut(input, "^")
		line, pos := []rune(prefix+suffix), len([]rune(prefix))
		if values, _ := (shellReadlineCompleter{}).Do(line, pos); len(values) != 0 {
			t.Fatalf("literal completion: %q", values)
		}
		if _, _, ok := handleShellCompletionHintKey(io.Discard, line, pos, readline.CharTab, nil); ok {
			t.Fatal("literal was replaced")
		}
	}
}
