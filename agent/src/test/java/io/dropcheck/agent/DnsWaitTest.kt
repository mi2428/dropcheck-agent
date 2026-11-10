package io.dropcheck.agent

import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicReference
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class DnsWaitTest {
    @Test
    fun completedAnswerDoesNotCancelAndTimeoutCancelsOnce() {
        val canceled = AtomicInteger()
        assertTrue(awaitDnsAnswer(CountDownLatch(0), System.nanoTime() + TimeUnit.MILLISECONDS.toNanos(10)) { canceled.incrementAndGet() })
        assertEquals(0, canceled.get())
        assertFalse(awaitDnsAnswer(CountDownLatch(1), System.nanoTime() + TimeUnit.MILLISECONDS.toNanos(1)) { canceled.incrementAndGet() })
        assertEquals(1, canceled.get())
    }

    @Test
    fun cancellationPropagatesAndNeverContinuesToNextProbe() {
        val started = CountDownLatch(1)
        val canceled = AtomicInteger()
        val failure = AtomicReference<Throwable>()
        val interrupted = AtomicReference<Boolean>()
        val continued = AtomicReference(false)
        val command = Thread {
            started.countDown()
            try {
                awaitDnsAnswer(CountDownLatch(1), System.nanoTime() + TimeUnit.SECONDS.toNanos(60)) { canceled.incrementAndGet() }
                continued.set(true)
            } catch (e: Throwable) {
                failure.set(e)
                interrupted.set(Thread.currentThread().isInterrupted)
            }
        }
        try {
            command.start()
            assertTrue(started.await(2, TimeUnit.SECONDS))
            command.interrupt()
            command.join(2_000)
            assertFalse(command.isAlive)
            assertTrue(failure.get() is InterruptedException)
            assertEquals(true, interrupted.get())
            assertEquals(1, canceled.get())
            assertFalse(continued.get())
        } finally {
            command.interrupt()
            command.join(2_000)
        }
    }

    @Test(timeout = 2_000)
    fun sequentialRecordTypesShareOneDeadlineAndExpiredQueriesNeverWaitAgain() {
        val deadline = System.nanoTime() + TimeUnit.MILLISECONDS.toNanos(50)
        val canceled = AtomicInteger()
        assertFalse(awaitDnsAnswer(CountDownLatch(1), deadline) { canceled.incrementAndGet() })
        assertFalse(awaitDnsAnswer(CountDownLatch(1), deadline) { canceled.incrementAndGet() })
        assertEquals(2, canceled.get())
    }
}
