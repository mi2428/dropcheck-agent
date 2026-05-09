package io.dropcheck.agent

import android.annotation.SuppressLint
import android.content.Context
import android.os.Build
import android.provider.Settings
import io.dropcheck.agent.grpc.CommandLog
import io.dropcheck.agent.grpc.CommandResult
import io.dropcheck.agent.grpc.ConnectWifi
import io.dropcheck.agent.grpc.RunCommand
import io.dropcheck.agent.grpc.StandaloneConfig
import io.dropcheck.agent.grpc.StandaloneRunArchive
import java.net.HttpURLConnection
import java.net.URL

/**
 * Pushes stored standalone archives to an unauthenticated S3-compatible HTTP endpoint.
 *
 * The configured URL is treated as a path-style bucket/prefix URL, for example
 * http://192.168.50.10:8080/dropcheck/incoming. Each run is written as
 * <url>/<device-key>/<run-id>.pb and is deleted locally only after HTTP 2xx.
 */
internal class StandaloneUploader(
    context: Context,
    private val resultStore: StandaloneResultStore,
) {
    private val appContext = context.applicationContext
    private val logger = terminalLogger()

    fun flush(config: StandaloneConfig): String {
        val upload = config.upload
        val baseUrl = upload.url.trim().trimEnd('/')
        val wifi = upload.wifi
        if (baseUrl.isBlank() || wifi.ssid.isBlank()) return ""

        return AgentCommandLock.run {
            val pending = resultStore.list(includeSynced = false, limit = 0)
                .runsList
                .sortedBy { it.startedUnixMs }
            if (pending.isEmpty()) return@run ""

            val connect = connectManagementWifi(wifi)
            if (connect.status != CommandResult.Status.STATUS_OK) {
                val message = "standalone upload skipped: management wifi failed: ${connect.message.ifBlank { connect.status.name }}"
                TerminalLog.warn(appContext, message)
                return@run message
            }

            var uploaded = 0
            var lastStatusCode = 0
            for (summary in pending) {
                val archive = resultStore.load(summary.runId) ?: continue
                val result = putArchive(baseUrl, archive)
                if (!result.ok) {
                    val message = "standalone upload stopped: ${result.message}"
                    TerminalLog.warn(appContext, message)
                    return@run message
                }
                lastStatusCode = result.statusCode
                if (resultStore.delete(summary.runId)) {
                    uploaded += 1
                }
            }
            val message = "standalone upload completed: uploaded=$uploaded last_http_status=$lastStatusCode"
            TerminalLog.info(appContext, message)
            message
        }
    }

    private fun connectManagementWifi(wifi: ConnectWifi): CommandResult {
        val command = RunCommand.newBuilder()
            .setLabel("standalone upload management wifi ${wifi.ssid}")
            .setConnectWifi(wifi)
            .build()
        return CommandExecutor(appContext, logger).execute(command)
    }

    private fun putArchive(baseUrl: String, archive: StandaloneRunArchive): UploadResult {
        val target = objectUrl(baseUrl, archive.summary.runId)
        val payload = archive.toByteArray()
        val connection = (URL(target).openConnection() as HttpURLConnection).apply {
            requestMethod = "PUT"
            doOutput = true
            connectTimeout = 10_000
            readTimeout = 30_000
            setRequestProperty("Content-Type", "application/x-protobuf")
            setRequestProperty("User-Agent", "dropcheck-agent/${Build.MODEL}")
            setFixedLengthStreamingMode(payload.size)
        }
        return try {
            connection.outputStream.use { it.write(payload) }
            val code = connection.responseCode
            if (code in 200..299) {
                TerminalLog.info(appContext, "standalone upload ok run_id=${archive.summary.runId} bytes=${payload.size} url=$target status=$code")
                UploadResult(ok = true, message = "uploaded ${archive.summary.runId}", statusCode = code)
            } else {
                val body = runCatching {
                    (connection.errorStream ?: connection.inputStream)?.bufferedReader()?.use { it.readText() }
                }.getOrNull().orEmpty().take(200)
                UploadResult(ok = false, message = "run_id=${archive.summary.runId} status=$code body=${body.ifBlank { "-" }}", statusCode = code)
            }
        } catch (e: Exception) {
            UploadResult(ok = false, message = "run_id=${archive.summary.runId} error=${e.javaClass.simpleName}:${e.message.orEmpty()}")
        } finally {
            connection.disconnect()
        }
    }

    private fun objectUrl(baseUrl: String, runId: String): String {
        return "$baseUrl/${safePathComponent(deviceKey())}/${safePathComponent(runId)}.pb"
    }

    @SuppressLint("HardwareIds")
    private fun deviceKey(): String {
        // Standalone uploads need a stable per-device object prefix inside the
        // event bucket; it is not used for advertising or cross-app tracking.
        val androidId = runCatching {
            Settings.Secure.getString(appContext.contentResolver, Settings.Secure.ANDROID_ID)
        }.getOrNull().orEmpty()
        return listOf(Build.MANUFACTURER, Build.MODEL, androidId)
            .filter { it.isNotBlank() }
            .joinToString("-")
            .ifBlank { "unknown-device" }
    }

    private fun safePathComponent(value: String): String {
        return value.trim().replace(Regex("[^A-Za-z0-9._-]+"), "_").trim('_').ifBlank { "unknown" }
    }

    private fun terminalLogger(): CommandLogger {
        return object : CommandLogger {
            override fun log(level: CommandLog.Level, message: String, scope: CommandLogScope) {
                TerminalLog.log(appContext, terminalLevelName(level), CommandTerminalLog.standalone(scope, message))
            }
        }
    }

    private fun terminalLevelName(level: CommandLog.Level): String = when (level) {
        CommandLog.Level.LEVEL_DEBUG -> "DEBUG"
        CommandLog.Level.LEVEL_WARN -> "WARN"
        CommandLog.Level.LEVEL_ERROR -> "ERROR"
        else -> "INFO"
    }

    private data class UploadResult(
        val ok: Boolean,
        val message: String,
        val statusCode: Int = 0,
    )
}
