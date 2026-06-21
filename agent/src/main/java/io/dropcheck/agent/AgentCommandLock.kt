package io.dropcheck.agent

import java.util.concurrent.locks.ReentrantLock
import kotlin.concurrent.withLock

/**
 * Serializes Android-side command execution across controller and local shell work.
 *
 * Wi-Fi association and network diagnostics are device-global side effects.
 * Sharing one lock prevents overlapping interactive commands from racing each
 * other on the same handset.
 */
internal object AgentCommandLock {
    private val lock = ReentrantLock()

    fun <T> run(block: () -> T): T = lock.withLock(block)
}
