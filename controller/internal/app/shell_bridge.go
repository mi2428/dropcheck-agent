package app

import (
	"io"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/shell"
)

type shellCommandKind = shell.CommandKind

const (
	shellNoop                   = shell.Noop
	shellExit                   = shell.Exit
	shellHelp                   = shell.Help
	shellShowDevices            = shell.ShowDevices
	shellAgentCommand           = shell.AgentCommand
	shellADBDiagnostics         = shell.ADBDiagnostics
	shellVersion                = shell.Version
	shellProfile                = shell.Profile
	shellProfiles               = shell.Profiles
	shellLastReport             = shell.LastReport
	shellSetDefaultPassphrase   = shell.SetDefaultPassphrase
	shellClearDefaultPassphrase = shell.ClearDefaultPassphrase
	shellConnect                = shell.Connect
	shellCLI                    = shell.CLI
	shellWifiTest               = shell.WifiTest
)

type shellCommand struct {
	kind                             shellCommandKind
	operation                        Operation
	adbKind                          string
	helpTopic                        string
	pipeline                         pipePipeline
	profileName, ssid, family, bssid string
	lab                              command.LabConfig
	detail                           bool
	rejection                        string
	passphrase                       string
	path, effect                     string
	connectOptions                   *command.WifiConnectOptions
	adbTimeoutMs                     uint32
}

func parseShellLine(line string) (shellCommand, error) {
	parsed, err := shell.ParseLine(line)
	return shellCommand{kind: parsed.Kind, operation: parsed.Operation, adbKind: parsed.ADBDiagnosticsKind, helpTopic: parsed.HelpTopic, pipeline: wrapPipePipeline(parsed.Pipeline), profileName: parsed.ProfileName, ssid: parsed.SSID, family: parsed.Family, bssid: parsed.BSSID, lab: parsed.Lab, detail: parsed.Detail, rejection: parsed.Rejection, passphrase: parsed.Passphrase, path: parsed.Path, effect: parsed.Effect, connectOptions: parsed.ConnectOptions, adbTimeoutMs: parsed.ADBTimeoutMs}, err
}

func isHelpLine(line string) bool                         { return shell.IsHelpLine(line) }
func isShellHelpRune(value rune) bool                     { return shell.IsHelpRune(value) }
func printShellContextHelp(line string, _ ...*shellState) { shell.PrintContextHelp(line) }
func writeShellContextHelp(w io.Writer, line string, _ ...*shellState) {
	shell.WriteContextHelp(w, line)
}
func completeShellLine(line string, _ *shellState) []string { return shell.CompleteLine(line) }
func shellCompletionHintLine(line string, _ *shellState) string {
	return shell.CompletionHintLine(line)
}
func isPlaceholderCandidate(candidate string) bool { return shell.IsPlaceholderCandidate(candidate) }
