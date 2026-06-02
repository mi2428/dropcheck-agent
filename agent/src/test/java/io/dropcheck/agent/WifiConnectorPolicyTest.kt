package io.dropcheck.agent

import io.dropcheck.agent.grpc.ConnectWifi
import io.dropcheck.agent.grpc.WifiBand
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
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
    fun parsesSecurityFromScanCapabilities() {
        assertEquals(
            ConnectWifi.Security.SECURITY_WPA3_SAE,
            WifiConnectorPolicy.securityFromCapabilities("[WPA2-PSK-CCMP][RSN-SAE-CCMP][ESS]"),
        )
        assertEquals(
            ConnectWifi.Security.SECURITY_WPA2_PSK,
            WifiConnectorPolicy.securityFromCapabilities("[WPA2-PSK-CCMP][ESS]"),
        )
        assertNull(WifiConnectorPolicy.securityFromCapabilities("[OWE][ESS]"))
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
    fun resolvesBandOnlyConnectToStrongestMatchingBssid() {
        val candidates = listOf(
            WifiConnectorPolicy.ScanSecurityCandidate(
                ssid = "Lab",
                bssid = "00:00:00:00:00:01",
                capabilities = "[RSN-SAE-CCMP][ESS]",
                frequencyMhz = 2412,
                levelDbm = -70,
            ),
            WifiConnectorPolicy.ScanSecurityCandidate(
                ssid = "Lab",
                bssid = "00:00:00:00:00:02",
                capabilities = "[RSN-SAE-CCMP][ESS]",
                frequencyMhz = 2462,
                levelDbm = -42,
            ),
            WifiConnectorPolicy.ScanSecurityCandidate(
                ssid = "Lab",
                bssid = "70:a7:41:a0:9a:6f",
                capabilities = "[RSN-SAE-CCMP][ESS]",
                frequencyMhz = 5200,
                levelDbm = -35,
            ),
        )

        assertEquals(
            "00:00:00:00:00:02",
            WifiConnectorPolicy.resolveConnectBssid(
                requested = "",
                candidates = candidates,
                ssid = "Lab",
                band = WifiBand.WIFI_BAND_2_4_GHZ,
                security = ConnectWifi.Security.SECURITY_UNSPECIFIED,
            ),
        )
    }

    @Test
    fun keepsExplicitBssidWhenResolvingConnectBssid() {
        assertEquals(
            "22:0B:8B:B6:2C:E0",
            WifiConnectorPolicy.resolveConnectBssid(
                requested = "22:0B:8B:B6:2C:E0",
                candidates = emptyList(),
                ssid = "Lab",
                band = WifiBand.WIFI_BAND_2_4_GHZ,
                security = ConnectWifi.Security.SECURITY_UNSPECIFIED,
            ),
        )
    }

    @Test
    fun leavesConnectBssidBlankWithoutSpecificBand() {
        val candidates = listOf(
            WifiConnectorPolicy.ScanSecurityCandidate(
                ssid = "Lab",
                bssid = "00:00:00:00:00:01",
                capabilities = "[RSN-SAE-CCMP][ESS]",
                frequencyMhz = 2412,
                levelDbm = -42,
            ),
        )

        assertEquals(
            "",
            WifiConnectorPolicy.resolveConnectBssid(
                requested = "",
                candidates = candidates,
                ssid = "Lab",
                band = WifiBand.WIFI_BAND_ALL,
                security = ConnectWifi.Security.SECURITY_UNSPECIFIED,
            ),
        )
    }

    @Test
    fun prefersBandPinnedBssidThatMatchesRequestedSecurity() {
        val candidates = listOf(
            WifiConnectorPolicy.ScanSecurityCandidate(
                ssid = "Lab",
                bssid = "00:00:00:00:00:01",
                capabilities = "[RSN-SAE-CCMP][ESS]",
                frequencyMhz = 5200,
                levelDbm = -35,
            ),
            WifiConnectorPolicy.ScanSecurityCandidate(
                ssid = "Lab",
                bssid = "00:00:00:00:00:02",
                capabilities = "[RSN-PSK-CCMP][ESS]",
                frequencyMhz = 5220,
                levelDbm = -45,
            ),
        )

        assertEquals(
            "00:00:00:00:00:02",
            WifiConnectorPolicy.resolveConnectBssid(
                requested = "",
                candidates = candidates,
                ssid = "Lab",
                band = WifiBand.WIFI_BAND_5_GHZ,
                security = ConnectWifi.Security.SECURITY_WPA2_PSK,
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
    fun selectsConflictingProfilesBeforeBssidPinnedConnect() {
        val current = WifiConnectorPolicy.CurrentConnectionRef(
            networkId = 10,
            ssid = "Lab",
            bssid = "70:a7:41:a0:9a:6f",
            frequencyMhz = 5200,
            securityType = "sae",
        )
        val configs = listOf(
            WifiConnectorPolicy.ConfiguredNetworkRef(networkId = 7, ssid = "\"Lab\""),
            WifiConnectorPolicy.ConfiguredNetworkRef(networkId = 8, ssid = "\"Lab\"", bssid = "22:0b:8b:b6:2c:e1"),
            WifiConnectorPolicy.ConfiguredNetworkRef(networkId = 9, ssid = "\"Guest\""),
        )

        assertEquals(
            listOf(7),
            WifiConnectorPolicy.bssidPinCleanupNetworkIds(
                ssid = "Lab",
                bssid = "22:0B:8B:B6:2C:E1",
                current = current,
                configs = configs,
            ),
        )
    }

    @Test
    fun keepsProfilesWhenBssidPinnedConnectAlreadyMatches() {
        val current = WifiConnectorPolicy.CurrentConnectionRef(
            networkId = 10,
            ssid = "Lab",
            bssid = "22:0b:8b:b6:2c:e1",
            frequencyMhz = 5220,
            securityType = "sae",
        )
        val configs = listOf(
            WifiConnectorPolicy.ConfiguredNetworkRef(networkId = 8, ssid = "\"Lab\"", bssid = "22:0b:8b:b6:2c:e1"),
        )

        assertEquals(
            emptyList<Int>(),
            WifiConnectorPolicy.bssidPinCleanupNetworkIds(
                ssid = "Lab",
                bssid = "22:0B:8B:B6:2C:E1",
                current = current,
                configs = configs,
            ),
        )
    }

    @Test
    fun treatsDisconnectedFrameworkStatesAsSettled() {
        assertTrue(WifiConnectorPolicy.disconnectSettled(-1, "\"Lab\"", "70:a7:41:a0:9a:6f", "COMPLETED"))
        assertTrue(WifiConnectorPolicy.disconnectSettled(7, "<unknown ssid>", "", "SCANNING"))
        assertTrue(WifiConnectorPolicy.disconnectSettled(7, "\"Lab\"", "00:00:00:00:00:00", "DISCONNECTED"))
        assertFalse(WifiConnectorPolicy.disconnectSettled(7, "\"Lab\"", "70:a7:41:a0:9a:6f", "COMPLETED"))
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
