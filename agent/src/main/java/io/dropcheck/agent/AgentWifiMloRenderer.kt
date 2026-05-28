package io.dropcheck.agent

import android.os.Build
import io.dropcheck.agent.grpc.MloLinkInfo
import io.dropcheck.agent.grpc.WifiEhtCapabilities
import io.dropcheck.agent.grpc.WifiEhtOperation
import io.dropcheck.agent.grpc.WifiHe6GhzCapabilities
import io.dropcheck.agent.grpc.WifiMcsNssSupport
import io.dropcheck.agent.grpc.WifiCapabilities
import io.dropcheck.agent.grpc.WifiConnection
import io.dropcheck.agent.grpc.WifiInformationElement
import io.dropcheck.agent.grpc.WifiScan
import io.dropcheck.agent.grpc.WifiScanResult
import io.dropcheck.agent.grpc.WifiSecurityDetails
import io.dropcheck.agent.grpc.WifiStatus

internal data class AgentWifiMloContext(
    val scanSource: String = "cached",
    val sdkInt: Int = Build.VERSION.SDK_INT,
    val wifi7Supported: Boolean? = null,
    val wifiCapabilities: WifiCapabilities? = null,
    val scanCommandStatus: String = "",
    val scanCommandMessage: String = "",
)

/** MLO-focused renderer for the on-device shell. */
internal object AgentWifiMloRenderer {
    fun render(status: WifiStatus, scan: WifiScan, context: AgentWifiMloContext = AgentWifiMloContext()): List<String> {
        val out = mutableListOf<String>()
        val candidates = scan.resultsList.filter { isMloCapableCandidate(it) }
        val groups = mloGroups(candidates)
        val current = activeWifiConnection(status)

        renderCurrentRelation(out, current, candidates)
        renderConnectedMlo(out, current)
        renderConnectedSecurityDetails(out, current)
        renderConnectedHe6GhzDetails(out, current)
        renderConnectedEhtMultiLink(out, current)
        renderConnectedEhtDetails(out, current)
        renderScanSummary(out, scan, candidates, context)
        renderNearbyMlo(out, groups, current)
        renderWifi7DeviceReadiness(out, context)
        renderDiagnostics(out, status, scan, current, candidates, context)
        return out
    }

    private fun renderConnectedMlo(out: MutableList<String>, conn: WifiConnection?) {
        section(out, "Connected MLO")
        if (conn == null) {
            out += "  no active Wi-Fi connection"
            return
        }
        val present = connectionHasMlo(conn)
        kv(out,
            "ssid" to empty(conn.ssid, "<hidden>"),
            "bssid" to empty(conn.bssid, "<unknown>"),
            "standard" to empty(conn.wifiStandard, "<unknown>"),
            "present" to present.toString(),
            "ap_mld" to empty(connectionMldMac(conn), "<none>"),
            "ap_link_id" to connectionLinkID(conn),
            "affiliated" to conn.affiliatedMloLinksCount.toString(),
            "associated" to conn.associatedMloLinksCount.toString(),
        )
        renderMloLinks(out, "Associated MLO Links", conn.associatedMloLinksList)
        renderMloLinks(out, "Affiliated MLO Links", conn.affiliatedMloLinksList)
    }

    private fun renderConnectedEhtMultiLink(out: MutableList<String>, conn: WifiConnection?) {
        if (conn == null) return
        val lines = formatEhtMultiLinkElements("connection", parseEhtMultiLinkElements(conn.informationElementsList))
        if (lines.isEmpty()) return
        section(out, "Connected EHT Multi-Link Elements")
        out += lines.map { "  $it" }
    }

    private fun renderConnectedEhtDetails(out: MutableList<String>, conn: WifiConnection?) {
        if (conn == null || !conn.hasEhtDetails()) return
        section(out, "Connected EHT Details")
        renderEhtDetails(out, "connection", conn.ehtCapabilities.takeIf { conn.hasEhtCapabilities() }, conn.ehtOperation.takeIf { conn.hasEhtOperation() })
    }

    private fun renderScanSummary(
        out: MutableList<String>,
        scan: WifiScan,
        candidates: List<WifiScanResult>,
        context: AgentWifiMloContext,
    ) {
        val fields = scan.fieldsList.associate { it.key to it.value }
        section(out, "MLO Scan")
        val rows = mutableListOf(
            "source" to context.scanSource,
            "results" to fields["scan_result_count"].orEmpty().ifBlank { scan.resultsCount.toString() },
            "total" to fields["scan_result_total_count"].orEmpty().ifBlank { scan.resultsCount.toString() },
            "mlo_candidates" to candidates.size.toString(),
            "errors" to scan.errorsCount.toString(),
        )
        listOf(
            "requested_band",
            "wifi_enabled",
            "wifi_state",
            "scan_always_available",
            "scan_throttle_enabled",
            "fresh_scan_start_scan",
            "fresh_scan_broadcast_received",
            "fresh_scan_results_updated",
            "fresh_scan_wait_completed",
            "fresh_scan_elapsed_ms",
        ).forEach { key ->
            fields[key]?.let { rows += key to it }
        }
        kv(out, rows)
    }

    private fun renderNearbyMlo(out: MutableList<String>, groups: List<MloGroup>, current: WifiConnection?) {
        section(out, "Nearby MLO APs")
        if (groups.isEmpty()) {
            out += "  no MLO-capable scan results"
            return
        }
        tableWithColumns(out,
            listOf(
                TableColumn("SSID", 14),
                TableColumn("BANDS", 8),
                TableColumn("RSSI", 4),
                TableColumn("SEC", 7),
                TableColumn("STANDARD", 8),
                TableColumn("EHT_W", 9),
                TableColumn("PUNCT", 7),
            ),
            groups.map { group ->
                listOf(
                    joined(group.results.map { empty(it.ssid, "<hidden>") }),
                    joined(group.bands, "unknown"),
                    group.bestRssi.toString(),
                    joined(group.security, "-"),
                    joined(group.standards, "-"),
                    groupEhtOperationWidths(group),
                    groupEhtOperationPuncturing(group),
                )
            },
        )
        renderScanLinks(out, groups, current)
        renderScanSecurityDetails(out, groups.flatMap { it.results })
        renderScanRnrDetails(out, groups.flatMap { it.results })
        renderScanMultipleBssidDetails(out, groups.flatMap { it.results })
        renderScanHe6GhzDetails(out, groups.flatMap { it.results })
        renderScanEhtMultiLink(out, groups.flatMap { it.results })
        renderScanEhtDetails(out, groups.flatMap { it.results })
    }

    private fun renderConnectedSecurityDetails(out: MutableList<String>, conn: WifiConnection?) {
        if (conn == null || !conn.hasSecurityDetails()) return
        section(out, "Connected Wi-Fi Security")
        renderSecurityDetails(out, "connection", conn.securityDetails)
    }

    private fun renderScanSecurityDetails(out: MutableList<String>, results: List<WifiScanResult>) {
        val securityResults = results.filter { it.hasSecurityDetails() }
        if (securityResults.isEmpty()) return
        section(out, "Scan Wi-Fi 7 Security")
        securityResults.forEachIndexed { index, result ->
            if (index > 0) out += ""
            val label = "ap ssid=${empty(result.ssid, "<hidden>")} bssid=${empty(result.bssid, "<unknown>")}"
            renderSecurityDetails(out, label, result.securityDetails)
        }
    }

    private fun renderSecurityDetails(out: MutableList<String>, label: String, value: WifiSecurityDetails) {
        out += "  $label"
        wifiMloSecuritySummaryLines(value).forEach { line -> out += "    $line" }
    }

    private fun renderScanRnrDetails(out: MutableList<String>, results: List<WifiScanResult>) {
        val lines = results.flatMap { result ->
            formatWifiMloRnrDetails(
                "ap ssid=${empty(result.ssid, "<hidden>")} bssid=${empty(result.bssid, "<unknown>")}",
                result.informationElementsList,
            )
        }
        if (lines.isEmpty()) return
        section(out, "Scan RNR Details")
        out += lines.map { "  $it" }
    }

    private fun renderScanMultipleBssidDetails(out: MutableList<String>, results: List<WifiScanResult>) {
        val lines = results.flatMap { result ->
            formatWifiMloMultipleBssidDetails(
                "ap ssid=${empty(result.ssid, "<hidden>")} bssid=${empty(result.bssid, "<unknown>")}",
                result.informationElementsList,
            )
        }
        if (lines.isEmpty()) return
        section(out, "Scan Multiple BSSID Details")
        out += lines.map { "  $it" }
    }

    private fun renderWifi7DeviceReadiness(out: MutableList<String>, context: AgentWifiMloContext) {
        val rows = wifi7DeviceReadinessRows(context.wifiCapabilities, context.wifi7Supported)
            .filter { it.second.isNotBlank() }
        if (rows.isEmpty()) return
        section(out, "Wi-Fi 7 Device Readiness")
        kv(out, rows)
    }

    private fun renderConnectedHe6GhzDetails(out: MutableList<String>, conn: WifiConnection?) {
        if (conn == null || !conn.hasHe6GhzCapabilities()) return
        section(out, "Connected HE 6GHz Details")
        out += "  connection ${he6GhzSummary(conn.he6GhzCapabilities)}"
    }

    private fun renderScanHe6GhzDetails(out: MutableList<String>, results: List<WifiScanResult>) {
        val he6Results = results.filter { it.hasHe6GhzCapabilities() }
        if (he6Results.isEmpty()) return
        section(out, "Scan HE 6GHz Details")
        he6Results.forEach { result ->
            val label = "ap ssid=${empty(result.ssid, "<hidden>")} bssid=${empty(result.bssid, "<unknown>")}"
            out += "  $label ${he6GhzSummary(result.he6GhzCapabilities)}"
        }
    }

    private fun renderScanEhtMultiLink(out: MutableList<String>, results: List<WifiScanResult>) {
        val lines = results.flatMap { result ->
            formatEhtMultiLinkElements(
                "ap ssid=${empty(result.ssid, "<hidden>")} bssid=${empty(result.bssid, "<unknown>")}",
                parseEhtMultiLinkElements(result.informationElementsList),
            )
        }
        if (lines.isEmpty()) return
        section(out, "Scan EHT Multi-Link Elements")
        out += lines.map { "  $it" }
    }

    private fun renderScanEhtDetails(out: MutableList<String>, results: List<WifiScanResult>) {
        val ehtResults = results.filter { it.hasEhtDetails() }
        if (ehtResults.isEmpty()) return
        section(out, "Scan EHT Details")
        ehtResults.forEachIndexed { index, result ->
            if (index > 0) out += ""
            val label = "ap ssid=${empty(result.ssid, "<hidden>")} bssid=${empty(result.bssid, "<unknown>")}"
            renderEhtDetails(out, label, result.ehtCapabilities.takeIf { result.hasEhtCapabilities() }, result.ehtOperation.takeIf { result.hasEhtOperation() })
        }
    }

    private fun renderEhtDetails(
        out: MutableList<String>,
        label: String,
        capabilities: WifiEhtCapabilities?,
        operation: WifiEhtOperation?,
    ) {
        out += "  $label"
        capabilities?.let { cap ->
            if (cap.hasMac()) out += ehtMacSummaryLines(cap).map { "    $it" }
            if (cap.hasPhy()) out += ehtPhySummaryLines(cap).map { "    $it" }
            if (cap.mcsNssCount > 0) out += mcsNssLines("mcs_nss", cap.mcsNssList).map { "    $it" }
            if (cap.ppeThresholdsPresent) {
                out += "    ppe nss=${cap.ppeNssCount} ru=${joined(cap.ppeRuIndicesList)} hex=0x${cap.ppeThresholdsHex}"
            }
            if (cap.warningsCount > 0) out += "    cap_warnings ${cap.warningsList.joinToString(",")}"
        }
        operation?.let { oper ->
            out += ehtOperationSummaryLines(oper).map { "    $it" }
            if (oper.basicMcsNssCount > 0) out += mcsNssLines("basic_mcs_nss", oper.basicMcsNssList).map { "    $it" }
            if (oper.warningsCount > 0) out += "    oper_warnings ${oper.warningsList.joinToString(",")}"
        }
    }

    private fun ehtMacSummaryLines(value: WifiEhtCapabilities): List<String> {
        val mac = value.mac
        val flags = listOfNotNull(
            "epcs".takeIf { mac.epcsPriorityAccess },
            "om_control".takeIf { mac.omControl },
            "restricted_twt".takeIf { mac.restrictedTwt },
            "trs".takeIf { mac.ehtTrs },
            "txop_return".takeIf { mac.txopReturn },
            "two_bqrs".takeIf { mac.twoBqrs },
            "unsol_epcs".takeIf { mac.unsolicitedEpcsPriorityAccess },
        )
        return listOf("mac max_mpdu=${mac.maxMpduLengthBytes} max_ampdu_ext=${mac.maxAmpduLengthExponentExtension} link_adapt=${empty(mac.linkAdaptation, "<unknown>")}") +
            wrappedListLines("mac flags", flags, "<none>")
    }

    private fun he6GhzSummary(value: WifiHe6GhzCapabilities): String {
        val flags = listOfNotNull(
            "rd_responder".takeIf { value.rdResponder },
            "rx_antpat".takeIf { value.rxAntennaPatternConsistency },
            "tx_antpat".takeIf { value.txAntennaPatternConsistency },
        )
        val warningSuffix = if (value.warningsCount > 0) " warnings=${value.warningsList.joinToString(",")}" else ""
        return "max_mpdu=${value.maxMpduLengthBytes} max_ampdu_exp=${value.maxAmpduLengthExponent} max_ampdu=${value.maxAmpduLengthBytes} min_mpdu_start=${empty(value.minimumMpduStartSpacing, "<unknown>")} smps=${empty(value.smPowerSave, "<unknown>")} flags=${joined(flags)}${warningSuffix}"
    }

    private fun ehtPhySummaryLines(value: WifiEhtCapabilities): List<String> {
        val phy = value.phy
        val muMimo = listOfNotNull(
            "80".takeIf { phy.nonOfdmaUlMuMimo80Mhz },
            "160".takeIf { phy.nonOfdmaUlMuMimo160Mhz },
            "320".takeIf { phy.nonOfdmaUlMuMimo320Mhz },
        )
        val muBeamformer = listOfNotNull(
            "80".takeIf { phy.muBeamformer80Mhz },
            "160".takeIf { phy.muBeamformer160Mhz },
            "320".takeIf { phy.muBeamformer320Mhz },
        )
        val mcs15 = listOfNotNull(
            "80".takeIf { phy.mcs15Supported80Mhz },
            "160".takeIf { phy.mcs15Supported160Mhz },
            "320".takeIf { phy.mcs15Supported320Mhz },
        )
        val qam = listOfNotNull(
            "1024qam_wider_dl_ofdma".takeIf { phy.rx1024QamWiderBwDlOfdma },
            "4096qam_wider_dl_ofdma".takeIf { phy.rx4096QamWiderBwDlOfdma },
        )
        return listOf(
            "phy caps 320mhz=${phy.supports320MhzIn6Ghz} ru_gt20=${phy.supports242ToneRuGt20Mhz} ltf=max${phy.maxSupportedEhtLtf}/extra=${phy.extraEhtLtfSupported} padding=${empty(phy.commonNominalPacketPadding, "<unknown>")}",
            "phy beamformee_ss 80=${phy.beamformeeSs80Mhz} 160=${phy.beamformeeSs160Mhz} 320=${phy.beamformeeSs320Mhz}",
            "phy sounding 80=${phy.soundingDimensions80Mhz} 160=${phy.soundingDimensions160Mhz} 320=${phy.soundingDimensions320Mhz}",
            "phy mu_mimo=${joined(muMimo)} mu_bf=${joined(muBeamformer)}",
            "phy mcs15=${joined(mcs15)} qam=${joined(qam)}",
        )
    }

    private fun ehtOperationSummaryLines(value: WifiEhtOperation): List<String> = buildList {
        add("oper op_info=${value.operationInformationPresent} width=${empty(value.channelWidth, "<unknown>")} width_mhz=${value.channelWidthMhz} code=${value.channelWidthCode}")
        add("oper ccfs0=${value.centerFreqSegment0} ccfs1=${value.centerFreqSegment1} mcs15_disabled=${value.mcs15Disabled} group_bu_exp=${value.groupAddressedBuIndicationExponent}")
        if (value.disabledSubchannelBitmapPresent || value.disabledSubchannelBitmap != 0) {
            add("oper disabled=0x${empty(value.disabledSubchannelBitmapHex, value.disabledSubchannelBitmap.toString(16))} punctured=${joined(value.disabledSubchannelIndicesList.map { it.toString() })}")
        }
    }

    private fun mcsNssLines(label: String, values: List<WifiMcsNssSupport>): List<String> {
        return values
            .groupBy { Triple(it.bandwidth, it.mcsRange, it.standard) }
            .map { (key, rows) ->
                val rx = rows.firstOrNull { it.direction == "rx" }
                val tx = rows.firstOrNull { it.direction == "tx" }
                val rxValue = rx?.maxNss?.takeIf { it > 0 } ?: rx?.nss ?: 0
                val txValue = tx?.maxNss?.takeIf { it > 0 } ?: tx?.nss ?: 0
                "$label ${key.third}/${key.first}/${key.second} rx=nss$rxValue tx=nss$txValue"
            }
    }

    private fun renderScanLinks(out: MutableList<String>, groups: List<MloGroup>, current: WifiConnection?) {
        if (groups.isEmpty()) return
        section(out, "MLO Scan Links")
        groups.forEach { group ->
            group.results.forEach { result ->
                renderScanLinkBlock(out, group, result, current)
                result.affiliatedMloLinksList.forEach { link ->
                    renderAffiliatedLinkBlock(out, group, result, link, current)
                }
            }
        }
    }

    private fun renderScanLinkBlock(
        out: MutableList<String>,
        group: MloGroup,
        result: WifiScanResult,
        current: WifiConnection?,
    ) {
        blockGap(out)
        blockTitle(out, resultMark(group, result, current), empty(result.ssid, "<hidden>"))
        out += "  ap_mld=${group.displayMld} link=${scanLinkID(result)} bssid=${empty(result.bssid, "<unknown>")}"
        out += "  band=${empty(result.band, wifiBandFromFrequency(result.frequencyMhz))} ch=${wifiChannelFromFrequency(result.frequencyMhz)} freq=${result.frequencyMhz}MHz width=${empty(formatWifiChannelWidth(result.channelWidth), "<unknown>")}${scanEhtOperationSuffix(result)} rssi=${result.rssiDbm}dBm"
        out += "  ${wifiMloInformationElementChecklist(result)}"
        out += "  ${wifiMloScanSdkFlags(result)}"
    }

    private fun renderAffiliatedLinkBlock(
        out: MutableList<String>,
        group: MloGroup,
        result: WifiScanResult,
        link: MloLinkInfo,
        current: WifiConnection?,
    ) {
        blockGap(out)
        blockTitle(out, linkMark(group, link, current), "affiliated ${empty(result.ssid, "<hidden>")}")
        out += "  ap_mld=${group.displayMld}"
        out += "  link=${link.linkId} parent_bssid=${empty(result.bssid, "<unknown>")}"
        out += "  band=${empty(link.band, "<unknown>")} ch=${link.channel} state=${empty(link.state, "<unknown>")} rssi=${link.rssiDbm}dBm tx=${link.txLinkSpeedMbps} rx=${link.rxLinkSpeedMbps} max_tx=${link.maxSupportedTxLinkSpeedMbps} max_rx=${link.maxSupportedRxLinkSpeedMbps} ap_mac=${empty(link.apMacAddress, "<unknown>")}"
    }

    private fun blockTitle(out: MutableList<String>, mark: String, label: String) {
        val prefix = mark.ifBlank { "-" }
        out += "[$prefix] $label"
    }

    private fun blockGap(out: MutableList<String>) {
        if (out.isNotEmpty() && out.last().isNotBlank() && out.last() != "MLO Scan Links") {
            out += ""
        }
    }

    private fun renderCurrentRelation(out: MutableList<String>, current: WifiConnection?, candidates: List<WifiScanResult>) {
        section(out, "Current AP Relation")
        if (current == null) {
            out += "  no active Wi-Fi connection"
            return
        }
        val sameMldResults = candidates.filter { sameMld(current, it) }
        val visibleLinks = sameMldResults.flatMap { scanLinkIds(it) }.toSet()
        val associatedLinks = associatedLinkIds(current)
        val missingAssociatedLinks = associatedLinks - visibleLinks
        kv(out,
            "connected_bssid" to empty(current.bssid, "<unknown>"),
            "connected_ap_mld" to empty(connectionMldMac(current), "<none>"),
            "connected_link" to connectionLinkID(current),
            "current_bssid_seen" to candidates.any { bssidEquals(it.bssid, current.bssid) }.toString(),
            "same_mld_results" to sameMldResults.size.toString(),
            "visible_links" to joined(visibleLinks.map { it.toString() }, "<none>"),
            "associated_links" to joined(associatedLinks.map { it.toString() }, "<none>"),
            "missing_associated" to joined(missingAssociatedLinks.map { it.toString() }, "<none>"),
        )
    }

    private fun renderDiagnostics(
        out: MutableList<String>,
        status: WifiStatus,
        scan: WifiScan,
        current: WifiConnection?,
        candidates: List<WifiScanResult>,
        context: AgentWifiMloContext,
    ) {
        val warnings = mutableListOf<String>()
        val fields = scan.fieldsList.associate { it.key to it.value }
        if (context.sdkInt < 33) {
            warnings += "android_api_unavailable sdk=${context.sdkInt} required_sdk=33"
        }
        if (context.wifi7Supported == false) {
            warnings += "wifi_7_standard_unsupported"
        }
        if (!status.enabled) {
            warnings += "wifi_disabled"
        }
        if (status.state.isNotBlank() && status.state != "enabled") {
            warnings += "wifi_state=${status.state}"
        }
        status.permissionsList.filterNot { it.endsWith("=granted") }.forEach {
            warnings += "permission $it"
        }
        if (fields["wifi_enabled"] == "false") {
            warnings += "scan_wifi_enabled=false"
        }
        if (fields["scan_always_available"] == "false") {
            warnings += "scan_always_available=false"
        }
        if (fields["scan_throttle_enabled"] == "true") {
            warnings += "scan_throttle_enabled=true"
        }
        scan.errorsList.forEach {
            warnings += "scan_error=$it"
        }
        if (context.scanCommandStatus.isNotBlank() && context.scanCommandStatus != "STATUS_OK") {
            warnings += "scan_command_status=${context.scanCommandStatus}"
        }
        if (context.scanCommandMessage.isNotBlank()) {
            warnings += "scan_command_message=${context.scanCommandMessage}"
        }
        if (current != null && !connectionHasMlo(current)) {
            warnings += "connected_mlo_present=false"
        }
        if (candidates.isEmpty()) {
            warnings += "mlo_scan_results=0"
        }
        if (current != null && current.apMldMacAddress.isNotBlank() && candidates.none { sameMld(current, it) }) {
            warnings += "connected_ap_mld_not_seen_in_scan"
        }
        warnings += mloMetadataWarnings(candidates)
        if (current != null) {
            val visibleLinks = candidates.filter { sameMld(current, it) }.flatMap { scanLinkIds(it) }.toSet()
            val missingLinks = associatedLinkIds(current) - visibleLinks
            if (missingLinks.isNotEmpty()) {
                warnings += "associated_link_missing_from_scan ids=${joined(missingLinks.map { it.toString() })}"
            }
        }

        section(out, "Diagnostics / Warnings")
        if (warnings.isEmpty()) {
            out += "  none"
        } else {
            warnings.distinct().forEach { out += "  $it" }
        }
    }

    private fun mloMetadataWarnings(candidates: List<WifiScanResult>): List<String> {
        val beResults = candidates.filter { it.wifiStandard.equals("802.11be", ignoreCase = true) }
        if (beResults.isEmpty()) return emptyList()

        val apMldSeen = beResults.count { it.apMldMacAddress.isNotBlank() || mloMldMacFromElements(it.informationElementsList).isNotBlank() }
        val linkIdSeen = beResults.count { it.apMloLinkId >= 0 || mloCurrentLinkIdFromElements(it.informationElementsList) != null }
        val withoutMetadata = beResults.count { !it.hasMloScanMetadata() && it.apMloLinkId < 0 }
        return when {
            apMldSeen == 0 && linkIdSeen == 0 ->
                listOf("scan_mlo_metadata_absent 11be_results=${beResults.size} ap_mld=0 link_id=0")
            withoutMetadata > 0 ->
                listOf("scan_mlo_metadata_partial missing=$withoutMetadata 11be_results=${beResults.size}")
            else -> emptyList()
        }
    }

    private fun renderMloLinks(out: MutableList<String>, title: String, links: List<MloLinkInfo>) {
        if (links.isEmpty()) return
        section(out, title)
        table(out,
            listOf("ID", "STATE", "BAND", "CHANNEL", "RSSI", "TX", "RX", "MAX_TX", "MAX_RX", "AP_MAC", "STA_MAC"),
            links.map { link ->
                listOf(
                    link.linkId.toString(),
                    empty(link.state, "<unknown>"),
                    empty(link.band, "<unknown>"),
                    link.channel.toString(),
                    link.rssiDbm.toString(),
                    link.txLinkSpeedMbps.toString(),
                    link.rxLinkSpeedMbps.toString(),
                    link.maxSupportedTxLinkSpeedMbps.toString(),
                    link.maxSupportedRxLinkSpeedMbps.toString(),
                    empty(link.apMacAddress, "<unknown>"),
                    empty(link.staMacAddress, "<unknown>"),
                )
            },
        )
    }

    private fun mloGroups(results: List<WifiScanResult>): List<MloGroup> {
        return results
            .groupBy { groupKey(it) }
            .map { (_, groupResults) -> MloGroup(groupResults) }
            .sortedWith(compareByDescending<MloGroup> { it.bestRssi }.thenBy { it.displayMld })
    }

    private fun groupKey(result: WifiScanResult): String {
        val mld = result.mloMldMac().trim().lowercase()
        if (mld.isNotBlank()) return "mld:$mld"
        val bssid = result.bssid.trim().lowercase()
        if (bssid.isNotBlank()) return "bssid:$bssid"
        return "unknown:${result.ssid}:${result.frequencyMhz}"
    }

    private fun isMloCapableCandidate(result: WifiScanResult): Boolean {
        return result.hasMloScanMetadata() ||
            result.wifiStandard.equals("802.11be", ignoreCase = true)
    }

    private fun WifiScanResult.hasMloScanMetadata(): Boolean {
        return apMldMacAddress.isNotBlank() ||
            affiliatedMloLinksCount > 0 ||
            hasMloElement(informationElementsList)
    }

    private fun WifiConnection.hasEhtDetails(): Boolean = hasEhtCapabilities() || hasEhtOperation()

    private fun WifiScanResult.hasEhtDetails(): Boolean = hasEhtCapabilities() || hasEhtOperation()

    private fun connectionHasMlo(conn: WifiConnection): Boolean {
        return conn.apMldMacAddress.isNotBlank() ||
            conn.affiliatedMloLinksCount > 0 ||
            conn.associatedMloLinksCount > 0 ||
            hasMloElement(conn.informationElementsList) ||
            (conn.wifiStandard.equals("802.11be", ignoreCase = true) && conn.apMloLinkId >= 0)
    }

    private fun sameMld(conn: WifiConnection, result: WifiScanResult): Boolean {
        val currentMld = connectionMldMac(conn).trim()
        val resultMld = result.mloMldMac()
        if (currentMld.isNotBlank() && currentMld.equals(resultMld, ignoreCase = true)) return true
        return bssidEquals(conn.bssid, result.bssid)
    }

    private fun bssidEquals(left: String, right: String): Boolean {
        return left.isNotBlank() && right.isNotBlank() && left.equals(right, ignoreCase = true)
    }

    private fun resultMark(group: MloGroup, result: WifiScanResult, current: WifiConnection?): String {
        if (current == null) return ""
        return when {
            bssidEquals(result.bssid, current.bssid) -> "*"
            group.results.any { sameMld(current, it) } -> "+"
            else -> ""
        }
    }

    private fun linkMark(group: MloGroup, link: MloLinkInfo, current: WifiConnection?): String {
        if (current == null) return ""
        return when {
            bssidEquals(link.apMacAddress, current.bssid) -> "*"
            group.results.any { sameMld(current, it) } -> "+"
            else -> ""
        }
    }

    private fun scanLinkIds(result: WifiScanResult): Set<Int> {
        val ids = mutableSetOf<Int>()
        if (isMloCapableCandidate(result) && result.apMloLinkId >= 0) {
            ids += result.apMloLinkId
        }
        result.affiliatedMloLinksList.filter { it.linkId >= 0 }.forEach { ids += it.linkId }
        ids += mloLinkIdsFromElements(result.informationElementsList)
        return ids
    }

    private fun associatedLinkIds(conn: WifiConnection): Set<Int> {
        val ids = mutableSetOf<Int>()
        if (connectionHasMlo(conn) && conn.apMloLinkId >= 0) ids += conn.apMloLinkId
        conn.associatedMloLinksList.filter { it.linkId >= 0 }.forEach { ids += it.linkId }
        ids += mloLinkIdsFromElements(conn.informationElementsList)
        return ids
    }

    private fun connectionLinkID(conn: WifiConnection): String {
        if (!connectionHasMlo(conn)) return "<none>"
        if (conn.apMloLinkId >= 0) return conn.apMloLinkId.toString()
        return mloCurrentLinkIdFromElements(conn.informationElementsList)?.toString() ?: "<none>"
    }

    private fun scanLinkID(result: WifiScanResult): String {
        val explicitMlo = result.hasMloScanMetadata()
        val elementLinkId = mloCurrentLinkIdFromElements(result.informationElementsList)
        return when {
            (explicitMlo || result.wifiStandard.equals("802.11be", ignoreCase = true)) && result.apMloLinkId >= 0 -> result.apMloLinkId.toString()
            elementLinkId != null -> elementLinkId.toString()
            explicitMlo || result.wifiStandard.equals("802.11be", ignoreCase = true) -> "<unknown>"
            else -> "<none>"
        }
    }

    private fun WifiScanResult.mloMldMac(): String =
        firstNonBlank(apMldMacAddress, mloMldMacFromElements(informationElementsList))

    private fun connectionMldMac(conn: WifiConnection): String =
        firstNonBlank(conn.apMldMacAddress, mloMldMacFromElements(conn.informationElementsList))

    private fun hasMloElement(elements: List<WifiInformationElement>): Boolean =
        parseEhtMultiLinkElements(elements).isNotEmpty()

    private fun mloMldMacFromElements(elements: List<WifiInformationElement>): String {
        return parseEhtMultiLinkElements(elements)
            .firstNotNullOfOrNull { it.commonInfo?.mldMacAddress?.takeIf(String::isNotBlank) }
            .orEmpty()
    }

    private fun mloCurrentLinkIdFromElements(elements: List<WifiInformationElement>): Int? {
        return parseEhtMultiLinkElements(elements).firstNotNullOfOrNull { it.commonInfo?.linkId }
    }

    private fun mloLinkIdsFromElements(elements: List<WifiInformationElement>): Set<Int> {
        val ids = mutableSetOf<Int>()
        parseEhtMultiLinkElements(elements).forEach { element ->
            element.commonInfo?.linkId?.let { ids += it }
            element.subelements.mapNotNull { it.perStaProfile?.linkId }.forEach { ids += it }
        }
        return ids
    }

    private fun security(result: WifiScanResult): String {
        return result.securityTypesList.joinToString(",").ifBlank { result.capabilities }
    }

    private fun groupEhtOperationWidths(group: MloGroup): String {
        return joined(group.results.map { scanEhtOperationWidth(it) }, "-")
    }

    private fun groupEhtOperationPuncturing(group: MloGroup): String {
        return joined(group.results.map { scanEhtOperationPuncturing(it) }, "-")
    }

    private fun scanEhtOperationSuffix(result: WifiScanResult): String {
        val width = scanEhtOperationWidth(result)
        val puncturing = scanEhtOperationPuncturing(result)
        if (width.isBlank() && puncturing.isBlank()) return ""
        return " eht_width=${empty(width, "<unknown>")} puncture=${empty(puncturing, "-")}"
    }

    private fun scanEhtOperationWidth(result: WifiScanResult): String {
        if (!result.hasEhtOperation()) return ""
        val operation = result.ehtOperation
        if (!operation.operationInformationPresent) return "<unknown>"
        if (operation.channelWidthMhz > 0) return "${operation.channelWidthMhz}MHz"
        return operation.channelWidth
    }

    private fun scanEhtOperationPuncturing(result: WifiScanResult): String {
        if (!result.hasEhtOperation()) return ""
        val operation = result.ehtOperation
        if (!operation.disabledSubchannelBitmapPresent && operation.disabledSubchannelBitmap == 0) return ""
        if (operation.disabledSubchannelIndicesCount == 0) return "none"
        return operation.disabledSubchannelIndicesList.joinToString(",")
    }

    private fun section(out: MutableList<String>, title: String) {
        if (out.isNotEmpty() && out.last().isNotBlank()) out += ""
        out += title
    }

    private fun kv(out: MutableList<String>, vararg rows: Pair<String, String>) {
        kv(out, rows.toList())
    }

    private fun kv(out: MutableList<String>, rows: List<Pair<String, String>>) {
        val filtered = rows.filter { it.first.isNotBlank() && it.second.isNotBlank() }
        val width = filtered.maxOfOrNull { it.first.length } ?: 0
        filtered.forEach { (key, value) ->
            out += "  ${key.padEnd(width)}  $value"
        }
    }

    private fun table(out: MutableList<String>, headers: List<String>, rows: List<List<String>>) {
        tableWithColumns(out, headers.map { TableColumn(it) }, rows)
    }

    private fun tableWithColumns(out: MutableList<String>, columns: List<TableColumn>, rows: List<List<String>>) {
        val preparedRows = rows.map { row ->
            columns.indices.map { index ->
                fitCell(row.getOrElse(index) { "" }, columns[index].maxWidth)
            }
        }
        val preparedHeaders = columns.map { fitCell(it.header, it.maxWidth) }
        val widths = columns.indices.map { index ->
            (listOf(preparedHeaders[index]) + preparedRows.map { it[index] }).maxOf { displayWidth(it) }
        }
        out += preparedHeaders.mapIndexed { index, value -> padDisplayEnd(value, widths[index]) }.joinToString("  ").trimEnd()
        rows.forEach { row ->
            val prepared = columns.indices.map { index ->
                fitCell(row.getOrElse(index) { "" }, columns[index].maxWidth)
            }
            out += columns.indices
                .map { index -> padDisplayEnd(prepared[index], widths[index]) }
                .joinToString("  ")
                .trimEnd()
        }
    }

    private fun fitCell(value: String, maxWidth: Int): String {
        val cleaned = value.replace('\t', ' ')
        if (maxWidth == Int.MAX_VALUE || displayWidth(cleaned) <= maxWidth) return cleaned
        if (maxWidth <= 0) return ""
        if (maxWidth <= 3) return ".".repeat(maxWidth)

        val suffix = "..."
        val targetWidth = maxWidth - displayWidth(suffix)
        val builder = StringBuilder()
        var width = 0
        var index = 0
        while (index < cleaned.length) {
            val codePoint = cleaned.codePointAt(index)
            val codePointWidth = codePointDisplayWidth(codePoint)
            if (width + codePointWidth > targetWidth) break
            builder.appendCodePoint(codePoint)
            width += codePointWidth
            index += Character.charCount(codePoint)
        }
        return builder.append(suffix).toString()
    }

    private fun padDisplayEnd(value: String, width: Int): String {
        val padding = width - displayWidth(value)
        return if (padding <= 0) value else value + " ".repeat(padding)
    }

    private fun displayWidth(value: String): Int {
        var width = 0
        var index = 0
        while (index < value.length) {
            val codePoint = value.codePointAt(index)
            width += codePointDisplayWidth(codePoint)
            index += Character.charCount(codePoint)
        }
        return width
    }

    private fun codePointDisplayWidth(codePoint: Int): Int {
        val type = Character.getType(codePoint)
        if (Character.isISOControl(codePoint) ||
            type == Character.NON_SPACING_MARK.toInt() ||
            type == Character.ENCLOSING_MARK.toInt() ||
            type == Character.COMBINING_SPACING_MARK.toInt()
        ) {
            return 0
        }
        return if (isWideCodePoint(codePoint)) 2 else 1
    }

    private fun isWideCodePoint(codePoint: Int): Boolean {
        return when (codePoint) {
            in 0x1100..0x11FF,
            in 0x2E80..0xA4CF,
            in 0xAC00..0xD7A3,
            in 0xF900..0xFAFF,
            in 0xFE10..0xFE19,
            in 0xFE30..0xFE6F,
            in 0xFF01..0xFF60,
            in 0xFFE0..0xFFE6,
            in 0x1F200..0x1F2FF,
            in 0x20000..0x3FFFD -> true
            else -> false
        }
    }

    private fun joined(values: Iterable<String>, emptyValue: String = "<none>"): String {
        return values.filter { it.isNotBlank() }.distinct().joinToString(",").ifBlank { emptyValue }
    }

    private fun wrappedListLines(label: String, values: Iterable<String>, emptyValue: String): List<String> {
        val unique = values.filter { it.isNotBlank() }.distinct()
        if (unique.isEmpty()) return listOf("$label $emptyValue")
        val lines = mutableListOf<String>()
        var current = ""
        unique.forEach { value ->
            val next = if (current.isEmpty()) value else "$current,$value"
            if (next.length > 56 && current.isNotEmpty()) {
                lines += "$label $current"
                current = value
            } else {
                current = next
            }
        }
        if (current.isNotEmpty()) lines += "$label $current"
        return lines
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

    private class MloGroup(val results: List<WifiScanResult>) {
        val displayMld: String = results.firstNotNullOfOrNull { it.mloMldMac().takeIf(String::isNotBlank) } ?: "<unknown>"
        val bestRssi: Int = results.maxOfOrNull { it.rssiDbm } ?: 0
        val bands: List<String> = results.flatMap { result ->
            listOf(result.band.ifBlank { wifiBandFromFrequency(result.frequencyMhz) }) +
                result.affiliatedMloLinksList.map { it.band }
        }.filter { it.isNotBlank() }.distinct()
        val security: List<String> = results.map { security(it) }.filter { it.isNotBlank() }.distinct()
        val standards: List<String> = results.map { it.wifiStandard }.filter { it.isNotBlank() }.distinct()
    }

    private data class TableColumn(val header: String, val maxWidth: Int = Int.MAX_VALUE)
}
