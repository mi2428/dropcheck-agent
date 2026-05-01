package command

import (
	"strings"

	"dropcheck/controller/internal/controlpb"
)

type WifiConnectOptions struct {
	SSID             string
	Passphrase       string
	Security         string
	BSSID            string
	Band             string
	MacRandomization string
	Timeout          string
}

type WifiCycleOptions struct {
	WifiConnectOptions
	Count           string
	PingHost        string
	HTTPURL         string
	ForgetAfterEach bool
	Pause           string
}

type WifiExpectationOptions struct {
	SSID             string
	BSSID            string
	Security         string
	Band             string
	RequireIP        bool
	RequireValidated bool
	Timeout          string
}

type PingOptions struct {
	Host    string
	Count   string
	Size    string
	Timeout string
}

type TracerouteOptions struct {
	Host    string
	MaxHops string
	Via     []string
	Size    string
	Timeout string
}

type PathMTUOptions struct {
	Host    string
	MinMTU  string
	MaxMTU  string
	Timeout string
}

func WifiStatusOperation() Operation {
	return NewOperation("wifi.status", &controlpb.RunCommand{
		Label:   "wifi status",
		Command: &controlpb.RunCommand_GetWifiStatus{GetWifiStatus: &controlpb.GetWifiStatus{}},
	}, Options{})
}

func WifiDiagnosticsOperation() Operation {
	return NewOperation("wifi.diagnostics", &controlpb.RunCommand{
		Label:   "wifi diagnostics",
		Command: &controlpb.RunCommand_GetWifiDiagnostics{GetWifiDiagnostics: &controlpb.GetWifiDiagnostics{}},
	}, Options{})
}

func WifiCapabilitiesOperation() Operation {
	return NewOperation("wifi.capabilities", &controlpb.RunCommand{
		Label:   "wifi capabilities",
		Command: &controlpb.RunCommand_GetWifiCapabilities{GetWifiCapabilities: &controlpb.GetWifiCapabilities{}},
	}, Options{})
}

func WifiDisconnectOperation() Operation {
	return NewOperation("wifi.disconnect", &controlpb.RunCommand{
		Label:   "wifi disconnect",
		Command: &controlpb.RunCommand_DisconnectWifi{DisconnectWifi: &controlpb.DisconnectWifi{}},
	}, Options{})
}

func WifiForgetOperation(target string) Operation {
	return NewOperation("wifi.forget", &controlpb.RunCommand{
		Label:   strings.Join([]string{"wifi", "forget", target}, " "),
		Command: &controlpb.RunCommand_ForgetWifi{ForgetWifi: &controlpb.ForgetWifi{Target: target}},
	}, Options{})
}

func WifiScanOperation(bandValue string) (Operation, error) {
	band, err := parseWifiBand(bandValue)
	if err != nil {
		return Operation{}, err
	}
	parts := []string{"wifi", "scan"}
	if bandValue != "" {
		parts = append(parts, bandValue)
	}
	return NewOperation("wifi.scan", &controlpb.RunCommand{
		Label:   strings.Join(parts, " "),
		Command: &controlpb.RunCommand_GetWifiScan{GetWifiScan: &controlpb.GetWifiScan{Band: band}},
	}, Options{}), nil
}

func WifiFreshScanOperation(bandValue string, timeoutValue string) (Operation, error) {
	band, err := parseWifiBand(bandValue)
	if err != nil {
		return Operation{}, err
	}
	timeoutMs, err := parseOptionalUint32(timeoutValue, "timeout", 10000)
	if err != nil {
		return Operation{}, err
	}
	parts := []string{"wifi", "scan", "fresh"}
	if bandValue != "" {
		parts = append(parts, bandValue)
	}
	appendValueOption(&parts, "--timeout", timeoutValue)
	return NewOperation("wifi.scan.fresh", &controlpb.RunCommand{
		Label:   strings.Join(parts, " "),
		Command: &controlpb.RunCommand_GetFreshWifiScan{GetFreshWifiScan: &controlpb.GetFreshWifiScan{Band: band, TimeoutMs: timeoutMs}},
	}, Options{}), nil
}

func WifiScanDetailOperation(target string, bandValue string) (Operation, error) {
	band, err := parseWifiBand(bandValue)
	if err != nil {
		return Operation{}, err
	}
	parts := []string{"wifi", "scan", "detail", target}
	if bandValue != "" {
		parts = append(parts, bandValue)
	}
	return NewOperation("wifi.scan.detail", &controlpb.RunCommand{
		Label:   strings.Join(parts, " "),
		Command: &controlpb.RunCommand_GetWifiScanDetail{GetWifiScanDetail: &controlpb.GetWifiScanDetail{Target: target, Band: band}},
	}, Options{}), nil
}

func WifiConnectOperation(opts WifiConnectOptions) (Operation, error) {
	connect, err := connectWifi(opts)
	if err != nil {
		return Operation{}, err
	}
	return NewOperation("wifi.connect", &controlpb.RunCommand{
		Label:   wifiConnectLabel("connect", opts),
		Command: &controlpb.RunCommand_ConnectWifi{ConnectWifi: connect},
	}, Options{}), nil
}

func WifiCycleOperation(opts WifiCycleOptions) (Operation, error) {
	connect, err := connectWifi(opts.WifiConnectOptions)
	if err != nil {
		return Operation{}, err
	}
	count, err := parseOptionalUint32(opts.Count, "count", 3)
	if err != nil {
		return Operation{}, err
	}
	pauseMs, err := parseOptionalUint32(opts.Pause, "pause", 1000)
	if err != nil {
		return Operation{}, err
	}
	return NewOperation("wifi.cycle", &controlpb.RunCommand{
		Label: wifiCycleLabel(opts),
		Command: &controlpb.RunCommand_CycleWifi{CycleWifi: &controlpb.CycleWifi{
			Connect:         connect,
			Count:           count,
			PingHost:        opts.PingHost,
			HttpUrl:         opts.HTTPURL,
			ForgetAfterEach: opts.ForgetAfterEach,
			PauseMs:         pauseMs,
		}},
	}, Options{}), nil
}

func WifiWaitConnectedOperation(ssid string, opts WifiExpectationOptions) (Operation, error) {
	opts.SSID = ssid
	req := &controlpb.WaitWifiConnected{
		Ssid:             opts.SSID,
		Bssid:            opts.BSSID,
		RequireIp:        opts.RequireIP,
		RequireValidated: opts.RequireValidated,
		Band:             controlpb.WifiBand_WIFI_BAND_ALL,
		TimeoutMs:        30000,
	}
	if opts.Security != "" {
		security, err := parseSecurity(opts.Security)
		if err != nil {
			return Operation{}, err
		}
		req.Security = security
	}
	if opts.Band != "" {
		band, err := parseWifiBand(opts.Band)
		if err != nil {
			return Operation{}, err
		}
		req.Band = band
	}
	timeoutMs, err := parseOptionalUint32(opts.Timeout, "timeout", req.TimeoutMs)
	if err != nil {
		return Operation{}, err
	}
	req.TimeoutMs = timeoutMs
	return NewOperation("wifi.wait", &controlpb.RunCommand{
		Label:   wifiExpectationLabel([]string{"wifi", "wait", "connected"}, opts),
		Command: &controlpb.RunCommand_WaitWifiConnected{WaitWifiConnected: req},
	}, Options{}), nil
}

func WifiAssertOperation(opts WifiExpectationOptions) (Operation, error) {
	req := &controlpb.AssertWifi{
		Ssid:             opts.SSID,
		Bssid:            opts.BSSID,
		RequireIp:        opts.RequireIP,
		RequireValidated: opts.RequireValidated,
		Band:             controlpb.WifiBand_WIFI_BAND_ALL,
	}
	if opts.Security != "" {
		security, err := parseSecurity(opts.Security)
		if err != nil {
			return Operation{}, err
		}
		req.Security = security
	}
	if opts.Band != "" {
		band, err := parseWifiBand(opts.Band)
		if err != nil {
			return Operation{}, err
		}
		req.Band = band
	}
	timeoutMs, err := parseOptionalUint32(opts.Timeout, "timeout", 0)
	if err != nil {
		return Operation{}, err
	}
	req.TimeoutMs = timeoutMs
	return NewOperation("wifi.assert", &controlpb.RunCommand{
		Label:   wifiExpectationLabel([]string{"wifi", "assert"}, opts),
		Command: &controlpb.RunCommand_AssertWifi{AssertWifi: req},
	}, Options{}), nil
}

func WifiWatchOperation(duration string, interval string) (Operation, error) {
	durationMs, err := parseOptionalUint32(duration, "duration_ms", 10000)
	if err != nil {
		return Operation{}, err
	}
	intervalMs, err := parseOptionalUint32(interval, "interval_ms", 1000)
	if err != nil {
		return Operation{}, err
	}
	parts := []string{"wifi", "watch"}
	appendRawValue(&parts, duration)
	appendRawValue(&parts, interval)
	return NewOperation("wifi.watch", &controlpb.RunCommand{
		Label:   strings.Join(parts, " "),
		Command: &controlpb.RunCommand_WatchWifi{WatchWifi: &controlpb.WatchWifi{DurationMs: durationMs, IntervalMs: intervalMs}},
	}, Options{}), nil
}

func WifiMonitorOperation(duration string, interval string) (Operation, error) {
	durationMs, err := parseOptionalUint32(duration, "duration_ms", 10000)
	if err != nil {
		return Operation{}, err
	}
	intervalMs, err := parseOptionalUint32(interval, "interval_ms", 1000)
	if err != nil {
		return Operation{}, err
	}
	parts := []string{"wifi", "monitor"}
	appendRawValue(&parts, duration)
	appendRawValue(&parts, interval)
	return NewOperation("wifi.monitor", &controlpb.RunCommand{
		Label:   strings.Join(parts, " "),
		Command: &controlpb.RunCommand_MonitorWifi{MonitorWifi: &controlpb.MonitorWifi{DurationMs: durationMs, IntervalMs: intervalMs}},
	}, Options{}), nil
}

func WifiReconnectOperation(timeout string) (Operation, error) {
	timeoutMs, err := parseOptionalUint32(timeout, "timeout_ms", 30000)
	if err != nil {
		return Operation{}, err
	}
	parts := []string{"wifi", "reconnect"}
	appendRawValue(&parts, timeout)
	return NewOperation("wifi.reconnect", &controlpb.RunCommand{
		Label:   strings.Join(parts, " "),
		Command: &controlpb.RunCommand_ReconnectWifi{ReconnectWifi: &controlpb.ReconnectWifi{TimeoutMs: timeoutMs}},
	}, Options{}), nil
}

func IPStatusOperation() Operation {
	return NewOperation("ip.status", &controlpb.RunCommand{
		Label:   "ip",
		Command: &controlpb.RunCommand_GetIpStatus{GetIpStatus: &controlpb.GetIpStatus{Selector: &controlpb.NetworkSelector{}}},
	}, Options{})
}

func PingOperation(opts PingOptions) (Operation, error) {
	count, err := parseOptionalUint32(opts.Count, "ping count", 3)
	if err != nil {
		return Operation{}, err
	}
	size, err := parseOptionalUint32(opts.Size, "size", 0)
	if err != nil {
		return Operation{}, err
	}
	timeoutMs, err := parseOptionalUint32(opts.Timeout, "timeout", 0)
	if err != nil {
		return Operation{}, err
	}
	if timeoutMs == 0 {
		timeoutMs = count*2000 + 3000
	}
	parts := []string{"ping", opts.Host}
	appendRawValue(&parts, opts.Count)
	appendValueOption(&parts, "--size", opts.Size)
	appendValueOption(&parts, "--timeout", opts.Timeout)
	return NewOperation("ping", &controlpb.RunCommand{
		Label: strings.Join(parts, " "),
		Command: &controlpb.RunCommand_Ping{Ping: &controlpb.Ping{
			Host: opts.Host, Count: count, SizeBytes: size, TimeoutMs: timeoutMs, Selector: &controlpb.NetworkSelector{},
		}},
	}, Options{}), nil
}

func TracerouteOperation(opts TracerouteOptions) (Operation, error) {
	maxHops, err := parseOptionalUint32(opts.MaxHops, "max_hops", 30)
	if err != nil {
		return Operation{}, err
	}
	size, err := parseOptionalUint32(opts.Size, "size", 0)
	if err != nil {
		return Operation{}, err
	}
	timeoutMs, err := parseOptionalUint32(opts.Timeout, "timeout", 60000)
	if err != nil {
		return Operation{}, err
	}
	parts := []string{"traceroute", opts.Host}
	appendRawValue(&parts, opts.MaxHops)
	for _, via := range opts.Via {
		appendValueOption(&parts, "--via", via)
	}
	appendValueOption(&parts, "--size", opts.Size)
	appendValueOption(&parts, "--timeout", opts.Timeout)
	return NewOperation("traceroute", &controlpb.RunCommand{
		Label: strings.Join(parts, " "),
		Command: &controlpb.RunCommand_Traceroute{Traceroute: &controlpb.Traceroute{
			Host: opts.Host, MaxHops: maxHops, SizeBytes: size, TimeoutMs: timeoutMs, Selector: &controlpb.NetworkSelector{},
		}},
	}, Options{TracerouteRequiredHops: append([]string(nil), opts.Via...)}), nil
}

func PathMTUOperation(opts PathMTUOptions) (Operation, error) {
	minMTU, err := parseOptionalUint32(opts.MinMTU, "min-mtu", 0)
	if err != nil {
		return Operation{}, err
	}
	maxMTU, err := parseOptionalUint32(opts.MaxMTU, "max-mtu", 0)
	if err != nil {
		return Operation{}, err
	}
	timeoutMs, err := parseOptionalUint32(opts.Timeout, "timeout", 30000)
	if err != nil {
		return Operation{}, err
	}
	parts := []string{"path-mtu", opts.Host}
	appendValueOption(&parts, "--min-mtu", opts.MinMTU)
	appendValueOption(&parts, "--max-mtu", opts.MaxMTU)
	appendValueOption(&parts, "--timeout", opts.Timeout)
	return NewOperation("path-mtu", &controlpb.RunCommand{
		Label: strings.Join(parts, " "),
		Command: &controlpb.RunCommand_PathMtu{PathMtu: &controlpb.PathMtu{
			Host: opts.Host, MinMtuBytes: minMTU, MaxMtuBytes: maxMTU, TimeoutMs: timeoutMs, Selector: &controlpb.NetworkSelector{},
		}},
	}, Options{}), nil
}

func GlobalIPOperation(familyValue string, timeoutValue string) (Operation, error) {
	family, err := parseIpFamily(familyValue)
	if err != nil {
		return Operation{}, err
	}
	timeoutMs, err := parseOptionalUint32(timeoutValue, "timeout", 5000)
	if err != nil {
		return Operation{}, err
	}
	parts := []string{"global-ip"}
	appendRawValue(&parts, familyValue)
	appendValueOption(&parts, "--timeout", timeoutValue)
	return NewOperation("global-ip", &controlpb.RunCommand{
		Label: strings.Join(parts, " "),
		Command: &controlpb.RunCommand_GlobalIp{GlobalIp: &controlpb.GlobalIp{
			Family: family, TimeoutMs: timeoutMs, Selector: &controlpb.NetworkSelector{},
		}},
	}, Options{}), nil
}

func DownloadOperation(url string, timeoutValue string) (Operation, error) {
	timeoutMs, err := parseOptionalUint32(timeoutValue, "timeout", 60000)
	if err != nil {
		return Operation{}, err
	}
	parts := []string{"download", url}
	appendValueOption(&parts, "--timeout", timeoutValue)
	return NewOperation("download", &controlpb.RunCommand{
		Label: strings.Join(parts, " "),
		Command: &controlpb.RunCommand_Wget{Wget: &controlpb.Wget{
			Url: url, TimeoutMs: timeoutMs, Selector: &controlpb.NetworkSelector{},
		}},
	}, Options{}), nil
}

func DNSOperation(name string, qtypeValue string, timeoutValue string) (Operation, error) {
	qtypes, err := parseQTypes(qtypeValue)
	if err != nil {
		return Operation{}, err
	}
	timeoutMs, err := parseOptionalUint32(timeoutValue, "timeout", 5000)
	if err != nil {
		return Operation{}, err
	}
	parts := []string{"dns", name}
	appendRawValue(&parts, qtypeValue)
	appendValueOption(&parts, "--timeout", timeoutValue)
	return NewOperation("dns", &controlpb.RunCommand{
		Label: strings.Join(parts, " "),
		Command: &controlpb.RunCommand_ResolveDns{ResolveDns: &controlpb.ResolveDns{
			Name: name, Qtypes: qtypes, TimeoutMs: timeoutMs, Selector: &controlpb.NetworkSelector{},
		}},
	}, Options{}), nil
}

func HTTPOperation(url string, expectedStatus string, timeoutValue string) (Operation, error) {
	expected, err := parseOptionalUint32(expectedStatus, "expected_status", 200)
	if err != nil {
		return Operation{}, err
	}
	timeoutMs, err := parseOptionalUint32(timeoutValue, "timeout", 5000)
	if err != nil {
		return Operation{}, err
	}
	normalized := normalizeHTTPURL(url)
	parts := []string{"http", url}
	appendRawValue(&parts, expectedStatus)
	appendValueOption(&parts, "--timeout", timeoutValue)
	return NewOperation("http", &controlpb.RunCommand{
		Label: strings.Join(parts, " "),
		Command: &controlpb.RunCommand_HttpCheck{HttpCheck: &controlpb.HttpCheck{
			Url: normalized, ExpectedStatus: expected, TimeoutMs: timeoutMs, Selector: &controlpb.NetworkSelector{},
		}},
	}, Options{}), nil
}

func connectWifi(opts WifiConnectOptions) (*controlpb.ConnectWifi, error) {
	security, err := parseSecurity(opts.Security)
	if err != nil {
		return nil, err
	}
	band, err := parseWifiBand(opts.Band)
	if err != nil {
		return nil, err
	}
	macRandomization, err := parseMacRandomization(opts.MacRandomization)
	if err != nil {
		return nil, err
	}
	timeoutMs, err := parseOptionalUint32(opts.Timeout, "timeout", 45000)
	if err != nil {
		return nil, err
	}
	return &controlpb.ConnectWifi{
		Ssid: opts.SSID, Passphrase: opts.Passphrase, Security: security, Bssid: opts.BSSID,
		Band: band, MacRandomization: macRandomization, TimeoutMs: timeoutMs,
	}, nil
}

func parseOptionalUint32(value string, name string, fallback uint32) (uint32, error) {
	if value == "" {
		return fallback, nil
	}
	return parseUint32(value, name)
}

func appendRawValue(parts *[]string, value string) {
	if value != "" {
		*parts = append(*parts, value)
	}
}

func appendValueOption(parts *[]string, name string, value string) {
	if value != "" {
		*parts = append(*parts, name, value)
	}
}

func appendFlag(parts *[]string, name string, enabled bool) {
	if enabled {
		*parts = append(*parts, name)
	}
}

func wifiConnectLabel(command string, opts WifiConnectOptions) string {
	parts := []string{"wifi", command, opts.SSID, "<redacted>"}
	appendRawValue(&parts, opts.Security)
	appendValueOption(&parts, "--bssid", opts.BSSID)
	appendValueOption(&parts, "--band", opts.Band)
	appendValueOption(&parts, "--mac-randomization", opts.MacRandomization)
	appendValueOption(&parts, "--timeout", opts.Timeout)
	return strings.Join(parts, " ")
}

func wifiCycleLabel(opts WifiCycleOptions) string {
	parts := []string{"wifi", "cycle", opts.SSID, "<redacted>"}
	appendRawValue(&parts, opts.Security)
	appendValueOption(&parts, "--count", opts.Count)
	appendValueOption(&parts, "--bssid", opts.BSSID)
	appendValueOption(&parts, "--band", opts.Band)
	appendValueOption(&parts, "--mac-randomization", opts.MacRandomization)
	appendValueOption(&parts, "--ping", opts.PingHost)
	appendValueOption(&parts, "--http", opts.HTTPURL)
	appendValueOption(&parts, "--pause", opts.Pause)
	appendValueOption(&parts, "--timeout", opts.Timeout)
	appendFlag(&parts, "--forget", opts.ForgetAfterEach)
	return strings.Join(parts, " ")
}

func wifiExpectationLabel(prefix []string, opts WifiExpectationOptions) string {
	parts := append([]string(nil), prefix...)
	if prefix[len(prefix)-1] == "connected" {
		appendRawValue(&parts, opts.SSID)
	} else {
		appendValueOption(&parts, "--ssid", opts.SSID)
	}
	appendValueOption(&parts, "--bssid", opts.BSSID)
	appendValueOption(&parts, "--security", opts.Security)
	appendValueOption(&parts, "--band", opts.Band)
	appendValueOption(&parts, "--timeout", opts.Timeout)
	appendFlag(&parts, "--ip", opts.RequireIP)
	appendFlag(&parts, "--validated", opts.RequireValidated)
	return strings.Join(parts, " ")
}
