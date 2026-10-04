package io.dropcheck.agent

import java.io.File
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertSame
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

class WifiNetworkInfoTest {
    @Test
    fun allNetworkIdentityCallersUseAssociationInsteadOfGlobalSnapshot() {
        val sourceDir = listOf(File("src/main/java/io/dropcheck/agent"),
            File("agent/src/main/java/io/dropcheck/agent")).first { it.isDirectory }
        for (file in listOf("NetworkRepository.kt", "AgentClockWidgetProvider.kt", "WifiEventLogger.kt")) {
            val source = File(sourceDir, file).readText()
            assertTrue("$file must use the shared association", source.contains("networkWifiInfo("))
            assertFalse("$file must not map a global snapshot", source.contains("connectionInfo"))
            assertFalse("$file must not keep the old fallback", source.contains("bestWifiInfo("))
        }
        val shared = File(sourceDir, "WifiNetworkInfo.kt").readText()
        assertTrue(shared.contains("FLAG_INCLUDE_LOCATION_INFO"))
        assertTrue(shared.contains("registerNetworkCallback(request, callback, Handler(delivery.looper))"))
        assertTrue(shared.contains("isKnownWifiSsid(it.ssid.orEmpty()) || clockWidgetWifiInfoIsUsable"))
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
        assertEquals("wifi identity unavailable; cannot select SSID", failure.message)
        assertEquals("physical", selectWifiCandidate(listOf("vpn", "physical"), classify = {
            if (!isPhysicalWifiNetwork(true, it == "vpn")) WifiNetworkMatch.NOT_WIFI else networkWifiSsidMatch("", "")
        }, onCandidate = { _, _ -> }))
        assertNull(selectWifiCandidate(listOf("other"), classify = { networkWifiSsidMatch("FixtureB", "Other") },
            onCandidate = { _, _ -> }))
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

    @Test fun selectorBoundaryRejectsBlankOnlyWildcardRegression() {
        val sourceDir = listOf(File("src/main/java/io/dropcheck/agent"),
            File("agent/src/main/java/io/dropcheck/agent")).first { it.isDirectory }
        val source = File(sourceDir, "NetworkRepository.kt").readText()
        assertTrue(source.contains("if (selector.ssid.isEmpty()) return WifiNetworkMatch.MATCH"))
        assertFalse(source.contains("selector.ssid.isBlank()"))
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
