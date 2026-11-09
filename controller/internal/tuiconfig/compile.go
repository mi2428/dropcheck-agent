package tuiconfig

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/tuirun"
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

func compileCheck(check Check, target Target, id string) (tuirun.Check, error) {
	check.Type = strings.TrimSpace(check.Type)
	if check.Name == "" {
		check.Name = check.Type
	}
	p := tuirun.Policy{Attempts: 4, Delay: check.Policy.Delay.Duration, Eventually: check.Policy.Eventually.Duration, StableFor: check.Policy.StableFor.Duration, Interval: check.Policy.Interval.Duration, Timeout: check.Policy.Timeout.Duration}
	if check.Policy.Attempts != nil {
		if *check.Policy.Attempts == 0 {
			return tuirun.Check{}, fmt.Errorf("attempts must be positive")
		}
		p.Attempts = *check.Policy.Attempts
	}
	if check.Policy.Repeat != nil {
		if *check.Policy.Repeat == 0 {
			return tuirun.Check{}, fmt.Errorf("repeat must be positive")
		}
		p.Repeat = *check.Policy.Repeat
	}
	if (check.Type == "ip_status" || check.Type == "wifi_status") && p.Eventually == 0 {
		p.Eventually = check.Timeout.Duration
	}
	expect, err := tuirun.CompileExpectations(check.Expect)
	if err != nil {
		return tuirun.Check{}, err
	}
	if check.Type == "gateway_ping" {
		count, size := uint32(0), uint32(0)
		if check.Count != nil {
			if *check.Count == 0 {
				return tuirun.Check{}, fmt.Errorf("count must be positive")
			}
			count = *check.Count
		}
		if check.SizeBytes != nil {
			if *check.SizeBytes == 0 {
				return tuirun.Check{}, fmt.Errorf("size must be positive")
			}
			size = *check.SizeBytes
		}
		return tuirun.GatewayPing(check.Name, id, tuirun.GatewayPingOptions{Count: count, SizeBytes: size, Family: check.Family, Timeout: check.Timeout.Duration}, p, check.Required, expect...), nil
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
		return tuirun.Check{}, fmt.Errorf("unknown check type %q", check.Type)
	}
	if err != nil {
		return tuirun.Check{}, err
	}
	return tuirun.NewCheck(check.Name, id, op, p, check.Required, expect...), nil
}
