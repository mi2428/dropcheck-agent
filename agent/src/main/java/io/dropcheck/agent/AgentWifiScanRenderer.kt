package io.dropcheck.agent

import io.dropcheck.agent.grpc.DiagnosticField
import io.dropcheck.agent.grpc.MloLinkInfo
import io.dropcheck.agent.grpc.WifiScan
import io.dropcheck.agent.grpc.WifiScanDetail
import io.dropcheck.agent.grpc.WifiScanResult

/** Text renderer used by the on-device shell for `show wifi scan`. */
internal object AgentWifiScanRenderer {
    fun render(scan: WifiScan): List<String> {
        val out = mutableListOf<String>()
        section(out, "Wi-Fi Scan")
        kv(out,
            "results" to scan.resultsCount.toString(),
            "errors" to scan.errorsCount.toString(),
        )
        blank(out)
        renderDiagnosticFields(out, scan.fieldsList)
        blank(out)
        renderScanResults(out, scan.resultsList)
        renderErrors(out, scan.errorsList)
        return out
    }

    fun renderDetail(detail: WifiScanDetail): List<String> {
        val out = mutableListOf<String>()
        section(out, "Wi-Fi Scan Detail")
        kv(out,
            "target" to detail.target,
            "results" to detail.resultsCount.toString(),
        )
        blank(out)
        renderDiagnosticFields(out, detail.fieldsList)
        blank(out)
        renderScanResults(out, detail.resultsList)
        renderErrors(out, detail.errorsList)
        return out
    }

    private fun renderDiagnosticFields(out: MutableList<String>, fields: List<DiagnosticField>) {
        if (fields.isEmpty()) return
        table(out,
            listOf("FIELD", "VALUE"),
            fields.map { listOf(it.key, it.value) },
        )
    }

    private fun renderScanResults(out: MutableList<String>, results: List<WifiScanResult>) {
        if (results.isEmpty()) {
            out += "  no results"
            return
        }
        table(out,
            listOf("SSID", "BSSID", "RSSI", "BAND", "FREQ", "STANDARD", "SECURITY", "AP_MLD", "AP_LINK", "AFFILIATED"),
            results.map { result ->
                listOf(
                    empty(result.ssid, "<hidden>"),
                    empty(result.bssid, "unknown"),
                    result.rssiDbm.toString(),
                    empty(result.band, wifiBandFromFrequency(result.frequencyMhz)),
                    result.frequencyMhz.toString(),
                    empty(result.wifiStandard, "-"),
                    empty(result.securityTypesList.joinToString(","), empty(result.capabilities, "-")),
                    empty(result.apMldMacAddress, "<none>"),
                    scanMLOLinkID(result),
                    result.affiliatedMloLinksCount.toString(),
                )
            },
        )
        renderScanMLOLinks(out, results)
    }

    private fun renderScanMLOLinks(out: MutableList<String>, results: List<WifiScanResult>) {
        val rows = results.flatMap { result ->
            result.affiliatedMloLinksList.map { link ->
                scanMLOLinkRow(result, link)
            }
        }
        if (rows.isEmpty()) return
        section(out, "Scan Affiliated MLO Links")
        table(out,
            listOf("SSID", "BSSID", "AP_MLD", "AP_LINK", "ID", "STATE", "BAND", "CHANNEL", "RSSI", "TX", "RX", "AP_MAC", "STA_MAC"),
            rows,
        )
    }

    private fun scanMLOLinkRow(result: WifiScanResult, link: MloLinkInfo): List<String> {
        return listOf(
            empty(result.ssid, "<hidden>"),
            empty(result.bssid, "unknown"),
            empty(result.apMldMacAddress, "<none>"),
            scanMLOLinkID(result),
            link.linkId.toString(),
            empty(link.state, "unknown"),
            empty(link.band, "unknown"),
            link.channel.toString(),
            link.rssiDbm.toString(),
            link.txLinkSpeedMbps.toString(),
            link.rxLinkSpeedMbps.toString(),
            empty(link.apMacAddress, "unknown"),
            empty(link.staMacAddress, "unknown"),
        )
    }

    private fun renderErrors(out: MutableList<String>, errors: List<String>) {
        if (errors.isEmpty()) return
        section(out, "Errors")
        errors.forEach { out += "  $it" }
    }

    private fun section(out: MutableList<String>, title: String) {
        if (out.isNotEmpty() && out.last().isNotBlank()) out += ""
        out += title
    }

    private fun blank(out: MutableList<String>) {
        if (out.isNotEmpty() && out.last().isNotBlank()) out += ""
    }

    private fun kv(out: MutableList<String>, vararg rows: Pair<String, String>) {
        val filtered = rows.filter { it.first.isNotBlank() && it.second.isNotBlank() }
        val width = filtered.maxOfOrNull { it.first.length } ?: 0
        filtered.forEach { (key, value) ->
            out += "  ${key.padEnd(width)}  $value"
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

    private fun scanMLOLinkID(result: WifiScanResult): String {
        if (result.apMloLinkId == 0 && result.apMldMacAddress.isBlank() && result.affiliatedMloLinksCount == 0) {
            return "<none>"
        }
        return mloLinkID(result.apMloLinkId)
    }

    private fun mloLinkID(id: Int): String = if (id < 0) "<none>" else id.toString()

    private fun empty(value: String, fallback: String): String = value.ifBlank { fallback }

    private fun wifiBandFromFrequency(freq: Int): String = when (freq) {
        in 2400 until 2500 -> "2.4ghz"
        in 4900 until 5900 -> "5ghz"
        in 5925 until 7125 -> "6ghz"
        in 57000 until 71000 -> "60ghz"
        else -> "unknown"
    }
}
