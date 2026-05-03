package app

import (
	"io"
	"os"

	"dropcheck/controller/internal/shell"
)

type shellCommandKind int

const (
	shellNoop shellCommandKind = iota
	shellExit
	shellExitMode
	shellHelp
	shellEnterRequestMode
	shellShowDevices
	shellShowTarget
	shellShowConfig
	shellSetTarget
	shellClearTarget
	shellAgentCommand
	shellStandaloneSync
)

type shellCommand struct {
	kind        shellCommandKind
	target      string
	targetAll   bool
	configScope string
	operation   Operation
	syncOutput  string
	syncLimit   string
	syncMark    bool
	pipeline    pipePipeline
	rawCommand  string
}

type helpEntry struct {
	token       string
	description string
}

func parseShellLine(line string) (shellCommand, error) {
	parsed, err := shell.ParseLine(line)
	return wrapShellCommand(parsed), err
}

func parseShellRequestLine(line string) (shellCommand, error) {
	parsed, err := shell.ParseRequestLine(line)
	return wrapShellCommand(parsed), err
}

func wrapShellCommand(parsed shell.Command) shellCommand {
	return shellCommand{
		kind:        shellCommandKind(parsed.Kind),
		target:      parsed.Target,
		targetAll:   parsed.TargetAll,
		configScope: parsed.ConfigScope,
		operation:   parsed.Operation,
		syncOutput:  parsed.StandaloneSyncOutput,
		syncLimit:   parsed.StandaloneSyncLimit,
		syncMark:    parsed.StandaloneSyncMark,
		pipeline:    wrapPipePipeline(parsed.Pipeline),
		rawCommand:  parsed.RawCommand,
	}
}

func isHelpLine(line string) bool {
	return shell.IsHelpLine(line)
}

func isShellHelpRune(value rune) bool {
	return shell.IsHelpRune(value)
}

func printShellHelp() {
	shell.PrintHelp()
}

func printShellContextHelp(line string, states ...*shellState) {
	state := optionalShellState(states)
	if state != nil && state.requestMode {
		shell.WriteRequestContextHelp(os.Stdout, line)
		return
	}
	shell.PrintContextHelp(line)
}

func writeShellContextHelp(w io.Writer, line string, states ...*shellState) {
	state := optionalShellState(states)
	if state != nil && state.requestMode {
		shell.WriteRequestContextHelp(w, line)
		return
	}
	shell.WriteContextHelp(w, line)
}

func shellHelpEntries(line string, states ...*shellState) []helpEntry {
	state := optionalShellState(states)
	if state != nil && state.requestMode {
		return wrapHelpEntries(shell.RequestHelpEntries(line))
	}
	return wrapHelpEntries(shell.HelpEntries(line))
}

func optionalShellState(states []*shellState) *shellState {
	if len(states) == 0 {
		return nil
	}
	return states[0]
}

func wrapHelpEntries(entries []shell.HelpEntry) []helpEntry {
	wrapped := make([]helpEntry, 0, len(entries))
	for _, entry := range entries {
		wrapped = append(wrapped, helpEntry{token: entry.Token, description: entry.Description})
	}
	return wrapped
}

func completeShellLine(line string, state *shellState) []string {
	if state != nil && state.requestMode {
		return shell.CompleteRequestLine(line)
	}
	return shell.CompleteLine(line)
}

func shellCompletionHintLine(line string, state *shellState) string {
	if state != nil && state.requestMode {
		return shell.RequestCompletionHintLine(line)
	}
	return shell.CompletionHintLine(line)
}

func isPlaceholderCandidate(candidate string) bool {
	return shell.IsPlaceholderCandidate(candidate)
}
