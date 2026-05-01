package io.dropcheck.agent

import android.net.NetworkCapabilities
import android.net.wifi.ScanResult
import android.net.wifi.WifiInfo
import android.net.wifi.WifiManager
import io.dropcheck.agent.grpc.WifiBand
import java.nio.ByteBuffer
import java.util.Locale

/** Normalizes WifiManager state constants into stable wire/debug strings. */
internal fun wifiStateName(state: Int): String = when (state) {
    WifiManager.WIFI_STATE_DISABLED -> "disabled"
    WifiManager.WIFI_STATE_DISABLING -> "disabling"
    WifiManager.WIFI_STATE_ENABLED -> "enabled"
    WifiManager.WIFI_STATE_ENABLING -> "enabling"
    WifiManager.WIFI_STATE_UNKNOWN -> "unknown"
    else -> "unknown($state)"
}

/** Normalizes Android Wi-Fi standard constants into protocol-facing names. */
internal fun wifiStandardName(standard: Int): String = when (standard) {
    ScanResult.WIFI_STANDARD_LEGACY -> "legacy"
    ScanResult.WIFI_STANDARD_11N -> "802.11n"
    ScanResult.WIFI_STANDARD_11AC -> "802.11ac"
    ScanResult.WIFI_STANDARD_11AX -> "802.11ax"
    ScanResult.WIFI_STANDARD_11AD -> "802.11ad"
    ScanResult.WIFI_STANDARD_11BE -> "802.11be"
    ScanResult.WIFI_STANDARD_UNKNOWN -> "unknown"
    else -> "unknown($standard)"
}

/** Normalizes Android security type constants into the strings used by assertions. */
internal fun securityTypeName(type: Int): String = when (type) {
    WifiInfo.SECURITY_TYPE_OPEN -> "open"
    WifiInfo.SECURITY_TYPE_WEP -> "wep"
    WifiInfo.SECURITY_TYPE_PSK -> "psk"
    WifiInfo.SECURITY_TYPE_EAP -> "eap"
    WifiInfo.SECURITY_TYPE_SAE -> "sae"
    WifiInfo.SECURITY_TYPE_OWE -> "owe"
    WifiInfo.SECURITY_TYPE_OSEN -> "osen"
    WifiInfo.SECURITY_TYPE_DPP -> "dpp"
    WifiInfo.SECURITY_TYPE_WAPI_PSK -> "wapi_psk"
    WifiInfo.SECURITY_TYPE_WAPI_CERT -> "wapi_cert"
    WifiInfo.SECURITY_TYPE_EAP_WPA3_ENTERPRISE -> "eap_wpa3_enterprise"
    WifiInfo.SECURITY_TYPE_EAP_WPA3_ENTERPRISE_192_BIT -> "eap_wpa3_enterprise_192_bit"
    WifiInfo.SECURITY_TYPE_PASSPOINT_R1_R2 -> "passpoint_r1_r2"
    WifiInfo.SECURITY_TYPE_PASSPOINT_R3 -> "passpoint_r3"
    WifiInfo.SECURITY_TYPE_UNKNOWN -> "unknown"
    else -> "unknown($type)"
}

internal fun securityTypeName(types: IntArray): List<String> = types.map { securityTypeName(it) }

internal fun capabilityName(capability: Int): String = when (capability) {
    NetworkCapabilities.NET_CAPABILITY_MMS -> "mms"
    NetworkCapabilities.NET_CAPABILITY_SUPL -> "supl"
    NetworkCapabilities.NET_CAPABILITY_DUN -> "dun"
    NetworkCapabilities.NET_CAPABILITY_FOTA -> "fota"
    NetworkCapabilities.NET_CAPABILITY_IMS -> "ims"
    NetworkCapabilities.NET_CAPABILITY_CBS -> "cbs"
    NetworkCapabilities.NET_CAPABILITY_WIFI_P2P -> "wifi_p2p"
    NetworkCapabilities.NET_CAPABILITY_IA -> "ia"
    NetworkCapabilities.NET_CAPABILITY_RCS -> "rcs"
    NetworkCapabilities.NET_CAPABILITY_XCAP -> "xcap"
    NetworkCapabilities.NET_CAPABILITY_EIMS -> "eims"
    NetworkCapabilities.NET_CAPABILITY_NOT_METERED -> "not_metered"
    NetworkCapabilities.NET_CAPABILITY_INTERNET -> "internet"
    NetworkCapabilities.NET_CAPABILITY_NOT_RESTRICTED -> "not_restricted"
    NetworkCapabilities.NET_CAPABILITY_TRUSTED -> "trusted"
    NetworkCapabilities.NET_CAPABILITY_NOT_VPN -> "not_vpn"
    NetworkCapabilities.NET_CAPABILITY_VALIDATED -> "validated"
    NetworkCapabilities.NET_CAPABILITY_CAPTIVE_PORTAL -> "captive_portal"
    NetworkCapabilities.NET_CAPABILITY_NOT_ROAMING -> "not_roaming"
    NetworkCapabilities.NET_CAPABILITY_FOREGROUND -> "foreground"
    NetworkCapabilities.NET_CAPABILITY_NOT_CONGESTED -> "not_congested"
    NetworkCapabilities.NET_CAPABILITY_NOT_SUSPENDED -> "not_suspended"
    NetworkCapabilities.NET_CAPABILITY_MCX -> "mcx"
    NetworkCapabilities.NET_CAPABILITY_TEMPORARILY_NOT_METERED -> "temporarily_not_metered"
    NetworkCapabilities.NET_CAPABILITY_ENTERPRISE -> "enterprise"
    NetworkCapabilities.NET_CAPABILITY_PRIORITIZE_BANDWIDTH -> "prioritize_bandwidth"
    NetworkCapabilities.NET_CAPABILITY_PRIORITIZE_LATENCY -> "prioritize_latency"
    28 -> "not_vcn_managed"
    NetworkCapabilities.NET_CAPABILITY_NOT_BANDWIDTH_CONSTRAINED -> "not_bandwidth_constrained"
    NetworkCapabilities.NET_CAPABILITY_HEAD_UNIT -> "head_unit"
    NetworkCapabilities.NET_CAPABILITY_MMTEL -> "mmtel"
    NetworkCapabilities.NET_CAPABILITY_LOCAL_NETWORK -> "local_network"
    else -> "unknown($capability)"
}

internal fun enterpriseIdName(id: Int): String = when (id) {
    NetworkCapabilities.NET_ENTERPRISE_ID_1 -> "enterprise_1"
    NetworkCapabilities.NET_ENTERPRISE_ID_2 -> "enterprise_2"
    NetworkCapabilities.NET_ENTERPRISE_ID_3 -> "enterprise_3"
    NetworkCapabilities.NET_ENTERPRISE_ID_4 -> "enterprise_4"
    NetworkCapabilities.NET_ENTERPRISE_ID_5 -> "enterprise_5"
    else -> "unknown($id)"
}

internal fun channelWidthName(width: Int): String = when (width) {
    ScanResult.CHANNEL_WIDTH_20MHZ -> "20MHz"
    ScanResult.CHANNEL_WIDTH_40MHZ -> "40MHz"
    ScanResult.CHANNEL_WIDTH_80MHZ -> "80MHz"
    ScanResult.CHANNEL_WIDTH_160MHZ -> "160MHz"
    ScanResult.CHANNEL_WIDTH_80MHZ_PLUS_MHZ -> "80+80MHz"
    ScanResult.CHANNEL_WIDTH_320MHZ -> "320MHz"
    else -> "unknown($width)"
}

/** Classifies a center frequency into the coarse Wi-Fi band labels used by scans/assertions. */
internal fun bandNameForFrequency(frequencyMhz: Int): String = when (frequencyMhz) {
    in 2400..2499 -> "2.4GHz"
    in 4900..5899 -> "5GHz"
    in 5925..7125 -> "6GHz"
    in 57000..71000 -> "60GHz"
    else -> "unknown"
}

internal fun scanResultMatchesBand(result: ScanResult, band: WifiBand): Boolean =
    frequencyMatchesWifiBand(result.frequency, band)

/** Returns whether an observed frequency satisfies a controller-requested band filter. */
internal fun frequencyMatchesWifiBand(frequencyMhz: Int, band: WifiBand): Boolean = when (band) {
    WifiBand.WIFI_BAND_UNSPECIFIED,
    WifiBand.WIFI_BAND_ALL -> true
    WifiBand.WIFI_BAND_2_4_GHZ -> frequencyMhz in 2400..2499
    WifiBand.WIFI_BAND_5_GHZ -> frequencyMhz in 4900..5899
    WifiBand.WIFI_BAND_6_GHZ -> frequencyMhz in 5925..7125
    WifiBand.WIFI_BAND_60_GHZ -> frequencyMhz in 57000..71000
    WifiBand.UNRECOGNIZED -> true
}

/** Renders the protobuf band enum as a concise diagnostic label. */
internal fun wifiBandName(band: WifiBand): String = when (band) {
    WifiBand.WIFI_BAND_UNSPECIFIED,
    WifiBand.WIFI_BAND_ALL -> "all"
    WifiBand.WIFI_BAND_2_4_GHZ -> "2.4GHz"
    WifiBand.WIFI_BAND_5_GHZ -> "5GHz"
    WifiBand.WIFI_BAND_6_GHZ -> "6GHz"
    WifiBand.WIFI_BAND_60_GHZ -> "60GHz"
    WifiBand.UNRECOGNIZED -> "unrecognized"
}

internal fun scanBandName(band: Int): String = when (band) {
    ScanResult.WIFI_BAND_24_GHZ -> "2.4GHz"
    ScanResult.WIFI_BAND_5_GHZ -> "5GHz"
    ScanResult.WIFI_BAND_6_GHZ -> "6GHz"
    ScanResult.WIFI_BAND_60_GHZ -> "60GHz"
    ScanResult.UNSPECIFIED -> "unspecified"
    else -> "unknown($band)"
}

internal fun mloLinkStateName(state: Int): String = when (state) {
    android.net.wifi.MloLink.MLO_LINK_STATE_INVALID -> "invalid"
    android.net.wifi.MloLink.MLO_LINK_STATE_UNASSOCIATED -> "unassociated"
    android.net.wifi.MloLink.MLO_LINK_STATE_IDLE -> "idle"
    android.net.wifi.MloLink.MLO_LINK_STATE_ACTIVE -> "active"
    else -> "unknown($state)"
}

internal fun multiInternetModeName(mode: Int): String = when (mode) {
    WifiManager.WIFI_MULTI_INTERNET_MODE_DISABLED -> "disabled"
    WifiManager.WIFI_MULTI_INTERNET_MODE_DBS_AP -> "dbs_ap"
    WifiManager.WIFI_MULTI_INTERNET_MODE_MULTI_AP -> "multi_ap"
    else -> "unknown($mode)"
}

/**
 * Copies from the buffer's current position without mutating the source buffer.
 */
internal fun byteBufferBytes(buffer: ByteBuffer?): ByteArray {
    if (buffer == null) return ByteArray(0)
    val copy = buffer.asReadOnlyBuffer()
    val bytes = ByteArray(copy.remaining())
    copy.get(bytes)
    return bytes
}

internal fun ByteArray.toHex(): String = joinToString("") { "%02x".format(it.toInt() and 0xff) }

/**
 * Formats WifiInfo.ipAddress, which Android exposes as a little-endian IPv4 int.
 */
internal fun formatIpv4(value: Int): String {
    if (value == 0) return ""
    return String.format(
        Locale.US,
        "%d.%d.%d.%d",
        value and 0xff,
        value shr 8 and 0xff,
        value shr 16 and 0xff,
        value shr 24 and 0xff,
    )
}
