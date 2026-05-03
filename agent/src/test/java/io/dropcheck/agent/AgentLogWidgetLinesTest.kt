package io.dropcheck.agent

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class AgentLogWidgetLinesTest {
    @Test
    fun fromTailReturnsBoundedRecentLines() {
        val tail = (0 until TerminalDisplayPolicy.MAX_DISPLAY_LINES + 5)
            .joinToString("\n") { index -> "line-$index" }

        val lines = AgentLogWidgetLines.fromTail(tail)

        assertEquals(TerminalDisplayPolicy.MAX_DISPLAY_LINES, lines.size)
        assertEquals("line-5", lines.first().text)
        assertEquals("line-${TerminalDisplayPolicy.MAX_DISPLAY_LINES + 4}", lines.last().text)
    }

    @Test
    fun fromTailTruncatesLongRows() {
        val lines = AgentLogWidgetLines.fromTail("x".repeat(TerminalDisplayPolicy.MAX_LINE_CHARS + 100))

        assertEquals(1, lines.size)
        assertEquals(TerminalDisplayPolicy.MAX_LINE_CHARS, lines.single().text.length)
        assertTrue(lines.single().text.endsWith(" ... [truncated]"))
    }
}
