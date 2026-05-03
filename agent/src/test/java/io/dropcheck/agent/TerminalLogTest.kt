package io.dropcheck.agent

import java.nio.file.Files
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class TerminalLogTest {
    @Test
    fun compactsOversizedFileToRecentTail() {
        val file = Files.createTempFile("terminal-log", ".log").toFile()
        try {
            file.writeText(buildString {
                repeat(80_000) { index ->
                    append("old-$index filler filler filler filler filler filler filler filler\n")
                }
                append("recent-marker\n")
            })
            val before = file.length()

            TerminalLog.compactFileIfNeeded(file)

            val after = file.length()
            val text = file.readText()
            assertTrue(after < before)
            assertTrue(after < 1024 * 1024)
            assertFalse(text.contains("old-0"))
            assertTrue(text.endsWith("recent-marker\n"))
        } finally {
            file.delete()
        }
    }

    @Test
    fun leavesSmallFileUnchanged() {
        val file = Files.createTempFile("terminal-log", ".log").toFile()
        try {
            file.writeText("small\n")

            TerminalLog.compactFileIfNeeded(file)

            assertEquals("small\n", file.readText())
        } finally {
            file.delete()
        }
    }
}
