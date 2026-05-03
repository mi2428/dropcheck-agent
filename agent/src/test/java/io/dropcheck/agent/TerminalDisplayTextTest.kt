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
}
