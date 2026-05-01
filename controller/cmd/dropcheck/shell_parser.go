package main

import (
	"fmt"
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

var shellTopKeywords = []string{"show", "set", "clear", "request", "monitor", "ping", "traceroute", "path-mtu", "test", "help", "exit", "quit"}

func parseShellLine(line string) (shellCommand, error) {
	parts, err := splitPipeline(line)
	if err != nil {
		return shellCommand{}, err
	}
	pipeline, err := parsePipePipeline(parts[1:])
	if err != nil {
		return shellCommand{}, err
	}
	args, err := splitArgs(parts[0])
	if err != nil {
		return shellCommand{}, err
	}
	cmd, err := parseShellArgs(args)
	if err != nil {
		return shellCommand{}, err
	}
	cmd.pipeline = pipeline
	cmd.rawCommand = parts[0]
	return cmd, nil
}

func parseShellArgs(args []string) (shellCommand, error) {
	if len(args) == 0 {
		return shellCommand{kind: shellNoop}, nil
	}
	top, err := resolveShellKeyword("command", args[0], shellTopKeywords)
	if err != nil {
		return shellCommand{}, err
	}
	switch top {
	case "exit", "quit":
		if len(args) != 1 {
			return shellCommand{}, fmt.Errorf("usage: %s", top)
		}
		return shellCommand{kind: shellExit}, nil
	case "help":
		if len(args) != 1 {
			return shellCommand{}, fmt.Errorf("usage: help")
		}
		return shellCommand{kind: shellHelp}, nil
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
	case "test":
		return parseShellTest(args[1:])
	default:
		return shellCommand{}, fmt.Errorf("unknown command %q", args[0])
	}
}

func parseShellShow(args []string) (shellCommand, error) {
	if len(args) == 0 {
		return shellCommand{}, fmt.Errorf("usage: show <devices|target|wifi>")
	}
	name, err := resolveShellKeyword("show command", args[0], []string{"devices", "target", "wifi"})
	if err != nil {
		return shellCommand{}, err
	}
	switch name {
	case "devices":
		if len(args) != 1 {
			return shellCommand{}, fmt.Errorf("usage: show devices")
		}
		return shellCommand{kind: shellShowDevices}, nil
	case "target":
		if len(args) != 1 {
			return shellCommand{}, fmt.Errorf("usage: show target")
		}
		return shellCommand{kind: shellShowTarget}, nil
	case "wifi":
		return parseShellShowWifi(args[1:])
	default:
		return shellCommand{}, fmt.Errorf("unknown show command %q", args[0])
	}
}

func parseShellShowWifi(args []string) (shellCommand, error) {
	if len(args) == 0 {
		return shellCommand{}, fmt.Errorf("usage: show wifi <status|diagnostics|scan|capabilities>")
	}
	name, err := resolveShellKeyword("show wifi command", args[0], []string{"status", "diagnostics", "scan", "capabilities"})
	if err != nil {
		return shellCommand{}, err
	}
	switch name {
	case "status":
		if len(args) != 1 {
			return shellCommand{}, fmt.Errorf("usage: show wifi status")
		}
		return agentShellCommand("wifi", "status"), nil
	case "diagnostics":
		if len(args) != 1 {
			return shellCommand{}, fmt.Errorf("usage: show wifi diagnostics")
		}
		return agentShellCommand("wifi", "diagnostics"), nil
	case "capabilities":
		if len(args) != 1 {
			return shellCommand{}, fmt.Errorf("usage: show wifi capabilities")
		}
		return agentShellCommand("wifi", "capabilities"), nil
	case "scan":
		return parseShellShowWifiScan(args[1:])
	default:
		return shellCommand{}, fmt.Errorf("unknown show wifi command %q", args[0])
	}
}

func parseShellShowWifiScan(args []string) (shellCommand, error) {
	legacy := []string{"wifi", "scan"}
	if len(args) == 0 {
		return agentShellCommand(legacy...), nil
	}
	first, err := resolveShellKeyword("show wifi scan argument", args[0], append([]string{"fresh", "detail"}, wifiBandValues()...))
	if err != nil {
		return shellCommand{}, err
	}
	switch first {
	case "fresh":
		legacy = append(legacy, "fresh")
		rest := args[1:]
		if len(rest) > 0 && !isShellKeyword(rest[0], []string{"timeout"}) {
			band, err := resolveShellKeyword("wifi band", rest[0], wifiBandValues())
			if err != nil {
				return shellCommand{}, err
			}
			legacy = append(legacy, band)
			rest = rest[1:]
		}
		for i := 0; i < len(rest); i++ {
			key, err := resolveShellKeyword("show wifi scan fresh option", rest[i], []string{"timeout"})
			if err != nil {
				return shellCommand{}, err
			}
			value, next, err := shellValue(rest, i, key)
			if err != nil {
				return shellCommand{}, err
			}
			legacy = append(legacy, "--timeout", value)
			i = next
		}
		return agentShellCommand(legacy...), nil
	case "detail":
		if len(args) < 2 || len(args) > 3 {
			return shellCommand{}, fmt.Errorf("usage: show wifi scan detail <ssid|bssid> [all|2.4ghz|5ghz|6ghz|60ghz]")
		}
		legacy = append(legacy, "detail", args[1])
		if len(args) == 3 {
			band, err := resolveShellKeyword("wifi band", args[2], wifiBandValues())
			if err != nil {
				return shellCommand{}, err
			}
			legacy = append(legacy, band)
		}
		return agentShellCommand(legacy...), nil
	default:
		if len(args) != 1 {
			return shellCommand{}, fmt.Errorf("usage: show wifi scan [all|2.4ghz|5ghz|6ghz|60ghz]")
		}
		return agentShellCommand("wifi", "scan", first), nil
	}
}

func parseShellSet(args []string) (shellCommand, error) {
	if len(args) != 2 {
		return shellCommand{}, fmt.Errorf("usage: set target <agent_id|adb_serial|number|all>")
	}
	name, err := resolveShellKeyword("set command", args[0], []string{"target"})
	if err != nil {
		return shellCommand{}, err
	}
	if name != "target" {
		return shellCommand{}, fmt.Errorf("unknown set command %q", args[0])
	}
	if args[1] == "all" {
		return shellCommand{kind: shellSetTarget, targetAll: true}, nil
	}
	return shellCommand{kind: shellSetTarget, target: args[1]}, nil
}

func parseShellClear(args []string) (shellCommand, error) {
	if len(args) != 1 {
		return shellCommand{}, fmt.Errorf("usage: clear target")
	}
	name, err := resolveShellKeyword("clear command", args[0], []string{"target"})
	if err != nil {
		return shellCommand{}, err
	}
	if name != "target" {
		return shellCommand{}, fmt.Errorf("unknown clear command %q", args[0])
	}
	return shellCommand{kind: shellClearTarget}, nil
}

func parseShellRequest(args []string) (shellCommand, error) {
	if len(args) == 0 {
		return shellCommand{}, fmt.Errorf("usage: request wifi <command>")
	}
	name, err := resolveShellKeyword("request command", args[0], []string{"wifi"})
	if err != nil {
		return shellCommand{}, err
	}
	if name != "wifi" {
		return shellCommand{}, fmt.Errorf("unknown request command %q", args[0])
	}
	return parseShellRequestWifi(args[1:])
}

func parseShellRequestWifi(args []string) (shellCommand, error) {
	if len(args) == 0 {
		return shellCommand{}, fmt.Errorf("usage: request wifi <connect|disconnect|forget|reconnect|wait|assert|cycle>")
	}
	name, err := resolveShellKeyword("request wifi command", args[0], []string{"connect", "disconnect", "forget", "reconnect", "wait", "assert", "cycle"})
	if err != nil {
		return shellCommand{}, err
	}
	switch name {
	case "connect":
		return parseShellWifiConnect(args[1:], "connect")
	case "disconnect":
		if len(args) != 1 {
			return shellCommand{}, fmt.Errorf("usage: request wifi disconnect")
		}
		return agentShellCommand("wifi", "disconnect"), nil
	case "forget":
		if len(args) != 2 {
			return shellCommand{}, fmt.Errorf("usage: request wifi forget <ssid|network_id>")
		}
		return agentShellCommand("wifi", "forget", args[1]), nil
	case "reconnect":
		return parseShellWifiReconnect(args[1:])
	case "wait":
		return parseShellWifiWait(args[1:])
	case "assert":
		return parseShellWifiAssert(args[1:])
	case "cycle":
		return parseShellWifiConnect(args[1:], "cycle")
	default:
		return shellCommand{}, fmt.Errorf("unknown request wifi command %q", args[0])
	}
}

func parseShellWifiConnect(args []string, command string) (shellCommand, error) {
	if len(args) == 0 {
		return shellCommand{}, fmt.Errorf("usage: request wifi %s <ssid> passphrase <passphrase> [security <wpa2|wpa3|transition>] ...", command)
	}
	ssid := args[0]
	rest := args[1:]
	values := map[string]string{}
	flags := map[string]bool{}
	allowed := []string{"passphrase", "security", "bssid", "band", "mac-randomization", "timeout"}
	if command == "cycle" {
		allowed = append(allowed, "count", "ping", "http", "forget", "pause")
	}
	for i := 0; i < len(rest); i++ {
		key, err := resolveShellKeyword("request wifi "+command+" option", rest[i], allowed)
		if err != nil {
			return shellCommand{}, err
		}
		if key == "forget" {
			flags[key] = true
			continue
		}
		value, next, err := shellValue(rest, i, key)
		if err != nil {
			return shellCommand{}, err
		}
		values[key] = value
		i = next
	}
	passphrase := values["passphrase"]
	if passphrase == "" {
		return shellCommand{}, fmt.Errorf("request wifi %s requires passphrase <passphrase>", command)
	}
	legacy := []string{"wifi", command, ssid, passphrase}
	if security := values["security"]; security != "" {
		legacy = append(legacy, security)
	}
	for _, key := range []string{"count", "bssid", "band", "mac-randomization", "ping", "http", "pause", "timeout"} {
		if value := values[key]; value != "" {
			legacy = append(legacy, "--"+key, value)
		}
	}
	if flags["forget"] {
		legacy = append(legacy, "--forget")
	}
	return agentShellCommand(legacy...), nil
}

func parseShellWifiReconnect(args []string) (shellCommand, error) {
	legacy := []string{"wifi", "reconnect"}
	if len(args) == 0 {
		return agentShellCommand(legacy...), nil
	}
	if len(args) != 2 {
		return shellCommand{}, fmt.Errorf("usage: request wifi reconnect [timeout <ms>]")
	}
	key, err := resolveShellKeyword("request wifi reconnect option", args[0], []string{"timeout"})
	if err != nil {
		return shellCommand{}, err
	}
	if key != "timeout" {
		return shellCommand{}, fmt.Errorf("unknown request wifi reconnect option %q", args[0])
	}
	return agentShellCommand(append(legacy, args[1])...), nil
}

func parseShellWifiWait(args []string) (shellCommand, error) {
	if len(args) == 0 {
		return shellCommand{}, fmt.Errorf("usage: request wifi wait connected [ssid] ...")
	}
	name, err := resolveShellKeyword("request wifi wait command", args[0], []string{"connected"})
	if err != nil {
		return shellCommand{}, err
	}
	legacy := []string{"wifi", "wait", name}
	return parseShellWifiExpectation(legacy, args[1:], true)
}

func parseShellWifiAssert(args []string) (shellCommand, error) {
	legacy := []string{"wifi", "assert"}
	return parseShellWifiExpectation(legacy, args, false)
}

func parseShellWifiExpectation(legacy []string, args []string, allowPositionalSSID bool) (shellCommand, error) {
	rest := args
	if allowPositionalSSID && len(rest) > 0 && !isShellKeyword(rest[0], []string{"ssid", "bssid", "security", "band", "ip", "validated", "timeout"}) {
		legacy = append(legacy, rest[0])
		rest = rest[1:]
	}
	for i := 0; i < len(rest); i++ {
		key, err := resolveShellKeyword("wifi expectation option", rest[i], []string{"ssid", "bssid", "security", "band", "ip", "validated", "timeout"})
		if err != nil {
			return shellCommand{}, err
		}
		switch key {
		case "ip", "validated":
			legacy = append(legacy, "--"+key)
		default:
			value, next, err := shellValue(rest, i, key)
			if err != nil {
				return shellCommand{}, err
			}
			legacy = append(legacy, "--"+key, value)
			i = next
		}
	}
	return agentShellCommand(legacy...), nil
}

func parseShellMonitor(args []string) (shellCommand, error) {
	if len(args) == 0 {
		return shellCommand{}, fmt.Errorf("usage: monitor wifi [duration <ms>] [interval <ms>]")
	}
	name, err := resolveShellKeyword("monitor command", args[0], []string{"wifi"})
	if err != nil {
		return shellCommand{}, err
	}
	if name != "wifi" {
		return shellCommand{}, fmt.Errorf("unknown monitor command %q", args[0])
	}
	legacy := []string{"wifi", "monitor"}
	values := map[string]string{}
	for i := 1; i < len(args); i++ {
		key, err := resolveShellKeyword("monitor wifi option", args[i], []string{"duration", "interval"})
		if err != nil {
			return shellCommand{}, err
		}
		value, next, err := shellValue(args, i, key)
		if err != nil {
			return shellCommand{}, err
		}
		values[key] = value
		i = next
	}
	if values["duration"] != "" {
		legacy = append(legacy, values["duration"])
	}
	if values["interval"] != "" {
		if values["duration"] == "" {
			legacy = append(legacy, "10000")
		}
		legacy = append(legacy, values["interval"])
	}
	return agentShellCommand(legacy...), nil
}

func parseShellPing(args []string) (shellCommand, error) {
	if len(args) == 0 {
		return shellCommand{}, fmt.Errorf("usage: ping <host> [count <n>] [size <bytes>] [timeout <ms>]")
	}
	legacy := []string{"ping", args[0]}
	values := map[string]string{}
	for i := 1; i < len(args); i++ {
		key, err := resolveShellKeyword("ping option", args[i], []string{"count", "size", "timeout"})
		if err != nil {
			return shellCommand{}, err
		}
		value, next, err := shellValue(args, i, key)
		if err != nil {
			return shellCommand{}, err
		}
		values[key] = value
		i = next
	}
	if values["count"] != "" {
		legacy = append(legacy, values["count"])
	}
	for _, key := range []string{"size", "timeout"} {
		if values[key] != "" {
			legacy = append(legacy, "--"+key, values[key])
		}
	}
	return agentShellCommand(legacy...), nil
}

func parseShellTraceroute(args []string) (shellCommand, error) {
	if len(args) == 0 {
		return shellCommand{}, fmt.Errorf("usage: traceroute <host> [max-hops <n>] [via <host_or_ip>] [size <bytes>] [timeout <ms>]")
	}
	legacy := []string{"traceroute", args[0]}
	var via []string
	values := map[string]string{}
	for i := 1; i < len(args); i++ {
		key, err := resolveShellKeyword("traceroute option", args[i], []string{"max-hops", "via", "size", "timeout"})
		if err != nil {
			return shellCommand{}, err
		}
		value, next, err := shellValue(args, i, key)
		if err != nil {
			return shellCommand{}, err
		}
		if key == "via" {
			via = append(via, value)
		} else {
			values[key] = value
		}
		i = next
	}
	if values["max-hops"] != "" {
		legacy = append(legacy, values["max-hops"])
	}
	for _, hop := range via {
		legacy = append(legacy, "--via", hop)
	}
	for _, key := range []string{"size", "timeout"} {
		if values[key] != "" {
			legacy = append(legacy, "--"+key, values[key])
		}
	}
	return agentShellCommand(legacy...), nil
}

func parseShellPathMtu(args []string) (shellCommand, error) {
	if len(args) == 0 {
		return shellCommand{}, fmt.Errorf("usage: path-mtu <host> [min-mtu <bytes>] [max-mtu <bytes>] [timeout <ms>]")
	}
	legacy := []string{"path-mtu", args[0]}
	values := map[string]string{}
	for i := 1; i < len(args); i++ {
		key, err := resolveShellKeyword("path-mtu option", args[i], []string{"min-mtu", "max-mtu", "timeout"})
		if err != nil {
			return shellCommand{}, err
		}
		value, next, err := shellValue(args, i, key)
		if err != nil {
			return shellCommand{}, err
		}
		values[key] = value
		i = next
	}
	for _, key := range []string{"min-mtu", "max-mtu", "timeout"} {
		if values[key] != "" {
			legacy = append(legacy, "--"+key, values[key])
		}
	}
	return agentShellCommand(legacy...), nil
}

func parseShellTest(args []string) (shellCommand, error) {
	if len(args) == 0 {
		return shellCommand{}, fmt.Errorf("usage: test <dns|http|download>")
	}
	name, err := resolveShellKeyword("test command", args[0], []string{"dns", "http", "download"})
	if err != nil {
		return shellCommand{}, err
	}
	switch name {
	case "dns":
		return parseShellTestDNS(args[1:])
	case "http":
		return parseShellTestHTTP(args[1:])
	case "download":
		return parseShellTestDownload(args[1:])
	default:
		return shellCommand{}, fmt.Errorf("unknown test command %q", args[0])
	}
}

func parseShellTestDNS(args []string) (shellCommand, error) {
	if len(args) == 0 {
		return shellCommand{}, fmt.Errorf("usage: test dns <name> [type A|AAAA|ALL] [timeout <ms>]")
	}
	var name string
	values := map[string]string{}
	for i := 0; i < len(args); i++ {
		key, err := resolveShellKeyword("test dns option", args[i], []string{"type", "timeout"})
		if err == nil {
			value, next, err := shellValue(args, i, key)
			if err != nil {
				return shellCommand{}, err
			}
			if key == "type" {
				value, err = normalizeDNSQType(value)
				if err != nil {
					return shellCommand{}, err
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
			return shellCommand{}, fmt.Errorf("unexpected test dns argument %q", args[i])
		}
		name = args[i]
	}
	if name == "" {
		return shellCommand{}, fmt.Errorf("usage: test dns <name> [type A|AAAA|ALL] [timeout <ms>]")
	}
	legacy := []string{"dns", name}
	if values["type"] != "" {
		legacy = append(legacy, values["type"])
	}
	if values["timeout"] != "" {
		legacy = append(legacy, "--timeout", values["timeout"])
	}
	return agentShellCommand(legacy...), nil
}

func parseShellTestHTTP(args []string) (shellCommand, error) {
	if len(args) == 0 {
		return shellCommand{}, fmt.Errorf("usage: test http <url> [expected-status <code>] [timeout <ms>]")
	}
	var url string
	values := map[string]string{}
	for i := 0; i < len(args); i++ {
		key, err := resolveShellKeyword("test http option", args[i], []string{"expected-status", "timeout"})
		if err == nil {
			value, next, err := shellValue(args, i, key)
			if err != nil {
				return shellCommand{}, err
			}
			values[key] = value
			i = next
			continue
		}
		if url != "" {
			return shellCommand{}, fmt.Errorf("unexpected test http argument %q", args[i])
		}
		url = args[i]
	}
	if url == "" {
		return shellCommand{}, fmt.Errorf("usage: test http <url> [expected-status <code>] [timeout <ms>]")
	}
	legacy := []string{"http", normalizeHTTPURL(url)}
	if values["expected-status"] != "" {
		legacy = append(legacy, values["expected-status"])
	}
	if values["timeout"] != "" {
		legacy = append(legacy, "--timeout", values["timeout"])
	}
	return agentShellCommand(legacy...), nil
}

func parseShellTestDownload(args []string) (shellCommand, error) {
	if len(args) == 0 {
		return shellCommand{}, fmt.Errorf("usage: test download <url> [timeout <ms>]")
	}
	legacy := []string{"download", args[0]}
	for i := 1; i < len(args); i++ {
		key, err := resolveShellKeyword("test download option", args[i], []string{"timeout"})
		if err != nil {
			return shellCommand{}, err
		}
		value, next, err := shellValue(args, i, key)
		if err != nil {
			return shellCommand{}, err
		}
		legacy = append(legacy, "--"+key, value)
		i = next
	}
	return agentShellCommand(legacy...), nil
}

func agentShellCommand(args ...string) shellCommand {
	return shellCommand{kind: shellAgentCommand, operation: operationFromLegacyArgs(args)}
}

func shellValue(args []string, index int, name string) (string, int, error) {
	if index+1 >= len(args) {
		return "", index, fmt.Errorf("%s requires a value", name)
	}
	return args[index+1], index + 1, nil
}

func resolveShellKeyword(kind string, value string, candidates []string) (string, error) {
	return resolveUniquePrefix(kind, value, candidates)
}

func isShellKeyword(value string, candidates []string) bool {
	_, err := resolveShellKeyword("keyword", value, candidates)
	return err == nil
}

func wifiBandValues() []string {
	return []string{"all", "2.4ghz", "5ghz", "6ghz", "60ghz"}
}
