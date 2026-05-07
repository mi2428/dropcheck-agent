package io.dropcheck.agent

import android.os.Build
import io.dropcheck.agent.grpc.MloLinkInfo
import io.dropcheck.agent.grpc.WifiConnection
import io.dropcheck.agent.grpc.WifiScan
import io.dropcheck.agent.grpc.WifiScanResult
import io.dropcheck.agent.grpc.WifiStatus

internal data class AgentWifiMloContext(
    val scanSource: String = "cached",
    val sdkInt: Int = Build.VERSION.SDK_INT,
    val wifi7Supported: Boolean? = null,
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
        renderScanSummary(out, scan, candidates, context)
        renderNearbyMlo(out, groups, current)
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
            "ap_mld" to empty(conn.apMldMacAddress, "<none>"),
            "ap_link_id" to connectionLinkID(conn),
            "affiliated" to conn.affiliatedMloLinksCount.toString(),
            "associated" to conn.associatedMloLinksCount.toString(),
        )
        renderMloLinks(out, "Associated MLO Links", conn.associatedMloLinksList)
        renderMloLinks(out, "Affiliated MLO Links", conn.affiliatedMloLinksList)
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
                TableColumn("MARK", 4),
                TableColumn("SSID", 24),
                TableColumn("BANDS", 18),
                TableColumn("RSSI", 4),
                TableColumn("SECURITY", 12),
                TableColumn("STANDARD", 8),
            ),
            groups.map { group ->
                listOf(
                    groupMark(group, current),
                    joined(group.results.map { empty(it.ssid, "<hidden>") }),
                    joined(group.bands, "unknown"),
                    group.bestRssi.toString(),
                    joined(group.security, "-"),
                    joined(group.standards, "-"),
                )
            },
        )
        renderScanLinks(out, groups, current)
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
        out += "  band=${empty(result.band, wifiBandFromFrequency(result.frequencyMhz))} ch=${wifiChannelFromFrequency(result.frequencyMhz)} freq=${result.frequencyMhz}MHz width=${empty(formatWifiChannelWidth(result.channelWidth), "<unknown>")} rssi=${result.rssiDbm}dBm"
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
            "connected_ap_mld" to empty(current.apMldMacAddress, "<none>"),
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

        val apMldSeen = beResults.count { it.apMldMacAddress.isNotBlank() }
        val linkIdSeen = beResults.count { it.apMloLinkId >= 0 }
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
        val mld = result.apMldMacAddress.trim().lowercase()
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
            affiliatedMloLinksCount > 0
    }

    private fun connectionHasMlo(conn: WifiConnection): Boolean {
        return conn.apMldMacAddress.isNotBlank() ||
            conn.affiliatedMloLinksCount > 0 ||
            conn.associatedMloLinksCount > 0 ||
            (conn.wifiStandard.equals("802.11be", ignoreCase = true) && conn.apMloLinkId >= 0)
    }

    private fun sameMld(conn: WifiConnection, result: WifiScanResult): Boolean {
        val currentMld = conn.apMldMacAddress.trim()
        if (currentMld.isNotBlank() && currentMld.equals(result.apMldMacAddress, ignoreCase = true)) return true
        return bssidEquals(conn.bssid, result.bssid)
    }

    private fun bssidEquals(left: String, right: String): Boolean {
        return left.isNotBlank() && right.isNotBlank() && left.equals(right, ignoreCase = true)
    }

    private fun groupMark(group: MloGroup, current: WifiConnection?): String {
        if (current == null) return ""
        return when {
            group.results.any { bssidEquals(it.bssid, current.bssid) } -> "*"
            group.results.any { sameMld(current, it) } -> "+"
            else -> ""
        }
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
        return ids
    }

    private fun associatedLinkIds(conn: WifiConnection): Set<Int> {
        val ids = mutableSetOf<Int>()
        if (connectionHasMlo(conn) && conn.apMloLinkId >= 0) ids += conn.apMloLinkId
        conn.associatedMloLinksList.filter { it.linkId >= 0 }.forEach { ids += it.linkId }
        return ids
    }

    private fun connectionLinkID(conn: WifiConnection): String {
        return if (connectionHasMlo(conn) && conn.apMloLinkId >= 0) conn.apMloLinkId.toString() else "<none>"
    }

    private fun scanLinkID(result: WifiScanResult): String {
        val explicitMlo = result.hasMloScanMetadata()
        return when {
            (explicitMlo || result.wifiStandard.equals("802.11be", ignoreCase = true)) && result.apMloLinkId >= 0 -> result.apMloLinkId.toString()
            result.wifiStandard.equals("802.11be", ignoreCase = true) -> "<unknown>"
            else -> "<none>"
        }
    }

    private fun security(result: WifiScanResult): String {
        return result.securityTypesList.joinToString(",").ifBlank { result.capabilities }
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

    private class MloGroup(val results: List<WifiScanResult>) {
        val displayMld: String = results.firstNotNullOfOrNull { it.apMldMacAddress.takeIf(String::isNotBlank) } ?: "<unknown>"
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
