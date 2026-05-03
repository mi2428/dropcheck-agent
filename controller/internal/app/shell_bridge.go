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
	shellEnterConfigureMode
	shellEnterRequestMode
	shellShowDevices
	shellShowConfig
	shellAgentCommand
	shellStandaloneSync
)

type shellCommand struct {
	kind        shellCommandKind
	configScope string
	operation   Operation
	syncOutput  string
	syncLimit   string
	syncMark    bool
	pipeline    pipePipeline
	rawCommand  string
}

func parseShellLine(line string) (shellCommand, error) {
	parsed, err := shell.ParseLine(line)
	return wrapShellCommand(parsed), err
}

func parseShellRequestLine(line string) (shellCommand, error) {
	parsed, err := shell.ParseRequestLine(line)
	return wrapShellCommand(parsed), err
}

func parseShellConfigureLine(line string) (shellCommand, error) {
	parsed, err := shell.ParseConfigureLine(line)
	return wrapShellCommand(parsed), err
}

func wrapShellCommand(parsed shell.Command) shellCommand {
	return shellCommand{
		kind:        shellCommandKind(parsed.Kind),
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
	if state != nil && state.mode == shellModeRequest {
		shell.WriteRequestContextHelp(os.Stdout, line)
		return
	}
	if state != nil && state.mode == shellModeConfigure {
		shell.WriteConfigureContextHelp(os.Stdout, line)
		return
	}
	shell.PrintContextHelp(line)
}

func writeShellContextHelp(w io.Writer, line string, states ...*shellState) {
	state := optionalShellState(states)
	if state != nil && state.mode == shellModeRequest {
		shell.WriteRequestContextHelp(w, line)
		return
	}
	if state != nil && state.mode == shellModeConfigure {
		shell.WriteConfigureContextHelp(w, line)
		return
	}
	shell.WriteContextHelp(w, line)
}

func optionalShellState(states []*shellState) *shellState {
	if len(states) == 0 {
		return nil
	}
	return states[0]
}

func completeShellLine(line string, state *shellState) []string {
	if state != nil && state.mode == shellModeRequest {
		return shell.CompleteRequestLine(line)
	}
	if state != nil && state.mode == shellModeConfigure {
		return shell.CompleteConfigureLine(line)
	}
	return shell.CompleteLine(line)
}

func shellCompletionHintLine(line string, state *shellState) string {
	if state != nil && state.mode == shellModeRequest {
		return shell.RequestCompletionHintLine(line)
	}
	if state != nil && state.mode == shellModeConfigure {
		return shell.ConfigureCompletionHintLine(line)
	}
	return shell.CompletionHintLine(line)
}

func isPlaceholderCandidate(candidate string) bool {
	return shell.IsPlaceholderCandidate(candidate)
}
