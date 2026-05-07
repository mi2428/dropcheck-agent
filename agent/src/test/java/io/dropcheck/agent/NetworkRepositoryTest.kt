package io.dropcheck.agent

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Test

class NetworkRepositoryTest {
    @Test
    fun effectiveLinkMtuUsesExplicitLinkMtuFirst() {
        var fallbackCalled = false

        val mtu = effectiveLinkMtu(1400, "wlan0") {
            fallbackCalled = true
            1500
        }

        assertEquals(1400, mtu)
        assertFalse(fallbackCalled)
    }

    @Test
    fun effectiveLinkMtuFallsBackToInterfaceMtuWhenLinkMtuIsDefault() {
        val mtu = effectiveLinkMtu(0, "wlan0") { name ->
            assertEquals("wlan0", name)
            1500
        }

        assertEquals(1500, mtu)
    }

    @Test
    fun effectiveLinkMtuKeepsZeroWhenFallbackIsUnavailable() {
        assertEquals(0, effectiveLinkMtu(0, "") { error("fallback should not be called") })
        assertEquals(0, effectiveLinkMtu(0, "wlan0") { null })
        assertEquals(0, effectiveLinkMtu(0, "wlan0") { 0 })
        assertEquals(0, effectiveLinkMtu(0, "wlan0") { error("lookup failed") })
    }
}
