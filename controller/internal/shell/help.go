package shell

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// PrintHelp writes the full shell help summary to stdout.
func PrintHelp() {
	writeShellHelp(os.Stdout)
}

func writeShellHelp(w io.Writer) {
	_, _ = fmt.Fprintln(w, `commands:
  show devices
  show config [standalone]
  show wifi status
  show wifi diagnostics
  show wifi eht [fresh [timeout <ms>]] [ssid <ssid>|bssid <bssid>]
  show wifi scan [brief [mlo]] [all|2.4ghz|5ghz|6ghz|60ghz]
  show wifi scan fresh [brief [mlo]] [timeout <ms>] [all|2.4ghz|5ghz|6ghz|60ghz]
  show wifi scan detail [all|2.4ghz|5ghz|6ghz|60ghz] <ssid|bssid>
  show wifi capabilities
  show ip status
  show adb cmd wifi status
  show adb dumpsys wifi
  show adb dumpsys connectivity [networks|requests|diagnostics|trafficcontroller]
  show adb diagnostics full
  show standalone status
  show standalone runs [limit <n>] [synced]
  show standalone run <run-id>
  clear standalone runs [synced|all]
  sync standalone runs [output <dir>] [limit <n>] [mark-synced|keep-unsynced]
  configure
  request [<request-command>]

configure mode:
  show [standalone]
  set standalone enabled
  set standalone disabled
  set standalone retention <duration>
  set standalone max-size <bytes>
  set standalone live watch <file> [<file>...]
  set standalone upload to <url>
  set standalone upload via wifi essid <essid> passphrase <psk> [security <auto|wpa2|wpa3|transition>]
  set standalone festa <name> enabled
  set standalone festa <name> interval <duration>
  set standalone festa <name> wifi <name> match <essid|bssid> <value> [mac-randomization <mode>]
  set standalone festa <name> wifi <name> passphrase <passphrase> [security <auto|wpa2|wpa3|transition>]
  set standalone festa <name> check <name> test dns name <domain> [type <A|AAAA|ALL>] [timeout <duration>]
  set standalone festa <name> check <name> test ping host <host> [count <n>] [size <bytes>] [timeout <duration>]
  set standalone festa <name> check <name> test http url <url> [expected-status <code>] [timeout <duration>]
  delete standalone [upload|upload to|upload via wifi|festa <name>|festa <name> wifi <name>|festa <name> check <name>]
  run show <devices|config|wifi|ip|adb|standalone>
  run clear standalone runs [synced|all]
  run sync standalone runs [output <dir>] [limit <n>] [mark-synced|keep-unsynced]
  run request <request-command>
  exit
  quit

request mode:
  wifi connect passphrase <passphrase> [security <auto|wpa2|wpa3|transition>] [bssid <bssid>] [band <band>] [mac-randomization <mode>] [timeout <ms>] <ssid>
  wifi disconnect
  wifi forget <ssid|network_id>
  wifi reconnect [timeout <ms>]
  wifi wait connected [bssid <bssid>] [security <mode>] [band <band>] [ip] [validated] [timeout <ms>] [ssid]
  wifi assert [ssid <ssid>] [bssid <bssid>] [security <mode>] [band <band>] [ip] [validated] [timeout <ms>]
  wifi cycle passphrase <passphrase> [security <auto|wpa2|wpa3|transition>] [count <n>] [bssid <bssid>] [band <band>] [mac-randomization <mode>] [ping <host>] [http <url>] [forget] [pause <ms>] [timeout <ms>] <ssid>
  standalone run once [festa <name>] [save]
  monitor wifi [duration <ms>] [interval <ms>]
  ping [count <n>] [size <bytes>] [timeout <ms>] <host>
  traceroute [max-hops <n>] [via <host_or_ip>] [size <bytes>] [timeout <ms>] <host>
  path-mtu [min-mtu <bytes>] [max-mtu <bytes>] [timeout <ms>] <host>
  global-ip [timeout <ms>] [ipv4|ipv6|all]
  dns [type A|AAAA|ALL] [timeout <ms>] <name>
  http [expected-status <code>] [timeout <ms>] <url>
  download [timeout <ms>] <url>
  exit
  quit

pipes:
  | display json
  | display set
  | match <regex>
  | except <regex>
  | count
  | no-more`)
}

// IsHelpLine reports whether line is a help request.
//
// A line is a help request when it is exactly "?" or when the command segment
// ends in a help suffix. Pipeline parse errors are ignored so the REPL can keep
// help detection lightweight.
func IsHelpLine(line string) bool {
	return isHelpLine(line)
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

// IsHelpRune reports whether value is a supported help trigger rune.
func IsHelpRune(value rune) bool {
	return isShellHelpRune(value)
}

func isShellHelpRune(value rune) bool {
	return value == '?' || value == '？'
}

func hasShellHelpSuffix(value string) bool {
	return strings.HasSuffix(value, "?") || strings.HasSuffix(value, "？")
}

// PrintContextHelp writes contextual help for line to stdout.
func PrintContextHelp(line string) {
	printShellContextHelp(line)
}

func printShellContextHelp(line string) {
	writeShellContextHelp(os.Stdout, line)
}

// WriteContextHelp writes contextual help for line to w.
//
// If line cannot be resolved to a narrower context, the full shell help summary
// is written.
func WriteContextHelp(w io.Writer, line string) {
	writeShellContextHelp(w, line)
}

// WriteRequestContextHelp writes contextual help for a request-mode line to w.
func WriteRequestContextHelp(w io.Writer, line string) {
	writeShellContextHelpInMode(w, line, ModeRequest)
}

// WriteConfigureContextHelp writes contextual help for a configure-mode line to
// w.
func WriteConfigureContextHelp(w io.Writer, line string) {
	writeShellContextHelpInMode(w, line, ModeConfigure)
}

func writeShellContextHelp(w io.Writer, line string) {
	writeShellContextHelpInMode(w, line, ModeOperational)
}

func writeShellContextHelpInMode(w io.Writer, line string, mode Mode) {
	entries := shellHelpEntriesInMode(line, mode)
	if len(entries) == 0 {
		writeShellHelp(w)
		return
	}
	for _, entry := range entries {
		if entry.Description == "" {
			_, _ = fmt.Fprintf(w, "  %-24s\n", entry.Token)
			continue
		}
		_, _ = fmt.Fprintf(w, "  %-24s %s\n", entry.Token, entry.Description)
	}
}

func shellHelpEntriesInMode(line string, mode Mode) []HelpEntry {
	if pipeLine, ok := trimShellHelpSuffixPreserveSpace(line); ok {
		if segment, inPipe, err := currentPipelineSegment(pipeLine); err == nil && inPipe {
			return pipeHelpEntriesForSegment(segment)
		}
	}
	commandLine := trimShellHelpSuffix(line)
	parts, err := splitPipeline(commandLine)
	if err != nil || len(parts) == 0 {
		return topHelpEntriesInMode(mode)
	}
	args, err := splitArgs(parts[0])
	if err != nil {
		return topHelpEntriesInMode(mode)
	}
	if len(args) == 0 {
		return topHelpEntriesInMode(mode)
	}
	// Contextual help should behave like parsing: partial but unambiguous
	// prefixes are resolved before selecting the next set of candidates.
	for i, arg := range args {
		if resolved, err := resolveContextKeywordInMode(i, args[:i], arg, mode); err == nil {
			args[i] = resolved
		}
	}
	if entries := valueHelpEntriesForArgsInMode(args, mode); len(entries) > 0 {
		return entries
	}
	if entries := helpEntriesForArgsInMode(args, mode); len(entries) > 0 {
		return entries
	}
	return terminalHelpEntriesInMode(commandLine, mode)
}

func trimShellHelpSuffix(line string) string {
	trimmed := strings.TrimSpace(line)
	trimmed = strings.TrimSuffix(trimmed, "?")
	trimmed = strings.TrimSuffix(trimmed, "？")
	return strings.TrimSpace(trimmed)
}

func trimShellHelpSuffixPreserveSpace(line string) (string, bool) {
	trimmedRight := strings.TrimRight(line, " \t\r\n")
	if trimmedRight == "" {
		return "", false
	}
	last := []rune(trimmedRight)
	if !isShellHelpRune(last[len(last)-1]) {
		return "", false
	}
	return strings.TrimRightFunc(trimmedRight, isShellHelpRune), true
}

func valueHelpEntriesForArgsInMode(args []string, mode Mode) []HelpEntry {
	switch mode {
	case ModeRequest:
		if entries := requestValueHelpEntriesForArgs(args); len(entries) > 0 {
			return entries
		}
	case ModeOperational:
		if requestArgs, ok := operationalRequestArgs(args); ok {
			if entries := requestValueHelpEntriesForArgs(requestArgs); len(entries) > 0 {
				return entries
			}
		}
	}
	candidates, ok := valueCompletionCandidatesForArgs(args)
	if !ok {
		return nil
	}
	entries := make([]HelpEntry, 0, len(candidates))
	for _, candidate := range candidates {
		entries = append(entries, HelpEntry{Token: candidate})
	}
	return entries
}

func requestValueHelpEntriesForArgs(args []string) []HelpEntry {
	candidates, ok := requestValueCompletionCandidatesForArgs(args)
	if !ok {
		return nil
	}
	entries := make([]HelpEntry, 0, len(candidates))
	for _, candidate := range candidates {
		entries = append(entries, HelpEntry{Token: candidate})
	}
	return entries
}

func resolveContextKeyword(index int, previous []string, value string) (string, error) {
	return resolveContextKeywordInMode(index, previous, value, ModeOperational)
}

func resolveContextKeywordInMode(index int, previous []string, value string, mode Mode) (string, error) {
	if mode == ModeRequest {
		return resolveRequestContextKeyword(index, previous, value)
	}
	if mode == ModeConfigure {
		return resolveConfigureContextKeyword(index, previous, value)
	}
	if len(previous) > 0 {
		if previous[0] == "request" {
			return resolveRequestContextKeyword(index-1, previous[1:], value)
		}
	}
	switch index {
	case 0:
		return resolveShellKeyword("command", value, shellOperationalKeywords)
	case 1:
		switch previous[0] {
		case "show":
			return resolveShellKeyword("show command", value, []string{"devices", "config", "wifi", "ip", "standalone", "adb"})
		case "sync":
			return resolveShellKeyword("sync command", value, []string{"standalone"})
		case "clear":
			return resolveShellKeyword(previous[0]+" command", value, []string{"standalone"})
		}
	case 2:
		if previous[0] == "show" && previous[1] == "config" {
			return resolveShellKeyword("show config command", value, []string{"standalone"})
		}
		if previous[0] == "show" && previous[1] == "wifi" {
			return resolveShellKeyword("show wifi command", value, []string{"status", "diagnostics", "eht", "scan", "capabilities"})
		}
		if previous[0] == "show" && previous[1] == "ip" {
			return resolveShellKeyword("show ip command", value, []string{"status"})
		}
		if previous[0] == "show" && previous[1] == "standalone" {
			return resolveShellKeyword("show standalone command", value, []string{"status", "runs", "run"})
		}
		if previous[0] == "show" && previous[1] == "adb" {
			return resolveShellKeyword("show adb command", value, []string{"cmd", "dumpsys", "diagnostics", "wifi", "connectivity"})
		}
		if previous[0] == "sync" && previous[1] == "standalone" {
			return resolveShellKeyword("sync standalone command", value, []string{"runs"})
		}
		if previous[0] == "clear" && previous[1] == "standalone" {
			return resolveShellKeyword("clear standalone command", value, []string{"runs"})
		}
	}
	return value, nil
}

func resolveConfigureContextKeyword(index int, previous []string, value string) (string, error) {
	switch index {
	case 0:
		return resolveShellKeyword("configure command", value, shellConfigureKeywords)
	case 1:
		switch previous[0] {
		case "show":
			return resolveShellKeyword("show config command", value, []string{"standalone"})
		case "set":
			return resolveShellKeyword("set command", value, []string{"standalone"})
		case "delete":
			return resolveShellKeyword("delete command", value, []string{"standalone"})
		case "run":
			return resolveShellKeyword("run command", value, shellTopKeywords)
		}
	case 2:
		if previous[0] == "set" && previous[1] == "standalone" {
			return resolveShellKeyword("set standalone command", value, []string{"enabled", "disabled", "retention", "max-size", "live", "upload", "festa"})
		}
		if previous[0] == "delete" && previous[1] == "standalone" {
			return resolveShellKeyword("delete standalone command", value, []string{"upload", "festa"})
		}
	}
	return value, nil
}

func resolveRequestContextKeyword(index int, previous []string, value string) (string, error) {
	switch index {
	case 0:
		return resolveShellKeyword("request command", value, shellRequestKeywords)
	case 1:
		switch previous[0] {
		case "wifi":
			return resolveShellKeyword("wifi command", value, []string{"connect", "disconnect", "forget", "reconnect", "wait", "assert", "cycle"})
		case "standalone":
			return resolveShellKeyword("standalone command", value, []string{"run"})
		case "monitor":
			return resolveShellKeyword("monitor command", value, []string{"wifi"})
		}
	case 2:
		if previous[0] == "wifi" && previous[1] == "wait" {
			return resolveShellKeyword("wifi wait command", value, []string{"connected"})
		}
		if previous[0] == "standalone" && previous[1] == "run" {
			return resolveShellKeyword("standalone run command", value, []string{"once"})
		}
	}
	return value, nil
}

func helpEntriesForArgsInMode(args []string, mode Mode) []HelpEntry {
	if mode == ModeRequest {
		return requestHelpEntriesForArgs(args)
	}
	if mode == ModeConfigure {
		return configureHelpEntriesForArgs(args)
	}
	if len(args) == 0 {
		return topHelpEntries()
	}
	if requestArgs, ok := operationalRequestArgs(args); ok {
		if args[0] == "request" && len(args) == 1 {
			return append([]HelpEntry{{"<cr>", "Enter request mode"}}, requestCommandHelpEntries()...)
		}
		return requestHelpEntriesForArgs(requestArgs)
	}
	switch args[0] {
	case "show":
		if len(args) == 1 {
			return []HelpEntry{{"devices", "Connected Android agents"}, {"config", "Persistent Agent App configuration"}, {"wifi", "Wi-Fi state and diagnostics"}, {"ip", "IP addressing and routing"}, {"standalone", "Standalone state and stored runs"}, {"adb", "Raw ADB diagnostics from the selected device"}}
		}
		if len(args) == 2 && args[1] == "config" {
			return []HelpEntry{{"standalone", "Standalone configuration subtree"}}
		}
		if len(args) == 2 && args[1] == "wifi" {
			return []HelpEntry{{"status", "Current Wi-Fi connection and IP state"}, {"diagnostics", "Wi-Fi status, capabilities, networks, and scan"}, {"eht", "Connected and nearby EHT state"}, {"scan", "Cached or fresh scan results"}, {"capabilities", "Device Wi-Fi capabilities"}}
		}
		if len(args) == 2 && args[1] == "ip" {
			return []HelpEntry{{"status", "IP addresses, routes, DNS, and validation state"}}
		}
		if len(args) >= 3 && args[1] == "wifi" && args[2] == "scan" {
			return showWifiScanHelpEntries(args[3:])
		}
		if len(args) == 3 && args[1] == "wifi" && args[2] == "eht" {
			return []HelpEntry{
				{"fresh", "Trigger a fresh scan before rendering EHT"},
				{"ssid", "Filter EHT output by SSID"},
				{"bssid", "Filter EHT output by BSSID or affiliated AP MAC"},
			}
		}
		if len(args) == 4 && args[1] == "wifi" && args[2] == "eht" && args[3] == "fresh" {
			return []HelpEntry{
				{"timeout", "Fresh-scan timeout in milliseconds"},
				{"ssid", "Filter EHT output by SSID"},
				{"bssid", "Filter EHT output by BSSID or affiliated AP MAC"},
			}
		}
		if len(args) == 2 && args[1] == "standalone" {
			return []HelpEntry{{"status", "Live standalone runner state"}, {"runs", "Stored run summaries"}, {"run", "Stored run archive"}}
		}
		if len(args) >= 2 && args[1] == "adb" {
			return adbHelpEntries(args[2:])
		}
	case "clear":
		if len(args) == 1 {
			return []HelpEntry{{"standalone", "Remove stored standalone run archives"}}
		}
		if len(args) == 2 && args[1] == "standalone" {
			return []HelpEntry{{"runs", "Remove stored standalone run archives"}}
		}
		if len(args) == 3 && args[1] == "standalone" && args[2] == "runs" {
			return []HelpEntry{{"synced", "Remove only synced runs"}, {"all", "Remove all runs"}}
		}
	case "request":
		if len(args) == 1 {
			return append([]HelpEntry{{"<cr>", "Enter request mode"}}, requestCommandHelpEntries()...)
		}
	case "configure":
		if len(args) == 1 {
			return []HelpEntry{{"<cr>", "Enter configure mode"}}
		}
	case "sync":
		if len(args) == 1 {
			return []HelpEntry{{"standalone", "Download stored standalone archives"}}
		}
		if len(args) == 2 && args[1] == "standalone" {
			return []HelpEntry{{"runs", "Download stored standalone archives"}}
		}
		if len(args) >= 3 && args[1] == "standalone" && args[2] == "runs" {
			return []HelpEntry{{"output", "Output directory"}, {"limit", "Maximum runs"}, {"mark-synced", "Acknowledge downloaded runs"}, {"keep-unsynced", "Do not acknowledge downloaded runs"}}
		}
	}
	return nil
}

func configureHelpEntriesForArgs(args []string) []HelpEntry {
	if len(args) == 0 {
		return configureTopHelpEntries()
	}
	if len(args) > 1 && (len(args) != 2 || args[0] != "run" || args[1] != "request") {
		standaloneSet := args[0] == "set" && len(args) >= 2 && args[1] == "standalone"
		if !standaloneSet {
			if _, err := parseShellArgsInMode(args, ModeConfigure); err == nil {
				return nil
			}
		}
	}
	switch args[0] {
	case "show":
		if len(args) == 1 {
			return []HelpEntry{{"standalone", "Standalone configuration subtree"}, {"<cr>", "Show all persistent config"}}
		}
	case "set":
		if len(args) == 1 {
			return []HelpEntry{{"standalone", "Edit persistent standalone settings"}}
		}
		if len(args) >= 2 && args[1] == "standalone" {
			return setStandaloneHelpEntries(args[2:])
		}
	case "delete":
		if len(args) == 1 {
			return []HelpEntry{{"standalone", "Delete persistent standalone settings"}}
		}
		if len(args) >= 2 && args[1] == "standalone" {
			return []HelpEntry{{"upload", "Upload target and management Wi-Fi"}, {"festa", "Named connectivity scenario"}}
		}
	case "run":
		if len(args) == 1 {
			return []HelpEntry{{"show", "Display operational state"}, {"clear", "Remove stored run archives"}, {"sync", "Download standalone archives"}, {"request", "Run one request"}}
		}
		if len(args) >= 2 && args[1] == "request" {
			return requestHelpEntriesForArgs(args[2:])
		}
		return helpEntriesForArgsInMode(args[1:], ModeOperational)
	}
	if _, err := parseShellArgsInMode(args, ModeConfigure); err == nil {
		return nil
	}
	return nil
}

func setStandaloneHelpEntries(args []string) []HelpEntry {
	entries := helpEntriesForCompletionCandidates(setStandaloneCompletionCandidates(args), map[string]string{
		"enabled":           "Start persistent checks",
		"disabled":          "Stop persistent checks",
		"retention":         "Synced-result retention such as 7d",
		"max-size":          "Store budget such as 512m",
		"live":              "Seed shell use targets",
		"watch":             "Load Wi-Fi targets from watch config files",
		"upload":            "Upload target and management Wi-Fi",
		"to":                "Upload endpoint URL",
		"via":               "Upload network selector",
		"wifi":              "Named Wi-Fi target",
		"festa":             "Named connectivity scenario",
		"interval":          "Run interval such as 10m",
		"check":             "Connectivity check to run",
		"test":              "ping, dns, or http",
		"match":             "SSID or BSSID matcher",
		"essid":             "Match by SSID",
		"bssid":             "Match by BSSID",
		"passphrase":        "WPA/WPA3 passphrase",
		"security":          "auto, wpa2, wpa3, or transition",
		"band":              "all, 2.4ghz, 5ghz, 6ghz, or 60ghz",
		"mac-randomization": "auto, none, persistent, or non-persistent",
		"wait":              "Post-connect wait condition",
		"timeout":           "Timeout duration",
		"dns":               "Resolve a DNS name",
		"ping":              "Ping a host",
		"http":              "Check an HTTP status",
		"name":              "DNS name to resolve",
		"type":              "A, AAAA, or ALL",
		"host":              "Host or IP address",
		"count":             "Packet count",
		"size":              "Payload size in bytes",
		"url":               "HTTP or HTTPS URL",
		"expected-status":   "Expected HTTP status code",
		"<name>":            "Name to create or edit",
		"<url>":             "URL value",
		"<path>":            "Path to a watch config file",
		"<essid>":           "SSID value",
		"<bssid>":           "BSSID value",
		"<passphrase>":      "Passphrase value",
		"<domain>":          "DNS name",
		"<host>":            "Host or IP address",
		"<duration>":        "Duration such as 8s or 10m",
		"<bytes>":           "Size in bytes or units such as 512m",
		"<n>":               "Number",
		"<code>":            "HTTP status code",
	})
	terminal := terminalHelpEntriesForArgsInMode(append([]string{"set", "standalone"}, args...), ModeConfigure)
	if len(entries) == 0 {
		return terminal
	}
	return append(entries, terminal...)
}

func helpEntriesForCompletionCandidates(candidates []string, descriptions map[string]string) []HelpEntry {
	entries := make([]HelpEntry, 0, len(candidates))
	for _, candidate := range candidates {
		entries = append(entries, HelpEntry{Token: candidate, Description: descriptions[candidate]})
	}
	return entries
}

func requestHelpEntriesForArgs(args []string) []HelpEntry {
	if len(args) == 0 {
		return requestTopHelpEntries()
	}
	switch args[0] {
	case "wifi":
		if len(args) == 1 {
			return []HelpEntry{{"connect", "Connect to an SSID"}, {"disconnect", "Disconnect Wi-Fi"}, {"forget", "Forget an SSID or network id"}, {"reconnect", "Reconnect Wi-Fi"}, {"wait", "Wait for Wi-Fi state"}, {"assert", "Assert Wi-Fi state"}, {"cycle", "Repeat connect checks"}}
		}
		if len(args) >= 2 {
			return requestWifiHelp(args[1])
		}
	case "standalone":
		if len(args) == 1 {
			return []HelpEntry{{"run", "Run one festa"}}
		}
		if len(args) >= 2 {
			return []HelpEntry{{"once", "Run once"}, {"festa", "Festa name"}, {"save", "Persist the archive"}}
		}
	case "monitor":
		if len(args) == 1 {
			return []HelpEntry{{"wifi", "Stream Wi-Fi state changes for a bounded duration"}}
		}
		if len(args) == 2 && args[1] == "wifi" {
			return []HelpEntry{{"duration", "Duration in milliseconds"}, {"interval", "Sample interval in milliseconds"}}
		}
	case "ping":
		return []HelpEntry{{"count", "Packet count"}, {"size", "Payload size in bytes"}, {"timeout", "Timeout in milliseconds"}, {"<host>", "Host or IP address to ping"}}
	case "traceroute":
		return []HelpEntry{{"max-hops", "Maximum hops"}, {"via", "Required hop for analysis"}, {"size", "Packet size in bytes"}, {"timeout", "Timeout in milliseconds"}, {"<host>", "Destination host or IP address"}}
	case "path-mtu":
		return []HelpEntry{{"min-mtu", "Minimum MTU in bytes"}, {"max-mtu", "Maximum MTU in bytes"}, {"timeout", "Timeout in milliseconds"}, {"<host>", "Destination host or IP address"}}
	case "global-ip":
		return globalIPHelp(args[1:])
	case "dns":
		return dnsHelp(args[1:])
	case "http":
		return httpHelp(args[1:])
	case "download":
		return downloadHelp(args[1:])
	}
	return nil
}

func terminalHelpEntriesInMode(line string, mode Mode) []HelpEntry {
	command, err := parseShellLineInMode(line, mode)
	if err != nil {
		return nil
	}
	return terminalHelpEntriesForCommand(command)
}

func terminalHelpEntriesForArgs(args []string) []HelpEntry {
	return terminalHelpEntriesForArgsInMode(args, ModeOperational)
}

func terminalHelpEntriesForArgsInMode(args []string, mode Mode) []HelpEntry {
	command, err := parseShellArgs(args)
	if mode != ModeOperational {
		command, err = parseShellArgsInMode(args, mode)
	} else if err != nil {
		command, err = parseShellRequestModeArgs(args)
	}
	if err != nil {
		return nil
	}
	return terminalHelpEntriesForCommand(command)
}

func terminalHelpEntriesForCommand(command Command) []HelpEntry {
	entries := []HelpEntry{{"<cr>", "Execute command; no further command keywords"}}
	if commandSupportsPipeHelp(command) {
		entries = append(entries, pipeHelpEntries()...)
	}
	return entries
}

func httpHelp(args []string) []HelpEntry {
	hasURL := false
	used := map[string]bool{}
	for i := 0; i < len(args); i++ {
		key, err := resolveShellKeyword("http option", args[i], []string{"expected-status", "timeout"})
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
	var entries []HelpEntry
	if !used["expected-status"] {
		entries = append(entries, HelpEntry{"expected-status", "Expected HTTP status"})
	}
	if !used["timeout"] {
		entries = append(entries, HelpEntry{"timeout", "Timeout in milliseconds"})
	}
	if !hasURL {
		entries = append(entries, HelpEntry{"<url>", "HTTP or HTTPS URL; https:// is assumed when omitted"})
	}
	if hasURL {
		terminal := terminalHelpEntriesForArgsInMode(append([]string{"http"}, args...), ModeRequest)
		entries = append(entries, terminal...)
	}
	return entries
}

func dnsHelp(args []string) []HelpEntry {
	hasName := false
	used := map[string]bool{}
	for i := 0; i < len(args); i++ {
		key, err := resolveShellKeyword("dns option", args[i], []string{"type", "timeout"})
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
	var entries []HelpEntry
	if !used["type"] {
		entries = append(entries, HelpEntry{"type", "A, AAAA, or ALL"})
	}
	if !used["timeout"] {
		entries = append(entries, HelpEntry{"timeout", "Timeout in milliseconds"})
	}
	if !hasName {
		entries = append(entries, HelpEntry{"<name>", "DNS name to resolve"})
	}
	if hasName {
		terminal := terminalHelpEntriesForArgsInMode(append([]string{"dns"}, args...), ModeRequest)
		entries = append(entries, terminal...)
	}
	return entries
}

func downloadHelp(args []string) []HelpEntry {
	hasURL := false
	usedTimeout := false
	for i := 0; i < len(args); i++ {
		key, err := resolveShellKeyword("download option", args[i], []string{"timeout"})
		if err == nil {
			if key == "timeout" {
				usedTimeout = true
			}
			if i+1 < len(args) {
				i++
			}
			continue
		}
		if !hasURL {
			hasURL = true
		}
	}
	var entries []HelpEntry
	if !usedTimeout {
		entries = append(entries, HelpEntry{"timeout", "Timeout in milliseconds"})
	}
	if !hasURL {
		entries = append(entries, HelpEntry{"<url>", "URL to download"})
	}
	if hasURL {
		terminal := terminalHelpEntriesForArgsInMode(append([]string{"download"}, args...), ModeRequest)
		entries = append(entries, terminal...)
	}
	return entries
}

func globalIPHelp(args []string) []HelpEntry {
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
		if _, err := normalizeIPFamily(args[i]); err == nil {
			hasFamily = true
		}
	}
	var entries []HelpEntry
	if !usedTimeout {
		entries = append(entries, HelpEntry{"timeout", "Per-family timeout in milliseconds"})
	}
	if !hasFamily {
		entries = append(entries,
			HelpEntry{"ipv4", "Check global IPv4 through ifconfig.me"},
			HelpEntry{"ipv6", "Check global IPv6 through ifconfig.me"},
			HelpEntry{"all", "Check both address families"},
		)
	}
	terminal := terminalHelpEntriesForArgs(append([]string{"global-ip"}, args...))
	entries = append(entries, terminal...)
	return entries
}

func commandSupportsPipeHelp(command Command) bool {
	switch command.Kind {
	case shellShowDevices, shellShowConfig, shellAgentCommand, shellADBDiagnostics:
		return true
	default:
		return false
	}
}

func adbHelpEntries(args []string) []HelpEntry {
	if len(args) == 0 {
		return []HelpEntry{
			{"cmd", "Run whitelisted adb shell cmd diagnostics"},
			{"dumpsys", "Run whitelisted adb shell dumpsys diagnostics"},
			{"diagnostics", "Run the full raw diagnostics bundle"},
			{"wifi", "Wi-Fi status or dumpsys shortcuts"},
			{"connectivity", "Connectivity dumpsys shortcuts"},
		}
	}
	switch args[0] {
	case "cmd":
		if len(args) == 1 {
			return []HelpEntry{{"wifi", "Android cmd wifi diagnostics"}}
		}
		if len(args) == 2 && args[1] == "wifi" {
			return []HelpEntry{{"status", "Run adb shell cmd wifi status"}}
		}
	case "dumpsys":
		if len(args) == 1 {
			return []HelpEntry{{"wifi", "Run adb shell dumpsys wifi"}, {"connectivity", "Run adb shell dumpsys connectivity"}}
		}
		if len(args) == 2 && args[1] == "connectivity" {
			return []HelpEntry{{"networks", "Only current networks"}, {"requests", "Only network requests"}, {"diagnostics", "Connectivity --diag measurements"}, {"--diag", "Connectivity --diag measurements"}, {"trafficcontroller", "Traffic controller/BPF maps"}}
		}
	case "diagnostics":
		if len(args) == 1 {
			return []HelpEntry{{"full", "Run cmd wifi status plus Wi-Fi and connectivity dumps"}}
		}
	case "wifi":
		if len(args) == 1 {
			return []HelpEntry{{"status", "Run adb shell cmd wifi status"}, {"dumpsys", "Run adb shell dumpsys wifi"}}
		}
	case "connectivity":
		if len(args) == 1 {
			return []HelpEntry{{"dumpsys", "Run adb shell dumpsys connectivity"}, {"networks", "Only current networks"}, {"requests", "Only network requests"}, {"diagnostics", "Connectivity --diag measurements"}, {"--diag", "Connectivity --diag measurements"}, {"trafficcontroller", "Traffic controller/BPF maps"}}
		}
	}
	if _, err := parseShellArgs(append([]string{"show", "adb"}, args...)); err == nil {
		return terminalHelpEntriesForArgs(append([]string{"show", "adb"}, args...))
	}
	return nil
}

func pipeHelpEntries() []HelpEntry {
	return []HelpEntry{
		{"| display json", "Render JSON output"},
		{"| display set", "Render configuration set commands"},
		{"| match <regex>", "Include matching lines"},
		{"| except <regex>", "Exclude matching lines"},
		{"| count", "Count non-empty output lines"},
		{"| no-more", "Disable paging"},
	}
}

func requestWifiHelp(command string) []HelpEntry {
	switch command {
	case "connect":
		return []HelpEntry{{"passphrase", "WPA/WPA3 passphrase"}, {"security", "auto, wpa2, wpa3, or transition"}, {"bssid", "Target BSSID"}, {"band", "all, 2.4ghz, 5ghz, 6ghz, or 60ghz"}, {"mac-randomization", "auto, none, persistent, or non-persistent"}, {"timeout", "Timeout in milliseconds"}, {"<ssid>", "SSID to connect"}}
	case "disconnect":
		return nil
	case "forget":
		return []HelpEntry{{"<ssid|network_id>", "Network to forget"}}
	case "reconnect":
		return []HelpEntry{{"timeout", "Timeout in milliseconds"}}
	case "wait":
		return []HelpEntry{{"connected", "Wait until connected"}}
	case "assert":
		return []HelpEntry{{"ssid", "Expected SSID"}, {"bssid", "Expected BSSID"}, {"security", "Expected security"}, {"band", "Expected band"}, {"ip", "Require IP address"}, {"validated", "Require validated internet"}, {"timeout", "Timeout in milliseconds"}}
	case "cycle":
		return []HelpEntry{{"passphrase", "WPA/WPA3 passphrase"}, {"security", "auto, wpa2, wpa3, or transition"}, {"count", "Cycle count"}, {"bssid", "Target BSSID"}, {"band", "Target band"}, {"mac-randomization", "MAC randomization mode"}, {"ping", "Ping host after connect"}, {"http", "HTTP URL after connect"}, {"forget", "Forget after each cycle"}, {"pause", "Pause between cycles in milliseconds"}, {"timeout", "Per-connect timeout in milliseconds"}, {"<ssid>", "SSID to connect"}}
	default:
		return nil
	}
}

func topHelpEntries() []HelpEntry {
	return topHelpEntriesInMode(ModeOperational)
}

func topHelpEntriesInMode(mode Mode) []HelpEntry {
	switch mode {
	case ModeRequest:
		return requestTopHelpEntries()
	case ModeConfigure:
		return configureTopHelpEntries()
	}
	entries := []HelpEntry{
		{"show", "Display state or persistent config"},
		{"clear", "Remove stored standalone run archives"},
		{"sync", "Download stored standalone archives"},
		{"configure", "Enter configure mode"},
		{"request", "Enter request mode or prefix one request"},
		{"help", "Show command summary"},
		{"quit", "Exit the shell"},
	}
	return entries
}

func configureTopHelpEntries() []HelpEntry {
	return []HelpEntry{
		{"show", "Display persistent Agent App config"},
		{"set", "Edit persistent Agent App config"},
		{"delete", "Delete persistent standalone config nodes"},
		{"run", "Run an operational command without leaving configure mode"},
		{"exit", "Return to top-level mode"},
		{"quit", "Exit the shell"},
	}
}

func requestTopHelpEntries() []HelpEntry {
	return append(requestCommandHelpEntries(),
		HelpEntry{"exit", "Return to top-level mode"},
		HelpEntry{"quit", "Exit the shell"},
	)
}

func requestCommandHelpEntries() []HelpEntry {
	return []HelpEntry{
		{"wifi", "Run a Wi-Fi operation"},
		{"standalone", "Run standalone measurements"},
		{"monitor", "Run a bounded monitor"},
		{"ping", "Ping from the selected Android agent"},
		{"traceroute", "Traceroute from the selected Android agent"},
		{"path-mtu", "Discover path MTU from the selected Android agent"},
		{"global-ip", "Check global IPv4/IPv6 via ifconfig.me"},
		{"dns", "Resolve a DNS name"},
		{"http", "Check an HTTP status"},
		{"download", "Download a URL"},
	}
}

// CompleteLine returns full-line completion candidates for line.
//
// Returned strings include the unchanged line prefix plus the completed token.
// Placeholder candidates such as "<host>" are included when they are the most
// useful hint for a positional value.
func CompleteLine(line string) []string {
	return completeShellLine(line)
}

// CompleteRequestLine returns full-line completion candidates for a request
// mode line.
func CompleteRequestLine(line string) []string {
	return completeShellLineInMode(line, ModeRequest)
}

// CompleteConfigureLine returns full-line completion candidates for a
// configure-mode line.
func CompleteConfigureLine(line string) []string {
	return completeShellLineInMode(line, ModeConfigure)
}

func completeShellLine(line string) []string {
	return completeShellLineInMode(line, ModeOperational)
}

func completeShellLineInMode(line string, mode Mode) []string {
	if segment, inPipe, err := currentPipelineSegment(line); err != nil {
		return nil
	} else if inPipe {
		return completePipeSegment(line, segment)
	}
	parts, err := splitPipeline(line)
	if err != nil || len(parts) == 0 {
		return nil
	}
	trailingSpace := strings.HasSuffix(line, " ") || line == ""
	args, err := splitArgs(parts[0])
	if err != nil {
		return nil
	}
	prefix := ""
	baseArgs := args
	if !trailingSpace && len(args) > 0 {
		// readline expects suffix completions, but this package returns full
		// lines. Split the token under the cursor so callers can derive either.
		prefix = args[len(args)-1]
		baseArgs = args[:len(args)-1]
	}
	candidates := completionCandidatesForArgsInMode(baseArgs, mode)
	head := line[:len(line)-len(prefix)]
	var out []string
	for _, candidate := range candidates {
		if strings.HasPrefix(candidate, prefix) {
			completed := head + candidate
			if !trailingSpace && candidate == prefix && shouldAppendCompletionSpace(baseArgs, candidate) {
				completed += " "
			}
			out = append(out, completed)
		}
	}
	return out
}

// CompletionHintLine returns inline placeholder hints for line.
//
// It returns an empty string when real token completions are available, allowing
// the caller to show either completions or hints without duplicating noise.
func CompletionHintLine(line string) string {
	return shellCompletionHintLine(line)
}

// RequestCompletionHintLine returns inline placeholder hints for request mode.
func RequestCompletionHintLine(line string) string {
	return shellCompletionHintLineInMode(line, ModeRequest)
}

// ConfigureCompletionHintLine returns inline placeholder hints for configure
// mode.
func ConfigureCompletionHintLine(line string) string {
	return shellCompletionHintLineInMode(line, ModeConfigure)
}

func shellCompletionHintLine(line string) string {
	return shellCompletionHintLineInMode(line, ModeOperational)
}

func shellCompletionHintLineInMode(line string, mode Mode) string {
	lineRunes := []rune(line)
	var hints []string
	realCandidates := 0
	for _, candidate := range completeShellLineInMode(line, mode) {
		candidateRunes := []rune(candidate)
		if !hasRunePrefix(candidateRunes, lineRunes) {
			continue
		}
		completion := string(candidateRunes[len(lineRunes):])
		if isPlaceholderCandidate(completion) {
			hints = append(hints, completion)
			continue
		}
		realCandidates++
	}
	if realCandidates > 0 || len(hints) == 0 {
		return ""
	}
	return strings.Join(hints, "  ")
}

func hasRunePrefix(value []rune, prefix []rune) bool {
	if len(value) < len(prefix) {
		return false
	}
	for i := range prefix {
		if value[i] != prefix[i] {
			return false
		}
	}
	return true
}

func completePipeSegment(line string, segment string) []string {
	trimmed := strings.TrimLeft(segment, " ")
	trailingSpace := strings.HasSuffix(trimmed, " ") || trimmed == ""
	args, err := splitArgs(trimmed)
	if err != nil {
		return nil
	}
	prefix := ""
	baseArgs := args
	if !trailingSpace && len(args) > 0 {
		prefix = args[len(args)-1]
		baseArgs = args[:len(args)-1]
	}
	head := line[:len(line)-len(prefix)]
	var out []string
	for _, candidate := range pipeCompletionCandidates(baseArgs) {
		if strings.HasPrefix(candidate, prefix) {
			completed := head + candidate
			if !trailingSpace && candidate == prefix && shouldAppendPipeCompletionSpace(baseArgs, candidate) {
				completed += " "
			}
			out = append(out, completed)
		}
	}
	return out
}

func currentPipelineSegment(line string) (string, bool, error) {
	quote := rune(0)
	escaped := false
	lastPipe := -1
	for i, r := range line {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			}
			continue
		}
		switch r {
		case '\'', '"':
			quote = r
		case '|':
			lastPipe = i
		}
	}
	if escaped {
		return "", false, fmt.Errorf("trailing escape")
	}
	if quote != 0 {
		return "", false, fmt.Errorf("unterminated quote")
	}
	if lastPipe < 0 {
		return "", false, nil
	}
	return line[lastPipe+1:], true, nil
}

func pipeCompletionCandidates(args []string) []string {
	if len(args) == 0 {
		return []string{"display", "match", "except", "count", "no-more"}
	}
	if len(args) > 1 {
		return nil
	}
	switch args[0] {
	case "display":
		return []string{"json", "set"}
	case "match", "except":
		return []string{"<regex>"}
	default:
		return nil
	}
}

func shouldAppendPipeCompletionSpace(baseArgs []string, candidate string) bool {
	if len(baseArgs) != 0 {
		return false
	}
	switch candidate {
	case "display", "match", "except":
		return true
	default:
		return false
	}
}

func pipeHelpEntriesForSegment(segment string) []HelpEntry {
	trimmed := strings.TrimLeft(segment, " ")
	trailingSpace := strings.HasSuffix(trimmed, " ") || trimmed == ""
	args, err := splitArgs(trimmed)
	if err != nil {
		return nil
	}
	baseArgs := args
	if !trailingSpace && len(args) > 0 {
		baseArgs = args[:len(args)-1]
	}
	switch {
	case len(baseArgs) == 0:
		return pipeHelpEntries()
	case len(baseArgs) == 1 && baseArgs[0] == "display":
		return []HelpEntry{
			{"json", "Render JSON output"},
			{"set", "Render configuration set commands"},
		}
	case len(baseArgs) == 1 && (baseArgs[0] == "match" || baseArgs[0] == "except"):
		return []HelpEntry{{"<regex>", "Regular expression"}}
	default:
		return nil
	}
}

func completionCandidatesForArgsInMode(args []string, mode Mode) []string {
	if mode == ModeRequest {
		return requestCompletionCandidatesForArgs(args)
	}
	if mode == ModeConfigure {
		return configureCompletionCandidatesForArgs(args)
	}
	if len(args) == 0 {
		return shellOperationalKeywords
	}
	resolved := append([]string(nil), args...)
	for i, arg := range resolved {
		if value, err := resolveContextKeyword(i, resolved[:i], arg); err == nil {
			resolved[i] = value
		}
	}
	if requestArgs, ok := operationalRequestArgs(resolved); ok {
		if candidates, ok := requestValueCompletionCandidatesForArgs(requestArgs); ok {
			return candidates
		}
	}
	if candidates, ok := valueCompletionCandidatesForArgs(resolved); ok {
		return candidates
	}
	if requestArgs, ok := operationalRequestArgs(resolved); ok {
		return requestCompletionCandidatesForArgs(requestArgs)
	}
	switch len(resolved) {
	case 1:
		switch resolved[0] {
		case "show":
			return []string{"devices", "config", "wifi", "ip", "standalone", "adb"}
		case "clear":
			return []string{"standalone"}
		case "sync":
			return []string{"standalone"}
		case "request":
			return requestCompletionCandidatesForArgs(nil)
		case "configure":
			return nil
		}
	case 2:
		if resolved[0] == "show" && resolved[1] == "config" {
			return []string{"standalone"}
		}
		if resolved[0] == "show" && resolved[1] == "wifi" {
			return []string{"status", "diagnostics", "eht", "scan", "capabilities"}
		}
		if resolved[0] == "show" && resolved[1] == "ip" {
			return []string{"status"}
		}
		if resolved[0] == "show" && resolved[1] == "standalone" {
			return []string{"status", "runs", "run"}
		}
		if resolved[0] == "show" && resolved[1] == "adb" {
			return []string{"cmd", "dumpsys", "diagnostics", "wifi", "connectivity"}
		}
		if resolved[0] == "clear" && resolved[1] == "standalone" {
			return []string{"runs"}
		}
		if resolved[0] == "sync" && resolved[1] == "standalone" {
			return []string{"runs"}
		}
	case 3:
		if resolved[0] == "show" && resolved[1] == "wifi" && resolved[2] == "scan" {
			return []string{"brief", "fresh", "detail", "all", "2.4ghz", "5ghz", "6ghz", "60ghz"}
		}
		if resolved[0] == "show" && resolved[1] == "wifi" && resolved[2] == "eht" {
			return []string{"brief", "fresh", "ssid", "bssid"}
		}
		if resolved[0] == "show" && resolved[1] == "adb" && resolved[2] == "cmd" {
			return []string{"wifi"}
		}
		if resolved[0] == "show" && resolved[1] == "adb" && resolved[2] == "dumpsys" {
			return []string{"wifi", "connectivity"}
		}
		if resolved[0] == "show" && resolved[1] == "adb" && resolved[2] == "diagnostics" {
			return []string{"full"}
		}
		if resolved[0] == "show" && resolved[1] == "adb" && resolved[2] == "wifi" {
			return []string{"status", "dumpsys"}
		}
		if resolved[0] == "show" && resolved[1] == "adb" && resolved[2] == "connectivity" {
			return []string{"dumpsys", "networks", "requests", "diagnostics", "--diag", "trafficcontroller"}
		}
		if resolved[0] == "clear" && resolved[1] == "standalone" && resolved[2] == "runs" {
			return []string{"synced", "all"}
		}
		if resolved[0] == "sync" && resolved[1] == "standalone" && resolved[2] == "runs" {
			return syncStandaloneCompletionCandidates(resolved[3:])
		}
	}
	if len(resolved) >= 1 {
		switch {
		case len(resolved) >= 3 && resolved[0] == "show" && resolved[1] == "wifi" && resolved[2] == "scan":
			return showWifiScanCompletionCandidates(resolved[3:])
		case len(resolved) >= 3 && resolved[0] == "show" && resolved[1] == "wifi" && resolved[2] == "eht":
			return showWifiEHTCompletionCandidates(resolved[3:])
		case len(resolved) == 4 && resolved[0] == "show" && resolved[1] == "adb" && resolved[2] == "cmd" && resolved[3] == "wifi":
			return []string{"status"}
		case len(resolved) == 4 && resolved[0] == "show" && resolved[1] == "adb" && resolved[2] == "dumpsys" && resolved[3] == "connectivity":
			return []string{"networks", "requests", "diagnostics", "--diag", "trafficcontroller"}
		case resolved[0] == "sync" && len(resolved) >= 3 && resolved[1] == "standalone" && resolved[2] == "runs":
			return syncStandaloneCompletionCandidates(resolved[3:])
		}
	}
	return nil
}

func operationalRequestArgs(args []string) ([]string, bool) {
	if len(args) == 0 {
		return nil, false
	}
	if args[0] == "request" {
		return args[1:], true
	}
	return nil, false
}

func configureCompletionCandidatesForArgs(args []string) []string {
	if len(args) == 0 {
		return shellConfigureKeywords
	}
	resolved := append([]string(nil), args...)
	for i, arg := range resolved {
		if value, err := resolveConfigureContextKeyword(i, resolved[:i], arg); err == nil {
			resolved[i] = value
		}
	}
	if candidates, ok := valueCompletionCandidatesForArgs(resolved); ok {
		return candidates
	}
	switch len(resolved) {
	case 1:
		switch resolved[0] {
		case "show":
			return []string{"standalone"}
		case "set":
			return []string{"standalone"}
		case "delete":
			return []string{"standalone"}
		case "run":
			return []string{"show", "clear", "sync", "request"}
		case "exit", "quit":
			return nil
		}
	case 2:
		if resolved[0] == "set" && resolved[1] == "standalone" {
			return []string{"enabled", "disabled", "retention", "max-size", "live", "upload", "festa"}
		}
		if resolved[0] == "delete" && resolved[1] == "standalone" {
			return []string{"upload", "festa"}
		}
		if resolved[0] == "run" {
			if resolved[1] == "request" {
				return requestCompletionCandidatesForArgs(nil)
			}
			return completionCandidatesForArgsInMode(resolved[1:], ModeOperational)
		}
	case 3:
		if resolved[0] == "set" && resolved[1] == "standalone" && resolved[2] == "festa" {
			return []string{"<name>"}
		}
	}
	switch {
	case len(resolved) >= 2 && resolved[0] == "set" && resolved[1] == "standalone":
		return setStandaloneCompletionCandidates(resolved[2:])
	case len(resolved) >= 2 && resolved[0] == "run":
		if resolved[1] == "request" {
			return requestCompletionCandidatesForArgs(resolved[2:])
		}
		return completionCandidatesForArgsInMode(resolved[1:], ModeOperational)
	default:
		return nil
	}
}

func requestCompletionCandidatesForArgs(args []string) []string {
	if len(args) == 0 {
		return shellRequestKeywords
	}
	resolved := append([]string(nil), args...)
	for i, arg := range resolved {
		if value, err := resolveRequestContextKeyword(i, resolved[:i], arg); err == nil {
			resolved[i] = value
		}
	}
	if candidates, ok := requestValueCompletionCandidatesForArgs(resolved); ok {
		return candidates
	}
	switch len(resolved) {
	case 1:
		switch resolved[0] {
		case "wifi":
			return []string{"connect", "disconnect", "forget", "reconnect", "wait", "assert", "cycle"}
		case "standalone":
			return []string{"run"}
		case "monitor":
			return []string{"wifi"}
		}
	case 2:
		if resolved[0] == "wifi" && resolved[1] == "wait" {
			return []string{"connected"}
		}
		if resolved[0] == "standalone" && resolved[1] == "run" {
			return []string{"once"}
		}
		if resolved[0] == "monitor" && resolved[1] == "wifi" {
			return monitorWifiCompletionCandidates(nil)
		}
	}
	lastCommand := resolved[len(resolved)-1]
	switch {
	case resolved[0] == "ping":
		return pingCompletionCandidates(resolved[1:])
	case resolved[0] == "traceroute":
		return tracerouteCompletionCandidates(resolved[1:])
	case resolved[0] == "path-mtu":
		return pathMtuCompletionCandidates(resolved[1:])
	case resolved[0] == "global-ip":
		return globalIPCompletionCandidates(resolved[1:])
	case resolved[0] == "dns":
		return dnsCompletionCandidates(resolved[1:])
	case resolved[0] == "http":
		return httpCompletionCandidates(resolved[1:])
	case resolved[0] == "download":
		return downloadCompletionCandidates(resolved[1:])
	case resolved[0] == "monitor" && len(resolved) >= 2 && resolved[1] == "wifi":
		return monitorWifiCompletionCandidates(resolved[2:])
	case resolved[0] == "wifi" && len(resolved) >= 2:
		return requestWifiCompletionCandidates(resolved[1], resolved[2:], lastCommand)
	case resolved[0] == "standalone" && len(resolved) >= 2:
		return requestStandaloneCompletionCandidates(resolved[1], resolved[2:])
	}
	return nil
}

func shouldAppendCompletionSpace(baseArgs []string, candidate string) bool {
	return candidate != "" && !isTerminalTopCompletion(baseArgs, candidate)
}

func isPlaceholderCandidate(candidate string) bool {
	return strings.HasPrefix(candidate, "<") && strings.HasSuffix(candidate, ">")
}

// IsPlaceholderCandidate reports whether candidate is an inline value
// placeholder such as "<host>".
func IsPlaceholderCandidate(candidate string) bool {
	return isPlaceholderCandidate(candidate)
}

func isTerminalTopCompletion(baseArgs []string, candidate string) bool {
	if len(baseArgs) != 0 {
		return false
	}
	switch candidate {
	case "help", "exit", "quit":
		return true
	default:
		return false
	}
}

type completionOption struct {
	name        string
	placeholder string
	values      []string
	flag        bool
	repeatable  bool
}

type completionArgs struct {
	used        map[string]bool
	positionals []string
	pending     *completionOption
}

func scanCompletionArgs(Kind string, args []string, options []completionOption) completionArgs {
	state := completionArgs{used: map[string]bool{}}
	names := completionOptionNames(options)
	for i := 0; i < len(args); i++ {
		key, err := resolveShellKeyword(Kind, args[i], names)
		if err != nil {
			state.positionals = append(state.positionals, args[i])
			continue
		}
		option := completionOptionByName(options, key)
		state.used[key] = true
		if option.flag {
			continue
		}
		if i+1 >= len(args) {
			state.pending = &option
			return state
		}
		i++
	}
	return state
}

func completionOptionNames(options []completionOption) []string {
	names := make([]string, 0, len(options))
	for _, option := range options {
		names = append(names, option.name)
	}
	return names
}

func completionOptionByName(options []completionOption, name string) completionOption {
	for _, option := range options {
		if option.name == name {
			return option
		}
	}
	return completionOption{name: name}
}

func optionValueCandidates(option completionOption) []string {
	if len(option.values) > 0 {
		return option.values
	}
	if option.placeholder != "" {
		return []string{option.placeholder}
	}
	return nil
}

func optionAndPositionalCandidates(state completionArgs, options []completionOption, positionalLimit int, positionalHints ...string) []string {
	var candidates []string
	for _, option := range options {
		if state.used[option.name] && !option.repeatable {
			continue
		}
		candidates = append(candidates, option.name)
	}
	if len(state.positionals) < positionalLimit {
		candidates = append(candidates, positionalHints...)
	}
	return candidates
}

func valueCompletionCandidatesForArgs(args []string) ([]string, bool) {
	if len(args) == 0 {
		return nil, false
	}
	last := args[len(args)-1]
	switch {
	case len(args) >= 4 && args[0] == "show" && args[1] == "wifi" && args[2] == "scan":
		if args[3] == "fresh" && isResolvedKeyword("show wifi scan fresh option", last, []string{"timeout"}) {
			return []string{"<ms>"}, true
		}
		return nil, false
	case len(args) >= 4 && args[0] == "show" && args[1] == "wifi" && args[2] == "eht":
		if isResolvedKeyword("show wifi eht option", last, []string{"timeout"}) {
			return []string{"<ms>"}, true
		}
		if isResolvedKeyword("show wifi eht option", last, []string{"ssid"}) {
			return []string{"<ssid>"}, true
		}
		if isResolvedKeyword("show wifi eht option", last, []string{"bssid"}) {
			return []string{"<bssid>"}, true
		}
		return nil, false
	case len(args) >= 4 && args[0] == "sync" && args[1] == "standalone" && args[2] == "runs":
		return syncStandaloneValueCompletionCandidates(last)
	case len(args) >= 3 && args[0] == "set" && args[1] == "standalone":
		return setStandaloneValueCompletionCandidates(last)
	default:
		return nil, false
	}
}

func requestWifiValueCompletionCandidates(command string, last string) ([]string, bool) {
	switch command {
	case "connect", "cycle":
		switch {
		case isResolvedKeyword("wifi "+command+" option", last, []string{"security"}):
			return wifiConnectSecurityValues(), true
		case isResolvedKeyword("wifi "+command+" option", last, []string{"band"}):
			return wifiBandValues(), true
		case isResolvedKeyword("wifi "+command+" option", last, []string{"mac-randomization"}):
			return wifiMacRandomizationValues(), true
		case isResolvedKeyword("wifi "+command+" option", last, []string{"passphrase"}):
			return []string{"<passphrase>"}, true
		case isResolvedKeyword("wifi "+command+" option", last, []string{"bssid"}):
			return []string{"<bssid>"}, true
		case isResolvedKeyword("wifi "+command+" option", last, []string{"timeout"}):
			return []string{"<ms>"}, true
		case command == "cycle" && isResolvedKeyword("wifi "+command+" option", last, []string{"pause"}):
			return []string{"<ms>"}, true
		case command == "cycle" && isResolvedKeyword("wifi "+command+" option", last, []string{"count"}):
			return []string{"<n>"}, true
		case command == "cycle" && isResolvedKeyword("wifi "+command+" option", last, []string{"ping"}):
			return []string{"<host>"}, true
		case command == "cycle" && isResolvedKeyword("wifi "+command+" option", last, []string{"http"}):
			return []string{"<url>"}, true
		default:
			return nil, false
		}
	case "reconnect":
		if isResolvedKeyword("wifi reconnect option", last, []string{"timeout"}) {
			return []string{"<ms>"}, true
		}
		return nil, false
	case "wait", "assert":
		switch {
		case isResolvedKeyword("wifi expectation option", last, []string{"security"}):
			return wifiSecurityValues(), true
		case isResolvedKeyword("wifi expectation option", last, []string{"band"}):
			return wifiBandValues(), true
		case isResolvedKeyword("wifi expectation option", last, []string{"ssid"}):
			return []string{"<ssid>"}, true
		case isResolvedKeyword("wifi expectation option", last, []string{"bssid"}):
			return []string{"<bssid>"}, true
		case isResolvedKeyword("wifi expectation option", last, []string{"timeout"}):
			return []string{"<ms>"}, true
		default:
			return nil, false
		}
	default:
		return nil, false
	}
}

func globalIPValueCompletionCandidates(last string) ([]string, bool) {
	switch {
	case isResolvedKeyword("global-ip option", last, []string{"family"}):
		return ipFamilyValues(), true
	case isResolvedKeyword("global-ip option", last, []string{"timeout"}):
		return []string{"<ms>"}, true
	default:
		return nil, false
	}
}

func dnsValueCompletionCandidates(last string) ([]string, bool) {
	switch {
	case isResolvedKeyword("dns option", last, []string{"type"}):
		return dnsTypeValues(), true
	case isResolvedKeyword("dns option", last, []string{"timeout"}):
		return []string{"<ms>"}, true
	default:
		return nil, false
	}
}

func httpValueCompletionCandidates(last string) ([]string, bool) {
	switch {
	case isResolvedKeyword("http option", last, []string{"expected-status"}):
		return []string{"<code>"}, true
	case isResolvedKeyword("http option", last, []string{"timeout"}):
		return []string{"<ms>"}, true
	default:
		return nil, false
	}
}

func downloadValueCompletionCandidates(last string) ([]string, bool) {
	if isResolvedKeyword("download option", last, []string{"timeout"}) {
		return []string{"<ms>"}, true
	}
	return nil, false
}

func isResolvedKeyword(Kind string, value string, candidates []string) bool {
	_, err := resolveShellKeyword(Kind, value, candidates)
	return err == nil
}

func wifiSecurityValues() []string {
	return []string{"wpa2", "wpa3", "transition"}
}

func wifiConnectSecurityValues() []string {
	return append([]string{"auto"}, wifiSecurityValues()...)
}

func wifiMacRandomizationValues() []string {
	return []string{"auto", "none", "persistent", "non-persistent"}
}

func ipFamilyValues() []string {
	return []string{"ipv4", "ipv6", "all"}
}

func dnsTypeValues() []string {
	return []string{"A", "AAAA", "ALL"}
}

func showWifiScanCompletionCandidates(args []string) []string {
	if len(args) == 0 {
		return append([]string{"brief", "fresh", "detail"}, wifiBandValues()...)
	}
	first, err := resolveShellKeyword("show wifi scan argument", args[0], append([]string{"brief", "fresh", "detail", "mlo"}, wifiBandValues()...))
	if err != nil {
		return nil
	}
	switch first {
	case "fresh":
		return showWifiScanFreshCompletionCandidates(args[1:])
	case "detail":
		switch {
		case len(args) == 1:
			return append(wifiBandValues(), "<ssid|bssid>")
		case len(args) == 2 && isResolvedKeyword("wifi band", args[1], wifiBandValues()):
			return []string{"<ssid|bssid>"}
		case len(args) == 2:
			return wifiBandValues()
		default:
			return nil
		}
	default:
		briefUsed := false
		bandUsed := false
		mloUsed := false
		for _, arg := range args {
			if key, err := resolveShellKeyword("show wifi scan option", arg, []string{"brief", "mlo"}); err == nil {
				if key == "brief" {
					briefUsed = true
				}
				if key == "mlo" {
					mloUsed = true
				}
				continue
			}
			if _, err := resolveShellKeyword("wifi band", arg, wifiBandValues()); err == nil {
				bandUsed = true
			}
		}
		var candidates []string
		if !briefUsed {
			candidates = append(candidates, "brief")
		}
		if briefUsed && !mloUsed {
			candidates = append(candidates, "mlo")
		}
		if !bandUsed {
			candidates = append(candidates, wifiBandValues()...)
		}
		return candidates
	}
}

func showWifiScanHelpEntries(args []string) []HelpEntry {
	entries := helpEntriesForCompletionCandidates(showWifiScanCompletionCandidates(args), map[string]string{
		"brief":        "Hide scan detail sections; add mlo to show only 11be MLO rows with inline affiliated links",
		"fresh":        "Trigger a fresh scan",
		"detail":       "Show detail for an SSID or BSSID",
		"mlo":          "Only with brief: show 11be MLO scan rows and affiliated links inline",
		"timeout":      "Fresh-scan timeout in milliseconds",
		"all":          "All bands",
		"2.4ghz":       "2.4 GHz band",
		"5ghz":         "5 GHz band",
		"6ghz":         "6 GHz band",
		"60ghz":        "60 GHz band",
		"<ssid|bssid>": "SSID or BSSID target",
	})
	terminal := terminalHelpEntriesForArgsInMode(append([]string{"show", "wifi", "scan"}, args...), ModeOperational)
	if len(entries) == 0 {
		return terminal
	}
	return append(entries, terminal...)
}

func showWifiEHTCompletionCandidates(args []string) []string {
	if len(args) == 0 {
		return []string{"fresh", "ssid", "bssid"}
	}
	used := map[string]bool{}
	timeoutUsed := false
	freshUsed := false
	for i := 0; i < len(args); i++ {
		if key, err := resolveShellKeyword("show wifi eht option", args[i], []string{"fresh", "timeout", "ssid", "bssid"}); err == nil {
			used[key] = true
			if key == "fresh" {
				freshUsed = true
				continue
			}
			timeoutUsed = key == "timeout"
			if i+1 < len(args) {
				i++
			}
			continue
		}
		timeoutUsed = true
	}
	out := []string{}
	if !freshUsed {
		out = append(out, "fresh")
	}
	if freshUsed && !timeoutUsed && !used["timeout"] {
		out = append(out, "timeout")
	}
	if !used["ssid"] && !used["bssid"] {
		out = append(out, "ssid", "bssid")
	}
	return out
}

func showWifiScanFreshCompletionCandidates(args []string) []string {
	timeoutUsed := false
	bandUsed := false
	briefUsed := false
	mloUsed := false
	for i := 0; i < len(args); i++ {
		if key, err := resolveShellKeyword("show wifi scan fresh option", args[i], []string{"brief", "mlo", "timeout"}); err == nil {
			if key == "brief" {
				briefUsed = true
				continue
			}
			if key == "mlo" {
				mloUsed = true
				continue
			}
			timeoutUsed = key == "timeout"
			if i+1 < len(args) {
				i++
			}
			continue
		}
		if _, err := resolveShellKeyword("wifi band", args[i], wifiBandValues()); err == nil {
			bandUsed = true
		}
	}
	var candidates []string
	if !briefUsed {
		candidates = append(candidates, "brief")
	}
	if briefUsed && !mloUsed {
		candidates = append(candidates, "mlo")
	}
	if !timeoutUsed {
		candidates = append(candidates, "timeout")
	}
	if !bandUsed {
		candidates = append(candidates, wifiBandValues()...)
	}
	return candidates
}

func monitorWifiCompletionCandidates(args []string) []string {
	options := []completionOption{
		{name: "duration", placeholder: "<ms>"},
		{name: "interval", placeholder: "<ms>"},
	}
	state := scanCompletionArgs("monitor wifi option", args, options)
	if state.pending != nil {
		return optionValueCandidates(*state.pending)
	}
	return optionAndPositionalCandidates(state, options, 0)
}

func pingCompletionCandidates(args []string) []string {
	options := []completionOption{
		{name: "count", placeholder: "<n>"},
		{name: "size", placeholder: "<bytes>"},
		{name: "timeout", placeholder: "<ms>"},
	}
	state := scanCompletionArgs("ping option", args, options)
	if state.pending != nil {
		return optionValueCandidates(*state.pending)
	}
	return optionAndPositionalCandidates(state, options, 1, "<host>")
}

func tracerouteCompletionCandidates(args []string) []string {
	options := []completionOption{
		{name: "max-hops", placeholder: "<n>"},
		{name: "via", placeholder: "<host_or_ip>", repeatable: true},
		{name: "size", placeholder: "<bytes>"},
		{name: "timeout", placeholder: "<ms>"},
	}
	state := scanCompletionArgs("traceroute option", args, options)
	if state.pending != nil {
		return optionValueCandidates(*state.pending)
	}
	return optionAndPositionalCandidates(state, options, 1, "<host>")
}

func pathMtuCompletionCandidates(args []string) []string {
	options := []completionOption{
		{name: "min-mtu", placeholder: "<bytes>"},
		{name: "max-mtu", placeholder: "<bytes>"},
		{name: "timeout", placeholder: "<ms>"},
	}
	state := scanCompletionArgs("path-mtu option", args, options)
	if state.pending != nil {
		return optionValueCandidates(*state.pending)
	}
	return optionAndPositionalCandidates(state, options, 1, "<host>")
}

func requestWifiCompletionCandidates(command string, args []string, last string) []string {
	_ = last
	switch command {
	case "connect":
		return wifiConnectCompletionCandidates(args, false)
	case "reconnect":
		options := []completionOption{{name: "timeout", placeholder: "<ms>"}}
		state := scanCompletionArgs("wifi reconnect option", args, options)
		if state.pending != nil {
			return optionValueCandidates(*state.pending)
		}
		return optionAndPositionalCandidates(state, options, 0)
	case "assert":
		return wifiExpectationCompletionCandidates(args, false)
	case "cycle":
		return wifiConnectCompletionCandidates(args, true)
	case "wait":
		if len(args) == 0 {
			return []string{"connected"}
		}
		return wifiExpectationCompletionCandidates(args[1:], true)
	default:
		return nil
	}
}

func wifiConnectCompletionCandidates(args []string, cycle bool) []string {
	options := []completionOption{
		{name: "passphrase", placeholder: "<passphrase>"},
		{name: "security", values: wifiConnectSecurityValues()},
		{name: "bssid", placeholder: "<bssid>"},
		{name: "band", values: wifiBandValues()},
		{name: "mac-randomization", values: wifiMacRandomizationValues()},
		{name: "timeout", placeholder: "<ms>"},
	}
	if cycle {
		options = append(options,
			completionOption{name: "count", placeholder: "<n>"},
			completionOption{name: "ping", placeholder: "<host>"},
			completionOption{name: "http", placeholder: "<url>"},
			completionOption{name: "forget", flag: true},
			completionOption{name: "pause", placeholder: "<ms>"},
		)
	}
	state := scanCompletionArgs("wifi option", args, options)
	if state.pending != nil {
		return optionValueCandidates(*state.pending)
	}
	return optionAndPositionalCandidates(state, options, 1, "<ssid>")
}

func wifiExpectationCompletionCandidates(args []string, allowPositionalSSID bool) []string {
	options := []completionOption{
		{name: "ssid", placeholder: "<ssid>"},
		{name: "bssid", placeholder: "<bssid>"},
		{name: "security", values: wifiSecurityValues()},
		{name: "band", values: wifiBandValues()},
		{name: "ip", flag: true},
		{name: "validated", flag: true},
		{name: "timeout", placeholder: "<ms>"},
	}
	state := scanCompletionArgs("wifi expectation option", args, options)
	if state.pending != nil {
		return optionValueCandidates(*state.pending)
	}
	positionalLimit := 0
	var positionalHints []string
	if allowPositionalSSID {
		positionalLimit = 1
		positionalHints = []string{"<ssid>"}
	}
	return optionAndPositionalCandidates(state, options, positionalLimit, positionalHints...)
}

func setStandaloneCompletionCandidates(args []string) []string {
	if len(args) == 0 {
		return []string{"enabled", "disabled", "retention", "max-size", "live", "upload", "festa"}
	}
	switch {
	case args[0] == "live" && len(args) == 1:
		return []string{"watch"}
	case args[0] == "live" && len(args) >= 2 && args[1] == "watch":
		return []string{"<path>"}
	case args[0] == "upload" && len(args) == 1:
		return []string{"to", "via"}
	case args[0] == "upload" && len(args) == 2 && args[1] == "to":
		return []string{"<url>"}
	case args[0] == "upload" && len(args) == 2 && args[1] == "via":
		return []string{"wifi"}
	case args[0] == "upload" && len(args) == 3 && args[1] == "via" && args[2] == "wifi":
		return []string{"essid"}
	case args[0] == "upload" && len(args) >= 3 && args[1] == "via" && args[2] == "wifi":
		options := []completionOption{
			{name: "essid", placeholder: "<essid>"},
			{name: "passphrase", placeholder: "<passphrase>"},
			{name: "security", placeholder: "<auto|wpa2|wpa3|transition>"},
			{name: "bssid", placeholder: "<bssid>"},
			{name: "band", placeholder: "<all|2.4ghz|5ghz|6ghz|60ghz>"},
			{name: "mac-randomization", placeholder: "<auto|none|persistent|non-persistent>"},
			{name: "timeout", placeholder: "<duration>"},
		}
		state := scanCompletionArgs("set standalone upload via wifi option", args[3:], options)
		if state.pending != nil {
			return optionValueCandidates(*state.pending)
		}
		return optionAndPositionalCandidates(state, options, 0)
	case args[0] == "festa":
		return setStandaloneFestaCompletionCandidates(args[1:])
	}
	options := []completionOption{
		{name: "enabled", flag: true},
		{name: "disabled", flag: true},
		{name: "retention", placeholder: "<duration>"},
		{name: "max-size", placeholder: "<bytes>"},
	}
	state := scanCompletionArgs("set standalone option", args, options)
	if state.pending != nil {
		return optionValueCandidates(*state.pending)
	}
	return optionAndPositionalCandidates(state, options, 1, "live", "upload", "festa")
}

func setStandaloneFestaCompletionCandidates(args []string) []string {
	if len(args) == 0 {
		return []string{"<name>"}
	}
	if len(args) == 1 {
		return []string{"enabled", "disabled", "interval", "wifi", "check"}
	}
	switch args[1] {
	case "wifi":
		return setStandaloneFestaWifiCompletionCandidates(args[2:])
	case "check":
		return setStandaloneFestaCheckCompletionCandidates(args[2:])
	default:
		return nil
	}
}

func setStandaloneFestaWifiCompletionCandidates(args []string) []string {
	if len(args) == 0 {
		return []string{"<name>"}
	}
	if len(args) == 1 {
		return []string{"match", "passphrase", "band", "wait", "timeout"}
	}
	switch args[1] {
	case "match":
		if len(args) == 2 {
			return []string{"essid", "bssid"}
		}
		if len(args) == 3 {
			return []string{"<value>"}
		}
		return setStandaloneKeyedCompletionCandidates(
			"set standalone festa wifi match option",
			args[4:],
			[]completionOption{{name: "mac-randomization", values: wifiMacRandomizationValues()}},
		)
	case "passphrase":
		if len(args) == 2 {
			return []string{"<passphrase>"}
		}
		return setStandaloneKeyedCompletionCandidates(
			"set standalone festa wifi passphrase option",
			args[3:],
			[]completionOption{{name: "security", values: wifiConnectSecurityValues()}},
		)
	case "band":
		if len(args) == 2 {
			return wifiBandValues()
		}
	case "wait":
		if len(args) == 2 {
			return []string{"ip", "validated"}
		}
	case "timeout":
		if len(args) == 2 {
			return []string{"<duration>"}
		}
	}
	return nil
}

func setStandaloneFestaCheckCompletionCandidates(args []string) []string {
	if len(args) == 0 {
		return []string{"<name>"}
	}
	if len(args) == 1 {
		return []string{"test"}
	}
	if args[1] != "test" {
		return nil
	}
	if len(args) == 2 {
		return []string{"ping", "dns", "http"}
	}
	switch args[2] {
	case "dns":
		return setStandaloneKeyedCompletionCandidates(
			"set standalone festa named check option",
			args[3:],
			[]completionOption{
				{name: "name", placeholder: "<domain>"},
				{name: "type", values: dnsTypeValues()},
				{name: "timeout", placeholder: "<duration>"},
			},
		)
	case "ping":
		return setStandaloneKeyedCompletionCandidates(
			"set standalone festa named check option",
			args[3:],
			[]completionOption{
				{name: "host", placeholder: "<host>"},
				{name: "count", placeholder: "<n>"},
				{name: "size", placeholder: "<bytes>"},
				{name: "timeout", placeholder: "<duration>"},
			},
		)
	case "http":
		return setStandaloneKeyedCompletionCandidates(
			"set standalone festa named check option",
			args[3:],
			[]completionOption{
				{name: "url", placeholder: "<url>"},
				{name: "expected-status", placeholder: "<code>"},
				{name: "timeout", placeholder: "<duration>"},
			},
		)
	default:
		return nil
	}
}

func setStandaloneKeyedCompletionCandidates(kind string, args []string, options []completionOption) []string {
	state := scanCompletionArgs(kind, args, options)
	if state.pending != nil {
		return optionValueCandidates(*state.pending)
	}
	return optionAndPositionalCandidates(state, options, 0)
}

func setStandaloneValueCompletionCandidates(last string) ([]string, bool) {
	switch {
	case isResolvedKeyword("set standalone option", last, []string{"interval", "retention", "timeout"}):
		return []string{"<duration>"}, true
	case isResolvedKeyword("set standalone option", last, []string{"max-size"}):
		return []string{"<bytes>"}, true
	case isResolvedKeyword("set standalone option", last, []string{"name"}):
		return []string{"<domain>"}, true
	case isResolvedKeyword("set standalone option", last, []string{"essid"}):
		return []string{"<essid>"}, true
	case isResolvedKeyword("set standalone option", last, []string{"bssid"}):
		return []string{"<bssid>"}, true
	case isResolvedKeyword("set standalone option", last, []string{"passphrase"}):
		return []string{"<passphrase>"}, true
	case isResolvedKeyword("set standalone option", last, []string{"security"}):
		return wifiConnectSecurityValues(), true
	case isResolvedKeyword("set standalone option", last, []string{"band"}):
		return wifiBandValues(), true
	case isResolvedKeyword("set standalone option", last, []string{"type"}):
		return dnsTypeValues(), true
	case isResolvedKeyword("set standalone option", last, []string{"mac-randomization"}):
		return wifiMacRandomizationValues(), true
	case isResolvedKeyword("set standalone option", last, []string{"wait"}):
		return []string{"ip", "validated"}, true
	case isResolvedKeyword("set standalone option", last, []string{"host"}):
		return []string{"<host>"}, true
	case isResolvedKeyword("set standalone option", last, []string{"url"}):
		return []string{"<url>"}, true
	case isResolvedKeyword("set standalone option", last, []string{"to"}):
		return []string{"<url>"}, true
	case isResolvedKeyword("set standalone option", last, []string{"watch"}):
		return []string{"<path>"}, true
	case isResolvedKeyword("set standalone option", last, []string{"count"}):
		return []string{"<n>"}, true
	case isResolvedKeyword("set standalone option", last, []string{"expected-status"}):
		return []string{"<code>"}, true
	case isResolvedKeyword("set standalone option", last, []string{"size"}):
		return []string{"<bytes>"}, true
	default:
		return nil, false
	}
}

func requestValueCompletionCandidatesForArgs(args []string) ([]string, bool) {
	if len(args) == 0 {
		return nil, false
	}
	last := args[len(args)-1]
	switch {
	case len(args) >= 3 && args[0] == "wifi":
		return requestWifiValueCompletionCandidates(args[1], last)
	case len(args) >= 3 && args[0] == "standalone":
		return requestStandaloneValueCompletionCandidates(args[1], last)
	case len(args) >= 3 && args[0] == "monitor" && args[1] == "wifi":
		if isResolvedKeyword("monitor wifi option", last, []string{"duration", "interval"}) {
			return []string{"<ms>"}, true
		}
		return nil, false
	case args[0] == "ping":
		switch {
		case isResolvedKeyword("ping option", last, []string{"count"}):
			return []string{"<n>"}, true
		case isResolvedKeyword("ping option", last, []string{"size"}):
			return []string{"<bytes>"}, true
		case isResolvedKeyword("ping option", last, []string{"timeout"}):
			return []string{"<ms>"}, true
		}
	case args[0] == "traceroute":
		switch {
		case isResolvedKeyword("traceroute option", last, []string{"max-hops"}):
			return []string{"<n>"}, true
		case isResolvedKeyword("traceroute option", last, []string{"via"}):
			return []string{"<host_or_ip>"}, true
		case isResolvedKeyword("traceroute option", last, []string{"size"}):
			return []string{"<bytes>"}, true
		case isResolvedKeyword("traceroute option", last, []string{"timeout"}):
			return []string{"<ms>"}, true
		}
	case args[0] == "path-mtu":
		switch {
		case isResolvedKeyword("path-mtu option", last, []string{"min-mtu", "max-mtu"}):
			return []string{"<bytes>"}, true
		case isResolvedKeyword("path-mtu option", last, []string{"timeout"}):
			return []string{"<ms>"}, true
		}
	case args[0] == "global-ip":
		return globalIPValueCompletionCandidates(last)
	case args[0] == "dns":
		return dnsValueCompletionCandidates(last)
	case args[0] == "http":
		return httpValueCompletionCandidates(last)
	case args[0] == "download":
		return downloadValueCompletionCandidates(last)
	}
	return nil, false
}

func requestStandaloneCompletionCandidates(command string, args []string) []string {
	switch command {
	case "run":
		if len(args) == 0 {
			return []string{"once"}
		}
		if args[0] != "once" {
			return nil
		}
		options := []completionOption{
			{name: "festa", placeholder: "<name>"},
			{name: "save", flag: true},
		}
		state := scanCompletionArgs("standalone run once option", args[1:], options)
		if state.pending != nil {
			return optionValueCandidates(*state.pending)
		}
		return optionAndPositionalCandidates(state, options, 0)
	default:
		return nil
	}
}

func requestStandaloneValueCompletionCandidates(command string, last string) ([]string, bool) {
	switch command {
	case "run":
		if isResolvedKeyword("standalone run once option", last, []string{"festa"}) {
			return []string{"<name>"}, true
		}
	}
	return nil, false
}

func syncStandaloneCompletionCandidates(args []string) []string {
	options := []completionOption{
		{name: "output", placeholder: "<dir>"},
		{name: "limit", placeholder: "<n>"},
		{name: "mark-synced", flag: true},
		{name: "keep-unsynced", flag: true},
	}
	state := scanCompletionArgs("sync standalone runs option", args, options)
	if state.pending != nil {
		return optionValueCandidates(*state.pending)
	}
	return optionAndPositionalCandidates(state, options, 0)
}

func syncStandaloneValueCompletionCandidates(last string) ([]string, bool) {
	switch {
	case isResolvedKeyword("sync standalone runs option", last, []string{"output"}):
		return []string{"<dir>"}, true
	case isResolvedKeyword("sync standalone runs option", last, []string{"limit"}):
		return []string{"<n>"}, true
	default:
		return nil, false
	}
}

func dnsCompletionCandidates(args []string) []string {
	options := []completionOption{
		{name: "type", values: dnsTypeValues()},
		{name: "timeout", placeholder: "<ms>"},
	}
	state := scanCompletionArgs("dns option", args, options)
	if state.pending != nil {
		return optionValueCandidates(*state.pending)
	}
	return optionAndPositionalCandidates(state, options, 1, "<name>")
}

func httpCompletionCandidates(args []string) []string {
	options := []completionOption{
		{name: "expected-status", placeholder: "<code>"},
		{name: "timeout", placeholder: "<ms>"},
	}
	state := scanCompletionArgs("http option", args, options)
	if state.pending != nil {
		return optionValueCandidates(*state.pending)
	}
	return optionAndPositionalCandidates(state, options, 1, "<url>")
}

func downloadCompletionCandidates(args []string) []string {
	options := []completionOption{{name: "timeout", placeholder: "<ms>"}}
	state := scanCompletionArgs("download option", args, options)
	if state.pending != nil {
		return optionValueCandidates(*state.pending)
	}
	return optionAndPositionalCandidates(state, options, 1, "<url>")
}

func globalIPCompletionCandidates(args []string) []string {
	hasFamily := false
	usedTimeout := false
	for i := 0; i < len(args); i++ {
		if key, err := resolveShellKeyword("global-ip option", args[i], []string{"family", "timeout"}); err == nil {
			if key == "family" {
				if i+1 < len(args) {
					hasFamily = true
					i++
				}
				continue
			}
			usedTimeout = true
			if i+1 < len(args) {
				i++
			}
			continue
		}
		if _, err := normalizeIPFamily(args[i]); err == nil {
			hasFamily = true
		}
	}
	var candidates []string
	if !usedTimeout {
		candidates = append(candidates, "timeout")
	}
	if !hasFamily {
		candidates = append(candidates, ipFamilyValues()...)
	}
	return candidates
}
