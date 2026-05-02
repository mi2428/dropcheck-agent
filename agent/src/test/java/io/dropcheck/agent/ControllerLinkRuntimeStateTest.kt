package io.dropcheck.agent

import io.dropcheck.agent.grpc.ControllerLinkConfig
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ControllerLinkRuntimeStateTest {
    @Test
    fun reportsConnectedAndRetryStateWithoutToken() {
        val config = ControllerLinkConfig.newBuilder()
            .setEnabled(true)
            .setHost("192.168.7.1")
            .setPort(37588)
            .setToken("secret-token")
            .build()

        ControllerLinkRuntimeState.markConnecting("192.168.7.1:37588", "direct-tcp")
        var status = ControllerLinkRuntimeState.status(config)
        assertFalse(status.connected)
        assertEquals("192.168.7.1:37588", status.endpoint)
        assertEquals("direct-tcp", status.transport)

        ControllerLinkRuntimeState.markConnected("192.168.7.1:37588", "direct-tcp")
        status = ControllerLinkRuntimeState.status(config)
        assertTrue(status.connected)
        assertTrue(status.lastConnectedUnixMs > 0)

        ControllerLinkRuntimeState.markRetryAt(123_456L, "controller disconnected; retrying")
        status = ControllerLinkRuntimeState.status(config)
        assertFalse(status.connected)
        assertEquals(123_456L, status.nextRetryUnixMs)
        assertEquals("controller disconnected; retrying", status.lastError)
    }

    @Test
    fun fallsBackToConfiguredEndpoint() {
        val config = ControllerLinkConfig.newBuilder()
            .setEnabled(true)
            .setHost("192.168.7.1")
            .setPort(37588)
            .build()

        assertEquals("192.168.7.1:37588", config.endpoint())
        assertEquals("", ControllerLinkConfig.getDefaultInstance().endpoint())
    }
}
