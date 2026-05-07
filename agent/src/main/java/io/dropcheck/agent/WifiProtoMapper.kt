package io.dropcheck.agent

import android.annotation.SuppressLint
import android.net.wifi.MloLink
import android.net.wifi.ScanResult
import android.net.wifi.WifiInfo
import android.net.wifi.WifiManager
import android.os.Build
import io.dropcheck.agent.grpc.MloLinkInfo
import io.dropcheck.agent.grpc.WifiConnection
import io.dropcheck.agent.grpc.WifiInformationElement
import io.dropcheck.agent.grpc.WifiScanResult
import java.util.Collections

@Suppress("DEPRECATION")
/**
 * Maps Android Wi-Fi framework objects to the wire protobuf shape.
 *
 * The mapper should preserve Android-reported facts and avoid policy decisions
 * such as whether a scan or assertion succeeded.
 *
 * Protobuf `raw` fields emitted here are string snapshots of Android framework
 * objects such as WifiInfo, ScanResult, or MloLink. They are not ADB command
 * stdout/stderr captures.
 */
class WifiProtoMapper(
    private val wifi: WifiManager,
    private val onMloUnavailable: (List<Pair<String, Any?>>) -> Unit = {},
) {
    private val reportedMloUnavailable = Collections.synchronizedSet(mutableSetOf<String>())

    /**
     * Maps a connected WifiInfo snapshot.
     *
     * The protobuf raw string is WifiInfo.toString() so OEM-specific framework
     * fields remain visible during device lab debugging.
     */
    @SuppressLint("HardwareIds")
    fun wifiConnection(info: WifiInfo): WifiConnection {
        val builder = WifiConnection.newBuilder()
            .setSsid(info.ssid?.trim('"').orEmpty())
            .setBssid(info.bssid.orEmpty())
            .setRssiDbm(info.rssi)
            .setNetworkId(info.networkId)
            .setSupplicantState(info.supplicantState?.toString().orEmpty())
            .setFrequencyMhz(info.frequency)
            .setLinkSpeedMbps(info.linkSpeed)
            .setTxLinkSpeedMbps(info.txLinkSpeedMbps)
            .setRxLinkSpeedMbps(info.rxLinkSpeedMbps)
            .setWifiStandard(wifiStandardName(info.wifiStandard))
            .setChannelWidth(connectionChannelWidth(info))
            .setSecurityType(securityTypeName(info.currentSecurityType))
            .setIpv4Address(formatIpv4(info.ipAddress))
            .setMacAddress(info.macAddress.orEmpty())
            .setHiddenSsid(info.hiddenSSID)
            .setDetailedState(WifiInfo.getDetailedStateOf(info.supplicantState).toString())
            .setSignalLevel(runCatching { wifi.calculateSignalLevel(info.rssi) }.getOrDefault(0))
            .setMaxSignalLevel(runCatching { wifi.maxSignalLevel }.getOrDefault(0))
            .setMaxSupportedTxLinkSpeedMbps(info.maxSupportedTxLinkSpeedMbps)
            .setMaxSupportedRxLinkSpeedMbps(info.maxSupportedRxLinkSpeedMbps)
            .setPasspointFqdn(info.passpointFqdn.orEmpty())
            .setPasspointProviderFriendlyName(info.passpointProviderFriendlyName.orEmpty())
            .setSubscriptionId(info.subscriptionId)
            .setApplicableRedactions(info.applicableRedactions.toString())
            .setApMloLinkId(-1)
            .setRaw(info.toString())

        if (Build.VERSION.SDK_INT >= 33) {
            builder.restricted = info.isRestricted
        }
        if (Build.VERSION.SDK_INT >= 35) {
            builder.passpointUniqueId = info.passpointUniqueId.orEmpty()
        }
        if (Build.VERSION.SDK_INT >= 33) {
            runCatching { info.apMldMacAddress?.toString().orEmpty() }
                .onSuccess { builder.apMldMacAddress = it }
                .onFailure { warnMloUnavailable("api_call_failed", "scope" to "connection", "field" to "ap_mld_mac_address", "error" to errorSummary(it)) }
            runCatching { info.apMloLinkId }
                .onSuccess { builder.apMloLinkId = it }
                .onFailure { warnMloUnavailable("api_call_failed", "scope" to "connection", "field" to "ap_mlo_link_id", "error" to errorSummary(it)) }
            runCatching { info.affiliatedMloLinks.map { mloLinkInfo(it) } }
                .onSuccess { builder.addAllAffiliatedMloLinks(it) }
                .onFailure { warnMloUnavailable("api_call_failed", "scope" to "connection", "field" to "affiliated_mlo_links", "error" to errorSummary(it)) }
        } else {
            warnMloUnavailable("android_api_unavailable", "scope" to "connection", "required_sdk" to 33)
        }
        if (Build.VERSION.SDK_INT >= 34) {
            runCatching { info.associatedMloLinks.map { mloLinkInfo(it) } }
                .onSuccess { builder.addAllAssociatedMloLinks(it) }
                .onFailure { warnMloUnavailable("api_call_failed", "scope" to "connection", "field" to "associated_mlo_links", "error" to errorSummary(it)) }
        } else {
            warnMloUnavailable("android_api_unavailable", "scope" to "connection", "field" to "associated_mlo_links", "required_sdk" to 34)
        }
        val informationElements = info.informationElements.orEmpty().map { informationElement(it) }
        builder.addAllInformationElements(informationElements)
        applyDecodedInformationElements(builder, decodeWifiInformationElements(informationElements))
        return builder.build()
    }

    /**
     * Infers the connected channel width from the most recent scan cache.
     *
     * Android exposes channel width on ScanResult but not on WifiInfo. Matching
     * by BSSID is the least ambiguous route; SSID+frequency is a fallback for
     * devices that redact BSSID from WifiInfo but still return scan metadata.
     */
    @SuppressLint("MissingPermission")
    private fun connectionChannelWidth(info: WifiInfo): String {
        val results = runCatching { wifi.scanResults.orEmpty() }.getOrDefault(emptyList())
        if (results.isEmpty()) return ""

        val bssid = info.bssid.orEmpty()
        val ssid = info.ssid?.trim('"').orEmpty()
        val matched = results.firstOrNull { result ->
            bssid.isNotEmpty() && result.BSSID.orEmpty().equals(bssid, ignoreCase = true)
        } ?: results.firstOrNull { result ->
            ssid.isNotEmpty() &&
                result.SSID.orEmpty() == ssid &&
                result.frequency == info.frequency
        }
        return matched?.let { channelWidthName(it.channelWidth) }.orEmpty()
    }

    /**
     * Maps one cached scan result, including newer MLO and ranging fields when API level allows.
     *
     * The protobuf raw string is ScanResult.toString(); ADB command output is
     * collected separately by the controller diagnostics path.
     */
    fun scanResult(result: ScanResult): WifiScanResult {
        val builder = WifiScanResult.newBuilder()
            .setSsid(result.SSID.orEmpty())
            .setBssid(result.BSSID.orEmpty())
            .setCapabilities(result.capabilities.orEmpty())
            .setRssiDbm(result.level)
            .setFrequencyMhz(result.frequency)
            .setBand(bandNameForFrequency(result.frequency))
            .setChannelWidth(channelWidthName(result.channelWidth))
            .setCenterFreq0Mhz(result.centerFreq0)
            .setCenterFreq1Mhz(result.centerFreq1)
            .setTimestampUs(result.timestamp)
            .setWifiStandard(wifiStandardName(result.wifiStandard))
            .setOperatorFriendlyName(result.operatorFriendlyName?.toString().orEmpty())
            .setVenueName(result.venueName?.toString().orEmpty())
            .setPasspoint(result.isPasspointNetwork)
            .setResponder80211Mc(result.is80211mcResponder)
            .setApMloLinkId(-1)
            .setRaw(result.toString())

        if (Build.VERSION.SDK_INT >= 33) {
            builder.wifiSsid = result.wifiSsid?.toString().orEmpty()
            builder.addAllSecurityTypes(result.securityTypes.map { securityTypeName(it) })
            runCatching { result.apMldMacAddress?.toString().orEmpty() }
                .onSuccess { builder.apMldMacAddress = it }
                .onFailure { warnMloUnavailable("api_call_failed", "scope" to "scan", "field" to "ap_mld_mac_address", "error" to errorSummary(it)) }
            runCatching { result.apMloLinkId }
                .onSuccess { builder.apMloLinkId = it }
                .onFailure { warnMloUnavailable("api_call_failed", "scope" to "scan", "field" to "ap_mlo_link_id", "error" to errorSummary(it)) }
            runCatching { result.affiliatedMloLinks.map { mloLinkInfo(it) } }
                .onSuccess { builder.addAllAffiliatedMloLinks(it) }
                .onFailure { warnMloUnavailable("api_call_failed", "scope" to "scan", "field" to "affiliated_mlo_links", "error" to errorSummary(it)) }
        } else {
            warnMloUnavailable("android_api_unavailable", "scope" to "scan", "required_sdk" to 33)
        }
        if (Build.VERSION.SDK_INT >= 35) {
            builder.responder80211AzNtb = result.is80211azNtbResponder
            builder.twtResponder = result.isTwtResponder
        }
        if (Build.VERSION.SDK_INT >= 36) {
            builder.rangingFrameProtectionRequired = result.isRangingFrameProtectionRequired
            builder.secureHeLtfSupported = result.isSecureHeLtfSupported
        }
        val informationElements = result.informationElements.orEmpty().map { informationElement(it) }
        builder.addAllInformationElements(informationElements)
        applyDecodedInformationElements(builder, decodeWifiInformationElements(informationElements))
        return builder.build()
    }

    /**
     * Maps one Multi-Link Operation link while respecting API-level field availability.
     *
     * The protobuf raw string is MloLink.toString(), preserving framework
     * detail without implying an ADB stdout/stderr payload.
     */
    private fun mloLinkInfo(link: MloLink): MloLinkInfo {
        val builder = MloLinkInfo.newBuilder()
            .setRaw(link.toString())
        if (Build.VERSION.SDK_INT >= 33) {
            builder
                .setLinkId(link.linkId)
                .setState(mloLinkStateName(link.state))
                .setBand(scanBandName(link.band))
                .setChannel(link.channel)
                .setApMacAddress(link.apMacAddress?.toString().orEmpty())
                .setStaMacAddress(link.staMacAddress?.toString().orEmpty())
        }
        if (Build.VERSION.SDK_INT >= 34) {
            builder
                .setRssiDbm(link.rssi)
                .setTxLinkSpeedMbps(link.txLinkSpeedMbps)
                .setRxLinkSpeedMbps(link.rxLinkSpeedMbps)
        }
        builder
            .setMaxSupportedTxLinkSpeedMbps(intGetterOrDefault(link, "getMaxSupportedTxLinkSpeedMbps", 0))
            .setMaxSupportedRxLinkSpeedMbps(intGetterOrDefault(link, "getMaxSupportedRxLinkSpeedMbps", 0))
        return builder.build()
    }

    private fun intGetterOrDefault(target: Any, methodName: String, defaultValue: Int): Int {
        return runCatching {
            target.javaClass.getMethod(methodName).invoke(target) as? Int ?: defaultValue
        }.getOrDefault(defaultValue)
    }

    private fun applyDecodedInformationElements(
        builder: WifiConnection.Builder,
        decodes: WifiInformationElementDecodes,
    ) {
        decodes.heCapabilities?.let { builder.setHeCapabilities(it) }
        decodes.heOperation?.let { builder.setHeOperation(it) }
        decodes.ehtCapabilities?.let { builder.setEhtCapabilities(it) }
        decodes.ehtOperation?.let { builder.setEhtOperation(it) }
        decodes.heUoraParameterSet?.let { builder.setHeUoraParameterSet(it) }
        decodes.heMuEdcaParameterSet?.let { builder.setHeMuEdcaParameterSet(it) }
        decodes.heSpatialReuseParameterSet?.let { builder.setHeSpatialReuseParameterSet(it) }
    }

    private fun applyDecodedInformationElements(
        builder: WifiScanResult.Builder,
        decodes: WifiInformationElementDecodes,
    ) {
        decodes.heCapabilities?.let { builder.setHeCapabilities(it) }
        decodes.heOperation?.let { builder.setHeOperation(it) }
        decodes.ehtCapabilities?.let { builder.setEhtCapabilities(it) }
        decodes.ehtOperation?.let { builder.setEhtOperation(it) }
        decodes.heUoraParameterSet?.let { builder.setHeUoraParameterSet(it) }
        decodes.heMuEdcaParameterSet?.let { builder.setHeMuEdcaParameterSet(it) }
        decodes.heSpatialReuseParameterSet?.let { builder.setHeSpatialReuseParameterSet(it) }
    }

    private fun warnMloUnavailable(reason: String, vararg fields: Pair<String, Any?>) {
        val allFields = listOf("reason" to reason, "sdk" to Build.VERSION.SDK_INT) + fields
        val key = allFields.joinToString("|") { "${it.first}=${it.second}" }
        if (reportedMloUnavailable.add(key)) {
            onMloUnavailable(allFields)
        }
    }

    private fun errorSummary(error: Throwable): String {
        return "${error.javaClass.simpleName}:${error.message.orEmpty()}"
    }

    /** Copies the raw information element bytes as hex for controller-side decoding. */
    private fun informationElement(element: ScanResult.InformationElement): WifiInformationElement {
        val bytes = byteBufferBytes(element.bytes)
        return WifiInformationElement.newBuilder()
            .setId(element.id)
            .setIdExt(element.idExt)
            .setByteCount(bytes.size)
            .setBytesHex(bytes.toHex())
            .build()
    }
}
