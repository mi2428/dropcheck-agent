package io.dropcheck.agent

import android.content.Context
import android.content.Intent
import android.util.Log
import java.io.File
import java.time.Instant

/**
 * Small append-only terminal log used by the foreground activity and gRPC logs.
 *
 * It avoids Android logging as the only source of truth because test devices are
 * often driven over adb sessions where a local tail is easier to collect.
 */
object TerminalLog {
    const val ACTION_LINE = "io.dropcheck.agent.TERMINAL_LINE"
    const val EXTRA_LINE = "line"

    private const val TAG = "dropcheck"
    private const val MAX_BYTES = 1024 * 1024
    private const val KEEP_LINES = 3000

    /** Location of the persisted terminal tail for adb collection and the on-device activity. */
    fun file(context: Context): File = File(context.getExternalFilesDir(null) ?: context.filesDir, "terminal.log")

    fun debug(context: Context, message: String) = log(context, "DEBUG", message)
    fun info(context: Context, message: String) = log(context, "INFO", message)
    fun warn(context: Context, message: String, error: Throwable? = null) = log(context, "WARN", message, error)
    fun error(context: Context, message: String, error: Throwable? = null) = log(context, "ERROR", message, error)

    /**
     * Writes one formatted line to logcat, terminal.log, and the activity broadcast.
     *
     * The method is synchronized so file trim/append operations remain ordered
     * across gRPC, command, and heartbeat executors.
     */
    @Synchronized
    fun log(context: Context, level: String, message: String, error: Throwable? = null) {
        val rendered = if (error == null) message else "$message: $error"
        val formatted = "${Instant.now()} ${level.padEnd(5)} $rendered"
        when (level) {
            "DEBUG" -> Log.d(TAG, rendered, error)
            "WARN" -> Log.w(TAG, rendered, error)
            "ERROR" -> Log.e(TAG, rendered, error)
            else -> Log.i(TAG, rendered, error)
        }

        val file = file(context)
        file.parentFile?.mkdirs()
        trimIfNeeded(file)
        file.appendText(formatted + "\n")

        context.sendBroadcast(Intent(ACTION_LINE).apply {
            setPackage(context.packageName)
            putExtra(EXTRA_LINE, formatted)
        })
    }

    /** Reads the most recent terminal lines for display when the activity opens. */
    fun tail(context: Context, maxLines: Int = 160): String {
        val file = file(context)
        if (!file.exists()) return ""
        return file.readLines().takeLast(maxLines).joinToString("\n")
    }

    private fun trimIfNeeded(file: File) {
        if (!file.exists() || file.length() < MAX_BYTES) return
        val tail = file.readLines().takeLast(KEEP_LINES).joinToString("\n")
        file.writeText(tail + "\n")
    }
}
