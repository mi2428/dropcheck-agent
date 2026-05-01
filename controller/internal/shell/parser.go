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
	shellHelp
	shellShowDevices
	shellShowTarget
	shellSetTarget
	shellClearTarget
	shellAgentCommand
)

// Command is the parsed representation of one interactive shell command.
type Command struct {
	// Kind selects the shell action to perform.
	Kind CommandKind
	// Target is populated by "set target" commands.
	Target string
	// TargetAll is true for broadcast target selection.
	TargetAll bool
	// Operation is populated when Kind is AgentCommand.
	Operation command.Operation
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
	// Help represents the top-level help command.
	Help = shellHelp
	// ShowDevices represents "show devices".
	ShowDevices = shellShowDevices
	// ShowTarget represents "show target".
	ShowTarget = shellShowTarget
	// SetTarget represents "set target ...".
	SetTarget = shellSetTarget
	// ClearTarget represents "clear target".
	ClearTarget = shellClearTarget
	// AgentCommand represents a command that should be sent to Android agents.
	AgentCommand = shellAgentCommand
)

var shellTopKeywords = []string{"show", "set", "clear", "request", "monitor", "ping", "traceroute", "path-mtu", "global-ip", "test", "help", "exit", "quit"}

// ParseLine parses a complete interactive shell line, including pipelines.
//
// The returned Command keeps RawCommand separate from Pipeline so callers can
// render or execute the command and then apply output filters.
func ParseLine(line string) (Command, error) {
	return parseShellLine(line)
}

func parseShellLine(line string) (Command, error) {
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
	cmd, err := parseShellArgs(args)
	if err != nil {
		return Command{}, err
	}
	cmd.Pipeline = parsedPipeline
	cmd.RawCommand = parts[0]
	return cmd, nil
}

// ParseArgs parses already-tokenized shell command arguments.
//
// Pipeline syntax is not handled here; use ParseLine when parsing raw user
// input from the REPL.
func ParseArgs(args []string) (Command, error) {
	return parseShellArgs(args)
}

func parseShellArgs(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{Kind: shellNoop}, nil
	}
	top, err := resolveShellKeyword("command", args[0], shellTopKeywords)
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
	case "set":
		return parseShellSet(args[1:])
	case "clear":
		return parseShellClear(args[1:])
	case "request":
		return parseShellRequest(args[1:])
	case "monitor":
		return parseShellMonitor(args[1:])
	case "ping":
		return parseShellPing(args[1:])
	case "traceroute":
		return parseShellTraceroute(args[1:])
	case "path-mtu":
		return parseShellPathMtu(args[1:])
	case "global-ip":
		return parseShellGlobalIp(args[1:])
	case "test":
		return parseShellTest(args[1:])
	default:
		return Command{}, fmt.Errorf("unknown command %q", args[0])
	}
}

func parseShellShow(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{}, fmt.Errorf("usage: show <devices|target|wifi>")
	}
	name, err := resolveShellKeyword("show command", args[0], []string{"devices", "target", "wifi"})
	if err != nil {
		return Command{}, err
	}
	switch name {
	case "devices":
		if len(args) != 1 {
			return Command{}, fmt.Errorf("usage: show devices")
		}
		return Command{Kind: shellShowDevices}, nil
	case "target":
		if len(args) != 1 {
			return Command{}, fmt.Errorf("usage: show target")
		}
		return Command{Kind: shellShowTarget}, nil
	case "wifi":
		return parseShellShowWifi(args[1:])
	default:
		return Command{}, fmt.Errorf("unknown show command %q", args[0])
	}
}

func parseShellShowWifi(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{}, fmt.Errorf("usage: show wifi <status|diagnostics|scan|capabilities>")
	}
	name, err := resolveShellKeyword("show wifi command", args[0], []string{"status", "diagnostics", "scan", "capabilities"})
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
				values[key] = value
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
	if len(args) != 2 {
		return Command{}, fmt.Errorf("usage: set target <agent_id|adb_serial|number|all>")
	}
	name, err := resolveShellKeyword("set command", args[0], []string{"target"})
	if err != nil {
		return Command{}, err
	}
	if name != "target" {
		return Command{}, fmt.Errorf("unknown set command %q", args[0])
	}
	if args[1] == "all" {
		return Command{Kind: shellSetTarget, TargetAll: true}, nil
	}
	return Command{Kind: shellSetTarget, Target: args[1]}, nil
}

func parseShellClear(args []string) (Command, error) {
	if len(args) != 1 {
		return Command{}, fmt.Errorf("usage: clear target")
	}
	name, err := resolveShellKeyword("clear command", args[0], []string{"target"})
	if err != nil {
		return Command{}, err
	}
	if name != "target" {
		return Command{}, fmt.Errorf("unknown clear command %q", args[0])
	}
	return Command{Kind: shellClearTarget}, nil
}

func parseShellRequest(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{}, fmt.Errorf("usage: request wifi <command>")
	}
	name, err := resolveShellKeyword("request command", args[0], []string{"wifi"})
	if err != nil {
		return Command{}, err
	}
	if name != "wifi" {
		return Command{}, fmt.Errorf("unknown request command %q", args[0])
	}
	return parseShellRequestWifi(args[1:])
}

func parseShellRequestWifi(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{}, fmt.Errorf("usage: request wifi <connect|disconnect|forget|reconnect|wait|assert|cycle>")
	}
	name, err := resolveShellKeyword("request wifi command", args[0], []string{"connect", "disconnect", "forget", "reconnect", "wait", "assert", "cycle"})
	if err != nil {
		return Command{}, err
	}
	switch name {
	case "connect":
		return parseShellWifiConnect(args[1:], "connect")
	case "disconnect":
		if len(args) != 1 {
			return Command{}, fmt.Errorf("usage: request wifi disconnect")
		}
		return agentShellCommand(command.WifiDisconnectOperation()), nil
	case "forget":
		if len(args) != 2 {
			return Command{}, fmt.Errorf("usage: request wifi forget <ssid|network_id>")
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
		return Command{}, fmt.Errorf("unknown request wifi command %q", args[0])
	}
}

func parseShellWifiConnect(args []string, operation string) (Command, error) {
	if len(args) == 0 {
		return Command{}, fmt.Errorf("usage: request wifi %s passphrase <passphrase> [security <wpa2|wpa3|transition>] ... <ssid>", operation)
	}
	var ssid string
	values := map[string]string{}
	flags := map[string]bool{}
	allowed := []string{"passphrase", "security", "bssid", "band", "mac-randomization", "timeout"}
	if operation == "cycle" {
		allowed = append(allowed, "count", "ping", "http", "forget", "pause")
	}
	for i := 0; i < len(args); i++ {
		key, err := resolveShellKeyword("request wifi "+operation+" option", args[i], allowed)
		if err != nil {
			if ssid != "" {
				return Command{}, fmt.Errorf("unexpected request wifi %s argument %q", operation, args[i])
			}
			ssid = args[i]
			continue
		}
		if key == "forget" {
			flags[key] = true
			continue
		}
		value, next, err := shellValue(args, i, key)
		if err != nil {
			return Command{}, err
		}
		values[key] = value
		i = next
	}
	if ssid == "" {
		return Command{}, fmt.Errorf("request wifi %s requires <ssid>", operation)
	}
	passphrase := values["passphrase"]
	if passphrase == "" {
		return Command{}, fmt.Errorf("request wifi %s requires passphrase <passphrase>", operation)
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
		return Command{}, fmt.Errorf("usage: request wifi reconnect [timeout <ms>]")
	}
	key, err := resolveShellKeyword("request wifi reconnect option", args[0], []string{"timeout"})
	if err != nil {
		return Command{}, err
	}
	if key != "timeout" {
		return Command{}, fmt.Errorf("unknown request wifi reconnect option %q", args[0])
	}
	op, err := command.WifiReconnectOperation(args[1])
	return agentShellCommand(op), err
}

func parseShellWifiWait(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{}, fmt.Errorf("usage: request wifi wait connected [ssid]")
	}
	_, err := resolveShellKeyword("request wifi wait command", args[0], []string{"connected"})
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
			flags[key] = true
		default:
			value, next, err := shellValue(args, i, key)
			if err != nil {
				return Command{}, err
			}
			values[key] = value
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
		values[key] = value
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
		values[key] = value
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
			values[key] = value
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
		values[key] = value
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

func parseShellGlobalIp(args []string) (Command, error) {
	var family string
	values := map[string]string{}
	for i := 0; i < len(args); i++ {
		key, err := resolveShellKeyword("global-ip option", args[i], []string{"family", "timeout"})
		if err != nil {
			value, err := normalizeIpFamily(args[i])
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
			value, err = normalizeIpFamily(value)
			if err != nil {
				return Command{}, err
			}
		}
		if key == "family" && (family != "" || values["family"] != "") {
			return Command{}, fmt.Errorf("global-ip family specified twice")
		}
		values[key] = value
		i = next
	}
	if values["family"] != "" {
		family = values["family"]
	}
	op, err := command.GlobalIPOperation(family, values["timeout"])
	return agentShellCommand(op), err
}

func parseShellTest(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{}, fmt.Errorf("usage: test <dns|http|download>")
	}
	name, err := resolveShellKeyword("test command", args[0], []string{"dns", "http", "download"})
	if err != nil {
		return Command{}, err
	}
	switch name {
	case "dns":
		return parseShellTestDNS(args[1:])
	case "http":
		return parseShellTestHTTP(args[1:])
	case "download":
		return parseShellTestDownload(args[1:])
	default:
		return Command{}, fmt.Errorf("unknown test command %q", args[0])
	}
}

func parseShellTestDNS(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{}, fmt.Errorf("usage: test dns [type A|AAAA|ALL] [timeout <ms>] <name>")
	}
	var name string
	values := map[string]string{}
	for i := 0; i < len(args); i++ {
		key, err := resolveShellKeyword("test dns option", args[i], []string{"type", "timeout"})
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
			values[key] = value
			i = next
			continue
		}
		if name != "" && values["type"] == "" {
			if qtype, err := normalizeDNSQType(args[i]); err == nil {
				values["type"] = qtype
				continue
			}
		}
		if name != "" {
			return Command{}, fmt.Errorf("unexpected test dns argument %q", args[i])
		}
		name = args[i]
	}
	if name == "" {
		return Command{}, fmt.Errorf("usage: test dns [type A|AAAA|ALL] [timeout <ms>] <name>")
	}
	op, err := command.DNSOperation(name, values["type"], values["timeout"])
	return agentShellCommand(op), err
}

func parseShellTestHTTP(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{}, fmt.Errorf("usage: test http [expected-status <code>] [timeout <ms>] <url>")
	}
	var url string
	values := map[string]string{}
	for i := 0; i < len(args); i++ {
		key, err := resolveShellKeyword("test http option", args[i], []string{"expected-status", "timeout"})
		if err == nil {
			value, next, err := shellValue(args, i, key)
			if err != nil {
				return Command{}, err
			}
			values[key] = value
			i = next
			continue
		}
		if url != "" {
			return Command{}, fmt.Errorf("unexpected test http argument %q", args[i])
		}
		url = args[i]
	}
	if url == "" {
		return Command{}, fmt.Errorf("usage: test http [expected-status <code>] [timeout <ms>] <url>")
	}
	op, err := command.HTTPOperation(url, values["expected-status"], values["timeout"])
	return agentShellCommand(op), err
}

func parseShellTestDownload(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{}, fmt.Errorf("usage: test download [timeout <ms>] <url>")
	}
	var url string
	values := map[string]string{}
	for i := 0; i < len(args); i++ {
		key, err := resolveShellKeyword("test download option", args[i], []string{"timeout"})
		if err != nil {
			if url != "" {
				return Command{}, fmt.Errorf("unexpected test download argument %q", args[i])
			}
			url = args[i]
			continue
		}
		value, next, err := shellValue(args, i, key)
		if err != nil {
			return Command{}, err
		}
		values[key] = value
		i = next
	}
	if url == "" {
		return Command{}, fmt.Errorf("usage: test download [timeout <ms>] <url>")
	}
	op, err := command.DownloadOperation(url, values["timeout"])
	return agentShellCommand(op), err
}

func agentShellCommand(op command.Operation) Command {
	return Command{Kind: shellAgentCommand, Operation: op}
}

func shellValue(args []string, index int, name string) (string, int, error) {
	if index+1 >= len(args) {
		return "", index, fmt.Errorf("%s requires a value", name)
	}
	return args[index+1], index + 1, nil
}

func resolveShellKeyword(Kind string, value string, candidates []string) (string, error) {
	return resolveUniquePrefix(Kind, value, candidates)
}

func wifiBandValues() []string {
	return []string{"all", "2.4ghz", "5ghz", "6ghz", "60ghz"}
}
