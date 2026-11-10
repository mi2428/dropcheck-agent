package render

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"github.com/mi2428/dropcheck-agent/controller/internal/oui"
)

// Values share one column; wrapped/list values hang beneath that column.
func writeWifiSummaryGroup(b *strings.Builder, title string, view Presentation, rows ...kvRow) {
	if strings.HasPrefix(title, " ") {
		fmt.Fprintln(b, title)
	} else {
		writeSection(b, title)
	}
	indent := len(title) - len(strings.TrimLeft(title, " ")) + 2
	if len(rows) == 0 {
		rows = []kvRow{kv("Observation", "? (not present in this snapshot)")}
	}
	column := 29
	for _, row := range rows {
		column = max(column, indent+displayWidth(strings.TrimRight(cleanDisplayCell(row.label), " "))+1)
	}
	for _, row := range rows {
		label := strings.Repeat(" ", indent) + strings.TrimRight(cleanDisplayCell(row.label), " ")
		prefix := padDisplayEnd(label+":", column) + " "
		if view.Width > 0 && displayWidth(prefix)+8 > view.Width {
			writeFieldLabel(b, label, view.Width)
			prefix = strings.Repeat(" ", indent+2)
		}
		for value := range strings.SplitSeq(empty(row.value, "none"), "\n") {
			value = cleanDisplayCell(value)
			if view.Width > 0 {
				value = ansi.Hardwrap(value, max(1, view.Width-displayWidth(prefix)), true)
			}
			for line := range strings.SplitSeq(value, "\n") {
				fmt.Fprintln(b, prefix+line)
				prefix = strings.Repeat(" ", displayWidth(prefix))
			}
		}
	}
}

func renderWifiSummary(b *strings.Builder, status *controlpb.WifiStatus, view Presentation) {
	group := func(title string, rows ...kvRow) { writeWifiSummaryGroup(b, title, view, rows...) }
	if status == nil {
		group("Connection", kv("Observation", "? (Wi-Fi payload unavailable)"))
		return
	}
	radio := "off"
	if status.Enabled {
		radio = "on"
	}
	conn := status.GetConnection()
	link := observedValue(status.ObservationFields, "connection", empty(conn.GetDetailedState(), conn.GetSupplicantState()))
	if conn == nil && link == "none" {
		link = "disconnected"
	}
	writeSection(b, "Connection")
	group("  State", kv("Radio", fmt.Sprintf("%s; framework=%s", observedValue(status.ObservationFields, "radio", radio), status.State)),
		kv("Networks", fmt.Sprintf("active=%s; Wi-Fi=%d", empty(status.ActiveNetwork, "none"), status.WifiNetworkCount)),
		kv("Link", link))
	if conn != nil {
		observed := func(field, value string) string { return observedValue(conn.ObservationFields, field, value) }
		ssid, bssid := cleanDisplayCell(conn.Ssid), cleanDisplayCell(conn.Bssid)
		if ssid == "" || strings.EqualFold(ssid, "<unknown ssid>") {
			ssid = "? (identity unavailable)"
		}
		if !wifiMLOKnownBSSID(bssid) {
			bssid = "?"
		}
		if strings.EqualFold(conn.DetailedState, "DISCONNECTED") {
			ssid, bssid = "none", "none"
		}
		passpoint := "none"
		if conn.PasspointFqdn != "" || conn.PasspointProviderFriendlyName != "" || conn.PasspointUniqueId != "" {
			passpoint = fmt.Sprintf("fqdn=%s; provider=%s; unique-id=%s", empty(conn.PasspointFqdn, "none"), empty(conn.PasspointProviderFriendlyName, "none"), empty(conn.PasspointUniqueId, "none"))
		}
		group("  Identity", kv("SSID", observed("identity", ssid)), kv("BSSID", observed("identity", bssid)),
			kv("BSSID vendor", observed("identity", oui.Vendor(bssid))),
			kv("STA MAC", conn.MacAddress), kv("Configuration", fmt.Sprintf("network-id=%d; subscription-id=%d", conn.NetworkId, conn.SubscriptionId)), kv("Supplicant state", conn.SupplicantState),
			kv("Flags", fmt.Sprintf("hidden=%t; restricted=%t; redactions=%s", conn.HiddenSsid, conn.Restricted, empty(conn.ApplicableRedactions, "none"))), kv("Passpoint", passpoint))
		group("  Radio", kv("Channel", fmt.Sprintf("%s; ch=%s; width=%s; %dMHz", wifiBandFromFrequency(conn.FrequencyMhz), wifiChannelFromFrequency(conn.FrequencyMhz), empty(wifiChannelWidth(conn), "?"), conn.FrequencyMhz)),
			kv("PHY", conn.WifiStandard), kv("Signal", fmt.Sprintf("%s; level=%s", observed("rssi", fmt.Sprintf("%ddBm", conn.RssiDbm)), empty(wifiSignalLevel(conn), "? (level unavailable)"))),
			kv("Link rates", fmt.Sprintf("tx=%s; rx=%s; link=%dMbps", observed("tx_link_speed_mbps", fmt.Sprintf("%dMbps", conn.TxLinkSpeedMbps)), observed("rx_link_speed_mbps", fmt.Sprintf("%dMbps", conn.RxLinkSpeedMbps)), conn.LinkSpeedMbps)),
			kv("Reported maximum rates", empty(wifiMaxLink(conn), "? (not supplied)")+"; availability unknown, not current rates"))
		security := []kvRow{kv("Mode", conn.SecurityType)}
		if conn.SecurityDetails != nil {
			security = append(security, wifiSecurityDetailRows(conn.SecurityDetails)...)
		} else {
			security = append(security, kv("Decoded security", "? (RSN/RSNXE observations unavailable)"))
		}
		group("  Security", security...)
		var capabilities []kvRow
		for _, row := range wifiAPCapabilityRows(conn) {
			if row.label != "hidden" && row.label != "restricted" && row.label != "max_link" && !strings.HasPrefix(row.label, "passpoint_") {
				capabilities = append(capabilities, row)
			}
		}
		group("  AP capabilities (decoded)", capabilities...)
		group("  HE/EHT (decoded)", wifiDetailedCapabilityRows(conn)...)
		group("  MLO", kv("MLD", observed("ap_mld_mac_address", conn.ApMldMacAddress)),
			kv("AP link ID", observed("ap_mlo_link_id", fmt.Sprint(conn.ApMloLinkId))),
			kv("Links", fmt.Sprintf("associated=%s; affiliated=%s", observed("associated_mlo_links", fmt.Sprint(len(conn.AssociatedMloLinks))), observed("affiliated_mlo_links", fmt.Sprint(len(conn.AffiliatedMloLinks))))))
		for _, links := range []struct {
			name string
			rows []*controlpb.MloLinkInfo
		}{{"Associated", conn.AssociatedMloLinks}, {"Affiliated", conn.AffiliatedMloLinks}} {
			for i, link := range links.rows {
				group(fmt.Sprintf("    %s link %d", links.name, i+1),
					kv("Link ID", observedValue(link.ObservationFields, "identity", fmt.Sprint(link.LinkId))),
					kv("AP MAC", observedValue(link.ObservationFields, "identity", link.ApMacAddress)),
					kv("AP MAC vendor", observedValue(link.ObservationFields, "identity", oui.Vendor(link.ApMacAddress))),
					kv("STA MAC", observedValue(link.ObservationFields, "identity", link.StaMacAddress)),
					kv("State", observedValue(link.ObservationFields, "identity", link.State)),
					kv("Band/channel", observedValue(link.ObservationFields, "identity", fmt.Sprintf("%s/%d", link.Band, link.Channel))),
					kv("RSSI", observedValue(link.ObservationFields, "rates", fmt.Sprintf("%ddBm", link.RssiDbm))),
					kv("Tx/Rx link rates", observedValue(link.ObservationFields, "rates", fmt.Sprintf("%d/%dMbps", link.TxLinkSpeedMbps, link.RxLinkSpeedMbps))))
			}
		}
	}
	writeSection(b, "IP (separate Network observation)")
	if ip := status.GetIpStatus(); ip != nil {
		fields := ip.ObservationFields
		metadata := diagnosticFieldMap(fields)
		properties := func(value string) string { return observedValue(fields, "link_properties", value) }
		group("  Selection", kv("Network", ip.NetworkId), kv("Interface", properties(ip.InterfaceName)),
			kv("Transports", observedList(fields, "capabilities", ip.Transports)),
			kv("Default Network", observedValue(fields, "default_network", fmt.Sprintf("%s; selected=%s", empty(metadata["default_network_id"], "? (not supplied)"), empty(metadata["selected_is_default"], "? (not supplied)")))),
			kv("Connectivity", observedValue(fields, "capabilities", fmt.Sprintf("internet=%t; validated=%t", ip.Internet, ip.Validated))))
		ipv4, ipv6, other := splitIPAddresses(ip.Addresses)
		group("  IPv4", kv("Addresses", observedList(fields, "link_properties", ipv4)))
		group("  IPv6", kv("Addresses", observedList(fields, "link_properties", ipv6)))
		if len(other) != 0 {
			group("  Other address families", kv("Addresses", observedList(fields, "link_properties", other)))
		}
		group("  Routes", kv("Routes", observedList(fields, "link_properties", ip.Routes)))
		group("  DNS", kv("Servers", observedList(fields, "link_properties", ip.DnsServers)), kv("Domains", properties(ip.Domains)),
			kv("Private DNS", properties(fmt.Sprintf("active=%t; server=%s", ip.PrivateDnsActive, empty(ip.PrivateDnsServerName, "none")))))
		group("  Link properties", kv("MTU", properties(fmt.Sprint(ip.Mtu))), kv("DHCP server", properties(ip.DhcpServer)),
			kv("NAT64", properties(ip.Nat64Prefix)), kv("Proxy", properties(ip.HttpProxy)), kv("Wake on LAN", observedBool(fields, "link_properties", ip.WakeOnLanSupported)))
		var subscriptions []string
		for _, id := range ip.SubscriptionIds {
			subscriptions = append(subscriptions, fmt.Sprint(id))
		}
		group("  Network capabilities", kv("Capabilities", observedList(fields, "capabilities", ip.Capabilities)),
			kv("Bandwidth estimate", observedValue(fields, "capabilities", networkBandwidth(ip))),
			kv("Signal strength", observedValue(fields, "capabilities", fmt.Sprint(ip.SignalStrength))),
			kv("Specifier", observedValue(fields, "capabilities", ip.NetworkSpecifier)),
			kv("Ownership", observedValue(fields, "capabilities", fmt.Sprintf("uid=%d; enterprise=%s; subscriptions=%s", ip.OwnerUid, empty(strings.Join(ip.EnterpriseIds, ","), "none"), empty(strings.Join(subscriptions, ","), "none")))))
		var ra []kvRow
		for _, field := range ip.Ipv6Ra {
			ra = append(ra, kv(field.Key, field.Value))
		}
		group("  IPv6 RA (Android)", ra...)
	}
	if status.IpStatus == nil {
		group("  Selection", kv("Observation", "? (IP payload unavailable)"))
	}
	var permissions []kvRow
	for _, entry := range status.Permissions {
		name, value, ok := strings.Cut(entry, "=")
		if !ok {
			name, value = "Permission", entry
		}
		permissions = append(permissions, kv(name, value))
	}
	group("Permissions", permissions...)
	group("Details", kv("More", "show wifi status full; help show wifi status"),
		kv("Validation scope", "Android capability observation, not an active connectivity test"))
}
