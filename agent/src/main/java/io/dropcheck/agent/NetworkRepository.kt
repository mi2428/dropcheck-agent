package io.dropcheck.agent

import android.Manifest
import android.annotation.SuppressLint
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.content.pm.PackageManager
import android.net.ConnectivityManager
import android.net.LinkProperties
import android.net.Network
import android.net.NetworkCapabilities
import android.net.NetworkRequest
import android.net.wifi.ScanResult
import android.net.wifi.WifiInfo
import android.net.wifi.WifiManager
import android.os.Build
import io.dropcheck.agent.grpc.DiagnosticField
import io.dropcheck.agent.grpc.IpStatus
import io.dropcheck.agent.grpc.NetworkSelector
import io.dropcheck.agent.grpc.NetworkDiagnostics
import io.dropcheck.agent.grpc.WifiCapabilities
import io.dropcheck.agent.grpc.WifiDiagnostics
import io.dropcheck.agent.grpc.WifiBand
import io.dropcheck.agent.grpc.WifiEvent
import io.dropcheck.agent.grpc.WifiMonitorResult
import io.dropcheck.agent.grpc.WifiScan
import io.dropcheck.agent.grpc.WifiScanDetail
import io.dropcheck.agent.grpc.WifiStatus
import java.util.Collections
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

@Suppress("DEPRECATION")
/**
 * Reads Android connectivity and Wi-Fi framework state and maps it to protobufs.
 *
 * This class is intentionally state-observation heavy and policy-light: it
 * should describe what Android reports, while callers decide whether a command
 * succeeded.
 */
class NetworkRepository(
    private val context: Context,
    private val logger: CommandLogger,
) {
    private val connectivity = context.getSystemService(ConnectivityManager::class.java)
    private val wifi = context.applicationContext.getSystemService(WifiManager::class.java)
    private val mapper = WifiProtoMapper(wifi)

    /**
     * Captures a point-in-time Wi-Fi status snapshot.
     *
     * The selected network is the active Wi-Fi network when available, otherwise
     * the first Wi-Fi network Android reports.
     */
    @SuppressLint("MissingPermission")
    fun wifiStatus(): WifiStatus {
        val active = connectivity.activeNetwork
        val wifiNetworks = connectivity.allNetworks.filter { network ->
            val caps = connectivity.getNetworkCapabilities(network) ?: return@filter false
            caps.hasTransport(NetworkCapabilities.TRANSPORT_WIFI)
        }
        val selected = wifiNetworks.firstOrNull { it == active } ?: wifiNetworks.firstOrNull()
        logger.debug("wifiStatus active=${active ?: "none"} wifi_networks=${wifiNetworks.size} selected=${selected ?: "none"} manager_enabled=${wifi.isWifiEnabled} manager_state=${wifiStateName(wifi.wifiState)}")
        wifiNetworks.forEachIndexed { index, network ->
            logger.debug("wifiStatus wifi_candidate[$index] network=$network ${describeNetwork(network)}")
        }
        val bestInfo = bestWifiInfo(selected?.let {
            connectivity.getNetworkCapabilities(it)?.transportInfo as? WifiInfo
        })

        val builder = WifiStatus.newBuilder()
            .setEnabled(wifi.isWifiEnabled)
            .setState(wifiStateName(wifi.wifiState))
            .setActiveNetwork(active?.toString().orEmpty())
            .setWifiNetworkCount(wifiNetworks.size)
            .addAllPermissions(permissionSummary())

        if (bestInfo != null) {
            builder.connection = mapper.wifiConnection(bestInfo)
        }
        if (selected != null) {
            builder.ipStatus = ipStatus(selected)
        }
        if (bestInfo == null) {
            logger.debug("wifiStatus no usable WifiInfo selected_network=${selected ?: "none"}")
        }
        return builder.build()
    }

    /** Builds a broad diagnostic bundle: current status, capabilities, networks, and scan cache. */
    @SuppressLint("MissingPermission")
    fun wifiDiagnostics(): WifiDiagnostics {
        logger.info("wifi diagnostics requested")
        val active = connectivity.activeNetwork
        val diagnostics = WifiDiagnostics.newBuilder()
            .setStatus(wifiStatus())
            .setCapabilities(wifiCapabilities())
            .setScan(wifiScan())

        connectivity.allNetworks.forEach { network ->
            val networkStatus = NetworkDiagnostics.newBuilder()
                .setNetworkId(network.toString())
                .setActive(network == active)
                .setIpStatus(ipStatus(network))
                .build()
            diagnostics.addNetworks(networkStatus)
        }
        val built = diagnostics.build()
        logger.debug("wifi diagnostics collected networks=${built.networksCount} scan_results=${built.scan.resultsCount} capability_fields=${built.capabilities.fieldsCount}")
        return built
    }

    /**
     * Returns Android's cached scan results filtered by requested band.
     *
     * This does not start a scan; use [wifiFreshScan] when the controller wants
     * to request a new framework scan cycle.
     */
    @SuppressLint("MissingPermission")
    fun wifiScan(band: WifiBand = WifiBand.WIFI_BAND_ALL): WifiScan {
        val builder = WifiScan.newBuilder()
        addDiagnosticField(builder, "requested_band") { wifiBandName(band) }
        addDiagnosticField(builder, "wifi_enabled") { wifi.isWifiEnabled }
        addDiagnosticField(builder, "wifi_state") { wifiStateName(wifi.wifiState) }
        addDiagnosticField(builder, "scan_always_available") { wifi.isScanAlwaysAvailable }
        addDiagnosticField(builder, "scan_throttle_enabled") { wifi.isScanThrottleEnabled }

        val allResults = runCatching { wifi.scanResults }
            .onFailure { builder.addErrors("get_scan_results=${errorSummary(it)}") }
            .getOrDefault(emptyList())
        val results = allResults
            .filter { scanResultMatchesBand(it, band) }
            .sortedWith(compareByDescending<ScanResult> { it.level }.thenBy { it.SSID }.thenBy { it.BSSID })

        builder.addFields(diagnosticField("scan_result_count", results.size))
        builder.addFields(diagnosticField("scan_result_total_count", allResults.size))
        results.forEach { builder.addResults(mapper.scanResult(it)) }
        logger.debug("wifi scan collected requested_band=${wifiBandName(band)} results=${results.size} total=${allResults.size} errors=${builder.errorsList.joinToString(",")}")
        return builder.build()
    }

    /**
     * Requests a framework scan and waits for the scan-results broadcast.
     *
     * Android may throttle scans or return false immediately; those conditions
     * are encoded as diagnostic errors for command-level classification.
     */
    @SuppressLint("MissingPermission")
    fun wifiFreshScan(band: WifiBand = WifiBand.WIFI_BAND_ALL, timeoutMs: Int = 10000): WifiScan {
        val startedAt = System.currentTimeMillis()
        val latch = CountDownLatch(1)
        var broadcastReceived = false
        var resultsUpdated = false
        val receiver = object : BroadcastReceiver() {
            override fun onReceive(context: Context, intent: Intent) {
                if (intent.action == WifiManager.SCAN_RESULTS_AVAILABLE_ACTION) {
                    broadcastReceived = true
                    resultsUpdated = intent.getBooleanExtra(WifiManager.EXTRA_RESULTS_UPDATED, false)
                    logger.debug("wifi fresh scan broadcast received updated=$resultsUpdated")
                    latch.countDown()
                }
            }
        }
        val filter = IntentFilter(WifiManager.SCAN_RESULTS_AVAILABLE_ACTION)
        val appContext = context.applicationContext
        runCatching {
            if (Build.VERSION.SDK_INT >= 33) {
                appContext.registerReceiver(receiver, filter, Context.RECEIVER_NOT_EXPORTED)
            } else {
                @Suppress("DEPRECATION")
                appContext.registerReceiver(receiver, filter)
            }
        }.onFailure {
            logger.warn("wifi fresh scan register receiver failed error=$it")
            return wifiScan(band).toBuilder()
                .addFields(diagnosticField("fresh_scan_receiver_registered", false))
                .addErrors("register_receiver=${errorSummary(it)}")
                .build()
        }
        val scanStarted = runCatching { wifi.startScan() }
            .onFailure { logger.warn("wifi fresh scan startScan failed error=$it") }
            .getOrDefault(false)
        logger.info("wifi fresh scan requested band=${wifiBandName(band)} start_scan=$scanStarted timeout_ms=$timeoutMs")
        val waited = if (scanStarted) {
            runCatching { latch.await(timeoutMs.toLong(), TimeUnit.MILLISECONDS) }.getOrDefault(false)
        } else {
            false
        }
        runCatching { appContext.unregisterReceiver(receiver) }
            .onFailure { logger.warn("wifi fresh scan unregister receiver failed error=$it") }
        val scan = wifiScan(band).toBuilder()
            .addFields(diagnosticField("fresh_scan_receiver_registered", true))
            .addFields(diagnosticField("fresh_scan_start_scan", scanStarted))
            .addFields(diagnosticField("fresh_scan_broadcast_received", broadcastReceived))
            .addFields(diagnosticField("fresh_scan_results_updated", resultsUpdated))
            .addFields(diagnosticField("fresh_scan_wait_completed", waited))
            .addFields(diagnosticField("fresh_scan_elapsed_ms", System.currentTimeMillis() - startedAt))
        if (!scanStarted) {
            scan.addErrors("start_scan=false")
        }
        if (scanStarted && !broadcastReceived) {
            scan.addErrors("scan_broadcast_timeout=${timeoutMs}ms")
        }
        return scan.build()
    }

    /** Returns scan entries matching an SSID or BSSID target after band filtering. */
    @SuppressLint("MissingPermission")
    fun wifiScanDetail(target: String, band: WifiBand = WifiBand.WIFI_BAND_ALL): WifiScanDetail {
        val builder = WifiScanDetail.newBuilder()
            .setTarget(target)
            .addFields(diagnosticField("requested_band", wifiBandName(band)))
            .addFields(diagnosticField("target", target))
        val allResults = runCatching { wifi.scanResults }
            .onFailure { builder.addErrors("get_scan_results=${errorSummary(it)}") }
            .getOrDefault(emptyList())
        val results = allResults
            .filter { scanResultMatchesBand(it, band) }
            .filter { target.isBlank() || it.SSID == target || it.BSSID.equals(target, ignoreCase = true) }
            .sortedWith(compareByDescending<ScanResult> { it.level }.thenBy { it.SSID }.thenBy { it.BSSID })
        builder.addFields(diagnosticField("scan_result_total_count", allResults.size))
        builder.addFields(diagnosticField("scan_result_match_count", results.size))
        results.forEach { builder.addResults(mapper.scanResult(it)) }
        if (results.isEmpty()) {
            builder.addErrors("scan_detail_no_match")
        }
        logger.debug("wifi scan detail target=$target band=${wifiBandName(band)} matches=${results.size} total=${allResults.size}")
        return builder.build()
    }

    /**
     * Combines ConnectivityManager callbacks with periodic status signatures.
     *
     * Callback delivery can be sparse on some builds, so polling remains in the
     * monitor to catch state changes that callbacks miss.
     */
    fun wifiMonitor(durationMs: Int, intervalMs: Int): WifiMonitorResult {
        val events = Collections.synchronizedList(mutableListOf<WifiEvent>())
        val builder = WifiMonitorResult.newBuilder()
        val boundedDuration = durationMs.coerceIn(1, 120000)
        val boundedInterval = intervalMs.coerceIn(100, 60000)
        val startedAt = System.currentTimeMillis()
        val deadline = startedAt + boundedDuration
        fun addEvent(type: String, message: String, network: Network? = null, status: WifiStatus? = null) {
            val event = WifiEvent.newBuilder()
                .setUnixTimeMs(System.currentTimeMillis())
                .setType(type)
                .setMessage(message)
                .addFields(diagnosticField("elapsed_ms", System.currentTimeMillis() - startedAt))
            if (status != null) {
                event.status = status
            }
            if (network != null) {
                event.addFields(diagnosticField("network", network.toString()))
                event.addFields(diagnosticField("network_state", describeNetwork(network)))
            }
            events += event.build()
            logger.debug("wifi monitor event type=$type message=$message network=${network ?: "none"}")
        }
        val callback = object : ConnectivityManager.NetworkCallback(
            ConnectivityManager.NetworkCallback.FLAG_INCLUDE_LOCATION_INFO,
        ) {
            override fun onAvailable(network: Network) {
                addEvent("network_available", "wifi network available", network)
            }

            override fun onLost(network: Network) {
                addEvent("network_lost", "wifi network lost", network)
            }

            override fun onCapabilitiesChanged(network: Network, networkCapabilities: NetworkCapabilities) {
                addEvent("capabilities_changed", "wifi capabilities changed", network)
            }

            override fun onLinkPropertiesChanged(network: Network, linkProperties: LinkProperties) {
                addEvent("link_properties_changed", "wifi link properties changed", network)
            }
        }
        val request = NetworkRequest.Builder()
            .addTransportType(NetworkCapabilities.TRANSPORT_WIFI)
            .build()
        val registered = runCatching { connectivity.registerNetworkCallback(request, callback) }
            .onFailure { builder.addErrors("register_network_callback=${errorSummary(it)}") }
            .isSuccess
        addEvent("monitor_start", "wifi monitor started")
        var lastSignature = ""
        logger.info("wifi monitor begin duration_ms=$boundedDuration interval_ms=$boundedInterval registered=$registered")
        while (System.currentTimeMillis() < deadline) {
            val status = runCatching { wifiStatus() }.getOrNull()
            if (status != null) {
                val signature = statusSignature(status)
                if (signature != lastSignature) {
                    lastSignature = signature
                    val event = WifiEvent.newBuilder()
                        .setUnixTimeMs(System.currentTimeMillis())
                        .setType("status_changed")
                        .setMessage(signature)
                        .setStatus(status)
                        .addFields(diagnosticField("signature", signature))
                        .addFields(diagnosticField("elapsed_ms", System.currentTimeMillis() - startedAt))
                        .build()
                    events += event
                    logger.debug("wifi monitor status_changed signature=$signature")
                }
            }
            val remaining = deadline - System.currentTimeMillis()
            if (remaining <= 0) break
            Thread.sleep(minOf(boundedInterval.toLong(), remaining))
        }
        if (registered) {
            runCatching { connectivity.unregisterNetworkCallback(callback) }
                .onFailure { builder.addErrors("unregister_network_callback=${errorSummary(it)}") }
        }
        addEvent("monitor_end", "wifi monitor ended")
        synchronized(events) {
            builder.addAllEvents(events)
        }
        val built = builder.build()
        logger.debug("wifi monitor end events=${built.eventsCount} errors=${built.errorsCount}")
        return built
    }

    /** Reports Wi-Fi feature/capability flags exposed by WifiManager on this device. */
    fun wifiCapabilities(): WifiCapabilities {
        val builder = WifiCapabilities.newBuilder()
        addCapabilityField(builder, "wifi_enabled") { wifi.isWifiEnabled }
        addCapabilityField(builder, "wifi_state") { wifiStateName(wifi.wifiState) }
        addCapabilityField(builder, "scan_always_available") { wifi.isScanAlwaysAvailable }
        addCapabilityField(builder, "scan_throttle_enabled") { wifi.isScanThrottleEnabled }
        addCapabilityField(builder, "max_signal_level") { wifi.maxSignalLevel }
        if (Build.VERSION.SDK_INT >= 34) {
            addCapabilityField(builder, "max_channels_per_network_specifier_request") { wifi.maxNumberOfChannelsPerNetworkSpecifierRequest }
        }
        addCapabilityField(builder, "max_network_suggestions_per_app") { wifi.maxNumberOfNetworkSuggestionsPerApp }
        if (Build.VERSION.SDK_INT >= 33) {
            addCapabilityField(builder, "sta_concurrency_for_multi_internet_mode") { multiInternetModeName(wifi.staConcurrencyForMultiInternetMode) }
        }
        addCapabilityField(builder, "ping_supplicant") { wifi.pingSupplicant() }
        if (Build.VERSION.SDK_INT >= 33) {
            addCapabilityField(builder, "wifi_passpoint_enabled") { wifi.isWifiPasspointEnabled }
        }

        addBandSupport(builder, "2.4GHz") { wifi.is24GHzBandSupported }
        addBandSupport(builder, "5GHz") { wifi.is5GHzBandSupported }
        addBandSupport(builder, "6GHz") { wifi.is6GHzBandSupported }
        addBandSupport(builder, "60GHz") { wifi.is60GHzBandSupported }

        addStandardSupport(builder, "legacy") { wifi.isWifiStandardSupported(ScanResult.WIFI_STANDARD_LEGACY) }
        addStandardSupport(builder, "802.11n") { wifi.isWifiStandardSupported(ScanResult.WIFI_STANDARD_11N) }
        addStandardSupport(builder, "802.11ac") { wifi.isWifiStandardSupported(ScanResult.WIFI_STANDARD_11AC) }
        addStandardSupport(builder, "802.11ax") { wifi.isWifiStandardSupported(ScanResult.WIFI_STANDARD_11AX) }
        addStandardSupport(builder, "802.11ad") { wifi.isWifiStandardSupported(ScanResult.WIFI_STANDARD_11AD) }
        if (Build.VERSION.SDK_INT >= 33) {
            addStandardSupport(builder, "802.11be") { wifi.isWifiStandardSupported(ScanResult.WIFI_STANDARD_11BE) }
        }

        addSecuritySupport(builder, "owe") { wifi.isEnhancedOpenSupported }
        if (Build.VERSION.SDK_INT >= 35) {
            addSecuritySupport(builder, "wpa_personal") { wifi.isWpaPersonalSupported }
        }
        addSecuritySupport(builder, "wpa3_sae") { wifi.isWpa3SaeSupported }
        addSecuritySupport(builder, "wpa3_sae_h2e") { wifi.isWpa3SaeH2eSupported }
        addSecuritySupport(builder, "wpa3_sae_public_key") { wifi.isWpa3SaePublicKeySupported }
        addSecuritySupport(builder, "wpa3_suite_b") { wifi.isWpa3SuiteBSupported }
        addSecuritySupport(builder, "wapi") { wifi.isWapiSupported }
        if (Build.VERSION.SDK_INT >= 35) {
            addSecuritySupport(builder, "wep") { wifi.isWepSupported }
        }

        if (Build.VERSION.SDK_INT >= 35) {
            addFeatureSupport(builder, "aggressive_roaming") { wifi.isAggressiveRoamingModeSupported }
        }
        addFeatureSupport(builder, "auto_wakeup") { wifi.isAutoWakeupEnabled }
        addFeatureSupport(builder, "bridged_ap_concurrency") { wifi.isBridgedApConcurrencySupported }
        if (Build.VERSION.SDK_INT >= 35) {
            addFeatureSupport(builder, "d2d_when_infra_sta_disabled") { wifi.isD2dSupportedWhenInfraStaDisabled }
        }
        addFeatureSupport(builder, "decorated_identity") { wifi.isDecoratedIdentitySupported }
        addFeatureSupport(builder, "device_to_ap_rtt") { wifi.isDeviceToApRttSupported }
        if (Build.VERSION.SDK_INT >= 34) {
            addFeatureSupport(builder, "dual_band_simultaneous") { wifi.isDualBandSimultaneousSupported }
        }
        if (Build.VERSION.SDK_INT >= 33) {
            addFeatureSupport(builder, "easy_connect_dpp_akm") { wifi.isEasyConnectDppAkmSupported }
        }
        addFeatureSupport(builder, "easy_connect_enrollee_responder") { wifi.isEasyConnectEnrolleeResponderModeSupported }
        addFeatureSupport(builder, "easy_connect") { wifi.isEasyConnectSupported }
        addFeatureSupport(builder, "enhanced_power_reporting") { wifi.isEnhancedPowerReportingSupported }
        addFeatureSupport(builder, "make_before_break_wifi_switching") { wifi.isMakeBeforeBreakWifiSwitchingSupported }
        addFeatureSupport(builder, "p2p") { wifi.isP2pSupported }
        addFeatureSupport(builder, "passpoint_terms_and_conditions") { wifi.isPasspointTermsAndConditionsSupported }
        addFeatureSupport(builder, "preferred_network_offload") { wifi.isPreferredNetworkOffloadSupported }
        addFeatureSupport(builder, "sta_ap_concurrency") { wifi.isStaApConcurrencySupported }
        addFeatureSupport(builder, "sta_bridged_ap_concurrency") { wifi.isStaBridgedApConcurrencySupported }
        addFeatureSupport(builder, "sta_concurrency_local_only") { wifi.isStaConcurrencyForLocalOnlyConnectionsSupported }
        if (Build.VERSION.SDK_INT >= 33) {
            addFeatureSupport(builder, "sta_concurrency_multi_internet") { wifi.isStaConcurrencyForMultiInternetSupported }
        }
        addFeatureSupport(builder, "tdls") { wifi.isTdlsSupported }
        if (Build.VERSION.SDK_INT >= 34) {
            addFeatureSupport(builder, "tid_to_link_mapping_negotiation") { wifi.isTidToLinkMappingNegotiationSupported }
            addFeatureSupport(builder, "tls_minimum_version") { wifi.isTlsMinimumVersionSupported }
            addFeatureSupport(builder, "tls_v13") { wifi.isTlsV13Supported }
        }
        if (Build.VERSION.SDK_INT >= 33) {
            addFeatureSupport(builder, "trust_on_first_use") { wifi.isTrustOnFirstUseSupported }
        }
        addFeatureSupport(builder, "wifi_display_r2") { wifi.isWifiDisplayR2Supported }

        val built = builder.build()
        logger.debug("wifi capabilities collected fields=${built.fieldsCount} bands=${built.supportedBandsList.joinToString(",")} standards=${built.supportedStandardsList.joinToString(",")} features=${built.supportedFeaturesCount}/${built.supportedFeaturesCount + built.unsupportedFeaturesCount} errors=${built.errorsCount}")
        return built
    }

    /** Selects a Wi-Fi network and maps its IP/link status, or returns null if no match exists. */
    fun ipStatusFor(selector: NetworkSelector, timeoutMs: Long): IpStatus? {
        val network = waitForNetwork(selector, timeoutMs) ?: return null
        return ipStatus(network)
    }

    /**
     * Waits for a Wi-Fi [Network] matching the selector.
     *
     * A zero timeout performs exactly one selection attempt, which is useful for
     * immediate probe commands.
     */
    fun waitForNetwork(selector: NetworkSelector, timeoutMs: Long): Network? {
        val deadline = System.currentTimeMillis() + timeoutMs
        var first = true
        var lastDump = 0L
        logger.debug("waitForNetwork begin selector=${selectorSummary(selector)} timeout_ms=$timeoutMs deadline=$deadline")
        while (first || System.currentTimeMillis() < deadline) {
            first = false
            val now = System.currentTimeMillis()
            selectNetwork(selector, dumpCandidates = now - lastDump > 2000L)?.let {
                logger.debug("waitForNetwork selected network=$it selector=${selectorSummary(selector)}")
                return it
            }
            if (now - lastDump > 2000L) {
                lastDump = now
            }
            if (timeoutMs <= 0) break
            Thread.sleep(500)
        }
        logger.debug("waitForNetwork timeout selector=${selectorSummary(selector)} timeout_ms=$timeoutMs")
        return null
    }

    /**
     * Temporarily binds native process probes to the selected Android Network.
     *
     * ConnectivityManager-backed Java APIs can use Network directly, but child
     * processes such as ping inherit the app process default network.
     */
    fun <T> withBoundNetwork(network: Network, block: () -> T): T {
        val previous = connectivity.boundNetworkForProcess
        val bound = connectivity.bindProcessToNetwork(network)
        if (!bound) {
            logger.warn("bindProcessToNetwork failed network=$network")
        }
        return try {
            block()
        } finally {
            connectivity.bindProcessToNetwork(previous)
        }
    }

    /** Returns the best currently matching Wi-Fi network without waiting. */
    @SuppressLint("MissingPermission")
    fun selectNetwork(selector: NetworkSelector): Network? {
        return selectNetwork(selector, dumpCandidates = true)
    }

    @SuppressLint("MissingPermission")
    private fun selectNetwork(selector: NetworkSelector, dumpCandidates: Boolean): Network? {
        val active = connectivity.activeNetwork
        if (dumpCandidates) {
            logger.debug("selectNetwork selector=${selectorSummary(selector)} active=${active ?: "none"} all_networks=${connectivity.allNetworks.size}")
        }
        if (active != null && matches(active, selector)) {
            if (dumpCandidates) {
                logger.debug("selectNetwork chose active network=$active ${describeNetwork(active)}")
            }
            return active
        }
        var selected: Network? = null
        for (network in connectivity.allNetworks) {
            val match = matches(network, selector)
            if (dumpCandidates) {
                logger.debug("selectNetwork candidate network=$network match=$match reject_reason=${rejectReason(network, selector)} ${describeNetwork(network)}")
            }
            if (match && selected == null) {
                selected = network
            }
        }
        if (dumpCandidates && selected == null) {
            logger.debug("selectNetwork no candidate matched selector=${selectorSummary(selector)}")
        }
        return selected
    }

    /** Maps Android NetworkCapabilities and LinkProperties into the wire IP status message. */
    fun ipStatus(network: Network): IpStatus {
        val caps = connectivity.getNetworkCapabilities(network)
        val link = connectivity.getLinkProperties(network)
        val builder = IpStatus.newBuilder()
            .setNetworkId(network.toString())

        if (caps != null) {
            builder.addAllTransports(transports(caps))
            builder.validated = caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_VALIDATED)
            builder.internet = caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
            builder.addAllCapabilities(caps.capabilities.map { capabilityName(it) })
            builder.downstreamKbps = caps.linkDownstreamBandwidthKbps
            builder.upstreamKbps = caps.linkUpstreamBandwidthKbps
            builder.signalStrength = caps.signalStrength
            builder.networkSpecifier = caps.networkSpecifier?.toString().orEmpty()
            builder.ownerUid = runCatching { caps.ownerUid }.getOrDefault(-1)
            if (Build.VERSION.SDK_INT >= 33) {
                builder.addAllEnterpriseIds(caps.enterpriseIds.map { enterpriseIdName(it) })
            }
            if (Build.VERSION.SDK_INT >= 35) {
                builder.addAllSubscriptionIds(caps.subscriptionIds.map { it.toInt() })
            }
            builder.rawCapabilities = caps.toString()
            val info = if (caps.hasTransport(NetworkCapabilities.TRANSPORT_WIFI)) {
                bestWifiInfo(caps.transportInfo as? WifiInfo)
            } else {
                null
            }
            if (info != null) {
                builder.wifi = mapper.wifiConnection(info)
            }
        }
        if (link != null) {
            builder.interfaceName = link.interfaceName.orEmpty()
            builder.mtu = link.mtu
            builder.addAllAddresses(link.linkAddresses.map { it.toString() })
            builder.addAllDnsServers(link.dnsServers.mapNotNull { it.hostAddress })
            builder.dhcpServer = link.dhcpServerAddress?.hostAddress.orEmpty()
            builder.addAllRoutes(link.routes.map { it.toString() })
            builder.domains = link.domains.orEmpty()
            builder.httpProxy = link.httpProxy?.toString().orEmpty()
            builder.nat64Prefix = link.nat64Prefix?.toString().orEmpty()
            builder.privateDnsActive = link.isPrivateDnsActive
            builder.privateDnsServerName = link.privateDnsServerName.orEmpty()
            builder.wakeOnLanSupported = link.isWakeOnLanSupported
            builder.rawLinkProperties = link.toString()
        }
        val built = builder.build()
        logger.debug("ipStatus network=${built.networkId} transports=${built.transportsList.joinToString(",")} capabilities=${built.capabilitiesList.joinToString(",")} iface=${built.interfaceName.ifBlank { "none" }} mtu=${built.mtu} addresses=${built.addressesList.joinToString(",")} dns=${built.dnsServersList.joinToString(",")} dhcp=${built.dhcpServer.ifBlank { "none" }} routes=${built.routesCount} validated=${built.validated} internet=${built.internet} down_kbps=${built.downstreamKbps} up_kbps=${built.upstreamKbps} signal=${built.signalStrength}")
        if (built.hasWifi()) {
            logger.debug("ipStatus wifi network=${built.networkId} ssid=${built.wifi.ssid} bssid=${built.wifi.bssid} rssi=${built.wifi.rssiDbm} freq=${built.wifi.frequencyMhz} link=${built.wifi.linkSpeedMbps} security=${built.wifi.securityType}")
        }
        return built
    }

    private fun matches(network: Network, selector: NetworkSelector): Boolean {
        val caps = connectivity.getNetworkCapabilities(network) ?: return false
        return caps.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) && ssidMatches(caps, selector.ssid)
    }

    private fun rejectReason(network: Network, selector: NetworkSelector): String {
        val caps = connectivity.getNetworkCapabilities(network) ?: return "no_capabilities"
        if (!caps.hasTransport(NetworkCapabilities.TRANSPORT_WIFI)) {
            return "not_wifi"
        }
        if (!ssidMatches(caps, selector.ssid)) {
            val actual = bestWifiInfo(caps.transportInfo as? WifiInfo)?.ssid?.trim('"').orEmpty()
            return "ssid_mismatch actual=${actual.ifBlank { "unknown" }}"
        }
        return "none"
    }

    private fun ssidMatches(caps: NetworkCapabilities, expected: String): Boolean {
        if (expected.isBlank()) return true
        val actual = bestWifiInfo(caps.transportInfo as? WifiInfo)?.ssid?.trim('"') ?: return false
        if (actual == WifiManager.UNKNOWN_SSID) return false
        return actual == expected
    }

    private fun transports(caps: NetworkCapabilities): List<String> = buildList {
        if (caps.hasTransport(NetworkCapabilities.TRANSPORT_WIFI)) add("wifi")
        if (caps.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR)) add("cellular")
        if (caps.hasTransport(NetworkCapabilities.TRANSPORT_BLUETOOTH)) add("bluetooth")
        if (caps.hasTransport(NetworkCapabilities.TRANSPORT_LOWPAN)) add("lowpan")
        if (caps.hasTransport(NetworkCapabilities.TRANSPORT_VPN)) add("vpn")
        if (caps.hasTransport(NetworkCapabilities.TRANSPORT_WIFI_AWARE)) add("wifi_aware")
        if (Build.VERSION.SDK_INT >= 35 && caps.hasTransport(NetworkCapabilities.TRANSPORT_SATELLITE)) add("satellite")
        if (Build.VERSION.SDK_INT >= 36 && caps.hasTransport(NetworkCapabilities.TRANSPORT_THREAD)) add("thread")
    }

    private fun selectorSummary(selector: NetworkSelector): String {
        return "ssid=${selector.ssid.ifBlank { "*" }}"
    }

    private fun describeNetwork(network: Network): String {
        val caps = connectivity.getNetworkCapabilities(network)
        val link = connectivity.getLinkProperties(network)
        val transports = caps?.let { transports(it).joinToString(",") } ?: "none"
        val wifiInfo = if (caps?.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) == true) {
            caps.transportInfo as? WifiInfo
        } else {
            null
        }
        val ssid = if (wifiInfo != null) bestWifiInfo(wifiInfo)?.ssid?.trim('"').orEmpty() else ""
        val addresses = link?.linkAddresses?.joinToString(",") { it.toString() }.orEmpty()
        val dns = link?.dnsServers?.joinToString(",") { it.hostAddress.orEmpty() }.orEmpty()
        val routes = link?.routes?.joinToString(" | ") { it.toString() }.orEmpty()
        return "transports=$transports validated=${caps?.hasCapability(NetworkCapabilities.NET_CAPABILITY_VALIDATED)} internet=${caps?.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)} captive=${caps?.hasCapability(NetworkCapabilities.NET_CAPABILITY_CAPTIVE_PORTAL)} metered=${caps?.hasCapability(NetworkCapabilities.NET_CAPABILITY_NOT_METERED)?.not()} iface=${link?.interfaceName.orEmpty()} mtu=${link?.mtu ?: 0} ssid=${ssid.ifBlank { "none" }} addresses=${addresses.ifBlank { "none" }} dns=${dns.ifBlank { "none" }} routes=${routes.ifBlank { "none" }}"
    }

    private fun statusSignature(status: WifiStatus): String {
        val connection = if (status.hasConnection()) status.connection else null
        val ip = if (status.hasIpStatus()) status.ipStatus else null
        return listOf(
            "enabled=${status.enabled}",
            "state=${status.state}",
            "ssid=${connection?.ssid.orEmpty()}",
            "bssid=${connection?.bssid.orEmpty()}",
            "security=${connection?.securityType.orEmpty()}",
            "freq=${connection?.frequencyMhz ?: 0}",
            "rssi=${connection?.rssiDbm ?: 0}",
            "network=${ip?.networkId.orEmpty()}",
            "iface=${ip?.interfaceName.orEmpty()}",
            "validated=${ip?.validated ?: false}",
            "internet=${ip?.internet ?: false}",
            "addresses=${ip?.addressesList?.joinToString(",").orEmpty()}",
        ).joinToString(" ")
    }

    /**
     * Falls back to WifiManager.connectionInfo when NetworkCapabilities contains
     * redacted placeholder Wi-Fi info.
     */
    @SuppressLint("MissingPermission")
    private fun bestWifiInfo(primary: WifiInfo?): WifiInfo? {
        if (primary != null && primary.ssid != WifiManager.UNKNOWN_SSID && primary.bssid != "02:00:00:00:00:00") {
            return primary
        }
        return wifi.connectionInfo ?: primary
    }

    private fun permissionSummary(): List<String> {
        val names = buildList {
            add(Manifest.permission.ACCESS_COARSE_LOCATION)
            add(Manifest.permission.ACCESS_FINE_LOCATION)
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
                add(Manifest.permission.ACCESS_BACKGROUND_LOCATION)
            }
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
                add(Manifest.permission.NEARBY_WIFI_DEVICES)
            }
        }
        return names.map { permission ->
            val shortName = permission.substringAfterLast(".")
            val granted = context.checkSelfPermission(permission) == PackageManager.PERMISSION_GRANTED
            "$shortName=${if (granted) "granted" else "denied"}"
        }
    }

    private fun addDiagnosticField(builder: WifiScan.Builder, key: String, value: () -> Any?) {
        runCatching { builder.addFields(diagnosticField(key, value())) }
            .onFailure { builder.addErrors("$key=${errorSummary(it)}") }
    }

    private fun addCapabilityField(builder: WifiCapabilities.Builder, key: String, value: () -> Any?) {
        runCatching { builder.addFields(diagnosticField(key, value())) }
            .onFailure { builder.addErrors("$key=${errorSummary(it)}") }
    }

    private fun addBandSupport(builder: WifiCapabilities.Builder, name: String, supported: () -> Boolean) {
        addSupport(name, supported, builder::addSupportedBands, builder::addUnsupportedBands, builder::addErrors)
    }

    private fun addStandardSupport(builder: WifiCapabilities.Builder, name: String, supported: () -> Boolean) {
        addSupport(name, supported, builder::addSupportedStandards, builder::addUnsupportedStandards, builder::addErrors)
    }

    private fun addSecuritySupport(builder: WifiCapabilities.Builder, name: String, supported: () -> Boolean) {
        addSupport(name, supported, builder::addSupportedSecurityModes, builder::addUnsupportedSecurityModes, builder::addErrors)
    }

    private fun addFeatureSupport(builder: WifiCapabilities.Builder, name: String, supported: () -> Boolean) {
        addSupport(name, supported, builder::addSupportedFeatures, builder::addUnsupportedFeatures, builder::addErrors)
    }

    private fun addSupport(
        name: String,
        supported: () -> Boolean,
        addSupported: (String) -> Unit,
        addUnsupported: (String) -> Unit,
        addError: (String) -> Unit,
    ) {
        runCatching { supported() }
            .onSuccess { if (it) addSupported(name) else addUnsupported(name) }
            .onFailure { addError("$name=${errorSummary(it)}") }
    }

    private fun diagnosticField(key: String, value: Any?): DiagnosticField {
        return DiagnosticField.newBuilder()
            .setKey(key)
            .setValue(value?.toString().orEmpty())
            .build()
    }

    private fun errorSummary(error: Throwable): String {
        return "${error.javaClass.simpleName}:${error.message.orEmpty()}"
    }
}
