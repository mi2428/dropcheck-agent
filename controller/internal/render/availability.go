package render

import (
	"fmt"
	"strings"

	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
)

// Observation metadata is the presence authority. Scalar values never establish it.
func observedValue(fields []*controlpb.DiagnosticField, field, value string) string {
	metadata := diagnosticFieldMap(fields)
	switch metadata[field+".state"] {
	case "available":
		if value == "" {
			return "none"
		}
		return value
	case "unavailable":
		return "? (" + field + ": " + safePresentationText(empty(metadata[field+".reason"], "unavailable; reason not supplied")) + ")"
	case "":
		return "? (" + field + ": observation metadata unavailable)"
	default:
		return "? (invalid " + field + ".state=" + metadata[field+".state"] + ")"
	}
}

func observedBool(fields []*controlpb.DiagnosticField, field string, value bool) string {
	text := "no"
	if value {
		text = "yes"
	}
	return observedValue(fields, field, text)
}

func observedList(fields []*controlpb.DiagnosticField, field string, values []string) string {
	return observedValue(fields, field, strings.Join(values, "\n"))
}

func scanObservationAge(ap *controlpb.WifiScanResult) string {
	if ap.ObservationAgeMs == nil {
		return "? (observation timestamp unavailable)"
	}
	return fmt.Sprintf("%dms", ap.GetObservationAgeMs())
}

func scanAgeSeconds(ap *controlpb.WifiScanResult) string {
	if ap.ObservationAgeMs == nil {
		return "?"
	}
	ms := ap.GetObservationAgeMs()
	if ms%1000 == 0 {
		return fmt.Sprint(ms / 1000)
	}
	return fmt.Sprintf("%d.%s", ms/1000, strings.TrimRight(fmt.Sprintf("%03d", ms%1000), "0"))
}

func scanAgeSummary(scan *controlpb.WifiScan) string {
	known := 0
	var youngest, oldest uint64
	for _, ap := range scan.GetResults() {
		if ap.ObservationAgeMs == nil {
			continue
		}
		age := ap.GetObservationAgeMs()
		if known == 0 || age < youngest {
			youngest = age
		}
		if known == 0 || age > oldest {
			oldest = age
		}
		known++
	}
	if known == 0 {
		return "? (observation timestamps unavailable)"
	}
	return fmt.Sprintf("%d-%dms at collection; known=%d/%d", youngest, oldest, known, len(scan.GetResults()))
}

func renderIPView(b *strings.Builder, status *controlpb.IpStatus, view Presentation) {
	if status == nil {
		writeRecord(b, "Note", "? (IP payload unavailable)")
		return
	}
	fields := status.GetObservationFields()
	metadata := diagnosticFieldMap(fields)
	writeRecord(b, "Network", empty(status.GetNetworkId(), "?"))
	writeRecord(b, "Transports", observedList(fields, "capabilities", status.GetTransports()))
	writeRecord(b, "Interface", observedValue(fields, "link_properties", status.GetInterfaceName()))
	writeRecord(b, "Default network", observedValue(fields, "default_network", metadata["default_network_id"]))
	selected := metadata["selected_is_default"]
	switch selected {
	case "true":
		selected = "yes"
	case "false":
		selected = "no"
	case "":
		selected = "? (selected_is_default not supplied)"
	}
	writeRecord(b, "Selected is default", observedValue(fields, "default_network", selected))
	if metadata["default_network.state"] == "available" && metadata["selected_is_default"] == "false" {
		writeRecord(b, "Note", "selected Wi-Fi differs from Android default Network; capability validation is not an active probe verdict")
	}
	writeRecord(b, "Internet", observedBool(fields, "capabilities", status.GetInternet()))
	writeRecord(b, "Validated", observedBool(fields, "capabilities", status.GetValidated()))
	writeRecord(b, "MTU", observedValue(fields, "link_properties", fmt.Sprint(status.GetMtu())))
	ipv4, ipv6, other := splitIPAddresses(status.GetAddresses())
	for _, family := range []struct {
		name   string
		values []string
	}{{"IPv4", ipv4}, {"IPv6", ipv6}, {"Other addresses", other}} {
		writeSection(b, family.name)
		value := observedList(fields, "link_properties", family.values)
		var body strings.Builder
		for line := range strings.SplitSeq(value, "\n") {
			writeRecord(&body, "Addr", line)
		}
		writeNested(b, body.String())
	}
	writeBlankLine(b)
	for _, field := range []struct {
		label  string
		values []string
	}{{"Routes", status.GetRoutes()}, {"DNS", status.GetDnsServers()}} {
		for line := range strings.SplitSeq(observedList(fields, "link_properties", field.values), "\n") {
			writeRecord(b, field.label, line)
		}
	}
	writeRecord(b, "DHCP server", observedValue(fields, "link_properties", status.GetDhcpServer()))
	privateDNS := "off"
	if status.GetPrivateDnsActive() {
		privateDNS = "on; server=" + empty(status.GetPrivateDnsServerName(), "none")
	} else {
		privateDNS += "; server=" + empty(status.GetPrivateDnsServerName(), "none")
	}
	writeRecord(b, "Private DNS", observedValue(fields, "link_properties", privateDNS))
	writeRecord(b, "NAT64", observedValue(fields, "link_properties", status.GetNat64Prefix()))
	if view.Detail || !view.DetailAvailable {
		writeBlankLine(b)
		writeRecord(b, "Capabilities", observedList(fields, "capabilities", status.GetCapabilities()))
		writeRecord(b, "Bandwidth", observedValue(fields, "capabilities", networkBandwidth(status)))
		writeRecord(b, "Signal strength", observedValue(fields, "capabilities", fmt.Sprint(status.GetSignalStrength())))
		writeRecord(b, "Specifier", observedValue(fields, "capabilities", status.GetNetworkSpecifier()))
		writeRecord(b, "Owner uid", observedValue(fields, "capabilities", fmt.Sprint(status.GetOwnerUid())))
		writeRecord(b, "Enterprise IDs", observedList(fields, "capabilities", status.GetEnterpriseIds()))
		var subscriptions []string
		for _, id := range status.GetSubscriptionIds() {
			subscriptions = append(subscriptions, fmt.Sprint(id))
		}
		writeRecord(b, "Subscription IDs", observedList(fields, "capabilities", subscriptions))
		writeRecord(b, "Domains", observedValue(fields, "link_properties", status.GetDomains()))
		writeRecord(b, "Proxy", observedValue(fields, "link_properties", status.GetHttpProxy()))
		writeRecord(b, "Wake on LAN", observedBool(fields, "link_properties", status.GetWakeOnLanSupported()))
		writeKVSection(b, "IPv6 RA (android)", kv("source", "android"), kv("values", multiLineValue(diagnosticFieldsForDetail(status.GetIpv6Ra()))))
		if status.GetWifi() != nil {
			writeSection(b, "WifiInfo from this NetworkCapabilities observation")
			var wifi strings.Builder
			renderWifiConnection(&wifi, status.GetWifi())
			conn := status.GetWifi()
			writeRecord(&wifi, "Network configuration ID", conn.GetNetworkId())
			writeRecord(&wifi, "Link speed Mbps", conn.GetLinkSpeedMbps())
			writeRecord(&wifi, "WifiInfo IPv4 (separate observation)", conn.GetIpv4Address())
			writeRecord(&wifi, "Subscription ID", conn.GetSubscriptionId())
			writeRecord(&wifi, "Applicable redactions", conn.GetApplicableRedactions())
			if !conn.GetHiddenSsid() {
				writeRecord(&wifi, "Hidden SSID", false)
			}
			if !conn.GetRestricted() {
				writeRecord(&wifi, "Restricted", false)
			}
			writeRecord(&wifi, "MLD", observedValue(conn.GetObservationFields(), "ap_mld_mac_address", conn.GetApMldMacAddress()))
			writeRecord(&wifi, "AP link ID", observedValue(conn.GetObservationFields(), "ap_mlo_link_id", fmt.Sprint(conn.GetApMloLinkId())))
			renderMLOLinks(&wifi, "Associated links", status.GetWifi().GetAssociatedMloLinks(), true)
			renderMLOLinks(&wifi, "Affiliated links", status.GetWifi().GetAffiliatedMloLinks(), true)
			renderInformationElements(&wifi, status.GetWifi().GetInformationElements())
			renderDiagnosticFields(&wifi, status.GetWifi().GetObservationFields())
			writeNested(b, wifi.String())
		}
		renderDiagnosticFields(b, fields)
	}
}
