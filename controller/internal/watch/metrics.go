package watch

import (
	"fmt"
	"net/netip"
	"regexp"
	"strconv"
	"strings"

	"dropcheck/controller/internal/controlpb"
)

// Value is a typed probe metric value used by expectation matchers.
type Value struct {
	value any
}

func stringValue(value string) Value { return Value{value: value} }
func intValue(value int64) Value     { return Value{value: value} }
func uintValue(value uint64) Value   { return Value{value: value} }
func floatValue(value float64) Value { return Value{value: value} }
func boolValue(value bool) Value     { return Value{value: value} }
func stringListValue(value []string) Value {
	return Value{value: append([]string(nil), value...)}
}

// String renders the metric value for logs and findings.
func (v Value) String() string {
	switch value := v.value.(type) {
	case nil:
		return "<missing>"
	case string:
		return value
	case bool:
		return strconv.FormatBool(value)
	case int64:
		return strconv.FormatInt(value, 10)
	case uint64:
		return strconv.FormatUint(value, 10)
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64)
	case []string:
		return strings.Join(value, ",")
	default:
		return fmt.Sprint(value)
	}
}

// Bool returns the metric value as a boolean when it can be interpreted as one.
func (v Value) Bool() (bool, bool) {
	switch value := v.value.(type) {
	case bool:
		return value, true
	case string:
		parsed, err := strconv.ParseBool(strings.ToLower(value))
		return parsed, err == nil
	default:
		return false, false
	}
}

// Strings returns the metric value as a string slice for list matchers.
func (v Value) Strings() []string {
	switch value := v.value.(type) {
	case nil:
		return nil
	case []string:
		return append([]string(nil), value...)
	case string:
		if strings.TrimSpace(value) == "" {
			return nil
		}
		return []string{value}
	default:
		return []string{v.String()}
	}
}

// Float returns the metric value as a float when numeric comparison is possible.
func (v Value) Float() (float64, bool) {
	switch value := v.value.(type) {
	case int64:
		return float64(value), true
	case uint64:
		return float64(value), true
	case float64:
		return value, true
	case string:
		parsed, err := strconv.ParseFloat(value, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func metricsForResult(result *controlpb.CommandResult) map[string]Value {
	metrics := map[string]Value{}
	if result == nil {
		return metrics
	}
	metrics["status"] = stringValue(statusName(result.GetStatus()))
	metrics["elapsed_ms"] = intValue(result.GetElapsedMs())
	switch payload := result.Payload.(type) {
	case *controlpb.CommandResult_WifiStatus:
		addWifiStatusMetrics(metrics, payload.WifiStatus)
	case *controlpb.CommandResult_IpStatus:
		addIPStatusMetrics(metrics, payload.IpStatus)
	case *controlpb.CommandResult_Ping:
		addPingMetrics(metrics, payload.Ping)
	case *controlpb.CommandResult_Traceroute:
		addTracerouteMetrics(metrics, payload.Traceroute)
	case *controlpb.CommandResult_PathMtu:
		addPathMTUMetrics(metrics, payload.PathMtu)
	case *controlpb.CommandResult_ResolveDns:
		addDNSMetrics(metrics, payload.ResolveDns)
	case *controlpb.CommandResult_HttpCheck:
		addHTTPMetrics(metrics, payload.HttpCheck)
	case *controlpb.CommandResult_GlobalIp:
		addGlobalIPMetrics(metrics, payload.GlobalIp)
	case *controlpb.CommandResult_Wget:
		addDownloadMetrics(metrics, payload.Wget)
	case *controlpb.CommandResult_WifiScanDetail:
		addWifiScanDetailMetrics(metrics, payload.WifiScanDetail)
	}
	return metrics
}

func addWifiStatusMetrics(metrics map[string]Value, status *controlpb.WifiStatus) {
	if status == nil {
		return
	}
	metrics["enabled"] = boolValue(status.GetEnabled())
	metrics["validated"] = boolValue(status.GetIpStatus().GetValidated())
	conn := status.GetConnection()
	if conn == nil {
		return
	}
	metrics["ssid"] = stringValue(conn.GetSsid())
	metrics["bssid"] = stringValue(conn.GetBssid())
	metrics["rssi"] = intValue(int64(conn.GetRssiDbm()))
	metrics["frequency_mhz"] = intValue(int64(conn.GetFrequencyMhz()))
	metrics["band"] = stringValue(bandFromFrequency(conn.GetFrequencyMhz()))
	metrics["standard"] = stringValue(conn.GetWifiStandard())
	metrics["security"] = stringValue(conn.GetSecurityType())
	metrics["link_speed_mbps"] = intValue(int64(conn.GetLinkSpeedMbps()))
	metrics["tx_link_speed_mbps"] = intValue(int64(conn.GetTxLinkSpeedMbps()))
	metrics["rx_link_speed_mbps"] = intValue(int64(conn.GetRxLinkSpeedMbps()))
	metrics["associated_mlo_link_count"] = intValue(int64(len(conn.GetAssociatedMloLinks())))
	metrics["affiliated_mlo_link_count"] = intValue(int64(len(conn.GetAffiliatedMloLinks())))
	metrics["channel_width"] = stringValue(conn.GetChannelWidth())
}

func addIPStatusMetrics(metrics map[string]Value, status *controlpb.IpStatus) {
	if status == nil {
		return
	}
	metrics["validated"] = boolValue(status.GetValidated())
	metrics["internet"] = boolValue(status.GetInternet())
	metrics["interface"] = stringValue(status.GetInterfaceName())
	metrics["mtu"] = uintValue(uint64(status.GetMtu()))
	metrics["address_count"] = intValue(int64(len(status.GetAddresses())))
	metrics["addresses"] = stringListValue(status.GetAddresses())
	metrics["ip_addresses"] = stringListValue(status.GetAddresses())
	ipv4Addresses, ipv6Addresses := splitIPListByFamily(status.GetAddresses())
	metrics["ipv4_addresses"] = stringListValue(ipv4Addresses)
	metrics["ipv6_addresses"] = stringListValue(ipv6Addresses)
	metrics["dns_server_count"] = intValue(int64(len(status.GetDnsServers())))
	metrics["dns_servers"] = stringListValue(status.GetDnsServers())
	ipv4DNSServers, ipv6DNSServers := splitIPListByFamily(status.GetDnsServers())
	metrics["ipv4_dns_servers"] = stringListValue(ipv4DNSServers)
	metrics["ipv6_dns_servers"] = stringListValue(ipv6DNSServers)
	metrics["route_count"] = intValue(int64(len(status.GetRoutes())))
	metrics["default_route"] = boolValue(hasDefaultRoute(status.GetRoutes()))
}

func addPingMetrics(metrics map[string]Value, ping *controlpb.PingResult) {
	if ping == nil {
		return
	}
	transmitted := uint64(ping.GetTransmitted())
	received := uint64(ping.GetReceived())
	lossPercent := ping.GetPacketLossPercent()
	minLatency := ping.GetMinMs()
	avgLatency := ping.GetAvgMs()
	maxLatency := ping.GetMaxMs()
	if parsed, ok := parsePingOutputMetrics(ping.GetOutput()); ok {
		transmitted = uint64(parsed.transmitted)
		received = uint64(parsed.received)
		lossPercent = parsed.lossPercent
		minLatency = parsed.minMs
		avgLatency = parsed.avgMs
		maxLatency = parsed.maxMs
	}
	metrics["host"] = stringValue(ping.GetHost())
	metrics["count"] = uintValue(uint64(ping.GetCount()))
	metrics["transmitted"] = uintValue(transmitted)
	metrics["received"] = uintValue(received)
	metrics["loss_percent"] = floatValue(lossPercent)
	metrics["min_latency_ms"] = floatValue(minLatency)
	metrics["avg_latency_ms"] = floatValue(avgLatency)
	metrics["max_latency_ms"] = floatValue(maxLatency)
	metrics["elapsed_ms"] = intValue(ping.GetElapsedMs())
}

type pingOutputMetrics struct {
	transmitted uint32
	received    uint32
	lossPercent float64
	minMs       float64
	avgMs       float64
	maxMs       float64
}

var (
	pingOutputSummary = regexp.MustCompile(`(?m)(\d+)\s+packets transmitted,\s+(\d+)\s+(?:packets\s+)?received,.*?([0-9.]+)%\s+packet loss`)
	pingOutputRTT     = regexp.MustCompile(`(?m)(?:rtt|round-trip) min/avg/max/(?:mdev|stddev) = ([0-9.]+)/([0-9.]+)/([0-9.]+)/[0-9.]+ ms`)
	tracerouteHopLine = regexp.MustCompile(`^\s*(\d{1,3})\s+(.+?)\s*$`)
)

func parsePingOutputMetrics(output string) (pingOutputMetrics, bool) {
	summary := pingOutputSummary.FindStringSubmatch(output)
	if summary == nil {
		return pingOutputMetrics{}, false
	}
	stats := pingOutputMetrics{
		transmitted: parseUint32Default(summary[1]),
		received:    parseUint32Default(summary[2]),
		lossPercent: parseFloatDefault(summary[3]),
	}
	if rtt := pingOutputRTT.FindStringSubmatch(output); rtt != nil {
		stats.minMs = parseFloatDefault(rtt[1])
		stats.avgMs = parseFloatDefault(rtt[2])
		stats.maxMs = parseFloatDefault(rtt[3])
	}
	return stats, true
}

func parseUint32Default(value string) uint32 {
	parsed, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return 0
	}
	return uint32(parsed)
}

func parseFloatDefault(value string) float64 {
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0
	}
	return parsed
}

func parseTracerouteOutputMetrics(output string, target string) (int64, bool, []string) {
	var hopCount int64
	reached := false
	hopIPs := []string(nil)
	target = strings.ToLower(strings.TrimSpace(target))
	for line := range strings.SplitSeq(output, "\n") {
		match := tracerouteHopLine.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		body := strings.TrimSpace(match[2])
		if body == "" || strings.TrimSpace(strings.ReplaceAll(body, "*", "")) == "" {
			continue
		}
		hopCount++
		hopIPs = append(hopIPs, ipLiteralsFromText(body)...)
		if target != "" && strings.Contains(strings.ToLower(body), target) {
			reached = true
		}
	}
	return hopCount, reached, uniqueStrings(hopIPs)
}

func addDNSMetrics(metrics map[string]Value, dns *controlpb.ResolveDnsResult) {
	if dns == nil {
		return
	}
	metrics["name"] = stringValue(dns.GetName())
	metrics["answer_count"] = intValue(int64(len(dns.GetAnswers())))
	var aCount int64
	var aaaaCount int64
	answers := make([]string, 0, len(dns.GetAnswers()))
	aAnswers := []string(nil)
	aaaaAnswers := []string(nil)
	for _, answer := range dns.GetAnswers() {
		address := answer.GetAddress()
		if address != "" {
			answers = append(answers, address)
		}
		switch answer.GetType() {
		case controlpb.DnsRecordType_DNS_RECORD_TYPE_A:
			aCount++
			if address != "" {
				aAnswers = append(aAnswers, address)
			}
		case controlpb.DnsRecordType_DNS_RECORD_TYPE_AAAA:
			aaaaCount++
			if address != "" {
				aaaaAnswers = append(aaaaAnswers, address)
			}
		}
	}
	metrics["answers"] = stringListValue(answers)
	metrics["a_answers"] = stringListValue(aAnswers)
	metrics["aaaa_answers"] = stringListValue(aaaaAnswers)
	metrics["a_count"] = intValue(aCount)
	metrics["aaaa_count"] = intValue(aaaaCount)
	metrics["elapsed_ms"] = intValue(dns.GetElapsedMs())
	metrics["error"] = stringValue(dns.GetError())
}

func addHTTPMetrics(metrics map[string]Value, http *controlpb.HttpCheckResult) {
	if http == nil {
		return
	}
	metrics["url"] = stringValue(http.GetUrl())
	metrics["status_code"] = uintValue(uint64(http.GetStatus()))
	metrics["expected_status"] = uintValue(uint64(http.GetExpectedStatus()))
	metrics["matched"] = boolValue(http.GetMatched())
	metrics["elapsed_ms"] = intValue(http.GetElapsedMs())
	metrics["error"] = stringValue(http.GetError())
}

func addGlobalIPMetrics(metrics map[string]Value, globalIP *controlpb.GlobalIpResult) {
	if globalIP == nil {
		return
	}
	metrics["address_count"] = intValue(int64(len(globalIP.GetAddresses())))
	var globalCount int64
	var errorCount int64
	var ipv4Count int64
	var ipv6Count int64
	globalIPs := []string(nil)
	ipv4GlobalIPs := []string(nil)
	ipv6GlobalIPs := []string(nil)
	for _, address := range globalIP.GetAddresses() {
		if address.GetGlobal() {
			globalCount++
		}
		if address.GetError() != "" {
			errorCount++
		}
		switch address.GetFamily() {
		case controlpb.IpFamily_IP_FAMILY_IPV4:
			ipv4Count++
			if address.GetIp() != "" {
				ipv4GlobalIPs = append(ipv4GlobalIPs, address.GetIp())
			}
		case controlpb.IpFamily_IP_FAMILY_IPV6:
			ipv6Count++
			if address.GetIp() != "" {
				ipv6GlobalIPs = append(ipv6GlobalIPs, address.GetIp())
			}
		}
		if address.GetIp() != "" {
			globalIPs = append(globalIPs, address.GetIp())
		}
	}
	metrics["global_ips"] = stringListValue(globalIPs)
	metrics["ipv4_global_ips"] = stringListValue(ipv4GlobalIPs)
	metrics["ipv6_global_ips"] = stringListValue(ipv6GlobalIPs)
	metrics["global_count"] = intValue(globalCount)
	metrics["error_count"] = intValue(errorCount)
	metrics["ipv4_count"] = intValue(ipv4Count)
	metrics["ipv6_count"] = intValue(ipv6Count)
	metrics["elapsed_ms"] = intValue(globalIP.GetElapsedMs())
	metrics["interface"] = stringValue(globalIP.GetInterfaceName())
	metrics["error"] = stringValue(globalIP.GetError())
}

func addTracerouteMetrics(metrics map[string]Value, traceroute *controlpb.TracerouteResult) {
	if traceroute == nil {
		return
	}
	hopCount, reached, hopIPs := parseTracerouteOutputMetrics(traceroute.GetOutput(), traceroute.GetHost())
	metrics["host"] = stringValue(traceroute.GetHost())
	metrics["max_hops"] = uintValue(uint64(traceroute.GetMaxHops()))
	metrics["size_bytes"] = uintValue(uint64(traceroute.GetSizeBytes()))
	metrics["exit_code"] = intValue(int64(traceroute.GetExitCode()))
	metrics["interface"] = stringValue(traceroute.GetInterfaceName())
	metrics["executable"] = stringValue(traceroute.GetExecutable())
	metrics["hop_count"] = intValue(hopCount)
	metrics["hop_ips"] = stringListValue(hopIPs)
	ipv4Hops, ipv6Hops := splitIPListByFamily(hopIPs)
	metrics["ipv4_hop_ips"] = stringListValue(ipv4Hops)
	metrics["ipv6_hop_ips"] = stringListValue(ipv6Hops)
	metrics["reached"] = boolValue(reached)
	metrics["elapsed_ms"] = intValue(traceroute.GetElapsedMs())
	metrics["error"] = stringValue(traceroute.GetError())
}

func addPathMTUMetrics(metrics map[string]Value, pathMTU *controlpb.PathMtuResult) {
	if pathMTU == nil {
		return
	}
	var passedProbes int64
	for _, probe := range pathMTU.GetProbes() {
		if probe.GetPassed() {
			passedProbes++
		}
	}
	metrics["host"] = stringValue(pathMTU.GetHost())
	metrics["discovered"] = boolValue(pathMTU.GetDiscovered())
	metrics["path_mtu_bytes"] = uintValue(uint64(pathMTU.GetPathMtuBytes()))
	metrics["payload_size_bytes"] = uintValue(uint64(pathMTU.GetPayloadSizeBytes()))
	metrics["min_mtu_bytes"] = uintValue(uint64(pathMTU.GetMinMtuBytes()))
	metrics["max_mtu_bytes"] = uintValue(uint64(pathMTU.GetMaxMtuBytes()))
	metrics["ip_overhead_bytes"] = uintValue(uint64(pathMTU.GetIpOverheadBytes()))
	metrics["probe_count"] = intValue(int64(len(pathMTU.GetProbes())))
	metrics["passed_probe_count"] = intValue(passedProbes)
	metrics["elapsed_ms"] = intValue(pathMTU.GetElapsedMs())
	metrics["interface"] = stringValue(pathMTU.GetInterfaceName())
	metrics["error"] = stringValue(pathMTU.GetError())
}

func addDownloadMetrics(metrics map[string]Value, download *controlpb.WgetResult) {
	if download == nil {
		return
	}
	metrics["url"] = stringValue(download.GetUrl())
	metrics["status_code"] = uintValue(uint64(download.GetStatus()))
	metrics["content_type"] = stringValue(download.GetContentType())
	metrics["content_length"] = intValue(download.GetContentLength())
	metrics["bytes_read"] = uintValue(download.GetBytesRead())
	metrics["elapsed_ms"] = intValue(download.GetElapsedMs())
	metrics["throughput_bps"] = floatValue(download.GetThroughputBps())
	metrics["interface"] = stringValue(download.GetInterfaceName())
	metrics["error"] = stringValue(download.GetError())
}

func addWifiScanDetailMetrics(metrics map[string]Value, detail *controlpb.WifiScanDetail) {
	if detail == nil {
		return
	}
	results := detail.GetResults()
	metrics["visible"] = boolValue(len(results) > 0)
	metrics["result_count"] = intValue(int64(len(results)))
	metrics["error_count"] = intValue(int64(len(detail.GetErrors())))
	if len(results) == 0 {
		return
	}
	first := results[0]
	metrics["ssid"] = stringValue(first.GetSsid())
	metrics["bssid"] = stringValue(first.GetBssid())
	metrics["rssi"] = intValue(int64(first.GetRssiDbm()))
	metrics["band"] = stringValue(firstNonEmpty(first.GetBand(), bandFromFrequency(first.GetFrequencyMhz())))
	metrics["standard"] = stringValue(first.GetWifiStandard())
	metrics["security"] = stringValue(strings.Join(first.GetSecurityTypes(), ","))
	metrics["channel_width"] = stringValue(first.GetChannelWidth())
}

func hasDefaultRoute(routes []string) bool {
	for _, route := range routes {
		if strings.Contains(route, "0.0.0.0/0") || strings.Contains(route, "::/0") || strings.Contains(strings.ToLower(route), "default") {
			return true
		}
	}
	return false
}

func splitIPListByFamily(values []string) ([]string, []string) {
	ipv4 := []string(nil)
	ipv6 := []string(nil)
	for _, value := range values {
		addr, ok := parseIPLiteral(value)
		if !ok {
			continue
		}
		if addr.Is4() {
			ipv4 = append(ipv4, value)
		} else if addr.Is6() {
			ipv6 = append(ipv6, value)
		}
	}
	return ipv4, ipv6
}

func ipLiteralsFromText(text string) []string {
	replacer := strings.NewReplacer("(", " ", ")", " ", "[", " ", "]", " ", ",", " ", ";", " ", "\t", " ")
	fields := strings.Fields(replacer.Replace(text))
	values := make([]string, 0, len(fields))
	for _, field := range fields {
		addr, ok := parseIPLiteral(field)
		if !ok {
			continue
		}
		values = append(values, addr.String())
	}
	return uniqueStrings(values)
}

func parseIPLiteral(value string) (netip.Addr, bool) {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, `"'<>[]{},;`)
	if value == "" {
		return netip.Addr{}, false
	}
	if before, _, ok := strings.Cut(value, "/"); ok {
		value = before
	}
	if before, _, ok := strings.Cut(value, "%"); ok {
		value = before
	}
	if strings.Count(value, ".") == 3 {
		value = strings.TrimRight(value, ":")
	}
	addr, err := netip.ParseAddr(value)
	if err != nil {
		return netip.Addr{}, false
	}
	return addr, true
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func bandFromFrequency(frequency int32) string {
	switch {
	case frequency >= 2400 && frequency < 2500:
		return "2.4ghz"
	case frequency >= 4900 && frequency < 5900:
		return "5ghz"
	case frequency >= 5925 && frequency < 7125:
		return "6ghz"
	case frequency >= 57000 && frequency < 71000:
		return "60ghz"
	default:
		return ""
	}
}

func statusName(status controlpb.CommandResult_Status) string {
	name := strings.ToLower(strings.TrimPrefix(status.String(), "STATUS_"))
	if name == "" || name == "unspecified" {
		return "unspecified"
	}
	return name
}
