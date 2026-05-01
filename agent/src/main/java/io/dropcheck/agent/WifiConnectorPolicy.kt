package io.dropcheck.agent

/**
 * Pure policy for Wi-Fi configuration bookkeeping.
 *
 * Android's WifiManager calls stay in [WifiConnector]; selection and success
 * rules live here so regressions can be caught by plain JVM tests.
 */
internal object WifiConnectorPolicy {
    /** Minimal view of configured networks needed by the forget selection rule. */
    data class ConfiguredNetworkRef(
        val networkId: Int,
        val ssid: String,
    )

    /** WifiConfiguration expects quoted SSID/passphrase strings for legacy addNetwork APIs. */
    fun quoteWifi(value: String): String {
        return if (value.startsWith("\"") && value.endsWith("\"")) value else "\"$value\""
    }

    /**
     * Selects configured network IDs by exact SSID match or numeric network ID.
     *
     * If a numeric target is not listed locally, return it anyway: some devices
     * hide configured networks while still accepting removeNetwork(id).
     */
    fun forgetNetworkIds(target: String, configs: List<ConfiguredNetworkRef>): List<Int> {
        if (target.isBlank()) return emptyList()
        val numeric = target.toIntOrNull()
        val matches = configs.filter { config ->
            numeric?.let { config.networkId == it } == true || config.ssid.trim('"') == target
        }
        return when {
            matches.isNotEmpty() -> matches.map { it.networkId }
            numeric != null -> listOf(numeric)
            else -> emptyList()
        }
    }

    /** A forget operation needs at least one successful remove and no framework exceptions. */
    fun forgetSucceeded(fields: List<Pair<String, String>>, errors: List<String>): Boolean {
        return errors.isEmpty() && fields.any {
            it.first.startsWith("remove_") && it.second == "true"
        }
    }
}
