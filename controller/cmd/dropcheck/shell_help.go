package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

func printShellHelp() {
	writeShellHelp(os.Stdout)
}

func writeShellHelp(w io.Writer) {
	fmt.Fprintln(w, `commands:
  show devices
  show target
  set target <agent_id|adb_serial|number|all>
  clear target
  show wifi status
  show wifi diagnostics
  show wifi scan [all|2.4ghz|5ghz|6ghz|60ghz]
  show wifi scan fresh [all|2.4ghz|5ghz|6ghz|60ghz] [timeout <ms>]
  show wifi scan detail <ssid|bssid> [all|2.4ghz|5ghz|6ghz|60ghz]
  show wifi capabilities
  request wifi connect <ssid> passphrase <passphrase> [security <wpa2|wpa3|transition>] [bssid <bssid>] [band <band>] [mac-randomization <mode>] [timeout <ms>]
  request wifi disconnect
  request wifi forget <ssid|network_id>
  request wifi reconnect [timeout <ms>]
  request wifi wait connected [ssid] [bssid <bssid>] [security <mode>] [band <band>] [ip] [validated] [timeout <ms>]
  request wifi assert [ssid <ssid>] [bssid <bssid>] [security <mode>] [band <band>] [ip] [validated] [timeout <ms>]
  request wifi cycle <ssid> passphrase <passphrase> [security <mode>] [count <n>] [bssid <bssid>] [band <band>] [mac-randomization <mode>] [ping <host>] [http <url>] [forget] [pause <ms>] [timeout <ms>]
  monitor wifi [duration <ms>] [interval <ms>]
  ping <host> [count <n>] [size <bytes>] [timeout <ms>]
  traceroute <host> [max-hops <n>] [via <host_or_ip>] [size <bytes>] [timeout <ms>]
  path-mtu <host> [min-mtu <bytes>] [max-mtu <bytes>] [timeout <ms>]
  global-ip [ipv4|ipv6|all] [timeout <ms>]
  test dns <name> [type A|AAAA|ALL] [timeout <ms>]
  test http <url> [expected-status <code>] [timeout <ms>]
  test download <url> [timeout <ms>]
  quit

pipes:
  | display json
  | match <regex>
  | except <regex>
  | count
  | no-more`)
}

func isHelpLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if isShellHelpToken(trimmed) {
		return true
	}
	parts, err := splitPipeline(trimmed)
	if err != nil || len(parts) == 0 {
		return false
	}
	args, err := splitArgs(parts[0])
	if err != nil || len(args) == 0 {
		return false
	}
	return hasShellHelpSuffix(args[len(args)-1])
}

func isShellHelpToken(value string) bool {
	return value == "?" || value == "？"
}

func isShellHelpRune(value rune) bool {
	return value == '?' || value == '？'
}

func hasShellHelpSuffix(value string) bool {
	return strings.HasSuffix(value, "?") || strings.HasSuffix(value, "？")
}

func printShellContextHelp(line string) {
	writeShellContextHelp(os.Stdout, line)
}

func writeShellContextHelp(w io.Writer, line string) {
	entries := shellHelpEntries(line)
	if len(entries) == 0 {
		writeShellHelp(w)
		return
	}
	for _, entry := range entries {
		if entry.description == "" {
			fmt.Fprintf(w, "  %-24s\n", entry.token)
			continue
		}
		fmt.Fprintf(w, "  %-24s %s\n", entry.token, entry.description)
	}
}

func shellHelpEntries(line string) []helpEntry {
	commandLine := trimShellHelpSuffix(line)
	parts, err := splitPipeline(commandLine)
	if err != nil || len(parts) == 0 {
		return topHelpEntries()
	}
	args, err := splitArgs(parts[0])
	if err != nil {
		return topHelpEntries()
	}
	if len(args) == 0 {
		return topHelpEntries()
	}
	for i, arg := range args {
		if resolved, err := resolveContextKeyword(i, args[:i], arg); err == nil {
			args[i] = resolved
		}
	}
	if entries := helpEntriesForArgs(args); len(entries) > 0 {
		return entries
	}
	return terminalHelpEntries(commandLine)
}

func trimShellHelpSuffix(line string) string {
	trimmed := strings.TrimSpace(line)
	trimmed = strings.TrimSuffix(trimmed, "?")
	trimmed = strings.TrimSuffix(trimmed, "？")
	return strings.TrimSpace(trimmed)
}

func resolveContextKeyword(index int, previous []string, value string) (string, error) {
	switch index {
	case 0:
		return resolveShellKeyword("command", value, shellTopKeywords)
	case 1:
		switch previous[0] {
		case "show":
			return resolveShellKeyword("show command", value, []string{"devices", "target", "wifi"})
		case "set", "clear":
			return resolveShellKeyword(previous[0]+" command", value, []string{"target"})
		case "request", "monitor":
			return resolveShellKeyword(previous[0]+" command", value, []string{"wifi"})
		case "test":
			return resolveShellKeyword("test command", value, []string{"dns", "http", "download"})
		}
	case 2:
		if previous[0] == "show" && previous[1] == "wifi" {
			return resolveShellKeyword("show wifi command", value, []string{"status", "diagnostics", "scan", "capabilities"})
		}
		if previous[0] == "request" && previous[1] == "wifi" {
			return resolveShellKeyword("request wifi command", value, []string{"connect", "disconnect", "forget", "reconnect", "wait", "assert", "cycle"})
		}
	}
	return value, nil
}

func helpEntriesForArgs(args []string) []helpEntry {
	if len(args) == 0 {
		return topHelpEntries()
	}
	switch args[0] {
	case "show":
		if len(args) == 1 {
			return []helpEntry{{"devices", "Connected Android agents"}, {"target", "Current command target"}, {"wifi", "Wi-Fi state and diagnostics"}}
		}
		if len(args) == 2 && args[1] == "wifi" {
			return []helpEntry{{"status", "Current Wi-Fi connection and IP state"}, {"diagnostics", "Wi-Fi status, capabilities, networks, and scan"}, {"scan", "Cached or fresh scan results"}, {"capabilities", "Device Wi-Fi capabilities"}}
		}
		if len(args) == 3 && args[1] == "wifi" && args[2] == "scan" {
			return []helpEntry{{"fresh", "Trigger a fresh scan"}, {"detail", "Show detail for an SSID or BSSID"}, {"all", "All bands"}, {"2.4ghz", "2.4 GHz band"}, {"5ghz", "5 GHz band"}, {"6ghz", "6 GHz band"}, {"60ghz", "60 GHz band"}}
		}
	case "set":
		if len(args) == 1 {
			return []helpEntry{{"target", "Select an agent or all agents"}}
		}
		if len(args) == 2 && args[1] == "target" {
			return []helpEntry{{"all", "Send commands to all connected agents"}, {"<agent>", "Agent number, id, or adb serial"}}
		}
	case "clear":
		if len(args) == 1 {
			return []helpEntry{{"target", "Clear all-target mode and return to the first agent"}}
		}
	case "request":
		if len(args) == 1 {
			return []helpEntry{{"wifi", "Run a Wi-Fi operation"}}
		}
		if len(args) == 2 && args[1] == "wifi" {
			return []helpEntry{{"connect", "Connect to an SSID"}, {"disconnect", "Disconnect Wi-Fi"}, {"forget", "Forget an SSID or network id"}, {"reconnect", "Reconnect Wi-Fi"}, {"wait", "Wait for Wi-Fi state"}, {"assert", "Assert Wi-Fi state"}, {"cycle", "Repeat connect checks"}}
		}
		if len(args) >= 3 && args[1] == "wifi" {
			return requestWifiHelp(args[2])
		}
	case "monitor":
		if len(args) == 1 {
			return []helpEntry{{"wifi", "Stream Wi-Fi state changes for a bounded duration"}}
		}
		if len(args) == 2 && args[1] == "wifi" {
			return []helpEntry{{"duration", "Duration in milliseconds"}, {"interval", "Sample interval in milliseconds"}}
		}
	case "ping":
		return []helpEntry{{"count", "Packet count"}, {"size", "Payload size in bytes"}, {"timeout", "Timeout in milliseconds"}}
	case "traceroute":
		return []helpEntry{{"max-hops", "Maximum hops"}, {"via", "Required hop for analysis"}, {"size", "Packet size in bytes"}, {"timeout", "Timeout in milliseconds"}}
	case "path-mtu":
		return []helpEntry{{"min-mtu", "Minimum MTU in bytes"}, {"max-mtu", "Maximum MTU in bytes"}, {"timeout", "Timeout in milliseconds"}}
	case "global-ip":
		return globalIPHelp(args[1:])
	case "test":
		if len(args) == 1 {
			return []helpEntry{{"dns", "Resolve a DNS name"}, {"http", "Check an HTTP status"}, {"download", "Download a URL"}}
		}
		switch args[1] {
		case "dns":
			return testDNSHelp(args[2:])
		case "http":
			return testHTTPHelp(args[2:])
		case "download":
			return []helpEntry{{"<url>", "URL to download"}, {"timeout", "Timeout in milliseconds"}}
		}
	}
	return nil
}

func terminalHelpEntries(line string) []helpEntry {
	command, err := parseShellLine(line)
	if err != nil {
		return nil
	}
	return terminalHelpEntriesForCommand(command)
}

func terminalHelpEntriesForArgs(args []string) []helpEntry {
	command, err := parseShellArgs(args)
	if err != nil {
		return nil
	}
	return terminalHelpEntriesForCommand(command)
}

func terminalHelpEntriesForCommand(command shellCommand) []helpEntry {
	entries := []helpEntry{{"<cr>", "Execute command; no further command keywords"}}
	if commandSupportsPipeHelp(command) {
		entries = append(entries, pipeHelpEntries()...)
	}
	return entries
}

func testHTTPHelp(args []string) []helpEntry {
	hasURL := false
	used := map[string]bool{}
	for i := 0; i < len(args); i++ {
		key, err := resolveShellKeyword("test http option", args[i], []string{"expected-status", "timeout"})
		if err == nil {
			used[key] = true
			if i+1 < len(args) {
				i++
			}
			continue
		}
		if !hasURL {
			hasURL = true
		}
	}
	var entries []helpEntry
	if !hasURL {
		entries = append(entries, helpEntry{"<url>", "HTTP or HTTPS URL; https:// is assumed when omitted"})
	}
	if !used["expected-status"] {
		entries = append(entries, helpEntry{"expected-status", "Expected HTTP status"})
	}
	if !used["timeout"] {
		entries = append(entries, helpEntry{"timeout", "Timeout in milliseconds"})
	}
	if hasURL {
		terminal := terminalHelpEntriesForArgs(append([]string{"test", "http"}, args...))
		entries = append(entries, terminal...)
	}
	return entries
}

func testDNSHelp(args []string) []helpEntry {
	hasName := false
	used := map[string]bool{}
	for i := 0; i < len(args); i++ {
		key, err := resolveShellKeyword("test dns option", args[i], []string{"type", "timeout"})
		if err == nil {
			used[key] = true
			if i+1 < len(args) {
				i++
			}
			continue
		}
		if hasName && !used["type"] {
			if _, err := normalizeDNSQType(args[i]); err == nil {
				used["type"] = true
				continue
			}
		}
		if !hasName {
			hasName = true
		}
	}
	var entries []helpEntry
	if !hasName {
		entries = append(entries, helpEntry{"<name>", "DNS name to resolve"})
	}
	if !used["type"] {
		entries = append(entries, helpEntry{"type", "A, AAAA, or ALL"})
	}
	if !used["timeout"] {
		entries = append(entries, helpEntry{"timeout", "Timeout in milliseconds"})
	}
	if hasName {
		terminal := terminalHelpEntriesForArgs(append([]string{"test", "dns"}, args...))
		entries = append(entries, terminal...)
	}
	return entries
}

func globalIPHelp(args []string) []helpEntry {
	hasFamily := false
	usedTimeout := false
	for i := 0; i < len(args); i++ {
		if key, err := resolveShellKeyword("global-ip option", args[i], []string{"timeout"}); err == nil {
			if key == "timeout" {
				usedTimeout = true
				if i+1 < len(args) {
					i++
				}
			}
			continue
		}
		if _, err := normalizeIpFamily(args[i]); err == nil {
			hasFamily = true
		}
	}
	var entries []helpEntry
	if !hasFamily {
		entries = append(entries,
			helpEntry{"ipv4", "Check global IPv4 through ifconfig.me"},
			helpEntry{"ipv6", "Check global IPv6 through ifconfig.me"},
			helpEntry{"all", "Check both address families"},
		)
	}
	if !usedTimeout {
		entries = append(entries, helpEntry{"timeout", "Per-family timeout in milliseconds"})
	}
	terminal := terminalHelpEntriesForArgs(append([]string{"global-ip"}, args...))
	entries = append(entries, terminal...)
	return entries
}

func commandSupportsPipeHelp(command shellCommand) bool {
	switch command.kind {
	case shellShowDevices, shellShowTarget, shellClearTarget, shellAgentCommand:
		return true
	default:
		return false
	}
}

func pipeHelpEntries() []helpEntry {
	return []helpEntry{
		{"| display json", "Render JSON output"},
		{"| match <regex>", "Include matching lines"},
		{"| except <regex>", "Exclude matching lines"},
		{"| count", "Count non-empty output lines"},
		{"| no-more", "Disable paging"},
	}
}

func requestWifiHelp(command string) []helpEntry {
	switch command {
	case "connect":
		return []helpEntry{{"passphrase", "WPA/WPA3 passphrase"}, {"security", "wpa2, wpa3, or transition"}, {"bssid", "Target BSSID"}, {"band", "all, 2.4ghz, 5ghz, 6ghz, or 60ghz"}, {"mac-randomization", "auto, none, persistent, or non-persistent"}, {"timeout", "Timeout in milliseconds"}}
	case "disconnect":
		return nil
	case "forget":
		return []helpEntry{{"<ssid|network_id>", "Network to forget"}}
	case "reconnect":
		return []helpEntry{{"timeout", "Timeout in milliseconds"}}
	case "wait":
		return []helpEntry{{"connected", "Wait until connected"}}
	case "assert":
		return []helpEntry{{"ssid", "Expected SSID"}, {"bssid", "Expected BSSID"}, {"security", "Expected security"}, {"band", "Expected band"}, {"ip", "Require IP address"}, {"validated", "Require validated internet"}, {"timeout", "Timeout in milliseconds"}}
	case "cycle":
		return []helpEntry{{"passphrase", "WPA/WPA3 passphrase"}, {"security", "wpa2, wpa3, or transition"}, {"count", "Cycle count"}, {"bssid", "Target BSSID"}, {"band", "Target band"}, {"mac-randomization", "MAC randomization mode"}, {"ping", "Ping host after connect"}, {"http", "HTTP URL after connect"}, {"forget", "Forget after each cycle"}, {"pause", "Pause between cycles in milliseconds"}, {"timeout", "Per-connect timeout in milliseconds"}}
	default:
		return nil
	}
}

func topHelpEntries() []helpEntry {
	return []helpEntry{
		{"show", "Display device, target, and Wi-Fi state"},
		{"set", "Set shell target"},
		{"clear", "Clear shell target state"},
		{"request", "Run a state-changing operation"},
		{"monitor", "Run a bounded monitor"},
		{"ping", "Ping from the selected Android agent"},
		{"traceroute", "Traceroute from the selected Android agent"},
		{"path-mtu", "Discover path MTU from the selected Android agent"},
		{"global-ip", "Check global IPv4/IPv6 via ifconfig.me"},
		{"test", "Run DNS, HTTP, or download checks"},
		{"help", "Show command summary"},
		{"quit", "Exit the shell"},
	}
}

func completeShellLine(line string, state *shellState) []string {
	_ = state
	parts, err := splitPipeline(line)
	if err != nil || len(parts) == 0 {
		return nil
	}
	if strings.Contains(line, "|") && strings.HasSuffix(line, parts[len(parts)-1]) {
		return completePipeSegment(line, parts[len(parts)-1])
	}
	trailingSpace := strings.HasSuffix(line, " ") || line == ""
	args, err := splitArgs(parts[0])
	if err != nil {
		return nil
	}
	prefix := ""
	baseArgs := args
	if !trailingSpace && len(args) > 0 {
		prefix = args[len(args)-1]
		baseArgs = args[:len(args)-1]
	}
	candidates := completionCandidatesForArgs(baseArgs)
	head := line[:len(line)-len(prefix)]
	var out []string
	for _, candidate := range candidates {
		if strings.HasPrefix(candidate, prefix) {
			out = append(out, head+candidate)
		}
	}
	return out
}

func completePipeSegment(line string, segment string) []string {
	trimmed := strings.TrimLeft(segment, " ")
	prefix := trimmed
	if strings.Contains(trimmed, " ") {
		return nil
	}
	head := line[:len(line)-len(prefix)]
	var out []string
	for _, candidate := range []string{"display json", "match", "except", "count", "no-more"} {
		if strings.HasPrefix(candidate, prefix) {
			out = append(out, head+candidate)
		}
	}
	return out
}

func completionCandidatesForArgs(args []string) []string {
	if len(args) == 0 {
		return shellTopKeywords
	}
	resolved := append([]string(nil), args...)
	for i, arg := range resolved {
		if value, err := resolveContextKeyword(i, resolved[:i], arg); err == nil {
			resolved[i] = value
		}
	}
	switch len(resolved) {
	case 1:
		switch resolved[0] {
		case "show":
			return []string{"devices", "target", "wifi"}
		case "set", "clear":
			return []string{"target"}
		case "request", "monitor":
			return []string{"wifi"}
		case "test":
			return []string{"dns", "http", "download"}
		default:
			return nil
		}
	case 2:
		if resolved[0] == "show" && resolved[1] == "wifi" {
			return []string{"status", "diagnostics", "scan", "capabilities"}
		}
		if resolved[0] == "request" && resolved[1] == "wifi" {
			return []string{"connect", "disconnect", "forget", "reconnect", "wait", "assert", "cycle"}
		}
		if resolved[0] == "monitor" && resolved[1] == "wifi" {
			return []string{"duration", "interval"}
		}
	case 3:
		if resolved[0] == "show" && resolved[1] == "wifi" && resolved[2] == "scan" {
			return []string{"fresh", "detail", "all", "2.4ghz", "5ghz", "6ghz", "60ghz"}
		}
		if resolved[0] == "request" && resolved[1] == "wifi" && resolved[2] == "wait" {
			return []string{"connected"}
		}
	}
	if len(resolved) >= 1 {
		lastCommand := resolved[len(resolved)-1]
		switch {
		case resolved[0] == "ping":
			return []string{"count", "size", "timeout"}
		case resolved[0] == "traceroute":
			return []string{"max-hops", "via", "size", "timeout"}
		case resolved[0] == "path-mtu":
			return []string{"min-mtu", "max-mtu", "timeout"}
		case resolved[0] == "global-ip":
			return globalIPCompletionCandidates(resolved[1:])
		case resolved[0] == "test" && len(resolved) >= 2:
			return testCompletionCandidates(resolved[1])
		case resolved[0] == "request" && len(resolved) >= 3 && resolved[1] == "wifi":
			return requestWifiCompletionCandidates(resolved[2], lastCommand)
		}
	}
	return nil
}

func requestWifiCompletionCandidates(command string, last string) []string {
	_ = last
	switch command {
	case "connect":
		return []string{"passphrase", "security", "bssid", "band", "mac-randomization", "timeout"}
	case "reconnect":
		return []string{"timeout"}
	case "assert":
		return []string{"ssid", "bssid", "security", "band", "ip", "validated", "timeout"}
	case "cycle":
		return []string{"passphrase", "security", "count", "bssid", "band", "mac-randomization", "ping", "http", "forget", "pause", "timeout"}
	case "wait":
		return []string{"connected", "bssid", "security", "band", "ip", "validated", "timeout"}
	default:
		return nil
	}
}

func testCompletionCandidates(command string) []string {
	switch command {
	case "dns":
		return []string{"type", "timeout"}
	case "http":
		return []string{"expected-status", "timeout"}
	case "download":
		return []string{"timeout"}
	default:
		return nil
	}
}

func globalIPCompletionCandidates(args []string) []string {
	hasFamily := false
	usedTimeout := false
	for i := 0; i < len(args); i++ {
		if args[i] == "timeout" {
			usedTimeout = true
			if i+1 < len(args) {
				i++
			}
			continue
		}
		if _, err := normalizeIpFamily(args[i]); err == nil {
			hasFamily = true
		}
	}
	var candidates []string
	if !hasFamily {
		candidates = append(candidates, "ipv4", "ipv6", "all")
	}
	if !usedTimeout {
		candidates = append(candidates, "timeout")
	}
	return candidates
}
