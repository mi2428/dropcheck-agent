package command

import (
	"fmt"
	"net"
	"os"
	"slices"
	"strconv"
	"strings"

	"dropcheck/controller/internal/controlpb"
)

var shellHostGrammar = []Grammar{
	{Path: "show devices", Description: "List connected Controller agents"},
	{Path: "show adb", Values: []string{"timeout"}, Description: "Collect all ten allowlisted ADB sources"},
	{Path: "show adb wifi", Values: []string{"timeout"}, Description: "Collect five Wi-Fi ADB sources"},
	{Path: "show adb connectivity", Values: []string{"timeout"}, Description: "Collect five Connectivity ADB sources"},
	{Path: "show adb connectivity networks", Values: []string{"timeout"}, Description: "Collect Connectivity Network records"},
	{Path: "show adb connectivity requests", Values: []string{"timeout"}, Description: "Collect Connectivity request records"},
	{Path: "show adb connectivity diagnostics", Values: []string{"timeout"}, Description: "Collect Connectivity diagnostic records"},
	{Path: "show adb connectivity traffic", Values: []string{"timeout"}, Description: "Collect traffic-controller records"},
}

func shellPath(args []string) ([]string, error) {
	var path []string
	for _, word := range args {
		part, err := resolve(word, siblings(ShellGrammarTable, path), false)
		if err != nil {
			return nil, err
		}
		path = append(path, part)
	}
	return path, nil
}

func shellPathSuggestions(args []string) []string {
	path, err := shellPath(args)
	if err != nil {
		return nil
	}
	return suggestedSiblings(ShellGrammarTable, path)
}

// ShellTestProfile recognizes a rejected attempt using the same keyword rules
// as valid input. It never retains any argument values in the historical report.
func ShellTestProfile(args []string) string {
	if len(args) < 2 {
		return ""
	}
	root, err := resolve(args[0], siblings(ShellGrammarTable, nil), false)
	if err != nil || root != "test" {
		return ""
	}
	profile, err := resolve(args[1], siblings(ShellGrammarTable, []string{"test"}), false)
	if err != nil {
		return "unsupported"
	}
	return profile
}

func parseInteractiveTokens(args []string) (result Parsed, err error) {
	// Recognized rejected tests replace historical success without retaining input.
	defer func() {
		if profile := ShellTestProfile(args); err != nil && profile != "" {
			result = Parsed{Path: "test " + profile, Profile: profile, Rejection: "invalid test syntax"}
			err = nil
		}
	}()
	if len(args) == 0 {
		return Parsed{}, fmt.Errorf("missing command")
	}
	if args[0] != "" && strings.HasPrefix("help", args[0]) {
		path, pathErr := shellPath(args[1:])
		return Parsed{Path: "help", Topic: strings.Join(path, " "), Effect: "local"}, pathErr
	}
	if path, pathErr := shellPath(args); pathErr == nil {
		if _, executable := exactSpec(ShellGrammarTable, path); !executable {
			return Parsed{Path: "help", Topic: strings.Join(path, " "), Effect: "local"}, nil
		}
	}
	spec, consumed, err := commandSpec(ShellGrammarTable, args, false)
	if err != nil {
		return Parsed{}, err
	}
	f, via, err := parseFields(spec, args[consumed:])
	if err != nil {
		return Parsed{}, fmt.Errorf("invalid %s arguments", spec.Path)
	}
	for key, value := range f {
		if choices, ok := spec.Enums[key]; ok && !slices.Contains(choices, value) {
			return Parsed{}, fmt.Errorf("invalid %s", key)
		}
		if slices.Contains([]string{"timeout", "count", "size", "max-hops", "min-mtu", "max-mtu", "duration", "interval", "pause", "expected-status"}, key) {
			n, parseErr := strconv.ParseUint(value, 10, 31)
			if parseErr != nil || n == 0 {
				return Parsed{}, fmt.Errorf("%s must be in 1..2147483647", key)
			}
		}
	}
	result = Parsed{Path: spec.Path, Effect: spec.Effect}
	if f["bssid"] != "" {
		mac, macErr := net.ParseMAC(f["bssid"])
		if macErr != nil || len(mac) != 6 {
			return Parsed{}, fmt.Errorf("invalid BSSID")
		}
	}
	if strings.HasPrefix(spec.Path, "show adb") {
		kinds := map[string]string{
			"show adb": "shell-full", "show adb wifi": "shell-wifi", "show adb connectivity": "shell-connectivity",
			"show adb connectivity networks": "dumpsys-connectivity-networks", "show adb connectivity requests": "dumpsys-connectivity-requests",
			"show adb connectivity diagnostics": "dumpsys-connectivity-diagnostics", "show adb connectivity traffic": "dumpsys-connectivity-trafficcontroller",
		}
		result.ADBKind = kinds[spec.Path]
		result.ADBTimeoutMs, err = parseOptionalUint32(f["timeout"], "timeout", 90000)
		if err != nil || result.ADBTimeoutMs > 900000 {
			return Parsed{}, fmt.Errorf("ADB collection timeout must be in 1..900000 ms")
		}
		return result, nil
	}
	switch spec.Path {
	case "show version", "show cli", "show devices", "show test profiles", "show test last", "clear cli default-passphrase":
		result.Detail = true
		return result, nil
	case "set cli default-passphrase":
		if !ValidPassphrase(f["passphrase"]) {
			return Parsed{}, fmt.Errorf("invalid passphrase length or encoding")
		}
		result.Passphrase = f["passphrase"]
		return result, nil
	case "test link", "test lab":
		result.Profile = strings.TrimPrefix(spec.Path, "test ")
		result.SSID, result.Family = f["ssid"], f["family"]
		if strings.TrimSpace(result.SSID) == "" {
			return Parsed{}, fmt.Errorf("SSID is required")
		}
		if result.Profile == "link" {
			if f["bssid"] != "" {
				return Parsed{Path: spec.Path, Profile: "link", Rejection: "strict BSSID pinning is unsupported by the SSID-only test selector"}, nil
			}
			if result.Family == "" {
				result.Family = "ipv4"
			}
		} else {
			status, _ := strconv.ParseUint(f["expected-status"], 10, 32)
			timeout, _ := strconv.ParseUint(f["timeout"], 10, 32)
			result.Lab = LabConfig{f["gateway"], f["dns"], f["resolver"], f["http"], uint32(status), uint32(timeout)}
			if !result.Lab.ValidFor(result.Family) {
				return Parsed{}, fmt.Errorf("invalid lab configuration")
			}
		}
		return result, nil
	case "request wifi connect":
		secret := f["passphrase"]
		if name := f["passphrase-env"]; name != "" {
			if secret != "" || !envName.MatchString(name) {
				return Parsed{}, fmt.Errorf("specify one valid credential source")
			}
			secret = os.Getenv(name)
			if secret == "" {
				return Parsed{}, fmt.Errorf("passphrase-env variable is missing or empty")
			}
		}
		if secret != "" && !ValidPassphrase(secret) {
			return Parsed{}, fmt.Errorf("invalid passphrase length or encoding")
		}
		options := WifiConnectOptions{SSID: f["ssid"], Passphrase: secret, Security: f["security"], BSSID: f["bssid"], Band: f["band"], MacRandomization: f["mac-randomization"], Timeout: f["timeout"]}
		// Validate all non-secret input before session resolution or dispatch.
		wire, buildErr := connectWifi(options)
		if buildErr != nil || options.SSID == "" || validateWifiTarget(wire) != nil {
			return Parsed{}, fmt.Errorf("invalid connection arguments")
		}
		result.Connect = &options
		return result, nil
	}
	var op Operation
	switch spec.Path {
	case "show network":
		op = NetworksOperation()
	case "show network default":
		op, err = NetworkOperation("default")
	case "show network id":
		op, err = NetworkOperation(f["network-id"])
	case "show permissions":
		op = PermissionsOperation()
	case "show wifi status":
		op = WifiDiagnosticsOperation()
		op.Options.WifiRenderMode = WifiRenderModeFull
	case "show wifi status connection":
		op = WifiStatusOperation()
	case "show wifi status ip":
		op = IPStatusOperation()
	case "show wifi status capabilities":
		op = WifiCapabilitiesOperation()
	case "show wifi status eht", "request wifi scan eht":
		op, err = WifiEHTOperationWithOptions(WifiEHTOptions{Fresh: spec.Path == "request wifi scan eht", Timeout: f["timeout"], SSID: f["ssid"], BSSID: f["bssid"]})
		if f["band"] != "" {
			if _, bandErr := parseWifiBand(f["band"]); bandErr != nil {
				return Parsed{}, bandErr
			}
			op.Options.WifiEHTFreshScanBand = f["band"]
		}
	case "show wifi scan":
		op, err = WifiScanOperationWithBrief(f["band"], f["brief"] != "", f["mlo"] != "")
	case "request wifi scan":
		op, err = WifiFreshScanOperationWithBrief(f["band"], f["timeout"], f["brief"] != "", f["mlo"] != "")
	case "request wifi disconnect":
		op = WifiDisconnectOperation()
	case "request wifi reconnect":
		op, err = WifiReconnectOperation(f["timeout"])
	case "request wifi forget ssid", "request wifi forget config-id":
		forget := &controlpb.ForgetWifi{}
		if spec.Path == "request wifi forget ssid" {
			if f["ssid"] == "" {
				return Parsed{}, fmt.Errorf("SSID is required")
			}
			forget.Selector = &controlpb.ForgetWifi_Ssid{Ssid: f["ssid"]}
		} else {
			id, parseErr := strconv.ParseInt(f["config-id"], 10, 32)
			if parseErr != nil || id < 0 || strconv.FormatInt(id, 10) != f["config-id"] {
				return Parsed{}, fmt.Errorf("config-id must be a canonical non-negative integer")
			}
			forget.Selector = &controlpb.ForgetWifi_ConfigId{ConfigId: int32(id)}
		}
		op = NewOperation("wifi.forget", &controlpb.RunCommand{Command: &controlpb.RunCommand_ForgetWifi{ForgetWifi: forget}}, Options{})
	case "request wifi cycle":
		if !ValidPassphrase(f["passphrase"]) {
			return Parsed{}, fmt.Errorf("invalid passphrase length or encoding")
		}
		op, err = WifiCycleOperation(WifiCycleOptions{WifiConnectOptions: WifiConnectOptions{SSID: f["ssid"], Passphrase: f["passphrase"], Security: f["security"], BSSID: f["bssid"], Band: f["band"], MacRandomization: f["mac-randomization"], Timeout: f["timeout"]}, Count: f["count"], PingHost: f["ping"], HTTPURL: f["http"], Pause: f["pause"], ForgetAfterEach: f["forget-after-each"] == "true"})
	case "test wifi":
		op, err = WifiAssertOperation(WifiExpectationOptions{SSID: f["ssid"], BSSID: f["bssid"], Security: f["security"], Band: f["band"], Timeout: f["timeout"], RequireIP: f["require-ip"] == "true", RequireValidated: f["require-validated"] == "true"})
	case "monitor wifi":
		op, err = WifiMonitorOperation(f["duration"], f["interval"])
	case "request ping":
		op, err = PingOperation(PingOptions{Host: f["host"], Count: f["count"], Size: f["size"], Family: f["family"], Timeout: f["timeout"]})
	case "request traceroute":
		op, err = TracerouteOperation(TracerouteOptions{Host: f["host"], MaxHops: f["max-hops"], Via: via, Size: f["size"], Family: f["family"], Timeout: f["timeout"]})
	case "request path-mtu":
		op, err = PathMTUOperation(PathMTUOptions{Host: f["host"], MinMTU: f["min-mtu"], MaxMTU: f["max-mtu"], Family: f["family"], Timeout: f["timeout"]})
	case "request global-ip":
		op, err = GlobalIPOperation(f["family"], f["timeout"])
	case "request dns":
		op, err = DNSOperation(f["name"], f["record"], f["timeout"])
	case "request http":
		op, err = HTTPOperation(f["url"], f["expected-status"], f["timeout"])
	case "request download":
		op, err = DownloadOperation(f["url"], f["timeout"])
	default:
		return Parsed{}, fmt.Errorf("unsupported command")
	}
	if err != nil {
		return Parsed{}, fmt.Errorf("invalid %s arguments", spec.Path)
	}
	if spec.Path == "show wifi scan" || spec.Path == "request wifi scan" {
		if f["ssid"] != "" && f["bssid"] != "" {
			return Parsed{}, fmt.Errorf("ssid and bssid cannot be combined")
		}
		op.Options.WifiScanSSID, op.Options.WifiScanBSSID = f["ssid"], f["bssid"]
	}
	if spec.Path == "show wifi status ip" || spec.Effect == "probe" && strings.HasPrefix(spec.Path, "request ") {
		selector := &controlpb.NetworkSelector{Ssid: f["ssid"], Bssid: f["bssid"], NetworkId: f["network-id"]}
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
	op.Options.Detail = true
	if err := ValidateOperation(op); err != nil {
		return Parsed{}, err
	}
	result.Operation = op
	return result, nil
}
