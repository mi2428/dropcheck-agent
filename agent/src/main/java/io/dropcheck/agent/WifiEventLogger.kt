@file:Suppress("DEPRECATION")

package io.dropcheck.agent

import android.annotation.SuppressLint
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.net.ConnectivityManager
import android.net.LinkProperties
import android.net.Network
import android.net.NetworkCapabilities
import android.net.NetworkInfo
import android.net.NetworkRequest
import android.net.wifi.SupplicantState
import android.net.wifi.WifiInfo
import android.net.wifi.WifiManager
import android.os.Build
import java.util.concurrent.Executors
import java.util.concurrent.ScheduledExecutorService
import java.util.concurrent.ScheduledFuture
import java.util.concurrent.TimeUnit
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.atomic.AtomicBoolean

/**
 * Background Wi-Fi/network event logger for lab diagnostics.
 *
 * It samples Android framework state after broadcasts and callbacks, then logs
 * only snapshot changes so long test runs remain readable.
 */
internal class WifiEventLogger(
    private val context: Context,
) {
    private val appContext = context.applicationContext
    private val connectivity = appContext.getSystemService(ConnectivityManager::class.java)
    private val wifi = appContext.getSystemService(WifiManager::class.java)
    private val mapper = WifiProtoMapper(wifi)
    private val started = AtomicBoolean(false)
    private val executor: ScheduledExecutorService = Executors.newSingleThreadScheduledExecutor()
    private val lock = Any()

    private var pollTask: ScheduledFuture<*>? = null
    private var broadcastRegistered = false
    private var wifiCallbackRegistered = false
    private var defaultCallbackRegistered = false
    private var lastSnapshot: WifiEventSnapshot? = null
    private val blockedNetworks = ConcurrentHashMap<String, Boolean>()

    private val receiver = object : BroadcastReceiver() {
        override fun onReceive(context: Context, intent: Intent) {
            if (!started.get()) return
            val fields = broadcastFields(intent)
            when (intent.action) {
                WifiManager.WIFI_STATE_CHANGED_ACTION -> logBroadcastInfo("wifi.broadcast.state_changed", intent, fields)
                WifiManager.NETWORK_STATE_CHANGED_ACTION -> logBroadcastInfo("wifi.broadcast.network_state_changed", intent, fields)
                WifiManager.SUPPLICANT_STATE_CHANGED_ACTION -> logBroadcastInfo("wifi.broadcast.supplicant_state_changed", intent, fields)
                WifiManager.RSSI_CHANGED_ACTION -> logBroadcastDebug("wifi.broadcast.rssi_changed", intent, fields)
                WifiManager.SCAN_RESULTS_AVAILABLE_ACTION -> logBroadcastDebug("wifi.broadcast.scan_results_available", intent, fields)
                WifiManager.ACTION_WIFI_NETWORK_SUGGESTION_POST_CONNECTION -> logBroadcastInfo("wifi.broadcast.suggestion_post_connection", intent, fields)
                else -> logBroadcastDebug("wifi.broadcast", intent, fields)
            }
            capture("broadcast", fields)
        }
    }

    private val wifiCallback = object : ConnectivityManager.NetworkCallback(
        ConnectivityManager.NetworkCallback.FLAG_INCLUDE_LOCATION_INFO,
    ) {
        override fun onAvailable(network: Network) {
            if (!started.get()) return
            logNetworkInfo("wifi.network.available", network)
            capture("network_available", listOf("network" to network.toString()))
        }

        override fun onLosing(network: Network, maxMsToLive: Int) {
            if (!started.get()) return
            logNetworkInfo("wifi.network.losing", network, listOf("max_ms_to_live" to maxMsToLive))
            capture("network_losing", listOf("network" to network.toString(), "max_ms_to_live" to maxMsToLive))
        }

        override fun onLost(network: Network) {
            if (!started.get()) return
            logNetworkInfo("wifi.network.lost", network)
            blockedNetworks.remove(network.toString())
            capture("network_lost", listOf("network" to network.toString()))
        }

        override fun onUnavailable() {
            if (!started.get()) return
            TerminalLog.infoEvent(appContext, "wifi.network.unavailable", listOf("callback" to "wifi"))
            capture("network_unavailable", listOf("callback" to "wifi"))
        }

        override fun onCapabilitiesChanged(network: Network, networkCapabilities: NetworkCapabilities) {
            if (!started.get()) return
            logNetworkDebug(
                "wifi.network.capabilities_changed",
                network,
                listOf("reported_capabilities" to capabilityNames(networkCapabilities)),
            )
            capture("capabilities_changed", listOf("network" to network.toString()))
        }

        override fun onLinkPropertiesChanged(network: Network, linkProperties: LinkProperties) {
            if (!started.get()) return
            logNetworkInfo(
                "wifi.network.link_properties_changed",
                network,
                linkFields(linkProperties, "reported_"),
            )
            capture("link_properties_changed", listOf("network" to network.toString()))
        }

        override fun onBlockedStatusChanged(network: Network, blocked: Boolean) {
            if (!started.get()) return
            blockedNetworks[network.toString()] = blocked
            logNetworkInfo("wifi.network.blocked_status_changed", network, listOf("blocked" to blocked))
            capture("blocked_status_changed", listOf("network" to network.toString(), "blocked" to blocked))
        }
    }

    private val defaultCallback = object : ConnectivityManager.NetworkCallback(
        ConnectivityManager.NetworkCallback.FLAG_INCLUDE_LOCATION_INFO,
    ) {
        override fun onAvailable(network: Network) {
            if (!started.get()) return
            logNetworkInfo("wifi.default_network.available", network)
            capture("default_network_available", listOf("network" to network.toString()))
        }

        override fun onLost(network: Network) {
            if (!started.get()) return
            logNetworkInfo("wifi.default_network.lost", network)
            blockedNetworks.remove(network.toString())
            capture("default_network_lost", listOf("network" to network.toString()))
        }

        override fun onCapabilitiesChanged(network: Network, networkCapabilities: NetworkCapabilities) {
            if (!started.get()) return
            if (!networkCapabilities.hasTransport(NetworkCapabilities.TRANSPORT_WIFI)) return
            logNetworkDebug(
                "wifi.default_network.capabilities_changed",
                network,
                listOf("reported_capabilities" to capabilityNames(networkCapabilities)),
            )
            capture("default_capabilities_changed", listOf("network" to network.toString()))
        }

        override fun onLinkPropertiesChanged(network: Network, linkProperties: LinkProperties) {
            if (!started.get()) return
            logNetworkInfo(
                "wifi.default_network.link_properties_changed",
                network,
                linkFields(linkProperties, "reported_"),
            )
            capture("default_link_properties_changed", listOf("network" to network.toString()))
        }
    }

    fun start() {
        if (!started.compareAndSet(false, true)) return
        registerBroadcasts()
        registerCallbacks()
        TerminalLog.infoEvent(appContext, "wifi.events.start", listOf(
            "broadcast_registered" to broadcastRegistered,
            "wifi_callback_registered" to wifiCallbackRegistered,
            "default_callback_registered" to defaultCallbackRegistered,
            "poll_interval_ms" to POLL_INTERVAL_MS,
        ))
        capture("start", emptyList())
        pollTask = executor.scheduleWithFixedDelay({
            capture("poll", emptyList())
        }, POLL_INTERVAL_MS, POLL_INTERVAL_MS, TimeUnit.MILLISECONDS)
    }

    fun stop() {
        if (!started.compareAndSet(true, false)) return
        pollTask?.cancel(true)
        pollTask = null
        if (broadcastRegistered) {
            runCatching { appContext.unregisterReceiver(receiver) }
                .onFailure { TerminalLog.warnEvent(appContext, "wifi.events.unregister_failed", listOf("target" to "broadcast", "error" to it.toString())) }
            broadcastRegistered = false
        }
        if (wifiCallbackRegistered) {
            runCatching { connectivity.unregisterNetworkCallback(wifiCallback) }
                .onFailure { TerminalLog.warnEvent(appContext, "wifi.events.unregister_failed", listOf("target" to "wifi_callback", "error" to it.toString())) }
            wifiCallbackRegistered = false
        }
        if (defaultCallbackRegistered) {
            runCatching { connectivity.unregisterNetworkCallback(defaultCallback) }
                .onFailure { TerminalLog.warnEvent(appContext, "wifi.events.unregister_failed", listOf("target" to "default_callback", "error" to it.toString())) }
            defaultCallbackRegistered = false
        }
        TerminalLog.infoEvent(appContext, "wifi.events.stop", emptyList())
        executor.shutdownNow()
    }

    private fun registerBroadcasts() {
        val filter = IntentFilter().apply {
            addAction(WifiManager.WIFI_STATE_CHANGED_ACTION)
            addAction(WifiManager.NETWORK_STATE_CHANGED_ACTION)
            addAction(WifiManager.SUPPLICANT_STATE_CHANGED_ACTION)
            addAction(WifiManager.RSSI_CHANGED_ACTION)
            addAction(WifiManager.SCAN_RESULTS_AVAILABLE_ACTION)
            addAction(WifiManager.ACTION_WIFI_NETWORK_SUGGESTION_POST_CONNECTION)
        }
        runCatching {
            if (Build.VERSION.SDK_INT >= 33) {
                appContext.registerReceiver(receiver, filter, Context.RECEIVER_NOT_EXPORTED)
            } else {
                appContext.registerReceiver(receiver, filter)
            }
        }.onSuccess {
            broadcastRegistered = true
        }.onFailure {
            TerminalLog.warnEvent(appContext, "wifi.events.register_failed", listOf("target" to "broadcast", "error" to it.toString()))
        }
    }

    private fun registerCallbacks() {
        val request = NetworkRequest.Builder()
            .addTransportType(NetworkCapabilities.TRANSPORT_WIFI)
            .build()
        runCatching { connectivity.registerNetworkCallback(request, wifiCallback) }
            .onSuccess { wifiCallbackRegistered = true }
            .onFailure { TerminalLog.warnEvent(appContext, "wifi.events.register_failed", listOf("target" to "wifi_callback", "error" to it.toString())) }

        runCatching { connectivity.registerDefaultNetworkCallback(defaultCallback) }
            .onSuccess { defaultCallbackRegistered = true }
            .onFailure { TerminalLog.warnEvent(appContext, "wifi.events.register_failed", listOf("target" to "default_callback", "error" to it.toString())) }
    }

    private fun logBroadcastInfo(event: String, intent: Intent, fields: List<Pair<String, Any?>>) {
        TerminalLog.infoEvent(appContext, event, listOf("action" to intent.action) + fields)
    }

    private fun logBroadcastDebug(event: String, intent: Intent, fields: List<Pair<String, Any?>>) {
        TerminalLog.debugEvent(appContext, event, listOf("action" to intent.action) + fields)
    }

    private fun logNetworkInfo(event: String, network: Network, extra: List<Pair<String, Any?>> = emptyList()) {
        TerminalLog.infoEvent(appContext, event, listOf("network" to network.toString()) + extra + networkFields(network))
    }

    private fun logNetworkDebug(event: String, network: Network, extra: List<Pair<String, Any?>> = emptyList()) {
        TerminalLog.debugEvent(appContext, event, listOf("network" to network.toString()) + extra + networkFields(network))
    }

    private fun capture(reason: String, triggerFields: List<Pair<String, Any?>>) {
        if (!started.get() && reason != "stop") return
        runCatching {
            val snapshot = snapshot()
            val previous = synchronized(lock) {
                val old = lastSnapshot
                lastSnapshot = snapshot
                old
            }
            val base = listOf("reason" to reason) + triggerFields + snapshot.fields()
            if (previous == null) {
                TerminalLog.infoEvent(appContext, "wifi.snapshot.initial", base)
                return
            }
            if (snapshot.signature != previous.signature) {
                logSnapshotChanges(previous, snapshot, reason, triggerFields)
                TerminalLog.debugEvent(appContext, "wifi.snapshot.changed", base + listOf("previous_signature" to previous.signature))
            } else if (reason != "poll") {
                TerminalLog.debugEvent(appContext, "wifi.snapshot.unchanged", base)
            }
        }.onFailure {
            TerminalLog.warnEvent(appContext, "wifi.snapshot.failed", listOf("reason" to reason, "error" to it.toString()))
        }
    }

    private fun logSnapshotChanges(
        previous: WifiEventSnapshot,
        current: WifiEventSnapshot,
        reason: String,
        triggerFields: List<Pair<String, Any?>>,
    ) {
        val fields = listOf("reason" to reason) + triggerFields + current.fields() + previous.previousFields()
        if (previous.wifiState != current.wifiState || previous.wifiEnabled != current.wifiEnabled) {
            TerminalLog.infoEvent(appContext, "wifi.state.changed", fields)
        }
        if (previous.connected && !current.connected) {
            TerminalLog.infoEvent(appContext, "wifi.disconnected", fields)
        }
        if (!previous.connected && current.connected) {
            TerminalLog.infoEvent(appContext, "wifi.joined", fields)
        }
        if (previous.connected && current.connected && previous.ssid != current.ssid) {
            TerminalLog.infoEvent(appContext, "wifi.ssid.changed", fields)
        }
        if (previous.connected && current.connected && previous.ssid == current.ssid && previous.bssid != current.bssid) {
            TerminalLog.infoEvent(appContext, "wifi.roamed", fields)
        }
        if (previous.bssid != current.bssid) {
            TerminalLog.infoEvent(appContext, "wifi.bssid.changed", fields)
        }
        if (previous.addresses.isEmpty() && current.addresses.isNotEmpty()) {
            TerminalLog.infoEvent(appContext, "wifi.dhcp.address_obtained", fields)
        } else if (previous.addresses != current.addresses) {
            TerminalLog.infoEvent(appContext, "wifi.ip.addresses_changed", fields)
        }
        if (previous.dnsServers != current.dnsServers || previous.dhcpServer != current.dhcpServer || previous.routes != current.routes) {
            TerminalLog.infoEvent(appContext, "wifi.link.changed", fields)
        }
        if (previous.validated != current.validated || previous.internet != current.internet || previous.captivePortal != current.captivePortal) {
            TerminalLog.infoEvent(appContext, "wifi.internet_state.changed", fields)
        }
        if (previous.supplicantState != current.supplicantState || previous.detailedState != current.detailedState) {
            TerminalLog.infoEvent(appContext, "wifi.supplicant.changed", fields)
        }
        if (previous.rssiDbm != current.rssiDbm || previous.frequencyMhz != current.frequencyMhz || previous.linkSpeedMbps != current.linkSpeedMbps) {
            TerminalLog.debugEvent(appContext, "wifi.radio.changed", fields)
        }
        if (previous.rawInfo != current.rawInfo) {
            TerminalLog.debugEvent(appContext, "wifi.raw_info.changed", fields)
        }
    }

    private fun broadcastFields(intent: Intent): List<Pair<String, Any?>> {
        return buildList {
            val extras = intent.extras
            val extraKeys = extras?.keySet()?.toList().orEmpty()
            add("extras" to extraKeys)
            add("extra_values" to extraKeys.map { key ->
                "$key=${StructuredLog.preview(extras?.get(key)?.toString().orEmpty(), 300)}"
            })
            if (intent.hasExtra(WifiManager.EXTRA_WIFI_STATE)) {
                val state = intent.getIntExtra(WifiManager.EXTRA_WIFI_STATE, WifiManager.WIFI_STATE_UNKNOWN)
                add("wifi_state" to wifiStateName(state))
                add("wifi_state_raw" to state)
            }
            if (intent.hasExtra(WifiManager.EXTRA_PREVIOUS_WIFI_STATE)) {
                val state = intent.getIntExtra(WifiManager.EXTRA_PREVIOUS_WIFI_STATE, WifiManager.WIFI_STATE_UNKNOWN)
                add("previous_wifi_state" to wifiStateName(state))
                add("previous_wifi_state_raw" to state)
            }
            if (intent.hasExtra(WifiManager.EXTRA_NEW_RSSI)) {
                add("rssi_dbm" to intent.getIntExtra(WifiManager.EXTRA_NEW_RSSI, Int.MIN_VALUE))
            }
            if (intent.hasExtra(WifiManager.EXTRA_RESULTS_UPDATED)) {
                add("scan_results_updated" to intent.getBooleanExtra(WifiManager.EXTRA_RESULTS_UPDATED, false))
            }
            intent.getParcelableExtraCompat<NetworkInfo>(WifiManager.EXTRA_NETWORK_INFO)?.let { info ->
                add("network_info_state" to info.state)
                add("network_info_detailed_state" to info.detailedState)
                add("network_info_type" to info.typeName)
                add("network_info_subtype" to info.subtypeName)
                add("network_info_available" to info.isAvailable)
                add("network_info_connected" to info.isConnected)
                add("network_info_connecting" to info.isConnectedOrConnecting)
                add("network_info_reason" to info.reason)
                add("network_info_extra" to info.extraInfo)
            }
            intent.getParcelableExtraCompat<WifiInfo>(WifiManager.EXTRA_WIFI_INFO)?.let { info ->
                addAll(wifiInfoFields(info, "intent_"))
            }
            intent.getParcelableExtraCompat<SupplicantState>(WifiManager.EXTRA_NEW_STATE)?.let { state ->
                add("supplicant_state" to state.name)
                add("supplicant_detailed_state" to WifiInfo.getDetailedStateOf(state).name)
            }
            if (intent.hasExtra(WifiManager.EXTRA_SUPPLICANT_ERROR)) {
                add("supplicant_error" to intent.getIntExtra(WifiManager.EXTRA_SUPPLICANT_ERROR, 0))
            }
        }
    }

    @SuppressLint("MissingPermission")
    private fun snapshot(): WifiEventSnapshot {
        val active = connectivity.activeNetwork
        val networks = connectivity.allNetworks.toList()
        val wifiNetworks = networks.filter { network ->
            connectivity.getNetworkCapabilities(network)?.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) == true
        }
        val selected = wifiNetworks.firstOrNull { it == active } ?: wifiNetworks.firstOrNull()
        val caps = selected?.let { connectivity.getNetworkCapabilities(it) }
        val link = selected?.let { connectivity.getLinkProperties(it) }
        val info = bestWifiInfo(caps?.transportInfo as? WifiInfo)
        val mapped = info?.let { runCatching { mapper.wifiConnection(it) }.getOrNull() }
        val addresses = link?.linkAddresses?.map { it.toString() }.orEmpty()
        val dnsServers = link?.dnsServers?.mapNotNull { it.hostAddress }.orEmpty()
        val routes = link?.routes?.map { it.toString() }.orEmpty()
        return WifiEventSnapshot(
            wifiEnabled = wifi.isWifiEnabled,
            wifiState = wifiStateName(wifi.wifiState),
            activeNetwork = active?.toString().orEmpty(),
            selectedNetwork = selected?.toString().orEmpty(),
            wifiNetworkCount = wifiNetworks.size,
            connected = info != null &&
                info.ssid != WifiManager.UNKNOWN_SSID &&
                info.bssid.orEmpty().isNotBlank() &&
                info.bssid != PLACEHOLDER_BSSID,
            ssid = mapped?.ssid.orEmpty(),
            bssid = mapped?.bssid.orEmpty(),
            networkId = mapped?.networkId ?: -1,
            securityType = mapped?.securityType.orEmpty(),
            supplicantState = mapped?.supplicantState.orEmpty(),
            detailedState = mapped?.detailedState.orEmpty(),
            rssiDbm = mapped?.rssiDbm ?: 0,
            frequencyMhz = mapped?.frequencyMhz ?: 0,
            linkSpeedMbps = mapped?.linkSpeedMbps ?: 0,
            txLinkSpeedMbps = mapped?.txLinkSpeedMbps ?: 0,
            rxLinkSpeedMbps = mapped?.rxLinkSpeedMbps ?: 0,
            wifiStandard = mapped?.wifiStandard.orEmpty(),
            ipv4Address = mapped?.ipv4Address.orEmpty(),
            hiddenSsid = mapped?.hiddenSsid ?: false,
            passpointFqdn = mapped?.passpointFqdn.orEmpty(),
            apMldMacAddress = mapped?.apMldMacAddress.orEmpty(),
            apMloLinkId = mapped?.apMloLinkId ?: 0,
            associatedMloLinks = mapped?.associatedMloLinksList?.map { it.raw }.orEmpty(),
            affiliatedMloLinks = mapped?.affiliatedMloLinksList?.map { it.raw }.orEmpty(),
            iface = link?.interfaceName.orEmpty(),
            mtu = link?.mtu ?: 0,
            addresses = addresses,
            dnsServers = dnsServers,
            dhcpServer = link?.dhcpServerAddress?.hostAddress.orEmpty(),
            routes = routes,
            domains = link?.domains.orEmpty(),
            httpProxy = link?.httpProxy?.toString().orEmpty(),
            nat64Prefix = link?.nat64Prefix?.toString().orEmpty(),
            privateDnsActive = link?.isPrivateDnsActive ?: false,
            privateDnsServerName = link?.privateDnsServerName.orEmpty(),
            wakeOnLanSupported = link?.isWakeOnLanSupported ?: false,
            validated = caps?.hasCapability(NetworkCapabilities.NET_CAPABILITY_VALIDATED) ?: false,
            internet = caps?.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET) ?: false,
            captivePortal = caps?.hasCapability(NetworkCapabilities.NET_CAPABILITY_CAPTIVE_PORTAL) ?: false,
            metered = caps?.hasCapability(NetworkCapabilities.NET_CAPABILITY_NOT_METERED)?.not() ?: false,
            notRoaming = caps?.hasCapability(NetworkCapabilities.NET_CAPABILITY_NOT_ROAMING) ?: false,
            blocked = selected?.let { blockedNetworks[it.toString()] } ?: false,
            signalStrength = caps?.signalStrength ?: Int.MIN_VALUE,
            downstreamKbps = caps?.linkDownstreamBandwidthKbps ?: 0,
            upstreamKbps = caps?.linkUpstreamBandwidthKbps ?: 0,
            transports = caps?.let { transportNames(it) }.orEmpty(),
            capabilities = caps?.let { capabilityNames(it) }.orEmpty(),
            rawCapabilities = caps?.toString().orEmpty(),
            rawLinkProperties = link?.toString().orEmpty(),
            rawInfo = mapped?.raw.orEmpty(),
        )
    }

    private fun networkFields(network: Network): List<Pair<String, Any?>> {
        val caps = connectivity.getNetworkCapabilities(network)
        val link = connectivity.getLinkProperties(network)
        return buildList {
            add("is_active" to (network == connectivity.activeNetwork))
            add("transports" to caps?.let { transportNames(it) }.orEmpty())
            add("capabilities" to caps?.let { capabilityNames(it) }.orEmpty())
            add("validated" to (caps?.hasCapability(NetworkCapabilities.NET_CAPABILITY_VALIDATED) ?: false))
            add("internet" to (caps?.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET) ?: false))
            add("captive_portal" to (caps?.hasCapability(NetworkCapabilities.NET_CAPABILITY_CAPTIVE_PORTAL) ?: false))
            add("metered" to (caps?.hasCapability(NetworkCapabilities.NET_CAPABILITY_NOT_METERED)?.not() ?: false))
            add("not_roaming" to (caps?.hasCapability(NetworkCapabilities.NET_CAPABILITY_NOT_ROAMING) ?: false))
            add("signal_strength" to (caps?.signalStrength ?: Int.MIN_VALUE))
            add("downstream_kbps" to (caps?.linkDownstreamBandwidthKbps ?: 0))
            add("upstream_kbps" to (caps?.linkUpstreamBandwidthKbps ?: 0))
            val info = bestWifiInfo(caps?.transportInfo as? WifiInfo)
            if (info != null) addAll(wifiInfoFields(info))
            if (link != null) addAll(linkFields(link))
        }
    }

    private fun linkFields(link: LinkProperties, prefix: String = ""): List<Pair<String, Any?>> = listOf(
        "${prefix}iface" to link.interfaceName.orEmpty(),
        "${prefix}mtu" to link.mtu,
        "${prefix}addresses" to link.linkAddresses.map { it.toString() },
        "${prefix}dns_servers" to link.dnsServers.mapNotNull { it.hostAddress },
        "${prefix}dhcp_server" to link.dhcpServerAddress?.hostAddress.orEmpty(),
        "${prefix}routes" to link.routes.map { it.toString() },
        "${prefix}domains" to link.domains.orEmpty(),
        "${prefix}http_proxy" to link.httpProxy?.toString().orEmpty(),
        "${prefix}nat64_prefix" to link.nat64Prefix?.toString().orEmpty(),
        "${prefix}private_dns_active" to link.isPrivateDnsActive,
        "${prefix}private_dns_server_name" to link.privateDnsServerName.orEmpty(),
        "${prefix}wake_on_lan_supported" to link.isWakeOnLanSupported,
        "${prefix}raw_link_properties" to StructuredLog.preview(link.toString(), 1200),
    )

    private fun wifiInfoFields(info: WifiInfo, prefix: String = ""): List<Pair<String, Any?>> {
        val mapped = runCatching { mapper.wifiConnection(info) }.getOrNull()
        return listOf(
            "${prefix}ssid" to mapped?.ssid.orEmpty(),
            "${prefix}bssid" to mapped?.bssid.orEmpty(),
            "${prefix}rssi_dbm" to (mapped?.rssiDbm ?: info.rssi),
            "${prefix}network_id" to (mapped?.networkId ?: info.networkId),
            "${prefix}supplicant_state" to mapped?.supplicantState.orEmpty(),
            "${prefix}detailed_state" to mapped?.detailedState.orEmpty(),
            "${prefix}frequency_mhz" to (mapped?.frequencyMhz ?: info.frequency),
            "${prefix}link_speed_mbps" to (mapped?.linkSpeedMbps ?: info.linkSpeed),
            "${prefix}tx_link_speed_mbps" to (mapped?.txLinkSpeedMbps ?: info.txLinkSpeedMbps),
            "${prefix}rx_link_speed_mbps" to (mapped?.rxLinkSpeedMbps ?: info.rxLinkSpeedMbps),
            "${prefix}wifi_standard" to mapped?.wifiStandard.orEmpty(),
            "${prefix}channel_width" to mapped?.channelWidth.orEmpty(),
            "${prefix}security_type" to mapped?.securityType.orEmpty(),
            "${prefix}ipv4_address" to mapped?.ipv4Address.orEmpty(),
            "${prefix}mac_address" to mapped?.macAddress.orEmpty(),
            "${prefix}hidden_ssid" to (mapped?.hiddenSsid ?: info.hiddenSSID),
            "${prefix}signal_level" to (mapped?.signalLevel ?: 0),
            "${prefix}max_signal_level" to (mapped?.maxSignalLevel ?: 0),
            "${prefix}passpoint_fqdn" to mapped?.passpointFqdn.orEmpty(),
            "${prefix}passpoint_provider" to mapped?.passpointProviderFriendlyName.orEmpty(),
            "${prefix}ap_mld_mac_address" to mapped?.apMldMacAddress.orEmpty(),
            "${prefix}ap_mlo_link_id" to (mapped?.apMloLinkId ?: 0),
            "${prefix}affiliated_mlo_links" to mapped?.affiliatedMloLinksList?.map { it.raw }.orEmpty(),
            "${prefix}associated_mlo_links" to mapped?.associatedMloLinksList?.map { it.raw }.orEmpty(),
            "${prefix}information_elements_count" to (mapped?.informationElementsCount ?: 0),
            "${prefix}raw_wifi_info" to StructuredLog.preview(mapped?.raw ?: info.toString(), 1200),
        )
    }

    private fun transportNames(caps: NetworkCapabilities): List<String> = buildList {
        if (caps.hasTransport(NetworkCapabilities.TRANSPORT_WIFI)) add("wifi")
        if (caps.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR)) add("cellular")
        if (caps.hasTransport(NetworkCapabilities.TRANSPORT_BLUETOOTH)) add("bluetooth")
        if (caps.hasTransport(NetworkCapabilities.TRANSPORT_LOWPAN)) add("lowpan")
        if (caps.hasTransport(NetworkCapabilities.TRANSPORT_VPN)) add("vpn")
        if (caps.hasTransport(NetworkCapabilities.TRANSPORT_WIFI_AWARE)) add("wifi_aware")
        if (Build.VERSION.SDK_INT >= 35 && caps.hasTransport(NetworkCapabilities.TRANSPORT_SATELLITE)) add("satellite")
        if (Build.VERSION.SDK_INT >= 36 && caps.hasTransport(NetworkCapabilities.TRANSPORT_THREAD)) add("thread")
    }

    private fun capabilityNames(caps: NetworkCapabilities): List<String> {
        return caps.capabilities.map { capabilityName(it) }
    }

    @SuppressLint("MissingPermission")
    private fun bestWifiInfo(primary: WifiInfo?): WifiInfo? {
        if (primary != null && primary.ssid != WifiManager.UNKNOWN_SSID && primary.bssid != PLACEHOLDER_BSSID) {
            return primary
        }
        return wifi.connectionInfo ?: primary
    }

    private inline fun <reified T> Intent.getParcelableExtraCompat(name: String): T? {
        return if (Build.VERSION.SDK_INT >= 33) {
            getParcelableExtra(name, T::class.java)
        } else {
            @Suppress("DEPRECATION")
            getParcelableExtra(name) as? T
        }
    }

    companion object {
        private const val POLL_INTERVAL_MS = 2_000L
        private const val PLACEHOLDER_BSSID = "02:00:00:00:00:00"
    }
}

private data class WifiEventSnapshot(
    val wifiEnabled: Boolean,
    val wifiState: String,
    val activeNetwork: String,
    val selectedNetwork: String,
    val wifiNetworkCount: Int,
    val connected: Boolean,
    val ssid: String,
    val bssid: String,
    val networkId: Int,
    val securityType: String,
    val supplicantState: String,
    val detailedState: String,
    val rssiDbm: Int,
    val frequencyMhz: Int,
    val linkSpeedMbps: Int,
    val txLinkSpeedMbps: Int,
    val rxLinkSpeedMbps: Int,
    val wifiStandard: String,
    val ipv4Address: String,
    val hiddenSsid: Boolean,
    val passpointFqdn: String,
    val apMldMacAddress: String,
    val apMloLinkId: Int,
    val associatedMloLinks: List<String>,
    val affiliatedMloLinks: List<String>,
    val iface: String,
    val mtu: Int,
    val addresses: List<String>,
    val dnsServers: List<String>,
    val dhcpServer: String,
    val routes: List<String>,
    val domains: String,
    val httpProxy: String,
    val nat64Prefix: String,
    val privateDnsActive: Boolean,
    val privateDnsServerName: String,
    val wakeOnLanSupported: Boolean,
    val validated: Boolean,
    val internet: Boolean,
    val captivePortal: Boolean,
    val metered: Boolean,
    val notRoaming: Boolean,
    val blocked: Boolean,
    val signalStrength: Int,
    val downstreamKbps: Int,
    val upstreamKbps: Int,
    val transports: List<String>,
    val capabilities: List<String>,
    val rawCapabilities: String,
    val rawLinkProperties: String,
    val rawInfo: String,
) {
    val signature: String = listOf(
        wifiEnabled,
        wifiState,
        activeNetwork,
        selectedNetwork,
        wifiNetworkCount,
        connected,
        ssid,
        bssid,
        networkId,
        securityType,
        supplicantState,
        detailedState,
        rssiDbm,
        frequencyMhz,
        linkSpeedMbps,
        txLinkSpeedMbps,
        rxLinkSpeedMbps,
        wifiStandard,
        ipv4Address,
        hiddenSsid,
        passpointFqdn,
        apMldMacAddress,
        apMloLinkId,
        associatedMloLinks.joinToString("|"),
        affiliatedMloLinks.joinToString("|"),
        iface,
        mtu,
        addresses.joinToString("|"),
        dnsServers.joinToString("|"),
        dhcpServer,
        routes.joinToString("|"),
        domains,
        httpProxy,
        nat64Prefix,
        privateDnsActive,
        privateDnsServerName,
        wakeOnLanSupported,
        validated,
        internet,
        captivePortal,
        metered,
        notRoaming,
        blocked,
        signalStrength,
        downstreamKbps,
        upstreamKbps,
        transports.joinToString("|"),
        capabilities.joinToString("|"),
    ).joinToString(";")

    fun fields(prefix: String = ""): List<Pair<String, Any?>> = listOf(
        "${prefix}wifi_enabled" to wifiEnabled,
        "${prefix}wifi_state" to wifiState,
        "${prefix}active_network" to activeNetwork,
        "${prefix}selected_network" to selectedNetwork,
        "${prefix}wifi_network_count" to wifiNetworkCount,
        "${prefix}connected" to connected,
        "${prefix}ssid" to ssid,
        "${prefix}bssid" to bssid,
        "${prefix}network_id" to networkId,
        "${prefix}security_type" to securityType,
        "${prefix}supplicant_state" to supplicantState,
        "${prefix}detailed_state" to detailedState,
        "${prefix}rssi_dbm" to rssiDbm,
        "${prefix}frequency_mhz" to frequencyMhz,
        "${prefix}link_speed_mbps" to linkSpeedMbps,
        "${prefix}tx_link_speed_mbps" to txLinkSpeedMbps,
        "${prefix}rx_link_speed_mbps" to rxLinkSpeedMbps,
        "${prefix}wifi_standard" to wifiStandard,
        "${prefix}ipv4_address" to ipv4Address,
        "${prefix}hidden_ssid" to hiddenSsid,
        "${prefix}passpoint_fqdn" to passpointFqdn,
        "${prefix}ap_mld_mac_address" to apMldMacAddress,
        "${prefix}ap_mlo_link_id" to apMloLinkId,
        "${prefix}associated_mlo_links" to associatedMloLinks,
        "${prefix}affiliated_mlo_links" to affiliatedMloLinks,
        "${prefix}iface" to iface,
        "${prefix}mtu" to mtu,
        "${prefix}addresses" to addresses,
        "${prefix}dns_servers" to dnsServers,
        "${prefix}dhcp_server" to dhcpServer,
        "${prefix}routes" to routes,
        "${prefix}domains" to domains,
        "${prefix}http_proxy" to httpProxy,
        "${prefix}nat64_prefix" to nat64Prefix,
        "${prefix}private_dns_active" to privateDnsActive,
        "${prefix}private_dns_server_name" to privateDnsServerName,
        "${prefix}wake_on_lan_supported" to wakeOnLanSupported,
        "${prefix}validated" to validated,
        "${prefix}internet" to internet,
        "${prefix}captive_portal" to captivePortal,
        "${prefix}metered" to metered,
        "${prefix}not_roaming" to notRoaming,
        "${prefix}blocked" to blocked,
        "${prefix}signal_strength" to signalStrength,
        "${prefix}downstream_kbps" to downstreamKbps,
        "${prefix}upstream_kbps" to upstreamKbps,
        "${prefix}transports" to transports,
        "${prefix}capabilities" to capabilities,
        "${prefix}raw_capabilities" to StructuredLog.preview(rawCapabilities, 1200),
        "${prefix}raw_link_properties" to StructuredLog.preview(rawLinkProperties, 1200),
        "${prefix}raw_wifi_info" to StructuredLog.preview(rawInfo, 1200),
    )

    fun previousFields(): List<Pair<String, Any?>> = fields("previous_")
}
