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
import android.net.NetworkRequest
import android.net.wifi.WifiManager
import android.os.Build
import java.util.concurrent.Executors
import java.util.concurrent.ScheduledExecutorService
import java.util.concurrent.ScheduledFuture
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean

@Suppress("DEPRECATION")
/**
 * Process-local observer that keeps active clock widgets fresh while the
 * foreground service is alive.
 *
 * It complements AppWidgetProvider callbacks with direct connectivity
 * callbacks and short follow-up refreshes after network churn.
 */
internal class ClockWidgetRefreshObserver(
    context: Context,
) {
    private val appContext = context.applicationContext
    private val connectivity = appContext.getSystemService(ConnectivityManager::class.java)
    private val executor: ScheduledExecutorService = Executors.newSingleThreadScheduledExecutor()
    private val started = AtomicBoolean(false)
    private val followUpRunning = AtomicBoolean(false)

    private var pollTask: ScheduledFuture<*>? = null
    private var broadcastRegistered = false
    private var wifiCallbackRegistered = false
    private var defaultCallbackRegistered = false

    private val receiver = object : BroadcastReceiver() {
        override fun onReceive(context: Context, intent: Intent) {
            requestRefreshWithFollowUps()
        }
    }

    private val wifiCallback = object : ConnectivityManager.NetworkCallback(
        ConnectivityManager.NetworkCallback.FLAG_INCLUDE_LOCATION_INFO,
    ) {
        override fun onAvailable(network: Network) = requestRefreshWithFollowUps()
        override fun onLosing(network: Network, maxMsToLive: Int) = requestRefreshWithFollowUps()
        override fun onLost(network: Network) = requestRefreshWithFollowUps()
        override fun onUnavailable() = requestRefreshWithFollowUps()
        override fun onCapabilitiesChanged(network: Network, networkCapabilities: NetworkCapabilities) = requestRefreshWithFollowUps()
        override fun onLinkPropertiesChanged(network: Network, linkProperties: LinkProperties) = requestRefreshWithFollowUps()
        override fun onBlockedStatusChanged(network: Network, blocked: Boolean) = requestRefreshWithFollowUps()
    }

    private val defaultCallback = object : ConnectivityManager.NetworkCallback(
        ConnectivityManager.NetworkCallback.FLAG_INCLUDE_LOCATION_INFO,
    ) {
        override fun onAvailable(network: Network) = requestRefreshWithFollowUps()
        override fun onLost(network: Network) = requestRefreshWithFollowUps()
        override fun onCapabilitiesChanged(network: Network, networkCapabilities: NetworkCapabilities) = requestRefreshWithFollowUps()
        override fun onLinkPropertiesChanged(network: Network, linkProperties: LinkProperties) = requestRefreshWithFollowUps()
    }

    fun start() {
        if (!started.compareAndSet(false, true)) return
        registerBroadcasts()
        registerCallbacks()
        requestRefreshWithFollowUps()
        pollTask = executor.scheduleWithFixedDelay(
            { refreshNow() },
            POLL_INTERVAL_MS,
            POLL_INTERVAL_MS,
            TimeUnit.MILLISECONDS,
        )
    }

    fun stop() {
        if (!started.compareAndSet(true, false)) return
        pollTask?.cancel(true)
        pollTask = null
        if (broadcastRegistered) {
            runCatching { appContext.unregisterReceiver(receiver) }
            broadcastRegistered = false
        }
        if (wifiCallbackRegistered) {
            runCatching { connectivity?.unregisterNetworkCallback(wifiCallback) }
            wifiCallbackRegistered = false
        }
        if (defaultCallbackRegistered) {
            runCatching { connectivity?.unregisterNetworkCallback(defaultCallback) }
            defaultCallbackRegistered = false
        }
        followUpRunning.set(false)
    }

    fun shutdown() {
        stop()
        executor.shutdownNow()
    }

    private fun requestRefreshWithFollowUps() {
        if (!started.get()) return
        runCatching { executor.execute { refreshWithFollowUps() } }
    }

    private fun registerBroadcasts() {
        val filter = IntentFilter().apply {
            addAction(WifiManager.WIFI_STATE_CHANGED_ACTION)
            addAction(WifiManager.NETWORK_STATE_CHANGED_ACTION)
            addAction(WifiManager.SUPPLICANT_STATE_CHANGED_ACTION)
            addAction(WifiManager.RSSI_CHANGED_ACTION)
        }
        runCatching {
            if (Build.VERSION.SDK_INT >= 33) {
                appContext.registerReceiver(receiver, filter, Context.RECEIVER_NOT_EXPORTED)
            } else {
                appContext.registerReceiver(receiver, filter)
            }
        }.onSuccess {
            broadcastRegistered = true
        }
    }

    @SuppressLint("MissingPermission")
    private fun registerCallbacks() {
        val connectivity = connectivity ?: return
        val request = NetworkRequest.Builder()
            .addTransportType(NetworkCapabilities.TRANSPORT_WIFI)
            .build()
        runCatching { connectivity.registerNetworkCallback(request, wifiCallback) }
            .onSuccess { wifiCallbackRegistered = true }
        runCatching { connectivity.registerDefaultNetworkCallback(defaultCallback) }
            .onSuccess { defaultCallbackRegistered = true }
    }

    private fun refreshWithFollowUps() {
        refreshNow()
        if (!followUpRunning.compareAndSet(false, true)) return
        FOLLOW_UP_DELAYS_MS.forEachIndexed { index, delayMs ->
            executor.schedule(
                {
                    try {
                        refreshNow()
                    } finally {
                        if (index == FOLLOW_UP_DELAYS_MS.lastIndex) {
                            followUpRunning.set(false)
                        }
                    }
                },
                delayMs,
                TimeUnit.MILLISECONDS,
            )
        }
    }

    private fun refreshNow() {
        if (!started.get()) return
        runCatching { AgentClockWidgetProvider.updateAll(appContext) }
    }

    private companion object {
        private const val POLL_INTERVAL_MS = 10_000L
        private val FOLLOW_UP_DELAYS_MS = longArrayOf(1_000L, 3_000L, 8_000L)
    }
}
