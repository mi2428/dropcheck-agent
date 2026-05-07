package io.dropcheck.agent

import io.dropcheck.agent.grpc.IpStatus
import io.dropcheck.agent.grpc.MloLinkInfo
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
import io.dropcheck.agent.grpc.WifiStatus

/** Text renderer used by the on-device shell for `show wifi status`. */
internal object AgentWifiStatusRenderer {
    fun render(status: WifiStatus): List<String> {
        val out = mutableListOf<String>()
        section(out, "Wi-Fi")
        kv(out,
            "enabled" to status.enabled.toString(),
            "state" to empty(status.state, "unknown"),
            "active" to empty(status.activeNetwork, "none"),
            "networks" to status.wifiNetworkCount.toString(),
            "permissions" to permissionSummary(status.permissionsList),
        )
        val connection = activeWifiConnection(status)
        if (connection != null) renderConnection(out, connection)
        if (status.hasIpStatus()) {
            renderIPStatus(out, status.ipStatus, connection)
        } else if (connection != null) {
            val rows = connectionNetworkRows(connection)
            if (rows.isNotEmpty()) {
                section(out, "Network")
                kv(out, *rows.toTypedArray())
            }
        }
        return out
    }

    private fun permissionSummary(permissions: List<String>): String {
        if (permissions.isEmpty()) return ""
        val granted = mutableListOf<String>()
        val missing = permissions.mapNotNull { permission ->
            val parts = permission.split("=", limit = 2)
            if (parts.size != 2) return@mapNotNull permission
            if (parts[1].equals("granted", ignoreCase = true)) {
                granted += parts[0]
                null
            } else {
                "${parts[0]}=${parts[1]}"
            }
        }
        return if (missing.isEmpty()) {
            multiLineValue(listOf("all_granted") + granted.sorted())
        } else {
            multiLineValue(buildList {
                add("missing")
                addAll(missing.sorted())
                if (granted.isNotEmpty()) {
                    add("granted")
                    addAll(granted.sorted())
                }
            })
        }
    }

    private fun renderConnection(out: MutableList<String>, conn: WifiConnection) {
        section(out, "Connection")
        kv(out,
            "ssid" to conn.ssid,
            "bssid" to empty(conn.bssid, "unknown"),
            "security" to empty(conn.securityType, "unknown"),
            "standard" to empty(conn.wifiStandard, "unknown"),
            "rssi" to "${conn.rssiDbm}dBm",
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
        renderDetailedWifiCapabilities(out, conn)
        renderMLO(out, conn)
    }

    private fun wifiSignalLevel(conn: WifiConnection): String {
        if (conn.maxSignalLevel <= 0 && conn.signalLevel == 0) return ""
        return "${conn.signalLevel}/${conn.maxSignalLevel}"
    }

    private fun wifiLinkSpeed(conn: WifiConnection): String {
        val parts = mutableListOf("${conn.linkSpeedMbps}Mbps")
        if (conn.txLinkSpeedMbps > 0) parts += "tx=${conn.txLinkSpeedMbps}Mbps"
        if (conn.rxLinkSpeedMbps > 0) parts += "rx=${conn.rxLinkSpeedMbps}Mbps"
        return parts.joinToString(" ")
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
                127 -> if (informationElementBit(element, 19)) {
                    summary.roaming += "11v_bss_transition"
                } else {
                    summary.other += informationElementName(element)
                }
                191 -> summary.phy += "vht"
                192 -> summary.operation += "vht_operation"
                195 -> summary.radio += "tx_power_envelope"
                201 -> summary.roaming += "reduced_neighbor_report"
                221 -> summary.vendorIe++
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

    private fun renderMLO(out: MutableList<String>, conn: WifiConnection) {
        val present = wifiConnectionHasMLO(conn)
        section(out, "MLO")
        kv(out,
            "source" to "android",
            "present" to present.toString(),
            "ap_mld" to empty(connectionMldMac(conn), "<none>"),
            "ap_link_id" to connectionMloLinkID(conn, present),
            "affiliated" to conn.affiliatedMloLinksCount.toString(),
            "associated" to conn.associatedMloLinksCount.toString(),
        )
        renderMLOLinks(out, conn.affiliatedMloLinksList, conn.associatedMloLinksList)
    }

    private fun wifiConnectionHasMLO(conn: WifiConnection): Boolean {
        return conn.apMldMacAddress.isNotBlank() ||
            conn.affiliatedMloLinksCount > 0 ||
            conn.associatedMloLinksCount > 0 ||
            hasMloElement(conn.informationElementsList) ||
            (conn.wifiStandard.equals("802.11be", ignoreCase = true) && conn.apMloLinkId >= 0)
    }

    private fun connectionMldMac(conn: WifiConnection): String {
        return firstNonBlank(conn.apMldMacAddress, mloMldMacFromElements(conn.informationElementsList))
    }

    private fun connectionMloLinkID(conn: WifiConnection, present: Boolean): String {
        if (!present) return "<none>"
        if (conn.apMloLinkId >= 0) return conn.apMloLinkId.toString()
        return mloCurrentLinkIdFromElements(conn.informationElementsList)?.toString() ?: "<none>"
    }

    private fun hasMloElement(elements: List<WifiInformationElement>): Boolean {
        return parseEhtMultiLinkElements(elements).isNotEmpty()
    }

    private fun mloMldMacFromElements(elements: List<WifiInformationElement>): String {
        return parseEhtMultiLinkElements(elements)
            .firstNotNullOfOrNull { it.commonInfo?.mldMacAddress?.takeIf(String::isNotBlank) }
            .orEmpty()
    }

    private fun mloCurrentLinkIdFromElements(elements: List<WifiInformationElement>): Int? {
        return parseEhtMultiLinkElements(elements).firstNotNullOfOrNull { it.commonInfo?.linkId }
    }

    private fun renderMLOLinks(out: MutableList<String>, affiliated: List<MloLinkInfo>, associated: List<MloLinkInfo>) {
        if (affiliated.isEmpty() && associated.isEmpty()) return
        section(out, "MLO Links")
        val rows = affiliated.map { "affiliated" to it } + associated.map { "associated" to it }
        table(out,
            listOf("TYPE", "ID", "STATE", "BAND", "CHANNEL", "RSSI", "TX", "RX", "MAX_TX", "MAX_RX", "AP_MAC", "STA_MAC"),
            rows.map { (type, link) ->
                listOf(
                    type,
                    link.linkId.toString(),
                    empty(link.state, "unknown"),
                    empty(link.band, "unknown"),
                    link.channel.toString(),
                    link.rssiDbm.toString(),
                    link.txLinkSpeedMbps.toString(),
                    link.rxLinkSpeedMbps.toString(),
                    link.maxSupportedTxLinkSpeedMbps.toString(),
                    link.maxSupportedRxLinkSpeedMbps.toString(),
                    empty(link.apMacAddress, "unknown"),
                    empty(link.staMacAddress, "unknown"),
                )
            },
        )
    }

    private fun renderIPStatus(out: MutableList<String>, status: IpStatus, connection: WifiConnection?) {
        section(out, "Network")
        kv(out, *networkRows(status, connection).toTypedArray())
    }

    private fun connectionNetworkRows(conn: WifiConnection): List<Pair<String, String>> {
        return listOf(
            "id" to connectionNetworkID(conn),
            "ipv4" to conn.ipv4Address,
        ).filter { it.second.isNotBlank() }
    }

    private fun networkRows(status: IpStatus, connection: WifiConnection?): List<Pair<String, String>> {
        val split = splitIPAddresses(status.addressesList)
        val ipv4 = split.ipv4.toMutableList()
        val ipv6 = split.ipv6
        val addresses = split.other
        val connectionIPv4 = connection?.ipv4Address.orEmpty()
        if (connectionIPv4.isNotBlank() && !addressListContainsIP(ipv4, connectionIPv4)) {
            ipv4 += connectionIPv4
        }
        val capabilities = networkCapabilitiesForDetail(status.capabilitiesList)
        val signalStrength = networkSignalStrengthForDetail(status, connection)
        val rows = mutableListOf<Pair<String, String>>()
        rows += "id" to empty(firstNonBlank(status.networkId, connectionNetworkID(connection)), "unknown")
        rows += "transports" to multiLineValue(status.transportsList)
        rows += "interface" to empty(status.interfaceName, "none")
        rows += "mtu" to status.mtu.toString()
        rows += "validated" to status.validated.toString()
        rows += "internet" to status.internet.toString()
        if (capabilities.isNotEmpty()) rows += "capabilities" to multiLineValue(capabilities)
        networkBandwidth(status)?.let { rows += "bandwidth" to it }
        if (signalStrength != null) rows += "signal_strength" to signalStrength.toString()
        if (status.networkSpecifier.isNotBlank()) rows += "network_specifier" to status.networkSpecifier
        if (status.ownerUid > 0) rows += "owner_uid" to status.ownerUid.toString()
        if (status.enterpriseIdsList.isNotEmpty()) rows += "enterprise_ids" to multiLineValue(status.enterpriseIdsList)
        if (status.subscriptionIdsList.isNotEmpty()) {
            rows += "subscription_ids" to multiLineValue(status.subscriptionIdsList.map { it.toString() })
        }
        if (status.dhcpServer.isNotBlank()) rows += "dhcp_server" to status.dhcpServer
        if (status.privateDnsActive || status.privateDnsServerName.isNotBlank()) {
            rows += "private_dns" to "active=${status.privateDnsActive} server=${empty(status.privateDnsServerName, "none")}"
        }
        if (ipv4.isNotEmpty()) rows += "ipv4" to multiLineValue(ipv4)
        if (ipv6.isNotEmpty()) rows += "ipv6" to multiLineValue(ipv6)
        if (addresses.isNotEmpty()) rows += "addresses" to multiLineValue(addresses)
        if (status.dnsServersList.isNotEmpty()) rows += "dns" to multiLineValue(status.dnsServersList)
        if (status.routesList.isNotEmpty()) rows += "routes" to multiLineValue(status.routesList)
        if (status.domains.isNotBlank()) rows += "domains" to status.domains
        if (status.httpProxy.isNotBlank()) rows += "http_proxy" to status.httpProxy
        if (status.nat64Prefix.isNotBlank()) rows += "nat64_prefix" to status.nat64Prefix
        if (status.wakeOnLanSupported) rows += "wake_on_lan" to "true"
        return rows
    }

    private fun multiLineValue(values: List<String>): String {
        return values.filter { it.isNotBlank() }.joinToString("\n")
    }

    private fun connectionNetworkID(conn: WifiConnection?): String {
        val id = conn?.networkId ?: 0
        return if (id == 0) "" else id.toString()
    }

    private fun addressListContainsIP(values: List<String>, ip: String): Boolean {
        return values.any { value ->
            value.substringBefore('/').substringBefore('%').trim() == ip
        }
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

    private fun networkSignalStrengthForDetail(status: IpStatus, connection: WifiConnection?): Int? {
        val signalStrength = status.signalStrength
        if (signalStrength == 0 || signalStrength == Int.MIN_VALUE) return null
        if (connection != null && connection.rssiDbm == signalStrength) return null
        return signalStrength
    }

    private fun networkCapabilitiesForDetail(values: List<String>): List<String> {
        return values.filterNot { value ->
            value.isBlank() ||
                value.equals("internet", ignoreCase = true) ||
                value.equals("validated", ignoreCase = true)
        }.sorted()
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

    private fun table(out: MutableList<String>, headers: List<String>, rows: List<List<String>>) {
        val widths = headers.indices.map { index ->
            (listOf(headers[index]) + rows.map { it.getOrElse(index) { "" } }).maxOf { it.length }
        }
        out += headers.mapIndexed { index, value -> value.padEnd(widths[index]) }.joinToString("  ").trimEnd()
        rows.forEach { row ->
            out += headers.indices
                .map { index -> row.getOrElse(index) { "" }.padEnd(widths[index]) }
                .joinToString("  ")
                .trimEnd()
        }
    }

    private fun empty(value: String, fallback: String): String = value.ifBlank { fallback }

    private fun firstNonBlank(vararg values: String): String = values.firstOrNull { it.isNotBlank() }.orEmpty()

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
