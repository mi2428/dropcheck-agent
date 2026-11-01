package io.dropcheck.agent

import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class AgentCommandLockTest {
    @Test
    fun interruptedWaiterCannotExecuteAfterOwnerReleasesLock() {
        val ownerStarted = CountDownLatch(1)
        val release = CountDownLatch(1)
        val owner = Thread { AgentCommandLock.run { ownerStarted.countDown(); release.await() } }
        owner.start()
        try {
            assertTrue(ownerStarted.await(2, TimeUnit.SECONDS))
            val waiterStarted = CountDownLatch(1)
            val executed = AtomicBoolean(false)
            val interrupted = AtomicBoolean(false)
            val waiter = Thread {
                waiterStarted.countDown()
                try {
                    AgentCommandLock.run { executed.set(true) }
                } catch (_: InterruptedException) {
                    interrupted.set(true)
                }
            }
            waiter.start()
            assertTrue(waiterStarted.await(2, TimeUnit.SECONDS))
            waiter.interrupt()
            waiter.join(2_000)
            assertFalse(waiter.isAlive)
            assertTrue(interrupted.get())
            assertFalse(executed.get())
        } finally {
            release.countDown()
            owner.join(2_000)
        }
    }
}
