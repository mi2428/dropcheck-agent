package io.dropcheck.agent

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.Service
import android.Manifest
import android.content.Intent
import android.content.Context
import android.content.pm.PackageManager
import android.content.pm.ServiceInfo
import android.location.LocationManager
import android.os.Build
import android.os.IBinder
import java.util.concurrent.Executors
import java.util.concurrent.Future

/**
 * Foreground service entry point for controller gRPC sessions.
 *
 * The service performs only Android lifecycle work: it validates the start
 * intent and keeps one active controller session started through ADB.
 */
class AgentService : Service() {
    private val executor = Executors.newSingleThreadExecutor()
    private val sessionLock = Any()
    private lateinit var clockWidgetObserver: ClockWidgetRefreshObserver
    private var wifiEvents: WifiEventLogger? = null
    private var wifiEventsStarted = false
    private var current: Future<*>? = null
    private var currentSessionKey: String = ""
    private var currentStartId: Int = 0

    override fun onCreate() {
        super.onCreate()
        ensureNotificationChannel()
        startForegroundWithType("starting")
        TerminalLog.compactIfNeeded(this)
        clockWidgetObserver = ClockWidgetRefreshObserver(this)
        if (AgentClockWidgetProvider.hasClockWidgets(this)) {
            clockWidgetObserver.start()
        }
        TerminalLog.infoEvent(this, "service.create", listOf(
            "sdk" to Build.VERSION.SDK_INT,
            "manufacturer" to Build.MANUFACTURER,
            "model" to Build.MODEL,
            "device" to Build.DEVICE,
        ))
    }

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        startForegroundWithType(notificationText(intent?.action))
        TerminalLog.infoEvent(this, "service.start", listOf(
            "action" to intent?.action,
            "start_id" to startId,
            "flags" to flags,
            "extras" to intent?.extras?.keySet()?.toList().orEmpty(),
        ))
        when (intent?.action) {
            ACTION_WIDGET_REFRESH_OBSERVER -> {
                clockWidgetObserver.start()
                TerminalLog.infoEvent(this, "widget.observer.start", listOf(
                    "clock_widgets" to AgentClockWidgetProvider.hasClockWidgets(this),
                ))
                AgentClockWidgetProvider.requestUpdate(this)
            }
            ACTION_WIDGET_REFRESH_STOP -> {
                if (!AgentClockWidgetProvider.hasClockWidgets(this)) {
                    clockWidgetObserver.stop()
                }
                TerminalLog.infoEvent(this, "widget.observer.stop", listOf(
                    "clock_widgets" to AgentClockWidgetProvider.hasClockWidgets(this),
                ))
                if (!shouldStayStarted()) {
                    stopForeground(STOP_FOREGROUND_REMOVE)
                    stopSelf(startId)
                }
            }
            ACTION_GRPC_SESSION -> {
                val host = intent.getStringExtra(EXTRA_GRPC_HOST) ?: "127.0.0.1"
                val port = intent.getIntExtra(EXTRA_GRPC_PORT, 0)
                val token = intent.getStringExtra(EXTRA_GRPC_TOKEN).orEmpty()
                val agentId = intent.getStringExtra(EXTRA_AGENT_ID).orEmpty()
                val adbSerial = intent.getStringExtra(EXTRA_ADB_SERIAL).orEmpty()
                ensureWifiEventsStarted()
                TerminalLog.debugEvent(this, "grpc.session.intent", listOf(
                    "host" to host,
                    "port" to port,
                    "agent_id" to agentId,
                    "adb_serial" to adbSerial,
                    "token_present" to token.isNotBlank(),
                    "current_active" to (current?.isDone == false),
                ))
                if (port <= 0 || token.isBlank()) {
                    TerminalLog.warnEvent(this, "grpc.session.ignored", listOf(
                        "reason" to "invalid_start_request",
                        "port" to port,
                        "token_present" to token.isNotBlank(),
                    ))
                } else {
                    startGrpcSession(host, port, token, agentId, adbSerial, startId)
                }
            }
            null -> Unit
            else -> TerminalLog.warnEvent(this, "service.start.ignored", listOf(
                "reason" to "unknown_action",
                "action" to intent?.action,
            ))
        }
        return if (shouldStayStarted()) START_STICKY else START_NOT_STICKY
    }

    override fun onDestroy() {
        current?.cancel(true)
        if (::clockWidgetObserver.isInitialized) {
            clockWidgetObserver.shutdown()
        }
        stopWifiEvents()
        executor.shutdownNow()
        TerminalLog.infoEvent(this, "service.destroy", emptyList())
        super.onDestroy()
    }

    /**
     * Starts one gRPC session worker.
     *
     * ADB-started sessions supersede older sessions so short-lived controller
     * invocations can run back-to-back without waiting for Android cleanup.
     */
    private fun startGrpcSession(
        host: String,
        port: Int,
        token: String,
        agentId: String,
        adbSerial: String,
        startId: Int,
    ) {
        val sessionKey = "$host:$port:$token"
        synchronized(sessionLock) {
            if (current?.isDone == false) {
                TerminalLog.infoEvent(this, "grpc.session.superseded", listOf(
                    "transport" to "adb-reverse",
                    "host" to host,
                    "port" to port,
                    "agent_id" to agentId,
                    "adb_serial" to adbSerial,
                ))
                current?.cancel(true)
            }
            currentSessionKey = sessionKey
            currentStartId = startId
            current = executor.submit {
                TerminalLog.debugEvent(this, "grpc.session.worker.start", listOf(
                    "thread" to Thread.currentThread().name,
                    "host" to host,
                    "port" to port,
                    "transport" to "adb-reverse",
                    "agent_id" to agentId,
                    "adb_serial" to adbSerial,
                ))
                try {
                    GrpcSessionClient(this, host, port, token, agentId, adbSerial).run()
                } finally {
                    TerminalLog.infoEvent(this, "grpc.session.finished", listOf(
                        "host" to host,
                        "port" to port,
                        "transport" to "adb-reverse",
                        "agent_id" to agentId,
                        "adb_serial" to adbSerial,
                    ))
                    afterGrpcSessionFinished(sessionKey, startId)
                }
            }
        }
        TerminalLog.infoEvent(this, "grpc.session.queued", listOf(
            "host" to host,
            "port" to port,
            "transport" to "adb-reverse",
            "agent_id" to agentId,
            "adb_serial" to adbSerial,
            "token_present" to token.isNotBlank(),
        ))
    }

    /**
     * Cleans up one completed session.
     *
     * New sessions intentionally supersede older sessions. The canceled worker
     * may still run its finally block after the replacement has been queued; the
     * session key prevents stale cleanup from clearing the fresh session state.
     */
    private fun afterGrpcSessionFinished(sessionKey: String, startId: Int) {
        synchronized(sessionLock) {
            if (currentSessionKey != sessionKey || currentStartId != startId) {
                TerminalLog.debug(
                    this,
                    "ignoring stale session cleanup current_start_id=$currentStartId",
                )
                return
            }
            current = null
            currentSessionKey = ""
            currentStartId = 0
        }
        stopWifiEvents()
        if (!shouldStayStarted()) {
            stopForeground(STOP_FOREGROUND_REMOVE)
            stopSelf(startId)
        }
    }

    private fun shouldStayStarted(): Boolean {
        return AgentClockWidgetProvider.hasClockWidgets(this)
    }

    private fun notificationText(action: String?): String {
        return when (action) {
            ACTION_GRPC_SESSION -> "controller session"
            ACTION_WIDGET_REFRESH_OBSERVER -> "widget updates"
            else -> "waiting for controller"
        }
    }

    private fun ensureWifiEventsStarted() {
        if (wifiEventsStarted) return
        val logger = WifiEventLogger(this)
        wifiEvents = logger
        wifiEventsStarted = true
        logger.start()
    }

    private fun stopWifiEvents() {
        if (!wifiEventsStarted) return
        wifiEvents?.stop()
        wifiEvents = null
        wifiEventsStarted = false
    }

    /** Creates the low-importance channel required for foreground service visibility. */
    private fun ensureNotificationChannel() {
        val manager = getSystemService(NotificationManager::class.java)
        manager.createNotificationChannel(NotificationChannel(
            CHANNEL_ID,
            "dropcheck agent",
            NotificationManager.IMPORTANCE_LOW,
        ))
    }

    /** Builds the persistent foreground notification shown while the agent waits/runs. */
    private fun notification(text: String): Notification {
        return Notification.Builder(this, CHANNEL_ID)
            .setContentTitle("dropcheck agent")
            .setContentText(text)
            .setSmallIcon(R.drawable.ic_stat_dropcheck)
            .build()
    }

    private fun startForegroundWithType(text: String) {
        val notification = notification(text)
        startForeground(NOTIFICATION_ID, notification, foregroundServiceTypeMask())
    }

    private fun foregroundServiceTypeMask(): Int {
        var mask = ServiceInfo.FOREGROUND_SERVICE_TYPE_CONNECTED_DEVICE
        if (canUseLocationForegroundService()) {
            mask = mask or ServiceInfo.FOREGROUND_SERVICE_TYPE_LOCATION
        }
        return mask
    }

    private fun canUseLocationForegroundService(): Boolean {
        if (!hasBackgroundLocationAccess()) return false
        val hasForegroundLocation =
            checkSelfPermission(Manifest.permission.ACCESS_FINE_LOCATION) == PackageManager.PERMISSION_GRANTED ||
                checkSelfPermission(Manifest.permission.ACCESS_COARSE_LOCATION) == PackageManager.PERMISSION_GRANTED
        if (!hasForegroundLocation) return false
        val locationManager = getSystemService(LocationManager::class.java)
        return locationManager?.isLocationEnabled ?: true
    }

    private fun hasBackgroundLocationAccess(): Boolean {
        return checkSelfPermission(Manifest.permission.ACCESS_BACKGROUND_LOCATION) == PackageManager.PERMISSION_GRANTED
    }

    companion object {
        private const val CHANNEL_ID = "dropcheck-agent"
        private const val NOTIFICATION_ID = 1001
        const val ACTION_GRPC_SESSION = "io.dropcheck.agent.action.GRPC_SESSION"
        const val ACTION_WIDGET_REFRESH_OBSERVER = "io.dropcheck.agent.action.WIDGET_REFRESH_OBSERVER"
        const val ACTION_WIDGET_REFRESH_STOP = "io.dropcheck.agent.action.WIDGET_REFRESH_STOP"
        const val EXTRA_GRPC_HOST = "grpc_host"
        const val EXTRA_GRPC_PORT = "grpc_port"
        const val EXTRA_GRPC_TOKEN = "grpc_token"
        const val EXTRA_AGENT_ID = "agent_id"
        const val EXTRA_ADB_SERIAL = "adb_serial"

        fun requestWidgetObserver(context: Context) {
            val intent = Intent(context, AgentService::class.java).setAction(ACTION_WIDGET_REFRESH_OBSERVER)
            runCatching {
                context.startForegroundService(intent)
            }
        }

        fun requestWidgetObserverStop(context: Context) {
            val intent = Intent(context, AgentService::class.java).setAction(ACTION_WIDGET_REFRESH_STOP)
            runCatching {
                context.startForegroundService(intent)
            }
        }

    }
}
