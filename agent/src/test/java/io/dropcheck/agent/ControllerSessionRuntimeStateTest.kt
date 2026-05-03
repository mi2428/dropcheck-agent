package io.dropcheck.agent

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ControllerSessionRuntimeStateTest {
    @Test
    fun heartbeatConnectedTracksSessionLifecycle() {
        ControllerSessionRuntimeState.markDisconnected()
        assertFalse(ControllerSessionRuntimeState.heartbeatConnected())

        ControllerSessionRuntimeState.markConnecting()
        assertFalse(ControllerSessionRuntimeState.heartbeatConnected())

        ControllerSessionRuntimeState.markConnected()
        assertTrue(ControllerSessionRuntimeState.heartbeatConnected())

        ControllerSessionRuntimeState.markHeartbeatTimedOut()
        assertFalse(ControllerSessionRuntimeState.heartbeatConnected())

        ControllerSessionRuntimeState.markHeartbeatRecovered()
        assertTrue(ControllerSessionRuntimeState.heartbeatConnected())

        ControllerSessionRuntimeState.markDisconnected()
        assertFalse(ControllerSessionRuntimeState.heartbeatConnected())
    }
}
