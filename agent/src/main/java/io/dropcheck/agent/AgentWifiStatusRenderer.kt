package io.dropcheck.agent

import io.dropcheck.agent.grpc.IpStatus
import io.dropcheck.agent.grpc.WifiConnection
import io.dropcheck.agent.grpc.WifiEhtCapabilities
import io.dropcheck.agent.grpc.WifiEhtOperation
import io.dropcheck.agent.grpc.WifiHe6GhzCapabilities
import io.dropcheck.agent.grpc.WifiHeCapabilities
import io.dropcheck.agent.grpc.WifiHeMuEdcaParameterSet
import io.dropcheck.agent.grpc.WifiHeOperation
import io.dropcheck.agent.grpc.WifiHeSpatialReuseParameterSet
import io.dropcheck.agent.grpc.WifiHeUoraParameterSet
import io.dropcheck.agent.grpc.WifiInformationElement
import io.dropcheck.agent.grpc.WifiMcsNssSupport
import io.dropcheck.agent.grpc.WifiSecurityDetails
import io.dropcheck.agent.grpc.WifiStatus

/** Typed presentation used by Agent Shell for Wi-Fi and IP observations. */
internal object AgentWifiStatusRenderer {
    fun securitySummary(details: WifiSecurityDetails): String = wifiSecurityDetailRows(details).joinToString("; ") { "${it.first}=${it.second}" }
    fun summary(status: WifiStatus): List<AgentBlockPart> = buildList {
        fun heading(title: String) {
            if (isNotEmpty() && !title.startsWith(' ')) add(AgentBlockPart.Text(""))
            add(AgentBlockPart.Text(title))
        }
        fun group(title: String, vararg rows: Pair<String, Any?>) {
            heading(title)
            val indent = title.takeWhile { it == ' ' } + "  "
            val values = rows.toList().ifEmpty { listOf("Observation" to "? (not present in this snapshot)") }
            values.forEach { (label, value) ->
                add(AgentBlockPart.Field((indent + label).padEnd(28), value?.toString().orEmpty().ifEmpty { "none" }))
            }
        }
        val fields = status.observationFieldsList
        val conn = activeWifiConnection(status)
        heading("Connection")
        val link = AgentObservationPresentation.value(fields, "connection", conn?.detailedState.orEmpty().ifEmpty { conn?.supplicantState.orEmpty() })
        group("  State", "Radio" to "${AgentObservationPresentation.value(fields, "radio", if (status.enabled) "on" else "off")}; framework=${status.state}",
            "Networks" to "active=${status.activeNetwork.ifEmpty { "none" }}; Wi-Fi=${status.wifiNetworkCount}",
            "Link" to if (conn == null && link == "none") "disconnected" else link)
        if (conn != null) {
            fun observed(field: String, value: String) = AgentObservationPresentation.value(conn.observationFieldsList, field, value)
            val disconnected = conn.detailedState.equals("DISCONNECTED", ignoreCase = true)
            val passpoint = if (listOf(conn.passpointFqdn, conn.passpointProviderFriendlyName, conn.passpointUniqueId).all(String::isEmpty)) "none"
                else "fqdn=${conn.passpointFqdn.ifEmpty { "none" }}; provider=${conn.passpointProviderFriendlyName.ifEmpty { "none" }}; unique-id=${conn.passpointUniqueId.ifEmpty { "none" }}"
            group("  Identity", "SSID" to observed("identity", if (disconnected) "none" else conn.ssid.takeUnless { it.isEmpty() || it.equals("<unknown ssid>", ignoreCase = true) } ?: "? (identity unavailable)"),
                "BSSID" to observed("identity", if (disconnected) "none" else conn.bssid.takeIf(::isKnownWifiBssid) ?: "?"),
                "STA MAC" to conn.macAddress, "Configuration" to "network-id=${conn.networkId}; subscription-id=${conn.subscriptionId}", "Supplicant state" to conn.supplicantState,
                "Flags" to "hidden=${conn.hiddenSsid}; restricted=${conn.restricted}; redactions=${conn.applicableRedactions.ifEmpty { "none" }}", "Passpoint" to passpoint)
            group("  Radio", "Channel" to "${wifiBandFromFrequency(conn.frequencyMhz)}; ch=${wifiChannelFromFrequency(conn.frequencyMhz)}; width=${formatWifiChannelWidth(conn.channelWidth).ifEmpty { "?" }}; ${conn.frequencyMhz}MHz",
                "PHY" to conn.wifiStandard, "Signal" to "${observed("rssi", "${conn.rssiDbm}dBm")}; level=${wifiSignalLevel(conn).ifEmpty { "? (level unavailable)" }}",
                "Link rates" to "tx=${observed("tx_link_speed_mbps", "${conn.txLinkSpeedMbps}Mbps")}; rx=${observed("rx_link_speed_mbps", "${conn.rxLinkSpeedMbps}Mbps")}; link=${conn.linkSpeedMbps}Mbps",
                "Reported maximum rates" to "${wifiMaxLink(conn) ?: "? (not supplied)"}; availability unknown, not current rates")
            val security = listOf("Mode" to conn.securityType) + if (conn.hasSecurityDetails()) wifiSecurityDetailRows(conn.securityDetails)
                else listOf("Decoded security" to "? (RSN/RSNXE observations unavailable)")
            group("  Security", *security.toTypedArray())
            group("  AP capabilities (decoded)", *apCapabilityRows(conn).filterNot { it.first in listOf("hidden", "restricted", "max_link") || it.first.startsWith("passpoint_") }.toTypedArray())
            group("  HE/EHT (decoded)", *detailedWifiCapabilityRows(conn).toTypedArray())
            group("  MLO", "MLD" to observed("ap_mld_mac_address", conn.apMldMacAddress), "AP link ID" to observed("ap_mlo_link_id", conn.apMloLinkId.toString()),
                "Links" to "associated=${observed("associated_mlo_links", conn.associatedMloLinksCount.toString())}; affiliated=${observed("affiliated_mlo_links", conn.affiliatedMloLinksCount.toString())}")
            for ((name, links) in listOf("Associated" to conn.associatedMloLinksList, "Affiliated" to conn.affiliatedMloLinksList)) {
                links.forEachIndexed { index, item ->
                    fun linkValue(field: String, value: String) = AgentObservationPresentation.value(item.observationFieldsList, field, value)
                    group("    $name link ${index + 1}", "Link ID" to linkValue("identity", item.linkId.toString()),
                        "AP MAC" to linkValue("identity", item.apMacAddress), "STA MAC" to linkValue("identity", item.staMacAddress),
                        "State" to linkValue("identity", item.state), "Band/channel" to linkValue("identity", "${item.band}/${item.channel}"),
                        "RSSI" to linkValue("rates", "${item.rssiDbm}dBm"), "Tx/Rx link rates" to linkValue("rates", "${item.txLinkSpeedMbps}/${item.rxLinkSpeedMbps}Mbps"))
                }
            }
        }
        heading("IP (separate Network observation)")
        if (status.hasIpStatus()) {
            val ip = status.ipStatus
            val metadata = ip.observationFieldsList.associate { it.key to it.value }
            fun observed(field: String, value: String) = AgentObservationPresentation.value(ip.observationFieldsList, field, value)
            fun properties(value: String) = observed("link_properties", value)
            fun values(field: String, items: List<String>) = AgentObservationPresentation.list(ip.observationFieldsList, field, items)
            fun bool(field: String, value: Boolean) = AgentObservationPresentation.bool(ip.observationFieldsList, field, value)
            group("  Selection", "Network" to ip.networkId, "Interface" to properties(ip.interfaceName), "Transports" to values("capabilities", ip.transportsList),
                "Default Network" to observed("default_network", "${metadata["default_network_id"] ?: "? (not supplied)"}; selected=${metadata["selected_is_default"] ?: "? (not supplied)"}"),
                "Connectivity" to observed("capabilities", "internet=${ip.internet}; validated=${ip.validated}"))
            val addresses = splitIPAddresses(ip.addressesList)
            group("  IPv4", "Addresses" to values("link_properties", addresses.ipv4))
            group("  IPv6", "Addresses" to values("link_properties", addresses.ipv6))
            if (addresses.other.isNotEmpty()) group("  Other address families", "Addresses" to values("link_properties", addresses.other))
            group("  Routes", "Routes" to values("link_properties", ip.routesList))
            group("  DNS", "Servers" to values("link_properties", ip.dnsServersList), "Domains" to properties(ip.domains),
                "Private DNS" to properties("active=${ip.privateDnsActive}; server=${ip.privateDnsServerName.ifEmpty { "none" }}"))
            group("  Link properties", "MTU" to properties(ip.mtu.toString()), "DHCP server" to properties(ip.dhcpServer),
                "NAT64" to properties(ip.nat64Prefix), "Proxy" to properties(ip.httpProxy), "Wake on LAN" to bool("link_properties", ip.wakeOnLanSupported))
            group("  Network capabilities", "Capabilities" to values("capabilities", ip.capabilitiesList), "Bandwidth estimate" to observed("capabilities", networkBandwidth(ip).orEmpty()),
                "Signal strength" to observed("capabilities", ip.signalStrength.toString()), "Specifier" to observed("capabilities", ip.networkSpecifier),
                "Ownership" to observed("capabilities", "uid=${ip.ownerUid}; enterprise=${ip.enterpriseIdsList.joinToString(",").ifEmpty { "none" }}; subscriptions=${ip.subscriptionIdsList.joinToString(",").ifEmpty { "none" }}"))
            group("  IPv6 RA (Android)", *ip.ipv6RaList.map { it.key to it.value }.toTypedArray())
        } else group("  Selection", "Observation" to "? (IP payload unavailable)")
        group("Permissions", *status.permissionsList.map { entry ->
            if ('=' in entry) entry.substringBefore('=') to entry.substringAfter('=') else "Permission" to entry
        }.toTypedArray())
        group("Details", "More" to "show wifi status full; help show wifi status",
            "Validation scope" to "Android capability observation, not an active connectivity test")
    }

    fun presentation(status: WifiStatus, detail: Boolean = false, detailAvailable: Boolean = false, ipAvailable: Boolean = false, includeIpSnapshot: Boolean = true): List<AgentBlockPart> = buildList {
        fun text(label: String, value: String) { add(AgentBlockPart.Field(label, value)) }
        fun observed(label: String, fields: List<io.dropcheck.agent.grpc.DiagnosticField>, field: String, value: String, fullValue: String = value) { add(AgentBlockPart.Field(label, AgentObservationPresentation.value(fields, field, value), fullValue)) }
        text("Radio", AgentObservationPresentation.value(status.observationFieldsList, "radio", if (status.enabled) "on" else "off"))
        text("Wi-Fi state", status.state)
        text("Active Network", status.activeNetwork.ifEmpty { "none" })
        text("Wi-Fi Networks", status.wifiNetworkCount.toString())
        val conn = activeWifiConnection(status)
        if (conn != null) {
            val disconnected = conn.detailedState.equals("DISCONNECTED", ignoreCase = true)
            text("Link", AgentObservationPresentation.value(status.observationFieldsList, "connection", conn.detailedState.ifEmpty { conn.supplicantState.ifEmpty { "?" } }))
            observed("SSID", conn.observationFieldsList, "identity", if (disconnected) "none" else conn.ssid.takeUnless { it.isEmpty() || it.equals("<unknown ssid>", ignoreCase = true) } ?: "? (identity unavailable)", conn.ssid)
            observed("BSSID", conn.observationFieldsList, "identity", if (disconnected) "none" else conn.bssid.takeIf(::isKnownWifiBssid) ?: "?", conn.bssid)
            text("Band", wifiBandFromFrequency(conn.frequencyMhz))
            text("CH/BW", "${wifiChannelFromFrequency(conn.frequencyMhz)}/${formatWifiChannelWidth(conn.channelWidth).ifEmpty { "?" }}")
            text("PHY", conn.wifiStandard.ifEmpty { "?" })
            observed("RSSI", conn.observationFieldsList, "rssi", "${conn.rssiDbm}dBm")
            text("Security", conn.securityType.ifEmpty { "?" })
            if (conn.hasSecurityDetails() && conn.securityDetails.rsnPresent) text("PMF", "capable=${conn.securityDetails.pmfCapable} required=${conn.securityDetails.pmfRequired}")
            observed("Tx link rate", conn.observationFieldsList, "tx_link_speed_mbps", "${conn.txLinkSpeedMbps}Mbps")
            observed("Rx link rate", conn.observationFieldsList, "rx_link_speed_mbps", "${conn.rxLinkSpeedMbps}Mbps")
            observed("MLD", conn.observationFieldsList, "ap_mld_mac_address", conn.apMldMacAddress)
            text("Associated", AgentObservationPresentation.value(conn.observationFieldsList, "associated_mlo_links", conn.associatedMloLinksCount.toString()))
            text("Affiliated", AgentObservationPresentation.value(conn.observationFieldsList, "affiliated_mlo_links", conn.affiliatedMloLinksCount.toString()))
            if (detail || !detailAvailable) {
                val lines = mutableListOf<String>()
                kv(lines, "configuration_id" to conn.networkId.toString(), "signal" to wifiSignalLevel(conn),
                    "frequency" to "${conn.frequencyMhz}MHz", "link_speed" to "${conn.linkSpeedMbps}Mbps", "sta_mac" to conn.macAddress,
                    "supplicant" to conn.supplicantState, "hidden" to conn.hiddenSsid.toString(), "restricted" to conn.restricted.toString(),
                    "subscription_id" to conn.subscriptionId.toString())
                renderAPCapabilities(lines, conn)
                renderWifiSecurityDetails(lines, conn)
                renderDetailedWifiCapabilities(lines, conn)
                addAll(lines.map { AgentBlockPart.Text(it) })
                add(AgentBlockPart.Field("AP link ID", AgentObservationPresentation.value(conn.observationFieldsList, "ap_mlo_link_id", conn.apMloLinkId.toString()), conn.apMloLinkId.toString()))
                addAll(mloLinkParts("Associated links", conn.associatedMloLinksList))
                addAll(mloLinkParts("Affiliated links", conn.affiliatedMloLinksList))
                text("WifiInfo IPv4 (separate observation)", conn.ipv4Address)
                addAll(informationElementParts(conn.informationElementsList))
                if (conn.raw.isNotEmpty()) text("Raw WifiInfo reference", conn.raw)
                conn.observationFieldsList.forEach { text(it.key, it.value) }
            }
            if (conn.applicableRedactions.isNotEmpty() && conn.applicableRedactions != "0") text("Note", "identity redactions=${conn.applicableRedactions}")
        } else {
            val connection = AgentObservationPresentation.value(status.observationFieldsList, "connection", "")
            text("Link", if (connection == "none") "disconnected" else connection)
            if (connection == "none") text("SSID/BSSID", "none")
        }
        status.permissionsList.forEach { text("Permission", it) }
        if (detail || !detailAvailable) status.observationFieldsList.forEach { text(it.key, it.value) }
        if (!ipAvailable && includeIpSnapshot && status.hasIpStatus()) {
            text("Related IP snapshot", "independently acquired Android Network observation")
            addAll(ipPresentation(status.ipStatus))
        } else if (includeIpSnapshot && status.hasConnection() && status.connection.ipv4Address.isNotEmpty()) {
            text("Related IP snapshot", status.connection.ipv4Address)
        }
    }

    fun ipPresentation(status: IpStatus, detail: Boolean = false, detailAvailable: Boolean = false): List<AgentBlockPart> = buildList {
        val fields = status.observationFieldsList
        val metadata = fields.associate { it.key to it.value }
        fun text(label: String, value: String) { add(AgentBlockPart.Field(label, value)) }
        fun observed(label: String, value: String) { add(AgentBlockPart.Field(label, AgentObservationPresentation.value(fields, "link_properties", value), value)) }
        fun values(label: String, values: List<String>) { add(AgentBlockPart.Field(label, AgentObservationPresentation.list(fields, "link_properties", values), values.joinToString("\n"))) }
        text("Network", status.networkId.ifEmpty { "?" })
        text("Transports", AgentObservationPresentation.list(fields, "capabilities", status.transportsList))
        observed("Interface", status.interfaceName)
        text("Default network", AgentObservationPresentation.value(fields, "default_network", metadata["default_network_id"].orEmpty()))
        val selected = when (metadata["selected_is_default"]) { "true" -> "yes"; "false" -> "no"; else -> "? (selected_is_default not supplied)" }
        text("Selected is default", AgentObservationPresentation.value(fields, "default_network", selected))
        if (metadata["default_network.state"] == "available" && metadata["selected_is_default"] == "false") text("Note", "selected Wi-Fi differs from Android default Network")
        add(AgentBlockPart.Field("Internet", AgentObservationPresentation.bool(fields, "capabilities", status.internet), if (status.internet) "yes" else "no"))
        add(AgentBlockPart.Field("Validated", AgentObservationPresentation.bool(fields, "capabilities", status.validated), if (status.validated) "yes" else "no"))
        observed("MTU", status.mtu.toString())
        val addresses = splitIPAddresses(status.addressesList)
        add(AgentBlockPart.Text("IPv4")); values("Addr", addresses.ipv4)
        add(AgentBlockPart.Text("IPv6")); values("Addr", addresses.ipv6)
        if (addresses.other.isNotEmpty()) values("Other address", addresses.other)
        values("Routes", status.routesList)
        values("DNS", status.dnsServersList)
        observed("DHCP server", status.dhcpServer)
        observed("Private DNS", "${if (status.privateDnsActive) "on" else "off"}; server=${status.privateDnsServerName.ifEmpty { "none" }}")
        observed("NAT64", status.nat64Prefix)
        if (detail || !detailAvailable) {
            text("Capabilities", AgentObservationPresentation.list(fields, "capabilities", status.capabilitiesList))
            text("Bandwidth", AgentObservationPresentation.value(fields, "capabilities", networkBandwidth(status).orEmpty()))
            text("Signal strength", AgentObservationPresentation.value(fields, "capabilities", status.signalStrength.toString()))
            text("Specifier", AgentObservationPresentation.value(fields, "capabilities", status.networkSpecifier))
            text("Owner uid", AgentObservationPresentation.value(fields, "capabilities", status.ownerUid.toString()))
            text("Enterprise IDs", AgentObservationPresentation.list(fields, "capabilities", status.enterpriseIdsList))
            text("Subscription IDs", AgentObservationPresentation.list(fields, "capabilities", status.subscriptionIdsList.map(Int::toString)))
            observed("Domains", status.domains)
            observed("Proxy", status.httpProxy)
            text("Wake on LAN", AgentObservationPresentation.bool(fields, "link_properties", status.wakeOnLanSupported))
            val lines = mutableListOf<String>()
            renderIPv6Ra(lines, status.ipv6RaList)
            addAll(lines.map { AgentBlockPart.Text(it) })
            if (status.hasWifi()) {
                add(AgentBlockPart.Text("WifiInfo from this NetworkCapabilities observation"))
                val connectionLines = mutableListOf<String>()
                renderConnection(connectionLines, status.wifi)
                addAll(connectionLines.map { AgentBlockPart.Text(it) })
                val conn = status.wifi
                text("Network configuration ID", conn.networkId.toString())
                text("Link speed Mbps", conn.linkSpeedMbps.toString())
                text("WifiInfo IPv4 (separate observation)", conn.ipv4Address)
                text("Subscription ID", conn.subscriptionId.toString())
                text("Applicable redactions", conn.applicableRedactions)
                if (!conn.hiddenSsid) text("Hidden SSID", "false")
                if (!conn.restricted) text("Restricted", "false")
                text("MLD", AgentObservationPresentation.value(conn.observationFieldsList, "ap_mld_mac_address", conn.apMldMacAddress))
                text("AP link ID", AgentObservationPresentation.value(conn.observationFieldsList, "ap_mlo_link_id", conn.apMloLinkId.toString()))
                addAll(mloLinkParts("Associated links", status.wifi.associatedMloLinksList))
                addAll(mloLinkParts("Affiliated links", status.wifi.affiliatedMloLinksList))
                addAll(informationElementParts(conn.informationElementsList))
                if (conn.raw.isNotEmpty()) text("Raw WifiInfo reference", conn.raw)
                conn.observationFieldsList.forEach { text(it.key, it.value) }
            }
            if (detail && status.rawLinkProperties.isNotEmpty()) text("Raw LinkProperties", status.rawLinkProperties)
            if (detail && status.rawCapabilities.isNotEmpty()) text("Raw capabilities", status.rawCapabilities)
            fields.forEach { text(it.key, it.value) }
        }
    }

    fun scanEvidence(ap: io.dropcheck.agent.grpc.WifiScanResult): List<AgentBlockPart> = buildList {
        add(AgentBlockPart.Field("Capabilities", ap.capabilities))
        add(AgentBlockPart.Field("WifiSsid representation", ap.wifiSsid))
        add(AgentBlockPart.Field("Timestamp us (boot clock)", ap.timestampUs.toString()))
        add(AgentBlockPart.Field("Passpoint", ap.passpoint.toString()))
        if (ap.operatorFriendlyName.isNotEmpty()) add(AgentBlockPart.Field("Operator", ap.operatorFriendlyName))
        if (ap.venueName.isNotEmpty()) add(AgentBlockPart.Field("Venue", ap.venueName))
        val conn = WifiConnection.newBuilder().addAllInformationElements(ap.informationElementsList)
        if (ap.hasHeCapabilities()) conn.setHeCapabilities(ap.heCapabilities)
        if (ap.hasHeOperation()) conn.setHeOperation(ap.heOperation)
        if (ap.hasEhtCapabilities()) conn.setEhtCapabilities(ap.ehtCapabilities)
        if (ap.hasEhtOperation()) conn.setEhtOperation(ap.ehtOperation)
        if (ap.hasHeUoraParameterSet()) conn.setHeUoraParameterSet(ap.heUoraParameterSet)
        if (ap.hasHeMuEdcaParameterSet()) conn.setHeMuEdcaParameterSet(ap.heMuEdcaParameterSet)
        if (ap.hasHeSpatialReuseParameterSet()) conn.setHeSpatialReuseParameterSet(ap.heSpatialReuseParameterSet)
        if (ap.hasHe6GhzCapabilities()) conn.setHe6GhzCapabilities(ap.he6GhzCapabilities)
        val lines = mutableListOf<String>()
        renderAPCapabilities(lines, conn.build())
        renderDetailedWifiCapabilities(lines, conn.build())
        addAll(lines.map { AgentBlockPart.Text(it) })
        add(AgentBlockPart.Field("AP link ID", AgentObservationPresentation.value(ap.observationFieldsList, "ap_mlo_link_id", ap.apMloLinkId.toString()), ap.apMloLinkId.toString()))
        addAll(mloLinkParts("Affiliated link details", ap.affiliatedMloLinksList))
        addAll(informationElementParts(ap.informationElementsList))
        if (ap.raw.isNotEmpty()) add(AgentBlockPart.Field("Raw ScanResult reference", ap.raw))
        ap.observationFieldsList.forEach { add(AgentBlockPart.Field(it.key, it.value)) }
    }

    private fun informationElementParts(elements: List<io.dropcheck.agent.grpc.WifiInformationElement>): List<AgentBlockPart> = elements.mapIndexed { index, element ->
        AgentBlockPart.Field("IE ${index + 1}", "id=${element.id} extension=${element.idExt} bytes=${element.byteCount} hex=${element.bytesHex}")
    }
    private fun mloLinkParts(title: String, links: List<io.dropcheck.agent.grpc.MloLinkInfo>): List<AgentBlockPart> = buildList {
        if (links.isNotEmpty()) add(AgentBlockPart.Text(title))
        links.forEach { link ->
            fun observed(label: String, field: String, value: String) { add(AgentBlockPart.Field(label, AgentObservationPresentation.value(link.observationFieldsList, field, value), value)) }
            observed("Link ID", "identity", link.linkId.toString())
            observed("AP MAC", "identity", link.apMacAddress)
            observed("STA MAC", "identity", link.staMacAddress)
            observed("State/Band/CH", "identity", "${link.state} ${link.band} ${link.channel}")
            observed("RSSI dBm", "rates", link.rssiDbm.toString())
            observed("Tx/Rx Mbps", "rates", "${link.txLinkSpeedMbps}/${link.rxLinkSpeedMbps}")
            add(AgentBlockPart.Field("Max Tx/Rx Mbps", "? (maximum-rate availability metadata not supplied)", "${link.maxSupportedTxLinkSpeedMbps}/${link.maxSupportedRxLinkSpeedMbps}"))
            add(AgentBlockPart.Field("Reported max Tx/Rx (availability uncollected)", "${link.maxSupportedTxLinkSpeedMbps}/${link.maxSupportedRxLinkSpeedMbps}Mbps"))
            if (link.raw.isNotEmpty()) add(AgentBlockPart.Field("Raw MloLink reference", link.raw))
            link.observationFieldsList.forEach { add(AgentBlockPart.Field(it.key, it.value)) }
        }
    }

    private fun renderConnection(out: MutableList<String>, conn: WifiConnection) {
        section(out, "Connection")
        kv(out,
            "ssid" to AgentObservationPresentation.value(conn.observationFieldsList, "identity", conn.ssid),
            "bssid" to AgentObservationPresentation.value(conn.observationFieldsList, "identity", conn.bssid),
            "security" to empty(conn.securityType, "unknown"),
            "standard" to empty(conn.wifiStandard, "unknown"),
            "rssi" to AgentObservationPresentation.value(conn.observationFieldsList, "rssi", "${conn.rssiDbm}dBm"),
            "signal" to wifiSignalLevel(conn),
            "band" to wifiBandFromFrequency(conn.frequencyMhz),
            "channel" to wifiChannelFromFrequency(conn.frequencyMhz),
            "frequency" to "${conn.frequencyMhz}MHz",
            "bandwidth" to empty(formatWifiChannelWidth(conn.channelWidth), "unknown"),
            "link" to wifiLinkSpeed(conn),
            "sta_mac" to conn.macAddress,
            "supplicant" to empty(conn.supplicantState, "unknown"),
            "detailed" to empty(conn.detailedState, "unknown"),
        )
        renderAPCapabilities(out, conn)
        renderWifiSecurityDetails(out, conn)
        renderDetailedWifiCapabilities(out, conn)
    }

    private fun wifiSignalLevel(conn: WifiConnection): String {
        if (conn.maxSignalLevel <= 0 && conn.signalLevel == 0) return ""
        return "${conn.signalLevel}/${conn.maxSignalLevel}"
    }

    private fun wifiLinkSpeed(conn: WifiConnection): String {
        val tx = AgentObservationPresentation.value(conn.observationFieldsList, "tx_link_speed_mbps", "${conn.txLinkSpeedMbps}Mbps")
        val rx = AgentObservationPresentation.value(conn.observationFieldsList, "rx_link_speed_mbps", "${conn.rxLinkSpeedMbps}Mbps")
        return "tx=$tx rx=$rx"
    }

    private fun renderAPCapabilities(out: MutableList<String>, conn: WifiConnection) {
        val rows = apCapabilityRows(conn)
        if (rows.isEmpty()) return
        section(out, "AP Capabilities")
        kv(out, *rows.toTypedArray())
    }

    private fun apCapabilityRows(conn: WifiConnection): List<Pair<String, String>> {
        val summary = newApCapabilitySummary(conn.informationElementsList)
        val rows = mutableListOf<Pair<String, String>>()
        fun add(name: String, values: Set<String>) {
            if (values.isNotEmpty()) rows += name to multiLineValue(values.toList())
        }
        add("roaming", summary.roaming)
        add("security", summary.security)
        add("phy", summary.phy)
        add("operation", summary.operation)
        add("radio", summary.radio)
        add("qos", summary.qos)
        add("network", summary.network)
        add("rates", summary.rates)
        if (conn.hiddenSsid) rows += "hidden" to "true"
        if (conn.restricted) rows += "restricted" to "true"
        if (conn.passpointFqdn.isNotBlank()) rows += "passpoint_fqdn" to conn.passpointFqdn
        if (conn.passpointProviderFriendlyName.isNotBlank()) rows += "passpoint_provider" to conn.passpointProviderFriendlyName
        if (conn.passpointUniqueId.isNotBlank()) rows += "passpoint_unique_id" to conn.passpointUniqueId
        wifiMaxLink(conn)?.let { rows += "max_link" to it }
        if (summary.vendorIe > 0) rows += "vendor_ie" to summary.vendorIe.toString()
        add("other_ie", summary.other)
        return rows
    }

    private class ApCapabilitySummary {
        val roaming = sortedSetOf<String>()
        val security = sortedSetOf<String>()
        val phy = sortedSetOf<String>()
        val operation = sortedSetOf<String>()
        val radio = sortedSetOf<String>()
        val qos = sortedSetOf<String>()
        val network = sortedSetOf<String>()
        val rates = sortedSetOf<String>()
        val other = sortedSetOf<String>()
        var vendorIe = 0
    }

    private fun newApCapabilitySummary(elements: List<WifiInformationElement>): ApCapabilitySummary {
        val summary = ApCapabilitySummary()
        elements.forEach { element ->
            when (element.id) {
                0 -> Unit
                1 -> summary.rates += "supported_rates"
                7 -> summary.radio += "country"
                11 -> summary.radio += "bss_load"
                32 -> summary.radio += "power_constraint"
                35 -> summary.radio += "tpc_report"
                45 -> summary.phy += "ht"
                48 -> summary.security += "rsn"
                50 -> summary.rates += "extended_supported_rates"
                54 -> summary.roaming += "11r"
                55 -> summary.roaming += "fast_bss_transition"
                59 -> summary.radio += "supported_operating_classes"
                61 -> summary.operation += "ht_operation"
                70 -> summary.roaming += "11k"
                107 -> summary.network += "interworking"
                111 -> summary.network += "roaming_consortium"
                127 -> {
                    if (informationElementBit(element, 19)) summary.roaming += "11v_bss_transition"
                    if (informationElementBit(element, 84)) summary.security += "beacon_protection"
                    if (!informationElementBit(element, 19) && !informationElementBit(element, 84)) {
                        summary.other += informationElementName(element)
                    }
                }
                244 -> summary.security += "rsn_extension"
                255 -> when (element.idExt) {
                    35 -> summary.phy += "he"
                    36 -> summary.operation += "he_operation"
                    37 -> summary.qos += "uora"
                    38 -> summary.qos += "mu_edca"
                    39 -> summary.qos += "spatial_reuse"
                    45 -> summary.radio += "he_bss_load"
                    59 -> summary.phy += "he_6ghz"
                    106 -> summary.operation += "eht_operation"
                    107 -> summary.operation += "eht_multi_link"
                    108 -> summary.phy += "eht"
                    else -> summary.other += informationElementExtensionName(element.idExt)
                }
                else -> summary.other += informationElementName(element)
            }
        }
        return summary
    }

    private fun wifiMaxLink(conn: WifiConnection): String? {
        val tx = conn.maxSupportedTxLinkSpeedMbps
        val rx = conn.maxSupportedRxLinkSpeedMbps
        if (tx <= 0 && rx <= 0) return null
        return "tx=${tx}Mbps rx=${rx}Mbps"
    }

    private fun renderWifiSecurityDetails(out: MutableList<String>, conn: WifiConnection) {
        if (!conn.hasSecurityDetails()) return
        val rows = wifiSecurityDetailRows(conn.securityDetails)
        if (rows.isEmpty()) return
        section(out, "Wi-Fi Security Details")
        kv(out, *rows.toTypedArray())
    }

    private fun wifiSecurityDetailRows(value: WifiSecurityDetails): List<Pair<String, String>> {
        return buildList {
            if (value.rsnPresent) {
                add("rsn" to "version=${value.rsnVersion} capabilities=0x${value.rsnCapabilitiesHex}")
                add("group_data" to value.groupDataCipher)
                add("pairwise" to joined(value.pairwiseCiphersList))
                add("akm" to joined(value.akmSuitesList))
                add("pmf" to "capable=${value.pmfCapable} required=${value.pmfRequired}")
                if (value.groupManagementCipher.isNotBlank()) add("group_mgmt" to value.groupManagementCipher)
            }
            add("wifi7" to "gcmp256=${value.gcmp256} sae_gdh=${value.saeGdh} ft_sae_gdh=${value.ftSaeGdh} beacon_protection=${value.beaconProtection} personal_ready=${value.wifi7PersonalReady}")
            if (value.rsnxePresent) add("rsnxe" to joined(value.rsnxeCapabilitiesList))
            if (value.extendedCapabilitiesPresent) add("extended" to joined(value.extendedCapabilitiesList))
            if (value.warningsCount > 0) add("warnings" to multiLineValue(value.warningsList))
        }.filter { it.second.isNotBlank() }
    }

    private fun informationElementName(element: WifiInformationElement): String {
        if (element.id == 255) return informationElementExtensionName(element.idExt)
        return when (element.id) {
            0 -> "ssid"
            1 -> "supported_rates"
            3 -> "dsss_parameter_set"
            5 -> "tim"
            7 -> "country"
            11 -> "bss_load"
            32 -> "power_constraint"
            33 -> "power_capability"
            35 -> "tpc_report"
            36 -> "supported_channels"
            42 -> "erp"
            45 -> "ht_capabilities"
            48 -> "rsn"
            50 -> "extended_supported_rates"
            54 -> "mobility_domain_11r"
            55 -> "fast_bss_transition"
            59 -> "supported_operating_classes"
            61 -> "ht_operation"
            70 -> "rm_enabled_capabilities_11k"
            74 -> "overlapping_bss_scan_parameters"
            107 -> "interworking"
            111 -> "roaming_consortium"
            127 -> "extended_capabilities"
            191 -> "vht_capabilities"
            192 -> "vht_operation"
            195 -> "tx_power_envelope"
            201 -> "reduced_neighbor_report"
            221 -> "vendor_specific"
            244 -> "rsn_extension"
            else -> "unknown_${element.id}"
        }
    }

    private fun informationElementExtensionName(idExt: Int): String = when (idExt) {
        35 -> "he_capabilities"
        36 -> "he_operation"
        37 -> "uora_parameter_set"
        38 -> "mu_edca_parameter_set"
        39 -> "spatial_reuse_parameter_set"
        45 -> "he_bss_load"
        59 -> "he_6ghz_capabilities"
        106 -> "eht_operation"
        107 -> "eht_multi_link"
        108 -> "eht_capabilities"
        else -> "extension_$idExt"
    }

    private fun informationElementBit(element: WifiInformationElement, bit: Int): Boolean {
        if (bit < 0) return false
        val byteIndex = bit / 8
        val hexIndex = byteIndex * 2
        val hex = element.bytesHex
        if (hexIndex + 2 > hex.length) return false
        val byteValue = hex.substring(hexIndex, hexIndex + 2).toIntOrNull(16) ?: return false
        return byteValue and (1 shl (bit % 8)) != 0
    }

    private fun renderDetailedWifiCapabilities(out: MutableList<String>, conn: WifiConnection) {
        val rows = detailedWifiCapabilityRows(conn)
        if (rows.isEmpty()) return
        section(out, "HE/EHT Details")
        kv(out, *rows.toTypedArray())
    }

    private fun detailedWifiCapabilityRows(conn: WifiConnection): List<Pair<String, String>> {
        val rows = mutableListOf<Pair<String, String>>()
        if (conn.hasHeCapabilities()) {
            rows += "he_cap" to heCapabilitiesSummary(conn.heCapabilities)
        }
        if (conn.hasHeOperation()) {
            rows += "he_oper" to heOperationSummary(conn.heOperation)
        }
        if (conn.hasEhtCapabilities()) {
            rows += "eht_cap" to ehtCapabilitiesSummary(conn.ehtCapabilities)
        }
        if (conn.hasEhtOperation()) {
            rows += "eht_oper" to ehtOperationSummary(conn.ehtOperation)
        }
        if (conn.hasHeUoraParameterSet()) {
            rows += "uora" to heUoraSummary(conn.heUoraParameterSet)
        }
        if (conn.hasHeMuEdcaParameterSet()) {
            rows += "mu_edca" to heMuEdcaSummary(conn.heMuEdcaParameterSet)
        }
        if (conn.hasHeSpatialReuseParameterSet()) {
            rows += "spatial_reuse" to heSpatialReuseSummary(conn.heSpatialReuseParameterSet)
        }
        if (conn.hasHe6GhzCapabilities()) {
            rows += "he_6ghz_cap" to he6GhzCapabilitiesSummary(conn.he6GhzCapabilities)
        }
        return rows
    }

    private fun heCapabilitiesSummary(value: WifiHeCapabilities): String {
        return multiLineValue(buildList {
            add("mac=0x${value.macCapabilitiesHex} phy=0x${value.phyCapabilitiesHex}")
            addAll(value.featuresList)
            addAll(mcsNssSummary(value.mcsNssList))
            if (value.ppeThresholdsPresent) {
                add("ppe nss=${value.ppeNssCount} ru=${joined(value.ppeRuIndicesList)} hex=0x${value.ppeThresholdsHex}")
            }
            if (value.truncated) add("truncated=true")
            addAll(value.warningsList.map { "warning=$it" })
        })
    }

    private fun he6GhzCapabilitiesSummary(value: WifiHe6GhzCapabilities): String {
        return multiLineValue(buildList {
            add("cap=0x${value.capabilities.toString(16)} max_mpdu=${value.maxMpduLengthBytes} max_ampdu_exp=${value.maxAmpduLengthExponent} max_ampdu=${value.maxAmpduLengthBytes}")
            add("min_mpdu_start=${value.minimumMpduStartSpacing} smps=${value.smPowerSave}")
            addAll(value.featuresList)
            if (value.truncated) add("truncated=true")
            addAll(value.warningsList.map { "warning=$it" })
        })
    }

    private fun ehtCapabilitiesSummary(value: WifiEhtCapabilities): String {
        return multiLineValue(buildList {
            add("mac=0x${value.macCapabilitiesHex} phy=0x${value.phyCapabilitiesHex}")
            addAll(value.featuresList)
            addAll(mcsNssSummary(value.mcsNssList))
            if (value.ppeThresholdsPresent) {
                add("ppe nss=${value.ppeNssCount} ru=${joined(value.ppeRuIndicesList)} hex=0x${value.ppeThresholdsHex}")
            }
            if (value.truncated) add("truncated=true")
            addAll(value.warningsList.map { "warning=$it" })
        })
    }

    private fun heOperationSummary(value: WifiHeOperation): String {
        return multiLineValue(buildList {
            add("params=0x${value.parameters.toString(16)} basic_mcs_nss=0x${value.basicMcsNssSetHex}")
            if (value.channelWidth.isNotBlank()) {
                add("width=${value.channelWidth} primary=${value.primaryChannel} ccfs0=${value.centerFreqSegment0} ccfs1=${value.centerFreqSegment1}")
            }
            add("bss_color=${value.bssColor} disabled=${value.bssColorDisabled}")
            addAll(value.flagsList)
            if (value.truncated) add("truncated=true")
            addAll(value.warningsList.map { "warning=$it" })
        })
    }

    private fun ehtOperationSummary(value: WifiEhtOperation): String {
        return multiLineValue(buildList {
            add("params=0x${value.parameters.toString(16)} basic_mcs_nss=0x${value.basicMcsNssSetHex}")
            if (value.channelWidth.isNotBlank()) {
                add("width=${value.channelWidth} ccfs0=${value.centerFreqSegment0} ccfs1=${value.centerFreqSegment1}")
            }
            if (value.disabledSubchannelBitmap != 0) {
                add("disabled_subchannel_bitmap=0x${value.disabledSubchannelBitmap.toString(16)}")
            }
            addAll(value.flagsList)
            if (value.truncated) add("truncated=true")
            addAll(value.warningsList.map { "warning=$it" })
        })
    }

    private fun heUoraSummary(value: WifiHeUoraParameterSet): String {
        return "eocw_min=${value.eocwMin} eocw_max=${value.eocwMax}${if (value.truncated) " truncated=true" else ""}"
    }

    private fun heMuEdcaSummary(value: WifiHeMuEdcaParameterSet): String {
        return multiLineValue(buildList {
            add("qos_info=0x${value.qosInfo.toString(16)}")
            value.acList.forEach { ac ->
                add("${ac.ac} aci=${ac.aci} aifsn=${ac.aifsn} acm=${ac.acm} ecw=${ac.ecwMin}/${ac.ecwMax} timer=${ac.timer}")
            }
            if (value.truncated) add("truncated=true")
            addAll(value.warningsList.map { "warning=$it" })
        })
    }

    private fun heSpatialReuseSummary(value: WifiHeSpatialReuseParameterSet): String {
        return multiLineValue(buildList {
            add("control=0x${value.srControl.toString(16)} flags=${joined(value.flagsList)}")
            if (value.nonSrgObssPdMaxOffset != 0) add("non_srg_obss_pd_max_offset=${value.nonSrgObssPdMaxOffset}")
            if (value.srgObssPdMinOffset != 0 || value.srgObssPdMaxOffset != 0) {
                add("srg_obss_pd=${value.srgObssPdMinOffset}/${value.srgObssPdMaxOffset}")
            }
            if (value.srgBssColorBitmapHex.isNotBlank()) add("srg_bss_color_bitmap=0x${value.srgBssColorBitmapHex}")
            if (value.srgPartialBssidBitmapHex.isNotBlank()) add("srg_partial_bssid_bitmap=0x${value.srgPartialBssidBitmapHex}")
            if (value.truncated) add("truncated=true")
            addAll(value.warningsList.map { "warning=$it" })
        })
    }

    private fun mcsNssSummary(values: List<WifiMcsNssSupport>): List<String> {
        if (values.isEmpty()) return emptyList()
        return values
            .groupBy { listOf(it.standard, it.bandwidth, it.mcsRange).joinToString("/") }
            .map { (key, group) ->
                val parts = group.joinToString(" ") { item ->
                    val nss = if (item.maxNss > 0) item.maxNss else item.nss
                    "${item.direction}=nss$nss"
                }
                "mcs_nss $key $parts"
            }
    }

    private fun joined(values: List<String>, fallback: String = "<none>"): String {
        return if (values.isEmpty()) fallback else values.joinToString(",")
    }

    private fun renderIPv6Ra(out: MutableList<String>, values: List<io.dropcheck.agent.grpc.DiagnosticField>) {
        val rows = ipv6RaDetailRows(values)
        if (rows.isEmpty()) return
        section(out, "IPv6 RA")
        kv(out, *rows.toTypedArray())
    }

    private fun ipv6RaDetailRows(values: List<io.dropcheck.agent.grpc.DiagnosticField>): List<Pair<String, String>> {
        val preferredOrder = listOf(
            "default_route",
            "default_gateways",
            "accept_ra",
            "accept_ra_defrtr",
            "accept_ra_pinfo",
            "accept_ra_rtr_pref",
            "accept_ra_min_lft",
            "accept_ra_min_hop_limit",
            "accept_ra_from_local",
        ).withIndex().associate { it.value to it.index }
        return values.mapIndexedNotNull { index, field ->
            val key = field.key.trim()
            val value = AgentSafePresentation.field(key, field.value).trim()
            if (key.isBlank() || value.isBlank()) {
                null
            } else {
                IndexedValue(index, key to value)
            }
        }.sortedWith(
            compareBy<IndexedValue<Pair<String, String>>> { preferredOrder[it.value.first] ?: preferredOrder.size + it.index }
                .thenBy { it.index },
        ).map { it.value }
    }

    private fun multiLineValue(values: List<String>): String {
        return values.filter { it.isNotBlank() }.joinToString("\n")
    }

    private data class SplitAddresses(
        val ipv4: List<String>,
        val ipv6: List<String>,
        val other: List<String>,
    )

    private fun splitIPAddresses(values: List<String>): SplitAddresses {
        val ipv4 = mutableListOf<String>()
        val ipv6 = mutableListOf<String>()
        val other = mutableListOf<String>()
        values.forEach { value ->
            when {
                value.contains(":") -> ipv6 += value
                value.contains(".") -> ipv4 += value
                value.isNotBlank() -> other += value
            }
        }
        return SplitAddresses(ipv4, ipv6, other)
    }

    private fun networkBandwidth(status: IpStatus): String? {
        val parts = mutableListOf<String>()
        if (status.downstreamKbps > 0) parts += "down=${status.downstreamKbps}kbps"
        if (status.upstreamKbps > 0) parts += "up=${status.upstreamKbps}kbps"
        return parts.takeIf { it.isNotEmpty() }?.joinToString(" ")
    }

    private fun section(out: MutableList<String>, title: String) {
        if (out.isNotEmpty() && out.last().isNotBlank()) out += ""
        out += title
    }

    private fun kv(out: MutableList<String>, vararg rows: Pair<String, String>) {
        val filtered = rows.filter { it.first.isNotBlank() && it.second.isNotBlank() }
        val width = filtered.maxOfOrNull { it.first.length } ?: 0
        filtered.forEach { (key, value) ->
            if (value.contains("\n")) {
                out += "  $key"
                value.lineSequence()
                    .filter { it.isNotBlank() }
                    .forEach { line -> out += "    $line" }
            } else {
                out += "  ${key.padEnd(width)}  $value"
            }
        }
    }

    private fun empty(value: String, fallback: String): String = value.ifBlank { fallback }

    private fun wifiBandFromFrequency(freq: Int): String = when (freq) {
        in 2400 until 2500 -> "2.4ghz"
        in 4900 until 5900 -> "5ghz"
        in 5925 until 7125 -> "6ghz"
        in 57000 until 71000 -> "60ghz"
        else -> "unknown"
    }

    private fun wifiChannelFromFrequency(freq: Int): String {
        val channel = when {
            freq == 2484 -> 14
            freq in 2412..2472 -> (freq - 2407) / 5
            freq in 5000..5895 -> (freq - 5000) / 5
            freq in 5955..7115 -> (freq - 5950) / 5
            else -> 0
        }
        return if (channel == 0) "unknown" else channel.toString()
    }

    private fun formatWifiChannelWidth(value: String): String {
        val trimmed = value.trim().trim(',', ';')
        if (trimmed.isBlank()) return ""
        var normalized = trimmed.lowercase()
        normalized = normalized.removePrefix("channel_width_")
        normalized = normalized.removePrefix("width_")
        normalized = normalized.replace("_", "").replace(" ", "")
        val core = normalized.removeSuffix("mhz")
        return if (core.matches(Regex("^[0-9+]+$"))) "${core}MHz" else trimmed
    }
}
