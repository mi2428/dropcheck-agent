package io.dropcheck.agent

import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class NativeProbeProcessTest {
    @Test
    fun interruptionTerminatesAndReapsChildBeforeNextRun() {
        repeat(3) {
            val child = ProcessBuilder("sh", "-c", "exec sleep 30").start()
            val started = CountDownLatch(1)
            val failure = AtomicReference<Throwable>()
            val interrupted = AtomicReference<Boolean>()
            val worker = Thread {
                started.countDown()
                try {
                    collectProbeProcess(child, 60_000)
                } catch (e: Throwable) {
                    failure.set(e)
                    interrupted.set(Thread.currentThread().isInterrupted)
                }
            }
            try {
                worker.start()
                assertTrue(started.await(2, TimeUnit.SECONDS))
                worker.interrupt()
                worker.join(2_000)
                assertFalse("command must not hang", worker.isAlive)
                assertTrue(failure.get() is InterruptedException)
                assertFalse("canceled child must be reaped", child.isAlive)
                assertEquals(true, interrupted.get())
            } finally {
                child.destroyForcibly()
                child.waitFor(2, TimeUnit.SECONDS)
                worker.interrupt()
                worker.join(2_000)
            }
        }
    }

    @Test
    fun completionAndTimeoutPreserveResultClassification() {
        val completed = collectProbeProcess(ProcessBuilder("sh", "-c", "printf probe; exit 7").start(), 2_000)
        assertTrue(completed.finished)
        assertEquals(7, completed.exitCode)
        assertEquals("probe", completed.output)
        assertEquals("", completed.error)
        val child = ProcessBuilder("sh", "-c", "exec sleep 30").start()
        try {
            val timeout = collectProbeProcess(child, 20)
            assertFalse(timeout.finished)
            assertEquals(-1, timeout.exitCode)
            assertEquals("process_timeout=20ms", timeout.error)
            assertFalse(child.isAlive)
        } finally {
            child.destroyForcibly()
            child.waitFor(2, TimeUnit.SECONDS)
        }
    }
}
