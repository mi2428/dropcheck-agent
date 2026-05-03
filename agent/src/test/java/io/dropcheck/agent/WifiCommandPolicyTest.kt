package io.dropcheck.agent

import io.dropcheck.agent.grpc.AssertWifi
import io.dropcheck.agent.grpc.ConnectWifi
import io.dropcheck.agent.grpc.CycleWifi
import io.dropcheck.agent.grpc.WaitWifiConnected
import io.dropcheck.agent.grpc.WifiBand
import io.dropcheck.agent.grpc.WifiCycleResult
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class WifiCommandPolicyTest {
    @Test
    fun appliesProtoZeroTimeoutDefaults() {
        assertEquals(12_345, WifiCommandPolicy.effectiveTimeoutMs(12_345, WifiCommandPolicy.DEFAULT_WAIT_TIMEOUT_MS))
        assertEquals(WifiCommandPolicy.DEFAULT_WAIT_TIMEOUT_MS, WifiCommandPolicy.effectiveTimeoutMs(0, WifiCommandPolicy.DEFAULT_WAIT_TIMEOUT_MS))
        assertEquals(WifiCommandPolicy.DEFAULT_CONNECT_TIMEOUT_MS, WifiCommandPolicy.effectiveTimeoutMs(-1, WifiCommandPolicy.DEFAULT_CONNECT_TIMEOUT_MS))
    }

    @Test
    fun classifiesFreshScanAndScanDetailResults() {
        assertTrue(WifiCommandPolicy.freshScanCompleted(emptyList()))
        assertTrue(WifiCommandPolicy.freshScanCompleted(listOf("get_scan_results=SecurityException:denied")))
        assertFalse(WifiCommandPolicy.freshScanCompleted(listOf("start_scan=false")))
        assertFalse(WifiCommandPolicy.freshScanCompleted(listOf("scan_broadcast_timeout=10000ms")))

        assertTrue(WifiCommandPolicy.scanDetailMatched(resultCount = 1, errors = emptyList()))
        assertFalse(WifiCommandPolicy.scanDetailMatched(resultCount = 0, errors = emptyList()))
        assertFalse(WifiCommandPolicy.scanDetailMatched(resultCount = 1, errors = listOf("scan_detail_no_match")))
    }

    @Test
    fun mapsConnectionCommandsToExpectations() {
        val connect = ConnectWifi.newBuilder()
            .setSsid("Lab")
            .setBssid("aa:bb:cc:dd:ee:ff")
            .setSecurity(ConnectWifi.Security.SECURITY_WPA3_SAE)
            .setBand(WifiBand.WIFI_BAND_6_GHZ)
            .build()

        val connectExpectation = WifiCommandPolicy.connectExpectation(connect)

        assertEquals("Lab", connectExpectation.ssid)
        assertEquals("aa:bb:cc:dd:ee:ff", connectExpectation.bssid)
        assertEquals(ConnectWifi.Security.SECURITY_WPA3_SAE, connectExpectation.security)
        assertEquals(WifiBand.WIFI_BAND_6_GHZ, connectExpectation.band)
        assertTrue(connectExpectation.requireIp)
        assertFalse(connectExpectation.requireValidated)

        val wait = WaitWifiConnected.newBuilder()
            .setSsid("Lab")
            .setRequireValidated(true)
            .build()

        assertTrue(WifiCommandPolicy.waitExpectation(wait).requireValidated)

        val assertion = AssertWifi.newBuilder()
            .setSsid("Lab")
            .setBssid("11:22:33:44:55:66")
            .setSecurity(ConnectWifi.Security.SECURITY_WPA2_PSK)
            .setBand(WifiBand.WIFI_BAND_5_GHZ)
            .setRequireIp(true)
            .build()

        val assertExpectation = WifiCommandPolicy.assertExpectation(assertion)

        assertEquals("Lab", assertExpectation.ssid)
        assertEquals("11:22:33:44:55:66", assertExpectation.bssid)
        assertEquals(ConnectWifi.Security.SECURITY_WPA2_PSK, assertExpectation.security)
        assertEquals(WifiBand.WIFI_BAND_5_GHZ, assertExpectation.band)
        assertTrue(assertExpectation.requireIp)
        assertFalse(assertExpectation.requireValidated)
    }

    @Test
    fun boundsCycleInputsAndClassifiesCycleResults() {
        assertEquals(1, WifiCommandPolicy.cycleCount(CycleWifi.newBuilder().setCount(0).build()))
        assertEquals(100, WifiCommandPolicy.cycleCount(CycleWifi.newBuilder().setCount(150).build()))
        assertEquals(3, WifiCommandPolicy.cycleCount(CycleWifi.newBuilder().setCount(3).build()))
        assertEquals(0, WifiCommandPolicy.cyclePauseMs(CycleWifi.newBuilder().setPauseMs(-1).build()))
        assertEquals(60_000, WifiCommandPolicy.cyclePauseMs(CycleWifi.newBuilder().setPauseMs(90_000).build()))

        assertTrue(
            WifiCommandPolicy.cycleStepPassed(
                connected = true,
                pingRequested = false,
                pingOk = false,
                httpRequested = true,
                httpOk = true,
            ),
        )
        assertFalse(
            WifiCommandPolicy.cycleStepPassed(
                connected = true,
                pingRequested = true,
                pingOk = false,
                httpRequested = false,
                httpOk = false,
            ),
        )

        assertTrue(
            WifiCommandPolicy.cyclePassed(
                WifiCycleResult.newBuilder()
                    .setRequestedCount(2)
                    .setCompletedCount(2)
                    .setPassedCount(2)
                    .build(),
            ),
        )
        assertFalse(
            WifiCommandPolicy.cyclePassed(
                WifiCycleResult.newBuilder()
                    .setRequestedCount(2)
                    .setCompletedCount(2)
                    .setPassedCount(1)
                    .build(),
            ),
        )
    }
}
