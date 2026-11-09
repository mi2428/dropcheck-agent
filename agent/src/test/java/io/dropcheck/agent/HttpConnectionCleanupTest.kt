package io.dropcheck.agent

import java.net.HttpURLConnection
import java.net.URL
import java.net.SocketTimeoutException
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference
import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Assert.assertFalse
import org.junit.Test

class HttpConnectionCleanupTest {
    @Test
    fun closesConnectionOnSuccessFailureAndInterrupt() {
        for (failure in listOf<Exception?>(null, IllegalStateException("read"), InterruptedException("cancel"))) {
            var disconnects = 0
            val connection = object : HttpURLConnection(URL("http://localhost/")) {
                override fun connect() = Unit
                override fun disconnect() { disconnects++ }
                override fun usingProxy() = false
            }
            if (failure == null) {
                assertEquals(42, withHttpConnection(connection, System.nanoTime() + TimeUnit.SECONDS.toNanos(2)) { 42 })
            } else {
                val caught = assertThrows(failure.javaClass) {
                    withHttpConnection(connection, System.nanoTime() + TimeUnit.SECONDS.toNanos(2)) { throw failure }
                }
                assertEquals(failure, caught)
            }
            assertEquals(1, disconnects)
        }
    }

    @Test
    fun deadlineAndCancellationDisconnectBlockedHttpAndReapTheWorker() {
        for (cancel in listOf(false, true)) {
            val reading = CountDownLatch(1)
            val disconnected = CountDownLatch(1)
            val failure = AtomicReference<Throwable>()
            val interrupted = AtomicReference<Boolean>()
            val connection = object : HttpURLConnection(URL("http://localhost/")) {
                override fun connect() = Unit
                override fun disconnect() { disconnected.countDown() }
                override fun usingProxy() = false
            }
            val command = Thread {
                try {
                    withHttpConnection(connection, System.nanoTime() + TimeUnit.MILLISECONDS.toNanos(if (cancel) 60_000 else 100)) {
                        reading.countDown()
                        // A read that ignores per-read timeouts still cannot reset the total deadline.
                        disconnected.await()
                    }
                } catch (e: Throwable) {
                    failure.set(e)
                    interrupted.set(Thread.currentThread().isInterrupted)
                }
            }
            try {
                command.start()
                assertTrue(reading.await(2, TimeUnit.SECONDS))
                if (cancel) command.interrupt()
                command.join(2_000)
                assertFalse(command.isAlive)
                assertEquals(0L, disconnected.count)
                assertTrue(if (cancel) failure.get() is InterruptedException else failure.get() is SocketTimeoutException)
                assertEquals(cancel, interrupted.get())
                assertFalse(Thread.getAllStackTraces().keys.any { it.isAlive && it.name == "network-probe" })
            } finally {
                command.interrupt()
                connection.disconnect()
                command.join(2_000)
            }
        }
    }
}
