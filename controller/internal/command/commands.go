package command

import (
	"errors"
	"fmt"
	"strings"

	"dropcheck/controller/internal/controlpb"
)

func BuildCommand(args []string) (*controlpb.RunCommand, error) {
	args, err := NormalizeAgentCommandArgs(args)
	if err != nil {
		return nil, err
	}
	label := commandLabel(args)
	switch args[0] {
	case "wifi":
		if len(args) < 2 {
			return nil, errors.New(wifiUsage())
		}
		switch args[1] {
		case "status":
			if len(args) != 2 {
				return nil, errors.New("usage: wifi status")
			}
			return &controlpb.RunCommand{
				Label: label,
				Command: &controlpb.RunCommand_GetWifiStatus{
					GetWifiStatus: &controlpb.GetWifiStatus{},
				},
			}, nil
		case "diagnostics":
			if len(args) != 2 {
				return nil, errors.New("usage: wifi diagnostics")
			}
			return &controlpb.RunCommand{
				Label: label,
				Command: &controlpb.RunCommand_GetWifiDiagnostics{
					GetWifiDiagnostics: &controlpb.GetWifiDiagnostics{},
				},
			}, nil
		case "scan":
			return buildWifiScanCommand(label, args[2:])
		case "capabilities":
			if len(args) != 2 {
				return nil, errors.New("usage: wifi capabilities")
			}
			return &controlpb.RunCommand{
				Label: label,
				Command: &controlpb.RunCommand_GetWifiCapabilities{
					GetWifiCapabilities: &controlpb.GetWifiCapabilities{},
				},
			}, nil
		case "connect":
			return buildWifiConnectCommand(label, args[2:])
		case "disconnect":
			if len(args) != 2 {
				return nil, errors.New("usage: wifi disconnect")
			}
			return &controlpb.RunCommand{
				Label: label,
				Command: &controlpb.RunCommand_DisconnectWifi{
					DisconnectWifi: &controlpb.DisconnectWifi{},
				},
			}, nil
		case "forget":
			if len(args) != 3 {
				return nil, errors.New("usage: wifi forget <ssid|network_id>")
			}
			return &controlpb.RunCommand{
				Label: label,
				Command: &controlpb.RunCommand_ForgetWifi{
					ForgetWifi: &controlpb.ForgetWifi{Target: args[2]},
				},
			}, nil
		case "wait":
			return buildWifiWaitCommand(label, args[2:])
		case "assert":
			return buildWifiAssertCommand(label, args[2:])
		case "watch":
			return buildWifiWatchCommand(label, args[2:])
		case "monitor":
			return buildWifiMonitorCommand(label, args[2:])
		case "reconnect":
			return buildWifiReconnectCommand(label, args[2:])
		case "cycle":
			return buildWifiCycleCommand(label, args[2:])
		default:
			return nil, errors.New(wifiUsage())
		}
	case "ip":
		if len(args) != 1 {
			return nil, errors.New("usage: ip")
		}
		return &controlpb.RunCommand{
			Label: label,
			Command: &controlpb.RunCommand_GetIpStatus{
				GetIpStatus: &controlpb.GetIpStatus{Selector: &controlpb.NetworkSelector{}},
			},
		}, nil
	case "ping":
		return buildPingCommand(label, args[1:])
	case "traceroute":
		return buildTracerouteCommand(label, args[1:])
	case "path-mtu":
		return buildPathMtuCommand(label, args[1:])
	case "global-ip":
		return buildGlobalIpCommand(label, args[1:])
	case "download":
		return buildDownloadCommand(label, args[1:])
	case "dns":
		return buildDNSCommand(label, args[1:])
	case "http":
		return buildHTTPCommand(label, args[1:])
	default:
		return nil, fmt.Errorf("unknown command %q", args[0])
	}
}

func commandLabel(args []string) string {
	parts := append([]string(nil), args...)
	if len(parts) >= 4 && parts[0] == "wifi" {
		switch parts[1] {
		case "connect", "cycle":
			parts[3] = "<redacted>"
		}
	}
	return strings.Join(parts, " ")
}

func wifiUsage() string {
	return "usage: wifi status | wifi diagnostics | wifi scan [band] | wifi scan fresh [band] [--timeout ms] | wifi scan detail <ssid|bssid> [band] | wifi capabilities | wifi connect <ssid> <passphrase> [security] [--bssid bssid] [--band band] [--mac-randomization auto|none|persistent|non-persistent] [--timeout ms] | wifi disconnect | wifi forget <ssid|network_id> | wifi wait connected [ssid] [--bssid bssid] [--security security] [--band band] [--ip] [--validated] [--timeout ms] | wifi assert [--ssid ssid] [--bssid bssid] [--security security] [--band band] [--ip] [--validated] [--timeout ms] | wifi watch [duration_ms] [interval_ms] | wifi monitor [duration_ms] [interval_ms] | wifi reconnect [timeout_ms] | wifi cycle <ssid> <passphrase> [security] [--count n] [--bssid bssid] [--band band] [--mac-randomization auto|none|persistent|non-persistent] [--ping host] [--http url] [--forget] [--pause ms] [--timeout ms]"
}

func buildWifiScanCommand(label string, args []string) (*controlpb.RunCommand, error) {
	if len(args) > 0 && args[0] == "fresh" {
		band := controlpb.WifiBand_WIFI_BAND_ALL
		timeoutMs := uint32(10000)
		rest := args[1:]
		if len(rest) > 0 && !strings.HasPrefix(rest[0], "--") {
			parsed, err := parseWifiBand(rest[0])
			if err != nil {
				return nil, err
			}
			band = parsed
			rest = rest[1:]
		}
		for i := 0; i < len(rest); i++ {
			switch rest[i] {
			case "--timeout":
				value, next, err := parseUint32Option(rest, i, "timeout")
				if err != nil {
					return nil, err
				}
				timeoutMs = value
				i = next
			default:
				return nil, errors.New("usage: wifi scan fresh [all|2.4ghz|5ghz|6ghz|60ghz] [--timeout ms]")
			}
		}
		return &controlpb.RunCommand{
			Label: label,
			Command: &controlpb.RunCommand_GetFreshWifiScan{
				GetFreshWifiScan: &controlpb.GetFreshWifiScan{Band: band, TimeoutMs: timeoutMs},
			},
		}, nil
	}
	if len(args) > 0 && args[0] == "detail" {
		if len(args) < 2 || len(args) > 3 {
			return nil, errors.New("usage: wifi scan detail <ssid|bssid> [all|2.4ghz|5ghz|6ghz|60ghz]")
		}
		band, err := parseWifiBand(optionalArg(args, 2))
		if err != nil {
			return nil, err
		}
		return &controlpb.RunCommand{
			Label: label,
			Command: &controlpb.RunCommand_GetWifiScanDetail{
				GetWifiScanDetail: &controlpb.GetWifiScanDetail{Target: args[1], Band: band},
			},
		}, nil
	}
	if len(args) > 1 {
		return nil, errors.New("usage: wifi scan [all|2.4ghz|5ghz|6ghz|60ghz]")
	}
	band, err := parseWifiBand(optionalArg(args, 0))
	if err != nil {
		return nil, err
	}
	return &controlpb.RunCommand{
		Label: label,
		Command: &controlpb.RunCommand_GetWifiScan{
			GetWifiScan: &controlpb.GetWifiScan{Band: band},
		},
	}, nil
}

func buildWifiConnectCommand(label string, args []string) (*controlpb.RunCommand, error) {
	if len(args) < 2 {
		return nil, errors.New("usage: wifi connect <ssid> <passphrase> [wpa2|wpa3|transition] [--bssid bssid] [--band band] [--mac-randomization auto|none|persistent|non-persistent] [--timeout ms]")
	}
	connect := &controlpb.ConnectWifi{
		Ssid:       args[0],
		Passphrase: args[1],
		Security:   controlpb.ConnectWifi_SECURITY_WPA2_PSK,
		TimeoutMs:  45000,
		Band:       controlpb.WifiBand_WIFI_BAND_ALL,
	}
	rest := args[2:]
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "--") {
		security, err := parseSecurity(rest[0])
		if err != nil {
			return nil, err
		}
		connect.Security = security
		rest = rest[1:]
	}
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "--bssid":
			value, next, err := parseStringOption(rest, i, "bssid")
			if err != nil {
				return nil, err
			}
			connect.Bssid = value
			i = next
		case "--band":
			value, next, err := parseStringOption(rest, i, "band")
			if err != nil {
				return nil, err
			}
			band, err := parseWifiBand(value)
			if err != nil {
				return nil, err
			}
			connect.Band = band
			i = next
		case "--mac-randomization":
			value, next, err := parseStringOption(rest, i, "mac-randomization")
			if err != nil {
				return nil, err
			}
			macRandomization, err := parseMacRandomization(value)
			if err != nil {
				return nil, err
			}
			connect.MacRandomization = macRandomization
			i = next
		case "--timeout":
			value, next, err := parseUint32Option(rest, i, "timeout")
			if err != nil {
				return nil, err
			}
			connect.TimeoutMs = value
			i = next
		default:
			return nil, fmt.Errorf("unsupported wifi connect option %q", rest[i])
		}
	}
	return &controlpb.RunCommand{
		Label: label,
		Command: &controlpb.RunCommand_ConnectWifi{
			ConnectWifi: connect,
		},
	}, nil
}

func buildWifiWaitCommand(label string, args []string) (*controlpb.RunCommand, error) {
	if len(args) == 0 || args[0] != "connected" {
		return nil, errors.New("usage: wifi wait connected [ssid] [--bssid bssid] [--security wpa2|wpa3|transition] [--band band] [--ip] [--validated] [--timeout ms]")
	}
	req := &controlpb.WaitWifiConnected{
		Band:      controlpb.WifiBand_WIFI_BAND_ALL,
		TimeoutMs: 30000,
	}
	rest := args[1:]
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "--") {
		req.Ssid = rest[0]
		rest = rest[1:]
	}
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "--bssid":
			value, next, err := parseStringOption(rest, i, "bssid")
			if err != nil {
				return nil, err
			}
			req.Bssid = value
			i = next
		case "--security":
			value, next, err := parseStringOption(rest, i, "security")
			if err != nil {
				return nil, err
			}
			security, err := parseSecurity(value)
			if err != nil {
				return nil, err
			}
			req.Security = security
			i = next
		case "--band":
			value, next, err := parseStringOption(rest, i, "band")
			if err != nil {
				return nil, err
			}
			band, err := parseWifiBand(value)
			if err != nil {
				return nil, err
			}
			req.Band = band
			i = next
		case "--ip":
			req.RequireIp = true
		case "--validated":
			req.RequireValidated = true
		case "--timeout":
			value, next, err := parseUint32Option(rest, i, "timeout")
			if err != nil {
				return nil, err
			}
			req.TimeoutMs = value
			i = next
		default:
			return nil, fmt.Errorf("unsupported wifi wait option %q", rest[i])
		}
	}
	return &controlpb.RunCommand{
		Label: label,
		Command: &controlpb.RunCommand_WaitWifiConnected{
			WaitWifiConnected: req,
		},
	}, nil
}

func buildWifiAssertCommand(label string, args []string) (*controlpb.RunCommand, error) {
	req := &controlpb.AssertWifi{Band: controlpb.WifiBand_WIFI_BAND_ALL}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--ssid":
			value, next, err := parseStringOption(args, i, "ssid")
			if err != nil {
				return nil, err
			}
			req.Ssid = value
			i = next
		case "--bssid":
			value, next, err := parseStringOption(args, i, "bssid")
			if err != nil {
				return nil, err
			}
			req.Bssid = value
			i = next
		case "--security":
			value, next, err := parseStringOption(args, i, "security")
			if err != nil {
				return nil, err
			}
			security, err := parseSecurity(value)
			if err != nil {
				return nil, err
			}
			req.Security = security
			i = next
		case "--band":
			value, next, err := parseStringOption(args, i, "band")
			if err != nil {
				return nil, err
			}
			band, err := parseWifiBand(value)
			if err != nil {
				return nil, err
			}
			req.Band = band
			i = next
		case "--ip":
			req.RequireIp = true
		case "--validated":
			req.RequireValidated = true
		case "--timeout":
			value, next, err := parseUint32Option(args, i, "timeout")
			if err != nil {
				return nil, err
			}
			req.TimeoutMs = value
			i = next
		default:
			return nil, fmt.Errorf("unsupported wifi assert option %q", args[i])
		}
	}
	return &controlpb.RunCommand{
		Label: label,
		Command: &controlpb.RunCommand_AssertWifi{
			AssertWifi: req,
		},
	}, nil
}

func buildWifiWatchCommand(label string, args []string) (*controlpb.RunCommand, error) {
	if len(args) > 2 {
		return nil, errors.New("usage: wifi watch [duration_ms] [interval_ms]")
	}
	durationMs := uint32(10000)
	intervalMs := uint32(1000)
	var err error
	if len(args) >= 1 {
		durationMs, err = parseUint32(args[0], "duration_ms")
		if err != nil {
			return nil, err
		}
	}
	if len(args) == 2 {
		intervalMs, err = parseUint32(args[1], "interval_ms")
		if err != nil {
			return nil, err
		}
	}
	return &controlpb.RunCommand{
		Label: label,
		Command: &controlpb.RunCommand_WatchWifi{
			WatchWifi: &controlpb.WatchWifi{DurationMs: durationMs, IntervalMs: intervalMs},
		},
	}, nil
}

func buildWifiMonitorCommand(label string, args []string) (*controlpb.RunCommand, error) {
	if len(args) > 2 {
		return nil, errors.New("usage: wifi monitor [duration_ms] [interval_ms]")
	}
	durationMs := uint32(10000)
	intervalMs := uint32(1000)
	var err error
	if len(args) >= 1 {
		durationMs, err = parseUint32(args[0], "duration_ms")
		if err != nil {
			return nil, err
		}
	}
	if len(args) == 2 {
		intervalMs, err = parseUint32(args[1], "interval_ms")
		if err != nil {
			return nil, err
		}
	}
	return &controlpb.RunCommand{
		Label: label,
		Command: &controlpb.RunCommand_MonitorWifi{
			MonitorWifi: &controlpb.MonitorWifi{DurationMs: durationMs, IntervalMs: intervalMs},
		},
	}, nil
}

func buildWifiReconnectCommand(label string, args []string) (*controlpb.RunCommand, error) {
	if len(args) > 1 {
		return nil, errors.New("usage: wifi reconnect [timeout_ms]")
	}
	timeoutMs := uint32(30000)
	if len(args) == 1 {
		parsed, err := parseUint32(args[0], "timeout_ms")
		if err != nil {
			return nil, err
		}
		timeoutMs = parsed
	}
	return &controlpb.RunCommand{
		Label: label,
		Command: &controlpb.RunCommand_ReconnectWifi{
			ReconnectWifi: &controlpb.ReconnectWifi{TimeoutMs: timeoutMs},
		},
	}, nil
}

func buildWifiCycleCommand(label string, args []string) (*controlpb.RunCommand, error) {
	if len(args) < 2 {
		return nil, errors.New("usage: wifi cycle <ssid> <passphrase> [wpa2|wpa3|transition] [--count n] [--bssid bssid] [--band band] [--mac-randomization auto|none|persistent|non-persistent] [--ping host] [--http url] [--forget] [--pause ms] [--timeout ms]")
	}
	connect := &controlpb.ConnectWifi{
		Ssid:       args[0],
		Passphrase: args[1],
		Security:   controlpb.ConnectWifi_SECURITY_WPA2_PSK,
		TimeoutMs:  45000,
		Band:       controlpb.WifiBand_WIFI_BAND_ALL,
	}
	req := &controlpb.CycleWifi{
		Connect: connect,
		Count:   3,
		PauseMs: 1000,
	}
	rest := args[2:]
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "--") {
		security, err := parseSecurity(rest[0])
		if err != nil {
			return nil, err
		}
		connect.Security = security
		rest = rest[1:]
	}
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "--count":
			value, next, err := parseUint32Option(rest, i, "count")
			if err != nil {
				return nil, err
			}
			req.Count = value
			i = next
		case "--bssid":
			value, next, err := parseStringOption(rest, i, "bssid")
			if err != nil {
				return nil, err
			}
			connect.Bssid = value
			i = next
		case "--band":
			value, next, err := parseStringOption(rest, i, "band")
			if err != nil {
				return nil, err
			}
			band, err := parseWifiBand(value)
			if err != nil {
				return nil, err
			}
			connect.Band = band
			i = next
		case "--mac-randomization":
			value, next, err := parseStringOption(rest, i, "mac-randomization")
			if err != nil {
				return nil, err
			}
			macRandomization, err := parseMacRandomization(value)
			if err != nil {
				return nil, err
			}
			connect.MacRandomization = macRandomization
			i = next
		case "--ping":
			value, next, err := parseStringOption(rest, i, "ping")
			if err != nil {
				return nil, err
			}
			req.PingHost = value
			i = next
		case "--http":
			value, next, err := parseStringOption(rest, i, "http")
			if err != nil {
				return nil, err
			}
			req.HttpUrl = value
			i = next
		case "--forget":
			req.ForgetAfterEach = true
		case "--pause":
			value, next, err := parseUint32Option(rest, i, "pause")
			if err != nil {
				return nil, err
			}
			req.PauseMs = value
			i = next
		case "--timeout":
			value, next, err := parseUint32Option(rest, i, "timeout")
			if err != nil {
				return nil, err
			}
			connect.TimeoutMs = value
			i = next
		default:
			return nil, fmt.Errorf("unsupported wifi cycle option %q", rest[i])
		}
	}
	return &controlpb.RunCommand{
		Label: label,
		Command: &controlpb.RunCommand_CycleWifi{
			CycleWifi: req,
		},
	}, nil
}

func buildPingCommand(label string, args []string) (*controlpb.RunCommand, error) {
	if len(args) < 1 {
		return nil, errors.New("usage: ping <host> [count] [--size bytes] [--timeout ms]")
	}
	ping := &controlpb.Ping{
		Host:     args[0],
		Count:    3,
		Selector: &controlpb.NetworkSelector{},
	}
	rest := args[1:]
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "--") {
		count, err := parseUint32(rest[0], "ping count")
		if err != nil {
			return nil, err
		}
		ping.Count = count
		rest = rest[1:]
	}
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "--size":
			value, next, err := parseUint32Option(rest, i, "size")
			if err != nil {
				return nil, err
			}
			ping.SizeBytes = value
			i = next
		case "--timeout":
			value, next, err := parseUint32Option(rest, i, "timeout")
			if err != nil {
				return nil, err
			}
			ping.TimeoutMs = value
			i = next
		default:
			return nil, fmt.Errorf("unsupported ping option %q", rest[i])
		}
	}
	if ping.TimeoutMs == 0 {
		ping.TimeoutMs = ping.Count*2000 + 3000
	}
	return &controlpb.RunCommand{
		Label: label,
		Command: &controlpb.RunCommand_Ping{
			Ping: ping,
		},
	}, nil
}

func buildTracerouteCommand(label string, args []string) (*controlpb.RunCommand, error) {
	if len(args) < 1 {
		return nil, errors.New("usage: traceroute <host> [max_hops] [--via host_or_ip] [--size bytes] [--timeout ms]")
	}
	trace := &controlpb.Traceroute{
		Host:      args[0],
		MaxHops:   30,
		TimeoutMs: 60000,
		Selector:  &controlpb.NetworkSelector{},
	}
	rest := args[1:]
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "--") {
		maxHops, err := parseUint32(rest[0], "max_hops")
		if err != nil {
			return nil, err
		}
		trace.MaxHops = maxHops
		rest = rest[1:]
	}
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "--via":
			_, next, err := parseStringOption(rest, i, strings.TrimPrefix(rest[i], "--"))
			if err != nil {
				return nil, err
			}
			i = next
		case "--size":
			value, next, err := parseUint32Option(rest, i, "size")
			if err != nil {
				return nil, err
			}
			trace.SizeBytes = value
			i = next
		case "--timeout":
			value, next, err := parseUint32Option(rest, i, "timeout")
			if err != nil {
				return nil, err
			}
			trace.TimeoutMs = value
			i = next
		default:
			return nil, fmt.Errorf("unsupported traceroute option %q", rest[i])
		}
	}
	return &controlpb.RunCommand{
		Label: label,
		Command: &controlpb.RunCommand_Traceroute{
			Traceroute: trace,
		},
	}, nil
}

// buildPathMtuCommand accepts MTU byte bounds; the Android agent converts each candidate MTU to ping payload bytes.
func buildPathMtuCommand(label string, args []string) (*controlpb.RunCommand, error) {
	if len(args) < 1 {
		return nil, errors.New("usage: path-mtu <host> [--min-mtu bytes] [--max-mtu bytes] [--timeout ms]")
	}
	req := &controlpb.PathMtu{
		Host:      args[0],
		TimeoutMs: 30000,
		Selector:  &controlpb.NetworkSelector{},
	}
	rest := args[1:]
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "--min-mtu":
			value, next, err := parseUint32Option(rest, i, "min-mtu")
			if err != nil {
				return nil, err
			}
			req.MinMtuBytes = value
			i = next
		case "--max-mtu":
			value, next, err := parseUint32Option(rest, i, "max-mtu")
			if err != nil {
				return nil, err
			}
			req.MaxMtuBytes = value
			i = next
		case "--timeout":
			value, next, err := parseUint32Option(rest, i, "timeout")
			if err != nil {
				return nil, err
			}
			req.TimeoutMs = value
			i = next
		default:
			return nil, fmt.Errorf("unsupported path-mtu option %q", rest[i])
		}
	}
	return &controlpb.RunCommand{
		Label: label,
		Command: &controlpb.RunCommand_PathMtu{
			PathMtu: req,
		},
	}, nil
}

func buildGlobalIpCommand(label string, args []string) (*controlpb.RunCommand, error) {
	req := &controlpb.GlobalIp{
		Family:    controlpb.IpFamily_IP_FAMILY_ALL,
		TimeoutMs: 5000,
		Selector:  &controlpb.NetworkSelector{},
	}
	familySet := false
	rest := args
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "--") {
		family, err := parseIpFamily(rest[0])
		if err != nil {
			return nil, err
		}
		req.Family = family
		familySet = true
		rest = rest[1:]
	}
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "--family":
			if familySet {
				return nil, fmt.Errorf("global-ip family specified twice")
			}
			value, next, err := parseStringOption(rest, i, "family")
			if err != nil {
				return nil, err
			}
			family, err := parseIpFamily(value)
			if err != nil {
				return nil, err
			}
			req.Family = family
			familySet = true
			i = next
		case "--timeout":
			value, next, err := parseUint32Option(rest, i, "timeout")
			if err != nil {
				return nil, err
			}
			req.TimeoutMs = value
			i = next
		default:
			return nil, fmt.Errorf("unsupported global-ip option %q", rest[i])
		}
	}
	return &controlpb.RunCommand{
		Label: label,
		Command: &controlpb.RunCommand_GlobalIp{
			GlobalIp: req,
		},
	}, nil
}

func buildDownloadCommand(label string, args []string) (*controlpb.RunCommand, error) {
	if len(args) < 1 {
		return nil, errors.New("usage: download <url> [--timeout ms]")
	}
	wget := &controlpb.Wget{
		Url:       args[0],
		TimeoutMs: 60000,
		Selector:  &controlpb.NetworkSelector{},
	}
	rest := args[1:]
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "--timeout":
			value, next, err := parseUint32Option(rest, i, "timeout")
			if err != nil {
				return nil, err
			}
			wget.TimeoutMs = value
			i = next
		default:
			return nil, fmt.Errorf("unsupported download option %q", rest[i])
		}
	}
	return &controlpb.RunCommand{
		Label: label,
		Command: &controlpb.RunCommand_Wget{
			Wget: wget,
		},
	}, nil
}

func buildDNSCommand(label string, args []string) (*controlpb.RunCommand, error) {
	if len(args) < 1 {
		return nil, errors.New("usage: dns <name> [A|AAAA|ALL] [--timeout ms]")
	}
	resolve := &controlpb.ResolveDns{
		Name:      args[0],
		Qtypes:    []controlpb.DnsRecordType{controlpb.DnsRecordType_DNS_RECORD_TYPE_A, controlpb.DnsRecordType_DNS_RECORD_TYPE_AAAA},
		TimeoutMs: 5000,
		Selector:  &controlpb.NetworkSelector{},
	}
	rest := args[1:]
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "--") {
		qtypes, err := parseQTypes(rest[0])
		if err != nil {
			return nil, err
		}
		resolve.Qtypes = qtypes
		rest = rest[1:]
	}
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "--timeout":
			value, next, err := parseUint32Option(rest, i, "timeout")
			if err != nil {
				return nil, err
			}
			resolve.TimeoutMs = value
			i = next
		default:
			return nil, fmt.Errorf("unsupported dns option %q", rest[i])
		}
	}
	return &controlpb.RunCommand{
		Label: label,
		Command: &controlpb.RunCommand_ResolveDns{
			ResolveDns: resolve,
		},
	}, nil
}

func buildHTTPCommand(label string, args []string) (*controlpb.RunCommand, error) {
	if len(args) < 1 {
		return nil, errors.New("usage: http <url> [expected_status] [--timeout ms]")
	}
	http := &controlpb.HttpCheck{
		Url:            normalizeHTTPURL(args[0]),
		ExpectedStatus: 200,
		TimeoutMs:      5000,
		Selector:       &controlpb.NetworkSelector{},
	}
	rest := args[1:]
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "--") {
		expected, err := parseUint32(rest[0], "expected_status")
		if err != nil {
			return nil, err
		}
		http.ExpectedStatus = expected
		rest = rest[1:]
	}
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "--timeout":
			value, next, err := parseUint32Option(rest, i, "timeout")
			if err != nil {
				return nil, err
			}
			http.TimeoutMs = value
			i = next
		default:
			return nil, fmt.Errorf("unsupported http option %q", rest[i])
		}
	}
	return &controlpb.RunCommand{
		Label: label,
		Command: &controlpb.RunCommand_HttpCheck{
			HttpCheck: http,
		},
	}, nil
}

func normalizeHTTPURL(value string) string {
	if strings.Contains(value, "://") {
		return value
	}
	return "https://" + value
}

func NormalizeHTTPURL(value string) string {
	return normalizeHTTPURL(value)
}
