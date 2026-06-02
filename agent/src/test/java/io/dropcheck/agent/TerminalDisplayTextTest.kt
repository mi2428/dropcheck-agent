package io.dropcheck.agent

import org.junit.Assert.assertEquals
import org.junit.Test

class TerminalDisplayTextTest {
    @Test
    fun addsBreakOpportunitiesAfterVisibleCodePoints() {
        assertEquals("a\u200Bb\u200Bc\u200B\nx\u200By\u200B", terminalDisplayText("abc\nxy"))
    }

    @Test
    fun preservesSupplementaryCodePoints() {
        val supplementary = String(Character.toChars(0x1F600))

        assertEquals(
            "a\u200B${supplementary}\u200Bb\u200B\n",
            terminalDisplayText("a${supplementary}b\n"),
        )
    }

    @Test
    fun boundsTerminalLinesForActivityAndWidgetDisplay() {
        assertEquals("line\n", TerminalDisplayPolicy.boundedLine("line", appendNewline = true))
        assertEquals("line", TerminalDisplayPolicy.boundedLine("line\r\n", appendNewline = false))

        val longLine = "x".repeat(TerminalDisplayPolicy.MAX_LINE_CHARS + 100)
        val bounded = TerminalDisplayPolicy.boundedLine(longLine, appendNewline = true)

        assertEquals(TerminalDisplayPolicy.MAX_LINE_CHARS, bounded.length)
        assertEquals(true, bounded.endsWith(" ... [truncated]\n"))
    }

    @Test
    fun displayLengthCountsInsertedBreakOpportunities() {
        assertEquals(4, TerminalDisplayPolicy.displayLength("ab"))
        assertEquals(3, TerminalDisplayPolicy.displayLength("a\n"))
    }

    @Test
    fun trimPlanRemovesOldestLinesUntilBoundsFit() {
        val plan = TerminalDisplayPolicy.trimPlan(
            lineLengths = listOf(
                TerminalDisplayPolicy.MAX_LINE_CHARS,
                TerminalDisplayPolicy.MAX_LINE_CHARS,
                10,
            ),
            displayChars = TerminalDisplayPolicy.MAX_DISPLAY_CHARS + TerminalDisplayPolicy.MAX_LINE_CHARS + 10,
        )

        assertEquals(2, plan.linesToRemove)
        assertEquals(TerminalDisplayPolicy.MAX_LINE_CHARS * 2, plan.charsToRemove)
    }
}
