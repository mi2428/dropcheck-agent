package io.dropcheck.agent

internal object TerminalDisplayPolicy {
    const val STARTUP_TAIL_LINES = 600
    const val MAX_DISPLAY_LINES = 1000
    const val MAX_DISPLAY_CHARS = 300_000
    const val MAX_LINE_CHARS = 8_000
    const val AUTO_SCROLL_SLOP_DP = 2

    fun boundedLine(line: String, appendNewline: Boolean): String {
        val displayLine = if (appendNewline) {
            if (line.endsWith("\n")) line else "$line\n"
        } else {
            line.trimEnd('\r', '\n')
        }
        if (displayLine.length <= MAX_LINE_CHARS) return displayLine

        val suffix = if (appendNewline) " ... [truncated]\n" else " ... [truncated]"
        return displayLine.take(MAX_LINE_CHARS - suffix.length).trimEnd('\r', '\n') + suffix
    }

    fun displayLength(line: String): Int {
        return terminalDisplayText(line).length
    }
}
