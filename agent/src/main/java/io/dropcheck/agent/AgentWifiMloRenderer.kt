package io.dropcheck.agent

import android.os.Build
import io.dropcheck.agent.grpc.MloLinkInfo
import io.dropcheck.agent.grpc.WifiEhtCapabilities
import io.dropcheck.agent.grpc.WifiEhtOperation
import io.dropcheck.agent.grpc.WifiHe6GhzCapabilities
import io.dropcheck.agent.grpc.WifiHeCapabilities
import io.dropcheck.agent.grpc.WifiHeMuEdcaParameterSet
import io.dropcheck.agent.grpc.WifiHeOperation
import io.dropcheck.agent.grpc.WifiHeSpatialReuseParameterSet
import io.dropcheck.agent.grpc.WifiHeUoraParameterSet
import io.dropcheck.agent.grpc.WifiMcsNssSupport
import io.dropcheck.agent.grpc.WifiCapabilities
import io.dropcheck.agent.grpc.WifiConnection
import io.dropcheck.agent.grpc.WifiInformationElement
import io.dropcheck.agent.grpc.WifiScan
import io.dropcheck.agent.grpc.WifiScanResult
import io.dropcheck.agent.grpc.WifiSecurityDetails
import io.dropcheck.agent.grpc.WifiStatus

internal data class AgentWifiMloContext(
    val brief: Boolean = false,
    val scanSource: String = "cached",
    val sdkInt: Int = Build.VERSION.SDK_INT,
    val wifi7Supported: Boolean? = null,
    val wifiCapabilities: WifiCapabilities? = null,
    val ssidFilter: String = "",
    val bssidFilter: String = "",
    val scanCommandStatus: String = "",
    val scanCommandMessage: String = "",
)

/** EHT-focused renderer for the on-device shell. */
internal object AgentWifiMloRenderer {
    fun render(status: WifiStatus, scan: WifiScan, context: AgentWifiMloContext = AgentWifiMloContext()): List<String> {
        val out = mutableListOf<String>()
        val filter = MloFilter(context.ssidFilter, context.bssidFilter)
        val scanResults = filter.scanResults(scan.resultsList)
        val candidates = scanResults.filter { isMloCapableCandidate(it) }
        val groups = mloGroups(candidates)
        val current = filter.connection(activeWifiConnection(status))
        if (context.brief) {
            renderBrief(out, scan, candidates, groups, current, filter, context)
            return out
        }

        renderCurrentRelation(out, current, candidates)
        renderConnectedMlo(out, current)
        renderConnectedSecurityDetails(out, current)
        renderConnectedRoamingDetails(out, current)
        renderConnectedBssColoring(out, current)
        renderConnectedHeDetails(out, current)
        renderConnectedHe6GhzDetails(out, current)
        renderConnectedEhtMultiLink(out, current)
        renderConnectedEhtDetails(out, current)
        renderConnectedEhtPuncturing(out, current)
        renderScanSummary(out, scan, candidates, scanResults.size, filter, context)
        renderNearbyMlo(out, groups, current)
        renderWifi7DeviceReadiness(out, context)
        renderWifiMloCapabilities(out, context)
        renderDiagnostics(out, status, scan, current, candidates, context)
        return out
    }

    private fun renderBrief(
        out: MutableList<String>,
        scan: WifiScan,
        candidates: List<WifiScanResult>,
        groups: List<MloGroup>,
        current: WifiConnection?,
        filter: MloFilter,
        context: AgentWifiMloContext,
    ) {
        renderScanSummary(out, scan, candidates, filter.scanResults(scan.resultsList).size, filter, context)
        renderNearbyMloBrief(out, groups, current)
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
        filteredResults: Int,
        filter: MloFilter,
        context: AgentWifiMloContext,
    ) {
        val fields = scan.fieldsList.associate { it.key to it.value }
        section(out, "EHT Scan")
        val rows = mutableListOf(
            "source" to context.scanSource,
            "results" to fields["scan_result_count"].orEmpty().ifBlank { scan.resultsCount.toString() },
            "total" to fields["scan_result_total_count"].orEmpty().ifBlank { scan.resultsCount.toString() },
            "eht_candidates" to candidates.size.toString(),
            "errors" to scan.errorsCount.toString(),
        )
        if (filter.active) {
            rows += "filter" to filter.label
            rows += "filtered_results" to filteredResults.toString()
        }
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
        section(out, "Nearby EHT APs")
        if (groups.isEmpty()) {
            out += "  no EHT-capable scan results"
            return
        }
        renderNearbyMloTable(out, groups)
        renderScanLinks(out, groups, current)
        renderScanSecurityDetails(out, groups.flatMap { it.results })
        renderScanRoamingDetails(out, groups.flatMap { it.results })
        renderScanBssColoring(out, groups.flatMap { it.results })
        renderScanRnrDetails(out, groups.flatMap { it.results })
        renderScanMultipleBssidDetails(out, groups.flatMap { it.results })
        renderScanHeDetails(out, groups.flatMap { it.results })
        renderScanHe6GhzDetails(out, groups.flatMap { it.results })
        renderScanEhtMultiLink(out, groups.flatMap { it.results })
        renderScanEhtPuncturing(out, groups.flatMap { it.results })
        renderScanEhtDetails(out, groups.flatMap { it.results })
    }

    private fun renderNearbyMloBrief(out: MutableList<String>, groups: List<MloGroup>, current: WifiConnection?) {
        section(out, "Nearby EHT MLDs")
        if (groups.isEmpty()) {
            out += "  no EHT-capable scan results"
            return
        }
        tableWithColumns(out,
            listOf(
                TableColumn("ITEM"),
                TableColumn("MLD", 17),
                TableColumn("BAND", 6),
                TableColumn("RSSI", 4),
                TableColumn("SEC", 7),
                TableColumn("STD", 4),
                TableColumn("CLR", 5),
                TableColumn("EHT", 9),
                TableColumn("ADDR", 17),
            ),
            groups.flatMap { group ->
                buildList {
                    add(listOf(
                        joined(group.results.map { empty(it.ssid, "<hidden>") }),
                        briefCell(group.displayMld),
                        "",
                        "",
                        "",
                        "",
                        "",
                        "",
                        "",
                    ))
                    val links = briefLinkRows(group, current)
                    links.forEachIndexed { index, link ->
                        add(listOf(
                            briefNodeLabel(index, links.size, link.mark, link.linkId),
                            "",
                            link.band,
                            link.rssi,
                            link.security,
                            link.standard,
                            link.color,
                            link.eht,
                            link.addr,
                        ))
                    }
                }
            },
        )
    }

    private fun renderNearbyMloTable(out: MutableList<String>, groups: List<MloGroup>) {
        tableWithColumns(out,
            listOf(
                TableColumn("SSID", 14),
                TableColumn("BANDS", 8),
                TableColumn("RSSI", 4),
                TableColumn("SEC", 7),
                TableColumn("STANDARD", 8),
                TableColumn("COLOR", 8),
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
                    groupBssColors(group),
                    groupEhtOperationWidths(group),
                    groupEhtOperationPuncturing(group),
                )
            },
        )
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

    private fun renderConnectedRoamingDetails(out: MutableList<String>, conn: WifiConnection?) {
        if (conn == null) return
        val lines = wifiMloRoamingSummaryLines(
            conn.informationElementsList,
            conn.securityDetails.takeIf { conn.hasSecurityDetails() },
        )
        if (lines.isEmpty()) return
        section(out, "Connected Roaming / Transition")
        out += "  connection"
        lines.forEach { line -> out += "    $line" }
    }

    private fun renderScanRoamingDetails(out: MutableList<String>, results: List<WifiScanResult>) {
        val entries = results.mapNotNull { result ->
            val lines = wifiMloRoamingSummaryLines(
                result.informationElementsList,
                result.securityDetails.takeIf { result.hasSecurityDetails() },
            )
            if (lines.isEmpty()) {
                null
            } else {
                "ap ssid=${empty(result.ssid, "<hidden>")} bssid=${empty(result.bssid, "<unknown>")}" to lines
            }
        }
        if (entries.isEmpty()) return
        section(out, "Scan Roaming / Transition")
        entries.forEachIndexed { index, (label, lines) ->
            if (index > 0) out += ""
            out += "  $label"
            lines.forEach { line -> out += "    $line" }
        }
    }

    private fun renderConnectedBssColoring(out: MutableList<String>, conn: WifiConnection?) {
        if (conn == null || !conn.hasBssColoringContext()) return
        section(out, "Connected BSS Coloring")
        renderBssColoring(
            out,
            "connection",
            conn.heOperation.takeIf { conn.hasHeOperation() },
            conn.heSpatialReuseParameterSet.takeIf { conn.hasHeSpatialReuseParameterSet() },
        )
    }

    private fun renderScanBssColoring(out: MutableList<String>, results: List<WifiScanResult>) {
        val colorResults = results.filter { it.hasBssColoringContext() }
        if (colorResults.isEmpty()) return
        section(out, "Scan BSS Coloring")
        colorResults.forEachIndexed { index, result ->
            if (index > 0) out += ""
            val label = "ap ssid=${empty(result.ssid, "<hidden>")} bssid=${empty(result.bssid, "<unknown>")}"
            renderBssColoring(
                out,
                label,
                result.heOperation.takeIf { result.hasHeOperation() },
                result.heSpatialReuseParameterSet.takeIf { result.hasHeSpatialReuseParameterSet() },
            )
        }
    }

    private fun renderBssColoring(
        out: MutableList<String>,
        label: String,
        operation: WifiHeOperation?,
        spatialReuse: WifiHeSpatialReuseParameterSet?,
    ) {
        out += "  $label"
        bssColoringSummaryLines(operation, spatialReuse).forEach { out += "    $it" }
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

    private fun renderWifiMloCapabilities(out: MutableList<String>, context: AgentWifiMloContext) {
        val capabilities = context.wifiCapabilities ?: return
        val rows = mutableListOf<Pair<String, String>>()
        if (capabilities.supportedStandardsCount > 0) {
            rows += "supported_standards" to capabilities.supportedStandardsList.joinToString(",")
        }
        if (capabilities.unsupportedStandardsCount > 0) {
            rows += "unsupported_standards" to capabilities.unsupportedStandardsList.joinToString(",")
        }
        val features = wifiMloRelevantStrings(capabilities.supportedFeaturesList + capabilities.unsupportedFeaturesList)
        if (features.isNotEmpty()) rows += "mlo_features" to features.joinToString(",")

        val fieldRows = capabilities.fieldsList
            .filter { wifiMloRelevantText(it.key) || wifiMloRelevantText(it.value) }
            .map { it.key to it.value }

        if (rows.isNotEmpty()) {
            section(out, "MLO Capability Signals")
            kv(out, rows)
        }
        if (fieldRows.isNotEmpty()) {
            section(out, "MLO Capability Fields")
            kv(out, fieldRows)
        }
        if (capabilities.errorsCount > 0) {
            section(out, "MLO Capability Errors")
            capabilities.errorsList.forEach { out += "  $it" }
        }
    }

    private fun renderConnectedHeDetails(out: MutableList<String>, conn: WifiConnection?) {
        if (conn == null || !conn.hasHeDetails()) return
        section(out, "Connected HE Details")
        renderHeDetails(
            out,
            "connection",
            conn.heCapabilities.takeIf { conn.hasHeCapabilities() },
            conn.heOperation.takeIf { conn.hasHeOperation() },
            conn.heUoraParameterSet.takeIf { conn.hasHeUoraParameterSet() },
            conn.heMuEdcaParameterSet.takeIf { conn.hasHeMuEdcaParameterSet() },
        )
    }

    private fun renderScanHeDetails(out: MutableList<String>, results: List<WifiScanResult>) {
        val heResults = results.filter { it.hasHeDetails() }
        if (heResults.isEmpty()) return
        section(out, "Scan HE Details")
        heResults.forEachIndexed { index, result ->
            if (index > 0) out += ""
            val label = "ap ssid=${empty(result.ssid, "<hidden>")} bssid=${empty(result.bssid, "<unknown>")}"
            renderHeDetails(
                out,
                label,
                result.heCapabilities.takeIf { result.hasHeCapabilities() },
                result.heOperation.takeIf { result.hasHeOperation() },
                result.heUoraParameterSet.takeIf { result.hasHeUoraParameterSet() },
                result.heMuEdcaParameterSet.takeIf { result.hasHeMuEdcaParameterSet() },
            )
        }
    }

    private fun renderHeDetails(
        out: MutableList<String>,
        label: String,
        capabilities: WifiHeCapabilities?,
        operation: WifiHeOperation?,
        uora: WifiHeUoraParameterSet?,
        muEdca: WifiHeMuEdcaParameterSet?,
    ) {
        out += "  $label"
        capabilities?.let { cap ->
            if (cap.hasMac()) out += heMacSummaryLines(cap).map { "    $it" }
            if (cap.hasPhy()) out += hePhySummaryLines(cap).map { "    $it" }
            if (cap.featuresCount > 0) out += wrappedListLines("he features", cap.featuresList, "<none>").map { "    $it" }
            if (cap.mcsNssCount > 0) out += mcsNssLines("he_mcs_nss", cap.mcsNssList).map { "    $it" }
            if (cap.ppeThresholdsPresent) {
                out += "    he_ppe nss=${cap.ppeNssCount} ru=${joined(cap.ppeRuIndicesList)} hex=0x${cap.ppeThresholdsHex}"
            }
            if (cap.warningsCount > 0) out += "    he_cap_warnings ${cap.warningsList.joinToString(",")}"
        }
        operation?.let { oper ->
            out += heOperationSummaryLines(oper).map { "    $it" }
        }
        uora?.let { value ->
            out += "    he_uora eocw_min=${value.eocwMin} eocw_max=${value.eocwMax}"
            if (value.warningsCount > 0) out += "    he_uora_warnings ${value.warningsList.joinToString(",")}"
        }
        muEdca?.let { value ->
            out += "    he_mu_edca qos_info=0x${value.qosInfo.toString(16)}"
            value.acList.forEach { ac ->
                out += "    he_mu_edca_ac ${ac.ac} aci=${ac.aci} aifsn=${ac.aifsn} acm=${ac.acm} ecw=${ac.ecwMin}/${ac.ecwMax} timer=${ac.timer}"
            }
            if (value.warningsCount > 0) out += "    he_mu_edca_warnings ${value.warningsList.joinToString(",")}"
        }
    }

    private fun heMacSummaryLines(value: WifiHeCapabilities): List<String> {
        val mac = value.mac
        val flags = listOfNotNull(
            "twt_requester".takeIf { mac.twtRequester },
            "twt_responder".takeIf { mac.twtResponder },
            "om_control".takeIf { mac.omControl },
            "ofdma_ra".takeIf { mac.ofdmaRandomAccess },
            "srp_responder".takeIf { mac.srpResponder },
            "ul_2x996".takeIf { mac.ul2X996ToneRu },
            "punctured_sounding".takeIf { mac.puncturedSounding },
            "ht_vht_trigger_rx".takeIf { mac.htVhtTriggerFrameRx },
        )
        return listOf(
            "he_mac link_adapt=${empty(mac.linkAdaptation, "<unknown>")} max_ampdu_ext=${mac.maxAmpduLengthExponentExtension} multi_tid_rx_qos=${mac.multiTidAggregationRxQos} multi_tid_tx_qos=${mac.multiTidAggregationTxQos}",
        ) + wrappedListLines("he_mac flags", flags, "<none>")
    }

    private fun hePhySummaryLines(value: WifiHeCapabilities): List<String> {
        val phy = value.phy
        return listOf(
            "he_phy widths=${joined(phy.channelWidthSetList)} puncture_rx=${joined(phy.preamblePuncturingRxList)} dcm_tx=${empty(phy.dcmMaxConstellationTx, "<unknown>")}/nss${phy.dcmMaxNssTx} dcm_rx=${empty(phy.dcmMaxConstellationRx, "<unknown>")}/nss${phy.dcmMaxNssRx}",
            "he_phy bf su_bfer=${phy.suBeamformer} su_bfee=${phy.suBeamformee} mu_bfer=${phy.muBeamformer} bfee_sts<=80=${phy.beamformeeStsUnder80Mhz} bfee_sts>80=${phy.beamformeeStsAbove80Mhz}",
            "he_phy spatial_reuse=${phy.srpBasedSpatialReuse} partial_bw_dl_mu_mimo=${phy.partialBwDlMuMimo} max_nc=${phy.maxNc} padding=${empty(phy.nominalPacketPadding, "<unknown>")}",
        )
    }

    private fun heOperationSummaryLines(value: WifiHeOperation): List<String> = buildList {
        add("he_oper params=0x${value.parameters.toString(16)} basic_mcs_nss=0x${value.basicMcsNssSetHex} bss_color=${value.bssColor} disabled=${value.bssColorDisabled}")
        addAll(wrappedListLines("he_oper flags", value.flagsList, "<none>"))
        if (value.channelWidth.isNotBlank() || value.primaryChannel != 0 || value.centerFreqSegment0 != 0 || value.centerFreqSegment1 != 0) {
            add("he_oper_6ghz width=${empty(value.channelWidth, "<unknown>")} primary=${value.primaryChannel} ccfs0=${value.centerFreqSegment0} ccfs1=${value.centerFreqSegment1}")
        }
        if (value.truncated) add("he_oper_truncated=true")
        if (value.warningsCount > 0) add("he_oper_warnings ${value.warningsList.joinToString(",")}")
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

    private fun renderConnectedEhtPuncturing(out: MutableList<String>, conn: WifiConnection?) {
        if (conn == null || !conn.hasEhtPuncturingContext()) return
        section(out, "Connected EHT Puncturing")
        renderEhtPuncturing(
            out,
            "connection",
            conn.heCapabilities.takeIf { conn.hasHeCapabilities() },
            conn.ehtOperation.takeIf { conn.hasEhtOperation() },
        )
    }

    private fun renderScanEhtPuncturing(out: MutableList<String>, results: List<WifiScanResult>) {
        val puncturingResults = results.filter { it.hasEhtPuncturingContext() }
        if (puncturingResults.isEmpty()) return
        section(out, "Scan EHT Puncturing")
        puncturingResults.forEachIndexed { index, result ->
            if (index > 0) out += ""
            val label = "ap ssid=${empty(result.ssid, "<hidden>")} bssid=${empty(result.bssid, "<unknown>")}"
            renderEhtPuncturing(
                out,
                label,
                result.heCapabilities.takeIf { result.hasHeCapabilities() },
                result.ehtOperation.takeIf { result.hasEhtOperation() },
            )
        }
    }

    private fun renderEhtPuncturing(
        out: MutableList<String>,
        label: String,
        he: WifiHeCapabilities?,
        operation: WifiEhtOperation?,
    ) {
        out += "  $label"
        ehtPuncturingSummaryLines(he, operation).forEach { out += "    $it" }
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
            if (cap.featuresCount > 0) out += wrappedListLines("eht features", cap.featuresList, "<none>").map { "    $it" }
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
            "triggered_txop_mode1".takeIf { mac.triggeredTxopSharingMode1 },
            "triggered_txop_mode2".takeIf { mac.triggeredTxopSharingMode2 },
            "restricted_twt".takeIf { mac.restrictedTwt },
            "scs_traffic_description".takeIf { mac.scsTrafficDescription },
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
        addAll(wrappedListLines("oper flags", value.flagsList, "<none>"))
        if (value.disabledSubchannelBitmapPresent || value.disabledSubchannelBitmap != 0) {
            add("oper disabled=0x${empty(value.disabledSubchannelBitmapHex, value.disabledSubchannelBitmap.toString(16))} punctured=${joined(value.disabledSubchannelIndicesList.map { it.toString() })}")
        }
    }

    private fun ehtPuncturingSummaryLines(he: WifiHeCapabilities?, operation: WifiEhtOperation?): List<String> {
        return listOf(
            "he_preamble_puncturing_rx=${hePreamblePuncturingRx(he)}",
            "he_punctured_sounding=${hePuncturedSounding(he)}",
            "eht_disabled_subchannel_bitmap=${ehtDisabledSubchannelBitmap(operation)}",
        )
    }

    private fun hePreamblePuncturingRx(he: WifiHeCapabilities?): String {
        if (he == null || !he.hasPhy()) return "<unknown>"
        return joined(he.phy.preamblePuncturingRxList, "<none>")
    }

    private fun hePuncturedSounding(he: WifiHeCapabilities?): String {
        if (he == null || !he.hasMac()) return "<unknown>"
        return he.mac.puncturedSounding.toString()
    }

    private fun ehtDisabledSubchannelBitmap(operation: WifiEhtOperation?): String {
        if (operation == null) return "<unknown>"
        if (!operation.disabledSubchannelBitmapPresent && operation.disabledSubchannelBitmap == 0) return "absent"
        val bitmap = operation.disabledSubchannelBitmapHex.ifBlank {
            operation.disabledSubchannelBitmap.toString(16).padStart(4, '0')
        }
        return "0x$bitmap punctured=${joined(operation.disabledSubchannelIndicesList.map { it.toString() })}"
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
        section(out, "EHT Scan Links")
        groups.forEach { group ->
            group.results.forEach { result ->
                renderScanLinkBlock(out, group, result, current)
                renderAffiliatedLinkBlocks(out, group, result, current)
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
        out += "  type=ap ap_mld=${group.displayMld} link=${scanLinkID(result)} bssid=${empty(result.bssid, "<unknown>")}"
        out += "  band=${empty(result.band, wifiBandFromFrequency(result.frequencyMhz))} ch=${wifiChannelFromFrequency(result.frequencyMhz)} freq=${result.frequencyMhz}MHz width=${empty(formatWifiChannelWidth(result.channelWidth), "<unknown>")}${scanEhtOperationSuffix(result)} rssi=${result.rssiDbm}dBm"
        out += "  ${wifiMloInformationElementChecklist(result)}"
        out += "  ${wifiMloScanSdkFlags(result)}"
    }

    private fun renderAffiliatedLinkBlocks(
        out: MutableList<String>,
        group: MloGroup,
        result: WifiScanResult,
        current: WifiConnection?,
    ) {
        if (result.affiliatedMloLinksCount == 0) return
        out += "  affiliated_links"
        result.affiliatedMloLinksList.forEach { link ->
            renderAffiliatedLinkBlock(out, group, result, link, current)
        }
    }

    private fun renderAffiliatedLinkBlock(
        out: MutableList<String>,
        group: MloGroup,
        result: WifiScanResult,
        link: MloLinkInfo,
        current: WifiConnection?,
    ) {
        out += "    [${blockMarker(linkMark(group, link, current))}] type=aff link=${link.linkId} ap_mac=${empty(link.apMacAddress, "<unknown>")}"
        out += "      band=${empty(link.band, "<unknown>")} ch=${link.channel} state=${empty(link.state, "<unknown>")} rssi=${link.rssiDbm}dBm tx=${link.txLinkSpeedMbps} rx=${link.rxLinkSpeedMbps} max_tx=${link.maxSupportedTxLinkSpeedMbps} max_rx=${link.maxSupportedRxLinkSpeedMbps} parent_bssid=${empty(result.bssid, "<unknown>")}"
    }

    private fun blockTitle(out: MutableList<String>, mark: String, label: String) {
        out += "[${blockMarker(mark)}] $label"
    }

    private fun blockMarker(mark: String): String {
        return mark.ifBlank { " " }
    }

    private fun blockGap(out: MutableList<String>) {
        if (out.isNotEmpty() && out.last().isNotBlank() && out.last() != "EHT Scan Links") {
            out += ""
        }
    }

    private fun briefCell(value: String): String {
        return when (value.trim()) {
            "", "<unknown>", "<none>" -> "?"
            else -> value
        }
    }

    private fun briefMarker(mark: String): String {
        return "[${blockMarker(mark)}]"
    }

    private fun briefNodeLabel(index: Int, total: Int, mark: String, linkId: String): String {
        val branch = if (index == total - 1) "`--" else "|--"
        return "$branch ${briefMarker(mark)} ${if (linkId.isBlank()) "?" else linkId}"
    }

    private fun briefLinkID(link: MloLinkInfo): String {
        return if (link.linkId >= 0) link.linkId.toString() else "?"
    }

    private fun briefLinkId(result: WifiScanResult): Int? {
        if (result.apMloLinkId >= 0) return result.apMloLinkId
        return mloCurrentLinkIdFromElements(result.informationElementsList)
    }

    private fun briefMarkRank(mark: String): Int {
        return when (mark.trim()) {
            "*" -> 3
            "+" -> 2
            "-" -> 1
            else -> 0
        }
    }

    private fun briefBand(value: String): String {
        return when (value.trim().lowercase()) {
            "2.4ghz", "2ghz", "2.4g", "2g" -> "2G"
            "5ghz", "5g" -> "5G"
            "6ghz", "6g" -> "6G"
            "60ghz", "60g" -> "60G"
            else -> briefCell(value)
        }
    }

    private fun briefSecurity(value: String): String {
        return when (value.trim().lowercase()) {
            "", "<unknown>", "<none>", "-", "?" -> "?"
            "wpa3_sae", "sae" -> "sae"
            "wpa2_psk", "psk" -> "psk"
            "owe" -> "owe"
            "open" -> "open"
            else -> when {
                value.contains("SAE", ignoreCase = true) -> "sae"
                value.contains("PSK", ignoreCase = true) -> "psk"
                value.contains("OWE", ignoreCase = true) -> "owe"
                else -> briefCell(value.lowercase())
            }
        }
    }

    private fun briefStandard(value: String): String {
        val trimmed = value.trim().lowercase()
        if (trimmed.isBlank() || trimmed == "<unknown>" || trimmed == "-" || trimmed == "?" || trimmed == "unknown") {
            return "?"
        }
        return if (trimmed.startsWith("802.11")) trimmed.removePrefix("802.11").ifBlank { "?" } else trimmed
    }

    private fun briefColor(value: String): String {
        val trimmed = value.trim()
        return when {
            trimmed.isBlank() || trimmed == "<unknown>" || trimmed == "<none>" || trimmed == "-" || trimmed == "?" -> "?"
            trimmed.endsWith("(part)") -> trimmed.removeSuffix("(part)") + "p"
            trimmed.endsWith("(off)") -> trimmed.removeSuffix("(off)") + "off"
            else -> trimmed
        }
    }

    private fun briefEht(result: WifiScanResult?): String {
        if (result == null) return "-"
        val width = briefCell(scanEhtOperationWidth(result)).removeSuffix("MHz")
        var puncturing = briefCell(scanEhtOperationPuncturing(result))
        if (puncturing == "none") puncturing = "-"
        return "$width/$puncturing"
    }

    private fun briefLinkRows(group: MloGroup, current: WifiConnection?): List<BriefLinkRow> {
        val rows = mutableListOf<BriefLinkRow>()
        group.results.forEach { result ->
            upsertBriefLinkRow(rows, briefScanRow(group, result, current))
            result.affiliatedMloLinksList.forEach { link ->
                upsertBriefLinkRow(rows, briefAffiliatedRow(group, link, current))
            }
        }
        return rows.sortedWith(
            compareByDescending<BriefLinkRow> { briefMarkRank(it.mark) }
                .thenByDescending { it.sortLink != null }
                .thenBy { it.sortLink ?: Int.MAX_VALUE }
                .thenByDescending { it.hasScan }
                .thenBy { it.addr.lowercase() },
        )
    }

    private fun upsertBriefLinkRow(rows: MutableList<BriefLinkRow>, candidate: BriefLinkRow) {
        val index = rows.indexOfFirst { left ->
            (left.sortLink != null && candidate.sortLink != null && left.sortLink == candidate.sortLink) ||
                (left.addr.isNotBlank() && candidate.addr.isNotBlank() && left.addr.equals(candidate.addr, ignoreCase = true))
        }
        if (index < 0) {
            rows += candidate
            return
        }
        rows[index] = mergeBriefLinkRows(rows[index], candidate)
    }

    private fun mergeBriefLinkRows(current: BriefLinkRow, candidate: BriefLinkRow): BriefLinkRow {
        var preferred = current
        var secondary = candidate
        if (candidate.hasScan && !current.hasScan) {
            preferred = candidate
            secondary = current
        }
        return preferred.copy(
            mark = if (briefMarkRank(secondary.mark) > briefMarkRank(preferred.mark)) secondary.mark else preferred.mark,
            linkId = if (preferred.linkId == "?" && secondary.linkId != "?") secondary.linkId else preferred.linkId,
            band = if (preferred.band == "?" && secondary.band != "?") secondary.band else preferred.band,
            rssi = if (preferred.rssi == "?" && secondary.rssi != "?") secondary.rssi else preferred.rssi,
            security = if ((preferred.security.isBlank() || preferred.security == "-") && secondary.security.isNotBlank()) secondary.security else preferred.security,
            standard = if ((preferred.standard.isBlank() || preferred.standard == "-") && secondary.standard.isNotBlank()) secondary.standard else preferred.standard,
            color = if ((preferred.color.isBlank() || preferred.color == "-") && secondary.color.isNotBlank()) secondary.color else preferred.color,
            eht = if ((preferred.eht.isBlank() || preferred.eht == "-") && secondary.eht.isNotBlank()) secondary.eht else preferred.eht,
            addr = if ((preferred.addr.isBlank() || preferred.addr == "?") && secondary.addr.isNotBlank()) secondary.addr else preferred.addr,
            sortLink = preferred.sortLink ?: secondary.sortLink,
            hasScan = preferred.hasScan || secondary.hasScan,
        )
    }

    private fun briefScanRow(group: MloGroup, result: WifiScanResult, current: WifiConnection?): BriefLinkRow {
        return BriefLinkRow(
            mark = resultMark(group, result, current),
            linkId = briefCell(scanLinkID(result)),
            band = briefBand(empty(result.band, wifiBandFromFrequency(result.frequencyMhz))),
            rssi = result.rssiDbm.toString(),
            security = briefSecurity(security(result)),
            standard = briefStandard(result.wifiStandard),
            color = briefColor(heOperationBssColorValue(if (result.hasHeOperation()) result.heOperation else null)),
            eht = briefEht(result),
            addr = briefCell(result.bssid),
            sortLink = briefLinkId(result),
            hasScan = true,
        )
    }

    private fun briefAffiliatedRow(group: MloGroup, link: MloLinkInfo, current: WifiConnection?): BriefLinkRow {
        return BriefLinkRow(
            mark = linkMark(group, link, current),
            linkId = briefLinkID(link),
            band = briefBand(link.band),
            rssi = link.rssiDbm.toString(),
            security = "-",
            standard = "-",
            color = "-",
            eht = "-",
            addr = briefCell(link.apMacAddress),
            sortLink = link.linkId.takeIf { it >= 0 },
            hasScan = false,
        )
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
            warnings += "eht_scan_results=0"
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

    private fun WifiConnection.hasHeDetails(): Boolean =
        hasHeCapabilities() || hasHeOperation() || hasHeUoraParameterSet() || hasHeMuEdcaParameterSet()

    private fun WifiScanResult.hasHeDetails(): Boolean =
        hasHeCapabilities() || hasHeOperation() || hasHeUoraParameterSet() || hasHeMuEdcaParameterSet()

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
        if (current == null) return if (result.hasMloScanMetadata()) "-" else ""
        return when {
            bssidEquals(result.bssid, current.bssid) -> "*"
            group.results.any { sameMld(current, it) } -> "+"
            result.hasMloScanMetadata() -> "-"
            else -> ""
        }
    }

    private fun linkMark(group: MloGroup, link: MloLinkInfo, current: WifiConnection?): String {
        if (current == null) return "-"
        return when {
            bssidEquals(link.apMacAddress, current.bssid) -> "*"
            group.results.any { sameMld(current, it) } -> "+"
            else -> "-"
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

    private fun groupBssColors(group: MloGroup): String {
        return joined(group.results.map { result ->
            heOperationBssColorValue(if (result.hasHeOperation()) result.heOperation else null)
        }, "-")
    }

    private fun heOperationBssColorValue(operation: WifiHeOperation?): String {
        if (operation == null) return ""
        return when {
            operation.bssColorDisabled -> "${operation.bssColor}(off)"
            operation.flagsList.contains("partial_bss_color") -> "${operation.bssColor}(part)"
            else -> operation.bssColor.toString()
        }
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

    private fun WifiConnection.hasEhtPuncturingContext(): Boolean =
        hasHeCapabilities() || hasEhtOperation()

    private fun WifiScanResult.hasEhtPuncturingContext(): Boolean =
        hasHeCapabilities() || hasEhtOperation()

    private fun WifiConnection.hasBssColoringContext(): Boolean =
        hasHeOperation() || hasHeSpatialReuseParameterSet()

    private fun WifiScanResult.hasBssColoringContext(): Boolean =
        hasHeOperation() || hasHeSpatialReuseParameterSet()

    private fun bssColoringSummaryLines(
        operation: WifiHeOperation?,
        spatialReuse: WifiHeSpatialReuseParameterSet?,
    ): List<String> = buildList {
        operation?.let { oper ->
            add("he_operation bss_color=${oper.bssColor} disabled=${oper.bssColorDisabled} partial=${oper.flagsList.contains("partial_bss_color")}")
            if (oper.warningsCount > 0) add("he_operation_warnings ${oper.warningsList.joinToString(",")}")
        }
        spatialReuse?.let { sr ->
            add("spatial_reuse control=0x${sr.srControl.toString(16)} flags=${joined(sr.flagsList)}")
            if (sr.nonSrgObssPdMaxOffset != 0) add("non_srg_obss_pd_max_offset=${sr.nonSrgObssPdMaxOffset}")
            if (sr.srgObssPdMinOffset != 0 || sr.srgObssPdMaxOffset != 0) {
                add("srg_obss_pd=${sr.srgObssPdMinOffset}/${sr.srgObssPdMaxOffset}")
            }
            if (sr.srgBssColorBitmapHex.isNotBlank()) add("srg_bss_color_bitmap=0x${sr.srgBssColorBitmapHex}")
            if (sr.srgPartialBssidBitmapHex.isNotBlank()) add("srg_partial_bssid_bitmap=0x${sr.srgPartialBssidBitmapHex}")
            if (sr.truncated) add("spatial_reuse_truncated=true")
            if (sr.warningsCount > 0) add("spatial_reuse_warnings ${sr.warningsList.joinToString(",")}")
        }
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

    private fun wifiMloRelevantStrings(values: Iterable<String>): List<String> {
        return values.filter { wifiMloRelevantText(it) }.distinct()
    }

    private fun wifiMloRelevantText(value: String): Boolean {
        val lower = value.lowercase()
        return listOf("mlo", "mld", "multi-link", "tid_to_link", "tid-to-link", "802.11be", "wifi_7", "wifi7", "wi-fi 7")
            .any { lower.contains(it) }
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

    private data class BriefLinkRow(
        val mark: String,
        val linkId: String,
        val band: String,
        val rssi: String,
        val security: String,
        val standard: String,
        val color: String,
        val eht: String,
        val addr: String,
        val sortLink: Int? = null,
        val hasScan: Boolean = false,
    )

    private data class MloFilter(val ssid: String = "", val bssid: String = "") {
        val active: Boolean = ssid.isNotBlank() || bssid.isNotBlank()
        val label: String = when {
            ssid.isNotBlank() -> "ssid=$ssid"
            bssid.isNotBlank() -> "bssid=$bssid"
            else -> ""
        }

        fun scanResults(results: List<WifiScanResult>): List<WifiScanResult> {
            if (!active) return results
            return results.filter { matches(it) }
        }

        fun connection(conn: WifiConnection?): WifiConnection? {
            if (conn == null || !active || matches(conn)) return conn
            return null
        }

        private fun matches(result: WifiScanResult): Boolean {
            if (ssid.isNotBlank()) return result.ssid == ssid
            if (bssid.isNotBlank()) {
                return result.bssid.equals(bssid, ignoreCase = true) ||
                    result.affiliatedMloLinksList.any { it.apMacAddress.equals(bssid, ignoreCase = true) }
            }
            return true
        }

        private fun matches(conn: WifiConnection): Boolean {
            if (ssid.isNotBlank()) return conn.ssid == ssid
            if (bssid.isNotBlank()) {
                return conn.bssid.equals(bssid, ignoreCase = true) ||
                    conn.associatedMloLinksList.any { it.apMacAddress.equals(bssid, ignoreCase = true) } ||
                    conn.affiliatedMloLinksList.any { it.apMacAddress.equals(bssid, ignoreCase = true) }
            }
            return true
        }
    }

    private data class TableColumn(val header: String, val maxWidth: Int = Int.MAX_VALUE)
}
