package io.dropcheck.agent

import android.os.Handler
import android.os.Looper
import java.io.InterruptedIOException
import java.util.concurrent.TimeUnit

/** Owns one Shell action independently of an Activity instance. */
internal class ShellActionOwner(private val post: (() -> Unit) -> Unit = { Handler(Looper.getMainLooper()).post(it) }) {
    private var thread: Thread? = null
    private var name = ""
    private var startedNs = 0L
    private var result: AgentShellResult? = null
    private var listener: ((AgentShellResult) -> Unit)? = null

    @get:Synchronized val busy: Boolean get() = thread != null || result != null

    @Synchronized fun status(): String = if (!busy) "idle" else "$name (${TimeUnit.NANOSECONDS.toSeconds(System.nanoTime() - startedNs)}s elapsed)"

    fun attach(callback: (AgentShellResult) -> Unit) {
        synchronized(this) { listener = callback }
        post(::deliver)
    }

    @Synchronized fun detach(callback: (AgentShellResult) -> Unit) {
        if (listener === callback) listener = null
    }

    @Synchronized fun cancel() { thread?.interrupt() }

    @Synchronized fun start(operation: String, action: () -> AgentShellResult): Boolean {
        if (thread != null || result != null) return false
        name = operation
        startedNs = System.nanoTime()
        thread = Thread {
            val completed = try { action() } catch (failure: Exception) {
                val canceled = failure is InterruptedException || failure is InterruptedIOException || Thread.currentThread().isInterrupted
                if (canceled) Thread.currentThread().interrupt()
                AgentShellResult(false, listOf(if (canceled) "Command canceled" else "Command failed (execution error)"))
            }
            synchronized(this) {
                result = completed
                thread = null
            }
            post(::deliver)
        }.apply { name = "agent-shell-action"; start() }
        return true
    }

    private fun deliver() {
        val pair = synchronized(this) {
            val pending = result ?: return
            val active = listener ?: return
            result = null
            active to pending
        }
        pair.first(pair.second)
    }
}
