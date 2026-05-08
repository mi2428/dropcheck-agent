package io.dropcheck.agent

private const val CLOCK_WIDGET_UNKNOWN_SSID = "<unknown ssid>"
private const val CLOCK_WIDGET_PLACEHOLDER_BSSID = "02:00:00:00:00:00"
private const val CLOCK_WIDGET_ZERO_BSSID = "00:00:00:00:00:00"

internal fun clockWidgetWifiInfoIsUsable(
    networkId: Int,
    ssid: String?,
    bssid: String?,
    supplicantState: String?,
): Boolean {
    if (clockWidgetKnownSsid(ssid) || clockWidgetKnownBssid(bssid)) return true

    val connectedState = supplicantState.equals("COMPLETED", ignoreCase = true)
    return networkId >= 0 && connectedState
}

internal fun clockWidgetWifiNetworkIsDisplayable(
    localNetwork: Boolean,
    wifiInfoUsable: Boolean,
): Boolean {
    return wifiInfoUsable && !localNetwork
}

internal fun clockWidgetKnownSsid(value: String?): Boolean {
    val normalized = value.orEmpty().trim().removeSurrounding("\"")
    return normalized.isNotBlank() && !normalized.equals(CLOCK_WIDGET_UNKNOWN_SSID, ignoreCase = true)
}

internal fun clockWidgetKnownBssid(value: String?): Boolean {
    val normalized = value.orEmpty().trim()
    return normalized.isNotBlank() &&
        !normalized.equals(CLOCK_WIDGET_PLACEHOLDER_BSSID, ignoreCase = true) &&
        !normalized.equals(CLOCK_WIDGET_ZERO_BSSID, ignoreCase = true) &&
        !normalized.equals("unknown", ignoreCase = true)
}
