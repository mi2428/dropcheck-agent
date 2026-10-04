package app

import (
	"io"

	"dropcheck/controller/internal/shell"
)

type shellCommandKind = shell.CommandKind

const (
	shellNoop           = shell.Noop
	shellExit           = shell.Exit
	shellHelp           = shell.Help
	shellShowDevices    = shell.ShowDevices
	shellAgentCommand   = shell.AgentCommand
	shellADBDiagnostics = shell.ADBDiagnostics
	shellVersion        = shell.Version
	shellProfile        = shell.Profile
	shellProfiles       = shell.Profiles
	shellLastReport     = shell.LastReport
)

type shellCommand struct {
	kind                             shellCommandKind
	operation                        Operation
	adbKind                          string
	helpTopic                        string
	pipeline                         pipePipeline
	profileName, ssid, family, bssid string
	detail                           bool
	use                              bool
	rejection                        string
}

func parseShellLine(line string) (shellCommand, error) {
	parsed, err := shell.ParseLine(line)
	return shellCommand{kind: parsed.Kind, operation: parsed.Operation, adbKind: parsed.ADBDiagnosticsKind, helpTopic: parsed.HelpTopic, pipeline: wrapPipePipeline(parsed.Pipeline), profileName: parsed.ProfileName, ssid: parsed.SSID, family: parsed.Family, bssid: parsed.BSSID, detail: parsed.Detail, use: parsed.Use, rejection: parsed.Rejection}, err
}

func isHelpLine(line string) bool                         { return shell.IsHelpLine(line) }
func isShellHelpRune(value rune) bool                     { return shell.IsHelpRune(value) }
func printShellHelp()                                     { shell.PrintHelp() }
func printShellContextHelp(line string, _ ...*shellState) { shell.PrintContextHelp(line) }
func writeShellContextHelp(w io.Writer, line string, _ ...*shellState) {
	shell.WriteContextHelp(w, line)
}
func completeShellLine(line string, _ *shellState) []string { return shell.CompleteLine(line) }
func shellCompletionHintLine(line string, _ *shellState) string {
	return shell.CompletionHintLine(line)
}
func isPlaceholderCandidate(candidate string) bool { return shell.IsPlaceholderCandidate(candidate) }
