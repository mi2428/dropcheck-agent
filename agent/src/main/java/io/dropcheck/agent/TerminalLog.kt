package io.dropcheck.agent

import android.content.Context
import android.util.Log
import java.io.File
import java.io.IOException
import java.io.InterruptedIOException
import java.io.RandomAccessFile
import java.time.Instant
import java.util.concurrent.CopyOnWriteArraySet
import kotlin.math.min

/**
 * Small append-only terminal log used by the foreground activity and gRPC logs.
 *
 * It avoids Android logging as the only source of truth because test devices are
 * often driven over adb sessions where a local tail is easier to collect.
 */
object TerminalLog {
    private const val TAG = "dropcheck"
    private const val MAX_BYTES = 1024 * 1024
    private const val KEEP_BYTES = 512 * 1024
    private const val MAX_LINE_CHARS = 8_000
    private val listeners = CopyOnWriteArraySet<(String) -> Unit>()
    private var writeFailure: String? = null
    private var readFailure: String? = null

    /** Persistence is best-effort, but its failure must stay visible until recovery. */
    @get:Synchronized val persistenceWarning: String? get() = writeFailure ?: readFailure

    /** Location of the persisted terminal tail for adb collection and the on-device activity. */
    fun file(context: Context): File = File(context.getExternalFilesDir(null) ?: context.filesDir, "terminal.log")

    fun debug(context: Context, message: String) = log(context, "DEBUG", message)
    fun info(context: Context, message: String) = log(context, "INFO", message)
    fun warn(context: Context, message: String, error: Throwable? = null) = log(context, "WARN", message, error)
    fun error(context: Context, message: String, error: Throwable? = null) = log(context, "ERROR", message, error)

    fun addListener(listener: (String) -> Unit) {
        listeners.add(listener)
    }

    /** Atomically returns a bounded tail and subscribes to later lines. */
    @Synchronized fun addListenerWithTail(context: Context, listener: (String) -> Unit, maxLines: Int): String {
        val snapshot = tail(context, maxLines)
        listeners.add(listener)
        return snapshot
    }

    fun removeListener(listener: (String) -> Unit) {
        listeners.remove(listener)
    }

    /** Compacts an oversized persisted log without appending a new line. */
    @Synchronized
    fun compactIfNeeded(context: Context) {
        storageAttempt { compactFileIfNeeded(file(context)) }.exceptionOrNull()?.let {
            writeFailure = "Terminal log not saved (${it.javaClass.simpleName}); operations continue"
        }
    }

    /**
     * Writes one formatted line to logcat, terminal.log, and in-process listeners.
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

        val previous = writeFailure
        if (!record({ file(context) }, formatted) { line ->
            listeners.forEach { listener -> runCatching { listener(line) } }
        } && writeFailure != previous) Log.w(TAG, writeFailure, null)
        AgentLogWidgetProvider.requestUpdate(context)
    }

    /** Keep live delivery even when persistence fails; do not hide missing log intervals. */
    @Synchronized
    internal fun record(file: () -> File, line: String, publish: (String) -> Unit): Boolean {
        val previous = writeFailure
        val recovered = previous?.let { "${Instant.now()} WARN  Terminal log storage recovered; earlier lines were not saved" }
        val result = storageAttempt {
            val target = file()
            target.parentFile?.mkdirs()
            compactFileIfNeeded(target)
            target.appendText((recovered?.plus("\n") ?: "") + line + "\n")
        }
        writeFailure = result.exceptionOrNull()?.let {
            "Terminal log not saved (${it.javaClass.simpleName}); operations continue"
        }
        if (result.isSuccess && recovered != null) publish(recovered)
        if (writeFailure != null && writeFailure != previous) publish("${Instant.now()} WARN  $writeFailure")
        publish(line)
        return result.isSuccess
    }

    /** Reads the most recent terminal lines for display when the activity opens. */
    @Synchronized
    fun tail(context: Context, maxLines: Int = 160): String = readTail({ file(context) }, maxLines)

    @Synchronized
    internal fun readTail(file: () -> File, maxLines: Int): String {
        val result = storageAttempt {
            val target = file()
            if (!target.exists()) "" else tailText(target).lineSequence().toList().takeLast(maxLines).joinToString("\n")
        }
        readFailure = result.exceptionOrNull()?.let {
            "Saved terminal log cannot be read (${it.javaClass.simpleName}); operations continue"
        }
        return listOfNotNull(persistenceWarning?.let { "WARN  $it" }, result.getOrNull()?.takeIf(String::isNotEmpty)).joinToString("\n")
    }

    private inline fun <T> storageAttempt(action: () -> T): Result<T> = try {
        Result.success(action())
    } catch (error: InterruptedIOException) {
        Thread.currentThread().interrupt()
        throw error
    } catch (error: IOException) {
        if (Thread.currentThread().isInterrupted) throw error
        Result.failure(error)
    } catch (error: SecurityException) {
        Result.failure(error)
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
        if (!file.isFile) throw IOException("terminal log is not a regular file")
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
