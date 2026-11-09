package io.dropcheck.agent

import java.net.SocketTimeoutException
import java.util.concurrent.ExecutionException
import java.util.concurrent.FutureTask
import java.util.concurrent.TimeUnit
import java.util.concurrent.TimeoutException

internal fun throwIfInterrupted() {
    if (Thread.currentThread().isInterrupted) {
        throw InterruptedException("command interrupted")
    }
}

internal fun probeTimeoutMs(deadlineNanos: Long): Int {
    val remaining = deadlineNanos - System.nanoTime()
    if (remaining <= 0) throw SocketTimeoutException("probe deadline exceeded")
    return TimeUnit.NANOSECONDS.toMillis(remaining).coerceIn(1, Int.MAX_VALUE.toLong()).toInt()
}

/** A single deadline includes every blocking stage; closing the resource releases pending I/O. */
internal fun <T> withProbeDeadline(deadlineNanos: Long, close: () -> Unit, block: () -> T): T {
    val task = FutureTask(block)
    val worker = Thread(task, "network-probe").apply { isDaemon = true }
    try {
        if (Thread.currentThread().isInterrupted) throw InterruptedException("probe interrupted")
        probeTimeoutMs(deadlineNanos)
        worker.start()
        return task.get((deadlineNanos - System.nanoTime()).coerceAtLeast(0), TimeUnit.NANOSECONDS)
    } catch (e: TimeoutException) {
        throw SocketTimeoutException("probe deadline exceeded")
    } catch (e: ExecutionException) {
        throw requireNotNull(e.cause)
    } catch (e: InterruptedException) {
        Thread.currentThread().interrupt()
        throw e
    } finally {
        try {
            close()
        } finally {
            task.cancel(true)
            var interrupted = Thread.interrupted()
            try {
                val cleanupDeadline = System.nanoTime() + TimeUnit.MILLISECONDS.toNanos(500)
                while (worker.isAlive) {
                    val remainingMs = TimeUnit.NANOSECONDS.toMillis(cleanupDeadline - System.nanoTime())
                    check(remainingMs > 0) { "network probe worker did not terminate" }
                    try {
                        worker.join(remainingMs)
                    } catch (e: InterruptedException) {
                        interrupted = true
                    }
                }
            } finally {
                if (interrupted) Thread.currentThread().interrupt()
            }
        }
    }
}
