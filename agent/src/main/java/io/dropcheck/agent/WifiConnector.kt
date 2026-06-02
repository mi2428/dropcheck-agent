@file:Suppress("DEPRECATION")

package io.dropcheck.agent

import android.annotation.SuppressLint
import android.content.Context
import android.net.wifi.WifiConfiguration
import android.net.wifi.WifiInfo
import android.net.wifi.WifiManager
import android.os.Build
import io.dropcheck.agent.grpc.ConnectWifi
import io.dropcheck.agent.grpc.WifiBand

@Suppress("DEPRECATION")
/**
 * Thin wrapper around privileged WifiManager configuration calls.
 *
 * Anything that can be decided without Android framework state belongs in
 * [WifiConnectorPolicy], leaving this class focused on side effects.
 */
class WifiConnector(
    context: Context,
    private val logger: CommandLogger,
) {
    private val wifi = context.applicationContext.getSystemService(WifiManager::class.java)

    /** Result of staging a Wi-Fi configuration before connection assertion. */
    data class Setup(
        val networkId: Int? = null,
        val previousNetworkId: Int? = null,
        val error: String? = null,
    )

    /** Common shape for one-shot Wi-Fi framework operations. */
    data class Operation(
        val operation: String,
        val ok: Boolean,
        val message: String,
        val fields: List<Pair<String, String>> = emptyList(),
        val errors: List<String> = emptyList(),
    )

    /**
     * Adds/enables a privileged network configuration and requests reconnect.
     *
     * This method does not decide whether the device is connected; callers must
     * observe Android network state after the framework processes the request.
     */
    @SuppressLint("MissingPermission")
    fun connect(command: ConnectWifi): Setup {
        val ssid = command.ssid
        val passphrase = command.passphrase
        if (ssid.isBlank()) return Setup(error = "wifi ssid is required")

        val needsBandPin = command.bssid.isBlank() &&
            command.band != WifiBand.WIFI_BAND_UNSPECIFIED &&
            command.band != WifiBand.WIFI_BAND_ALL
        val securityCandidates = if (command.security == ConnectWifi.Security.SECURITY_UNSPECIFIED || needsBandPin) scanSecurityCandidates() else emptyList()
        val resolvedBssid = WifiConnectorPolicy.resolveConnectBssid(
            requested = command.bssid,
            candidates = securityCandidates,
            ssid = ssid,
            band = command.band,
            security = command.security,
        )
        val resolvedSecurity = WifiConnectorPolicy.resolveConnectSecurity(
            requested = command.security,
            candidates = securityCandidates,
            ssid = ssid,
            bssid = resolvedBssid,
            band = command.band,
        )

        logger.info("wifi configure ssid=$ssid security=${command.security} resolved_security=$resolvedSecurity bssid=${command.bssid.ifBlank { "*" }} resolved_bssid=${resolvedBssid.ifBlank { "*" }} band=${command.band} mac_randomization=${command.macRandomization} passphrase_present=${passphrase.isNotBlank()} security_candidates=${securityCandidates.size}")
        val beforeInfo = wifi.connectionInfo
        logger.debug("wifi manager state enabled=${wifi.isWifiEnabled} state=${wifi.wifiState} connection_network_id=${beforeInfo?.networkId} connection_ssid=${beforeInfo?.ssid} connection_bssid=${beforeInfo?.bssid} rssi=${beforeInfo?.rssi} freq=${beforeInfo?.frequency}")
        val previousNetworkId = beforeInfo?.networkId?.takeIf { it >= 0 }
        val current = currentConnectionRef(beforeInfo)
        if (WifiConnectorPolicy.currentConnectionSatisfiesConnect(
                current = current,
                ssid = ssid,
                bssid = resolvedBssid,
                security = command.security,
                band = command.band,
            )
        ) {
            logger.info("wifi connect already satisfied ssid=$ssid network_id=${current?.networkId} bssid=${current?.bssid} security=${current?.securityType} freq=${current?.frequencyMhz}")
            return Setup(networkId = previousNetworkId, previousNetworkId = previousNetworkId)
        }
        cleanupConflictingBssidProfiles(ssid, resolvedBssid, current)?.let { error ->
            return Setup(error = error)
        }
        val config = WifiConfiguration().apply {
            SSID = WifiConnectorPolicy.quoteWifi(ssid)
            if (resolvedBssid.isNotBlank()) BSSID = resolvedBssid
            if (passphrase.isNotBlank()) preSharedKey = WifiConnectorPolicy.quoteWifi(passphrase)
            when (resolvedSecurity) {
                ConnectWifi.Security.SECURITY_WPA3_SAE -> setSecurityParams(WifiConfiguration.SECURITY_TYPE_SAE)
                ConnectWifi.Security.SECURITY_WPA2_WPA3_TRANSITION -> setSecurityParams(WifiConfiguration.SECURITY_TYPE_PSK)
                ConnectWifi.Security.SECURITY_WPA2_PSK,
                ConnectWifi.Security.SECURITY_UNSPECIFIED -> setSecurityParams(WifiConfiguration.SECURITY_TYPE_PSK)
                ConnectWifi.Security.UNRECOGNIZED -> return Setup(error = "unsupported wifi security: ${command.security}")
            }
        }
        val macRandomization = configureMacRandomization(config, command.macRandomization)
        if (macRandomization.error != null) {
            return Setup(error = macRandomization.error)
        }
        logger.debug("wifi configuration prepared ssid=$ssid bssid=${config.BSSID.orEmpty().ifBlank { "*" }} security=${command.security} resolved_security=$resolvedSecurity band=${command.band} previous_network_id=${previousNetworkId ?: "none"} hidden=${config.hiddenSSID} mac_randomization_requested=${command.macRandomization} mac_randomization_applied=${macRandomization.applied}")

        val add = runCatching { wifi.addNetworkPrivileged(config) }.getOrElse {
            return Setup(error = "wifi addNetworkPrivileged failed: ${errorSummary(it)}")
        }
        logger.info("wifi addNetworkPrivileged status=${add.statusCode} network_id=${add.networkId}")
        if (add.statusCode != WifiManager.AddNetworkResult.STATUS_SUCCESS) {
            return Setup(error = "wifi addNetworkPrivileged failed: status=${add.statusCode}")
        }
        val networkId = add.networkId
        logger.debug("wifi network id selected network_id=$networkId ssid=$ssid")

        val enabled = runCatching { wifi.enableNetwork(networkId, true) }.getOrElse {
            return Setup(error = "wifi enableNetwork failed: ${errorSummary(it)}")
        }
        if (!enabled) {
            return Setup(error = "wifi enableNetwork failed: networkId=$networkId")
        }
        logger.info("wifi enableNetwork ok network_id=$networkId disable_others=true")
        val reconnect = runCatching { wifi.reconnect() }.getOrElse {
            return Setup(error = "wifi reconnect failed: ${errorSummary(it)}")
        }
        logger.info("wifi reconnect requested result=$reconnect")
        val afterInfo = wifi.connectionInfo
        logger.debug("wifi post-reconnect manager_enabled=${wifi.isWifiEnabled} connection_network_id=${afterInfo?.networkId} connection_ssid=${afterInfo?.ssid} connection_bssid=${afterInfo?.bssid} supplicant=${afterInfo?.supplicantState} rssi=${afterInfo?.rssi} freq=${afterInfo?.frequency}")
        return Setup(networkId = networkId, previousNetworkId = previousNetworkId)
    }

    /**
     * Reads the scan cache for auto security selection without making connect
     * depend on fresh scans, which Android may throttle during repeated tests.
     */
    @SuppressLint("MissingPermission")
    private fun scanSecurityCandidates(): List<WifiConnectorPolicy.ScanSecurityCandidate> {
        return runCatching {
            wifi.scanResults.orEmpty().map { result ->
                WifiConnectorPolicy.ScanSecurityCandidate(
                    ssid = result.SSID.orEmpty(),
                    bssid = result.BSSID.orEmpty(),
                    capabilities = result.capabilities.orEmpty(),
                    frequencyMhz = result.frequency,
                    levelDbm = result.level,
                )
            }
        }.onFailure {
            logger.warn("wifi scan cache unavailable for security auto selection error=${errorSummary(it)}")
        }.getOrDefault(emptyList())
    }

    /**
     * Android may merge a BSSID-pinned WifiConfiguration into an existing saved
     * SSID profile. Removing that profile first keeps framework selection from
     * reducing the request to targetBssid=any.
     */
    @SuppressLint("MissingPermission")
    private fun cleanupConflictingBssidProfiles(
        ssid: String,
        bssid: String,
        current: WifiConnectorPolicy.CurrentConnectionRef?,
    ): String? {
        if (bssid.isBlank()) return null
        val configs = runCatching { wifi.configuredNetworks.orEmpty() }.getOrElse {
            logger.warn("wifi configuredNetworks failed before BSSID pin cleanup error=${errorSummary(it)}")
            emptyList()
        }
        val refs = configs.map { config ->
            WifiConnectorPolicy.ConfiguredNetworkRef(
                networkId = config.networkId,
                ssid = config.SSID.orEmpty(),
                bssid = config.BSSID.orEmpty(),
            )
        }
        val networkIds = WifiConnectorPolicy.bssidPinCleanupNetworkIds(
            ssid = ssid,
            bssid = bssid,
            current = current,
            configs = refs,
        )
        if (networkIds.isEmpty()) {
            logger.debug("wifi BSSID pin cleanup skipped ssid=$ssid bssid=$bssid configured_network_count=${configs.size}")
            return null
        }
        logger.info("wifi BSSID pin cleanup ssid=$ssid bssid=$bssid configured_network_count=${configs.size} network_ids=${networkIds.joinToString(",")}")
        val errors = mutableListOf<String>()
        networkIds.forEach { networkId ->
            val removed = runCatching { wifi.removeNetwork(networkId) }
                .onSuccess {
                    logger.debug("wifi BSSID pin cleanup remove network_id=$networkId result=$it")
                    if (!it) {
                        logger.debug("wifi BSSID pin cleanup remove returned false network_id=$networkId; retrying after disable")
                    }
                }
                .onFailure { errors += "remove_$networkId=${errorSummary(it)}" }
                .getOrDefault(false)
            if (removed) return@forEach
            runCatching { wifi.disableNetwork(networkId) }
                .onSuccess { logger.debug("wifi BSSID pin cleanup disable network_id=$networkId result=$it") }
                .onFailure { errors += "disable_$networkId=${errorSummary(it)}" }
            runCatching { wifi.removeNetwork(networkId) }
                .onSuccess {
                    logger.debug("wifi BSSID pin cleanup remove_after_disable network_id=$networkId result=$it")
                    if (!it) errors += "remove_$networkId=false"
                }
                .onFailure { errors += "remove_after_disable_$networkId=${errorSummary(it)}" }
        }
        return errors.takeIf { it.isNotEmpty() }?.joinToString("; ", prefix = "wifi BSSID pin cleanup failed: ")
    }

    /** Requests Wi-Fi disconnect and records the connection that was active beforehand. */
    @SuppressLint("MissingPermission")
    fun disconnect(): Operation {
        val before = wifi.connectionInfo
        val ok = runCatching { wifi.disconnect() }.getOrElse {
            return Operation(
                operation = "disconnect",
                ok = false,
                message = "wifi disconnect failed",
                errors = listOf(errorSummary(it)),
            )
        }
        val settled = waitForDisconnectSettle(1500)
        val after = wifi.connectionInfo
        logger.info("wifi disconnect requested result=$ok settled=$settled previous_network_id=${before?.networkId} previous_ssid=${before?.ssid} previous_bssid=${before?.bssid} current_network_id=${after?.networkId} current_ssid=${after?.ssid} current_bssid=${after?.bssid} current_supplicant=${after?.supplicantState}")
        return Operation(
            operation = "disconnect",
            ok = true,
            message = if (ok) "disconnect requested" else "disconnect request not permitted by framework",
            fields = listOf(
                "framework_result" to ok.toString(),
                "settled" to settled.toString(),
                "previous_network_id" to (before?.networkId?.toString() ?: ""),
                "previous_ssid" to before?.ssid.orEmpty(),
                "previous_bssid" to before?.bssid.orEmpty(),
                "current_network_id" to (after?.networkId?.toString() ?: ""),
                "current_ssid" to after?.ssid.orEmpty(),
                "current_bssid" to after?.bssid.orEmpty(),
                "current_supplicant" to after?.supplicantState?.toString().orEmpty(),
            ),
        )
    }

    /** Requests framework reconnect for the current or most recent Wi-Fi network. */
    @SuppressLint("MissingPermission")
    fun reconnect(): Operation {
        val before = wifi.connectionInfo
        val ok = runCatching { wifi.reconnect() }.getOrElse {
            return Operation(
                operation = "reconnect",
                ok = false,
                message = "wifi reconnect failed",
                errors = listOf(errorSummary(it)),
            )
        }
        logger.info("wifi reconnect requested result=$ok current_network_id=${before?.networkId} current_ssid=${before?.ssid} current_bssid=${before?.bssid}")
        return Operation(
            operation = "reconnect",
            ok = ok,
            message = if (ok) "reconnect requested" else "reconnect rejected",
            fields = listOf(
                "network_id" to (before?.networkId?.toString() ?: ""),
                "ssid" to before?.ssid.orEmpty(),
                "bssid" to before?.bssid.orEmpty(),
            ),
        )
    }

    /**
     * Removes configured networks by SSID or network ID.
     *
     * Device-owner APIs can hide configured networks on some builds, so numeric
     * targets are still attempted even when not visible in configuredNetworks.
     */
    @SuppressLint("MissingPermission")
    fun forget(target: String): Operation {
        if (target.isBlank()) {
            return Operation(operation = "forget", ok = false, message = "wifi forget target is required")
        }
        val configs = runCatching { wifi.configuredNetworks.orEmpty() }.getOrElse {
            logger.warn("wifi configuredNetworks failed error=$it")
            emptyList()
        }
        val current = currentConnectionRef(wifi.connectionInfo)
        val refs = configs.map { config ->
            WifiConnectorPolicy.ConfiguredNetworkRef(
                networkId = config.networkId,
                ssid = config.SSID.orEmpty(),
            )
        }
        val networkIds = WifiConnectorPolicy.forgetNetworkIds(target, refs)
        logger.info("wifi forget requested target=$target configured_network_count=${configs.size} network_ids=${networkIds.joinToString(",")}")
        if (networkIds.isEmpty()) {
            if (WifiConnectorPolicy.currentConnectionMatchesForgetTarget(target, current)) {
                val fields = mutableListOf<Pair<String, String>>(
                    "target" to target,
                    "configured_network_count" to configs.size.toString(),
                    "current_network_id" to current!!.networkId.toString(),
                    "current_ssid" to current.ssid,
                    "current_bssid" to current.bssid,
                )
                val errors = mutableListOf<String>()
                runCatching { wifi.disableNetwork(current.networkId) }
                    .onSuccess { fields += "disable_${current.networkId}" to it.toString() }
                    .onFailure { errors += "disable_${current.networkId}=${errorSummary(it)}" }
                val removed = runCatching { wifi.removeNetwork(current.networkId) }
                    .onSuccess { fields += "remove_${current.networkId}" to it.toString() }
                    .onFailure { errors += "remove_${current.networkId}=${errorSummary(it)}" }
                    .getOrDefault(false)
                return Operation(
                    operation = "forget",
                    ok = errors.isEmpty(),
                    message = if (removed) "wifi network removed" else "wifi forget not permitted by framework",
                    fields = fields,
                    errors = errors,
                )
            }
            return Operation(
                operation = "forget",
                ok = false,
                message = "wifi network not found",
                fields = listOf("target" to target, "configured_network_count" to configs.size.toString()),
            )
        }
        val fields = mutableListOf<Pair<String, String>>(
            "target" to target,
            "configured_network_count" to configs.size.toString(),
            "matched_network_ids" to networkIds.joinToString(","),
        )
        val errors = mutableListOf<String>()
        networkIds.forEach { networkId ->
            runCatching { wifi.disableNetwork(networkId) }
                .onSuccess { fields += "disable_$networkId" to it.toString() }
                .onFailure { errors += "disable_$networkId=${errorSummary(it)}" }
            runCatching { wifi.removeNetwork(networkId) }
                .onSuccess { fields += "remove_$networkId" to it.toString() }
                .onFailure { errors += "remove_$networkId=${errorSummary(it)}" }
        }
        val ok = WifiConnectorPolicy.forgetSucceeded(fields, errors)
        return Operation(
            operation = "forget",
            ok = ok,
            message = if (ok) "wifi network removed" else "wifi network remove failed",
            fields = fields,
            errors = errors,
        )
    }

    private data class MacRandomizationSetup(
        val applied: String,
        val error: String? = null,
    )

    /**
     * Applies Android 13+ MAC randomization controls when requested.
     *
     * Older API levels reject explicit randomization requests instead of silently
     * ignoring them, because controller tests depend on knowing what was applied.
     */
    private fun configureMacRandomization(
        config: WifiConfiguration,
        requested: ConnectWifi.MacRandomization,
    ): MacRandomizationSetup {
        if (Build.VERSION.SDK_INT < 33) {
            return if (requested == ConnectWifi.MacRandomization.MAC_RANDOMIZATION_UNSPECIFIED) {
                MacRandomizationSetup("unavailable")
            } else {
                MacRandomizationSetup(
                    applied = "unavailable",
                    error = "wifi MAC randomization option requires Android 13/API 33+",
                )
            }
        }

        val setting = when (requested) {
            ConnectWifi.MacRandomization.MAC_RANDOMIZATION_UNSPECIFIED -> null
            ConnectWifi.MacRandomization.MAC_RANDOMIZATION_AUTO -> WifiConfiguration.RANDOMIZATION_AUTO
            ConnectWifi.MacRandomization.MAC_RANDOMIZATION_NONE -> WifiConfiguration.RANDOMIZATION_NONE
            ConnectWifi.MacRandomization.MAC_RANDOMIZATION_PERSISTENT -> WifiConfiguration.RANDOMIZATION_PERSISTENT
            ConnectWifi.MacRandomization.MAC_RANDOMIZATION_NON_PERSISTENT -> WifiConfiguration.RANDOMIZATION_NON_PERSISTENT
            ConnectWifi.MacRandomization.UNRECOGNIZED -> {
                return MacRandomizationSetup(
                    applied = macRandomizationName(config.macRandomizationSetting),
                    error = "unsupported wifi MAC randomization: $requested",
                )
            }
        }
        if (setting != null) {
            config.macRandomizationSetting = setting
        }
        return MacRandomizationSetup(macRandomizationName(config.macRandomizationSetting))
    }

    private fun macRandomizationName(value: Int): String {
        return if (Build.VERSION.SDK_INT >= 33) {
            when (value) {
                WifiConfiguration.RANDOMIZATION_AUTO -> "auto"
                WifiConfiguration.RANDOMIZATION_NONE -> "none"
                WifiConfiguration.RANDOMIZATION_PERSISTENT -> "persistent"
                WifiConfiguration.RANDOMIZATION_NON_PERSISTENT -> "non_persistent"
                else -> value.toString()
            }
        } else {
            "unavailable"
        }
    }

    private fun waitForDisconnectSettle(timeoutMs: Long): Boolean {
        val deadline = System.currentTimeMillis() + timeoutMs
        while (System.currentTimeMillis() < deadline) {
            val info = wifi.connectionInfo ?: return true
            if (WifiConnectorPolicy.disconnectSettled(info.networkId, info.ssid.orEmpty(), info.bssid.orEmpty(), info.supplicantState?.toString())) {
                return true
            }
            Thread.sleep(100)
        }
        val info = wifi.connectionInfo
        return WifiConnectorPolicy.disconnectSettled(
            networkId = info?.networkId ?: -1,
            ssid = info?.ssid.orEmpty(),
            bssid = info?.bssid.orEmpty(),
            supplicantState = info?.supplicantState?.toString(),
        )
    }

    private fun currentConnectionRef(info: WifiInfo?): WifiConnectorPolicy.CurrentConnectionRef? {
        return info?.let {
            WifiConnectorPolicy.CurrentConnectionRef(
                networkId = it.networkId,
                ssid = it.ssid?.trim('"').orEmpty(),
                bssid = it.bssid.orEmpty(),
                frequencyMhz = it.frequency,
                securityType = securityTypeName(it.currentSecurityType),
            )
        }
    }

    private fun errorSummary(error: Throwable): String {
        return "${error.javaClass.simpleName}:${error.message.orEmpty()}"
    }
}
