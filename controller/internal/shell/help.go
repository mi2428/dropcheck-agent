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

// WriteHelp writes the full shell help summary to w.
func WriteHelp(w io.Writer) {
	writeShellHelp(w)
}

func writeShellHelp(w io.Writer) {
	fmt.Fprintln(w, `commands:
  show devices
  show target
  set target <agent_id|adb_serial|number|all>
  set controller endpoint <host:port> enabled [min-backoff <duration>] [max-backoff <duration>]
  set controller endpoint disabled
  set festival standalone enabled plan <file> [interval <duration>] [retention <duration>] [max-size <bytes>]
  set festival standalone disabled
  clear target
  show controller endpoint
  show controller link
  show wifi status
  show wifi diagnostics
  show wifi scan [all|2.4ghz|5ghz|6ghz|60ghz]
  show wifi scan fresh [timeout <ms>] [all|2.4ghz|5ghz|6ghz|60ghz]
  show wifi scan detail [all|2.4ghz|5ghz|6ghz|60ghz] <ssid|bssid>
  show wifi capabilities
  show festival standalone status
  show festival standalone config
  show festival runs [limit <n>] [synced]
  show festival run <run-id>
  request wifi connect passphrase <passphrase> [security <auto|wpa2|wpa3|transition>] [bssid <bssid>] [band <band>] [mac-randomization <mode>] [timeout <ms>] <ssid>
  request wifi disconnect
  request wifi forget <ssid|network_id>
  request wifi reconnect [timeout <ms>]
  request wifi wait connected [bssid <bssid>] [security <mode>] [band <band>] [ip] [validated] [timeout <ms>] [ssid]
  request wifi assert [ssid <ssid>] [bssid <bssid>] [security <mode>] [band <band>] [ip] [validated] [timeout <ms>]
  request wifi cycle passphrase <passphrase> [security <auto|wpa2|wpa3|transition>] [count <n>] [bssid <bssid>] [band <band>] [mac-randomization <mode>] [ping <host>] [http <url>] [forget] [pause <ms>] [timeout <ms>] <ssid>
  request festival run once plan <file> [save]
  request festival sync [output <dir>] [limit <n>] [mark-synced|keep-unsynced]
  request festival clear [synced|all]
  request controller reconnect
  monitor wifi [duration <ms>] [interval <ms>]
  ping [count <n>] [size <bytes>] [timeout <ms>] <host>
  traceroute [max-hops <n>] [via <host_or_ip>] [size <bytes>] [timeout <ms>] <host>
  path-mtu [min-mtu <bytes>] [max-mtu <bytes>] [timeout <ms>] <host>
  global-ip [timeout <ms>] [ipv4|ipv6|all]
  test dns [type A|AAAA|ALL] [timeout <ms>] <name>
  test http [expected-status <code>] [timeout <ms>] <url>
  test download [timeout <ms>] <url>
  quit

pipes:
  | display json
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

// IsHelpToken reports whether value is a standalone help token.
func IsHelpToken(value string) bool {
	return isShellHelpToken(value)
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

// HasHelpSuffix reports whether value ends with a supported help suffix.
func HasHelpSuffix(value string) bool {
	return hasShellHelpSuffix(value)
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

func writeShellContextHelp(w io.Writer, line string) {
	entries := shellHelpEntries(line)
	if len(entries) == 0 {
		writeShellHelp(w)
		return
	}
	for _, entry := range entries {
		if entry.Description == "" {
			fmt.Fprintf(w, "  %-24s\n", entry.Token)
			continue
		}
		fmt.Fprintf(w, "  %-24s %s\n", entry.Token, entry.Description)
	}
}

// HelpEntries returns contextual help candidates for line.
//
// The function accepts the same trailing "?" suffix as the interactive shell,
// resolves unambiguous command prefixes, and falls back to top-level entries
// when the context is unknown.
func HelpEntries(line string) []HelpEntry {
	return shellHelpEntries(line)
}

func shellHelpEntries(line string) []HelpEntry {
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
	// Contextual help should behave like parsing: partial but unambiguous
	// prefixes are resolved before selecting the next set of candidates.
	for i, arg := range args {
		if resolved, err := resolveContextKeyword(i, args[:i], arg); err == nil {
			args[i] = resolved
		}
	}
	if entries := valueHelpEntriesForArgs(args); len(entries) > 0 {
		return entries
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

func valueHelpEntriesForArgs(args []string) []HelpEntry {
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

func resolveContextKeyword(index int, previous []string, value string) (string, error) {
	switch index {
	case 0:
		return resolveShellKeyword("command", value, shellTopKeywords)
	case 1:
		switch previous[0] {
		case "show":
			return resolveShellKeyword("show command", value, []string{"devices", "target", "wifi", "festival", "controller"})
		case "set", "clear":
			if previous[0] == "set" {
				return resolveShellKeyword(previous[0]+" command", value, []string{"target", "festival", "controller"})
			}
			return resolveShellKeyword(previous[0]+" command", value, []string{"target"})
		case "request", "monitor":
			if previous[0] == "request" {
				return resolveShellKeyword(previous[0]+" command", value, []string{"wifi", "festival", "controller"})
			}
			return resolveShellKeyword(previous[0]+" command", value, []string{"wifi"})
		case "test":
			return resolveShellKeyword("test command", value, []string{"dns", "http", "download"})
		}
	case 2:
		if previous[0] == "show" && previous[1] == "wifi" {
			return resolveShellKeyword("show wifi command", value, []string{"status", "diagnostics", "scan", "capabilities"})
		}
		if previous[0] == "show" && previous[1] == "festival" {
			return resolveShellKeyword("show festival command", value, []string{"standalone", "runs", "run"})
		}
		if previous[0] == "show" && previous[1] == "controller" {
			return resolveShellKeyword("show controller command", value, []string{"endpoint", "link"})
		}
		if previous[0] == "set" && previous[1] == "festival" {
			return resolveShellKeyword("set festival command", value, []string{"standalone"})
		}
		if previous[0] == "set" && previous[1] == "controller" {
			return resolveShellKeyword("set controller command", value, []string{"endpoint"})
		}
		if previous[0] == "request" && previous[1] == "wifi" {
			return resolveShellKeyword("request wifi command", value, []string{"connect", "disconnect", "forget", "reconnect", "wait", "assert", "cycle"})
		}
		if previous[0] == "request" && previous[1] == "festival" {
			return resolveShellKeyword("request festival command", value, []string{"run", "sync", "clear"})
		}
		if previous[0] == "request" && previous[1] == "controller" {
			return resolveShellKeyword("request controller command", value, []string{"reconnect"})
		}
	}
	return value, nil
}

func helpEntriesForArgs(args []string) []HelpEntry {
	if len(args) == 0 {
		return topHelpEntries()
	}
	switch args[0] {
	case "show":
		if len(args) == 1 {
			return []HelpEntry{{"devices", "Connected Android agents"}, {"target", "Current command target"}, {"wifi", "Wi-Fi state and diagnostics"}, {"festival", "Dropcheck Festival standalone state and stored runs"}, {"controller", "Controller endpoint and link state"}}
		}
		if len(args) == 2 && args[1] == "wifi" {
			return []HelpEntry{{"status", "Current Wi-Fi connection and IP state"}, {"diagnostics", "Wi-Fi status, capabilities, networks, and scan"}, {"scan", "Cached or fresh scan results"}, {"capabilities", "Device Wi-Fi capabilities"}}
		}
		if len(args) == 3 && args[1] == "wifi" && args[2] == "scan" {
			return []HelpEntry{{"fresh", "Trigger a fresh scan"}, {"detail", "Show detail for an SSID or BSSID"}, {"all", "All bands"}, {"2.4ghz", "2.4 GHz band"}, {"5ghz", "5 GHz band"}, {"6ghz", "6 GHz band"}, {"60ghz", "60 GHz band"}}
		}
		if len(args) == 2 && args[1] == "festival" {
			return []HelpEntry{{"standalone", "Standalone runner status and config"}, {"runs", "Stored measurement run summaries"}, {"run", "Stored measurement run archive"}}
		}
		if len(args) == 3 && args[1] == "festival" && args[2] == "standalone" {
			return []HelpEntry{{"status", "Live standalone runner state"}, {"config", "Persistent standalone configuration"}}
		}
		if len(args) == 2 && args[1] == "controller" {
			return []HelpEntry{{"endpoint", "Persistent controller reconnect endpoint"}, {"link", "Live controller connection state"}}
		}
	case "set":
		if len(args) == 1 {
			return []HelpEntry{{"target", "Select an agent or all agents"}, {"festival", "Change persistent Dropcheck Festival settings"}, {"controller", "Change persistent controller reconnect settings"}}
		}
		if len(args) == 2 && args[1] == "target" {
			return []HelpEntry{{"all", "Send commands to all connected agents"}, {"<agent>", "Agent number, id, or adb serial"}}
		}
		if len(args) == 2 && args[1] == "festival" {
			return []HelpEntry{{"standalone", "Enable or disable standalone measurements"}}
		}
		if len(args) >= 3 && args[1] == "festival" && args[2] == "standalone" {
			return []HelpEntry{{"enabled", "Start persistent measurements"}, {"disabled", "Stop persistent measurements"}, {"plan", "Protojson FestivalPlan file"}, {"interval", "Delay such as 30s or 5m"}, {"retention", "Synced-result retention such as 7d"}, {"max-size", "Store budget such as 512m"}}
		}
		if len(args) == 2 && args[1] == "controller" {
			return []HelpEntry{{"endpoint", "Persistent controller reconnect endpoint"}}
		}
		if len(args) >= 3 && args[1] == "controller" && args[2] == "endpoint" {
			return []HelpEntry{{"<host:port>", "Controller gRPC endpoint reachable by Android"}, {"enabled", "Enable direct TCP reconnect"}, {"disabled", "Disable direct TCP reconnect"}, {"min-backoff", "First retry delay"}, {"max-backoff", "Maximum retry delay"}}
		}
	case "clear":
		if len(args) == 1 {
			return []HelpEntry{{"target", "Clear all-target mode and return to the first agent"}}
		}
	case "request":
		if len(args) == 1 {
			return []HelpEntry{{"wifi", "Run a Wi-Fi operation"}, {"festival", "Run, sync, or clear Dropcheck Festival measurements"}, {"controller", "Run one-shot controller link operations"}}
		}
		if len(args) == 2 && args[1] == "wifi" {
			return []HelpEntry{{"connect", "Connect to an SSID"}, {"disconnect", "Disconnect Wi-Fi"}, {"forget", "Forget an SSID or network id"}, {"reconnect", "Reconnect Wi-Fi"}, {"wait", "Wait for Wi-Fi state"}, {"assert", "Assert Wi-Fi state"}, {"cycle", "Repeat connect checks"}}
		}
		if len(args) >= 3 && args[1] == "wifi" {
			return requestWifiHelp(args[2])
		}
		if len(args) == 2 && args[1] == "festival" {
			return []HelpEntry{{"run", "Run one measurement plan"}, {"sync", "Download stored measurement archives"}, {"clear", "Remove stored measurement archives"}}
		}
		if len(args) >= 3 && args[1] == "festival" {
			return []HelpEntry{{"once", "Run once"}, {"plan", "Protojson FestivalPlan file"}, {"output", "Output directory"}, {"limit", "Maximum runs"}, {"mark-synced", "Acknowledge downloaded runs"}, {"keep-unsynced", "Do not acknowledge downloaded runs"}, {"synced", "Clear synced runs"}, {"all", "Clear all runs"}}
		}
		if len(args) == 2 && args[1] == "controller" {
			return []HelpEntry{{"reconnect", "Close this stream and reconnect through the stored endpoint"}}
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
	case "test":
		if len(args) == 1 {
			return []HelpEntry{{"dns", "Resolve a DNS name"}, {"http", "Check an HTTP status"}, {"download", "Download a URL"}}
		}
		switch args[1] {
		case "dns":
			return testDNSHelp(args[2:])
		case "http":
			return testHTTPHelp(args[2:])
		case "download":
			return []HelpEntry{{"timeout", "Timeout in milliseconds"}, {"<url>", "URL to download"}}
		}
	}
	return nil
}

func terminalHelpEntries(line string) []HelpEntry {
	command, err := parseShellLine(line)
	if err != nil {
		return nil
	}
	return terminalHelpEntriesForCommand(command)
}

func terminalHelpEntriesForArgs(args []string) []HelpEntry {
	command, err := parseShellArgs(args)
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

func testHTTPHelp(args []string) []HelpEntry {
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
		terminal := terminalHelpEntriesForArgs(append([]string{"test", "http"}, args...))
		entries = append(entries, terminal...)
	}
	return entries
}

func testDNSHelp(args []string) []HelpEntry {
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
		terminal := terminalHelpEntriesForArgs(append([]string{"test", "dns"}, args...))
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
		if _, err := normalizeIpFamily(args[i]); err == nil {
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
	case shellShowDevices, shellShowTarget, shellClearTarget, shellAgentCommand:
		return true
	default:
		return false
	}
}

func pipeHelpEntries() []HelpEntry {
	return []HelpEntry{
		{"| display json", "Render JSON output"},
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
	return []HelpEntry{
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

// CompleteLine returns full-line completion candidates for line.
//
// Returned strings include the unchanged line prefix plus the completed token.
// Placeholder candidates such as "<host>" are included when they are the most
// useful hint for a positional value.
func CompleteLine(line string) []string {
	return completeShellLine(line)
}

func completeShellLine(line string) []string {
	parts, err := splitPipeline(line)
	if err != nil || len(parts) == 0 {
		return nil
	}
	if strings.Contains(line, "|") && strings.HasSuffix(line, parts[len(parts)-1]) {
		// Once the cursor is in the current pipeline segment, completions switch
		// from command grammar to pipe grammar.
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
		// readline expects suffix completions, but this package returns full
		// lines. Split the token under the cursor so callers can derive either.
		prefix = args[len(args)-1]
		baseArgs = args[:len(args)-1]
	}
	candidates := completionCandidatesForArgs(baseArgs)
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

func shellCompletionHintLine(line string) string {
	lineRunes := []rune(line)
	var hints []string
	realCandidates := 0
	for _, candidate := range completeShellLine(line) {
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
	if candidates, ok := valueCompletionCandidatesForArgs(resolved); ok {
		return candidates
	}
	switch len(resolved) {
	case 1:
		switch resolved[0] {
		case "show":
			return []string{"devices", "target", "wifi", "festival", "controller"}
		case "set":
			return []string{"target", "festival", "controller"}
		case "clear":
			return []string{"target"}
		case "request":
			return []string{"wifi", "festival", "controller"}
		case "monitor":
			return []string{"wifi"}
		case "test":
			return []string{"dns", "http", "download"}
		}
	case 2:
		if resolved[0] == "show" && resolved[1] == "wifi" {
			return []string{"status", "diagnostics", "scan", "capabilities"}
		}
		if resolved[0] == "show" && resolved[1] == "festival" {
			return []string{"standalone", "runs", "run"}
		}
		if resolved[0] == "show" && resolved[1] == "controller" {
			return []string{"endpoint", "link"}
		}
		if resolved[0] == "set" && resolved[1] == "festival" {
			return []string{"standalone"}
		}
		if resolved[0] == "set" && resolved[1] == "controller" {
			return []string{"endpoint"}
		}
		if resolved[0] == "request" && resolved[1] == "wifi" {
			return []string{"connect", "disconnect", "forget", "reconnect", "wait", "assert", "cycle"}
		}
		if resolved[0] == "request" && resolved[1] == "festival" {
			return []string{"run", "sync", "clear"}
		}
		if resolved[0] == "request" && resolved[1] == "controller" {
			return []string{"reconnect"}
		}
		if resolved[0] == "monitor" && resolved[1] == "wifi" {
			return monitorWifiCompletionCandidates(nil)
		}
	case 3:
		if resolved[0] == "show" && resolved[1] == "wifi" && resolved[2] == "scan" {
			return []string{"fresh", "detail", "all", "2.4ghz", "5ghz", "6ghz", "60ghz"}
		}
		if resolved[0] == "show" && resolved[1] == "festival" && resolved[2] == "standalone" {
			return []string{"status", "config"}
		}
		if resolved[0] == "set" && resolved[1] == "festival" && resolved[2] == "standalone" {
			return []string{"enabled", "disabled", "plan", "interval", "retention", "max-size"}
		}
		if resolved[0] == "set" && resolved[1] == "controller" && resolved[2] == "endpoint" {
			return []string{"<host:port>", "enabled", "disabled", "min-backoff", "max-backoff"}
		}
		if resolved[0] == "request" && resolved[1] == "wifi" && resolved[2] == "wait" {
			return []string{"connected"}
		}
		if resolved[0] == "request" && resolved[1] == "festival" && resolved[2] == "run" {
			return []string{"once"}
		}
	}
	if len(resolved) >= 1 {
		lastCommand := resolved[len(resolved)-1]
		switch {
		case len(resolved) >= 3 && resolved[0] == "show" && resolved[1] == "wifi" && resolved[2] == "scan":
			return showWifiScanCompletionCandidates(resolved[3:])
		case resolved[0] == "ping":
			return pingCompletionCandidates(resolved[1:])
		case resolved[0] == "traceroute":
			return tracerouteCompletionCandidates(resolved[1:])
		case resolved[0] == "path-mtu":
			return pathMtuCompletionCandidates(resolved[1:])
		case resolved[0] == "global-ip":
			return globalIPCompletionCandidates(resolved[1:])
		case resolved[0] == "test" && len(resolved) >= 2:
			return testCompletionCandidates(resolved[1], resolved[2:])
		case resolved[0] == "monitor" && len(resolved) >= 2 && resolved[1] == "wifi":
			return monitorWifiCompletionCandidates(resolved[2:])
		case resolved[0] == "request" && len(resolved) >= 3 && resolved[1] == "wifi":
			return requestWifiCompletionCandidates(resolved[2], resolved[3:], lastCommand)
		case resolved[0] == "request" && len(resolved) >= 3 && resolved[1] == "festival":
			return requestFestivalCompletionCandidates(resolved[2], resolved[3:])
		case resolved[0] == "set" && len(resolved) >= 3 && resolved[1] == "controller" && resolved[2] == "endpoint":
			return setControllerEndpointCompletionCandidates(resolved[3:])
		case resolved[0] == "set" && len(resolved) >= 3 && resolved[1] == "festival" && resolved[2] == "standalone":
			return setFestivalStandaloneCompletionCandidates(resolved[3:])
		}
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
	case len(args) >= 4 && args[0] == "request" && args[1] == "wifi":
		return requestWifiValueCompletionCandidates(args[2], last)
	case len(args) >= 4 && args[0] == "request" && args[1] == "festival":
		return requestFestivalValueCompletionCandidates(args[2], last)
	case len(args) >= 4 && args[0] == "set" && args[1] == "festival" && args[2] == "standalone":
		return setFestivalStandaloneValueCompletionCandidates(last)
	case len(args) >= 4 && args[0] == "set" && args[1] == "controller" && args[2] == "endpoint":
		return setControllerEndpointValueCompletionCandidates(last)
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
		return nil, false
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
		return nil, false
	case args[0] == "path-mtu":
		switch {
		case isResolvedKeyword("path-mtu option", last, []string{"min-mtu", "max-mtu"}):
			return []string{"<bytes>"}, true
		case isResolvedKeyword("path-mtu option", last, []string{"timeout"}):
			return []string{"<ms>"}, true
		}
		return nil, false
	case args[0] == "global-ip":
		return globalIPValueCompletionCandidates(last)
	case len(args) >= 2 && args[0] == "test":
		return testValueCompletionCandidates(args[1], last)
	default:
		return nil, false
	}
}

func requestWifiValueCompletionCandidates(command string, last string) ([]string, bool) {
	switch command {
	case "connect", "cycle":
		switch {
		case isResolvedKeyword("request wifi "+command+" option", last, []string{"security"}):
			return wifiConnectSecurityValues(), true
		case isResolvedKeyword("request wifi "+command+" option", last, []string{"band"}):
			return wifiBandValues(), true
		case isResolvedKeyword("request wifi "+command+" option", last, []string{"mac-randomization"}):
			return wifiMacRandomizationValues(), true
		case isResolvedKeyword("request wifi "+command+" option", last, []string{"passphrase"}):
			return []string{"<passphrase>"}, true
		case isResolvedKeyword("request wifi "+command+" option", last, []string{"bssid"}):
			return []string{"<bssid>"}, true
		case isResolvedKeyword("request wifi "+command+" option", last, []string{"timeout"}):
			return []string{"<ms>"}, true
		case command == "cycle" && isResolvedKeyword("request wifi "+command+" option", last, []string{"pause"}):
			return []string{"<ms>"}, true
		case command == "cycle" && isResolvedKeyword("request wifi "+command+" option", last, []string{"count"}):
			return []string{"<n>"}, true
		case command == "cycle" && isResolvedKeyword("request wifi "+command+" option", last, []string{"ping"}):
			return []string{"<host>"}, true
		case command == "cycle" && isResolvedKeyword("request wifi "+command+" option", last, []string{"http"}):
			return []string{"<url>"}, true
		default:
			return nil, false
		}
	case "reconnect":
		if isResolvedKeyword("request wifi reconnect option", last, []string{"timeout"}) {
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

func testValueCompletionCandidates(command string, last string) ([]string, bool) {
	switch command {
	case "dns":
		switch {
		case isResolvedKeyword("test dns option", last, []string{"type"}):
			return dnsTypeValues(), true
		case isResolvedKeyword("test dns option", last, []string{"timeout"}):
			return []string{"<ms>"}, true
		default:
			return nil, false
		}
	case "http":
		switch {
		case isResolvedKeyword("test http option", last, []string{"expected-status"}):
			return []string{"<code>"}, true
		case isResolvedKeyword("test http option", last, []string{"timeout"}):
			return []string{"<ms>"}, true
		}
		return nil, false
	case "download":
		if isResolvedKeyword("test download option", last, []string{"timeout"}) {
			return []string{"<ms>"}, true
		}
		return nil, false
	default:
		return nil, false
	}
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
		return append([]string{"fresh", "detail"}, wifiBandValues()...)
	}
	first, err := resolveShellKeyword("show wifi scan argument", args[0], append([]string{"fresh", "detail"}, wifiBandValues()...))
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
		return nil
	}
}

func showWifiScanFreshCompletionCandidates(args []string) []string {
	timeoutUsed := false
	bandUsed := false
	for i := 0; i < len(args); i++ {
		if key, err := resolveShellKeyword("show wifi scan fresh option", args[i], []string{"timeout"}); err == nil {
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
		state := scanCompletionArgs("request wifi reconnect option", args, options)
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
	state := scanCompletionArgs("request wifi option", args, options)
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

func setFestivalStandaloneCompletionCandidates(args []string) []string {
	options := []completionOption{
		{name: "enabled", flag: true},
		{name: "disabled", flag: true},
		{name: "plan", placeholder: "<file>"},
		{name: "interval", placeholder: "<duration>"},
		{name: "retention", placeholder: "<duration>"},
		{name: "max-size", placeholder: "<bytes>"},
	}
	state := scanCompletionArgs("set festival standalone option", args, options)
	if state.pending != nil {
		return optionValueCandidates(*state.pending)
	}
	return optionAndPositionalCandidates(state, options, 0)
}

func setFestivalStandaloneValueCompletionCandidates(last string) ([]string, bool) {
	switch {
	case isResolvedKeyword("set festival standalone option", last, []string{"plan"}):
		return []string{"<file>"}, true
	case isResolvedKeyword("set festival standalone option", last, []string{"interval", "retention"}):
		return []string{"<duration>"}, true
	case isResolvedKeyword("set festival standalone option", last, []string{"max-size"}):
		return []string{"<bytes>"}, true
	default:
		return nil, false
	}
}

func setControllerEndpointCompletionCandidates(args []string) []string {
	options := []completionOption{
		{name: "enabled", flag: true},
		{name: "disabled", flag: true},
		{name: "endpoint", placeholder: "<host:port>"},
		{name: "min-backoff", placeholder: "<duration>"},
		{name: "max-backoff", placeholder: "<duration>"},
	}
	state := scanCompletionArgs("set controller endpoint option", args, options)
	if state.pending != nil {
		return optionValueCandidates(*state.pending)
	}
	return optionAndPositionalCandidates(state, options, 1, "<host:port>")
}

func setControllerEndpointValueCompletionCandidates(last string) ([]string, bool) {
	switch {
	case isResolvedKeyword("set controller endpoint option", last, []string{"endpoint"}):
		return []string{"<host:port>"}, true
	case isResolvedKeyword("set controller endpoint option", last, []string{"min-backoff", "max-backoff"}):
		return []string{"<duration>"}, true
	default:
		return nil, false
	}
}

func requestFestivalCompletionCandidates(command string, args []string) []string {
	switch command {
	case "run":
		if len(args) == 0 {
			return []string{"once"}
		}
		if args[0] != "once" {
			return nil
		}
		options := []completionOption{
			{name: "plan", placeholder: "<file>"},
			{name: "save", flag: true},
		}
		state := scanCompletionArgs("request festival run once option", args[1:], options)
		if state.pending != nil {
			return optionValueCandidates(*state.pending)
		}
		return optionAndPositionalCandidates(state, options, 0)
	case "sync":
		options := []completionOption{
			{name: "output", placeholder: "<dir>"},
			{name: "limit", placeholder: "<n>"},
			{name: "mark-synced", flag: true},
			{name: "keep-unsynced", flag: true},
		}
		state := scanCompletionArgs("request festival sync option", args, options)
		if state.pending != nil {
			return optionValueCandidates(*state.pending)
		}
		return optionAndPositionalCandidates(state, options, 0)
	case "clear":
		return []string{"synced", "all"}
	default:
		return nil
	}
}

func requestFestivalValueCompletionCandidates(command string, last string) ([]string, bool) {
	switch command {
	case "run":
		if isResolvedKeyword("request festival run once option", last, []string{"plan"}) {
			return []string{"<file>"}, true
		}
	case "sync":
		switch {
		case isResolvedKeyword("request festival sync option", last, []string{"output"}):
			return []string{"<dir>"}, true
		case isResolvedKeyword("request festival sync option", last, []string{"limit"}):
			return []string{"<n>"}, true
		}
	}
	return nil, false
}

func testCompletionCandidates(command string, args []string) []string {
	switch command {
	case "dns":
		options := []completionOption{
			{name: "type", values: dnsTypeValues()},
			{name: "timeout", placeholder: "<ms>"},
		}
		state := scanCompletionArgs("test dns option", args, options)
		if state.pending != nil {
			return optionValueCandidates(*state.pending)
		}
		return optionAndPositionalCandidates(state, options, 1, "<name>")
	case "http":
		options := []completionOption{
			{name: "expected-status", placeholder: "<code>"},
			{name: "timeout", placeholder: "<ms>"},
		}
		state := scanCompletionArgs("test http option", args, options)
		if state.pending != nil {
			return optionValueCandidates(*state.pending)
		}
		return optionAndPositionalCandidates(state, options, 1, "<url>")
	case "download":
		options := []completionOption{{name: "timeout", placeholder: "<ms>"}}
		state := scanCompletionArgs("test download option", args, options)
		if state.pending != nil {
			return optionValueCandidates(*state.pending)
		}
		return optionAndPositionalCandidates(state, options, 1, "<url>")
	default:
		return nil
	}
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
		if _, err := normalizeIpFamily(args[i]); err == nil {
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
