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
        if (context.brief) {
            renderCompactScanResults(out, results, context)
            return
        }
        val layout = layoutFor(context)
        val rows = displayRows(results, context.mloOnly, layout)
        if (rows.isEmpty()) {
            out += "  no results"
            return
        }
        table(out, columnsFor(layout), rows.map { it.values })
    }

    private fun renderCompactScanResults(out: MutableList<String>, results: List<WifiScanResult>, context: AgentWifiScanContext) {
        val groups = resultGroups(results, context.mloOnly)
        if (groups.isEmpty()) {
            out += "  no results"
            return
        }
        if (context.mloOnly) {
            renderCompactMloGroups(out, groups)
        } else {
            renderCompactBriefGroups(out, groups)
        }
    }

    private fun renderCompactBriefGroups(out: MutableList<String>, groups: List<ResultGroup>) {
        groups.forEachIndexed { groupIndex, group ->
            if (groupIndex > 0) out += ""
            out += "  ${group.ssid}"
            table(out, briefScanColumns(), group.results.map(::briefScanTableRow))
        }
    }

    private fun renderCompactMloGroups(out: MutableList<String>, groups: List<ResultGroup>) {
        groups.forEachIndexed { groupIndex, group ->
            if (groupIndex > 0) out += ""
            out += "  ${group.ssid}"
            group.results.firstNotNullOfOrNull { scanMldMac(it).takeIf(String::isNotBlank) }?.let { out += "    mld  $it" }
            table(out, briefMloColumns(), compactMloRows(group.results).map { it.values })
        }
    }

    private fun compactMloRows(results: List<WifiScanResult>): List<CompactMloRow> {
        val rows = mutableListOf<SortableCompactMloRow>()
        results.forEach { result ->
            rows += SortableCompactMloRow(
                row = compactMloScanRow(result),
                bandOrder = bandOrder(result.band, result.frequencyMhz),
                isAffiliated = false,
                rssi = result.rssiDbm,
                bssid = empty(result.bssid, "unknown").lowercase(),
            )
        }
        results.forEach { result ->
            sortedAffiliatedLinks(result.affiliatedMloLinksList).forEach { link ->
                if (affiliatedLinkMatchesResult(result, link)) return@forEach
                rows += SortableCompactMloRow(
                    row = compactMloAffiliatedRow(link),
                    bandOrder = bandOrder(link.band, 0),
                    isAffiliated = true,
                    rssi = link.rssiDbm,
                    bssid = empty(link.apMacAddress, "unknown").lowercase(),
                )
            }
        }
        return rows.sortedWith(
            compareBy<SortableCompactMloRow> { it.isAffiliated }
                .thenBy { it.bandOrder }
                .thenComparator { left, right ->
                    if (!left.isAffiliated && left.rssi != right.rssi) {
                        return@thenComparator right.rssi.compareTo(left.rssi)
                    }
                    if (left.bssid != right.bssid) return@thenComparator left.bssid.compareTo(right.bssid)
                    right.rssi.compareTo(left.rssi)
                },
        ).map { it.row }
    }

    private fun compactMloScanRow(result: WifiScanResult): CompactMloRow {
        return CompactMloRow(
            values = listOf(
                "s",
                briefLinkIdValue(scanLinkId(result)),
                result.rssiDbm.toString(),
                compactBand(result.band, result.frequencyMhz),
                briefChannelCell(result.frequencyMhz),
                compactStandard(result.wifiStandard),
                compactSecurity(security(result)),
                briefFlagsCell(result),
                empty(result.bssid, "unknown"),
            ),
        )
    }

    private fun compactMloAffiliatedRow(link: MloLinkInfo): CompactMloRow {
        return CompactMloRow(
            values = listOf(
                "a",
                briefLinkIdValue(mloLinkId(link.linkId)),
                link.rssiDbm.takeIf { it != 0 }?.toString() ?: "?",
                compactBand(link.band, 0),
                briefChannelNumber(link.channel),
                "-",
                "-",
                briefStateCell(link.state),
                empty(link.apMacAddress, "unknown"),
            ),
        )
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
        return securityFeatureFlags(details).joinToString(",").ifBlank { "-" }
    }

    private fun securityFeatureFlags(details: WifiSecurityDetails?): List<String> {
        if (details == null) return emptyList()
        return buildList {
            if (details.gcmp256) add("gcmp256")
            if (details.saeGdh) add("sae-gdh")
            if (details.ftSaeGdh) add("ft-sae-gdh")
            if (details.rsnxeCapabilitiesList.contains("sae_h2e")) add("h2e")
            if (details.rsnxeCapabilitiesList.contains("ssid_protection")) add("ssid-prot")
            if (details.beaconProtection) add("beacon-prot")
        }
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

    private fun security(result: WifiScanResult): String {
        return result.securityTypesList.filter(String::isNotBlank).joinToString(",").ifBlank { empty(result.capabilities, "-") }
    }

    private fun compactBand(band: String, frequencyMhz: Int): String {
        return when (empty(band, bandNameForFrequency(frequencyMhz)).trim().lowercase()) {
            "2.4ghz", "2ghz", "2.4g", "2g" -> "2G"
            "5ghz", "5g" -> "5G"
            "6ghz", "6g" -> "6G"
            "60ghz", "60g" -> "60G"
            else -> "?"
        }
    }

    private fun compactStandard(value: String): String {
        return normalizedStandard(value).ifBlank { "?" }
    }

    private fun compactSecurity(value: String): String {
        return when (value.trim().lowercase()) {
            "", "<unknown>", "<none>", "-", "?" -> "?"
            "wpa3_sae", "sae" -> "sae"
            "wpa2_psk", "psk" -> "psk"
            "owe" -> "owe"
            "open" -> "opn"
            else -> when {
                value.contains("SAE", ignoreCase = true) -> "sae"
                value.contains("PSK", ignoreCase = true) -> "psk"
                value.contains("OWE", ignoreCase = true) -> "owe"
                value.contains("OPEN", ignoreCase = true) -> "opn"
                else -> value.lowercase()
            }
        }
    }

    private fun briefScanColumns(): List<TableColumn> {
        return listOf(
            TableColumn("RSSI", 4),
            TableColumn("B", 2),
            TableColumn("CH", 3),
            TableColumn("ST", 3),
            TableColumn("SEC", 3),
            TableColumn("MLO", 6),
            TableColumn("FL", 10),
            TableColumn("MAC", 17),
        )
    }

    private fun briefMloColumns(): List<TableColumn> {
        return listOf(
            TableColumn("K", 1),
            TableColumn("L", 2),
            TableColumn("RSSI", 4),
            TableColumn("B", 2),
            TableColumn("CH", 3),
            TableColumn("ST", 3),
            TableColumn("SEC", 3),
            TableColumn("FL", 10),
            TableColumn("MAC", 17),
        )
    }

    private fun briefScanTableRow(result: WifiScanResult): List<String> {
        return listOf(
            result.rssiDbm.toString(),
            compactBand(result.band, result.frequencyMhz),
            briefChannelCell(result.frequencyMhz),
            compactStandard(result.wifiStandard),
            compactSecurity(security(result)),
            briefMloCell(result),
            briefFlagsCell(result),
            empty(result.bssid, "unknown"),
        )
    }

    private fun briefMloCell(result: WifiScanResult): String {
        if (!hasMloScanMetadata(result) && !result.wifiStandard.equals("802.11be", ignoreCase = true)) {
            return "-"
        }
        val link = "l${briefLinkIdValue(scanLinkId(result))}"
        val affiliated = result.affiliatedMloLinksCount.takeIf { it > 0 }?.let { "+$it" }.orEmpty()
        return link + affiliated
    }

    private fun briefFlagsCell(result: WifiScanResult): String {
        return connectionCapabilityFlags(result)
            .map(::briefFlagToken)
            .joinToString(",")
            .ifBlank { "-" }
    }

    private fun briefFlagToken(value: String): String {
        return when (value) {
            "interworking" -> "iw"
            "roaming_consortium" -> "rc"
            else -> value
        }
    }

    private fun briefLinkIdValue(value: String): String {
        return when (value.trim()) {
            "", "<unknown>", "<none>", "?" -> "?"
            else -> value
        }
    }

    private fun briefChannelCell(frequencyMhz: Int): String {
        return wifiChannelFromFrequency(frequencyMhz).takeIf { it != "unknown" } ?: "?"
    }

    private fun briefChannelNumber(channel: Int): String {
        return if (channel > 0) channel.toString() else "?"
    }

    private fun briefStateCell(value: String): String {
        return when (value.trim().lowercase()) {
            "", "<unknown>", "<none>", "-", "?" -> "-"
            "unassociated" -> "ua"
            else -> value.lowercase()
        }
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

    private data class CompactMloRow(
        val values: List<String>,
    )

    private data class SortableCompactMloRow(
        val row: CompactMloRow,
        val bandOrder: Int,
        val isAffiliated: Boolean,
        val rssi: Int,
        val bssid: String,
    )

    private data class TableColumn(val header: String, val maxWidth: Int = Int.MAX_VALUE)
}
