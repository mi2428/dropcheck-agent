package io.dropcheck.agent

import io.dropcheck.agent.grpc.WifiScanConnections
import io.dropcheck.agent.grpc.WifiScanResult
import java.util.Locale

internal fun scanBssid(value: String): String? = value.takeIf {
    it.matches(Regex("[0-9a-fA-F]{2}(:[0-9a-fA-F]{2}){5}")) && isKnownWifiBssid(it) && it.take(2).toInt(16) and 1 == 0
}?.lowercase(Locale.ROOT)

internal fun validScanConnections(value: WifiScanConnections): Boolean = value.observedAtUnixMs > 0 &&
    value.bssidsList.all { scanBssid(it) != null } &&
    if (value.complete) value.errorsCount == 0 else value.errorsCount > 0

internal fun scanEhtEvidence(ap: WifiScanResult): String = when {
    ap.wifiStandard.equals("802.11be", ignoreCase = true) -> "observed (802.11be)"
    ap.hasEhtCapabilities() && !ap.ehtCapabilities.truncated && ap.ehtCapabilities.hasMac() && ap.ehtCapabilities.hasPhy() -> "observed (decoded EHT Capabilities)"
    ap.hasEhtOperation() && !ap.ehtOperation.truncated && ap.ehtOperation.basicMcsNssSetHex.isNotEmpty() -> "observed (decoded EHT Operation)"
    else -> ""
}

internal fun scanApMark(ap: WifiScanResult, connections: WifiScanConnections): String = when {
    validScanConnections(connections) && scanBssid(ap.bssid)?.let { key -> connections.bssidsList.any { scanBssid(it) == key } } == true -> "*"
    scanEhtEvidence(ap).isNotEmpty() -> "+"
    else -> " "
}

internal fun scanConnectionsSummary(value: WifiScanConnections): String {
    if (value.observedAtUnixMs == 0L) return "? (connection observation not captured)"
    if (!validScanConnections(value)) return "? (inconsistent connection observation)"
    val at = java.time.Instant.ofEpochMilli(value.observedAtUnixMs)
    val state = if (value.bssidsCount > 0) value.bssidsList.joinToString(", ") else if (value.complete) "disconnected" else "unknown"
    return "$state at $at; not atomic with scan" + if (value.complete) "" else "; incomplete: ${value.errorsList.joinToString("; ")}"
}
