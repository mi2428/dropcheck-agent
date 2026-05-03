package io.dropcheck.agent

import android.content.Context
import android.content.Intent
import android.util.Log
import java.io.File
import java.io.RandomAccessFile
import java.time.Instant
import kotlin.math.min

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
    private const val KEEP_BYTES = 512 * 1024
    private const val MAX_LINE_CHARS = 8_000

    /** Location of the persisted terminal tail for adb collection and the on-device activity. */
    fun file(context: Context): File = File(context.getExternalFilesDir(null) ?: context.filesDir, "terminal.log")

    fun debug(context: Context, message: String) = log(context, "DEBUG", message)
    fun info(context: Context, message: String) = log(context, "INFO", message)
    fun warn(context: Context, message: String, error: Throwable? = null) = log(context, "WARN", message, error)
    fun error(context: Context, message: String, error: Throwable? = null) = log(context, "ERROR", message, error)

    /** Compacts an oversized persisted log without appending a new line. */
    @Synchronized
    fun compactIfNeeded(context: Context) {
        compactFileIfNeeded(file(context))
    }

    /**
     * Writes one formatted line to logcat, terminal.log, and the activity broadcast.
     *
     * The method is synchronized so file trim/append operations remain ordered
     * across gRPC, command, and heartbeat executors.
     */
    @Synchronized
    fun log(context: Context, level: String, message: String, error: Throwable? = null) {
        val rendered = boundedLine(if (error == null) message else "$message: $error")
        val formatted = "${Instant.now()} ${level.padEnd(5)} $rendered"
        when (level) {
            "DEBUG" -> Log.d(TAG, rendered, error)
            "WARN" -> Log.w(TAG, rendered, error)
            "ERROR" -> Log.e(TAG, rendered, error)
            else -> Log.i(TAG, rendered, error)
        }

        val file = file(context)
        file.parentFile?.mkdirs()
        compactFileIfNeeded(file)
        file.appendText(formatted + "\n")

        context.sendBroadcast(Intent(ACTION_LINE).apply {
            setPackage(context.packageName)
            putExtra(EXTRA_LINE, formatted)
        })
        AgentLogWidgetProvider.requestUpdate(context)
    }

    /** Reads the most recent terminal lines for display when the activity opens. */
    @Synchronized
    fun tail(context: Context, maxLines: Int = 160): String {
        val file = file(context)
        if (!file.exists()) return ""
        return tailText(file)
            .lineSequence()
            .toList()
            .takeLast(maxLines)
            .joinToString("\n")
    }

    @Synchronized
    internal fun compactFileIfNeeded(file: File) {
        if (!file.exists() || file.length() < MAX_BYTES) return
        val tail = tailText(file)
        file.writeText(tail + "\n")
    }

    private fun boundedLine(value: String): String {
        if (value.length <= MAX_LINE_CHARS) return value
        val suffix = " ... [truncated]"
        return value.take(MAX_LINE_CHARS - suffix.length).trimEnd('\r', '\n') + suffix
    }

    private fun tailText(file: File): String {
        val length = file.length()
        if (length <= 0) return ""
        val keep = min(length, KEEP_BYTES.toLong()).toInt()
        val bytes = ByteArray(keep)
        RandomAccessFile(file, "r").use { reader ->
            reader.seek(length - keep)
            reader.readFully(bytes)
        }
        var text = bytes.toString(Charsets.UTF_8)
        if (length > keep) {
            val firstNewline = text.indexOf('\n')
            if (firstNewline >= 0) {
                text = text.drop(firstNewline + 1)
            }
        }
        return text.trimEnd('\r', '\n')
    }
}
