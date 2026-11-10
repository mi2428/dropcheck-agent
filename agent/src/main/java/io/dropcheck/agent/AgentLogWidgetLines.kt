package io.dropcheck.agent

import android.content.Context
import java.io.IOException
import java.io.InterruptedIOException

/** Bound the complete RemoteViews text payload, not the retained history on disk. */
internal object AgentLogWidgetLines {
    const val MAX_LINES = 80
    const val MAX_DISPLAY_CHARS = 32_768

    fun load(context: Context): List<String> {
        val lines = try { TerminalLog.page(context, maxLines = MAX_LINES).entries.map { it.text } }
        catch (error: InterruptedIOException) { throw error }
        catch (error: IOException) { return listOf("WARN ${context.getString(R.string.log_history_unavailable, error.javaClass.simpleName)}") }
        catch (error: SecurityException) { return listOf("WARN ${context.getString(R.string.log_history_unavailable, error.javaClass.simpleName)}") }
        return fromLines(lines + listOfNotNull(TerminalLog.persistenceWarning?.let { "WARN $it" }))
    }

    fun fromLines(lines: Iterable<String>): List<String> {
        val result = ArrayDeque<String>()
        var chars = 0
        for (raw in lines) {
            val line = TerminalDisplayPolicy.boundedLine(raw, appendNewline = false)
            result.addLast(line)
            chars += TerminalDisplayPolicy.displayLength(line) + 1
            while (result.size > MAX_LINES || chars > MAX_DISPLAY_CHARS) {
                chars -= TerminalDisplayPolicy.displayLength(result.removeFirst()) + 1
            }
        }
        return result.toList()
    }
}
