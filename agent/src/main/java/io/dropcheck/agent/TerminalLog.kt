package io.dropcheck.agent

import android.content.Context
import android.util.Log
import java.io.File
import java.io.IOException
import java.io.InterruptedIOException
import java.time.Instant
import java.util.concurrent.CopyOnWriteArraySet

/** Append-only terminal history shared by live listeners and the on-device log viewer. */
object TerminalLog {
    private const val TAG = "dropcheck"
    internal const val MAX_LINE_CHARS = 8_000
    private val listeners = CopyOnWriteArraySet<(TerminalLogEntry) -> Unit>()
    @Volatile private var writeFailure: String? = null
    @Volatile private var readFailure: String? = null

    val persistenceWarning: String? get() = writeFailure ?: readFailure
    fun file(context: Context): File = File(context.getExternalFilesDir(null) ?: context.filesDir, "terminal.log")
    fun debug(context: Context, message: String) = log(context, "DEBUG", message)
    fun info(context: Context, message: String) = log(context, "INFO", message)
    fun warn(context: Context, message: String, error: Throwable? = null) = log(context, "WARN", message, error)
    fun error(context: Context, message: String, error: Throwable? = null) = log(context, "ERROR", message, error)

    internal fun addListener(listener: (TerminalLogEntry) -> Unit) { listeners.add(listener) }
    internal fun removeListener(listener: (TerminalLogEntry) -> Unit) { listeners.remove(listener) }

    @Synchronized fun compactIfNeeded(context: Context) {
        storageAttempt { compactFileIfNeeded(file(context)) }.exceptionOrNull()?.let {
            writeFailure = "Terminal log not saved (${it.javaClass.simpleName}); operations continue"
        }
    }

    @Synchronized fun log(context: Context, level: String, message: String, error: Throwable? = null) {
        val rendered = boundedLine(if (error == null) message else "$message: $error")
        val formatted = "${Instant.now()} ${level.padEnd(5)} $rendered"
        when (level) {
            "DEBUG" -> Log.d(TAG, rendered, error)
            "WARN" -> Log.w(TAG, rendered, error)
            "ERROR" -> Log.e(TAG, rendered, error)
            else -> Log.i(TAG, rendered, error)
        }
        val previous = writeFailure
        if (!record({ file(context) }, formatted) { entry -> listeners.forEach { listener -> runCatching { listener(entry) } } } && writeFailure != previous) Log.w(TAG, writeFailure, null)
        AgentLogWidgetProvider.requestUpdate(context)
    }

    @Synchronized internal fun record(file: () -> File, line: String, publish: (TerminalLogEntry) -> Unit): Boolean {
        val previous = writeFailure
        val recovered = previous?.let { "${Instant.now()} WARN  Terminal log storage recovered; earlier lines were not saved" }
        val userLines = physicalLines(line).map { boundedLine(it.trimEnd('\r')) }
        val saved = ArrayList<TerminalLogEntry>()
        var recoverySaved = false
        val result = storageAttempt {
            val target = file()
            target.parentFile?.mkdirs()
            compactFileIfNeeded(target)
            if (recovered != null) {
                saved += TerminalLogHistory.append(target, recovered)
                recoverySaved = true
            }
            userLines.forEach { saved += TerminalLogHistory.append(target, it) }
        }
        writeFailure = result.exceptionOrNull()?.let { "Terminal log not saved (${it.javaClass.simpleName}); operations continue" }
        saved.forEach(publish)
        if (result.isFailure) {
            if (writeFailure != previous) publish(TerminalLogEntry(null, "${Instant.now()} WARN  $writeFailure"))
            userLines.drop((saved.size - if (recoverySaved) 1 else 0).coerceAtLeast(0)).forEach { publish(TerminalLogEntry(null, it)) }
        }
        return result.isSuccess
    }

    @Synchronized fun tail(context: Context, maxLines: Int = 160): String = readTail({ file(context) }, maxLines)

    @Synchronized internal fun readTail(file: () -> File, maxLines: Int): String {
        val result = storageAttempt {
            val target = file()
            TerminalLogHistory.page(target, null, null, maxLines).entries.joinToString("\n") { it.text }
        }
        readFailure = result.exceptionOrNull()?.let { "Saved terminal log cannot be read (${it.javaClass.simpleName}); operations continue" }
        return listOfNotNull(persistenceWarning?.let { "WARN  $it" }, result.getOrNull()?.takeIf(String::isNotEmpty)).joinToString("\n")
    }

    @Synchronized internal fun page(context: Context, before: TerminalLogPosition? = null, after: TerminalLogPosition? = null, maxLines: Int = 256): TerminalLogPage = pageInternal({ file(context) }, before, after, maxLines)

    @Synchronized internal fun readPage(file: () -> File, before: TerminalLogPosition? = null, after: TerminalLogPosition? = null, maxLines: Int = 256): TerminalLogPage = pageInternal(file, before, after, maxLines)

    private fun pageInternal(file: () -> File, before: TerminalLogPosition?, after: TerminalLogPosition?, maxLines: Int): TerminalLogPage {
        require(before == null || after == null) { "before and after are mutually exclusive" }
        val result = storageAttempt { TerminalLogHistory.page(file(), before, after, maxLines) }
        readFailure = result.exceptionOrNull()?.let { "Saved terminal log cannot be read (${it.javaClass.simpleName}); operations continue" }
        return result.getOrThrow()
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

    @Synchronized internal fun compactFileIfNeeded(file: File) { TerminalLogHistory.compact(file) }

    private fun boundedLine(value: String): String {
        if (value.length <= MAX_LINE_CHARS) return value
        val suffix = " ... [truncated]"
        var end = MAX_LINE_CHARS - suffix.length
        if (value[end - 1].isHighSurrogate()) end--
        return value.substring(0, end).trimEnd('\r', '\n') + suffix
    }

    private fun physicalLines(value: String): List<String> = value.split('\n').let { if (value.endsWith('\n')) it.dropLast(1) else it }
}
