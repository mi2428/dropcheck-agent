package io.dropcheck.agent

import java.nio.file.Files
import java.io.RandomAccessFile
import java.io.FileOutputStream
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class TerminalLogHistoryTest {
    @Test fun invalidArchiveStopsAppendBeforeChangingTheCurrentFile() {
        val root = Files.createTempDirectory("terminal-invalid-archive").toFile()
        try {
            val file = root.resolve("terminal.log").apply { writeText("preserve\n") }
            root.resolve(".terminal.log.1.segment").mkdir()
            var rejected = false
            try { TerminalLogHistory.append(file, "must not append") } catch (_: java.io.IOException) { rejected = true }
            assertTrue(rejected)
            assertEquals("preserve\n", file.readText())
        } finally { root.deleteRecursively() }
    }

    @Test fun inaccessibleHistoryDirectoryIsNotAnEmptySuccessfulPage() {
        val root = Files.createTempDirectory("terminal-directory-failure").toFile()
        try {
            val blocked = root.resolve("not-a-directory").apply { writeText("preserve") }
            var rejected = false
            try { TerminalLog.readPage({ blocked.resolve("terminal.log") }) } catch (_: java.io.IOException) { rejected = true }
            assertTrue(rejected)
            assertEquals("preserve", blocked.readText())
        } finally {
            TerminalLog.readPage({ root.resolve("healthy.log") })
            root.deleteRecursively()
        }
    }

    @Test fun widgetTailCanReadClosedHistoryWithoutACurrentFile() {
        val root = Files.createTempDirectory("terminal-closed-tail").toFile()
        try {
            root.resolve(".terminal.log.1.segment").writeText("old\nlatest\n")
            val file = root.resolve("terminal.log")
            assertFalse(file.exists())
            assertEquals("latest", TerminalLog.readTail({ file }, 1))
        } finally { root.deleteRecursively() }
    }

    @Test fun pagesKeepDuplicateRowsAndUtf8PositionsAcrossRestartAndRotation() {
        val root = Files.createTempDirectory("terminal-pages").toFile()
        val file = root.resolve("terminal.log")
        try {
            val live = mutableListOf<TerminalLogEntry>()
            assertTrue(TerminalLog.record({ file }, "same\nsame\n日本語\r\n", live::add))
            assertEquals(listOf("same", "same", "日本語"), live.map { it.text })
            val last = TerminalLog.readPage({ file }, maxLines = 2)
            assertEquals(listOf("same", "日本語"), last.entries.map { it.text })
            assertTrue(last.entries[0].position != last.entries[1].position)
            assertTrue(last.hasOlder)
            val older = TerminalLog.readPage({ file }, before = last.entries.first().position, maxLines = 2)
            assertEquals(listOf("same"), older.entries.map { it.text })
            assertTrue(older.entries.single().position != last.entries.first().position)
            assertEquals(last.entries.first().position, TerminalLog.readPage({ file }, after = older.entries.last().position, maxLines = 2).entries.first().position)

            val additional = (0 until 530).map { index -> "$index ${"x".repeat(7_980)}" }
            val expected = listOf("same", "same", "日本語") + additional
            additional.forEach { assertTrue(TerminalLog.record({ file }, it, { })) }
            val rotated = TerminalLog.readPage({ file }, maxLines = 2)
            assertEquals(2, rotated.entries.size)
            assertTrue(root.listFiles()!!.any { it.name.endsWith(".segment") })
            val savedCursor = rotated.entries.first().position!!
            val restarted = TerminalLog.readPage({ file }, before = rotated.entries.first().position, maxLines = 1)
            assertEquals(savedCursor.generation, restarted.entries.last().position!!.generation)
            assertTrue(rotated.entries.sumOf { it.text.length } <= TerminalLogHistory.MAX_PAGE_CHARS)

            val backwardPages = ArrayList<List<String>>()
            var backwardPage = rotated
            while (true) {
                assertTrue(backwardPage.entries.size <= TerminalLogHistory.MAX_PAGE_LINES)
                assertTrue(backwardPage.entries.sumOf { it.text.toByteArray().size } <= TerminalLogHistory.MAX_PAGE_BYTES)
                backwardPages += backwardPage.entries.map { it.text }
                if (!backwardPage.hasOlder) break
                backwardPage = TerminalLog.readPage({ file }, before = backwardPage.entries.first().position, maxLines = 100)
            }
            assertEquals(expected, backwardPages.asReversed().flatten())

            var oldestPage = TerminalLog.readPage({ file }, maxLines = 100)
            while (oldestPage.hasOlder) oldestPage = TerminalLog.readPage({ file }, before = oldestPage.entries.first().position, maxLines = 100)
            val forward = ArrayList<String>().apply { addAll(oldestPage.entries.map { it.text }) }
            var anchor = oldestPage.entries.last().position
            while (anchor != null) {
                val page = TerminalLog.readPage({ file }, after = anchor, maxLines = 100)
                assertTrue(page.entries.size <= TerminalLogHistory.MAX_PAGE_LINES)
                assertTrue(page.entries.sumOf { it.text.toByteArray().size } <= TerminalLogHistory.MAX_PAGE_BYTES)
                forward.addAll(page.entries.map { it.text })
                anchor = if (page.hasNewer) page.entries.lastOrNull()?.position else null
            }
            assertEquals(expected, forward)
        } finally { root.deleteRecursively() }
    }

    @Test fun staleCursorAndAmbiguousAnchorsFailAndPageBudgetIsPublic() {
        val root = Files.createTempDirectory("terminal-stale-cursor").toFile()
        val file = root.resolve("terminal.log")
        try {
            TerminalLog.record({ file }, "one\ntwo", { })
            val cursor = TerminalLogPosition(Long.MAX_VALUE, 0)
            var rejected = false
            try { TerminalLog.readPage({ file }, before = cursor, after = cursor) } catch (_: IllegalArgumentException) { rejected = true }
            assertTrue(rejected)
            file.writeText("new history\n")
            rejected = false
            try { TerminalLog.readPage({ file }, after = cursor) } catch (_: java.io.IOException) { rejected = true }
            assertTrue(rejected)
            assertTrue(TerminalLogHistory.MAX_PAGE_BYTES > 0)
            assertFalse(TerminalLogHistory.MAX_PAGE_BYTES.toLong() > TerminalLogHistory.MAX_HISTORY_BYTES)
        } finally { root.deleteRecursively() }
    }

    @Test fun retentionDeletesOldestUsingActualSegmentBytes() {
        val root = Files.createTempDirectory("terminal-retention").toFile()
        val file = root.resolve("terminal.log")
        try {
            repeat(16) { index ->
                RandomAccessFile(root.resolve(".terminal.log.${index + 1}.segment"), "rw").use {
                    if (index == 0) it.write("old\n".toByteArray())
                    it.setLength(TerminalLogHistory.SEGMENT_BYTES)
                }
            }
            val survivor = root.resolve(".terminal.log.17.segment").apply { writeText("later\n") }
            val survivorBytes = survivor.readBytes()
            val stale = TerminalLogPosition(1, 0)
            TerminalLogHistory.append(file, "latest")
            val storedBytes = root.listFiles()!!.filter { it.name.endsWith(".segment") || it.name == file.name }.sumOf { it.length() }
            assertTrue(storedBytes <= TerminalLogHistory.MAX_HISTORY_BYTES)
            assertFalse(root.resolve(".terminal.log.1.segment").exists())
            assertTrue(survivor.exists())
            assertEquals(survivorBytes.toList(), survivor.readBytes().toList())
            assertEquals(listOf("latest"), TerminalLogHistory.page(file, after = TerminalLogPosition(17, 0), maxLines = 1).entries.map { it.text })
            var expired = false
            try { TerminalLogHistory.page(file, after = stale, maxLines = 1) } catch (_: java.io.IOException) { expired = true }
            assertTrue(expired)
        } finally { root.deleteRecursively() }
    }

    @Test fun appendSeparatesUnterminatedCurrentAndNamespacesDifferentFiles() {
        val root = Files.createTempDirectory("terminal-file-namespaces").toFile()
        val terminal = root.resolve("terminal.log")
        val healthy = root.resolve("healthy.log")
        try {
            terminal.writeText("seed")
            assertTrue(TerminalLog.record({ terminal }, "next", { }))
            assertEquals("seed\nnext\n", terminal.readText())
            assertEquals(listOf("seed", "next"), TerminalLog.readPage({ terminal }, maxLines = 4).entries.map { it.text })
            assertTrue(TerminalLog.record({ healthy }, "isolated", { }))
            assertEquals(listOf("isolated"), TerminalLog.readPage({ healthy }, maxLines = 4).entries.map { it.text })
            assertFalse(TerminalLog.readPage({ terminal }, maxLines = 4).entries.any { it.text == "isolated" })
        } finally { root.deleteRecursively() }
    }

    @Test fun pageByteLimitNeverReturnsPartialUtf8AndOversizedLineFails() {
        val root = Files.createTempDirectory("terminal-byte-budget").toFile()
        val file = root.resolve("terminal.log")
        try {
            assertTrue(TerminalLog.record({ file }, "🙂".repeat(1_000), { }))
            assertEquals("🙂".repeat(1_000), TerminalLog.readPage({ file }, maxLines = 1).entries.single().text)
            FileOutputStream(file).use { it.write(("é".repeat(TerminalLogHistory.MAX_PAGE_BYTES) + "\n").toByteArray()) }
            var rejected = false
            try { TerminalLog.readPage({ file }, maxLines = 1) } catch (_: java.io.IOException) { rejected = true }
            assertTrue(rejected)
        } finally { root.deleteRecursively() }
    }

    @Test fun oversizedLegacyCurrentMigratesNewestRowsIntoBoundedGenerations() {
        val root = Files.createTempDirectory("terminal-legacy-migration").toFile()
        val file = root.resolve("terminal.log")
        try {
            FileOutputStream(file).buffered().use { output ->
                repeat(530) { index -> output.write("legacy-$index ${"z".repeat(7_990)}\n".toByteArray()) }
            }
            val oldCursor = TerminalLogPosition(1, 0)
            val latest = TerminalLogHistory.page(file, maxLines = 1)
            assertTrue(latest.entries.single().text.startsWith("legacy-529"))
            val segments = root.listFiles()!!.filter { it.name.endsWith(".segment") }
            assertTrue(segments.size > 1)
            assertTrue(segments.all { it.length() <= TerminalLogHistory.SEGMENT_BYTES })
            assertEquals(0L, file.length())
            assertFalse(root.listFiles()!!.any { it.name.contains("migration-") || it.name.endsWith("migration-in-progress") })
            var expired = false
            try { TerminalLogHistory.page(file, after = oldCursor, maxLines = 1) } catch (_: java.io.IOException) { expired = true }
            assertTrue(expired)
        } finally { root.deleteRecursively() }
    }

    @Test fun hugeLegacyCurrentKeepsNewestTailWithinHistoryBound() {
        val root = Files.createTempDirectory("terminal-huge-legacy").toFile()
        val file = root.resolve("terminal.log")
        try {
            RandomAccessFile(file, "rw").use { output ->
                output.setLength(TerminalLogHistory.MAX_HISTORY_BYTES + 6 * 1024 * 1024)
                output.seek(output.length() - "\nrecent-marker\n".toByteArray().size)
                output.write("\nrecent-marker\n".toByteArray())
            }
            val page = TerminalLogHistory.page(file, maxLines = 10)
            assertEquals(listOf("recent-marker"), page.entries.map { it.text })
            val storedBytes = root.listFiles()!!.filter { it.name.endsWith(".segment") || it.name == file.name }.sumOf { it.length() }
            assertTrue(storedBytes <= TerminalLogHistory.MAX_HISTORY_BYTES)
        } finally { root.deleteRecursively() }
    }

    @Test fun interruptedMigrationMarkerFailsClosedWithoutChangingSource() {
        val root = Files.createTempDirectory("terminal-migration-interrupted").toFile()
        val file = root.resolve("terminal.log")
        try {
            RandomAccessFile(file, "rw").use { output ->
                output.setLength(TerminalLogHistory.SEGMENT_BYTES + 32)
                output.seek(output.length() - 5)
                output.write("end\n".toByteArray())
            }
            val length = file.length()
            val tail = RandomAccessFile(file, "r").use { input -> input.seek(length - 5); ByteArray(5).also { input.readFully(it) } }
            root.resolve(".terminal.log.migration-in-progress").createNewFile()
            var rejected = false
            try { TerminalLogHistory.page(file, maxLines = 1) } catch (_: java.io.IOException) { rejected = true }
            assertTrue(rejected)
            assertEquals(length, file.length())
            assertEquals(tail.toList(), RandomAccessFile(file, "r").use { input -> input.seek(length - 5); ByteArray(5).also { input.readFully(it) }.toList() })
        } finally { root.deleteRecursively() }
    }
}
