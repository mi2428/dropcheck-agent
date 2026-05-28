package shell

import (
	"fmt"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/pipeline"
)

// CommandKind identifies the action parsed from an interactive shell line.
type CommandKind int

const (
	shellNoop CommandKind = iota
	shellExit
	shellExitMode
	shellHelp
	shellEnterConfigureMode
	shellEnterRequestMode
	shellShowDevices
	shellShowConfig
	shellAgentCommand
	shellADBDiagnostics
	shellStandaloneSync
)

// Mode identifies the parser context for the interactive shell.
type Mode int

const (
	// ModeOperational is the default top-level shell mode.
	ModeOperational Mode = iota
	// ModeConfigure edits persistent Agent App configuration.
	ModeConfigure
	// ModeRequest runs one-shot requests against the selected Agent App.
	ModeRequest
)

// Command is the parsed representation of one interactive shell command.
type Command struct {
	// Kind selects the shell action to perform.
	Kind CommandKind
	// Operation is populated when Kind is AgentCommand.
	Operation command.Operation
	// ConfigScope is populated by "show config".
	ConfigScope string
	// StandaloneSyncOutput is populated by "sync standalone runs".
	StandaloneSyncOutput string
	// StandaloneSyncLimit caps "sync standalone runs" downloads.
	StandaloneSyncLimit string
	// StandaloneSyncMark marks downloaded runs as synced.
	StandaloneSyncMark bool
	// ADBDiagnosticsKind is populated by "show adb ...".
	ADBDiagnosticsKind string
	// Pipeline contains output filters parsed from "| ..." suffixes.
	Pipeline pipeline.Pipeline
	// RawCommand is the command segment before any pipeline stages.
	RawCommand string
}

// HelpEntry describes one contextual help or completion candidate.
type HelpEntry struct {
	// Token is the command token or placeholder shown to the user.
	Token string
	// Description explains the token in contextual help output.
	Description string
}

const (
	// Noop represents an empty shell line.
	Noop = shellNoop
	// Exit represents "exit" or "quit".
	Exit = shellExit
	// ExitMode represents leaving the current shell submode.
	ExitMode = shellExitMode
	// Help represents the top-level help command.
	Help = shellHelp
	// EnterConfigureMode represents entering the configure submode.
	EnterConfigureMode = shellEnterConfigureMode
	// EnterRequestMode represents entering the request submode.
	EnterRequestMode = shellEnterRequestMode
	// ShowDevices represents "show devices".
	ShowDevices = shellShowDevices
	// ShowConfig represents "show config ...".
	ShowConfig = shellShowConfig
	// AgentCommand represents a command that should be sent to Android agents.
	AgentCommand = shellAgentCommand
	// ADBDiagnostics represents a local adb diagnostics command.
	ADBDiagnostics = shellADBDiagnostics
	// StandaloneSync represents downloading stored standalone archives.
	StandaloneSync = shellStandaloneSync
)

var shellTopKeywords = []string{"show", "clear", "sync", "configure", "request", "help", "quit"}
var shellRequestCommandKeywords = []string{"wifi", "standalone", "monitor", "ping", "traceroute", "path-mtu", "global-ip", "dns", "http", "download"}
var shellOperationalKeywords = shellTopKeywords
var shellOperationalParseKeywords = appendShellKeywords(shellTopKeywords, []string{"exit"})
var shellConfigureKeywords = []string{"show", "set", "delete", "run", "help", "exit", "quit"}
var shellRequestKeywords = appendShellKeywords(shellRequestCommandKeywords, []string{"help", "exit", "quit"})

// ParseLine parses a complete interactive shell line, including pipelines.
//
// The returned Command keeps RawCommand separate from Pipeline so callers can
// render or execute the command and then apply output filters.
func ParseLine(line string) (Command, error) {
	return parseShellLine(line)
}

// ParseRequestLine parses a complete interactive shell line inside request
// mode, including pipelines.
func ParseRequestLine(line string) (Command, error) {
	return parseShellLineInMode(line, ModeRequest)
}

// ParseConfigureLine parses a complete interactive shell line inside configure
// mode, including pipelines.
func ParseConfigureLine(line string) (Command, error) {
	return parseShellLineInMode(line, ModeConfigure)
}

func parseShellLine(line string) (Command, error) {
	return parseShellLineInMode(line, ModeOperational)
}

func parseShellLineInMode(line string, mode Mode) (Command, error) {
	parts, err := pipeline.Split(line)
	if err != nil {
		return Command{}, err
	}
	parsedPipeline, err := pipeline.Parse(parts[1:])
	if err != nil {
		return Command{}, err
	}
	args, err := command.SplitArgs(parts[0])
	if err != nil {
		return Command{}, err
	}
	cmd, err := parseShellArgsInMode(args, mode)
	if err != nil {
		return Command{}, err
	}
	cmd.Pipeline = parsedPipeline
	cmd.RawCommand = parts[0]
	return cmd, nil
}

func parseShellArgs(args []string) (Command, error) {
	return parseShellArgsInMode(args, ModeOperational)
}

func parseShellArgsInMode(args []string, mode Mode) (Command, error) {
	switch mode {
	case ModeRequest:
		return parseShellRequestModeArgs(args)
	case ModeConfigure:
		return parseShellConfigureModeArgs(args)
	default:
		return parseShellOperationalArgs(args, false)
	}
}

func parseShellOperationalArgs(args []string, _ bool) (Command, error) {
	if len(args) == 0 {
		return Command{Kind: shellNoop}, nil
	}
	top, err := resolveShellKeyword("command", args[0], shellOperationalParseKeywords)
	if err != nil {
		return Command{}, err
	}
	switch top {
	case "exit", "quit":
		if len(args) != 1 {
			return Command{}, fmt.Errorf("usage: %s", top)
		}
		return Command{Kind: shellExit}, nil
	case "help":
		if len(args) != 1 {
			return Command{}, fmt.Errorf("usage: help")
		}
		return Command{Kind: shellHelp}, nil
	case "show":
		return parseShellShow(args[1:])
	case "clear":
		return parseShellClear(args[1:])
	case "sync":
		return parseShellSync(args[1:])
	case "configure":
		if len(args) != 1 {
			return Command{}, fmt.Errorf("usage: configure")
		}
		return Command{Kind: shellEnterConfigureMode}, nil
	case "request":
		if len(args) > 1 {
			return parseShellRequestModeArgs(args[1:])
		}
		return parseShellRequest(args[1:])
	default:
		return Command{}, fmt.Errorf("unknown command %q", args[0])
	}
}

func parseShellConfigureModeArgs(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{Kind: shellNoop}, nil
	}
	top, err := resolveShellKeyword("configure command", args[0], shellConfigureKeywords)
	if err != nil {
		return Command{}, err
	}
	switch top {
	case "exit":
		if len(args) != 1 {
			return Command{}, fmt.Errorf("usage: exit")
		}
		return Command{Kind: shellExitMode}, nil
	case "quit":
		if len(args) != 1 {
			return Command{}, fmt.Errorf("usage: quit")
		}
		return Command{Kind: shellExit}, nil
	case "help":
		if len(args) != 1 {
			return Command{}, fmt.Errorf("usage: help")
		}
		return Command{Kind: shellHelp}, nil
	case "show":
		return parseShellConfigureShow(args[1:])
	case "set":
		return parseShellSet(args[1:])
	case "delete":
		return parseShellDelete(args[1:])
	case "run":
		if len(args) == 1 {
			return Command{}, fmt.Errorf("usage: run <show|clear|sync|request> <command>")
		}
		if len(args) == 2 && args[1] == "request" {
			return Command{}, fmt.Errorf("usage: run request <request-command>")
		}
		return parseShellOperationalArgs(args[1:], true)
	default:
		return Command{}, fmt.Errorf("unknown configure command %q", args[0])
	}
}

func parseShellRequestModeArgs(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{Kind: shellNoop}, nil
	}
	top, err := resolveShellKeyword("request command", args[0], shellRequestKeywords)
	if err != nil {
		return Command{}, err
	}
	switch top {
	case "exit":
		if len(args) != 1 {
			return Command{}, fmt.Errorf("usage: exit")
		}
		return Command{Kind: shellExitMode}, nil
	case "quit":
		if len(args) != 1 {
			return Command{}, fmt.Errorf("usage: quit")
		}
		return Command{Kind: shellExit}, nil
	case "help":
		if len(args) != 1 {
			return Command{}, fmt.Errorf("usage: help")
		}
		return Command{Kind: shellHelp}, nil
	case "wifi":
		return parseShellRequestWifi(args[1:])
	case "standalone":
		return parseShellRequestStandalone(args[1:])
	case "monitor":
		return parseShellMonitor(args[1:])
	case "ping":
		return parseShellPing(args[1:])
	case "traceroute":
		return parseShellTraceroute(args[1:])
	case "path-mtu":
		return parseShellPathMtu(args[1:])
	case "global-ip":
		return parseShellGlobalIP(args[1:])
	case "dns":
		return parseShellDNS(args[1:])
	case "http":
		return parseShellHTTP(args[1:])
	case "download":
		return parseShellDownload(args[1:])
	default:
		return Command{}, fmt.Errorf("unknown request command %q", args[0])
	}
}

func parseShellShow(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{}, fmt.Errorf("usage: show <devices|config|wifi|ip|standalone|adb>")
	}
	name, err := resolveShellKeyword("show command", args[0], []string{"devices", "config", "wifi", "ip", "standalone", "adb"})
	if err != nil {
		return Command{}, err
	}
	switch name {
	case "devices":
		if len(args) != 1 {
			return Command{}, fmt.Errorf("usage: show devices")
		}
		return Command{Kind: shellShowDevices}, nil
	case "config":
		return parseShellShowConfig(args[1:])
	case "wifi":
		return parseShellShowWifi(args[1:])
	case "ip":
		return parseShellShowIP(args[1:])
	case "standalone":
		return parseShellShowStandalone(args[1:])
	case "adb":
		return parseShellShowADB(args[1:])
	default:
		return Command{}, fmt.Errorf("unknown show command %q", args[0])
	}
}

func parseShellShowADB(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{}, fmt.Errorf("usage: show adb <cmd|dumpsys|diagnostics|wifi|connectivity>")
	}
	name, err := resolveShellKeyword("show adb command", args[0], []string{"cmd", "dumpsys", "diagnostics", "wifi", "connectivity"})
	if err != nil {
		return Command{}, err
	}
	switch name {
	case "cmd":
		if len(args) == 3 && args[1] == "wifi" && args[2] == "status" {
			return adbDiagnosticsShellCommand("cmd-wifi-status"), nil
		}
		return Command{}, fmt.Errorf("usage: show adb cmd wifi status")
	case "dumpsys":
		return parseShellShowADBDumpsys(args[1:])
	case "diagnostics":
		if len(args) == 2 && args[1] == "full" {
			return adbDiagnosticsShellCommand("full"), nil
		}
		return Command{}, fmt.Errorf("usage: show adb diagnostics full")
	case "wifi":
		if len(args) == 2 && args[1] == "status" {
			return adbDiagnosticsShellCommand("cmd-wifi-status"), nil
		}
		if len(args) == 2 && args[1] == "dumpsys" {
			return adbDiagnosticsShellCommand("dumpsys-wifi"), nil
		}
		return Command{}, fmt.Errorf("usage: show adb wifi <status|dumpsys>")
	case "connectivity":
		return parseShellShowADBConnectivity(args[1:])
	default:
		return Command{}, fmt.Errorf("unknown show adb command %q", args[0])
	}
}

func parseShellShowADBDumpsys(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{}, fmt.Errorf("usage: show adb dumpsys <wifi|connectivity>")
	}
	service, err := resolveShellKeyword("show adb dumpsys service", args[0], []string{"wifi", "connectivity"})
	if err != nil {
		return Command{}, err
	}
	switch service {
	case "wifi":
		if len(args) != 1 {
			return Command{}, fmt.Errorf("usage: show adb dumpsys wifi")
		}
		return adbDiagnosticsShellCommand("dumpsys-wifi"), nil
	case "connectivity":
		if len(args) == 1 {
			return adbDiagnosticsShellCommand("dumpsys-connectivity"), nil
		}
		return parseShellShowADBConnectivity(args[1:])
	default:
		return Command{}, fmt.Errorf("unknown show adb dumpsys service %q", args[0])
	}
}

func parseShellShowADBConnectivity(args []string) (Command, error) {
	if len(args) == 0 {
		return adbDiagnosticsShellCommand("dumpsys-connectivity"), nil
	}
	if len(args) != 1 {
		return Command{}, fmt.Errorf("usage: show adb connectivity [dumpsys|networks|requests|diagnostics|trafficcontroller]")
	}
	if args[0] == "--diag" {
		return adbDiagnosticsShellCommand("dumpsys-connectivity-diagnostics"), nil
	}
	name, err := resolveShellKeyword("show adb connectivity command", args[0], []string{"dumpsys", "networks", "requests", "diagnostics", "trafficcontroller"})
	if err != nil {
		return Command{}, err
	}
	switch name {
	case "dumpsys":
		return adbDiagnosticsShellCommand("dumpsys-connectivity"), nil
	case "networks":
		return adbDiagnosticsShellCommand("dumpsys-connectivity-networks"), nil
	case "requests":
		return adbDiagnosticsShellCommand("dumpsys-connectivity-requests"), nil
	case "diagnostics":
		return adbDiagnosticsShellCommand("dumpsys-connectivity-diagnostics"), nil
	case "trafficcontroller":
		return adbDiagnosticsShellCommand("dumpsys-connectivity-trafficcontroller"), nil
	default:
		return Command{}, fmt.Errorf("unknown show adb connectivity command %q", args[0])
	}
}

func parseShellShowIP(args []string) (Command, error) {
	if len(args) != 1 {
		return Command{}, fmt.Errorf("usage: show ip status")
	}
	name, err := resolveShellKeyword("show ip command", args[0], []string{"status"})
	if err != nil {
		return Command{}, err
	}
	if name != "status" {
		return Command{}, fmt.Errorf("unknown show ip command %q", args[0])
	}
	return agentShellCommand(command.IPStatusOperation()), nil
}

func parseShellConfigureShow(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{Kind: shellShowConfig, ConfigScope: "all"}, nil
	}
	return parseShellShowConfig(args)
}

func parseShellShowConfig(args []string) (Command, error) {
	switch len(args) {
	case 0:
		return Command{Kind: shellShowConfig, ConfigScope: "all"}, nil
	case 1:
		name, err := resolveShellKeyword("show config command", args[0], []string{"standalone"})
		if err != nil {
			return Command{}, err
		}
		if name == "standalone" {
			return Command{Kind: shellShowConfig, ConfigScope: "standalone"}, nil
		}
		return Command{}, fmt.Errorf("usage: show config [standalone]")
	default:
		return Command{}, fmt.Errorf("usage: show config [standalone]")
	}
}

func parseShellShowStandalone(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{}, fmt.Errorf("usage: show standalone <status|runs|run>")
	}
	name, err := resolveShellKeyword("show standalone command", args[0], []string{"status", "runs", "run"})
	if err != nil {
		return Command{}, err
	}
	switch name {
	case "status":
		if len(args) != 1 {
			return Command{}, fmt.Errorf("usage: show standalone status")
		}
		return agentShellCommand(command.StandaloneStatusOperation()), nil
	case "runs":
		values := map[string]string{}
		flags := map[string]bool{}
		for i := 1; i < len(args); i++ {
			key, err := resolveShellKeyword("show standalone runs option", args[i], []string{"limit", "synced"})
			if err != nil {
				return Command{}, err
			}
			if key == "synced" {
				if err := setShellFlag(flags, key); err != nil {
					return Command{}, err
				}
				continue
			}
			value, next, err := shellValue(args, i, key)
			if err != nil {
				return Command{}, err
			}
			if err := setShellValue(values, key, value); err != nil {
				return Command{}, err
			}
			i = next
		}
		op, err := command.StandaloneListRunsOperation(command.StandaloneListOptions{Limit: values["limit"], IncludeSynced: flags["synced"]})
		return agentShellCommand(op), err
	case "run":
		if len(args) != 2 {
			return Command{}, fmt.Errorf("usage: show standalone run <run-id>")
		}
		op, err := command.StandaloneRunOperation(args[1], false)
		return agentShellCommand(op), err
	default:
		return Command{}, fmt.Errorf("unknown show standalone command %q", args[0])
	}
}

func parseShellShowWifi(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{}, fmt.Errorf("usage: show wifi <status|diagnostics|mlo|scan|capabilities>")
	}
	name, err := resolveShellKeyword("show wifi command", args[0], []string{"status", "diagnostics", "mlo", "scan", "capabilities"})
	if err != nil {
		return Command{}, err
	}
	switch name {
	case "status":
		if len(args) != 1 {
			return Command{}, fmt.Errorf("usage: show wifi status")
		}
		return agentShellCommand(command.WifiStatusOperation()), nil
	case "diagnostics":
		if len(args) != 1 {
			return Command{}, fmt.Errorf("usage: show wifi diagnostics")
		}
		return agentShellCommand(command.WifiDiagnosticsOperation()), nil
	case "mlo":
		return parseShellShowWifiMLO(args[1:])
	case "capabilities":
		if len(args) != 1 {
			return Command{}, fmt.Errorf("usage: show wifi capabilities")
		}
		return agentShellCommand(command.WifiCapabilitiesOperation()), nil
	case "scan":
		return parseShellShowWifiScan(args[1:])
	default:
		return Command{}, fmt.Errorf("unknown show wifi command %q", args[0])
	}
}

func parseShellShowWifiMLO(args []string) (Command, error) {
	if len(args) == 0 {
		return agentShellCommand(command.WifiMLOOperation()), nil
	}
	values := map[string]string{}
	fresh := false
	for i := 0; i < len(args); i++ {
		if key, err := resolveShellKeyword("show wifi mlo option", args[i], []string{"fresh", "timeout", "ssid", "bssid"}); err == nil {
			if key == "fresh" {
				if fresh {
					return Command{}, fmt.Errorf("fresh specified twice")
				}
				fresh = true
				continue
			}
			value, next, err := shellValue(args, i, key)
			if err != nil {
				return Command{}, err
			}
			if err := setShellValue(values, key, value); err != nil {
				return Command{}, err
			}
			i = next
			continue
		}
		if fresh && values["timeout"] == "" && len(args)-i == 1 {
			if err := setShellValue(values, "timeout", args[i]); err != nil {
				return Command{}, err
			}
			continue
		}
		return Command{}, fmt.Errorf("usage: show wifi mlo [fresh [timeout <ms>]] [ssid <ssid>|bssid <bssid>]")
	}
	if !fresh && values["timeout"] != "" {
		return Command{}, fmt.Errorf("timeout is supported only with wifi mlo fresh")
	}
	if values["ssid"] != "" && values["bssid"] != "" {
		return Command{}, fmt.Errorf("ssid and bssid filters cannot be used together")
	}
	op, err := command.WifiMLOOperationWithOptions(command.WifiMLOOptions{
		Fresh:   fresh,
		Timeout: values["timeout"],
		SSID:    values["ssid"],
		BSSID:   values["bssid"],
	})
	return agentShellCommand(op), err
}

func parseShellShowWifiScan(args []string) (Command, error) {
	if len(args) == 0 {
		op, err := command.WifiScanOperation("")
		return agentShellCommand(op), err
	}
	first, err := resolveShellKeyword("show wifi scan argument", args[0], append([]string{"fresh", "detail"}, wifiBandValues()...))
	if err != nil {
		return Command{}, err
	}
	switch first {
	case "fresh":
		var band string
		values := map[string]string{}
		for i := 1; i < len(args); i++ {
			if key, err := resolveShellKeyword("show wifi scan fresh option", args[i], []string{"timeout"}); err == nil {
				value, next, err := shellValue(args, i, key)
				if err != nil {
					return Command{}, err
				}
				if err := setShellValue(values, key, value); err != nil {
					return Command{}, err
				}
				i = next
				continue
			}
			value, err := resolveShellKeyword("wifi band", args[i], wifiBandValues())
			if err != nil {
				return Command{}, err
			}
			if band != "" {
				return Command{}, fmt.Errorf("wifi scan fresh band specified twice")
			}
			band = value
		}
		op, err := command.WifiFreshScanOperation(band, values["timeout"])
		return agentShellCommand(op), err
	case "detail":
		if len(args) < 2 || len(args) > 3 {
			return Command{}, fmt.Errorf("usage: show wifi scan detail [all|2.4ghz|5ghz|6ghz|60ghz] <ssid|bssid>")
		}
		target := args[1]
		var band string
		if len(args) == 3 {
			if value, err := resolveShellKeyword("wifi band", args[1], wifiBandValues()); err == nil {
				band = value
				target = args[2]
			} else {
				value, err := resolveShellKeyword("wifi band", args[2], wifiBandValues())
				if err != nil {
					return Command{}, err
				}
				band = value
			}
		}
		op, err := command.WifiScanDetailOperation(target, band)
		return agentShellCommand(op), err
	default:
		if len(args) != 1 {
			return Command{}, fmt.Errorf("usage: show wifi scan [all|2.4ghz|5ghz|6ghz|60ghz]")
		}
		op, err := command.WifiScanOperation(first)
		return agentShellCommand(op), err
	}
}

func parseShellSet(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{}, fmt.Errorf("usage: set standalone")
	}
	name, err := resolveShellKeyword("set command", args[0], []string{"standalone"})
	if err != nil {
		return Command{}, err
	}
	switch name {
	case "standalone":
		return parseShellSetStandalone(args[1:])
	default:
		return Command{}, fmt.Errorf("unknown set command %q", args[0])
	}
}

func parseShellSetStandalone(args []string) (Command, error) {
	edits, err := command.StandaloneSetEdits(args)
	if err != nil {
		return Command{}, err
	}
	op, err := command.StandaloneEditOperation(edits)
	return agentShellCommand(op), err
}

func parseShellDelete(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{}, fmt.Errorf("usage: delete standalone [festa <name>|...]")
	}
	name, err := resolveShellKeyword("delete command", args[0], []string{"standalone"})
	if err != nil {
		return Command{}, err
	}
	if name != "standalone" {
		return Command{}, fmt.Errorf("unknown delete command %q", args[0])
	}
	edits, err := command.StandaloneDeleteEdits(args[1:])
	if err != nil {
		return Command{}, err
	}
	op, err := command.StandaloneEditOperation(edits)
	return agentShellCommand(op), err
}

func parseShellClear(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{}, fmt.Errorf("usage: clear standalone runs [synced|all]")
	}
	name, err := resolveShellKeyword("clear command", args[0], []string{"standalone"})
	if err != nil {
		return Command{}, err
	}
	switch name {
	case "standalone":
		if len(args) < 2 || len(args) > 3 {
			return Command{}, fmt.Errorf("usage: clear standalone runs [synced|all]")
		}
		sub, err := resolveShellKeyword("clear standalone command", args[1], []string{"runs"})
		if err != nil {
			return Command{}, err
		}
		if sub != "runs" {
			return Command{}, fmt.Errorf("unknown clear standalone command %q", args[1])
		}
		mode := "synced"
		if len(args) == 3 {
			mode, err = resolveShellKeyword("clear standalone runs mode", args[2], []string{"synced", "all"})
			if err != nil {
				return Command{}, err
			}
		}
		op, err := command.StandaloneClearRunsOperation(mode)
		return agentShellCommand(op), err
	default:
		return Command{}, fmt.Errorf("unknown clear command %q", args[0])
	}
}

func parseShellRequest(args []string) (Command, error) {
	if len(args) != 0 {
		return Command{}, fmt.Errorf("usage: request")
	}
	return Command{Kind: shellEnterRequestMode}, nil
}

func appendShellKeywords(groups ...[]string) []string {
	var out []string
	for _, group := range groups {
		out = append(out, group...)
	}
	return out
}

func parseShellSync(args []string) (Command, error) {
	if len(args) < 2 {
		return Command{}, fmt.Errorf("usage: sync standalone runs [output <dir>] [limit <n>] [mark-synced|keep-unsynced]")
	}
	name, err := resolveShellKeyword("sync command", args[0], []string{"standalone"})
	if err != nil {
		return Command{}, err
	}
	sub, err := resolveShellKeyword("sync standalone command", args[1], []string{"runs"})
	if err != nil {
		return Command{}, err
	}
	if name != "standalone" || sub != "runs" {
		return Command{}, fmt.Errorf("usage: sync standalone runs [output <dir>] [limit <n>] [mark-synced|keep-unsynced]")
	}
	values := map[string]string{}
	flagsSeen := map[string]bool{}
	markSynced := true
	for i := 2; i < len(args); i++ {
		key, err := resolveShellKeyword("sync standalone runs option", args[i], []string{"output", "limit", "mark-synced", "keep-unsynced"})
		if err != nil {
			return Command{}, err
		}
		switch key {
		case "mark-synced":
			if !markSynced && flagsSeen["keep-unsynced"] {
				return Command{}, fmt.Errorf("mark-synced and keep-unsynced cannot be used together")
			}
			if flagsSeen["mark-synced"] {
				return Command{}, fmt.Errorf("mark-synced specified twice")
			}
			flagsSeen["mark-synced"] = true
			markSynced = true
		case "keep-unsynced":
			if flagsSeen["mark-synced"] {
				return Command{}, fmt.Errorf("mark-synced and keep-unsynced cannot be used together")
			}
			if flagsSeen["keep-unsynced"] {
				return Command{}, fmt.Errorf("keep-unsynced specified twice")
			}
			flagsSeen["keep-unsynced"] = true
			markSynced = false
		default:
			value, next, err := shellValue(args, i, key)
			if err != nil {
				return Command{}, err
			}
			if err := setShellValue(values, key, value); err != nil {
				return Command{}, err
			}
			i = next
		}
	}
	return Command{
		Kind:                 shellStandaloneSync,
		StandaloneSyncOutput: values["output"],
		StandaloneSyncLimit:  values["limit"],
		StandaloneSyncMark:   markSynced,
	}, nil
}

func parseShellRequestStandalone(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{}, fmt.Errorf("usage: standalone run once [festa <name>] [save]")
	}
	name, err := resolveShellKeyword("standalone command", args[0], []string{"run"})
	if err != nil {
		return Command{}, err
	}
	switch name {
	case "run":
		if len(args) < 2 {
			return Command{}, fmt.Errorf("usage: standalone run once [festa <name>] [save]")
		}
		sub, err := resolveShellKeyword("standalone run command", args[1], []string{"once"})
		if err != nil {
			return Command{}, err
		}
		if sub != "once" {
			return Command{}, fmt.Errorf("unknown standalone run command %q", args[1])
		}
		values := map[string]string{}
		save := false
		for i := 2; i < len(args); i++ {
			key, err := resolveShellKeyword("standalone run once option", args[i], []string{"festa", "save"})
			if err != nil {
				return Command{}, err
			}
			if key == "save" {
				if save {
					return Command{}, fmt.Errorf("save specified twice")
				}
				save = true
				continue
			}
			value, next, err := shellValue(args, i, key)
			if err != nil {
				return Command{}, err
			}
			if err := setShellValue(values, key, value); err != nil {
				return Command{}, err
			}
			i = next
		}
		op, err := command.StandaloneRunOnceOperation(command.StandaloneRunOptions{Festa: values["festa"], Save: save})
		return agentShellCommand(op), err
	default:
		return Command{}, fmt.Errorf("unknown standalone command %q", args[0])
	}
}

func parseShellRequestWifi(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{}, fmt.Errorf("usage: wifi <connect|disconnect|forget|reconnect|wait|assert|cycle>")
	}
	name, err := resolveShellKeyword("wifi command", args[0], []string{"connect", "disconnect", "forget", "reconnect", "wait", "assert", "cycle"})
	if err != nil {
		return Command{}, err
	}
	switch name {
	case "connect":
		return parseShellWifiConnect(args[1:], "connect")
	case "disconnect":
		if len(args) != 1 {
			return Command{}, fmt.Errorf("usage: wifi disconnect")
		}
		return agentShellCommand(command.WifiDisconnectOperation()), nil
	case "forget":
		if len(args) != 2 {
			return Command{}, fmt.Errorf("usage: wifi forget <ssid|network_id>")
		}
		return agentShellCommand(command.WifiForgetOperation(args[1])), nil
	case "reconnect":
		return parseShellWifiReconnect(args[1:])
	case "wait":
		return parseShellWifiWait(args[1:])
	case "assert":
		return parseShellWifiAssert(args[1:])
	case "cycle":
		return parseShellWifiConnect(args[1:], "cycle")
	default:
		return Command{}, fmt.Errorf("unknown wifi command %q", args[0])
	}
}

func parseShellWifiConnect(args []string, operation string) (Command, error) {
	if len(args) == 0 {
		return Command{}, fmt.Errorf("usage: wifi %s passphrase <passphrase> [security <auto|wpa2|wpa3|transition>] ... <ssid>", operation)
	}
	var ssid string
	values := map[string]string{}
	flags := map[string]bool{}
	allowed := []string{"passphrase", "security", "bssid", "band", "mac-randomization", "timeout"}
	if operation == "cycle" {
		allowed = append(allowed, "count", "ping", "http", "forget", "pause")
	}
	for i := 0; i < len(args); i++ {
		key, err := resolveShellKeyword("wifi "+operation+" option", args[i], allowed)
		if err != nil {
			if ssid != "" {
				return Command{}, fmt.Errorf("unexpected wifi %s argument %q", operation, args[i])
			}
			ssid = args[i]
			continue
		}
		if key == "forget" {
			if err := setShellFlag(flags, key); err != nil {
				return Command{}, err
			}
			continue
		}
		value, next, err := shellValue(args, i, key)
		if err != nil {
			return Command{}, err
		}
		if !shellWifiValueCanBeTrailing(key) && ssid == "" && next == len(args)-1 {
			return Command{}, fmt.Errorf("%s requires a value before <ssid>", key)
		}
		if err := setShellValue(values, key, value); err != nil {
			return Command{}, err
		}
		i = next
	}
	if ssid == "" {
		return Command{}, fmt.Errorf("wifi %s requires <ssid>", operation)
	}
	passphrase := values["passphrase"]
	if passphrase == "" {
		return Command{}, fmt.Errorf("wifi %s requires passphrase <passphrase>", operation)
	}
	opts := command.WifiConnectOptions{
		SSID:             ssid,
		Passphrase:       passphrase,
		Security:         values["security"],
		BSSID:            values["bssid"],
		Band:             values["band"],
		MacRandomization: values["mac-randomization"],
		Timeout:          values["timeout"],
	}
	if operation == "cycle" {
		op, err := command.WifiCycleOperation(command.WifiCycleOptions{
			WifiConnectOptions: opts,
			Count:              values["count"],
			PingHost:           values["ping"],
			HTTPURL:            values["http"],
			ForgetAfterEach:    flags["forget"],
			Pause:              values["pause"],
		})
		return agentShellCommand(op), err
	}
	op, err := command.WifiConnectOperation(opts)
	return agentShellCommand(op), err
}

func parseShellWifiReconnect(args []string) (Command, error) {
	if len(args) == 0 {
		op, err := command.WifiReconnectOperation("")
		return agentShellCommand(op), err
	}
	if len(args) != 2 {
		return Command{}, fmt.Errorf("usage: wifi reconnect [timeout <ms>]")
	}
	key, err := resolveShellKeyword("wifi reconnect option", args[0], []string{"timeout"})
	if err != nil {
		return Command{}, err
	}
	if key != "timeout" {
		return Command{}, fmt.Errorf("unknown wifi reconnect option %q", args[0])
	}
	op, err := command.WifiReconnectOperation(args[1])
	return agentShellCommand(op), err
}

func parseShellWifiWait(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{}, fmt.Errorf("usage: wifi wait connected [ssid]")
	}
	_, err := resolveShellKeyword("wifi wait command", args[0], []string{"connected"})
	if err != nil {
		return Command{}, err
	}
	return parseShellWifiExpectation(true, args[1:], true)
}

func parseShellWifiAssert(args []string) (Command, error) {
	return parseShellWifiExpectation(false, args, false)
}

func parseShellWifiExpectation(wait bool, args []string, allowPositionalSSID bool) (Command, error) {
	var positionalSSID string
	values := map[string]string{}
	flags := map[string]bool{}
	for i := 0; i < len(args); i++ {
		key, err := resolveShellKeyword("wifi expectation option", args[i], []string{"ssid", "bssid", "security", "band", "ip", "validated", "timeout"})
		if err != nil {
			if !allowPositionalSSID || positionalSSID != "" {
				return Command{}, err
			}
			positionalSSID = args[i]
			continue
		}
		switch key {
		case "ip", "validated":
			if err := setShellFlag(flags, key); err != nil {
				return Command{}, err
			}
		default:
			value, next, err := shellValue(args, i, key)
			if err != nil {
				return Command{}, err
			}
			if err := setShellValue(values, key, value); err != nil {
				return Command{}, err
			}
			i = next
		}
	}
	if positionalSSID != "" && values["ssid"] != "" {
		return Command{}, fmt.Errorf("wifi ssid specified twice")
	}
	if positionalSSID != "" {
		values["ssid"] = positionalSSID
	}
	opts := command.WifiExpectationOptions{
		SSID:             values["ssid"],
		BSSID:            values["bssid"],
		Security:         values["security"],
		Band:             values["band"],
		Timeout:          values["timeout"],
		RequireIP:        flags["ip"],
		RequireValidated: flags["validated"],
	}
	var op command.Operation
	var err error
	if wait {
		op, err = command.WifiWaitConnectedOperation(values["ssid"], opts)
	} else {
		op, err = command.WifiAssertOperation(opts)
	}
	return agentShellCommand(op), err
}

func parseShellMonitor(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{}, fmt.Errorf("usage: monitor wifi [duration <ms>] [interval <ms>]")
	}
	name, err := resolveShellKeyword("monitor command", args[0], []string{"wifi"})
	if err != nil {
		return Command{}, err
	}
	if name != "wifi" {
		return Command{}, fmt.Errorf("unknown monitor command %q", args[0])
	}
	values := map[string]string{}
	for i := 1; i < len(args); i++ {
		key, err := resolveShellKeyword("monitor wifi option", args[i], []string{"duration", "interval"})
		if err != nil {
			return Command{}, err
		}
		value, next, err := shellValue(args, i, key)
		if err != nil {
			return Command{}, err
		}
		if err := setShellValue(values, key, value); err != nil {
			return Command{}, err
		}
		i = next
	}
	duration := values["duration"]
	if values["interval"] != "" && duration == "" {
		duration = "10000"
	}
	op, err := command.WifiMonitorOperation(duration, values["interval"])
	return agentShellCommand(op), err
}

func parseShellPing(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{}, fmt.Errorf("usage: ping [count <n>] [size <bytes>] [timeout <ms>] <host>")
	}
	var host string
	values := map[string]string{}
	for i := 0; i < len(args); i++ {
		key, err := resolveShellKeyword("ping option", args[i], []string{"count", "size", "timeout"})
		if err != nil {
			if host != "" {
				return Command{}, fmt.Errorf("unexpected ping argument %q", args[i])
			}
			host = args[i]
			continue
		}
		value, next, err := shellValue(args, i, key)
		if err != nil {
			return Command{}, err
		}
		if err := setShellValue(values, key, value); err != nil {
			return Command{}, err
		}
		i = next
	}
	if host == "" {
		return Command{}, fmt.Errorf("usage: ping [count <n>] [size <bytes>] [timeout <ms>] <host>")
	}
	op, err := command.PingOperation(command.PingOptions{
		Host: host, Count: values["count"], Size: values["size"], Timeout: values["timeout"],
	})
	return agentShellCommand(op), err
}

func parseShellTraceroute(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{}, fmt.Errorf("usage: traceroute [max-hops <n>] [via <host_or_ip>] [size <bytes>] [timeout <ms>] <host>")
	}
	var host string
	var via []string
	values := map[string]string{}
	for i := 0; i < len(args); i++ {
		key, err := resolveShellKeyword("traceroute option", args[i], []string{"max-hops", "via", "size", "timeout"})
		if err != nil {
			if host != "" {
				return Command{}, fmt.Errorf("unexpected traceroute argument %q", args[i])
			}
			host = args[i]
			continue
		}
		value, next, err := shellValue(args, i, key)
		if err != nil {
			return Command{}, err
		}
		if key == "via" {
			via = append(via, value)
		} else {
			if err := setShellValue(values, key, value); err != nil {
				return Command{}, err
			}
		}
		i = next
	}
	if host == "" {
		return Command{}, fmt.Errorf("usage: traceroute [max-hops <n>] [via <host_or_ip>] [size <bytes>] [timeout <ms>] <host>")
	}
	op, err := command.TracerouteOperation(command.TracerouteOptions{
		Host: host, MaxHops: values["max-hops"], Via: via, Size: values["size"], Timeout: values["timeout"],
	})
	return agentShellCommand(op), err
}

func parseShellPathMtu(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{}, fmt.Errorf("usage: path-mtu [min-mtu <bytes>] [max-mtu <bytes>] [timeout <ms>] <host>")
	}
	var host string
	values := map[string]string{}
	for i := 0; i < len(args); i++ {
		key, err := resolveShellKeyword("path-mtu option", args[i], []string{"min-mtu", "max-mtu", "timeout"})
		if err != nil {
			if host != "" {
				return Command{}, fmt.Errorf("unexpected path-mtu argument %q", args[i])
			}
			host = args[i]
			continue
		}
		value, next, err := shellValue(args, i, key)
		if err != nil {
			return Command{}, err
		}
		if err := setShellValue(values, key, value); err != nil {
			return Command{}, err
		}
		i = next
	}
	if host == "" {
		return Command{}, fmt.Errorf("usage: path-mtu [min-mtu <bytes>] [max-mtu <bytes>] [timeout <ms>] <host>")
	}
	op, err := command.PathMTUOperation(command.PathMTUOptions{
		Host: host, MinMTU: values["min-mtu"], MaxMTU: values["max-mtu"], Timeout: values["timeout"],
	})
	return agentShellCommand(op), err
}

func parseShellGlobalIP(args []string) (Command, error) {
	var family string
	values := map[string]string{}
	for i := 0; i < len(args); i++ {
		key, err := resolveShellKeyword("global-ip option", args[i], []string{"family", "timeout"})
		if err != nil {
			value, err := normalizeIPFamily(args[i])
			if err != nil {
				return Command{}, err
			}
			if family != "" || values["family"] != "" {
				return Command{}, fmt.Errorf("global-ip family specified twice")
			}
			family = value
			continue
		}
		value, next, err := shellValue(args, i, key)
		if err != nil {
			return Command{}, err
		}
		if key == "family" {
			value, err = normalizeIPFamily(value)
			if err != nil {
				return Command{}, err
			}
		}
		if key == "family" && (family != "" || values["family"] != "") {
			return Command{}, fmt.Errorf("global-ip family specified twice")
		}
		if err := setShellValue(values, key, value); err != nil {
			return Command{}, err
		}
		i = next
	}
	if values["family"] != "" {
		family = values["family"]
	}
	op, err := command.GlobalIPOperation(family, values["timeout"])
	return agentShellCommand(op), err
}

func parseShellDNS(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{}, fmt.Errorf("usage: dns [type A|AAAA|ALL] [timeout <ms>] <name>")
	}
	var name string
	values := map[string]string{}
	for i := 0; i < len(args); i++ {
		key, err := resolveShellKeyword("dns option", args[i], []string{"type", "timeout"})
		if err == nil {
			value, next, err := shellValue(args, i, key)
			if err != nil {
				return Command{}, err
			}
			if key == "type" {
				value, err = normalizeDNSQType(value)
				if err != nil {
					return Command{}, err
				}
			}
			if err := setShellValue(values, key, value); err != nil {
				return Command{}, err
			}
			i = next
			continue
		}
		if name != "" && values["type"] == "" {
			if qtype, err := normalizeDNSQType(args[i]); err == nil {
				if err := setShellValue(values, "type", qtype); err != nil {
					return Command{}, err
				}
				continue
			}
		}
		if name != "" {
			return Command{}, fmt.Errorf("unexpected dns argument %q", args[i])
		}
		name = args[i]
	}
	if name == "" {
		return Command{}, fmt.Errorf("usage: dns [type A|AAAA|ALL] [timeout <ms>] <name>")
	}
	op, err := command.DNSOperation(name, values["type"], values["timeout"])
	return agentShellCommand(op), err
}

func parseShellHTTP(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{}, fmt.Errorf("usage: http [expected-status <code>] [timeout <ms>] <url>")
	}
	var url string
	values := map[string]string{}
	for i := 0; i < len(args); i++ {
		key, err := resolveShellKeyword("http option", args[i], []string{"expected-status", "timeout"})
		if err == nil {
			value, next, err := shellValue(args, i, key)
			if err != nil {
				return Command{}, err
			}
			if err := setShellValue(values, key, value); err != nil {
				return Command{}, err
			}
			i = next
			continue
		}
		if url != "" {
			return Command{}, fmt.Errorf("unexpected http argument %q", args[i])
		}
		url = args[i]
	}
	if url == "" {
		return Command{}, fmt.Errorf("usage: http [expected-status <code>] [timeout <ms>] <url>")
	}
	op, err := command.HTTPOperation(url, values["expected-status"], values["timeout"])
	return agentShellCommand(op), err
}

func parseShellDownload(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{}, fmt.Errorf("usage: download [timeout <ms>] <url>")
	}
	var url string
	values := map[string]string{}
	for i := 0; i < len(args); i++ {
		key, err := resolveShellKeyword("download option", args[i], []string{"timeout"})
		if err != nil {
			if url != "" {
				return Command{}, fmt.Errorf("unexpected download argument %q", args[i])
			}
			url = args[i]
			continue
		}
		value, next, err := shellValue(args, i, key)
		if err != nil {
			return Command{}, err
		}
		if err := setShellValue(values, key, value); err != nil {
			return Command{}, err
		}
		i = next
	}
	if url == "" {
		return Command{}, fmt.Errorf("usage: download [timeout <ms>] <url>")
	}
	op, err := command.DownloadOperation(url, values["timeout"])
	return agentShellCommand(op), err
}

func agentShellCommand(op command.Operation) Command {
	return Command{Kind: shellAgentCommand, Operation: op}
}

func adbDiagnosticsShellCommand(kind string) Command {
	return Command{Kind: shellADBDiagnostics, ADBDiagnosticsKind: kind}
}

func shellValue(args []string, index int, name string) (string, int, error) {
	if index+1 >= len(args) {
		return "", index, fmt.Errorf("%s requires a value", name)
	}
	return args[index+1], index + 1, nil
}

func setShellValue(values map[string]string, key, value string) error {
	if _, ok := values[key]; ok {
		return fmt.Errorf("%s specified twice", key)
	}
	values[key] = value
	return nil
}

func setShellFlag(flags map[string]bool, key string) error {
	if flags[key] {
		return fmt.Errorf("%s specified twice", key)
	}
	flags[key] = true
	return nil
}

func shellWifiValueCanBeTrailing(key string) bool {
	switch key {
	case "bssid", "band", "ping", "http":
		return false
	default:
		return true
	}
}

func resolveShellKeyword(Kind string, value string, candidates []string) (string, error) {
	return resolveUniquePrefix(Kind, value, candidates)
}

func wifiBandValues() []string {
	return []string{"all", "2.4ghz", "5ghz", "6ghz", "60ghz"}
}
