package io.dropcheck.agent

import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import org.junit.Assert.*
import org.junit.Test

class WidgetUpdateQueueTest {
    @Test fun retainsLatestRequestDuringRendering() {
        val executor = Executors.newSingleThreadScheduledExecutor()
        val queue = WidgetUpdateQueue("test", 0, 0, executor)
        val started = CountDownLatch(1)
        val release = CountDownLatch(1)
        val done = CountDownLatch(1)
        val values = java.util.Collections.synchronizedList(mutableListOf<Int>())
        try {
            queue.request { values.add(1); started.countDown(); check(release.await(2, TimeUnit.SECONDS)) }
            assertTrue(started.await(2, TimeUnit.SECONDS))
            queue.request { values.add(2) }
            queue.request { values.add(3); done.countDown() }
            release.countDown()
            assertTrue(done.await(2, TimeUnit.SECONDS))
            assertEquals(listOf(1, 3), values.toList())
        } finally { release.countDown(); queue.cancel(); executor.shutdownNow() }
    }

    @Test fun cancellationDropsPendingWorkAndAllowsReenable() {
        val executor = Executors.newSingleThreadScheduledExecutor()
        val release = CountDownLatch(1)
        val blocker = executor.submit { check(release.await(2, TimeUnit.SECONDS)) }
        val queue = WidgetUpdateQueue("test", 0, 0, executor)
        val done = CountDownLatch(1)
        val values = java.util.Collections.synchronizedList(mutableListOf<Int>())
        try {
            queue.request { values.add(1) }
            queue.cancel()
            queue.request { values.add(2); done.countDown() }
            release.countDown()
            blocker.get(2, TimeUnit.SECONDS)
            assertTrue(done.await(2, TimeUnit.SECONDS))
            assertEquals(listOf(2), values.toList())
        } finally { release.countDown(); queue.cancel(); executor.shutdownNow() }
    }

    @Test fun failedRenderingDoesNotLoseTheNextUpdateOrBypassTheInterval() {
        val executor = Executors.newSingleThreadScheduledExecutor()
        val queue = WidgetUpdateQueue("test", 0, 30, executor)
        val release = CountDownLatch(1)
        val started = CountDownLatch(1)
        val done = CountDownLatch(1)
        var failedAt = 0L
        var renderedAt = 0L
        try {
            queue.request { started.countDown(); check(release.await(2, TimeUnit.SECONDS)); failedAt = System.nanoTime(); error("synthetic render failure") }
            assertTrue(started.await(2, TimeUnit.SECONDS))
            queue.request { renderedAt = System.nanoTime(); done.countDown() }
            release.countDown()
            assertTrue(done.await(2, TimeUnit.SECONDS))
            assertTrue(TimeUnit.NANOSECONDS.toMillis(renderedAt - failedAt) >= 30)
        } finally { release.countDown(); queue.cancel(); executor.shutdownNow() }
    }
}
