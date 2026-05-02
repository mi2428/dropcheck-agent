package io.dropcheck.agent

import io.dropcheck.agent.grpc.ConnectWifi
import io.dropcheck.agent.grpc.WifiBand
import java.util.Locale

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

    /** Minimal cached scan data needed to infer how a passphrase network should be configured. */
    data class ScanSecurityCandidate(
        val ssid: String,
        val bssid: String,
        val capabilities: String,
        val frequencyMhz: Int,
        val levelDbm: Int,
    )

    /** Minimal connected-network view used to avoid unnecessary privileged reconfiguration. */
    data class CurrentConnectionRef(
        val networkId: Int,
        val ssid: String,
        val bssid: String,
        val frequencyMhz: Int,
        val securityType: String,
    )

    /** WifiConfiguration expects quoted SSID/passphrase strings for legacy addNetwork APIs. */
    fun quoteWifi(value: String): String {
        return if (value.startsWith("\"") && value.endsWith("\"")) value else "\"$value\""
    }

    /**
     * Resolves an omitted connect security mode from cached scan capabilities.
     *
     * The controller can leave security as UNSPECIFIED when a user did not
     * choose WPA2/WPA3. Android's legacy addNetwork API still needs one
     * concrete security parameter, so the agent uses the strongest matching
     * scan result it can observe and falls back to WPA2 for old or hidden APs.
     */
    fun resolveConnectSecurity(
        requested: ConnectWifi.Security,
        candidates: List<ScanSecurityCandidate>,
        ssid: String,
        bssid: String,
        band: WifiBand,
    ): ConnectWifi.Security {
        if (requested != ConnectWifi.Security.SECURITY_UNSPECIFIED) return requested
        return candidates
            .filter { it.ssid == ssid }
            .filter { bssid.isBlank() || it.bssid.equals(bssid, ignoreCase = true) }
            .filter { frequencyMatchesWifiBand(it.frequencyMhz, band) }
            .sortedByDescending { it.levelDbm }
            .mapNotNull { securityFromCapabilities(it.capabilities) }
            .firstOrNull()
            ?: ConnectWifi.Security.SECURITY_WPA2_PSK
    }

    /**
     * Parses Android ScanResult capability text into the connect security enum.
     *
     * Transition-mode APs often advertise both PSK and SAE. Auto mode prefers
     * SAE because a modern Dropcheck test device can then verify it is actually
     * using WPA3; users can still request `transition` or `wpa2` explicitly.
     */
    fun securityFromCapabilities(capabilities: String): ConnectWifi.Security? {
        val upper = capabilities.uppercase(Locale.US)
        val hasSae = upper.contains("SAE")
        val hasPsk = upper.contains("PSK")
        return when {
            hasSae -> ConnectWifi.Security.SECURITY_WPA3_SAE
            hasPsk -> ConnectWifi.Security.SECURITY_WPA2_PSK
            else -> null
        }
    }

    /** Reports whether the current Wi-Fi connection already satisfies a connect command. */
    fun currentConnectionSatisfiesConnect(
        current: CurrentConnectionRef?,
        ssid: String,
        bssid: String,
        security: ConnectWifi.Security,
        band: WifiBand,
    ): Boolean {
        if (current == null || current.networkId < 0) return false
        if (current.ssid != ssid) return false
        if (bssid.isNotBlank() && !current.bssid.equals(bssid, ignoreCase = true)) return false
        if (!frequencyMatchesWifiBand(current.frequencyMhz, band)) return false
        val expectedSecurityTypes = when (security) {
            ConnectWifi.Security.SECURITY_WPA2_PSK -> setOf("psk")
            ConnectWifi.Security.SECURITY_WPA3_SAE -> setOf("sae")
            ConnectWifi.Security.SECURITY_WPA2_WPA3_TRANSITION -> setOf("psk", "sae")
            ConnectWifi.Security.SECURITY_UNSPECIFIED -> emptySet()
            ConnectWifi.Security.UNRECOGNIZED -> return false
        }
        return expectedSecurityTypes.isEmpty() || current.securityType in expectedSecurityTypes
    }

    /** Reports whether a forget target names the currently connected network. */
    fun currentConnectionMatchesForgetTarget(target: String, current: CurrentConnectionRef?): Boolean {
        if (target.isBlank() || current == null || current.networkId < 0) return false
        val numeric = target.toIntOrNull()
        return numeric == current.networkId ||
            current.ssid == target ||
            current.bssid.equals(target, ignoreCase = true)
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
