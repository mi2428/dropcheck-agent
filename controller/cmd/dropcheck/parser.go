package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"dropcheck/controller/internal/controlpb"
	"google.golang.org/protobuf/proto"
)

func optionalArg(args []string, index int) string {
	if len(args) <= index {
		return ""
	}
	return args[index]
}

func parseStringOption(args []string, index int, name string) (string, int, error) {
	if index+1 >= len(args) {
		return "", index, fmt.Errorf("%s requires a value", name)
	}
	return args[index+1], index + 1, nil
}

func parseUint32Option(args []string, index int, name string) (uint32, int, error) {
	value, next, err := parseStringOption(args, index, name)
	if err != nil {
		return 0, index, err
	}
	parsed, err := parseUint32(value, name)
	if err != nil {
		return 0, index, err
	}
	return parsed, next, nil
}

func parseUint32(value string, name string) (uint32, error) {
	parsed, err := strconv.ParseUint(value, 10, 32)
	if err != nil || parsed == 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return uint32(parsed), nil
}

func parseSecurity(value string) (controlpb.ConnectWifi_Security, error) {
	switch strings.ToLower(value) {
	case "", "wpa2":
		return controlpb.ConnectWifi_SECURITY_WPA2_PSK, nil
	case "wpa3":
		return controlpb.ConnectWifi_SECURITY_WPA3_SAE, nil
	case "transition":
		return controlpb.ConnectWifi_SECURITY_WPA2_WPA3_TRANSITION, nil
	default:
		return controlpb.ConnectWifi_SECURITY_UNSPECIFIED, fmt.Errorf("unsupported wifi security %q", value)
	}
}

func parseWifiBand(value string) (controlpb.WifiBand, error) {
	switch strings.ToLower(value) {
	case "", "all":
		return controlpb.WifiBand_WIFI_BAND_ALL, nil
	case "2.4ghz":
		return controlpb.WifiBand_WIFI_BAND_2_4_GHZ, nil
	case "5ghz":
		return controlpb.WifiBand_WIFI_BAND_5_GHZ, nil
	case "6ghz":
		return controlpb.WifiBand_WIFI_BAND_6_GHZ, nil
	case "60ghz":
		return controlpb.WifiBand_WIFI_BAND_60_GHZ, nil
	default:
		return controlpb.WifiBand_WIFI_BAND_UNSPECIFIED, fmt.Errorf("unsupported wifi band %q", value)
	}
}

func parseMacRandomization(value string) (controlpb.ConnectWifi_MacRandomization, error) {
	switch strings.ToLower(value) {
	case "":
		return controlpb.ConnectWifi_MAC_RANDOMIZATION_UNSPECIFIED, nil
	case "auto":
		return controlpb.ConnectWifi_MAC_RANDOMIZATION_AUTO, nil
	case "none":
		return controlpb.ConnectWifi_MAC_RANDOMIZATION_NONE, nil
	case "persistent":
		return controlpb.ConnectWifi_MAC_RANDOMIZATION_PERSISTENT, nil
	case "non-persistent":
		return controlpb.ConnectWifi_MAC_RANDOMIZATION_NON_PERSISTENT, nil
	default:
		return controlpb.ConnectWifi_MAC_RANDOMIZATION_UNSPECIFIED, fmt.Errorf("unsupported wifi MAC randomization %q", value)
	}
}

func parseQTypes(value string) ([]controlpb.DnsRecordType, error) {
	switch strings.ToUpper(value) {
	case "", "ALL":
		return []controlpb.DnsRecordType{
			controlpb.DnsRecordType_DNS_RECORD_TYPE_A,
			controlpb.DnsRecordType_DNS_RECORD_TYPE_AAAA,
		}, nil
	case "A":
		return []controlpb.DnsRecordType{controlpb.DnsRecordType_DNS_RECORD_TYPE_A}, nil
	case "AAAA":
		return []controlpb.DnsRecordType{controlpb.DnsRecordType_DNS_RECORD_TYPE_AAAA}, nil
	default:
		return nil, fmt.Errorf("unsupported DNS qtype %q", value)
	}
}

func normalizeDNSQType(value string) (string, error) {
	normalized := strings.ToUpper(strings.TrimSpace(value))
	switch normalized {
	case "A", "AAAA", "ALL":
		return normalized, nil
	default:
		return "", fmt.Errorf("unsupported DNS qtype %q", value)
	}
}

func timeoutFor(cmd *controlpb.RunCommand) time.Duration {
	switch c := cmd.Command.(type) {
	case *controlpb.RunCommand_ConnectWifi:
		return durationFromMillis(c.ConnectWifi.TimeoutMs, 60*time.Second)
	case *controlpb.RunCommand_GetWifiDiagnostics:
		return 30 * time.Second
	case *controlpb.RunCommand_GetWifiScan:
		return 20 * time.Second
	case *controlpb.RunCommand_GetFreshWifiScan:
		return durationFromMillis(c.GetFreshWifiScan.TimeoutMs, 20*time.Second) + 5*time.Second
	case *controlpb.RunCommand_GetWifiCapabilities:
		return 20 * time.Second
	case *controlpb.RunCommand_WaitWifiConnected:
		return durationFromMillis(c.WaitWifiConnected.TimeoutMs, 35*time.Second) + 5*time.Second
	case *controlpb.RunCommand_AssertWifi:
		return durationFromMillis(c.AssertWifi.TimeoutMs, 10*time.Second) + 5*time.Second
	case *controlpb.RunCommand_WatchWifi:
		return durationFromMillis(c.WatchWifi.DurationMs, 10*time.Second) + 5*time.Second
	case *controlpb.RunCommand_MonitorWifi:
		return durationFromMillis(c.MonitorWifi.DurationMs, 10*time.Second) + 5*time.Second
	case *controlpb.RunCommand_ReconnectWifi:
		return durationFromMillis(c.ReconnectWifi.TimeoutMs, 35*time.Second) + 5*time.Second
	case *controlpb.RunCommand_CycleWifi:
		count := c.CycleWifi.Count
		if count == 0 {
			count = 1
		}
		perCycle := durationFromMillis(c.CycleWifi.GetConnect().GetTimeoutMs(), 60*time.Second) + durationFromMillis(c.CycleWifi.PauseMs, time.Second) + 10*time.Second
		return time.Duration(count)*perCycle + 10*time.Second
	case *controlpb.RunCommand_Ping:
		return durationFromMillis(c.Ping.TimeoutMs, 20*time.Second) + 3*time.Second
	case *controlpb.RunCommand_Traceroute:
		return durationFromMillis(c.Traceroute.TimeoutMs, 60*time.Second) + 5*time.Second
	case *controlpb.RunCommand_PathMtu:
		return durationFromMillis(c.PathMtu.TimeoutMs, 30*time.Second) + 3*time.Second
	case *controlpb.RunCommand_Wget:
		return durationFromMillis(c.Wget.TimeoutMs, 60*time.Second) + 5*time.Second
	case *controlpb.RunCommand_ResolveDns:
		return durationFromMillis(c.ResolveDns.TimeoutMs, 10*time.Second) + 3*time.Second
	case *controlpb.RunCommand_HttpCheck:
		return durationFromMillis(c.HttpCheck.TimeoutMs, 10*time.Second) + 3*time.Second
	default:
		return 15 * time.Second
	}
}

func durationFromMillis(ms uint32, fallback time.Duration) time.Duration {
	if ms == 0 {
		return fallback
	}
	return time.Duration(ms) * time.Millisecond
}

func splitArgs(line string) ([]string, error) {
	var args []string
	var b strings.Builder
	var quote rune
	escaped := false
	inToken := false

	flush := func() {
		if inToken {
			args = append(args, b.String())
			b.Reset()
			inToken = false
		}
	}

	for _, r := range line {
		if escaped {
			b.WriteRune(r)
			escaped = false
			inToken = true
			continue
		}
		if r == '\\' {
			escaped = true
			inToken = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
				continue
			}
			b.WriteRune(r)
			inToken = true
			continue
		}
		switch r {
		case '\'', '"':
			quote = r
			inToken = true
		case ' ', '\t', '\n', '\r':
			flush()
		default:
			b.WriteRune(r)
			inToken = true
		}
	}
	if escaped {
		return nil, errors.New("trailing escape")
	}
	if quote != 0 {
		return nil, errors.New("unterminated quote")
	}
	flush()
	return args, nil
}

func redactedCommand(cmd *controlpb.RunCommand) *controlpb.RunCommand {
	cloned := proto.Clone(cmd).(*controlpb.RunCommand)
	if connect := cloned.GetConnectWifi(); connect != nil && connect.Passphrase != "" {
		cloned.Label = strings.ReplaceAll(cloned.Label, connect.Passphrase, "<redacted>")
		connect.Passphrase = "<redacted>"
	}
	if cycle := cloned.GetCycleWifi(); cycle != nil && cycle.GetConnect().GetPassphrase() != "" {
		cloned.Label = strings.ReplaceAll(cloned.Label, cycle.GetConnect().GetPassphrase(), "<redacted>")
		cycle.Connect.Passphrase = "<redacted>"
	}
	return cloned
}
