package io.dropcheck.agent

import org.junit.Assert.assertEquals
import org.junit.Test

class TerminalCatchUpTest {
    @Test fun priorAnchorAndBoundedGapAreDistinct() {
        assertEquals(listOf("later", "latest"), terminalCatchUp("seen", "older\nseen\nlater\nlatest"))
        assertEquals(listOf("[older stopped log lines omitted]", "later", "latest"), terminalCatchUp("evicted", "later\nlatest"))
        assertEquals(emptyList<String>(), terminalCatchUp(null, "later"))
    }
}
