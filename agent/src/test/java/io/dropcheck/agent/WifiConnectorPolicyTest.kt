package io.dropcheck.agent

import io.dropcheck.agent.grpc.ConnectWifi
import io.dropcheck.agent.grpc.WifiBand
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class WifiConnectorPolicyTest {
    @Test
    fun quotesWifiConfigurationStringsOnlyWhenNeeded() {
        assertEquals("\"Lab\"", WifiConnectorPolicy.quoteWifi("Lab"))
        assertEquals("\"Lab\"", WifiConnectorPolicy.quoteWifi("\"Lab\""))
    }

    @Test
    fun infersWpa3SecurityFromScanCapabilities() {
        val candidates = listOf(
            WifiConnectorPolicy.ScanSecurityCandidate(
                ssid = "Lab",
                bssid = "70:a7:41:a0:9a:6f",
                capabilities = "[WPA2-PSK-CCMP][RSN-SAE-CCMP][ESS]",
                frequencyMhz = 5200,
                levelDbm = -54,
            ),
        )

        assertEquals(
            ConnectWifi.Security.SECURITY_WPA3_SAE,
            WifiConnectorPolicy.resolveConnectSecurity(
                requested = ConnectWifi.Security.SECURITY_UNSPECIFIED,
                candidates = candidates,
                ssid = "Lab",
                bssid = "",
                band = WifiBand.WIFI_BAND_ALL,
            ),
        )
    }

    @Test
    fun fallsBackToWpa2WhenAutoSecurityHasNoMatchingScanResult() {
        assertEquals(
            ConnectWifi.Security.SECURITY_WPA2_PSK,
            WifiConnectorPolicy.resolveConnectSecurity(
                requested = ConnectWifi.Security.SECURITY_UNSPECIFIED,
                candidates = emptyList(),
                ssid = "Hidden",
                bssid = "",
                band = WifiBand.WIFI_BAND_ALL,
            ),
        )
    }

    @Test
    fun keepsExplicitSecuritySelection() {
        val candidates = listOf(
            WifiConnectorPolicy.ScanSecurityCandidate(
                ssid = "Lab",
                bssid = "70:a7:41:a0:9a:6f",
                capabilities = "[RSN-SAE-CCMP][ESS]",
                frequencyMhz = 5200,
                levelDbm = -54,
            ),
        )

        assertEquals(
            ConnectWifi.Security.SECURITY_WPA2_PSK,
            WifiConnectorPolicy.resolveConnectSecurity(
                requested = ConnectWifi.Security.SECURITY_WPA2_PSK,
                candidates = candidates,
                ssid = "Lab",
                bssid = "",
                band = WifiBand.WIFI_BAND_ALL,
            ),
        )
    }

    @Test
    fun filtersAutoSecurityCandidatesByBssidAndBand() {
        val candidates = listOf(
            WifiConnectorPolicy.ScanSecurityCandidate(
                ssid = "Lab",
                bssid = "00:00:00:00:00:01",
                capabilities = "[RSN-PSK-CCMP][ESS]",
                frequencyMhz = 2412,
                levelDbm = -20,
            ),
            WifiConnectorPolicy.ScanSecurityCandidate(
                ssid = "Lab",
                bssid = "70:a7:41:a0:9a:6f",
                capabilities = "[RSN-SAE-CCMP][ESS]",
                frequencyMhz = 5200,
                levelDbm = -54,
            ),
        )

        assertEquals(
            ConnectWifi.Security.SECURITY_WPA3_SAE,
            WifiConnectorPolicy.resolveConnectSecurity(
                requested = ConnectWifi.Security.SECURITY_UNSPECIFIED,
                candidates = candidates,
                ssid = "Lab",
                bssid = "70:a7:41:a0:9a:6f",
                band = WifiBand.WIFI_BAND_5_GHZ,
            ),
        )
    }

    @Test
    fun acceptsAlreadyConnectedNetworkWhenItMatchesConnectRequest() {
        val current = WifiConnectorPolicy.CurrentConnectionRef(
            networkId = 10,
            ssid = "Lab",
            bssid = "70:a7:41:a0:9a:6f",
            frequencyMhz = 5200,
            securityType = "sae",
        )

        assertTrue(
            WifiConnectorPolicy.currentConnectionSatisfiesConnect(
                current = current,
                ssid = "Lab",
                bssid = "70:A7:41:A0:9A:6F",
                security = ConnectWifi.Security.SECURITY_WPA3_SAE,
                band = WifiBand.WIFI_BAND_5_GHZ,
            ),
        )
        assertTrue(
            WifiConnectorPolicy.currentConnectionSatisfiesConnect(
                current = current,
                ssid = "Lab",
                bssid = "",
                security = ConnectWifi.Security.SECURITY_UNSPECIFIED,
                band = WifiBand.WIFI_BAND_ALL,
            ),
        )
    }

    @Test
    fun rejectsAlreadyConnectedNetworkWhenRequestedPropertiesDiffer() {
        val current = WifiConnectorPolicy.CurrentConnectionRef(
            networkId = 10,
            ssid = "Lab",
            bssid = "70:a7:41:a0:9a:6f",
            frequencyMhz = 5200,
            securityType = "sae",
        )

        assertFalse(
            WifiConnectorPolicy.currentConnectionSatisfiesConnect(
                current = current,
                ssid = "Guest",
                bssid = "",
                security = ConnectWifi.Security.SECURITY_UNSPECIFIED,
                band = WifiBand.WIFI_BAND_ALL,
            ),
        )
        assertFalse(
            WifiConnectorPolicy.currentConnectionSatisfiesConnect(
                current = current,
                ssid = "Lab",
                bssid = "",
                security = ConnectWifi.Security.SECURITY_WPA2_PSK,
                band = WifiBand.WIFI_BAND_ALL,
            ),
        )
        assertFalse(
            WifiConnectorPolicy.currentConnectionSatisfiesConnect(
                current = current,
                ssid = "Lab",
                bssid = "",
                security = ConnectWifi.Security.SECURITY_UNSPECIFIED,
                band = WifiBand.WIFI_BAND_2_4_GHZ,
            ),
        )
    }

    @Test
    fun matchesCurrentConnectionForgetTargets() {
        val current = WifiConnectorPolicy.CurrentConnectionRef(
            networkId = 10,
            ssid = "Lab",
            bssid = "70:a7:41:a0:9a:6f",
            frequencyMhz = 5200,
            securityType = "sae",
        )

        assertTrue(WifiConnectorPolicy.currentConnectionMatchesForgetTarget("Lab", current))
        assertTrue(WifiConnectorPolicy.currentConnectionMatchesForgetTarget("10", current))
        assertTrue(WifiConnectorPolicy.currentConnectionMatchesForgetTarget("70:A7:41:A0:9A:6F", current))
        assertFalse(WifiConnectorPolicy.currentConnectionMatchesForgetTarget("Guest", current))
        assertFalse(WifiConnectorPolicy.currentConnectionMatchesForgetTarget("Lab", null))
    }

    @Test
    fun selectsForgetTargetsBySsidOrNetworkId() {
        val configs = listOf(
            WifiConnectorPolicy.ConfiguredNetworkRef(networkId = 7, ssid = "\"Lab\""),
            WifiConnectorPolicy.ConfiguredNetworkRef(networkId = 8, ssid = "Guest"),
        )

        assertEquals(listOf(7), WifiConnectorPolicy.forgetNetworkIds("Lab", configs))
        assertEquals(listOf(8), WifiConnectorPolicy.forgetNetworkIds("8", configs))
        assertEquals(listOf(42), WifiConnectorPolicy.forgetNetworkIds("42", configs))
        assertEquals(emptyList<Int>(), WifiConnectorPolicy.forgetNetworkIds("Missing", configs))
        assertEquals(emptyList<Int>(), WifiConnectorPolicy.forgetNetworkIds("", configs))
    }

    @Test
    fun requiresAtLeastOneSuccessfulRemoveAndNoErrors() {
        assertTrue(
            WifiConnectorPolicy.forgetSucceeded(
                fields = listOf("remove_7" to "true"),
                errors = emptyList(),
            ),
        )
        assertFalse(
            WifiConnectorPolicy.forgetSucceeded(
                fields = listOf("remove_7" to "false"),
                errors = emptyList(),
            ),
        )
        assertFalse(
            WifiConnectorPolicy.forgetSucceeded(
                fields = listOf("remove_7" to "true"),
                errors = listOf("disable_7=SecurityException"),
            ),
        )
    }
}
