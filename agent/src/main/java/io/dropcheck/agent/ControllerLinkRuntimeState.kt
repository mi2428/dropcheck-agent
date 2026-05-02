package io.dropcheck.agent

import io.dropcheck.agent.grpc.ControllerLinkConfig
import io.dropcheck.agent.grpc.ControllerLinkStatus
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicLong
import java.util.concurrent.atomic.AtomicReference

/** Process-local controller link state exposed through GetControllerLinkStatus. */
internal object ControllerLinkRuntimeState {
    private val connected = AtomicBoolean(false)
    private val endpoint = AtomicReference("")
    private val transport = AtomicReference("")
    private val lastError = AtomicReference("")
    private val lastConnectedUnixMs = AtomicLong(0)
    private val lastDisconnectedUnixMs = AtomicLong(0)
    private val nextRetryUnixMs = AtomicLong(0)

    fun markConnecting(endpointValue: String, transportValue: String) {
        connected.set(false)
        endpoint.set(endpointValue)
        transport.set(transportValue)
        nextRetryUnixMs.set(0)
    }

    fun markConnected(endpointValue: String, transportValue: String) {
        connected.set(true)
        endpoint.set(endpointValue)
        transport.set(transportValue)
        lastError.set("")
        lastConnectedUnixMs.set(System.currentTimeMillis())
        nextRetryUnixMs.set(0)
    }

    fun markDisconnected(message: String = "") {
        connected.set(false)
        if (message.isNotBlank()) lastError.set(message)
        lastDisconnectedUnixMs.set(System.currentTimeMillis())
    }

    fun markRetryAt(unixTimeMs: Long, message: String) {
        connected.set(false)
        lastError.set(message)
        nextRetryUnixMs.set(unixTimeMs)
    }

    fun status(config: ControllerLinkConfig): ControllerLinkStatus {
        return ControllerLinkStatus.newBuilder()
            .setEnabled(config.enabled)
            .setConnected(connected.get())
            .setEndpoint(endpoint.get().ifBlank { config.endpoint() })
            .setTransport(transport.get())
            .setLastError(lastError.get())
            .setLastConnectedUnixMs(lastConnectedUnixMs.get())
            .setLastDisconnectedUnixMs(lastDisconnectedUnixMs.get())
            .setNextRetryUnixMs(nextRetryUnixMs.get())
            .build()
    }
}

internal fun ControllerLinkConfig.endpoint(): String {
    return if (host.isBlank() || port == 0) "" else "$host:$port"
}
