package io.dropcheck.agent

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertSame
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

class WifiNetworkInfoTest {
    @Test fun requestedStatusNetworkNeverFallsBack() {
        assertNull(selectedStatusNetwork(true, null as String?, "cellular"))
        assertEquals("selected", selectedStatusNetwork(true, "selected", "other"))
        assertEquals("default", selectedStatusNetwork(false, null as String?, "default"))
    }
    @Test
    fun unknownCandidateIsExplicitlyRejectedWithoutInterruptingKnownTargetSelection() {
        assertEquals(WifiNetworkMatch.MATCH, networkWifiSsidMatch("Fixture", "\"Fixture\""))
        assertEquals(WifiNetworkMatch.SSID_MISMATCH, networkWifiSsidMatch("Fixture", "Other"))
        assertEquals(WifiNetworkMatch.MATCH, networkWifiSsidMatch("", ""))
        for (unknown in listOf("", "<unknown ssid>", "\"<unknown ssid>\"")) {
            assertEquals(WifiNetworkMatch.IDENTITY_UNAVAILABLE, networkWifiSsidMatch("Fixture", unknown))
        }
        val rejected = mutableListOf<String>()
        // Active/first A is redacted, B is known and matches the requested SSID.
        assertEquals("active-a", selectWifiCandidate(listOf("active-a", "target-b"), classify = {
            networkWifiSsidMatch("", "")
        }, onCandidate = { _, _ -> }))
        assertEquals("target-b", selectWifiCandidate(listOf("active-a", "target-b"), classify = {
            networkWifiSsidMatch("FixtureB", if (it == "active-a") "<unknown ssid>" else "FixtureB")
        }, onCandidate = { network, match -> if (match != WifiNetworkMatch.MATCH) rejected += "$network:${match.rejectReason}" }))
        assertEquals(listOf("active-a:wifi_identity_unavailable"), rejected)
        val failure = assertThrows(IllegalStateException::class.java) {
            selectWifiCandidate(listOf("unknown-a", "unknown-b"), classify = {
                networkWifiSsidMatch("FixtureB", "<unknown ssid>")
            }, onCandidate = { _, _ -> })
        }
        assertEquals("wifi identity unavailable; cannot select requested Network", failure.message)
        assertEquals("physical", selectWifiCandidate(listOf("vpn", "physical"), classify = {
            if (!isPhysicalWifiNetwork(true, it == "vpn")) WifiNetworkMatch.NOT_WIFI else networkWifiSsidMatch("", "")
        }, onCandidate = { _, _ -> }))
        assertNull(selectWifiCandidate(listOf("other"), classify = { networkWifiSsidMatch("FixtureB", "Other") },
            onCandidate = { _, _ -> }))
    }

    @Test fun bssidSelectionRejectsSameSsidRoamAndRedactedIdentity() {
        val target = "aa:bb:cc:dd:ee:ff"
        assertEquals(WifiNetworkMatch.BSSID_MISMATCH,
            networkWifiIdentityMatch("Lab", target, "Lab", "aa:bb:cc:dd:ee:01"))
        assertEquals(WifiNetworkMatch.IDENTITY_UNAVAILABLE,
            networkWifiIdentityMatch("Lab", target, "Lab", "02:00:00:00:00:00"))
        val selected = selectWifiCandidate(listOf("old", "redacted", "new"), classify = {
            networkWifiIdentityMatch("Lab", target, "Lab", when (it) {
                "old" -> "aa:bb:cc:dd:ee:01"
                "redacted" -> "02:00:00:00:00:00"
                else -> target.uppercase()
            })
        }, onCandidate = { _, _ -> })
        assertEquals("new", selected)
        assertThrows(IllegalStateException::class.java) {
            selectWifiCandidate(listOf("redacted"), classify = {
                networkWifiIdentityMatch("Lab", target, "Lab", "02:00:00:00:00:00")
            }, onCandidate = { _, _ -> })
        }
    }

    @Test fun networkIdRejectsSameSsidOnAnotherAndroidNetwork() {
        assertEquals(WifiNetworkMatch.NETWORK_ID_MISMATCH, networkWifiIdMatch("949", "950"))
        assertEquals(WifiNetworkMatch.MATCH, networkWifiIdMatch("949", "949"))
        assertEquals(WifiNetworkMatch.MATCH, networkWifiIdMatch("", "950"))
        assertEquals("new", selectWifiCandidate(listOf("old", "new"), classify = {
            networkWifiIdMatch("949", if (it == "old") "948" else "949")
        }, onCandidate = { _, _ -> }))
    }

    @Test fun literalSsidSelectionNeverFallsBackToWrongWifiOrVpn() {
        val cases = listOf(
            "Lab" to "\"Lab\"",
            " Lab " to "\" Lab \"",
            "\"Lab\"" to "\"\"Lab\"\"",
            "La\"b" to "\"La\"b\"",
            " " to "\" \"",
        )
        for ((requested, frameworkSsid) in cases) {
            assertEquals(requested, normalizedWifiSsid(frameworkSsid))
            assertTrue(isKnownWifiSsid(frameworkSsid))
            val candidates = listOf("vpn", "ordinary", "redacted", "target")
            val selected = selectWifiCandidate(candidates, classify = { candidate ->
                if (candidate == "vpn") WifiNetworkMatch.NOT_WIFI else networkWifiSsidMatch(requested, when (candidate) {
                    "ordinary" -> "\"Lab\""
                    "redacted" -> "<unknown ssid>"
                    else -> frameworkSsid
                })
            }, onCandidate = { _, _ -> })
            assertEquals("target for literal '$requested'", if (requested == "Lab") "ordinary" else "target", selected)
            if (requested != "Lab") {
                assertEquals(WifiNetworkMatch.SSID_MISMATCH, networkWifiSsidMatch(requested, "\"Lab\""))
            }
            assertEquals(WifiNetworkMatch.IDENTITY_UNAVAILABLE, networkWifiSsidMatch(requested, "<unknown ssid>"))
        }
        assertEquals(WifiNetworkMatch.MATCH, networkWifiSsidMatch("", "<unknown ssid>")) // Only empty selector may ignore identity.
        assertEquals(WifiNetworkMatch.SSID_MISMATCH, networkWifiSsidMatch(" ", "\"Lab\""))
        assertEquals(WifiNetworkMatch.SSID_MISMATCH, networkWifiSsidMatch("lab", "\"Lab\""))
        val candidates = listOf("vpn", "ordinary", "unknown")
        assertThrows(IllegalStateException::class.java) {
            selectWifiCandidate(candidates, classify = { candidate ->
                if (candidate == "vpn") WifiNetworkMatch.NOT_WIFI else networkWifiSsidMatch(" ", if (candidate == "unknown") "<unknown ssid>" else "\"Lab\"")
            }, onCandidate = { _, _ -> })
        }
    }

    @Test
    fun twoNetworksNeverShareTheFirstNetworksIdentityWhenSecondIsRedacted() {
        var unregistered = 0
        val first = awaitNetworkValue<String, String>("network-a", 0, register = { report ->
            report("network-a", "FixtureA")
            report("network-b", null)
        }, unregister = { unregistered++ })
        val second = awaitNetworkValue<String, String>("network-b", 0, register = { report ->
            report("network-a", "FixtureA")
            report("network-b", null)
        }, unregister = { unregistered++ })
        assertEquals("FixtureA", first)
        assertNull(second)
        assertEquals(2, unregistered)
    }

    @Test
    fun locationInclusiveObservationRetainsSingleWifiAndDistinctConcurrentIdentities() {
        for ((network, ssid) in listOf("network-a" to "FixtureA", "network-b" to "FixtureB")) {
            assertEquals(ssid, awaitNetworkValue<String, String>(network, 0, register = { report ->
                report("network-a", "FixtureA")
                report("network-b", "FixtureB")
            }, unregister = { }))
        }
        assertEquals("FixtureSingle", awaitNetworkValue<String, String>("single", 0, register = {
            it("single", "FixtureSingle")
        }, unregister = { }))
    }

    @Test
    fun vpnUnderlyingWifiTransportIsNotAPhysicalWifiCandidate() {
        assertTrue(isPhysicalWifiNetwork(wifiTransport = true, vpnTransport = false))
        assertFalse(isPhysicalWifiNetwork(wifiTransport = true, vpnTransport = true))
        assertFalse(isPhysicalWifiNetwork(wifiTransport = false, vpnTransport = false))
        assertNull(awaitNetworkValue<String, String>("physical", 0, register = {
            it("vpn", "UnderlyingFixture")
        }, unregister = { }))
    }

    @Test
    fun lossAndReplacementDuringSnapshotCannotSupplyAnotherNetworksInfo() {
        assertNull(awaitNetworkValue<String, String>("old", 0, register = { report ->
            report("old", "FixtureOld")
            report("old", null)
            report("replacement", "FixtureNew")
        }, unregister = { }))
        assertNull(awaitNetworkValue<String, String>("missing", 0, register = { }, unregister = { }))
    }

    @Test
    fun interruptionAndCleanupFailuresRemainVisibleAndUnregisterExactlyOnce() {
        var unregistered = 0
        Thread.currentThread().interrupt()
        try {
            assertThrows(InterruptedException::class.java) {
                awaitNetworkValue<String, String>("waiting", 1000, register = { }, unregister = { unregistered++ })
            }
        } finally {
            Thread.interrupted()
        }
        assertEquals(1, unregistered)
        val failure = SecurityException("unregister denied")
        assertSame(failure, assertThrows(SecurityException::class.java) {
            awaitNetworkValue<String, String>("single", 0, register = { it("single", "Fixture") },
                unregister = { throw failure })
        })
        assertThrows(SecurityException::class.java) {
            awaitNetworkValue<String, String>("single", 0, register = { throw SecurityException("register denied") },
                unregister = { error("must not unregister a failed registration") })
        }
    }
}
