package io.dropcheck.agent

import java.io.BufferedInputStream
import java.io.ByteArrayOutputStream
import java.io.File
import java.io.FileInputStream
import java.io.FileOutputStream
import java.io.IOException
import java.io.RandomAccessFile
import java.nio.ByteBuffer
import java.nio.charset.CharacterCodingException
import java.nio.charset.CodingErrorAction

internal data class TerminalLogPosition(val generation: Long, val offset: Long)
internal data class TerminalLogEntry(val position: TerminalLogPosition?, val text: String)
internal data class TerminalLogPage(val entries: List<TerminalLogEntry>, val hasOlder: Boolean, val hasNewer: Boolean)

internal object TerminalLogHistory {
    const val SEGMENT_BYTES = 4 * 1024 * 1024L
    const val MAX_HISTORY_BYTES = 64 * 1024 * 1024L
    const val MAX_PAGE_BYTES = 256 * 1024
    const val MAX_PAGE_CHARS = 256 * 1024
    const val MAX_PAGE_LINES = 256
    private const val IO_BLOCK_BYTES = 8 * 1024
    private const val MAX_SAVED_LINE_BYTES = TerminalLog.MAX_LINE_CHARS * 4
    private const val SEGMENT_SUFFIX = ".segment"

    private data class Source(val generation: Long, val file: File)
    private data class Scan(val entries: List<TerminalLogEntry>, val bytes: Int, val stoppedEarly: Boolean, val nextOffset: Long? = null)

    @Synchronized fun append(file: File, line: String): List<TerminalLogEntry> {
        prepare(file)
        // Retry failed retention before accepting more bytes into the store.
        retain(file)
        file.parentFile?.mkdirs()
        var generation = nextGeneration(file)
        val entries = ArrayList<TerminalLogEntry>()
        val lines = line.split('\n').let { if (line.endsWith('\n')) it.dropLast(1) else it }
        for (text in lines) {
            val physical = text.trimEnd('\r')
            val bytes = (physical + "\n").toByteArray(Charsets.UTF_8)
            var length = file.length()
            val needsSeparator = length > 0 && RandomAccessFile(file, "r").use { it.seek(length - 1); it.read() != '\n'.code }
            if (length > 0 && length + (if (needsSeparator) 1 else 0) + bytes.size > SEGMENT_BYTES) {
                val closed = segment(file, generation)
                if (closed.exists()) throw IOException("terminal log segment already exists")
                if (!file.renameTo(closed)) throw IOException("terminal log rotation failed")
                generation = nextGeneration(file)
                length = 0
            } else if (needsSeparator) {
                RandomAccessFile(file, "rw").use { output -> output.seek(length); output.writeByte('\n'.code) }
                length++
            }
            RandomAccessFile(file, "rw").use { output -> output.seek(length); output.write(bytes) }
            entries += TerminalLogEntry(TerminalLogPosition(generation, length), physical)
        }
        retain(file)
        return entries
    }

    @Synchronized fun compact(file: File) {
        prepare(file)
        val generation = nextGeneration(file)
        if (!file.isFile || file.length() <= SEGMENT_BYTES) return
        val closed = segment(file, generation)
        if (closed.exists() || !file.renameTo(closed)) throw IOException("terminal log rotation failed")
        retain(file)
    }

    @Synchronized fun page(file: File, before: TerminalLogPosition? = null, after: TerminalLogPosition? = null, maxLines: Int = 256): TerminalLogPage {
        require(before == null || after == null) { "before and after are mutually exclusive" }
        require(maxLines > 0)
        val lineLimit = minOf(maxLines, MAX_PAGE_LINES)
        prepare(file)
        val sources = sources(file)
        if (sources.isEmpty()) return TerminalLogPage(emptyList(), false, false)
        val anchor = before ?: after
        val anchorIndex = if (anchor == null) sources.lastIndex else sources.indexOfFirst { it.generation == anchor.generation }
        if (anchor != null && anchorIndex < 0) throw IOException("terminal log cursor has expired")
        if (anchor != null) validateCursor(sources[anchorIndex], anchor)

        val entries = ArrayList<TerminalLogEntry>()
        var budget = MAX_PAGE_BYTES
        if (after != null) {
            var index = anchorIndex
            val anchorLength = lineLengthAt(sources[index].file, after.offset, budget)
            budget -= anchorLength.toInt()
            var offset = after.offset + anchorLength
            var lastSourceIndex = anchorIndex
            var lastEnd = offset
            var returned = false
            while (index < sources.size && entries.size < lineLimit && budget > 0) {
                val source = sources[index]
                val scan = readForward(source, if (index == anchorIndex) offset else 0L, lineLimit - entries.size, budget)
                entries += scan.entries
                if (scan.nextOffset != null) {
                    lastSourceIndex = index
                    lastEnd = scan.nextOffset
                    returned = true
                }
                budget -= scan.bytes
                if (!scan.stoppedEarly) { index++; offset = 0L } else break
            }
            val hasNewer = if (returned) lastSourceIndex < sources.lastIndex || lastEnd < sources[lastSourceIndex].file.length()
            else anchorIndex < sources.lastIndex || offset < sources[anchorIndex].file.length()
            return TerminalLogPage(entries, anchorIndex > 0 || after.offset > 0, hasNewer)
        }

        var index = if (before == null) sources.lastIndex else anchorIndex
        val end = before?.offset
        while (index >= 0 && entries.size < lineLimit && budget > 0) {
            val source = sources[index]
            val scan = readBackward(source, if (index == anchorIndex && before != null) end!! else source.file.length(), lineLimit - entries.size, budget)
            entries.addAll(0, scan.entries)
            budget -= scan.bytes
            if (!scan.stoppedEarly) index-- else break
        }
        val hasOlder = entries.firstOrNull()?.position?.let { p -> sources.first().generation < p.generation || p.offset > 0 } ?: false
        val hasNewer = before != null
        return TerminalLogPage(entries, hasOlder, hasNewer)
    }

    private fun readForward(source: Source, start: Long, maxLines: Int, budget: Int): Scan {
        val rows = ArrayList<TerminalLogEntry>()
        var used = 0
        var stopped = false
        var nextOffset: Long? = null
        RandomAccessFile(source.file, "r").use { input ->
            val length = input.length()
            if (start < 0 || start > length) throw IOException("terminal log cursor offset is invalid")
            input.seek(start)
            val reader = ForwardBlocks(input, start, length, budget)
            while (reader.position < length && rows.size < maxLines && used < budget) {
                val rowStart = reader.position
                val row = ByteArrayOutputStream()
                var terminated = false
                while (reader.position < length && used < budget) {
                    val byte = reader.next() ?: break
                    used++
                    if (byte == '\n'.code) { terminated = true; break }
                    if (row.size() == MAX_PAGE_BYTES) throw IOException("terminal log line exceeds page byte limit")
                    row.write(byte)
                }
                if (!terminated && reader.position < length) {
                    stopped = true
                    if (budget == MAX_PAGE_BYTES && rows.isEmpty()) throw IOException("terminal log line exceeds page byte limit")
                    break
                }
                if (row.size() > 0 || terminated) {
                    rows += TerminalLogEntry(TerminalLogPosition(source.generation, rowStart), decode(row.toByteArray()).trimEnd('\r'))
                    nextOffset = reader.position
                }
                if (!terminated) break
            }
            if (rows.size == maxLines && reader.position < length) stopped = true
            if (used >= budget && reader.position < length) stopped = true
        }
        return Scan(rows, used, stopped, nextOffset)
    }

    private fun readBackward(source: Source, end: Long, maxLines: Int, budget: Int): Scan {
        if (end < 0 || end > source.file.length()) throw IOException("terminal log cursor offset is invalid")
        if (end == 0L || maxLines <= 0 || budget <= 0) return Scan(emptyList(), 0, false)
        val rows = ArrayList<TerminalLogEntry>()
        var used = 0
        var stopped = false
        RandomAccessFile(source.file, "r").use { input ->
            val reader = ReverseBlocks(input, end, budget)
            var position = end
            var forceEmptyRow = false
            if (reader.peek() == '\n'.code) { reader.next(); position--; used++; forceEmptyRow = true }
            while ((position > 0 || forceEmptyRow) && rows.size < maxLines && used < budget) {
                forceEmptyRow = false
                val reversed = ByteArrayOutputStream()
                var separator = false
                while (position > 0 && used < budget) {
                    val byte = reader.next() ?: break
                    used++
                    position--
                    if (byte == '\n'.code) { separator = true; break }
                    if (reversed.size() == MAX_PAGE_BYTES) throw IOException("terminal log line exceeds page byte limit")
                    reversed.write(byte)
                }
                if (!separator && position > 0) {
                    stopped = true
                    if (budget == MAX_PAGE_BYTES && rows.isEmpty()) throw IOException("terminal log line exceeds page byte limit")
                    break
                }
                val rowStart = if (separator) position + 1 else 0L
                val bytes = reversed.toByteArray().reversedArray()
                rows += TerminalLogEntry(TerminalLogPosition(source.generation, rowStart), decode(bytes).trimEnd('\r'))
                if (separator && position == 0L) forceEmptyRow = true
                if (!separator) break
            }
            if (rows.size == maxLines && (position > 0 || forceEmptyRow)) stopped = true
            if (used >= budget && (position > 0 || forceEmptyRow)) stopped = true
        }
        rows.reverse()
        return Scan(rows, used, stopped)
    }

    private fun lineLengthAt(file: File, offset: Long, budget: Int): Long {
        RandomAccessFile(file, "r").use { input ->
            val length = input.length()
            if (offset < 0 || offset >= length) throw IOException("terminal log cursor offset is invalid")
            input.seek(offset)
            val reader = ForwardBlocks(input, offset, length, budget)
            var count = 0
            while (reader.position < length && count < budget) {
                if (reader.next() == '\n'.code) return (reader.position - offset)
                count++
            }
            if (reader.position == length) return length - offset
        }
        throw IOException("terminal log line exceeds page byte limit")
    }

    private fun validateCursor(source: Source, position: TerminalLogPosition) {
        RandomAccessFile(source.file, "r").use { input ->
            if (position.offset < 0 || position.offset >= input.length()) throw IOException("terminal log cursor offset is invalid")
            if (position.offset > 0) {
                input.seek(position.offset - 1)
                if (input.read() != '\n'.code) throw IOException("terminal log cursor is not at a row boundary")
            }
        }
    }

    private fun prepare(file: File) {
        if (file.exists() && !file.isFile) throw IOException("terminal log is not a regular file")
        val prefix = archivePrefix(file)
        if (directoryFiles(file).any { it.name == "${prefix}migration-backup" || it.name.startsWith("${prefix}migration-") }) {
            throw IOException("incomplete terminal log migration")
        }
        if (file.isFile && file.length() > SEGMENT_BYTES) migrateLargeCurrent(file)
    }

    private fun sources(file: File): List<Source> {
        val prefix = archivePrefix(file)
        val closed = directoryFiles(file).mapNotNull { candidate ->
            val name = candidate.name
            if (!name.startsWith(prefix) || !name.endsWith(SEGMENT_SUFFIX)) return@mapNotNull null
            val generation = name.removePrefix(prefix).removeSuffix(SEGMENT_SUFFIX).toLongOrNull()
                ?: throw IOException("invalid terminal log segment name")
            if (!candidate.isFile) throw IOException("terminal log segment is not a regular file")
            Source(generation, candidate)
        }.sortedBy { it.generation }
        if (closed.zipWithNext().any { it.first.generation == it.second.generation }) throw IOException("duplicate terminal log segment generation")
        val current = if (file.isFile && file.length() > 0) listOf(Source(nextGeneration(file), file)) else emptyList()
        return closed + current
    }

    private fun nextGeneration(file: File): Long {
        val prefix = archivePrefix(file)
        val max = directoryFiles(file).mapNotNull { candidate ->
            val name = candidate.name
            if (!name.startsWith(prefix) || !name.endsWith(SEGMENT_SUFFIX)) return@mapNotNull null
            if (!candidate.isFile) throw IOException("terminal log segment is not a regular file")
            name.removePrefix(prefix).removeSuffix(SEGMENT_SUFFIX).toLongOrNull()
                ?: throw IOException("invalid terminal log segment name")
        }.maxOrNull() ?: 0L
        if (max == Long.MAX_VALUE) throw IOException("terminal log generation exhausted")
        return max + 1
    }

    private fun retain(file: File) {
        val closed = sources(file).filter { it.file != file }
        var size = closed.sumOf { it.file.length() } + if (file.isFile) file.length() else 0L
        for (source in closed) {
            if (size <= MAX_HISTORY_BYTES) break
            val length = source.file.length()
            if (!source.file.delete()) throw IOException("terminal log retention failed")
            size -= length
        }
    }

    private fun migrateLargeCurrent(file: File) {
        val oldSize = file.length()
        val earliest = maxOf(0L, oldSize - MAX_HISTORY_BYTES)
        val start = if (earliest == 0L) 0L else findNextLine(file, earliest)
        if (start < 0) throw IOException("oversized terminal log has no complete recent line")
        var generation = nextGeneration(file)
        val staged = ArrayList<Pair<File, File>>()
        var output: FileOutputStream? = null
        var outputBytes = 0L
        val moved = ArrayList<File>()
        var backup: File? = null
        var committed = false
        try {
            BufferedInputStream(FileInputStream(file), IO_BLOCK_BYTES).use { input ->
                var skipped = 0L
                while (skipped < start) {
                    val n = input.skip(start - skipped)
                    if (n <= 0) throw IOException("terminal log migration seek failed")
                    skipped += n
                }
                var row = ByteArrayOutputStream()
                var dropping = false
                var foundAny = false
                while (true) {
                    val byte = input.read()
                    if (byte < 0 || byte == '\n'.code) {
                        if (byte < 0 && row.size() == 0) break
                        var raw = row.toByteArray()
                        if (dropping) raw = truncateUtf8(raw, MAX_SAVED_LINE_BYTES - 32)
                        var text = decode(raw).trimEnd('\r')
                        if (dropping) text = safePrefix(text, 8_000 - 16) + " ... [truncated]"
                        val line = (text + "\n").toByteArray(Charsets.UTF_8)
                        if (output == null || outputBytes + line.size > SEGMENT_BYTES) {
                            output?.close()
                            generation++
                            val temp = migrationFile(file, generation)
                            if (temp.exists()) throw IOException("terminal log migration staging collision")
                            output = FileOutputStream(temp)
                            staged.add(temp to segment(file, generation))
                            outputBytes = 0
                        }
                        output!!.write(line)
                        outputBytes += line.size
                        foundAny = true
                        row = ByteArrayOutputStream()
                        dropping = false
                        if (byte < 0) break
                    } else if (row.size() < MAX_SAVED_LINE_BYTES) row.write(byte) else dropping = true
                }
                if (!foundAny) throw IOException("oversized terminal log contains no complete lines")
            }
            output?.close()
            val marker = migrationMarker(file)
            if (!marker.createNewFile()) throw IOException("terminal log migration already in progress")
            for ((temp, destination) in staged) {
                if (destination.exists() || !temp.renameTo(destination)) throw IOException("terminal log migration install failed")
                moved += destination
            }
            val currentBackup = File(file.parentFile, migrationBackupName(file))
            backup = currentBackup
            if (currentBackup.exists() || !file.renameTo(currentBackup)) throw IOException("terminal log migration staging failed")
            if (!file.createNewFile()) {
                throw IOException("terminal log migration current file creation failed")
            }
            committed = true
            if (!currentBackup.delete()) throw IOException("terminal log migration cleanup failed")
            backup = null
            retain(file)
            if (!marker.delete()) throw IOException("terminal log migration marker cleanup failed")
        } catch (error: Exception) {
            output?.close()
            staged.forEach { it.first.delete() }
            if (!committed) {
                if (backup != null && backup.exists()) {
                    if (file.exists() && !file.delete()) throw IOException("terminal log migration rollback failed", error)
                    if (!backup.renameTo(file)) throw IOException("terminal log migration rollback failed", error)
                }
                for (destination in moved) {
                    if (destination.exists() && !destination.delete()) throw IOException("terminal log migration rollback failed", error)
                }
                val marker = migrationMarker(file)
                if (marker.exists() && !marker.delete()) throw IOException("terminal log migration rollback failed", error)
            }
            throw error
        }
    }

    private fun findNextLine(file: File, offset: Long): Long {
        RandomAccessFile(file, "r").use { input ->
            if (offset > 0) {
                input.seek(offset - 1)
                if (input.read() == '\n'.code) return offset
            }
            input.seek(offset)
            val bytes = ByteArray(IO_BLOCK_BYTES)
            var position = offset
            while (position < input.length()) {
                val count = input.read(bytes, 0, minOf(bytes.size.toLong(), input.length() - position).toInt())
                if (count < 0) break
                for (i in 0 until count) if (bytes[i] == '\n'.code.toByte()) return position + i + 1
                position += count
            }
        }
        return -1
    }

    private fun truncateUtf8(bytes: ByteArray, max: Int): ByteArray {
        var length = minOf(bytes.size, max)
        while (length > 0) {
            try { decode(bytes.copyOf(length)); return bytes.copyOf(length) }
            catch (_: IOException) { length-- }
        }
        return byteArrayOf()
    }

    private fun safePrefix(value: String, maxChars: Int): String {
        var end = minOf(value.length, maxChars)
        if (end > 0 && end < value.length && value[end - 1].isHighSurrogate()) end--
        return value.substring(0, end)
    }

    private fun decode(bytes: ByteArray): String = try {
        Charsets.UTF_8.newDecoder().onMalformedInput(CodingErrorAction.REPORT).onUnmappableCharacter(CodingErrorAction.REPORT).decode(ByteBuffer.wrap(bytes)).toString()
    } catch (error: CharacterCodingException) { throw IOException("terminal log contains invalid UTF-8", error) }

    private fun archivePrefix(file: File) = ".${file.name}."
    private fun directoryFiles(file: File): Array<File> {
        val parent = file.absoluteFile.parentFile ?: throw IOException("terminal log has no parent directory")
        if (!parent.exists()) return emptyArray()
        return parent.listFiles() ?: throw IOException("terminal log directory cannot be read")
    }
    private fun segment(file: File, generation: Long) = File(file.parentFile, "${archivePrefix(file)}$generation$SEGMENT_SUFFIX")
    private fun migrationFile(file: File, generation: Long) = File(file.parentFile, "${archivePrefix(file)}migration-$generation")
    private fun migrationBackupName(file: File) = ".${file.name}.migration-backup"
    private fun migrationMarker(file: File) = File(file.parentFile, ".${file.name}.migration-in-progress")

    private class ForwardBlocks(private val input: RandomAccessFile, start: Long, private val end: Long, private val limit: Int) {
        private val buffer = ByteArray(IO_BLOCK_BYTES)
        private var index = 0
        private var count = 0
        private var loaded = 0
        var position = start
            private set
        fun next(): Int? {
            if (position >= end) return null
            if (index >= count) {
                count = input.read(buffer, 0, minOf(minOf(buffer.size, limit - loaded).toLong(), end - position).toInt())
                if (count <= 0) return null
                loaded += count
                index = 0
            }
            position++
            return buffer[index++].toInt() and 0xff
        }
    }

    private class ReverseBlocks(private val input: RandomAccessFile, end: Long, private val limit: Int) {
        private val buffer = ByteArray(IO_BLOCK_BYTES)
        private var index = 0
        private var loaded = 0
        var position = end
            private set
        fun peek(): Int? {
            if (position <= 0) return null
            if (index == 0) load()
            return buffer[index - 1].toInt() and 0xff
        }
        fun next(): Int? {
            val value = peek() ?: return null
            index--
            position--
            return value
        }
        private fun load() {
            val count = minOf(minOf(buffer.size, limit - loaded).toLong(), position).toInt()
            if (count <= 0) return
            input.seek(position - count)
            input.readFully(buffer, 0, count)
            index = count
            loaded += count
        }
    }
}
