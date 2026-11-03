package io.dropcheck.agent

import io.dropcheck.agent.grpc.WifiScan
import io.dropcheck.agent.grpc.WifiScanResult

internal data class AgentWifiScanContext(val brief: Boolean = false, val mloOnly: Boolean = false, val requestedFresh: Boolean? = null, val detail: Boolean = false)

internal object AgentWifiScanRenderer {
    /** Full safe records when no native View measurement is supplied. */
    fun render(scan: WifiScan, context: AgentWifiScanContext = AgentWifiScanContext()): List<String> =
        AgentPresentationBlock.create(presentation(scan, context), 0).fullText.split('\n')

    fun presentation(scan: WifiScan, context: AgentWifiScanContext = AgentWifiScanContext()): List<AgentBlockPart> {
        val fields = scan.fieldsList.associate { it.key to it.value }
        val results = scan.resultsList.filter { !context.mloOnly || hasMloMetadata(it) }
            .groupBy { it.ssid }
            .entries.sortedWith(compareByDescending<Map.Entry<String, List<WifiScanResult>>> { entry -> entry.value.maxOf { it.rssiDbm } }.thenBy { it.key })
            .flatMap { entry -> entry.value.sortedWith(compareByDescending<WifiScanResult> { it.rssiDbm }.thenBy { it.bssid }) }
        val rows = results.map { result ->
            listOf(
                result.ssid.ifEmpty { "? (SSID presence unavailable)" }, result.bssid.ifEmpty { "?" }, result.rssiDbm.toString(),
                band(result.band.ifEmpty { bandNameForFrequency(result.frequencyMhz) }), channel(result.frequencyMhz),
                result.channelWidth.removeSuffix("MHz").ifEmpty { "?" }, phy(result.wifiStandard), security(result.securityTypesList),
            ) + if (context.brief) listOf(ageSeconds(result)) else emptyList()
        }
        return buildList {
            add(AgentBlockPart.Text("Wi-Fi Scan"))
            add(AgentBlockPart.Field("Requested", context.requestedFresh?.let { if (it) "fresh" else "cached" } ?: "? (request metadata unavailable)"))
            add(AgentBlockPart.Field("Data source", fields["scan_source"] ?: "? (source metadata unavailable)"))
            add(AgentBlockPart.Field("Updated", fields["fresh_scan_results_updated"] ?: "?"))
            add(AgentBlockPart.Field("Band", fields["requested_band"] ?: "?"))
            add(AgentBlockPart.Field("Update elapsed", "${fields["fresh_scan_elapsed_ms"] ?: "?"}ms"))
            add(AgentBlockPart.Field("APs", "acquired=${rows.size} total=${fields["scan_result_total_count"] ?: scan.resultsCount}"))
            if (context.mloOnly) add(AgentBlockPart.Text("Filter: mlo; shown counts filtered APs"))
            add(AgentBlockPart.Scan(rows, scanColumns + if (context.brief) listOf(AgentBlockPart.Column("AGE(s)")) else emptyList()))
            add(AgentBlockPart.Text("BW=MHz; RSSI=dBm; band G=GHz; PHY=802.11"))
            if (context.brief) add(AgentBlockPart.Field("AGE(s)", "seconds at collection; ?=timestamp unavailable; wpa3-eap=eap_wpa3_enterprise"))
            // AP numbers refer to row order, including rows retained in Inspect/copy.
            if (!context.brief || context.mloOnly || context.detail) results.forEachIndexed { index, ap ->
                add(AgentBlockPart.Text("AP ${index + 1}"))
                if (!context.brief) add(AgentBlockPart.Field("Observation age at collection", observationAge(ap)))
                if (!context.brief || context.mloOnly) {
                    add(AgentBlockPart.Field("MLD", ap.apMldMacAddress.ifEmpty { parseEhtMultiLinkElements(ap.informationElementsList).firstNotNullOfOrNull { it.commonInfo?.mldMacAddress }.orEmpty().ifEmpty { "? (metadata unavailable)" } }))
                    if (!context.detail) add(AgentBlockPart.Field("Affiliated", ap.affiliatedMloLinksList.joinToString("; ") { link -> "${link.linkId} ${link.state} ${link.band} AP=${link.apMacAddress} STA=${link.staMacAddress}" }.ifEmpty { "? (metadata unavailable)" }))
                }
                if (!context.brief) {
                    add(AgentBlockPart.Field("Frequency MHz", ap.frequencyMhz.toString()))
                    add(AgentBlockPart.Field("Centers MHz", "${ap.centerFreq0Mhz}/${ap.centerFreq1Mhz}"))
                    add(AgentBlockPart.Field("Security details", if (ap.hasSecurityDetails()) AgentWifiStatusRenderer.securitySummary(ap.securityDetails) else "?"))
                }
                if (context.detail) addAll(AgentWifiStatusRenderer.scanEvidence(ap))
            }
            if (!context.brief) scan.fieldsList.filterNot { it.key in listOf("scan_source", "fresh_scan_results_updated", "requested_band", "fresh_scan_elapsed_ms", "scan_result_total_count", "scan_result_count") }
                .forEach { add(AgentBlockPart.Field(it.key, it.value)) }
            if (context.brief) add(AgentBlockPart.Text("Full: show wifi scan (new passive observation)"))
            scan.errorsList.forEach { add(AgentBlockPart.Text("Error: $it")) }
        }
    }

    private fun hasMloMetadata(result: WifiScanResult): Boolean =
        (AgentObservationPresentation.available(result.observationFieldsList, "ap_mlo_link_id") && result.apMloLinkId >= 0) || result.apMldMacAddress.isNotEmpty() || result.affiliatedMloLinksCount > 0 || parseEhtMultiLinkElements(result.informationElementsList).isNotEmpty()

    internal fun observationAge(ap: WifiScanResult): String = if (ap.hasObservationAgeMs()) "${java.lang.Long.toUnsignedString(ap.observationAgeMs)}ms" else "? (observation timestamp unavailable)"

    internal fun ageSeconds(ap: WifiScanResult): String {
        if (!ap.hasObservationAgeMs()) return "?"
        val ms = ap.observationAgeMs.toULong()
        val fraction = (ms % 1000u).toString().padStart(3, '0').trimEnd('0')
        return (ms / 1000u).toString() + if (fraction.isEmpty()) "" else ".$fraction"
    }

    internal fun security(values: List<String>): String = values.joinToString("+") { token ->
        when (token) {
            "wpa3_sae" -> "sae"
            "eap_wpa3_enterprise" -> "wpa3-eap"
            "wpa2_psk" -> "psk"
            "wpa2_wpa3_personal", "wpa2_psk+wpa3_sae" -> "psk+sae"
            else -> token
        }
    }.ifEmpty { "?" }

    internal fun band(value: String): String = when (value.lowercase()) {
        "2.4ghz" -> "2.4G"
        "5ghz" -> "5G"
        "6ghz" -> "6G"
        "60ghz" -> "60G"
        "", "unknown" -> "?"
        else -> value
    }

    internal fun phy(value: String): String = when (value) {
        "802.11a", "802.11b", "802.11g", "802.11n", "802.11ac", "802.11ax", "802.11be" -> value.removePrefix("802.11")
        "" -> "?"
        else -> value
    }

    private fun channel(freq: Int): String = when {
        freq == 2484 -> "14"
        freq in 2412..2472 -> ((freq - 2407) / 5).toString()
        freq in 5000..5895 -> ((freq - 5000) / 5).toString()
        freq in 5955..7115 -> ((freq - 5950) / 5).toString()
        else -> "?"
    }
}
