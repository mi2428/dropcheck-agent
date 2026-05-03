package command

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"dropcheck/controller/internal/controlpb"
)

func parseUint32(value string, name string) (uint32, error) {
	parsed, err := strconv.ParseUint(value, 10, 32)
	if err != nil || parsed == 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return uint32(parsed), nil
}

func parseSecurity(value string) (controlpb.ConnectWifi_Security, error) {
	switch strings.ToLower(value) {
	case "", "auto":
		return controlpb.ConnectWifi_SECURITY_UNSPECIFIED, nil
	case "wpa2":
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

func parseIpFamily(value string) (controlpb.IpFamily, error) {
	switch strings.ToLower(value) {
	case "", "all":
		return controlpb.IpFamily_IP_FAMILY_ALL, nil
	case "ipv4":
		return controlpb.IpFamily_IP_FAMILY_IPV4, nil
	case "ipv6":
		return controlpb.IpFamily_IP_FAMILY_IPV6, nil
	default:
		return controlpb.IpFamily_IP_FAMILY_UNSPECIFIED, fmt.Errorf("unsupported IP family %q", value)
	}
}

func normalizeIpFamily(value string) (string, error) {
	switch family, err := parseIpFamily(value); {
	case err != nil:
		return "", err
	case family == controlpb.IpFamily_IP_FAMILY_IPV4:
		return "ipv4", nil
	case family == controlpb.IpFamily_IP_FAMILY_IPV6:
		return "ipv6", nil
	default:
		return "all", nil
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

// NormalizeIpFamily returns the canonical shell completion spelling for value.
func NormalizeIpFamily(value string) (string, error) {
	return normalizeIpFamily(value)
}

// NormalizeDNSQType returns the canonical DNS record-type spelling for value.
func NormalizeDNSQType(value string) (string, error) {
	return normalizeDNSQType(value)
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
	case *controlpb.RunCommand_GlobalIp:
		families := 2
		if c.GlobalIp.Family == controlpb.IpFamily_IP_FAMILY_IPV4 || c.GlobalIp.Family == controlpb.IpFamily_IP_FAMILY_IPV6 {
			families = 1
		}
		return time.Duration(families)*durationFromMillis(c.GlobalIp.TimeoutMs, 5*time.Second) + 3*time.Second
	case *controlpb.RunCommand_Wget:
		return durationFromMillis(c.Wget.TimeoutMs, 60*time.Second) + 5*time.Second
	case *controlpb.RunCommand_ResolveDns:
		return durationFromMillis(c.ResolveDns.TimeoutMs, 10*time.Second) + 3*time.Second
	case *controlpb.RunCommand_HttpCheck:
		return durationFromMillis(c.HttpCheck.TimeoutMs, 10*time.Second) + 3*time.Second
	case *controlpb.RunCommand_RunStandaloneOnce:
		return 30 * time.Minute
	case *controlpb.RunCommand_EditStandaloneConfig,
		*controlpb.RunCommand_GetStandaloneConfig,
		*controlpb.RunCommand_GetStandaloneStatus,
		*controlpb.RunCommand_ListStandaloneRuns,
		*controlpb.RunCommand_GetStandaloneRun,
		*controlpb.RunCommand_ClearStandaloneRuns:
		return 15 * time.Second
	default:
		return 15 * time.Second
	}
}

// TimeoutFor returns the controller-side execution deadline for cmd.
//
// The timeout is intentionally larger than the command's agent timeout when
// applicable, giving the Android side time to report a structured result before
// the controller cancels the command.
func TimeoutFor(cmd *controlpb.RunCommand) time.Duration {
	return timeoutFor(cmd)
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
			// Quotes delimit a token but are not part of the token value. Empty
			// quoted strings still count as an argument because inToken is set.
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

// SplitArgs tokenizes one command line using shell-like quotes and escapes.
//
// Quotes are removed from returned tokens. A trailing escape or unterminated
// quote is reported as a parse error.
func SplitArgs(line string) ([]string, error) {
	return splitArgs(line)
}
