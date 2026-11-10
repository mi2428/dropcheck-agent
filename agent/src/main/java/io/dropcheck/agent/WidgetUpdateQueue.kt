package io.dropcheck.agent

import java.util.concurrent.Executors
import java.util.concurrent.ScheduledExecutorService
import java.util.concurrent.ScheduledFuture
import java.util.concurrent.TimeUnit

/** Coalesce pending updates, but retain a request that arrives while rendering. */
internal class WidgetUpdateQueue(
    name: String,
    private val debounceMs: Long,
    private val minIntervalMs: Long,
    private val executor: ScheduledExecutorService = Executors.newSingleThreadScheduledExecutor { task ->
        Thread(task, name).apply { isDaemon = true }
    },
) {
    private var pending: (() -> Unit)? = null
    private var scheduled: ScheduledFuture<*>? = null
    private var running = false
    private var generation = 0L
    private var completedAtNanos: Long? = null

    init { require(debounceMs >= 0 && minIntervalMs >= 0) }

    @Synchronized fun request(update: () -> Unit) {
        pending = update
        if (!running && scheduled == null) schedule()
    }

    @Synchronized fun cancel() {
        generation++
        scheduled?.cancel(false)
        scheduled = null
        pending = null
    }

    private fun schedule() {
        val elapsed = completedAtNanos?.let { TimeUnit.NANOSECONDS.toMillis(System.nanoTime() - it) }
        val delay = maxOf(debounceMs, elapsed?.let { minIntervalMs - it } ?: 0L)
        val token = generation
        scheduled = executor.schedule({ render(token) }, delay, TimeUnit.MILLISECONDS)
    }

    private fun render(token: Long) {
        val update = synchronized(this) {
            if (token != generation) return
            scheduled = null
            val next = pending ?: return
            pending = null
            running = true
            next
        }
        try { update() } finally {
            synchronized(this) {
                running = false
                completedAtNanos = System.nanoTime()
                if (pending != null) schedule()
            }
        }
    }
}
