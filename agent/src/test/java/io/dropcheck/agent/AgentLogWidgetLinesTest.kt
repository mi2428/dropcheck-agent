package io.dropcheck.agent

import org.junit.Assert.*
import org.junit.Test

class AgentLogWidgetLinesTest {
    @Test fun boundsRowsAndDecoratedCharactersWithoutLosingTheNewestRow() {
        val rows = (0 until 500).map { "INFO row-$it ${"日本語".repeat(100)}" }
        val bounded = AgentLogWidgetLines.fromLines(rows)
        assertTrue(bounded.size in 1..AgentLogWidgetLines.MAX_LINES)
        assertTrue(bounded.sumOf { TerminalDisplayPolicy.displayLength(it) + 1 } <= AgentLogWidgetLines.MAX_DISPLAY_CHARS)
        assertEquals(rows.last(), bounded.last())
        assertEquals(AgentLogWidgetLines.MAX_LINES, AgentLogWidgetLines.fromLines(List(500) { "short" }).size)
    }

    @Test fun emptyInputIsNotABlankLogAndMaximumRowsStillFit() {
        assertTrue(AgentLogWidgetLines.fromLines(emptyList()).isEmpty())
        assertEquals(listOf(""), AgentLogWidgetLines.fromLines(listOf("")))
        val rows = AgentLogWidgetLines.fromLines(List(100) { "x".repeat(9_000) })
        assertTrue(rows.isNotEmpty())
        assertEquals(TerminalDisplayPolicy.MAX_LINE_CHARS, rows.last().length)
        assertTrue(rows.last().endsWith(" ... [truncated]"))
        assertTrue(rows.sumOf { TerminalDisplayPolicy.displayLength(it) + 1 } <= AgentLogWidgetLines.MAX_DISPLAY_CHARS)
    }

    @Test fun truncationDoesNotSplitASupplementaryCodePoint() {
        val suffix = " ... [truncated]"
        val prefix = "x".repeat(TerminalDisplayPolicy.MAX_LINE_CHARS - suffix.length - 1)
        assertEquals(prefix + suffix, AgentLogWidgetLines.fromLines(listOf(prefix + "🙂".repeat(100))).single())
    }
}
