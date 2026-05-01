package app

import (
	"io"

	"dropcheck/controller/internal/shell"
)

type shellCommandKind int

const (
	shellNoop shellCommandKind = iota
	shellExit
	shellHelp
	shellShowDevices
	shellShowTarget
	shellSetTarget
	shellClearTarget
	shellAgentCommand
)

type shellCommand struct {
	kind       shellCommandKind
	target     string
	targetAll  bool
	operation  Operation
	pipeline   pipePipeline
	rawCommand string
}

type helpEntry struct {
	token       string
	description string
}

func parseShellLine(line string) (shellCommand, error) {
	parsed, err := shell.ParseLine(line)
	return wrapShellCommand(parsed), err
}

func wrapShellCommand(parsed shell.Command) shellCommand {
	return shellCommand{
		kind:       shellCommandKind(parsed.Kind),
		target:     parsed.Target,
		targetAll:  parsed.TargetAll,
		operation:  parsed.Operation,
		pipeline:   wrapPipePipeline(parsed.Pipeline),
		rawCommand: parsed.RawCommand,
	}
}

func normalizeShellCommandArgs(args []string) ([]string, error) {
	return shell.NormalizeCommandArgs(args)
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

func printShellContextHelp(line string) {
	shell.PrintContextHelp(line)
}

func writeShellContextHelp(w io.Writer, line string) {
	shell.WriteContextHelp(w, line)
}

func shellHelpEntries(line string) []helpEntry {
	return wrapHelpEntries(shell.HelpEntries(line))
}

func wrapHelpEntries(entries []shell.HelpEntry) []helpEntry {
	wrapped := make([]helpEntry, 0, len(entries))
	for _, entry := range entries {
		wrapped = append(wrapped, helpEntry{token: entry.Token, description: entry.Description})
	}
	return wrapped
}

func completeShellLine(line string, _ *shellState) []string {
	return shell.CompleteLine(line)
}

func shellCompletionHintLine(line string, _ *shellState) string {
	return shell.CompletionHintLine(line)
}

func isPlaceholderCandidate(candidate string) bool {
	return shell.IsPlaceholderCandidate(candidate)
}
