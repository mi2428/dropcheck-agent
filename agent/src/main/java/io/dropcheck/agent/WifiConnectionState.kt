package io.dropcheck.agent

import io.dropcheck.agent.grpc.WifiConnection
import io.dropcheck.agent.grpc.WifiStatus

private const val UNKNOWN_SSID = "<unknown ssid>"
private const val PLACEHOLDER_BSSID = "02:00:00:00:00:00"
private const val ZERO_BSSID = "00:00:00:00:00:00"

internal fun activeWifiConnection(status: WifiStatus): WifiConnection? {
    if (!status.hasConnection()) return null
    return status.connection.takeIf { isActiveWifiConnection(it) }
}

internal fun isActiveWifiConnection(conn: WifiConnection): Boolean {
    if (isKnownWifiSsid(conn.ssid) || isKnownWifiBssid(conn.bssid)) return true
    if (conn.ipv4Address.isNotBlank()) return true
    if (hasMloIdentity(conn)) return true

    val connectedState =
        conn.supplicantState.equals("COMPLETED", ignoreCase = true) ||
            conn.detailedState.equals("CONNECTED", ignoreCase = true)
    return conn.networkId >= 0 && connectedState
}

internal fun normalizedWifiSsid(value: String?): String {
    return value.orEmpty().trim().trim('"')
}

internal fun isKnownWifiSsid(value: String): Boolean {
    val normalized = normalizedWifiSsid(value)
    return normalized.isNotBlank() && !normalized.equals(UNKNOWN_SSID, ignoreCase = true)
}

internal fun isKnownWifiBssid(value: String): Boolean {
    val normalized = value.trim()
    return normalized.isNotBlank() &&
        !normalized.equals(PLACEHOLDER_BSSID, ignoreCase = true) &&
        !normalized.equals(ZERO_BSSID, ignoreCase = true)
}

private fun hasMloIdentity(conn: WifiConnection): Boolean {
    return conn.apMldMacAddress.isNotBlank() ||
        conn.affiliatedMloLinksCount > 0 ||
        conn.associatedMloLinksCount > 0 ||
        (conn.wifiStandard.equals("802.11be", ignoreCase = true) && conn.apMloLinkId >= 0)
}
