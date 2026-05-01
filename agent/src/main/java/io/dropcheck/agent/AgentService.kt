package io.dropcheck.agent

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.Service
import android.content.Intent
import android.os.Build
import android.os.IBinder
import java.util.concurrent.Executors
import java.util.concurrent.Future

/**
 * Foreground service entry point for controller-initiated gRPC sessions.
 *
 * The service performs only Android lifecycle work: it validates the start
 * intent, keeps one active session, and tears the worker down when Android
 * stops the service.
 */
class AgentService : Service() {
    private val executor = Executors.newSingleThreadExecutor()
    private lateinit var wifiEvents: WifiEventLogger
    private var current: Future<*>? = null

    override fun onCreate() {
        super.onCreate()
        ensureNotificationChannel()
        wifiEvents = WifiEventLogger(this)
        wifiEvents.start()
        TerminalLog.infoEvent(this, "service.create", listOf(
            "sdk" to Build.VERSION.SDK_INT,
            "manufacturer" to Build.MANUFACTURER,
            "model" to Build.MODEL,
            "device" to Build.DEVICE,
        ))
    }

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        startForeground(NOTIFICATION_ID, notification("waiting for controller"))
        TerminalLog.infoEvent(this, "service.start", listOf(
            "action" to intent?.action,
            "start_id" to startId,
            "flags" to flags,
            "extras" to intent?.extras?.keySet()?.toList().orEmpty(),
        ))
        when (intent?.action) {
            ACTION_GRPC_SESSION -> {
                val host = intent.getStringExtra(EXTRA_GRPC_HOST) ?: "127.0.0.1"
                val port = intent.getIntExtra(EXTRA_GRPC_PORT, 0)
                val token = intent.getStringExtra(EXTRA_GRPC_TOKEN).orEmpty()
                val agentId = intent.getStringExtra(EXTRA_AGENT_ID).orEmpty()
                val adbSerial = intent.getStringExtra(EXTRA_ADB_SERIAL).orEmpty()
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
                    startGrpcSession(host, port, token, agentId, adbSerial)
                }
            }
            else -> TerminalLog.warnEvent(this, "service.start.ignored", listOf(
                "reason" to "unknown_action",
                "action" to intent?.action,
            ))
        }
        return START_NOT_STICKY
    }

    override fun onDestroy() {
        current?.cancel(true)
        if (::wifiEvents.isInitialized) {
            wifiEvents.stop()
        }
        executor.shutdownNow()
        TerminalLog.infoEvent(this, "service.destroy", emptyList())
        super.onDestroy()
    }

    /**
     * Starts one gRPC session worker.
     *
     * The service rejects concurrent sessions because command execution is
     * single-threaded and each APK instance represents one physical device.
     */
    private fun startGrpcSession(host: String, port: Int, token: String, agentId: String, adbSerial: String) {
        if (current?.isDone == false) {
            TerminalLog.warnEvent(this, "grpc.session.rejected", listOf(
                "reason" to "already_active",
                "host" to host,
                "port" to port,
                "agent_id" to agentId,
                "adb_serial" to adbSerial,
                "token_present" to token.isNotBlank(),
            ))
            return
        }
        TerminalLog.infoEvent(this, "grpc.session.queued", listOf(
            "host" to host,
            "port" to port,
            "agent_id" to agentId,
            "adb_serial" to adbSerial,
            "token_present" to token.isNotBlank(),
        ))
        current = executor.submit {
            TerminalLog.debugEvent(this, "grpc.session.worker.start", listOf(
                "thread" to Thread.currentThread().name,
                "host" to host,
                "port" to port,
                "agent_id" to agentId,
                "adb_serial" to adbSerial,
            ))
            try {
                GrpcSessionClient(this, host, port, token, agentId, adbSerial).run()
            } finally {
                TerminalLog.infoEvent(this, "grpc.session.finished", listOf(
                    "host" to host,
                    "port" to port,
                    "agent_id" to agentId,
                    "adb_serial" to adbSerial,
                ))
                stopForeground(STOP_FOREGROUND_REMOVE)
                stopSelf()
            }
        }
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
        const val EXTRA_GRPC_HOST = "grpc_host"
        const val EXTRA_GRPC_PORT = "grpc_port"
        const val EXTRA_GRPC_TOKEN = "grpc_token"
        const val EXTRA_AGENT_ID = "agent_id"
        const val EXTRA_ADB_SERIAL = "adb_serial"
    }
}
