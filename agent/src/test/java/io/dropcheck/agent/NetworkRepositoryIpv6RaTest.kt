package io.dropcheck.agent

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class NetworkRepositoryIpv6RaTest {
    @Test
    fun summarizesIpv6DefaultRoutes() {
        val summary = summarizeIpv6DefaultRoutes(listOf(
            "0.0.0.0/0 -> 192.0.2.1 wlan0",
            "::/0 -> fe80::1 wlan0",
            "::/0 -> fe80::2 wlan0",
        ))

        assertTrue(summary.present)
        assertEquals(listOf("fe80::1", "fe80::2"), summary.gateways)
    }

    @Test
    fun ignoresIpv4DefaultRoutes() {
        val summary = summarizeIpv6DefaultRoutes(listOf(
            "0.0.0.0/0 -> 192.0.2.1 wlan0",
            "2001:db8::/64 -> :: wlan0",
        ))

        assertFalse(summary.present)
        assertTrue(summary.gateways.isEmpty())
    }

    @Test
    fun treatsOnLinkIpv6DefaultRouteAsPresent() {
        val summary = summarizeIpv6DefaultRoutes(listOf(
            "::/0 -> :: wlan0",
        ))

        assertTrue(summary.present)
        assertTrue(summary.gateways.isEmpty())
    }
}
