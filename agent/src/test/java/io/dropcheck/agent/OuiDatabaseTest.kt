package io.dropcheck.agent

import org.junit.Assert.*
import org.junit.Test
import java.io.File
import java.io.IOException
import java.io.InterruptedIOException
import java.io.InputStream
import java.net.HttpURLConnection
import java.net.SocketTimeoutException
import java.net.URL
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicReference
import java.util.zip.GZIPOutputStream

class OuiDatabaseTest {
    private fun writeSnapshot(file: File, rows: String) {
        GZIPOutputStream(file.outputStream()).bufferedWriter().use { it.write("# dropcheck-oui-v1\t2026-11-08T00:00:00Z\n$rows") }
    }

    @Test fun offlineLookupParityAndBundledSnapshot() {
        val directory = kotlin.io.path.createTempDirectory().toFile()
        try {
            val file = File(directory, "registry.tsv.gz")
            writeSnapshot(file, "001122\tVendor24\n0011223\tVendor28\n001122334\tVendor36\n")
            val database = OuiDatabase(file)
            val cases = checkNotNull(javaClass.getResourceAsStream("/oui_lookup_cases.tsv")).bufferedReader().use { it.readLines() }
            for (line in cases.filterNot { it.startsWith('#') || it.isEmpty() }) {
                val (mac, expected) = line.split('\t')
                assertTrue("$mac: ${database.vendor(mac)}", database.vendor(mac).contains(expected))
            }
            assertTrue(OuiDatabase(null).info().assignments > 50000)
            assertTrue(OuiDatabase(null).vendor("e8:0a:b9:00:00:01").contains("Cisco"))
        } finally { directory.deleteRecursively() }
    }

    private fun fixture(path: String): ByteArray = when (path) {
        "oui/oui" -> "Organization\n00-11-22 (hex) Vendor24\n001122 (base 16) Vendor24\n"
        "oui28/mam" -> "Organization\n00-11-22 (hex) Vendor28\n300000-3FFFFF (base 16) Vendor28\n"
        "oui36/oui36" -> "Organization\n00-11-22 (hex) Vendor36\n334000-334FFF (base 16) Vendor36\n"
        else -> "Organization\n00-50-C2 (hex) IAB vendor\n111000-111FFF (base 16) IAB vendor\n"
    }.toByteArray()

    @Test fun atomicUpdateFailureCancellationAndCorruptCache() {
        val directory = kotlin.io.path.createTempDirectory().toFile()
        try {
            val file = File(directory, "oui/registry.tsv.gz")
            val database = OuiDatabase(file)
            val deadlines = mutableListOf<Long>()
            assertEquals(4, database.update { path, deadline -> deadlines += deadline; fixture(path) }.assignments)
            assertEquals(1, deadlines.distinct().size)
            val before = file.readBytes()
            assertTrue(OuiDatabase(file).vendor("00:11:22:33:44:55").contains("Vendor36"))
            try {
                database.update { path, _ -> if (path == "oui36/oui36") throw IOException("synthetic download failure") else fixture(path) }
                fail("failed update succeeded")
            } catch (_: IOException) { }
            assertArrayEquals(before, file.readBytes())
            Thread.currentThread().interrupt()
            try {
                database.update { path, _ -> fixture(path) }
                fail("canceled update succeeded")
            } catch (_: InterruptedIOException) { } finally { Thread.interrupted() }
            assertArrayEquals(before, file.readBytes())
            assertTrue(database.vendor("00:11:22:33:44:55").contains("Vendor36"))
            file.writeText("corrupt")
            val corrupt = OuiDatabase(file)
            try { corrupt.info(); fail("corrupt cache silently accepted") } catch (_: IOException) { }
            assertTrue(corrupt.vendor("00:11:22:33:44:55").contains("database error"))
            assertEquals(4, corrupt.update { path, _ -> fixture(path) }.assignments)
            assertTrue(corrupt.vendor("00:11:22:33:44:55").contains("Vendor36"))
            assertEquals(1, checkNotNull(file.parentFile?.listFiles()).size)
        } finally { directory.deleteRecursively() }
    }

    @Test fun totalDeadlineDisconnectsBlockedRegistryHeadersAndBody() = blockedDownload(cancel = false)

    @Test fun cancellationDisconnectsBlockedRegistryHeadersAndBody() = blockedDownload(cancel = true)

    @Test fun registryDownloadPreservesBytesAndHttpFailureCleanup() {
        val bytes = fixture("oui/oui")
        for (status in listOf(200, 503)) {
            var disconnects = 0
            val connection = object : HttpURLConnection(URL("https://example.test/registry")) {
                override fun connect() = Unit
                override fun usingProxy() = false
                override fun disconnect() { disconnects++ }
                override fun getResponseCode() = status
                override fun getInputStream(): InputStream = bytes.inputStream()
            }
            val deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(2)
            if (status == 200) assertArrayEquals(bytes, OuiDatabase.download(connection, deadline))
            else assertEquals("IEEE registry HTTP 503", assertThrows(IOException::class.java) { OuiDatabase.download(connection, deadline) }.message)
            assertEquals(1, disconnects)
            assertFalse(connection.instanceFollowRedirects)
        }
    }

    private fun blockedDownload(cancel: Boolean) {
        for (header in listOf(true, false)) {
            val entered = CountDownLatch(1)
            val disconnected = CountDownLatch(1)
            val disconnects = AtomicInteger()
            val ioThread = AtomicReference<Thread>()
            val failure = AtomicReference<Throwable>()
            val interrupted = AtomicReference<Boolean>()
            fun blockUntilDisconnect() {
                ioThread.set(Thread.currentThread())
                entered.countDown()
                // Model I/O that only disconnect, not thread interruption, can release.
                while (disconnected.count > 0) {
                    try { disconnected.await() } catch (_: InterruptedException) { }
                }
            }
            val connection = object : HttpURLConnection(URL("https://example.test/registry")) {
                override fun connect() = Unit
                override fun usingProxy() = false
                override fun disconnect() { disconnects.incrementAndGet(); disconnected.countDown() }
                override fun getResponseCode(): Int {
                    if (header) blockUntilDisconnect()
                    return 200
                }
                override fun getInputStream(): InputStream = object : InputStream() {
                    override fun read(): Int {
                        if (!header) blockUntilDisconnect()
                        return -1
                    }
                }
            }
            val command = Thread {
                try {
                    OuiDatabase.download(connection, System.nanoTime() + TimeUnit.MILLISECONDS.toNanos(if (cancel) 60_000 else 500))
                } catch (error: Throwable) {
                    failure.set(error)
                    interrupted.set(Thread.currentThread().isInterrupted)
                }
            }
            try {
                command.start()
                assertTrue("header=$header did not enter I/O", entered.await(2, TimeUnit.SECONDS))
                if (cancel) command.interrupt()
                command.join(2_000)
                assertFalse("header=$header download remained blocked", command.isAlive)
                assertEquals(1, disconnects.get())
                assertTrue("header=$header failure=${failure.get()}",
                    if (cancel) failure.get() is InterruptedException else failure.get() is SocketTimeoutException)
                assertEquals(cancel, interrupted.get())
                assertFalse("header=$header leaked its I/O worker", ioThread.get().isAlive)
            } finally {
                connection.disconnect()
                command.interrupt()
                command.join(2_000)
            }
        }
    }

    @Test fun rejectsMalformedSourcesAndPreservesAmbiguousIEEEAssignments() {
        val repeated = "Organization\n08-00-30 (hex) CERN\n080030 (base 16) CERN\n08-00-30 (hex) Other\n080030 (base 16) Other\n"
        assertEquals("multiple IEEE assignees: CERN / Other", OuiDatabase.parseIEEE(repeated.toByteArray(), 6)["080030"])
        assertEquals("Café Labs", OuiDatabase.parseIEEE("Organization\n00-11-22 (hex) Café Labs\n001122 (base 16) Café\u00a0\u0085Labs\n".toByteArray(), 6)["001122"])
        for (invalid in listOf("<html>error</html>", "Organization\n", "Organization\n001122 (base 16) missing hex\n", "Organization\n00-11-22 (hex) truncated\n", "Organization\n00-11-22 (hex) Bad\n001122 (base 16) Bad\u001b[2J\n")) {
            try { OuiDatabase.parseIEEE(invalid.toByteArray(), 6); fail("accepted invalid IEEE input") } catch (_: IOException) { }
        }
        try { OuiDatabase.parseIEEE("Organization\n00-11-22 (hex) Bad range\n334001-334FFF (base 16) Bad range\n".toByteArray(), 9); fail("accepted unaligned range") } catch (_: IOException) { }
        try { OuiDatabase.parseIEEE(byteArrayOf(0xff.toByte()), 6); fail("accepted invalid UTF-8") } catch (_: IOException) { }
    }
}
