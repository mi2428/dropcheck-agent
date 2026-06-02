package io.dropcheck.agent

/** Shared bounds for terminal rendering. */
internal object TerminalDisplayPolicy {
    data class TrimPlan(
        val linesToRemove: Int = 0,
        val charsToRemove: Int = 0,
    )

    const val STARTUP_TAIL_LINES = 1_800
    const val MAX_DISPLAY_LINES = 3_000
    const val MAX_DISPLAY_CHARS = 900_000
    const val MAX_LINE_CHARS = 8_000
    const val AUTO_SCROLL_SLOP_DP = 2

    fun trimPlan(lineLengths: Collection<Int>, displayChars: Int): TrimPlan {
        if (lineLengths.size <= MAX_DISPLAY_LINES && displayChars <= MAX_DISPLAY_CHARS) {
            return TrimPlan()
        }

        var remainingLines = lineLengths.size
        var remainingChars = displayChars
        var linesToRemove = 0
        var charsToRemove = 0
        for (lineLength in lineLengths) {
            if (remainingLines <= MAX_DISPLAY_LINES && remainingChars <= MAX_DISPLAY_CHARS) {
                break
            }
            linesToRemove += 1
            charsToRemove += lineLength
            remainingLines -= 1
            remainingChars -= lineLength
        }
        return TrimPlan(linesToRemove = linesToRemove, charsToRemove = charsToRemove)
    }

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
