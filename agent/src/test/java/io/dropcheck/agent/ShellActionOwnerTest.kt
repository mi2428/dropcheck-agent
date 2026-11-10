package io.dropcheck.agent

import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.io.InterruptedIOException
import java.util.concurrent.atomic.AtomicReference
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ShellActionOwnerTest {
    @Test fun cancellationAndExecutionFailuresRemainDistinctAndAllowRetry() {
        for (failure in listOf(InterruptedException("synthetic"), InterruptedIOException("synthetic"), IllegalStateException("synthetic"))) {
            val owner = ShellActionOwner { it() }
            val result = AtomicReference<AgentShellResult>()
            var delivered = CountDownLatch(1)
            owner.attach { result.set(it); delivered.countDown() }
            assertTrue(owner.start("synthetic") { throw failure })
            assertTrue(delivered.await(2, TimeUnit.SECONDS))
            assertFalse(result.get().ok)
            val expected = if (failure is IllegalStateException) "Command failed (execution error)" else "Command canceled"
            assertEquals(listOf(expected), result.get().lines)
            assertFalse(owner.busy)
            delivered = CountDownLatch(1)
            assertTrue(owner.start("retry") { AgentShellResult(true) })
            assertTrue(delivered.await(2, TimeUnit.SECONDS))
            assertTrue(result.get().ok)
            assertFalse(owner.busy)
        }
    }
    @Test fun recreationCancelAndCompletionDeliverExactlyOnce() {
        val owner = ShellActionOwner { action -> action() }
        val started = CountDownLatch(1)
        val finished = CountDownLatch(1)
        val delivered = CountDownLatch(1)
        val results = mutableListOf<AgentShellResult>()
        val obsolete: (AgentShellResult) -> Unit = { error("delivered into obsolete Activity") }
        owner.attach(obsolete)
        assertTrue(owner.start("ping") {
            started.countDown()
            try { Thread.sleep(30_000) } catch (_: InterruptedException) { /* Explicit cancel. */ }
            finished.countDown()
            AgentShellResult(false, listOf("canceled"))
        })
        assertTrue(started.await(2, TimeUnit.SECONDS))
        assertFalse(owner.start("another") { AgentShellResult(true) })
        assertTrue(owner.status().startsWith("ping ("))
        owner.detach(obsolete)
        owner.cancel()
        assertTrue(finished.await(2, TimeUnit.SECONDS))
        val current: (AgentShellResult) -> Unit = { result -> results.add(result); delivered.countDown() }
        owner.attach(current)
        assertTrue(delivered.await(2, TimeUnit.SECONDS))
        owner.attach(current)
        assertEquals(1, results.size)
        assertEquals(listOf("canceled"), results.single().lines)
        assertFalse(owner.busy)
    }
}
