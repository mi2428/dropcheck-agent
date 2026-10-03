package io.dropcheck.agent

import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream
import java.io.InputStream
import java.net.InetSocketAddress
import java.net.Socket
import java.net.SocketAddress
import java.net.SocketTimeoutException
import java.util.concurrent.TimeUnit
import java.util.concurrent.CountDownLatch
import java.util.concurrent.atomic.AtomicReference
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

class GlobalIpResponseTest {
    private class TestSocket(private val input: InputStream) : Socket() {
        var closed = false
        override fun connect(endpoint: SocketAddress, timeout: Int) { }
        override fun setSoTimeout(timeout: Int) { }
        override fun getInputStream() = input
        override fun getOutputStream() = ByteArrayOutputStream()
        override fun close() { closed = true; input.close() }
    }

    private fun read(socket: Socket, timeoutMs: Long = 2_000): String = readGlobalIpResponse(
        socket, InetSocketAddress.createUnresolved("example.test", 80), ByteArray(0),
        System.nanoTime() + TimeUnit.MILLISECONDS.toNanos(timeoutMs),
    )

    private fun assertNoWorkerLeak() {
        assertFalse(Thread.getAllStackTraces().keys.any { it.isAlive && it.name == "global-ip-response" })
    }

    @Test
    fun oversizedResponseFailsRatherThanTruncating() {
        val socket = TestSocket(ByteArrayInputStream(ByteArray(MAX_GLOBAL_IP_HEADER_BYTES + MAX_GLOBAL_IP_BODY_BYTES + 1)))
        assertThrows(IllegalStateException::class.java) { read(socket) }
        assertTrue(socket.closed)
        assertNoWorkerLeak()
    }

    @Test
    fun continuouslyTricklingPeerCannotResetTotalDeadline() {
        val input = object : InputStream() {
            var remaining = 100
            @Volatile var closed = false
            override fun read(): Int {
                if (closed || remaining-- <= 0) return -1
                Thread.sleep(20)
                return 'x'.code
            }
            override fun read(buffer: ByteArray, off: Int, len: Int): Int {
                val value = read()
                if (value < 0) return -1
                buffer[off] = value.toByte()
                return 1
            }
            override fun close() { closed = true }
        }
        val socket = TestSocket(input)
        val started = System.nanoTime()
        assertThrows(SocketTimeoutException::class.java) { read(socket, 100) }
        assertTrue(TimeUnit.NANOSECONDS.toMillis(System.nanoTime() - started) < 1_000)
        assertTrue(socket.closed)
        assertNoWorkerLeak()
    }

    @Test
    fun validSmallWireResponseIsPreservedAndSocketClosed() {
        val response = "HTTP/1.1 200 OK\r\nConnection: close\r\n\r\n2001:db8::1\n"
        val socket = TestSocket(ByteArrayInputStream(response.toByteArray()))
        assertEquals(response, read(socket))
        assertTrue(socket.closed)
        assertNoWorkerLeak()
    }

    @Test
    fun parsesIpv4Ipv6ContentLengthAndChunkedResponses() {
        for (ip in listOf("203.0.113.1", "2001:db8::1")) {
            assertEquals(GlobalIpHttpResponse(200, ip), parseGlobalIpHttpResponse("HTTP/1.1 200 OK\r\nContent-Length: ${ip.length}\r\n\r\n$ip"))
            assertEquals(GlobalIpHttpResponse(200, ip), parseGlobalIpHttpResponse("HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n${ip.length.toString(16)};test=1\r\n$ip\r\n0\r\n\r\n"))
            assertEquals(GlobalIpHttpResponse(200, "$ip\n"), parseGlobalIpHttpResponse("HTTP/1.1 200 OK\n\n$ip\n"))
        }
        for (response in listOf(
            "HTTP/1.1 200 OK\r\nX-Test: ${"x".repeat(MAX_GLOBAL_IP_HEADER_BYTES)}\r\n\r\n203.0.113.1",
            "HTTP/1.1 200 OK\r\n\r\n${"x".repeat(MAX_GLOBAL_IP_BODY_BYTES + 1)}",
            "HTTP/1.1 200 OK\r\nContent-Length: 100\r\n\r\n203.0.113.1",
            "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\nb\r\n203.0.113.1\r\n",
            "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n7fffffff\r\nx\r\n0\r\n\r\n",
            "not HTTP\r\n\r\n203.0.113.1",
        )) assertThrows(IllegalArgumentException::class.java) { parseGlobalIpHttpResponse(response) }
    }

    @Test
    fun cancellationClosesSocketAndStopsHelper() {
        val reading = CountDownLatch(1)
        val socket = TestSocket(object : InputStream() {
            override fun read(): Int {
                reading.countDown()
                Thread.sleep(30_000)
                return -1
            }
        })
        val failure = AtomicReference<Throwable>()
        val interrupted = AtomicReference<Boolean>()
        val command = Thread {
            try { read(socket, 60_000) } catch (e: Throwable) {
                failure.set(e)
                interrupted.set(Thread.currentThread().isInterrupted)
            }
        }
        try {
            command.start()
            assertTrue(reading.await(2, TimeUnit.SECONDS))
            command.interrupt()
            command.join(2_000)
            assertFalse(command.isAlive)
            assertTrue(failure.get() is InterruptedException)
            assertEquals(true, interrupted.get())
            assertTrue(socket.closed)
            assertNoWorkerLeak()
        } finally {
            command.interrupt()
            command.join(2_000)
            socket.close()
        }
    }
}
