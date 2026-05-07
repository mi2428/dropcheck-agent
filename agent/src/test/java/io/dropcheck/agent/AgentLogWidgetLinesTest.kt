package io.dropcheck.agent

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class AgentLogWidgetLinesTest {
    @Test
    fun fromTailReturnsGenerousRecentTailForWidgetClipping() {
        val tail = (0 until 300)
            .joinToString("\n") { index -> "line-$index" }

        val lines = AgentLogWidgetLines.fromTail(tail)

        assertEquals(240, lines.size)
        assertEquals("line-60", lines.first().text)
        assertEquals("line-299", lines.last().text)
    }

    @Test
    fun fromTailTruncatesLongRows() {
        val lines = AgentLogWidgetLines.fromTail("x".repeat(TerminalDisplayPolicy.MAX_LINE_CHARS + 100))

        assertEquals(1, lines.size)
        assertEquals(TerminalDisplayPolicy.MAX_LINE_CHARS, lines.single().text.length)
        assertTrue(lines.single().text.endsWith(" ... [truncated]"))
    }
}
