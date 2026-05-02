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

    /** Records the endpoint a session worker is attempting before hello succeeds. */
    fun markConnecting(endpointValue: String, transportValue: String) {
        connected.set(false)
        endpoint.set(endpointValue)
        transport.set(transportValue)
        nextRetryUnixMs.set(0)
    }

    /** Records an authenticated gRPC stream after the hello frame is sent. */
    fun markConnected(endpointValue: String, transportValue: String) {
        connected.set(true)
        endpoint.set(endpointValue)
        transport.set(transportValue)
        lastError.set("")
        lastConnectedUnixMs.set(System.currentTimeMillis())
        nextRetryUnixMs.set(0)
    }

    /** Records a stream close without scheduling information. */
    fun markDisconnected(message: String = "") {
        connected.set(false)
        if (message.isNotBlank()) lastError.set(message)
        lastDisconnectedUnixMs.set(System.currentTimeMillis())
    }

    /** Records the next direct-TCP retry chosen by the service backoff loop. */
    fun markRetryAt(unixTimeMs: Long, message: String) {
        connected.set(false)
        lastError.set(message)
        nextRetryUnixMs.set(unixTimeMs)
    }

    /** Builds the protocol-facing view without exposing the stored token. */
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

/** Renders a configured controller endpoint only when both host and port exist. */
internal fun ControllerLinkConfig.endpoint(): String {
    return if (host.isBlank() || port == 0) "" else "$host:$port"
}
