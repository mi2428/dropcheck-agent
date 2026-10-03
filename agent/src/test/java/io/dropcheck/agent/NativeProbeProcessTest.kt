package io.dropcheck.agent

import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

class NativeProbeProcessTest {
    private fun assertNoReaderLeak() {
        assertFalse(Thread.getAllStackTraces().keys.any { it.isAlive && it.name == "native-probe-output" })
    }

    @Test
    fun captureLimitFailsVisiblyRatherThanPassingTruncatedDataToParsers() {
        val child = ProcessBuilder("dd", "if=/dev/zero", "bs=1048576", "count=2")
            .redirectErrorStream(true).start()
        try {
            val error = assertThrows(IllegalStateException::class.java) { collectProbeProcess(child, 5_000) }
            assertEquals("native probe output exceeded $MAX_NATIVE_PROBE_OUTPUT_BYTES bytes", error.message)
            assertFalse(child.isAlive)
            assertNoReaderLeak()
        } finally {
            child.destroyForcibly()
            child.waitFor(2, TimeUnit.SECONDS)
        }
    }

    @Test
    fun finiteOutputLargerThanPipeCapacityDoesNotTimeout() {
        val child = ProcessBuilder("dd", "if=/dev/zero", "bs=262144", "count=1")
            .redirectErrorStream(true).start()
        try {
            val result = collectProbeProcess(child, 2_000)
            assertTrue("finite writer must finish once output is drained", result.finished)
            assertEquals(0, result.exitCode)
            assertTrue(result.output.length >= 262_144)
            assertTrue(result.output.take(262_144).all { it == '\u0000' })
            assertEquals("", result.error)
            assertFalse(child.isAlive)
            assertNoReaderLeak()
        } finally {
            child.destroyForcibly()
            child.waitFor(2, TimeUnit.SECONDS)
        }
    }

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
                assertNoReaderLeak()
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
            assertNoReaderLeak()
        } finally {
            child.destroyForcibly()
            child.waitFor(2, TimeUnit.SECONDS)
        }
    }
}
