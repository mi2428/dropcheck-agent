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

/**
 * Maps Android Wi-Fi framework objects to the wire protobuf shape.
 *
 * The mapper should preserve Android-reported facts and avoid policy decisions
 * such as whether a scan or assertion succeeded.
 */
@Suppress("DEPRECATION")
class WifiProtoMapper(
    private val wifi: WifiManager,
) {
    /**
     * Maps a connected WifiInfo snapshot.
     *
     * The raw string is preserved because OEM builds often include extra fields
     * that are useful during device lab debugging.
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
            .setRaw(info.toString())

        if (Build.VERSION.SDK_INT >= 33) {
            builder.restricted = info.isRestricted
        }
        if (Build.VERSION.SDK_INT >= 35) {
            builder.passpointUniqueId = info.passpointUniqueId.orEmpty()
        }
        if (Build.VERSION.SDK_INT >= 33) {
            builder.apMldMacAddress = info.apMldMacAddress?.toString().orEmpty()
            builder.apMloLinkId = info.apMloLinkId
            builder.addAllAffiliatedMloLinks(info.affiliatedMloLinks.map { mloLinkInfo(it) })
        }
        if (Build.VERSION.SDK_INT >= 34) {
            builder.addAllAssociatedMloLinks(info.associatedMloLinks.map { mloLinkInfo(it) })
        }
        builder.addAllInformationElements(info.informationElements.orEmpty().map { informationElement(it) })
        return builder.build()
    }

    /** Maps one cached scan result, including newer MLO and ranging fields when API level allows. */
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
            .setRaw(result.toString())

        if (Build.VERSION.SDK_INT >= 33) {
            builder.wifiSsid = result.wifiSsid?.toString().orEmpty()
            builder.addAllSecurityTypes(result.securityTypes.map { securityTypeName(it) })
            builder.apMldMacAddress = result.apMldMacAddress?.toString().orEmpty()
            builder.apMloLinkId = result.apMloLinkId
            builder.addAllAffiliatedMloLinks(result.affiliatedMloLinks.map { mloLinkInfo(it) })
        }
        if (Build.VERSION.SDK_INT >= 35) {
            builder.responder80211AzNtb = result.is80211azNtbResponder
            builder.twtResponder = result.isTwtResponder
        }
        if (Build.VERSION.SDK_INT >= 36) {
            builder.rangingFrameProtectionRequired = result.isRangingFrameProtectionRequired
            builder.secureHeLtfSupported = result.isSecureHeLtfSupported
        }
        builder.addAllInformationElements(result.informationElements.orEmpty().map { informationElement(it) })
        return builder.build()
    }

    /** Maps one Multi-Link Operation link while respecting API-level field availability. */
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
        return builder.build()
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
