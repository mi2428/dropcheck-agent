package io.dropcheck.agent

import java.nio.file.Files
import java.io.InterruptedIOException
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class TerminalLogTest {
    @Test
    fun storageFaultKeepsLiveLinesAndAnExplicitWarningUntilRecovery() {
        val root = Files.createTempDirectory("terminal-storage").toFile()
        try {
            val blocked = root.resolve("terminal.log").apply { mkdir() }
            blocked.resolve("marker").writeText("unchanged")
            val live = mutableListOf<TerminalLogEntry>()
            for (target in listOf<() -> java.io.File>({ blocked }, { throw SecurityException("password=must-not-leak") })) {
                live.clear()
                assertFalse(TerminalLog.record(target, "live synthetic line", live::add))
                assertEquals("live synthetic line", live.last().text)
                assertTrue(live.any { "Terminal log not saved" in it.text })
                assertFalse(live.joinToString { it.text }.contains("must-not-leak"))
            }
            assertTrue(TerminalLog.readTail({ blocked }, 10).contains("Terminal log not saved"))
            assertEquals("unchanged", blocked.resolve("marker").readText())

            val healthy = root.resolve("healthy.log")
            val beforeRecovery = live.size
            assertTrue(TerminalLog.record({ healthy }, "saved synthetic line", live::add))
            assertTrue(healthy.readText().contains("earlier lines were not saved"))
            val recoveredEntries = live.drop(beforeRecovery).filter { "earlier lines were not saved" in it.text }
            assertEquals(1, recoveredEntries.size)
            assertTrue(recoveredEntries.single().position != null)
            // A successful write must not erase an independently failed history read.
            assertTrue(TerminalLog.persistenceWarning!!.contains("cannot be read"))
            assertTrue(TerminalLog.readTail({ healthy }, 10).contains("saved synthetic line"))
            assertEquals(null, TerminalLog.persistenceWarning)
        } finally { root.deleteRecursively() }
    }

    @Test
    fun storageDegradationDoesNotSwallowCancellationOrExecutionErrors() {
        for (error in listOf(InterruptedIOException("synthetic cancellation"), IllegalStateException("synthetic execution error"))) {
            try {
                TerminalLog.record({ throw error }, "must not publish", { throw AssertionError("unexpected live line") })
                throw AssertionError("failure was swallowed")
            } catch (actual: Exception) {
                assertTrue(actual === error)
                if (error is InterruptedIOException) assertTrue(Thread.currentThread().isInterrupted)
            } finally { Thread.interrupted() }
        }
    }
    @Test
    fun compactsOversizedFileToRecentTail() {
        val root = Files.createTempDirectory("terminal-log").toFile()
        val file = root.resolve("terminal.log")
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
            assertTrue(after <= before)
            val segments = root.listFiles()!!.filter { it.name.endsWith(".segment") }
            assertTrue(segments.isNotEmpty())
            assertTrue(segments.all { it.length() <= TerminalLogHistory.SEGMENT_BYTES })
            assertTrue(segments.maxBy { it.name.removePrefix(".terminal.log.").removeSuffix(".segment").toLong() }.readText().endsWith("recent-marker\n"))
            assertTrue(text.isEmpty())
        } finally { root.deleteRecursively() }
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
