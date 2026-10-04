package io.dropcheck.agent

import android.annotation.SuppressLint
import android.content.Context
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import android.net.NetworkRequest
import android.net.wifi.WifiInfo
import android.os.Handler
import android.os.HandlerThread
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference

internal fun isPhysicalWifiNetwork(wifiTransport: Boolean, vpnTransport: Boolean): Boolean =
    wifiTransport && !vpnTransport

internal fun NetworkCapabilities.isPhysicalWifiNetwork(): Boolean = isPhysicalWifiNetwork(
    hasTransport(NetworkCapabilities.TRANSPORT_WIFI), hasTransport(NetworkCapabilities.TRANSPORT_VPN),
)

internal enum class WifiNetworkMatch(val rejectReason: String) {
    MATCH("none"),
    SSID_MISMATCH("ssid_mismatch"),
    IDENTITY_UNAVAILABLE("wifi_identity_unavailable"),
    NOT_WIFI("not_physical_wifi"),
    NO_CAPABILITIES("no_capabilities"),
}

internal fun networkWifiSsidMatch(expected: String, actual: String): WifiNetworkMatch {
    if (expected.isEmpty()) return WifiNetworkMatch.MATCH
    if (!isKnownWifiSsid(actual)) return WifiNetworkMatch.IDENTITY_UNAVAILABLE
    return if (normalizedWifiSsid(actual) == expected) WifiNetworkMatch.MATCH else WifiNetworkMatch.SSID_MISMATCH
}

internal fun <N : Any> selectWifiCandidate(
    candidates: Iterable<N>,
    classify: (N) -> WifiNetworkMatch,
    onCandidate: (N, WifiNetworkMatch) -> Unit,
): N? {
    var identityUnavailable = false
    for (candidate in candidates) {
        val match = classify(candidate)
        onCandidate(candidate, match)
        when (match) {
            WifiNetworkMatch.MATCH -> return candidate
            WifiNetworkMatch.IDENTITY_UNAVAILABLE -> identityUnavailable = true
            WifiNetworkMatch.SSID_MISMATCH, WifiNetworkMatch.NOT_WIFI, WifiNetworkMatch.NO_CAPABILITIES -> Unit
        }
    }
    check(!identityUnavailable) { "wifi identity unavailable; cannot select SSID" }
    return null
}

/** A bounded, uncached observation; only the requested Network may supply its value. */
internal fun <N, T> awaitNetworkValue(
    network: N,
    timeoutMs: Long,
    register: ((N, T?) -> Unit) -> Unit,
    unregister: () -> Unit,
): T? {
    val ready = CountDownLatch(1)
    val value = AtomicReference<T?>()
    register { reportedNetwork, reportedValue ->
        if (reportedNetwork == network) {
            value.set(reportedValue)
            ready.countDown()
        }
    }
    try {
        if (!ready.await(timeoutMs, TimeUnit.MILLISECONDS)) return null
    } finally {
        unregister()
    }
    return value.get()
}

/** On-demand capabilities redact location data; the API 31 callback supplies Network + WifiInfo. */
@SuppressLint("MissingPermission")
internal fun networkWifiInfo(context: Context, network: Network?, caps: NetworkCapabilities?): WifiInfo? {
    if (network == null || caps?.isPhysicalWifiNetwork() != true) return null
    val primary = caps.transportInfo as? WifiInfo
    if (primary != null && (isKnownWifiSsid(primary.ssid.orEmpty()) || isKnownWifiBssid(primary.bssid.orEmpty()))) {
        return primary
    }
    val connectivity = context.getSystemService(ConnectivityManager::class.java)
    lateinit var callback: ConnectivityManager.NetworkCallback
    // A caller may itself be on ConnectivityManager's default callback thread.
    // ponytail: one short-lived thread per lookup; use an owned observer only if polling cost matters.
    val delivery = HandlerThread("wifi-identity").apply { start() }
    val info = try {
        awaitNetworkValue<Network, WifiInfo>(network, 1000, register = { report ->
            callback = object : ConnectivityManager.NetworkCallback(ConnectivityManager.NetworkCallback.FLAG_INCLUDE_LOCATION_INFO) {
                override fun onCapabilitiesChanged(reportedNetwork: Network, capabilities: NetworkCapabilities) {
                    report(reportedNetwork, (capabilities.transportInfo as? WifiInfo).takeIf { capabilities.isPhysicalWifiNetwork() })
                }

                override fun onLost(reportedNetwork: Network) {
                    report(reportedNetwork, null)
                }
            }
            val request = NetworkRequest.Builder().clearCapabilities()
                .addTransportType(NetworkCapabilities.TRANSPORT_WIFI)
                .addCapability(NetworkCapabilities.NET_CAPABILITY_NOT_VPN)
                .build()
            connectivity.registerNetworkCallback(request, callback, Handler(delivery.looper))
        }, unregister = { connectivity.unregisterNetworkCallback(callback) })
    } finally {
        delivery.quitSafely()
    }
    val usable = info?.takeIf {
        isKnownWifiSsid(it.ssid.orEmpty()) || clockWidgetWifiInfoIsUsable(it.networkId, it.ssid, it.bssid, it.supplicantState?.toString())
    }
    if (usable == null || (!isKnownWifiSsid(usable.ssid.orEmpty()) && !isKnownWifiBssid(usable.bssid.orEmpty()))) {
        TerminalLog.warnEvent(context, "wifi.identity.unavailable", listOf("network" to network, "reason" to "missing_or_redacted_callback"))
    }
    return usable
}
