package watch

import (
	"fmt"
	"strconv"
	"strings"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/harness"
	"dropcheck/controller/internal/harness/mlo"
	"dropcheck/controller/internal/harness/scan"
)

func scalar(value *uint32) string {
	if value == nil {
		return ""
	}
	return strconv.FormatUint(uint64(*value), 10)
}
func millis(d Duration) string {
	if d.Duration == 0 {
		return ""
	}
	return strconv.FormatInt(d.Milliseconds(), 10)
}

func compileCheck(check Check, target Target, id string) (harness.Check, error) {
	check.Type = strings.TrimSpace(check.Type)
	if check.Name == "" {
		check.Name = check.Type
	}
	p := harness.Policy{Attempts: 4, Delay: check.Policy.Delay.Duration, Eventually: check.Policy.Eventually.Duration, StableFor: check.Policy.StableFor.Duration, Interval: check.Policy.Interval.Duration, Timeout: check.Policy.Timeout.Duration}
	if check.Policy.Attempts != nil {
		if *check.Policy.Attempts == 0 {
			return nil, fmt.Errorf("attempts must be positive")
		}
		p.Attempts = *check.Policy.Attempts
	}
	if check.Policy.Repeat != nil {
		if *check.Policy.Repeat == 0 {
			return nil, fmt.Errorf("repeat must be positive")
		}
		p.Repeat = *check.Policy.Repeat
	}
	if (check.Type == "ip_status" || check.Type == "wifi_status") && p.Eventually == 0 {
		p.Eventually = check.Timeout.Duration
	}
	expect, err := compileExpect(check.Expect)
	if err != nil {
		return nil, err
	}
	if check.Type == "gateway_ping" {
		count, size := uint32(0), uint32(0)
		if check.Count != nil {
			if *check.Count == 0 {
				return nil, fmt.Errorf("count must be positive")
			}
			count = *check.Count
		}
		if check.SizeBytes != nil {
			if *check.SizeBytes == 0 {
				return nil, fmt.Errorf("size must be positive")
			}
			size = *check.SizeBytes
		}
		return harness.GatewayPing(check.Name, id, harness.GatewayPingOptions{Count: count, SizeBytes: size, Family: check.Family, Timeout: check.Timeout.Duration}, p, check.Required, expect...), nil
	}
	var op command.Operation
	switch check.Type {
	case "wifi_status":
		op = command.WifiStatusOperation()
	case "wifi_diagnostics":
		op = command.WifiDiagnosticsOperation()
	case "wifi_eht":
		op, err = command.WifiEHTOperationWithOptions(command.WifiEHTOptions{Fresh: check.Fresh, Timeout: millis(check.Timeout)})
	case "wifi_capabilities":
		op = command.WifiCapabilitiesOperation()
	case "wifi_scan":
		if check.Fresh {
			op, err = command.WifiFreshScanOperation(check.Band, millis(check.Timeout))
		} else {
			op, err = command.WifiScanOperation(check.Band)
		}
	case "ip_status":
		op = command.IPStatusOperation()
	case "ping":
		op, err = command.PingOperation(command.PingOptions{Host: firstNonEmpty(check.Host, "1.1.1.1"), Count: scalar(check.Count), Size: scalar(check.SizeBytes), Family: check.Family, Timeout: millis(check.Timeout)})
	case "traceroute":
		op, err = command.TracerouteOperation(command.TracerouteOptions{Host: firstNonEmpty(check.Host, "1.1.1.1"), MaxHops: scalar(check.MaxHops), Size: scalar(check.SizeBytes), Family: check.Family, Via: check.Via, Timeout: millis(check.Timeout)})
	case "path_mtu":
		op, err = command.PathMTUOperation(command.PathMTUOptions{Host: firstNonEmpty(check.Host, "1.1.1.1"), MinMTU: scalar(check.MinMTU), MaxMTU: scalar(check.MaxMTU), Family: check.Family, Timeout: millis(check.Timeout)})
	case "dns":
		op, err = command.DNSOperation(firstNonEmpty(check.Query, check.Host, "example.com"), check.Record, millis(check.Timeout))
	case "http":
		status := scalar(check.Status)
		if check.URL == "" && check.Status == nil {
			status = "204"
		}
		op, err = command.HTTPOperation(firstNonEmpty(check.URL, "http://connectivitycheck.gstatic.com/generate_204"), status, millis(check.Timeout))
	case "global_ip":
		op, err = command.GlobalIPOperation(firstNonEmpty(check.Family, "ipv4"), millis(check.Timeout))
	case "download":
		op, err = command.DownloadOperation(firstNonEmpty(check.URL, "http://1.1.1.1/cdn-cgi/trace"), millis(check.Timeout))
	case "scan_detail":
		op, err = command.WifiScanDetailOperation(firstNonEmpty(check.ScanTarget, target.BSSID, target.SSID), firstNonEmpty(check.Band, target.Band))
	default:
		return nil, fmt.Errorf("unknown check type %q", check.Type)
	}
	if err != nil {
		return nil, err
	}
	return harness.NewCheck(check.Name, id, op, p, check.Required, expect...), nil
}

func compileExpect(values map[string]any) ([]harness.Expectation, error) {
	ordinary := map[string]any{}
	var special []harness.Expectation
	for key, raw := range values {
		switch key {
		case "ap":
			fields, ok := raw.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("ap expectation must be a mapping")
			}
			selector := scan.APs()
			for field, value := range fields {
				text := fmt.Sprint(value)
				switch field {
				case "ssid":
					selector = selector.SSID(text)
				case "bssid":
					selector = selector.BSSID(text)
				case "band":
					selector = selector.Band(text)
				case "standard":
					selector = selector.Standard(text)
				case "security":
					selector = selector.Security(text)
				case "channel_width":
					selector = selector.ChannelWidth(text)
				case "channel":
					n, err := strconv.ParseInt(text, 10, 32)
					if err != nil {
						return nil, err
					}
					selector = selector.Channel(int32(n))
				case "ap_mld":
					selector = selector.APMLDMAC(text)
				case "mlo_link_id":
					n, err := strconv.ParseInt(text, 10, 32)
					if err != nil {
						return nil, err
					}
					selector = selector.MLOLinkID(int32(n))
				case "count", "min_count":
				default:
					return nil, fmt.Errorf("unknown AP selector %q", field)
				}
			}
			if n, ok := fields["count"]; ok {
				value, err := strconv.Atoi(fmt.Sprint(n))
				if err != nil {
					return nil, err
				}
				special = append(special, selector.Count().Eq(value))
			} else if n, ok := fields["min_count"]; ok {
				value, err := strconv.Atoi(fmt.Sprint(n))
				if err != nil {
					return nil, err
				}
				special = append(special, selector.Count().Ge(value))
			} else {
				special = append(special, selector.Exists())
			}
		case "mlo_connected":
			if raw != true {
				return nil, fmt.Errorf("mlo_connected requires true")
			}
			special = append(special, mlo.Connected().Present())
		case "mlo_coverage":
			if raw != true {
				return nil, fmt.Errorf("mlo_coverage requires true")
			}
			special = append(special, mlo.CurrentRelation().AssociatedLinksCoveredByScan().IsTrue())
		case "mlo_metadata_complete":
			if raw != true {
				return nil, fmt.Errorf("mlo_metadata_complete requires true")
			}
			special = append(special, mlo.Metadata().Complete().IsTrue())
		default:
			ordinary[key] = raw
		}
	}
	expect, err := harness.CompileExpectations(ordinary)
	return append(expect, special...), err
}
