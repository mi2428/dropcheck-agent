package io.dropcheck.agent

import android.content.Context

/** Loads enough terminal log rows for the fixed-size log widget to clip to its bounds. */
internal object AgentLogWidgetLines {
    private const val MAX_WIDGET_TAIL_LINES = 240
    private const val MAX_WIDGET_DISPLAY_CHARS = 80_000

    fun load(context: Context): List<WidgetLogLine> {
        return fromTail(TerminalLog.tail(context, MAX_WIDGET_TAIL_LINES))
    }

    fun fromTail(tail: String): List<WidgetLogLine> {
        val displayLines = ArrayDeque<WidgetLogLine>()
        var displayChars = 0

        tail.lineSequence()
            .forEach { rawLine ->
                val line = TerminalDisplayPolicy.boundedLine(rawLine, appendNewline = false)
                val lineChars = TerminalDisplayPolicy.displayLength(line) + 1
                displayLines.addLast(WidgetLogLine(id = line.hashCode().toLong(), text = line))
                displayChars += lineChars

                while (
                    displayLines.isNotEmpty() &&
                    (displayLines.size > MAX_WIDGET_TAIL_LINES || displayChars > MAX_WIDGET_DISPLAY_CHARS)
                ) {
                    displayChars -= TerminalDisplayPolicy.displayLength(displayLines.removeFirst().text) + 1
                }
            }
        return displayLines.toList()
    }
}

/** Stable row model rendered into the fixed log widget text. */
internal data class WidgetLogLine(
    val id: Long,
    val text: String,
)
