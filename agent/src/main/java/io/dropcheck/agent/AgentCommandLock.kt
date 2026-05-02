package io.dropcheck.agent

import java.util.concurrent.locks.ReentrantLock
import kotlin.concurrent.withLock

/**
 * Serializes Android-side command execution across gRPC and standalone runs.
 *
 * Wi-Fi association and network diagnostics are device-global side effects.
 * Sharing one lock prevents the foreground standalone runner from racing a
 * controller command while still allowing result sync commands between steps.
 */
internal object AgentCommandLock {
    private val lock = ReentrantLock()

    fun <T> run(block: () -> T): T = lock.withLock(block)
}
