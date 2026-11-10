package io.dropcheck.agent

import java.io.ByteArrayOutputStream
import java.io.File
import java.io.FileOutputStream
import java.io.IOException
import java.io.InputStream
import java.io.InterruptedIOException
import java.net.HttpURLConnection
import java.net.URL
import java.nio.ByteBuffer
import java.nio.charset.CodingErrorAction
import java.nio.file.Files
import java.nio.file.StandardCopyOption
import java.time.Instant
import java.util.zip.GZIPInputStream
import java.util.zip.GZIPOutputStream

/** Offline IEEE assignee lookup. A complete validated update replaces one snapshot atomically. */
internal class OuiDatabase(private val file: File?) {
    data class Info(val fetchedAt: String, val assignments: Int, val source: String)
    private data class Snapshot(val fetchedAt: String, val entries: Map<String, String>)
    private var snapshot: Snapshot? = null
    private var local = false

    @Synchronized fun info(): Info {
        val data = load()
        return Info(data.fetchedAt, data.entries.size, if (local) "local IEEE update" else "bundled IEEE snapshot")
    }

    @Synchronized fun vendor(address: String): String {
        val key = when {
            address.matches(Regex("[0-9a-fA-F]{2}(:[0-9a-fA-F]{2}){5}")) -> address.replace(":", "")
            address.matches(Regex("[0-9a-fA-F]{2}(-[0-9a-fA-F]{2}){5}")) -> address.replace("-", "")
            address.matches(Regex("[0-9a-fA-F]{4}(\\.[0-9a-fA-F]{4}){2}")) -> address.replace(".", "")
            else -> return "? (invalid MAC)"
        }.uppercase(java.util.Locale.ROOT)
        if (key == "000000000000") return "? (unspecified)"
        val first = key.take(2).toInt(16)
        if (first and 1 != 0) return "? (group MAC)"
        if (first and 2 != 0) return "? (local MAC)"
        val data = try { load() } catch (failure: IOException) {
            if (Thread.currentThread().isInterrupted) throw failure
            return "? (OUI database error: ${failure.message})"
        }
        for (digits in listOf(9, 7, 6)) {
            val prefix = key.take(digits)
            data.entries[prefix]?.let { return it }
        }
        return "? (unregistered)"
    }

    private fun load(): Snapshot {
        snapshot?.let { return it }
        fun bundled() = OuiDatabase::class.java.getResourceAsStream("/registry.tsv.gz") ?: throw IOException("bundled OUI snapshot missing")
        val input = if (file == null) bundled() else try {
            Files.newInputStream(file.toPath()).also { local = true }
        } catch (_: java.nio.file.NoSuchFileException) { bundled() }
        val data = input.use { decode(readBounded(it)) }
        snapshot = data
        return data
    }

    @Synchronized fun update(fetch: (String, Long) -> ByteArray = ::download): Info {
        val destination = file ?: throw IOException("OUI update storage is not configured")
        val entries = sortedMapOf<String, String>()
        val deadline = System.nanoTime() + 60_000_000_000L
        for ((path, digits) in sources) {
            checkDeadline(deadline)
            for ((prefix, name) in parseIEEE(fetch(path, deadline), digits)) {
                val previous = entries.put(prefix, name)
                if (previous != null && previous != name) throw IOException("conflicting IEEE prefix $prefix")
            }
        }
        checkDeadline(deadline)
        val fetched = Instant.now().toString()
        val bytes = ByteArrayOutputStream().apply {
            GZIPOutputStream(this).bufferedWriter(Charsets.UTF_8).use { out ->
                out.appendLine(HEADER + fetched)
                entries.forEach { (prefix, name) -> out.appendLine("$prefix\t$name") }
            }
        }.toByteArray()
        Files.createDirectories(destination.toPath().parent)
        val temporary = Files.createTempFile(destination.toPath().parent, ".oui-", ".tmp")
        try {
            FileOutputStream(temporary.toFile()).use { it.write(bytes); it.fd.sync() }
            checkDeadline(deadline)
            Files.move(temporary, destination.toPath(), StandardCopyOption.ATOMIC_MOVE, StandardCopyOption.REPLACE_EXISTING)
        } finally { Files.deleteIfExists(temporary) }
        snapshot = Snapshot(fetched, entries)
        local = true
        return info()
    }

    companion object {
        @Volatile var current = OuiDatabase(null)
        private const val MAX_BYTES = 16 * 1024 * 1024
        private const val HEADER = "# dropcheck-oui-v1\t"
        private val sources = listOf("oui/oui" to 6, "oui28/mam" to 7, "oui36/oui36" to 9, "iab/iab" to 9)
        private val hexLine = Regex("^([0-9A-F]{2}-[0-9A-F]{2}-[0-9A-F]{2})\\s+\\(hex\\)\\s+(.+)$", RegexOption.DOT_MATCHES_ALL)
        private val baseLine = Regex("^([0-9A-F]{6})(?:-([0-9A-F]{6}))?\\s+\\(base 16\\)\\s+(.+)$", RegexOption.DOT_MATCHES_ALL)
        private val prefixPattern = Regex("(?:[0-9A-F]{6}|[0-9A-F]{7}|[0-9A-F]{9})")

        private fun validName(name: String) = name.isNotEmpty() && name.toByteArray(Charsets.UTF_8).size <= 2048 && name.none { it.isISOControl() }
        private fun utf8(data: ByteArray) = Charsets.UTF_8.newDecoder().onMalformedInput(CodingErrorAction.REPORT)
            .onUnmappableCharacter(CodingErrorAction.REPORT).decode(ByteBuffer.wrap(data)).toString()

        internal fun parseIEEE(data: ByteArray, digits: Int): Map<String, String> {
            if (data.size > MAX_BYTES) throw IOException("oversized IEEE registry")
            val text = utf8(data)
            if (!text.take(1024).contains("Organization") || digits !in listOf(6, 7, 9)) throw IOException("invalid IEEE registry header")
            val assignments = mutableMapOf<String, MutableSet<String>>()
            var pending = ""
            for (line in text.lineSequence()) {
                if (Thread.currentThread().isInterrupted) throw InterruptedIOException("OUI parsing canceled")
                hexLine.matchEntire(line)?.let {
                    if (pending.isNotEmpty()) throw IOException("incomplete IEEE assignment")
                    pending = it.groupValues[1].replace("-", "")
                }
                if (!line.contains("(base 16)")) continue
                val match = baseLine.matchEntire(line) ?: throw IOException("malformed IEEE assignment")
                if (pending.isEmpty()) throw IOException("IEEE assignment lacks OUI")
                val start = match.groupValues[1]
                val end = match.groupValues[2]
                val prefix = if (digits == 6) {
                    if (start != pending || end.isNotEmpty()) throw IOException("inconsistent IEEE MA-L assignment")
                    start
                } else {
                    val n = digits - 6
                    if (start.drop(n) != "0".repeat(6 - n) || end != start.take(n) + "F".repeat(6 - n)) throw IOException("inconsistent IEEE assignment range")
                    pending + start.take(n)
                }
                val name = match.groupValues[3].replace(Regex("[\\s\\p{Z}\\u0085]+"), " ").trim()
                if (!validName(name)) throw IOException("invalid IEEE assignment $prefix")
                assignments.getOrPut(prefix) { sortedSetOf() }.add(name)
                pending = ""
            }
            if (assignments.isEmpty() || pending.isNotEmpty()) throw IOException("empty or incomplete IEEE registry")
            return assignments.mapValues { (_, names) -> (if (names.size > 1) "multiple IEEE assignees: " else "") + names.joinToString(" / ") }
        }

        private fun decode(data: ByteArray): Snapshot {
            val text = GZIPInputStream(data.inputStream()).use { utf8(readBounded(it)) }
            val lines = text.removeSuffix("\n").split('\n')
            if (lines.size < 2 || !lines.first().startsWith(HEADER)) throw IOException("invalid OUI snapshot header")
            val fetched = lines.first().removePrefix(HEADER)
            try { Instant.parse(fetched) } catch (failure: java.time.format.DateTimeParseException) { throw IOException("invalid OUI snapshot date", failure) }
            val entries = mutableMapOf<String, String>()
            for (line in lines.drop(1)) {
                val parts = line.split('\t', limit = 2)
                if (parts.size != 2 || !prefixPattern.matches(parts[0]) || !validName(parts[1]) || entries.put(parts[0], parts[1]) != null) throw IOException("invalid or duplicate OUI snapshot assignment")
            }
            return Snapshot(fetched, entries)
        }

        private fun checkDeadline(deadline: Long) {
            if (Thread.currentThread().isInterrupted) throw InterruptedIOException("OUI update canceled")
            if (System.nanoTime() >= deadline) throw IOException("OUI update deadline exceeded")
        }

        private fun readBounded(input: InputStream, deadline: Long = Long.MAX_VALUE): ByteArray {
            val out = ByteArrayOutputStream()
            val buffer = ByteArray(8192)
            while (true) {
                checkDeadline(deadline)
                val size = input.read(buffer)
                if (size < 0) break
                if (out.size() + size > MAX_BYTES) throw IOException("oversized OUI data")
                out.write(buffer, 0, size)
            }
            return out.toByteArray()
        }

        private fun download(path: String, deadline: Long): ByteArray =
            download(URL("https://standards-oui.ieee.org/$path.txt").openConnection() as HttpURLConnection, deadline)

        internal fun download(connection: HttpURLConnection, deadline: Long): ByteArray =
            withProbeDeadline(deadline, connection::disconnect) {
                connection.apply {
                    connectTimeout = minOf(10000, probeTimeoutMs(deadline))
                    readTimeout = minOf(15000, probeTimeoutMs(deadline))
                    instanceFollowRedirects = false
                    setRequestProperty("User-Agent", "dropcheck-agent/1.0")
                }
                if (connection.responseCode != 200) throw IOException("IEEE registry HTTP ${connection.responseCode}")
                connection.inputStream.use { readBounded(it, deadline) }
            }
    }
}
