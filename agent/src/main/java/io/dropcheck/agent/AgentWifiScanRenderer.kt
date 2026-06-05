package io.dropcheck.agent

import io.dropcheck.agent.grpc.MloLinkInfo
import io.dropcheck.agent.grpc.WifiInformationElement
import io.dropcheck.agent.grpc.WifiScan
import io.dropcheck.agent.grpc.WifiScanResult
import io.dropcheck.agent.grpc.WifiSecurityDetails

internal data class AgentWifiScanContext(
    val brief: Boolean = false,
    val mloOnly: Boolean = false,
)

internal object AgentWifiScanRenderer {
    fun render(scan: WifiScan, context: AgentWifiScanContext = AgentWifiScanContext()): List<String> {
        val out = mutableListOf<String>()
        section(out, "Wi-Fi Scan")
        kv(out, scanSummaryRows(scan, context))
        if (out.isNotEmpty() && out.last().isNotBlank()) out += ""
        renderScanResults(out, scan.resultsList, context)
        renderErrors(out, scan.errorsList)
        return out
    }

    private fun scanSummaryRows(scan: WifiScan, context: AgentWifiScanContext): List<Pair<String, String>> {
        val fields = scan.fieldsList.associate { it.key to it.value }
        if (context.mloOnly) {
            val rows = displayRows(scan.resultsList, mloOnly = true, layout = layoutFor(context))
            return buildList {
                add("requested_band" to fields["requested_band"].orEmpty())
                add("mlo_results" to scan.resultsList.count(::isMloCapableResult).toString())
                add("affiliated_rows" to rows.count { it.isAffiliated }.toString())
                add("display_rows" to rows.size.toString())
                add("scan_results" to fields["scan_result_count"].orEmpty().ifBlank { scan.resultsCount.toString() })
                add("scan_total" to fields["scan_result_total_count"].orEmpty().ifBlank { scan.resultsCount.toString() })
                add("errors" to scan.errorsCount.toString())
                appendScanFieldRows(fields)
            }
        }
        return buildList {
            add("requested_band" to fields["requested_band"].orEmpty())
            add("results" to fields["scan_result_count"].orEmpty().ifBlank { scan.resultsCount.toString() })
            add("total" to fields["scan_result_total_count"].orEmpty().ifBlank { scan.resultsCount.toString() })
            add("errors" to scan.errorsCount.toString())
            appendScanFieldRows(fields)
        }
    }

    private fun MutableList<Pair<String, String>>.appendScanFieldRows(fields: Map<String, String>) {
        listOf(
            "wifi_enabled",
            "wifi_state",
            "scan_always_available",
            "scan_throttle_enabled",
            "fresh_scan_receiver_registered",
            "fresh_scan_start_scan",
            "fresh_scan_broadcast_received",
            "fresh_scan_results_updated",
            "fresh_scan_wait_completed",
            "fresh_scan_elapsed_ms",
        ).forEach { key ->
            fields[key]?.takeIf(String::isNotBlank)?.let { add(key to it) }
        }
    }

    private fun renderScanResults(out: MutableList<String>, results: List<WifiScanResult>, context: AgentWifiScanContext) {
        val layout = layoutFor(context)
        val rows = displayRows(results, context.mloOnly, layout)
        if (rows.isEmpty()) {
            out += "  no results"
            return
        }
        table(out, columnsFor(layout), rows.map { it.values })
    }

    private fun renderErrors(out: MutableList<String>, errors: List<String>) {
        if (errors.isEmpty()) return
        section(out, "Errors")
        errors.forEach { out += "  $it" }
    }

    private fun layoutFor(context: AgentWifiScanContext): ScanLayout {
        return ScanLayout(
            includeStandard = !context.mloOnly,
            includeSecurityFeatures = context.brief || context.mloOnly,
            includeApLink = !context.mloOnly,
            includeAffiliated = !context.mloOnly,
            blankLinkDetails = context.mloOnly,
        )
    }

    private fun columnsFor(layout: ScanLayout): List<TableColumn> {
        val columns = mutableListOf(
            TableColumn("SSID"),
            TableColumn("BSSID"),
            TableColumn("RSSI"),
            TableColumn("BAND"),
            TableColumn("FREQ"),
        )
        if (layout.includeStandard) columns += TableColumn("STANDARD")
        columns += TableColumn("SECURITY")
        if (layout.includeSecurityFeatures) columns += TableColumn("SEC_FEATURES")
        columns += TableColumn("FLAGS")
        columns += TableColumn("AP_MLD")
        if (layout.includeApLink) columns += TableColumn("AP_LINK")
        if (layout.includeAffiliated) columns += TableColumn("AFFILIATED")
        return columns
    }

    private fun displayRows(results: List<WifiScanResult>, mloOnly: Boolean, layout: ScanLayout): List<DisplayRow> {
        val groups = resultGroups(results, mloOnly)
        return buildList {
            groups.forEach { group ->
                val rows = if (mloOnly) mloDisplayRows(group.results, layout) else group.results.map { resultRow(it, layout) }
                if (mloOnly && rows.size > 1) {
                    rows.drop(1).forEach { it.values[0] = "" }
                }
                addAll(rows)
            }
        }
    }

    private fun mloDisplayRows(results: List<WifiScanResult>, layout: ScanLayout): List<DisplayRow> {
        val rows = mutableListOf<SortableRow>()
        results.forEach { result ->
            rows += SortableRow(
                row = resultRow(result, layout),
                bandOrder = bandOrder(result.band, result.frequencyMhz),
                isLink = false,
                rssi = result.rssiDbm,
                bssid = empty(result.bssid, "unknown").lowercase(),
            )
        }
        results.forEach { result ->
            sortedAffiliatedLinks(result.affiliatedMloLinksList).forEach { link ->
                if (affiliatedLinkMatchesResult(result, link)) return@forEach
                rows += SortableRow(
                    row = affiliatedLinkRow(result, link, layout),
                    bandOrder = bandOrder(link.band, 0),
                    isLink = true,
                    rssi = link.rssiDbm,
                    bssid = empty(link.apMacAddress, "unknown").lowercase(),
                )
            }
        }
        return rows.sortedWith(
            compareBy<SortableRow> { it.isLink }
                .thenBy { it.bandOrder }
                .thenComparator { left, right ->
                    if (!left.isLink && left.rssi != right.rssi) {
                        return@thenComparator right.rssi.compareTo(left.rssi)
                    }
                    if (left.bssid != right.bssid) return@thenComparator left.bssid.compareTo(right.bssid)
                    right.rssi.compareTo(left.rssi)
                },
        ).map { it.row }
    }

    private fun resultGroups(results: List<WifiScanResult>, mloOnly: Boolean): List<ResultGroup> {
        val grouped = linkedMapOf<String, MutableList<WifiScanResult>>()
        results.forEach { result ->
            if (mloOnly && !isMloCapableResult(result)) return@forEach
            val ssid = displaySsid(result)
            grouped.getOrPut(ssid) { mutableListOf() } += result
        }
        return grouped.map { (ssid, items) ->
            ResultGroup(
                ssid = ssid,
                bestRssi = items.maxOfOrNull { it.rssiDbm } ?: Int.MIN_VALUE,
                results = items.sortedWith { left, right -> compareResults(left, right) },
            )
        }.sortedWith(
            compareByDescending<ResultGroup> { it.bestRssi }
                .thenBy { it.ssid.lowercase() }
                .thenBy { it.ssid },
        )
    }

    private fun compareResults(left: WifiScanResult, right: WifiScanResult): Int {
        if (left.rssiDbm != right.rssiDbm) return right.rssiDbm.compareTo(left.rssiDbm)
        val leftBssid = empty(left.bssid, "unknown").lowercase()
        val rightBssid = empty(right.bssid, "unknown").lowercase()
        if (leftBssid != rightBssid) return leftBssid.compareTo(rightBssid)
        if (left.frequencyMhz != right.frequencyMhz) return left.frequencyMhz.compareTo(right.frequencyMhz)
        val leftBand = empty(left.band, bandNameForFrequency(left.frequencyMhz)).lowercase()
        val rightBand = empty(right.band, bandNameForFrequency(right.frequencyMhz)).lowercase()
        return leftBand.compareTo(rightBand)
    }

    private fun resultRow(result: WifiScanResult, layout: ScanLayout): DisplayRow {
        val values = mutableListOf<String>()
        values += displaySsid(result)
        values += empty(result.bssid, "unknown")
        values += result.rssiDbm.toString()
        values += empty(result.band, bandNameForFrequency(result.frequencyMhz))
        values += result.frequencyMhz.toString()
        if (layout.includeStandard) values += empty(result.wifiStandard, "-")
        values += result.securityTypesList.filter(String::isNotBlank).joinToString(",").ifBlank { empty(result.capabilities, "-") }
        if (layout.includeSecurityFeatures) values += securityFeatureCell(result.securityDetails.takeIf { result.hasSecurityDetails() })
        values += connectionCapabilityFlags(result).joinToString(",").ifBlank { "-" }
        values += empty(scanMldMac(result), "<none>")
        if (layout.includeApLink) values += scanLinkId(result)
        if (layout.includeAffiliated) values += result.affiliatedMloLinksCount.toString()
        return DisplayRow(values = values, isAffiliated = false)
    }

    private fun affiliatedLinkRow(result: WifiScanResult, link: MloLinkInfo, layout: ScanLayout): DisplayRow {
        val values = mutableListOf<String>()
        val flags = buildList {
            add("affiliated_link")
            link.state.takeIf(String::isNotBlank)?.let { add(it) }
        }
        values += displaySsid(result)
        values += empty(link.apMacAddress, "unknown")
        values += ""
        values += empty(link.band, "unknown")
        values += if (layout.blankLinkDetails) "" else "-"
        if (layout.includeStandard) values += ""
        values += if (layout.blankLinkDetails) "" else "-"
        if (layout.includeSecurityFeatures) values += if (layout.blankLinkDetails) "" else "-"
        values += flags.joinToString(",")
        values += empty(scanMldMac(result), "<none>")
        if (layout.includeApLink) values += mloLinkId(link.linkId)
        if (layout.includeAffiliated) values += "link"
        return DisplayRow(values = values, isAffiliated = true)
    }

    private fun displaySsid(result: WifiScanResult): String = empty(result.ssid, "<hidden>")

    private fun sortedAffiliatedLinks(links: List<MloLinkInfo>): List<MloLinkInfo> {
        return links.sortedWith(
            compareByDescending<MloLinkInfo> { it.rssiDbm }
                .thenBy { empty(it.apMacAddress, "unknown").lowercase() }
                .thenBy { it.linkId },
        )
    }

    private fun bandOrder(band: String, frequencyMhz: Int): Int {
        return when (empty(band, bandNameForFrequency(frequencyMhz)).trim().lowercase()) {
            "6ghz" -> 0
            "5ghz" -> 1
            "2.4ghz" -> 2
            "60ghz" -> 3
            else -> 4
        }
    }

    private fun affiliatedLinkMatchesResult(result: WifiScanResult, link: MloLinkInfo): Boolean {
        return result.bssid.equals(link.apMacAddress, ignoreCase = true) && link.linkId == result.apMloLinkId
    }

    private fun isMloCapableResult(result: WifiScanResult): Boolean {
        return hasMloScanMetadata(result) && allowedMloStandard(result.wifiStandard)
    }

    private fun hasMloScanMetadata(result: WifiScanResult): Boolean {
        return result.apMldMacAddress.isNotBlank() ||
            result.affiliatedMloLinksCount > 0 ||
            parseEhtMultiLinkElements(result.informationElementsList).isNotEmpty()
    }

    private fun allowedMloStandard(value: String): Boolean {
        return when (normalizedStandard(value)) {
            "", "unknown", "be" -> true
            else -> false
        }
    }

    private fun normalizedStandard(value: String): String {
        var normalized = value.trim().lowercase()
        when (normalized) {
            "", "<unknown>", "-", "?" -> return ""
            "unknown" -> return "unknown"
        }
        normalized = normalized.removePrefix("wifi_standard_")
            .removePrefix("standard_")
            .removePrefix("ieee80211")
            .removePrefix("ieee802.11")
            .removePrefix("802.11")
            .removePrefix("11")
            .removePrefix("wifi ")
            .removePrefix("wi-fi ")
            .replace(" ", "")
        return when (normalized) {
            "", "<unknown>", "-", "?" -> ""
            "unknown" -> "unknown"
            "7", "eht" -> "be"
            else -> normalized
        }
    }

    private fun securityFeatureCell(details: WifiSecurityDetails?): String {
        if (details == null) return "-"
        val flags = mutableListOf<String>()
        if (details.gcmp256) flags += "gcmp256"
        if (details.saeGdh) flags += "sae-gdh"
        if (details.ftSaeGdh) flags += "ft-sae-gdh"
        if (details.rsnxeCapabilitiesList.contains("sae_h2e")) flags += "h2e"
        if (details.rsnxeCapabilitiesList.contains("ssid_protection")) flags += "ssid-prot"
        if (details.beaconProtection) flags += "beacon-prot"
        return flags.joinToString(",").ifBlank { "-" }
    }

    private fun connectionCapabilityFlags(result: WifiScanResult): List<String> {
        return result.informationElementsList.mapNotNull(::connectionCapabilityName).distinct().sorted()
    }

    private fun connectionCapabilityName(element: WifiInformationElement): String? {
        return when (element.id) {
            54 -> "11r"
            55 -> "ft"
            70 -> "11k"
            107 -> "interworking"
            111 -> "roaming_consortium"
            127 -> if (informationElementBit(element, 19)) "11v" else null
            201 -> "rnr"
            255 -> if (element.idExt == 107) "mlo" else null
            else -> null
        }
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

    private fun scanMldMac(result: WifiScanResult): String {
        return firstNonBlank(
            result.apMldMacAddress,
            parseEhtMultiLinkElements(result.informationElementsList)
                .firstNotNullOfOrNull { it.commonInfo?.mldMacAddress?.takeIf(String::isNotBlank) }
                .orEmpty(),
        )
    }

    private fun scanLinkId(result: WifiScanResult): String {
        val explicitMlo = hasMloScanMetadata(result)
        val elementLinkId = parseEhtMultiLinkElements(result.informationElementsList)
            .firstNotNullOfOrNull { it.commonInfo?.linkId }
        return when {
            (explicitMlo || result.wifiStandard.equals("802.11be", ignoreCase = true)) && result.apMloLinkId >= 0 ->
                result.apMloLinkId.toString()
            elementLinkId != null -> elementLinkId.toString()
            explicitMlo || result.wifiStandard.equals("802.11be", ignoreCase = true) -> "<unknown>"
            else -> "<none>"
        }
    }

    private fun mloLinkId(id: Int): String = if (id < 0) "<none>" else id.toString()

    private fun section(out: MutableList<String>, title: String) {
        if (out.isNotEmpty() && out.last().isNotBlank()) out += ""
        out += title
    }

    private fun kv(out: MutableList<String>, rows: List<Pair<String, String>>) {
        val filtered = rows.filter { it.first.isNotBlank() && it.second.isNotBlank() }
        val width = filtered.maxOfOrNull { it.first.length } ?: 0
        filtered.forEach { (key, value) ->
            out += "  ${key.padEnd(width)}  $value"
        }
    }

    private fun table(out: MutableList<String>, columns: List<TableColumn>, rows: List<List<String>>) {
        val preparedRows = rows.map { row ->
            columns.indices.map { index -> fitCell(row.getOrElse(index) { "" }, columns[index].maxWidth) }
        }
        val preparedHeaders = columns.map { fitCell(it.header, it.maxWidth) }
        val widths = columns.indices.map { index ->
            (listOf(preparedHeaders[index]) + preparedRows.map { it[index] }).maxOf { displayWidth(it) }
        }
        out += preparedHeaders.mapIndexed { index, value -> padDisplayEnd(value, widths[index]) }.joinToString("  ").trimEnd()
        preparedRows.forEach { row ->
            out += columns.indices.map { index -> padDisplayEnd(row[index], widths[index]) }.joinToString("  ").trimEnd()
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

    private fun empty(value: String, fallback: String): String = value.ifBlank { fallback }

    private fun firstNonBlank(vararg values: String): String = values.firstOrNull { it.isNotBlank() }.orEmpty()

    private data class ScanLayout(
        val includeStandard: Boolean,
        val includeSecurityFeatures: Boolean,
        val includeApLink: Boolean,
        val includeAffiliated: Boolean,
        val blankLinkDetails: Boolean,
    )

    private data class ResultGroup(
        val ssid: String,
        val bestRssi: Int,
        val results: List<WifiScanResult>,
    )

    private data class DisplayRow(
        val values: MutableList<String>,
        val isAffiliated: Boolean,
    )

    private data class SortableRow(
        val row: DisplayRow,
        val bandOrder: Int,
        val isLink: Boolean,
        val rssi: Int,
        val bssid: String,
    )

    private data class TableColumn(val header: String, val maxWidth: Int = Int.MAX_VALUE)
}
