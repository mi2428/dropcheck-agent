package io.dropcheck.agent

import io.dropcheck.agent.grpc.ConnectWifi
import io.dropcheck.agent.grpc.IpStatus
import io.dropcheck.agent.grpc.WifiBand
import io.dropcheck.agent.grpc.WifiConnection
import io.dropcheck.agent.grpc.WifiStatus
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class WifiExpectationEvaluatorTest {
    @Test
    fun passesWhenConnectionMatchesEveryRequestedCondition() {
        val status = wifiStatus(
            ssid = "Lab",
            bssid = "AA:BB:CC:DD:EE:FF",
            security = "sae",
            frequencyMhz = 5975,
            ipAddresses = listOf("192.0.2.10/24"),
            validated = true,
        )

        val result = WifiExpectationEvaluator.evaluate(
            status,
            WifiExpectation(
                ssid = "Lab",
                bssid = "aa:bb:cc:dd:ee:ff",
                security = ConnectWifi.Security.SECURITY_WPA3_SAE,
                band = WifiBand.WIFI_BAND_6_GHZ,
                requireIp = true,
                requireValidated = true,
            ),
            elapsedMs = 42,
        )

        assertTrue(result.passed)
        assertEquals(42, result.elapsedMs)
        assertEquals(
            listOf("connected", "ssid", "bssid", "security", "band", "ip", "validated"),
            result.checksList.map { it.key },
        )
        assertTrue(result.checksList.all { it.passed })
    }

    @Test
    fun acceptsTransitionSecurityWhenCurrentSecurityIsPsk() {
        val status = wifiStatus(ssid = "Lab", security = "psk")

        val result = WifiExpectationEvaluator.evaluate(
            status,
            WifiExpectation(security = ConnectWifi.Security.SECURITY_WPA2_WPA3_TRANSITION),
            elapsedMs = 0,
        )

        assertTrue(result.passed)
        assertEquals("psk|sae", result.checksList.single { it.key == "security" }.expected)
    }

    @Test
    fun failsWithSpecificChecksWhenConnectionDoesNotMatch() {
        val status = wifiStatus(
            ssid = "Guest",
            bssid = "00:11:22:33:44:55",
            security = "psk",
            frequencyMhz = 2412,
            ipAddresses = emptyList(),
            validated = false,
        )

        val result = WifiExpectationEvaluator.evaluate(
            status,
            WifiExpectation(
                ssid = "Lab",
                bssid = "aa:bb:cc:dd:ee:ff",
                security = ConnectWifi.Security.SECURITY_WPA3_SAE,
                band = WifiBand.WIFI_BAND_5_GHZ,
                requireIp = true,
                requireValidated = true,
            ),
            elapsedMs = 100,
        )

        assertFalse(result.passed)
        assertFalse(result.checksList.single { it.key == "ssid" }.passed)
        assertFalse(result.checksList.single { it.key == "bssid" }.passed)
        assertFalse(result.checksList.single { it.key == "security" }.passed)
        assertFalse(result.checksList.single { it.key == "band" }.passed)
        assertFalse(result.checksList.single { it.key == "ip" }.passed)
        assertFalse(result.checksList.single { it.key == "validated" }.passed)
    }

    @Test
    fun failsConnectedCheckWhenNoConnectionIsPresent() {
        val result = WifiExpectationEvaluator.evaluate(
            WifiStatus.getDefaultInstance(),
            WifiExpectation(),
            elapsedMs = 0,
        )

        assertFalse(result.passed)
        assertEquals("connected", result.checksList.single().key)
        assertEquals("false", result.checksList.single().actual)
    }

    @Test
    fun classifiesWifiBandsByFrequency() {
        assertTrue(frequencyMatchesWifiBand(2412, WifiBand.WIFI_BAND_2_4_GHZ))
        assertFalse(frequencyMatchesWifiBand(2412, WifiBand.WIFI_BAND_5_GHZ))
        assertTrue(frequencyMatchesWifiBand(0, WifiBand.WIFI_BAND_ALL))
    }

    private fun wifiStatus(
        ssid: String,
        bssid: String = "",
        security: String = "",
        frequencyMhz: Int = 0,
        ipAddresses: List<String> = listOf("192.0.2.10/24"),
        validated: Boolean = false,
    ): WifiStatus {
        val connection = WifiConnection.newBuilder()
            .setSsid(ssid)
            .setBssid(bssid)
            .setSecurityType(security)
            .setFrequencyMhz(frequencyMhz)
            .setIpv4Address(ipAddresses.firstOrNull().orEmpty().substringBefore("/"))
            .build()
        val ip = IpStatus.newBuilder()
            .addAllAddresses(ipAddresses)
            .setValidated(validated)
            .build()
        return WifiStatus.newBuilder()
            .setConnection(connection)
            .setIpStatus(ip)
            .build()
    }
}
