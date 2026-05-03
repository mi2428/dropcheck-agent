package io.dropcheck.agent

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.Service
import android.content.Intent
import android.content.Context
import android.os.Build
import android.os.IBinder
import java.util.concurrent.Executors
import java.util.concurrent.Future

/**
 * Foreground service entry point for controller gRPC sessions.
 *
 * The service performs only Android lifecycle work: it validates the start
 * intent, keeps one active session, and retries a stored controller endpoint
 * after USB/ADB transport goes away.
 */
class AgentService : Service() {
    private val executor = Executors.newSingleThreadExecutor()
    private val sessionLock = Any()
    private lateinit var clockWidgetObserver: ClockWidgetRefreshObserver
    private var wifiEvents: WifiEventLogger? = null
    private var wifiEventsStarted = false
    private lateinit var standaloneRunner: StandaloneRunner
    private var current: Future<*>? = null
    private var currentMode: String = ""
    private var currentSessionKey: String = ""

    override fun onCreate() {
        super.onCreate()
        TerminalLog.compactIfNeeded(this)
        ensureNotificationChannel()
        clockWidgetObserver = ClockWidgetRefreshObserver(this)
        if (AgentClockWidgetProvider.hasClockWidgets(this)) {
            clockWidgetObserver.start()
        }
        standaloneRunner = StandaloneRunner(this)
        standaloneRunner.refresh()
        TerminalLog.infoEvent(this, "service.create", listOf(
            "sdk" to Build.VERSION.SDK_INT,
            "manufacturer" to Build.MANUFACTURER,
            "model" to Build.MODEL,
            "device" to Build.DEVICE,
        ))
    }

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        startForeground(NOTIFICATION_ID, notification(notificationText(intent?.action)))
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
                AgentClockWidgetProvider.updateAll(this)
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
                    startGrpcSession(host, port, token, agentId, adbSerial, "adb-reverse")
                }
            }
            ACTION_STANDALONE_REFRESH, ACTION_CONTROLLER_LINK_REFRESH, null -> {
                if (StandaloneConfigStore(this).load().enabled || ControllerLinkStore(this).load().enabled) {
                    ensureWifiEventsStarted()
                }
                TerminalLog.infoEvent(this, "standalone.refresh", listOf(
                    "enabled" to StandaloneConfigStore(this).load().enabled,
                    "current_active" to (current?.isDone == false),
                ))
                standaloneRunner.refresh()
                startControllerLinkLoop("refresh")
            }
            else -> TerminalLog.warnEvent(this, "service.start.ignored", listOf(
                "reason" to "unknown_action",
                "action" to intent?.action,
            ))
        }
        return if (shouldStayStarted()) START_STICKY else START_NOT_STICKY
    }

    override fun onDestroy() {
        current?.cancel(true)
        if (::standaloneRunner.isInitialized) {
            standaloneRunner.shutdown()
        }
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
     * The service rejects concurrent direct sessions because command execution
     * is single-threaded and each APK instance represents one physical device.
     * ADB-reverse sessions supersede older sessions so short-lived controller
     * invocations can run back-to-back without waiting for Android cleanup.
     */
    private fun startGrpcSession(host: String, port: Int, token: String, agentId: String, adbSerial: String, transport: String) {
        val sessionKey = "$transport:$host:$port:$token"
        synchronized(sessionLock) {
            if (current?.isDone == false && transport == "adb-reverse") {
                TerminalLog.infoEvent(this, "grpc.session.superseded", listOf(
                    "previous_transport" to currentMode,
                    "transport" to transport,
                    "host" to host,
                    "port" to port,
                    "agent_id" to agentId,
                    "adb_serial" to adbSerial,
                ))
                current?.cancel(true)
            } else if (current?.isDone == false) {
                TerminalLog.warnEvent(this, "grpc.session.rejected", listOf(
                    "reason" to "already_active",
                    "host" to host,
                    "port" to port,
                    "transport" to transport,
                    "agent_id" to agentId,
                    "adb_serial" to adbSerial,
                    "token_present" to token.isNotBlank(),
                ))
                return
            }
            currentMode = transport
            currentSessionKey = sessionKey
        }
        TerminalLog.infoEvent(this, "grpc.session.queued", listOf(
            "host" to host,
            "port" to port,
            "transport" to transport,
            "agent_id" to agentId,
            "adb_serial" to adbSerial,
            "token_present" to token.isNotBlank(),
        ))
        current = executor.submit {
            TerminalLog.debugEvent(this, "grpc.session.worker.start", listOf(
                "thread" to Thread.currentThread().name,
                "host" to host,
                "port" to port,
                "transport" to transport,
                "agent_id" to agentId,
                "adb_serial" to adbSerial,
            ))
            try {
                GrpcSessionClient(this, host, port, token, agentId, adbSerial, transport).run()
            } finally {
                TerminalLog.infoEvent(this, "grpc.session.finished", listOf(
                    "host" to host,
                    "port" to port,
                    "transport" to transport,
                    "agent_id" to agentId,
                    "adb_serial" to adbSerial,
                ))
                afterGrpcSessionFinished(transport, sessionKey)
            }
        }
    }

    /**
     * Cleans up one completed session and decides whether direct reconnect should continue.
     *
     * ADB sessions intentionally supersede older sessions. The canceled worker
     * may still run its finally block after the replacement has been queued; the
     * session key prevents stale cleanup from clearing the fresh session state.
     */
    private fun afterGrpcSessionFinished(transport: String, sessionKey: String) {
        synchronized(sessionLock) {
            if (currentMode != transport || currentSessionKey != sessionKey) {
                TerminalLog.debug(this, "ignoring stale session cleanup transport=$transport current_mode=$currentMode")
                return
            }
            current = null
            currentMode = ""
            currentSessionKey = ""
        }
        if (StandaloneConfigStore(this).load().enabled) {
            standaloneRunner.refresh()
            startForeground(NOTIFICATION_ID, notification("standalone checks"))
        }
        if (!StandaloneConfigStore(this).load().enabled && !ControllerLinkStore(this).load().enabled) {
            stopWifiEvents()
        }
        if (ControllerLinkStore(this).load().enabled) {
            startForeground(NOTIFICATION_ID, notification("controller link retry"))
            queueControllerLinkLoop("session-finished")
            return
        }
        if (!shouldStayStarted()) {
            stopForeground(STOP_FOREGROUND_REMOVE)
            stopSelf()
        }
    }

    private fun startControllerLinkLoop(reason: String) {
        synchronized(sessionLock) {
            if (current?.isDone == false) return
        }
        queueControllerLinkLoop(reason)
    }

    private fun queueControllerLinkLoop(reason: String) {
        val config = ControllerLinkStore(this).load()
        if (!config.enabled) return
        synchronized(sessionLock) {
            if (current?.isDone == false) return
            currentMode = "direct-tcp"
            current = executor.submit { runControllerLinkLoop(reason) }
        }
    }

    /**
     * Repeatedly opens direct-TCP gRPC sessions using the persisted endpoint.
     *
     * The loop does not evaluate standalone measurements or push data by
     * itself. It only restores the controller command channel so the PC can pull
     * stored structured results with the normal request/show commands.
     */
    private fun runControllerLinkLoop(reason: String) {
        TerminalLog.infoEvent(this, "controller.link.loop.start", listOf("reason" to reason))
        var backoffMs = 0L
        while (!Thread.currentThread().isInterrupted) {
            val config = ControllerLinkStore(this).load()
            if (!isUsableControllerLink(config)) {
                ControllerLinkRuntimeState.markDisconnected("controller endpoint is disabled or incomplete")
                AgentStatusBroadcast.send(this)
                break
            }
            val endpoint = config.endpoint()
            TerminalLog.infoEvent(this, "controller.link.connect", listOf(
                "endpoint" to endpoint,
                "agent_id" to config.agentId,
                "adb_serial" to config.adbSerial,
                "token_present" to config.token.isNotBlank(),
            ))
            GrpcSessionClient(
                service = this,
                host = config.host,
                port = config.port,
                token = config.token,
                agentId = config.agentId,
                adbSerial = config.adbSerial,
                transport = "direct-tcp",
            ).run()
            if (Thread.currentThread().isInterrupted) break

            val latest = ControllerLinkStore(this).load()
            if (!latest.enabled) break
            val minBackoff = latest.minBackoffMs.takeIf { it > 0 }?.toLong() ?: DEFAULT_CONTROLLER_LINK_MIN_BACKOFF_MS
            val maxBackoff = latest.maxBackoffMs.takeIf { it > 0 }?.toLong() ?: DEFAULT_CONTROLLER_LINK_MAX_BACKOFF_MS
            backoffMs = if (backoffMs == 0L) minBackoff else (backoffMs * 2).coerceAtMost(maxBackoff)
            val nextRetry = System.currentTimeMillis() + backoffMs
            ControllerLinkRuntimeState.markRetryAt(nextRetry, "controller disconnected; retrying")
            AgentStatusBroadcast.send(this)
            TerminalLog.warnEvent(this, "controller.link.retry", listOf(
                "endpoint" to latest.endpoint(),
                "backoff_ms" to backoffMs,
                "next_retry_unix_time_ms" to nextRetry,
            ))
            try {
                Thread.sleep(backoffMs)
            } catch (e: InterruptedException) {
                Thread.currentThread().interrupt()
                break
            }
        }
        TerminalLog.infoEvent(this, "controller.link.loop.stop", emptyList())
        if (!StandaloneConfigStore(this).load().enabled && !ControllerLinkStore(this).load().enabled) {
            stopWifiEvents()
        }
        if (!shouldStayStarted()) {
            stopForeground(STOP_FOREGROUND_REMOVE)
            stopSelf()
        }
    }

    private fun shouldStayStarted(): Boolean {
        return StandaloneConfigStore(this).load().enabled ||
            ControllerLinkStore(this).load().enabled ||
            AgentClockWidgetProvider.hasClockWidgets(this)
    }

    private fun notificationText(action: String?): String {
        return when (action) {
            ACTION_WIDGET_REFRESH_OBSERVER -> "widget updates"
            ACTION_STANDALONE_REFRESH -> "standalone checks"
            ACTION_CONTROLLER_LINK_REFRESH -> "controller link retry"
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

    private fun isUsableControllerLink(config: io.dropcheck.agent.grpc.ControllerLinkConfig): Boolean {
        return config.enabled && config.host.isNotBlank() && config.port > 0 && config.token.isNotBlank()
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

    companion object {
        private const val CHANNEL_ID = "dropcheck-agent"
        private const val NOTIFICATION_ID = 1001
        const val ACTION_GRPC_SESSION = "io.dropcheck.agent.action.GRPC_SESSION"
        const val ACTION_STANDALONE_REFRESH = "io.dropcheck.agent.action.STANDALONE_REFRESH"
        const val ACTION_CONTROLLER_LINK_REFRESH = "io.dropcheck.agent.action.CONTROLLER_LINK_REFRESH"
        const val ACTION_WIDGET_REFRESH_OBSERVER = "io.dropcheck.agent.action.WIDGET_REFRESH_OBSERVER"
        const val ACTION_WIDGET_REFRESH_STOP = "io.dropcheck.agent.action.WIDGET_REFRESH_STOP"
        const val EXTRA_GRPC_HOST = "grpc_host"
        const val EXTRA_GRPC_PORT = "grpc_port"
        const val EXTRA_GRPC_TOKEN = "grpc_token"
        const val EXTRA_AGENT_ID = "agent_id"
        const val EXTRA_ADB_SERIAL = "adb_serial"

        fun requestStandaloneRefresh(context: Context) {
            val intent = Intent(context, AgentService::class.java).setAction(ACTION_STANDALONE_REFRESH)
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                context.startForegroundService(intent)
            } else {
                @Suppress("DEPRECATION")
                context.startService(intent)
            }
        }

        fun requestControllerLinkRefresh(context: Context) {
            val intent = Intent(context, AgentService::class.java).setAction(ACTION_CONTROLLER_LINK_REFRESH)
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                context.startForegroundService(intent)
            } else {
                @Suppress("DEPRECATION")
                context.startService(intent)
            }
        }

        fun requestWidgetObserver(context: Context) {
            val intent = Intent(context, AgentService::class.java).setAction(ACTION_WIDGET_REFRESH_OBSERVER)
            runCatching {
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                    context.startForegroundService(intent)
                } else {
                    @Suppress("DEPRECATION")
                    context.startService(intent)
                }
            }
        }

        fun requestWidgetObserverStop(context: Context) {
            val intent = Intent(context, AgentService::class.java).setAction(ACTION_WIDGET_REFRESH_STOP)
            runCatching {
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                    context.startForegroundService(intent)
                } else {
                    @Suppress("DEPRECATION")
                    context.startService(intent)
                }
            }
        }

        private const val DEFAULT_CONTROLLER_LINK_MIN_BACKOFF_MS = 1_000L
        private const val DEFAULT_CONTROLLER_LINK_MAX_BACKOFF_MS = 30_000L
    }
}
