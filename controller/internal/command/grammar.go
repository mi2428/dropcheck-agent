package command

import (
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"

	"dropcheck/controller/internal/controlpb"
)

// Grammar is the PC command inventory (common commands plus host extensions).
// A path is resolved
// before positional literals; options never reinterpret literal values.
type Grammar struct {
	Path        string
	Positionals []string
	Required    []string
	Switches    []string
	Values      []string
	Unsupported bool
}

var GrammarTable = []Grammar{
	{Path: "show version"},
	{Path: "show devices"},
	{Path: "show adb cmd wifi status"},
	{Path: "show adb wifi status"},
	{Path: "show adb wifi dumpsys"},
	{Path: "show adb dumpsys wifi"},
	{Path: "show adb connectivity"},
	{Path: "show adb connectivity dumpsys"},
	{Path: "show adb connectivity networks"},
	{Path: "show adb connectivity requests"},
	{Path: "show adb connectivity diagnostics"},
	{Path: "show adb connectivity trafficcontroller"},
	{Path: "show adb connectivity --diag"},
	{Path: "show adb dumpsys connectivity"},
	{Path: "show adb dumpsys connectivity networks"},
	{Path: "show adb dumpsys connectivity requests"},
	{Path: "show adb dumpsys connectivity diagnostics"},
	{Path: "show adb dumpsys connectivity trafficcontroller"},
	{Path: "show adb diagnostics full"},
	{Path: "show wifi status", Switches: []string{"detail"}},
	{Path: "show wifi diagnostics", Switches: []string{"detail"}},
	{Path: "show wifi capabilities", Switches: []string{"detail"}},
	{Path: "show wifi scan", Switches: []string{"fresh", "brief", "mlo"}, Values: []string{"band", "timeout"}},
	{Path: "show wifi scan detail", Positionals: []string{"target"}, Values: []string{"band"}},
	{Path: "show wifi eht", Switches: []string{"detail", "fresh"}, Values: []string{"ssid", "bssid", "timeout"}},
	{Path: "show ip status", Switches: []string{"detail"}, Values: []string{"ssid"}},
	{Path: "wifi connect", Positionals: []string{"ssid"}, Required: []string{"passphrase"}, Values: []string{"passphrase", "security", "bssid", "band", "mac-randomization", "timeout"}},
	{Path: "wifi disconnect"},
	{Path: "wifi forget", Positionals: []string{"target"}},
	{Path: "wifi wait connected", Values: []string{"ssid", "bssid", "security", "band", "require-ip", "require-validated", "timeout"}},
	{Path: "wifi assert", Values: []string{"ssid", "bssid", "security", "band", "require-ip", "require-validated", "timeout"}},
	{Path: "wifi monitor", Values: []string{"duration", "interval"}},
	{Path: "wifi reconnect", Values: []string{"timeout"}},
	{Path: "wifi cycle", Positionals: []string{"ssid"}, Required: []string{"passphrase"}, Values: []string{"passphrase", "security", "bssid", "band", "mac-randomization", "timeout", "count", "ping", "http", "forget-after-each", "pause"}},
	{Path: "ping", Positionals: []string{"host"}, Values: []string{"count", "size", "family", "timeout", "ssid"}},
	{Path: "traceroute", Positionals: []string{"host"}, Values: []string{"max-hops", "via", "size", "family", "timeout", "ssid"}},
	{Path: "path-mtu", Positionals: []string{"host"}, Values: []string{"min-mtu", "max-mtu", "family", "timeout", "ssid"}},
	{Path: "global-ip", Values: []string{"family", "timeout", "ssid"}},
	{Path: "dns", Positionals: []string{"name"}, Values: []string{"record", "timeout", "ssid"}},
	{Path: "http", Positionals: []string{"url"}, Values: []string{"expected-status", "timeout", "ssid"}},
	{Path: "download", Positionals: []string{"url"}, Values: []string{"timeout", "ssid"}},
	{Path: "help", Positionals: []string{"topic"}},
	{Path: "set default passphrase", Positionals: []string{"passphrase"}, Unsupported: true},
	{Path: "clear default passphrase", Unsupported: true},
	{Path: "use", Positionals: []string{"ssid"}, Required: []string{"passphrase"}, Values: []string{"passphrase"}},
	{Path: "check", Positionals: []string{"name"}, Required: []string{"ssid"}, Values: []string{"ssid", "family", "bssid"}},
	{Path: "show checks"},
	{Path: "show check last", Switches: []string{"detail"}},
}

var aliases = map[string]string{"p": "ping", "tr": "traceroute", "pm": "path-mtu", "gip": "global-ip", "h": "help", "?": "help"}

func init() {
	if err := validateAliases(GrammarTable, aliases); err != nil {
		panic(err)
	}
}

func validateAliases(table []Grammar, mapping map[string]string) error {
	canonical := map[string]bool{}
	for _, spec := range table {
		canonical[strings.Fields(spec.Path)[0]] = true
	}
	for alias, target := range mapping {
		if canonical[alias] || !canonical[target] {
			return fmt.Errorf("invalid or colliding command alias")
		}
	}
	return nil
}

// Parsed is a validated action. Non-agent actions never dispatch to the runner.
type Parsed struct {
	Path                         string
	Topic                        string
	Operation                    Operation
	ADBKind                      string
	Profile, SSID, Family, BSSID string
	Detail                       bool
	Rejection                    string
}

func siblings(prefix []string) []string {
	var out []string
	for _, spec := range GrammarTable {
		path := strings.Fields(spec.Path)
		if len(path) <= len(prefix) || !slices.Equal(path[:len(prefix)], prefix) || slices.Contains(out, path[len(prefix)]) {
			continue
		}
		out = append(out, path[len(prefix)])
	}
	return out
}

func resolve(word string, options []string, first bool) (string, error) {
	if word == "" {
		return "", fmt.Errorf("empty keyword")
	}
	if slices.Contains(options, word) {
		return word, nil
	}
	if first {
		if canonical, ok := aliases[word]; ok {
			return canonical, nil
		}
	}
	var matches []string
	for _, option := range options {
		if strings.HasPrefix(option, word) {
			matches = append(matches, option)
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("unknown keyword")
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("ambiguous keyword (matches %s)", strings.Join(matches, ", "))
	}
}

func commandSpec(args []string) (Grammar, int, error) {
	var path []string
	for i, word := range args {
		options := siblings(path)
		if len(options) == 0 {
			break
		}
		// An executable path with options starts its argument grammar here.
		if len(path) > 0 && path[len(path)-1] == "scan" && word != "detail" && !strings.HasPrefix("detail", word) {
			break
		}
		if spec, ok := exactSpec(path); ok && len(spec.Positionals) > 0 {
			return spec, i, nil
		}
		resolved, err := resolve(word, options, i == 0)
		if err != nil {
			return Grammar{}, 0, err
		}
		path = append(path, resolved)
	}
	if spec, ok := exactSpec(path); ok {
		return spec, len(path), nil
	}
	return Grammar{}, 0, fmt.Errorf("incomplete command (expected %s)", strings.Join(siblings(path), ", "))
}

func exactSpec(path []string) (Grammar, bool) {
	for _, spec := range GrammarTable {
		if spec.Path == strings.Join(path, " ") {
			return spec, true
		}
	}
	return Grammar{}, false
}

func parseFields(spec Grammar, args []string) (map[string]string, []string, error) {
	fields := make(map[string]string)
	pos := 0
	var via []string
	for i := 0; i < len(args); i++ {
		if pos < len(spec.Positionals) {
			fields[spec.Positionals[pos]] = args[i]
			pos++
			continue
		}
		options := append(slices.Clone(spec.Switches), spec.Values...)
		key, err := resolve(args[i], options, false)
		if err != nil {
			return nil, nil, err
		}
		if key != "via" {
			if _, used := fields[key]; used {
				return nil, nil, fmt.Errorf("%s specified twice", key)
			}
		}
		if slices.Contains(spec.Switches, key) {
			fields[key] = "true"
			continue
		}
		i++
		if i >= len(args) {
			return nil, nil, fmt.Errorf("%s requires a value", key)
		}
		if args[i] == "" {
			return nil, nil, fmt.Errorf("%s requires a nonempty value", key)
		}
		if key == "via" {
			via = append(via, args[i])
		} else {
			fields[key] = args[i]
		}
	}
	if pos < len(spec.Positionals) && spec.Path != "help" {
		return nil, nil, fmt.Errorf("%s requires %s", spec.Path, spec.Positionals[pos])
	}
	for _, key := range spec.Required {
		if fields[key] == "" {
			return nil, nil, fmt.Errorf("%s requires %s", spec.Path, key)
		}
	}
	return fields, via, nil
}

// ParseTokens consumes argv tokens directly, or already-tokenized Shell input.
func ParseTokens(args []string) (Parsed, error) {
	if len(args) == 0 {
		return Parsed{}, fmt.Errorf("missing command")
	}
	spec, consumed, err := commandSpec(args)
	if err != nil {
		return Parsed{}, err
	}
	if spec.Unsupported {
		return Parsed{}, fmt.Errorf("%s is unsupported", spec.Path)
	}
	if spec.Path == "check" && len(args) == consumed {
		return Parsed{Path: "check"}, nil
	}
	if spec.Path == "check" && len(args) > consumed && args[consumed] != "link" {
		name := args[consumed]
		if !slices.Contains([]string{"lab", "internet", "eht"}, name) {
			name = "unsupported"
		}
		return Parsed{Path: "check", Profile: name, Rejection: "profile unsupported; candidates: link, lab, internet, eht"}, nil
	}
	f, via, err := parseFields(spec, args[consumed:])
	if err != nil {
		if spec.Path == "check" {
			return Parsed{Path: "check", Profile: "link", Rejection: "invalid link syntax: " + err.Error()}, nil
		}
		return Parsed{}, err
	}
	for key, value := range f {
		if slices.Contains([]string{"timeout", "count", "size", "max-hops", "min-mtu", "max-mtu", "duration", "interval", "pause", "expected-status"}, key) {
			n, err := strconv.ParseUint(value, 10, 31)
			if err != nil || n == 0 {
				return Parsed{}, fmt.Errorf("%s must be in 1..2147483647", key)
			}
			if key == "max-hops" && n > 255 || key == "count" && spec.Path == "wifi cycle" && n > 100 || key == "pause" && n > 60000 || key == "count" && spec.Path == "ping" && f["timeout"] == "" && n > (2147483647-3000)/2000 {
				return Parsed{}, fmt.Errorf("%s exceeds operation limit", key)
			}
		}
		if slices.Contains([]string{"require-ip", "require-validated", "forget-after-each"}, key) && value != "true" && value != "false" {
			return Parsed{}, fmt.Errorf("%s must be true or false", key)
		}
		if values, ok := enumValues(key, spec.Path); ok && !slices.Contains(values, value) {
			if spec.Path == "check" {
				return Parsed{Path: "check", Profile: "link", Rejection: "invalid family; choose ipv4 or ipv6"}, nil
			}
			return Parsed{}, fmt.Errorf("invalid %s", key)
		}
	}
	if f["bssid"] != "" {
		mac, err := net.ParseMAC(f["bssid"])
		if err != nil || len(mac) != 6 {
			if spec.Path == "check" {
				return Parsed{Path: "check", Profile: "link", Rejection: "invalid BSSID; strict BSSID pinning unsupported"}, nil
			}
			return Parsed{}, fmt.Errorf("invalid BSSID")
		}
	}
	if f["ssid"] == "" && slices.Contains(spec.Positionals, "ssid") || spec.Path == "check" && strings.TrimSpace(f["ssid"]) == "" {
		if spec.Path == "check" {
			return Parsed{Path: "check", Profile: "link", Rejection: "SSID is required"}, nil
		}
		return Parsed{}, fmt.Errorf("SSID is required")
	}
	if spec.Path == "wifi connect" || spec.Path == "wifi cycle" || spec.Path == "use" {
		psk := f["passphrase"]
		validHex := len(psk) == 64
		for _, r := range psk {
			if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
				validHex = false
				break
			}
		}
		if len(psk) < 8 || len(psk) > 63 && !validHex {
			return Parsed{}, fmt.Errorf("passphrase must be 8..63 bytes or 64 hex digits")
		}
	}
	if spec.Path == "wifi forget" && f["target"] == "" {
		return Parsed{}, fmt.Errorf("target is required")
	}
	if spec.Path == "show wifi eht" && f["ssid"] != "" && f["bssid"] != "" {
		return Parsed{}, fmt.Errorf("ssid and bssid cannot be combined")
	}
	if spec.Path == "show wifi eht" && f["timeout"] != "" && f["fresh"] == "" {
		return Parsed{}, fmt.Errorf("timeout requires fresh")
	}
	if spec.Path == "show wifi scan" && f["timeout"] != "" && f["fresh"] == "" {
		return Parsed{}, fmt.Errorf("timeout requires fresh")
	}
	if spec.Path == "show wifi scan" && f["mlo"] != "" && f["brief"] == "" {
		return Parsed{}, fmt.Errorf("mlo requires brief")
	}
	for _, hop := range via {
		if hop == "" || strings.ContainsAny(hop, " \t\r\n\x00") {
			return Parsed{}, fmt.Errorf("invalid via address")
		}
	}
	parsed := Parsed{Path: spec.Path}
	if spec.Path == "check" {
		if f["bssid"] != "" {
			return Parsed{Path: "check", Profile: "link", Rejection: "strict BSSID pinning is unsupported by the SSID-only NetworkSelector"}, nil
		}
		parsed.Profile, parsed.SSID, parsed.Family = f["name"], f["ssid"], f["family"]
		if parsed.Family == "" {
			parsed.Family = "ipv4"
		}
		return parsed, nil
	}
	if spec.Path == "show checks" || spec.Path == "show check last" {
		parsed.Detail = f["detail"] != ""
		return parsed, nil
	}
	if spec.Path == "help" {
		parsed.Topic = f["topic"]
	}
	var op Operation
	switch spec.Path {
	case "show version", "show devices", "help":
		return parsed, nil
	case "show adb cmd wifi status", "show adb wifi status":
		parsed.ADBKind = "cmd-wifi-status"
	case "show adb dumpsys wifi", "show adb wifi dumpsys":
		parsed.ADBKind = "dumpsys-wifi"
	case "show adb dumpsys connectivity", "show adb connectivity", "show adb connectivity dumpsys":
		parsed.ADBKind = "dumpsys-connectivity"
	case "show adb dumpsys connectivity networks", "show adb connectivity networks":
		parsed.ADBKind = "dumpsys-connectivity-networks"
	case "show adb dumpsys connectivity requests", "show adb connectivity requests":
		parsed.ADBKind = "dumpsys-connectivity-requests"
	case "show adb dumpsys connectivity diagnostics", "show adb connectivity diagnostics", "show adb connectivity --diag":
		parsed.ADBKind = "dumpsys-connectivity-diagnostics"
	case "show adb dumpsys connectivity trafficcontroller", "show adb connectivity trafficcontroller":
		parsed.ADBKind = "dumpsys-connectivity-trafficcontroller"
	case "show adb diagnostics full":
		parsed.ADBKind = "full"
	case "show wifi status":
		op = WifiStatusOperation()
	case "show wifi diagnostics":
		op = WifiDiagnosticsOperation()
	case "show wifi capabilities":
		op = WifiCapabilitiesOperation()
	case "show wifi scan":
		if f["fresh"] != "" {
			op, err = WifiFreshScanOperationWithBrief(f["band"], f["timeout"], f["brief"] != "", f["mlo"] != "")
		} else {
			op, err = WifiScanOperationWithBrief(f["band"], f["brief"] != "", f["mlo"] != "")
		}
	case "show wifi scan detail":
		op, err = WifiScanDetailOperation(f["target"], f["band"])
	case "show wifi eht":
		op, err = WifiEHTOperationWithOptions(WifiEHTOptions{Fresh: f["fresh"] != "", Timeout: f["timeout"], SSID: f["ssid"], BSSID: f["bssid"]})
	case "show ip status":
		op = IPStatusOperation()
	case "wifi connect", "wifi cycle", "use":
		connect := WifiConnectOptions{SSID: f["ssid"], Passphrase: f["passphrase"], Security: f["security"], BSSID: f["bssid"], Band: f["band"], MacRandomization: f["mac-randomization"], Timeout: f["timeout"]}
		if spec.Path == "wifi connect" || spec.Path == "use" {
			op, err = WifiConnectOperation(connect)
		} else {
			op, err = WifiCycleOperation(WifiCycleOptions{WifiConnectOptions: connect, Count: f["count"], PingHost: f["ping"], HTTPURL: f["http"], Pause: f["pause"], ForgetAfterEach: f["forget-after-each"] == "true"})
		}
	case "wifi disconnect":
		op = WifiDisconnectOperation()
	case "wifi forget":
		op = WifiForgetOperation(f["target"])
	case "wifi wait connected", "wifi assert":
		want := WifiExpectationOptions{SSID: f["ssid"], BSSID: f["bssid"], Security: f["security"], Band: f["band"], Timeout: f["timeout"], RequireIP: f["require-ip"] == "true", RequireValidated: f["require-validated"] == "true"}
		if spec.Path == "wifi assert" {
			op, err = WifiAssertOperation(want)
		} else {
			op, err = WifiWaitConnectedOperation("", want)
		}
	case "wifi monitor":
		op, err = WifiMonitorOperation(f["duration"], f["interval"])
	case "wifi reconnect":
		op, err = WifiReconnectOperation(f["timeout"])
	case "ping":
		op, err = PingOperation(PingOptions{Host: f["host"], Count: f["count"], Size: f["size"], Family: f["family"], Timeout: f["timeout"]})
	case "traceroute":
		op, err = TracerouteOperation(TracerouteOptions{Host: f["host"], MaxHops: f["max-hops"], Via: via, Size: f["size"], Family: f["family"], Timeout: f["timeout"]})
	case "path-mtu":
		op, err = PathMTUOperation(PathMTUOptions{Host: f["host"], MinMTU: f["min-mtu"], MaxMTU: f["max-mtu"], Family: f["family"], Timeout: f["timeout"]})
	case "global-ip":
		op, err = GlobalIPOperation(f["family"], f["timeout"])
	case "dns":
		op, err = DNSOperation(f["name"], f["record"], f["timeout"])
	case "http":
		op, err = HTTPOperation(f["url"], f["expected-status"], f["timeout"])
	case "download":
		op, err = DownloadOperation(f["url"], f["timeout"])
	}
	if parsed.ADBKind != "" {
		return parsed, nil
	}
	if err != nil {
		return Parsed{}, fmt.Errorf("invalid %s arguments", spec.Path)
	}
	if op.Command == nil {
		return Parsed{}, fmt.Errorf("unsupported command")
	}
	op.Options.Detail = f["detail"] != ""
	if f["ssid"] != "" && (spec.Path == "show ip status" || slices.Contains([]string{"ping", "traceroute", "path-mtu", "global-ip", "dns", "http", "download"}, spec.Path)) {
		selector := &controlpb.NetworkSelector{Ssid: f["ssid"]}
		switch c := op.Command.Command.(type) {
		case *controlpb.RunCommand_GetIpStatus:
			c.GetIpStatus.Selector = selector
		case *controlpb.RunCommand_Ping:
			c.Ping.Selector = selector
		case *controlpb.RunCommand_Traceroute:
			c.Traceroute.Selector = selector
		case *controlpb.RunCommand_PathMtu:
			c.PathMtu.Selector = selector
		case *controlpb.RunCommand_GlobalIp:
			c.GlobalIp.Selector = selector
		case *controlpb.RunCommand_ResolveDns:
			c.ResolveDns.Selector = selector
		case *controlpb.RunCommand_HttpCheck:
			c.HttpCheck.Selector = selector
		case *controlpb.RunCommand_Wget:
			c.Wget.Selector = selector
		}
	}
	if err := ValidateOperation(op); err != nil {
		return Parsed{}, err
	}
	parsed.Operation = op
	return parsed, nil
}

func enumValues(key, path string) ([]string, bool) {
	switch key {
	case "family":
		if path == "check" {
			return []string{"ipv4", "ipv6"}, true
		}
		if path == "global-ip" {
			return []string{"ipv4", "ipv6", "all"}, true
		}
		return []string{"ipv4", "ipv6", "auto"}, true
	case "record":
		return []string{"A", "AAAA", "ALL"}, true
	case "security":
		return []string{"auto", "wpa2", "wpa3", "transition"}, true
	case "band":
		return []string{"all", "2.4ghz", "5ghz", "6ghz", "60ghz"}, true
	case "mac-randomization":
		return []string{"auto", "none", "persistent", "non-persistent"}, true
	}
	return nil, false
}

// Suggestions uses the same paths/options as ParseTokens. Values are hints, not executions.
func Suggestions(args []string) []string {
	if len(args) == 0 {
		return suggestedSiblings(nil)
	}
	spec, n, err := commandSpec(args)
	if err != nil {
		var path []string
		for i, word := range args {
			resolved, resolveErr := resolve(word, siblings(path), i == 0)
			if resolveErr != nil {
				return nil
			}
			path = append(path, resolved)
		}
		return suggestedSiblings(path)
	}
	rest := args[n:]
	if len(rest) == 0 && len(spec.Positionals) > 0 && spec.Path != "help" {
		return []string{"<" + spec.Positionals[0] + ">"}
	}
	used := map[string]bool{}
	pos := 0
	for i := 0; i < len(rest); i++ {
		if pos < len(spec.Positionals) {
			pos++
			continue
		}
		key, err := resolve(rest[i], append(slices.Clone(spec.Switches), spec.Values...), false)
		if err != nil {
			return nil
		}
		used[key] = true
		if !slices.Contains(spec.Switches, key) {
			if i == len(rest)-1 {
				if values, ok := enumValues(key, spec.Path); ok {
					return values
				}
				if strings.HasPrefix(key, "require-") || key == "forget-after-each" {
					return []string{"true", "false"}
				}
				return []string{"<value>"}
			}
			i++
		}
	}
	if pos < len(spec.Positionals) {
		return []string{"<" + spec.Positionals[pos] + ">"}
	}
	var out []string
	for _, key := range append(slices.Clone(spec.Switches), spec.Values...) {
		if !used[key] || key == "via" {
			if key == "mlo" && !used["brief"] || key == "timeout" && (spec.Path == "show wifi scan" || spec.Path == "show wifi eht") && !used["fresh"] {
				continue
			}
			out = append(out, key)
		}
	}
	return out
}

func suggestedSiblings(path []string) []string {
	var out []string
	for _, spec := range GrammarTable {
		words := strings.Fields(spec.Path)
		if !spec.Unsupported && len(words) > len(path) && slices.Equal(words[:len(path)], path) && !slices.Contains(out, words[len(path)]) {
			out = append(out, words[len(path)])
		}
	}
	return out
}
