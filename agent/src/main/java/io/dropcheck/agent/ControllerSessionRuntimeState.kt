package io.dropcheck.agent

import java.util.concurrent.atomic.AtomicBoolean

/** Process-local controller session state used by the on-device activity chrome. */
internal object ControllerSessionRuntimeState {
    private val connected = AtomicBoolean(false)
    private val heartbeatTimedOut = AtomicBoolean(false)

    fun markConnecting() {
        connected.set(false)
        heartbeatTimedOut.set(false)
    }

    fun markConnected() {
        connected.set(true)
        heartbeatTimedOut.set(false)
    }

    fun markDisconnected() {
        connected.set(false)
        heartbeatTimedOut.set(false)
    }

    fun markHeartbeatTimedOut() {
        if (!connected.get()) return
        heartbeatTimedOut.set(true)
    }

    fun markHeartbeatRecovered() {
        if (!connected.get()) return
        heartbeatTimedOut.set(false)
    }

    fun heartbeatConnected(): Boolean = connected.get() && !heartbeatTimedOut.get()
}
