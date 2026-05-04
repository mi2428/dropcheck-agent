package io.dropcheck.agent

import android.content.Context
import java.util.concurrent.CopyOnWriteArraySet

/** Notifies process-local status listeners used by the on-device activity chrome. */
internal object AgentStatusBroadcast {
    private val listeners = CopyOnWriteArraySet<() -> Unit>()

    fun addListener(listener: () -> Unit) {
        listeners.add(listener)
    }

    fun removeListener(listener: () -> Unit) {
        listeners.remove(listener)
    }

    @Suppress("UNUSED_PARAMETER")
    fun send(context: Context) {
        listeners.forEach { listener -> runCatching { listener() } }
    }
}
