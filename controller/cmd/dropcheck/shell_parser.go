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

var shellTopKeywords = []string{"show", "set", "clear", "request", "monitor", "ping", "traceroute", "path-mtu", "global-ip", "test", "help", "exit", "quit"}

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
	case "global-ip":
		return parseShellGlobalIp(args[1:])
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
		var band string
		values := map[string]string{}
		for i := 1; i < len(args); i++ {
			if key, err := resolveShellKeyword("show wifi scan fresh option", args[i], []string{"timeout"}); err == nil {
				value, next, err := shellValue(args, i, key)
				if err != nil {
					return shellCommand{}, err
				}
				values[key] = value
				i = next
				continue
			}
			value, err := resolveShellKeyword("wifi band", args[i], wifiBandValues())
			if err != nil {
				return shellCommand{}, err
			}
			if band != "" {
				return shellCommand{}, fmt.Errorf("wifi scan fresh band specified twice")
			}
			band = value
		}
		if band != "" {
			legacy = append(legacy, band)
		}
		if values["timeout"] != "" {
			legacy = append(legacy, "--timeout", values["timeout"])
		}
		return agentShellCommand(legacy...), nil
	case "detail":
		if len(args) < 2 || len(args) > 3 {
			return shellCommand{}, fmt.Errorf("usage: show wifi scan detail [all|2.4ghz|5ghz|6ghz|60ghz] <ssid|bssid>")
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
					return shellCommand{}, err
				}
				band = value
			}
		}
		legacy = append(legacy, "detail", target)
		if band != "" {
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
		return shellCommand{}, fmt.Errorf("usage: request wifi %s passphrase <passphrase> [security <wpa2|wpa3|transition>] ... <ssid>", command)
	}
	var ssid string
	values := map[string]string{}
	flags := map[string]bool{}
	allowed := []string{"passphrase", "security", "bssid", "band", "mac-randomization", "timeout"}
	if command == "cycle" {
		allowed = append(allowed, "count", "ping", "http", "forget", "pause")
	}
	for i := 0; i < len(args); i++ {
		key, err := resolveShellKeyword("request wifi "+command+" option", args[i], allowed)
		if err != nil {
			if ssid != "" {
				return shellCommand{}, fmt.Errorf("unexpected request wifi %s argument %q", command, args[i])
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
			return shellCommand{}, err
		}
		values[key] = value
		i = next
	}
	if ssid == "" {
		return shellCommand{}, fmt.Errorf("request wifi %s requires <ssid>", command)
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
	var positionalSSID string
	values := map[string]string{}
	flags := map[string]bool{}
	for i := 0; i < len(args); i++ {
		key, err := resolveShellKeyword("wifi expectation option", args[i], []string{"ssid", "bssid", "security", "band", "ip", "validated", "timeout"})
		if err != nil {
			if !allowPositionalSSID || positionalSSID != "" {
				return shellCommand{}, err
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
				return shellCommand{}, err
			}
			values[key] = value
			i = next
		}
	}
	if positionalSSID != "" && values["ssid"] != "" {
		return shellCommand{}, fmt.Errorf("wifi ssid specified twice")
	}
	if positionalSSID != "" {
		legacy = append(legacy, positionalSSID)
	}
	for _, key := range []string{"ssid", "bssid", "security", "band", "timeout"} {
		if values[key] != "" {
			legacy = append(legacy, "--"+key, values[key])
		}
	}
	for _, key := range []string{"ip", "validated"} {
		if flags[key] {
			legacy = append(legacy, "--"+key)
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
		return shellCommand{}, fmt.Errorf("usage: ping [count <n>] [size <bytes>] [timeout <ms>] <host>")
	}
	var host string
	values := map[string]string{}
	for i := 0; i < len(args); i++ {
		key, err := resolveShellKeyword("ping option", args[i], []string{"count", "size", "timeout"})
		if err != nil {
			if host != "" {
				return shellCommand{}, fmt.Errorf("unexpected ping argument %q", args[i])
			}
			host = args[i]
			continue
		}
		value, next, err := shellValue(args, i, key)
		if err != nil {
			return shellCommand{}, err
		}
		values[key] = value
		i = next
	}
	if host == "" {
		return shellCommand{}, fmt.Errorf("usage: ping [count <n>] [size <bytes>] [timeout <ms>] <host>")
	}
	legacy := []string{"ping", host}
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
		return shellCommand{}, fmt.Errorf("usage: traceroute [max-hops <n>] [via <host_or_ip>] [size <bytes>] [timeout <ms>] <host>")
	}
	var host string
	var via []string
	values := map[string]string{}
	for i := 0; i < len(args); i++ {
		key, err := resolveShellKeyword("traceroute option", args[i], []string{"max-hops", "via", "size", "timeout"})
		if err != nil {
			if host != "" {
				return shellCommand{}, fmt.Errorf("unexpected traceroute argument %q", args[i])
			}
			host = args[i]
			continue
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
	if host == "" {
		return shellCommand{}, fmt.Errorf("usage: traceroute [max-hops <n>] [via <host_or_ip>] [size <bytes>] [timeout <ms>] <host>")
	}
	legacy := []string{"traceroute", host}
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
		return shellCommand{}, fmt.Errorf("usage: path-mtu [min-mtu <bytes>] [max-mtu <bytes>] [timeout <ms>] <host>")
	}
	var host string
	values := map[string]string{}
	for i := 0; i < len(args); i++ {
		key, err := resolveShellKeyword("path-mtu option", args[i], []string{"min-mtu", "max-mtu", "timeout"})
		if err != nil {
			if host != "" {
				return shellCommand{}, fmt.Errorf("unexpected path-mtu argument %q", args[i])
			}
			host = args[i]
			continue
		}
		value, next, err := shellValue(args, i, key)
		if err != nil {
			return shellCommand{}, err
		}
		values[key] = value
		i = next
	}
	if host == "" {
		return shellCommand{}, fmt.Errorf("usage: path-mtu [min-mtu <bytes>] [max-mtu <bytes>] [timeout <ms>] <host>")
	}
	legacy := []string{"path-mtu", host}
	for _, key := range []string{"min-mtu", "max-mtu", "timeout"} {
		if values[key] != "" {
			legacy = append(legacy, "--"+key, values[key])
		}
	}
	return agentShellCommand(legacy...), nil
}

func parseShellGlobalIp(args []string) (shellCommand, error) {
	legacy := []string{"global-ip"}
	var family string
	values := map[string]string{}
	for i := 0; i < len(args); i++ {
		key, err := resolveShellKeyword("global-ip option", args[i], []string{"family", "timeout"})
		if err != nil {
			value, err := normalizeIpFamily(args[i])
			if err != nil {
				return shellCommand{}, err
			}
			if family != "" || values["family"] != "" {
				return shellCommand{}, fmt.Errorf("global-ip family specified twice")
			}
			family = value
			continue
		}
		value, next, err := shellValue(args, i, key)
		if err != nil {
			return shellCommand{}, err
		}
		if key == "family" {
			value, err = normalizeIpFamily(value)
			if err != nil {
				return shellCommand{}, err
			}
		}
		if key == "family" && (family != "" || values["family"] != "") {
			return shellCommand{}, fmt.Errorf("global-ip family specified twice")
		}
		values[key] = value
		i = next
	}
	if values["family"] != "" {
		family = values["family"]
	}
	if family != "" {
		legacy = append(legacy, family)
	}
	if values["timeout"] != "" {
		legacy = append(legacy, "--timeout", values["timeout"])
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
		return shellCommand{}, fmt.Errorf("usage: test dns [type A|AAAA|ALL] [timeout <ms>] <name>")
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
		return shellCommand{}, fmt.Errorf("usage: test dns [type A|AAAA|ALL] [timeout <ms>] <name>")
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
		return shellCommand{}, fmt.Errorf("usage: test http [expected-status <code>] [timeout <ms>] <url>")
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
		return shellCommand{}, fmt.Errorf("usage: test http [expected-status <code>] [timeout <ms>] <url>")
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
		return shellCommand{}, fmt.Errorf("usage: test download [timeout <ms>] <url>")
	}
	var url string
	values := map[string]string{}
	for i := 0; i < len(args); i++ {
		key, err := resolveShellKeyword("test download option", args[i], []string{"timeout"})
		if err != nil {
			if url != "" {
				return shellCommand{}, fmt.Errorf("unexpected test download argument %q", args[i])
			}
			url = args[i]
			continue
		}
		value, next, err := shellValue(args, i, key)
		if err != nil {
			return shellCommand{}, err
		}
		values[key] = value
		i = next
	}
	if url == "" {
		return shellCommand{}, fmt.Errorf("usage: test download [timeout <ms>] <url>")
	}
	legacy := []string{"download", url}
	if values["timeout"] != "" {
		legacy = append(legacy, "--timeout", values["timeout"])
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
