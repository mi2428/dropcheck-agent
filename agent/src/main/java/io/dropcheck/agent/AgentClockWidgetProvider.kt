package io.dropcheck.agent

import android.annotation.SuppressLint
import android.app.AlarmManager
import android.app.PendingIntent
import android.appwidget.AppWidgetManager
import android.appwidget.AppWidgetProvider
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.location.Location
import android.location.LocationManager
import android.net.ConnectivityManager
import android.net.LinkProperties
import android.net.Network
import android.net.NetworkCapabilities
import android.net.NetworkRequest
import android.net.wifi.WifiInfo
import android.net.wifi.WifiManager
import android.os.Build
import android.os.Bundle
import android.os.SystemClock
import android.provider.Settings
import android.util.TypedValue
import android.widget.RemoteViews
import java.net.Inet4Address
import java.net.Inet6Address
import java.util.Locale
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicLong
import kotlin.math.roundToInt

/**
 * App widget provider for the compact clock, Wi-Fi status, and network address display.
 *
 * The widget refreshes from broadcasts, connectivity callbacks, and bounded
 * follow-up alarms so transient Android Wi-Fi state changes settle on screen.
 */
class AgentClockWidgetProvider : AppWidgetProvider() {
    override fun onUpdate(context: Context, appWidgetManager: AppWidgetManager, appWidgetIds: IntArray) {
        schedulePeriodicUpdate(context)
        registerWidgetNetworkCallback(context)
        AgentService.requestWidgetObserver(context)
        updateWidgets(context, appWidgetManager, appWidgetIds)
    }

    override fun onEnabled(context: Context) {
        super.onEnabled(context)
        schedulePeriodicUpdate(context)
        registerWidgetNetworkCallback(context)
        AgentService.requestWidgetObserver(context)
        updateAll(context)
    }

    override fun onDisabled(context: Context) {
        unregisterWidgetNetworkCallback(context)
        cancelPeriodicUpdate(context)
        cancelEventFollowUpUpdates(context)
        AgentService.requestWidgetObserverStop(context)
        super.onDisabled(context)
    }

    override fun onAppWidgetOptionsChanged(
        context: Context,
        appWidgetManager: AppWidgetManager,
        appWidgetId: Int,
        newOptions: Bundle,
    ) {
        updateWidgets(context, appWidgetManager, intArrayOf(appWidgetId))
    }

    @Suppress("DEPRECATION")
    override fun onReceive(context: Context, intent: Intent) {
        super.onReceive(context, intent)
        when (intent.action) {
            Intent.ACTION_MY_PACKAGE_REPLACED -> {
                registerWidgetNetworkCallback(context)
                schedulePeriodicUpdate(context)
                AgentService.requestWidgetObserver(context)
                requestUpdate(context)
            }
            ACTION_PERIODIC_UPDATE -> {
                registerWidgetNetworkCallback(context)
                schedulePeriodicUpdate(context)
                requestUpdate(context)
            }
            ACTION_NETWORK_CALLBACK_UPDATE -> {
                requestUpdate(context)
                schedulePeriodicUpdate(context)
                scheduleEventFollowUpUpdates(context)
            }
            ACTION_EVENT_FOLLOW_UP_UPDATE -> requestUpdate(context)
            in WIFI_EVENT_ACTIONS -> {
                requestUpdate(context)
                scheduleEventFollowUpUpdates(context)
            }
        }
    }

    companion object {
        private val updatePending = AtomicBoolean(false)
        private val lastUpdateElapsedMs = AtomicLong(0)
        private val updateExecutor = Executors.newSingleThreadScheduledExecutor { task ->
            Thread(task, "dropcheck-clock-widget-update")
        }

        fun requestUpdate(context: Context) {
            val appContext = context.applicationContext
            if (!updatePending.compareAndSet(false, true)) return
            val elapsedMs = SystemClock.elapsedRealtime()
            val nextAllowedMs = lastUpdateElapsedMs.get() + MIN_UPDATE_INTERVAL_MS
            val delayMs = maxOf(UPDATE_DEBOUNCE_MS, nextAllowedMs - elapsedMs)
            updateExecutor.schedule(
                {
                    try {
                        updateAll(appContext)
                        lastUpdateElapsedMs.set(SystemClock.elapsedRealtime())
                    } finally {
                        updatePending.set(false)
                    }
                },
                delayMs,
                TimeUnit.MILLISECONDS,
            )
        }

        fun updateAll(context: Context) {
            val appContext = context.applicationContext
            val manager = AppWidgetManager.getInstance(appContext)
            val appWidgetIds = clockWidgetIds(appContext, manager)
            if (appWidgetIds.isEmpty()) return

            updateWidgets(appContext, manager, appWidgetIds)
        }

        private fun clockWidgetIds(context: Context, manager: AppWidgetManager): IntArray {
            val component = ComponentName(context, AgentClockWidgetProvider::class.java)
            return manager.getAppWidgetIds(component)
        }

        fun hasClockWidgets(context: Context): Boolean {
            val appContext = context.applicationContext
            val manager = AppWidgetManager.getInstance(appContext)
            return clockWidgetIds(appContext, manager).isNotEmpty()
        }

        private fun schedulePeriodicUpdate(context: Context) {
            if (!hasClockWidgets(context)) return
            scheduleUpdateAlarm(
                context = context,
                action = ACTION_PERIODIC_UPDATE,
                requestCode = PERIODIC_UPDATE_REQUEST_CODE,
                delayMs = PERIODIC_UPDATE_INTERVAL_MS,
            )
        }

        private fun cancelPeriodicUpdate(context: Context) {
            cancelUpdateAlarm(context, ACTION_PERIODIC_UPDATE, PERIODIC_UPDATE_REQUEST_CODE)
        }

        private fun scheduleEventFollowUpUpdates(context: Context) {
            EVENT_FOLLOW_UP_DELAYS_MS.forEachIndexed { index, delayMs ->
                scheduleUpdateAlarm(
                    context = context,
                    action = ACTION_EVENT_FOLLOW_UP_UPDATE,
                    requestCode = EVENT_FOLLOW_UP_REQUEST_CODE_BASE + index,
                    delayMs = delayMs,
                )
            }
        }

        private fun cancelEventFollowUpUpdates(context: Context) {
            EVENT_FOLLOW_UP_DELAYS_MS.indices.forEach { index ->
                cancelUpdateAlarm(
                    context,
                    ACTION_EVENT_FOLLOW_UP_UPDATE,
                    EVENT_FOLLOW_UP_REQUEST_CODE_BASE + index,
                )
            }
        }

        private fun registerWidgetNetworkCallback(context: Context) {
            if (!hasClockWidgets(context)) return
            val appContext = context.applicationContext
            val connectivity = appContext.getSystemService(ConnectivityManager::class.java) ?: return
            val pendingIntent = updatePendingIntent(
                appContext,
                ACTION_NETWORK_CALLBACK_UPDATE,
                NETWORK_CALLBACK_REQUEST_CODE,
            )
            val request = NetworkRequest.Builder()
                .addTransportType(NetworkCapabilities.TRANSPORT_WIFI)
                .addTransportType(NetworkCapabilities.TRANSPORT_ETHERNET)
                .build()
            runCatching { connectivity.unregisterNetworkCallback(pendingIntent) }
            runCatching { connectivity.registerNetworkCallback(request, pendingIntent) }
        }

        private fun unregisterWidgetNetworkCallback(context: Context) {
            val appContext = context.applicationContext
            val connectivity = appContext.getSystemService(ConnectivityManager::class.java) ?: return
            runCatching {
                connectivity.unregisterNetworkCallback(
                    updatePendingIntent(appContext, ACTION_NETWORK_CALLBACK_UPDATE, NETWORK_CALLBACK_REQUEST_CODE),
                )
            }
        }

        private fun scheduleUpdateAlarm(
            context: Context,
            action: String,
            requestCode: Int,
            delayMs: Long,
        ) {
            val appContext = context.applicationContext
            val alarm = appContext.getSystemService(AlarmManager::class.java) ?: return
            alarm.set(
                AlarmManager.ELAPSED_REALTIME,
                SystemClock.elapsedRealtime() + delayMs,
                updatePendingIntent(appContext, action, requestCode),
            )
        }

        private fun cancelUpdateAlarm(context: Context, action: String, requestCode: Int) {
            val appContext = context.applicationContext
            val alarm = appContext.getSystemService(AlarmManager::class.java) ?: return
            alarm.cancel(updatePendingIntent(appContext, action, requestCode))
        }

        private fun updatePendingIntent(context: Context, action: String, requestCode: Int): PendingIntent {
            val intent = Intent(context, AgentClockWidgetProvider::class.java)
                .setAction(action)
                .setPackage(context.packageName)
            return PendingIntent.getBroadcast(
                context,
                requestCode,
                intent,
                PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
            )
        }

        private fun updateWidgets(context: Context, appWidgetManager: AppWidgetManager, appWidgetIds: IntArray) {
            val wifiText = currentWifiText(context)

            appWidgetIds.forEach { appWidgetId ->
                val size = clockSize(appWidgetManager.getAppWidgetOptions(appWidgetId))
                val views = RemoteViews(context.packageName, R.layout.agent_clock_widget).apply {
                    setTextViewText(R.id.agentClockWifiInfo, wifiText.infoLine)
                    setTextViewText(R.id.agentClockIpInfo, wifiText.ipLine)
                    setTextViewText(R.id.agentClockLocationInfo, wifiText.locationLine)
                    setTextViewTextSize(R.id.agentClockDate, TypedValue.COMPLEX_UNIT_SP, size.dateSp)
                    setTextViewTextSize(R.id.agentClockTime, TypedValue.COMPLEX_UNIT_SP, size.timeSp)
                    setTextViewTextSize(R.id.agentClockWifiInfo, TypedValue.COMPLEX_UNIT_SP, size.wifiSp)
                    setTextViewTextSize(R.id.agentClockIpInfo, TypedValue.COMPLEX_UNIT_SP, size.wifiSp)
                    setTextViewTextSize(R.id.agentClockLocationInfo, TypedValue.COMPLEX_UNIT_SP, size.wifiSp)
                    setOnClickPendingIntent(R.id.agentClockRoot, settingsPendingIntent(context))
                }
                appWidgetManager.updateAppWidget(appWidgetId, views)
            }
        }

        private fun settingsPendingIntent(context: Context): PendingIntent {
            val intent = Intent(Settings.ACTION_SETTINGS)
                .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            return PendingIntent.getActivity(
                context,
                SETTINGS_REQUEST_CODE,
                intent,
                PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
            )
        }

        @SuppressLint("MissingPermission")
        @Suppress("DEPRECATION")
        private fun currentWifiText(context: Context): ClockWifiText {
            val snapshot = currentWifiSnapshot(context)
            val ipLine = formatIpLine(snapshot.addresses)
            val locationLine = formatGnssLine(currentGnssLocation(context))
            if (!snapshot.connected) {
                return ClockWifiText(
                    infoLine = context.getString(R.string.agent_clock_widget_wifi_placeholder),
                    ipLine = ipLine,
                    locationLine = locationLine,
                )
            }

            val info = snapshot.info
            val essid = cleanEssid(info?.ssid).takeUnless { it == UNKNOWN_VALUE }
                ?: UNKNOWN_VALUE
            val bssid = cleanBssid(info?.bssid).takeUnless { it == UNKNOWN_VALUE }
                ?: UNKNOWN_VALUE
            val generation = info?.let { wifiGeneration(it) }
                ?.takeUnless { it == "Wi-Fi unknown" }
                ?: "Wi-Fi unknown"
            val frequencyMhz = info?.frequency?.takeIf { it > 0 }
            val channel = frequencyMhz?.let { wifiChannel(it)?.let { channel -> "ch$channel" } ?: "${it}MHz" }
                ?: "freq unknown"
            val rssi = info?.let { cleanRssi(it.rssi) }?.takeUnless { it == UNKNOWN_VALUE }
                ?: UNKNOWN_VALUE
            return ClockWifiText(
                infoLine = "$essid  $bssid  $generation  $channel  $rssi",
                ipLine = ipLine,
                locationLine = locationLine,
            )
        }

        @SuppressLint("MissingPermission")
        @Suppress("DEPRECATION")
        private fun currentWifiSnapshot(context: Context): WifiSnapshot {
            val appContext = context.applicationContext
            val connectivity = appContext.getSystemService(ConnectivityManager::class.java)
                ?: return disconnectedWifiSnapshot(null)
            val wifi = appContext.getSystemService(WifiManager::class.java)
            if (wifi?.isWifiOffOrTurningOff() == true) return disconnectedWifiSnapshot(connectivity)

            val activeNetwork = connectivity.activeNetwork
            val activeCapabilities = activeNetwork
                ?.let { connectivity.getNetworkCapabilities(it) }
            if (activeCapabilities?.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) == true) {
                wifiSnapshotForNetwork(appContext, connectivity, activeNetwork, activeCapabilities)?.let {
                    return it
                }
            }

            connectivity.allNetworks.orEmpty().forEach { network ->
                val capabilities = connectivity.getNetworkCapabilities(network) ?: return@forEach
                if (capabilities.hasTransport(NetworkCapabilities.TRANSPORT_WIFI)) {
                    wifiSnapshotForNetwork(appContext, connectivity, network, capabilities)?.let {
                        return it
                    }
                }
            }

            return disconnectedWifiSnapshot(connectivity)
        }

        private fun wifiSnapshotForNetwork(
            context: Context,
            connectivity: ConnectivityManager,
            network: Network,
            capabilities: NetworkCapabilities,
        ): WifiSnapshot? {
            val info = bestWifiInfo(context, capabilities.transportInfo as? WifiInfo)
            if (!clockWidgetWifiNetworkIsDisplayable(capabilities.isLocalWifiNetwork(), info != null)) {
                return null
            }
            val usableInfo = info ?: return null
            return WifiSnapshot(
                connected = true,
                info = usableInfo,
                addresses = networkAddresses(connectivity.getLinkProperties(network)),
            )
        }

        private fun disconnectedWifiSnapshot(connectivity: ConnectivityManager?): WifiSnapshot {
            return WifiSnapshot(
                connected = false,
                info = null,
                addresses = ethernetAddresses(connectivity),
            )
        }

        @Suppress("DEPRECATION")
        private fun bestWifiInfo(context: Context, primary: WifiInfo?): WifiInfo? {
            if (primary?.isUsableWifiInfo() == true) return primary

            val wifi = context.applicationContext.getSystemService(WifiManager::class.java)
            val fallback = wifi?.connectionInfo
            return if (fallback?.isUsableWifiInfo() == true) fallback else null
        }

        private fun cleanEssid(ssid: String?): String {
            val value = ssid.orEmpty().trim().removeSurrounding("\"")
            return if (clockWidgetKnownSsid(value)) value else UNKNOWN_VALUE
        }

        private fun cleanBssid(bssid: String?): String {
            val value = bssid.orEmpty().trim()
            return if (clockWidgetKnownBssid(value)) value else UNKNOWN_VALUE
        }

        private fun wifiGeneration(info: WifiInfo): String {
            return wifiGeneration(wifiStandardName(info.wifiStandard), info.frequency)
        }

        private fun wifiGeneration(standard: String, frequencyMhz: Int): String {
            return when (standard) {
                "802.11be" -> "Wi-Fi 7"
                "802.11ax" -> if (frequencyMhz in 5925..7125) "Wi-Fi 6E" else "Wi-Fi 6"
                "802.11ac" -> "Wi-Fi 5"
                "802.11n" -> "Wi-Fi 4"
                "802.11ad" -> "WiGig"
                "legacy" -> "Wi-Fi legacy"
                else -> "Wi-Fi unknown"
            }
        }

        private fun cleanRssi(rssi: Int): String {
            return if (rssi in -126..0) "$rssi dBm" else UNKNOWN_VALUE
        }

        private fun formatIpLine(addresses: NetworkAddresses): String {
            return "IPv4 ${addresses.ipv4 ?: NONE_VALUE}  IPv6 ${addresses.ipv6 ?: NONE_VALUE}"
        }

        @Suppress("DEPRECATION")
        private fun ethernetAddresses(connectivity: ConnectivityManager?): NetworkAddresses {
            connectivity ?: return NetworkAddresses()

            val activeNetwork = connectivity.activeNetwork
            val activeCapabilities = activeNetwork
                ?.let { connectivity.getNetworkCapabilities(it) }
            if (activeCapabilities?.hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET) == true) {
                val addresses = networkAddresses(connectivity.getLinkProperties(activeNetwork))
                if (addresses.hasAny()) return addresses
            }

            for (network in connectivity.allNetworks.orEmpty()) {
                val capabilities = connectivity.getNetworkCapabilities(network) ?: continue
                if (!capabilities.hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET)) continue

                val addresses = networkAddresses(connectivity.getLinkProperties(network))
                if (addresses.hasAny()) return addresses
            }

            return NetworkAddresses()
        }

        private fun networkAddresses(link: LinkProperties?): NetworkAddresses {
            val addresses = link?.linkAddresses.orEmpty()
            val ipv4 = addresses.firstNotNullOfOrNull { linkAddress ->
                (linkAddress.address as? Inet4Address)?.hostAddress
            }
            val ipv6 = addresses
                .mapNotNull { it.address as? Inet6Address }
                .sortedBy { it.isLinkLocalAddress }
                .firstOrNull()
                ?.hostAddress
                ?.substringBefore('%')
            return NetworkAddresses(ipv4 = ipv4, ipv6 = ipv6)
        }

        @SuppressLint("MissingPermission")
        private fun currentGnssLocation(context: Context): Location? {
            val locationManager = context.applicationContext.getSystemService(LocationManager::class.java)
                ?: return null
            return LOCATION_PROVIDERS.firstNotNullOfOrNull { provider ->
                runCatching {
                    locationManager.getLastKnownLocation(provider)
                }.getOrNull()
            }
        }

        private fun formatGnssLine(location: Location?): String {
            location ?: return "GPS/GNSS ${NONE_VALUE}"
            val latitude = String.format(Locale.US, "%.6f", location.latitude)
            val longitude = String.format(Locale.US, "%.6f", location.longitude)
            val accuracy = if (location.hasAccuracy()) {
                " +/-${location.accuracy.roundToInt()}m"
            } else {
                ""
            }
            return "GPS/GNSS $latitude,$longitude$accuracy"
        }

        private fun wifiChannel(frequencyMhz: Int): Int? = when (frequencyMhz) {
            2484 -> 14
            in 2412..2472 -> (frequencyMhz - 2407) / 5
            in 5000..5895 -> (frequencyMhz - 5000) / 5
            in 5955..7115 -> (frequencyMhz - 5950) / 5
            in 58320..70200 -> (frequencyMhz - 56160) / 2160
            else -> null
        }

        private const val UNKNOWN_VALUE = "unknown"
        private const val NONE_VALUE = "none"
        private const val PERIODIC_UPDATE_INTERVAL_MS = 60_000L
        private const val PERIODIC_UPDATE_REQUEST_CODE = 11_000
        private const val NETWORK_CALLBACK_REQUEST_CODE = 11_050
        private const val EVENT_FOLLOW_UP_REQUEST_CODE_BASE = 11_100
        private const val SETTINGS_REQUEST_CODE = 11_200
        private const val UPDATE_DEBOUNCE_MS = 1_000L
        private const val MIN_UPDATE_INTERVAL_MS = 1_000L
        private const val ACTION_PERIODIC_UPDATE = "io.dropcheck.agent.action.CLOCK_WIDGET_PERIODIC_UPDATE"
        private const val ACTION_NETWORK_CALLBACK_UPDATE = "io.dropcheck.agent.action.CLOCK_WIDGET_NETWORK_CALLBACK_UPDATE"
        private const val ACTION_EVENT_FOLLOW_UP_UPDATE = "io.dropcheck.agent.action.CLOCK_WIDGET_EVENT_FOLLOW_UP_UPDATE"
        private const val FUSED_PROVIDER = "fused"
        private const val GPS_HARDWARE_PROVIDER = "gps_hardware"
        @Suppress("DEPRECATION")
        private val WIFI_EVENT_ACTIONS = setOf(
            WifiManager.WIFI_STATE_CHANGED_ACTION,
            WifiManager.NETWORK_STATE_CHANGED_ACTION,
            WifiManager.SUPPLICANT_STATE_CHANGED_ACTION,
            WifiManager.RSSI_CHANGED_ACTION,
        )
        private val EVENT_FOLLOW_UP_DELAYS_MS = longArrayOf(1_000L, 3_000L, 8_000L)
        private val LOCATION_PROVIDERS = listOf(
            LocationManager.GPS_PROVIDER,
            GPS_HARDWARE_PROVIDER,
            FUSED_PROVIDER,
            LocationManager.NETWORK_PROVIDER,
            LocationManager.PASSIVE_PROVIDER,
        )

        private fun WifiInfo.isUsableWifiInfo(): Boolean {
            return clockWidgetWifiInfoIsUsable(
                networkId = networkId,
                ssid = ssid,
                bssid = bssid,
                supplicantState = supplicantState?.toString(),
            )
        }

        private fun NetworkCapabilities.isLocalWifiNetwork(): Boolean {
            return Build.VERSION.SDK_INT >= 35 &&
                hasCapability(NetworkCapabilities.NET_CAPABILITY_LOCAL_NETWORK)
        }

        private fun WifiManager.isWifiOffOrTurningOff(): Boolean {
            return wifiState == WifiManager.WIFI_STATE_DISABLED ||
                wifiState == WifiManager.WIFI_STATE_DISABLING
        }

        private fun clockSize(options: Bundle): ClockSize {
            val widthDp = optionDp(
                options,
                AppWidgetManager.OPTION_APPWIDGET_MIN_WIDTH,
                AppWidgetManager.OPTION_APPWIDGET_MAX_WIDTH,
                fallback = 260,
            )
            val heightDp = optionDp(
                options,
                AppWidgetManager.OPTION_APPWIDGET_MIN_HEIGHT,
                AppWidgetManager.OPTION_APPWIDGET_MAX_HEIGHT,
                fallback = 130,
            )

            val timeByWidth = widthDp * 0.36f
            val timeByHeight = heightDp * 0.72f
            val baseTimeSp = minOf(timeByWidth, timeByHeight).coerceIn(72f, 132f)
            val timeSp = baseTimeSp + 12f
            val dateSp = (baseTimeSp * 0.24f).coerceIn(16f, 28f)
            val wifiSp = (widthDp * 0.036f).coerceIn(9f, 13f)
            return ClockSize(dateSp = dateSp, timeSp = timeSp, wifiSp = wifiSp)
        }

        private fun optionDp(options: Bundle, minKey: String, maxKey: String, fallback: Int): Int {
            val min = options.getInt(minKey, 0)
            val max = options.getInt(maxKey, 0)
            return listOf(min, max)
                .filter { it > 0 }
                .average()
                .takeIf { !it.isNaN() }
                ?.toInt()
                ?: fallback
        }
    }
}

private data class ClockSize(
    val dateSp: Float,
    val timeSp: Float,
    val wifiSp: Float,
)

private data class WifiSnapshot(
    val connected: Boolean,
    val info: WifiInfo?,
    val addresses: NetworkAddresses,
)

private data class ClockWifiText(
    val infoLine: CharSequence,
    val ipLine: CharSequence,
    val locationLine: CharSequence,
)

private data class NetworkAddresses(
    val ipv4: String? = null,
    val ipv6: String? = null,
) {
    fun hasAny(): Boolean = ipv4 != null || ipv6 != null
}
