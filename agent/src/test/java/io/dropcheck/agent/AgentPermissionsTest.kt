package io.dropcheck.agent

import org.junit.Assert.assertEquals
import org.junit.Test

class AgentPermissionsTest {
    @Test fun runtimeRequestsAndPermissionReportsShareSdkBoundaries() {
        val location = listOf("android.permission.ACCESS_FINE_LOCATION", "android.permission.ACCESS_COARSE_LOCATION")
        val nearby = location + "android.permission.NEARBY_WIFI_DEVICES"
        assertEquals(location, agentRuntimePermissions(31))
        assertEquals(location, agentRuntimePermissions(32))
        assertEquals(nearby, agentRuntimePermissions(33))
        assertEquals(nearby, agentRuntimePermissions(36))
        assertEquals(nearby + "android.permission.ACCESS_LOCAL_NETWORK", agentRuntimePermissions(37))
    }
}
