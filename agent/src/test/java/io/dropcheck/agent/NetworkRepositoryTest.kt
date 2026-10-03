package io.dropcheck.agent

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Assert.assertThrows
import org.junit.Assert.assertSame
import org.junit.Test

class NetworkRepositoryTest {
    @Test
    fun failedBindingNeverExecutesProbe() {
        var executed = false
        var restored = false
        val error = assertThrows(IllegalStateException::class.java) {
            withNetworkBinding(
                bind = { false },
                restore = { restored = true; true },
            ) { executed = true }
        }
        assertEquals("bindProcessToNetwork failed", error.message)
        assertFalse(executed)
        assertFalse(restored)
    }

    @Test
    fun successfulBindingRestoresPreviousNetworkOnReturnAndException() {
        val calls = mutableListOf<String>()
        fun bind(): Boolean { calls += "bind"; return true }
        fun restore(): Boolean { calls += "restore"; return true }
        assertEquals(42, withNetworkBinding(::bind, ::restore) { calls += "probe"; 42 })
        assertEquals(listOf("bind", "probe", "restore"), calls)
        calls.clear()
        val failure = IllegalArgumentException("probe failed")
        assertSame(failure, assertThrows(IllegalArgumentException::class.java) {
            withNetworkBinding(::bind, ::restore) { calls += "probe"; throw failure }
        })
        assertEquals(listOf("bind", "probe", "restore"), calls)
    }

    @Test
    fun failedRestorationCannotReturnSuccessfulMeasurement() {
        var measured = false
        val error = assertThrows(IllegalStateException::class.java) {
            withNetworkBinding(bind = { true }, restore = { false }) { measured = true; 42 }
        }
        assertTrue(measured)
        assertEquals("restore bindProcessToNetwork failed", error.message)
        val failure = SecurityException("restore denied")
        assertSame(failure, assertThrows(SecurityException::class.java) {
            withNetworkBinding(bind = { true }, restore = { throw failure }) { 42 }
        })
    }

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
