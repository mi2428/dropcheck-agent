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
                bssid = "02:00:00:00:00:21",
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
                bssid = "02:00:00:00:00:21",
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
                bssid = "02:00:00:00:00:21",
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
                bssid = "02:00:00:00:00:21",
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
                bssid = "02:00:00:00:00:21",
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
            ),
        )
    }

    @Test
    fun keepsExplicitBssidWhenResolvingConnectBssid() {
        assertEquals(
            "02:00:00:00:00:10",
            WifiConnectorPolicy.resolveConnectBssid(
                requested = "02:00:00:00:00:10",
                candidates = emptyList(),
                ssid = "Lab",
                band = WifiBand.WIFI_BAND_2_4_GHZ,
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
            ),
        )
    }

    @Test
    fun acceptsAlreadyConnectedNetworkWhenItMatchesConnectRequest() {
        val current = WifiConnectorPolicy.CurrentConnectionRef(
            networkId = 10,
            ssid = "Lab",
            bssid = "02:00:00:00:00:21",
            frequencyMhz = 5200,
            securityType = "sae",
        )

        assertTrue(
            WifiConnectorPolicy.currentConnectionSatisfiesConnect(
                current = current,
                ssid = "Lab",
                bssid = "02:00:00:00:00:21",
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
            bssid = "02:00:00:00:00:21",
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
            bssid = "02:00:00:00:00:21",
            frequencyMhz = 5200,
            securityType = "sae",
        )

        assertTrue(WifiConnectorPolicy.currentConnectionMatchesForgetTarget("Lab", current))
        assertTrue(WifiConnectorPolicy.currentConnectionMatchesForgetTarget("10", current))
        assertTrue(WifiConnectorPolicy.currentConnectionMatchesForgetTarget("02:00:00:00:00:21", current))
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
            bssid = "02:00:00:00:00:21",
            frequencyMhz = 5200,
            securityType = "sae",
        )
        val configs = listOf(
            WifiConnectorPolicy.ConfiguredNetworkRef(networkId = 7, ssid = "\"Lab\""),
            WifiConnectorPolicy.ConfiguredNetworkRef(networkId = 8, ssid = "\"Lab\"", bssid = "02:00:00:00:00:11"),
            WifiConnectorPolicy.ConfiguredNetworkRef(networkId = 9, ssid = "\"Guest\""),
        )

        assertEquals(
            listOf(7),
            WifiConnectorPolicy.bssidPinCleanupNetworkIds(
                ssid = "Lab",
                bssid = "02:00:00:00:00:11",
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
            bssid = "02:00:00:00:00:11",
            frequencyMhz = 5220,
            securityType = "sae",
        )
        val configs = listOf(
            WifiConnectorPolicy.ConfiguredNetworkRef(networkId = 8, ssid = "\"Lab\"", bssid = "02:00:00:00:00:11"),
        )

        assertEquals(
            emptyList<Int>(),
            WifiConnectorPolicy.bssidPinCleanupNetworkIds(
                ssid = "Lab",
                bssid = "02:00:00:00:00:11",
                current = current,
                configs = configs,
            ),
        )
    }

    @Test
    fun treatsDisconnectedFrameworkStatesAsSettled() {
        assertTrue(WifiConnectorPolicy.disconnectSettled(-1, "\"Lab\"", "02:00:00:00:00:21", "COMPLETED"))
        assertTrue(WifiConnectorPolicy.disconnectSettled(7, "<unknown ssid>", "", "SCANNING"))
        assertTrue(WifiConnectorPolicy.disconnectSettled(7, "\"Lab\"", "00:00:00:00:00:00", "DISCONNECTED"))
        assertFalse(WifiConnectorPolicy.disconnectSettled(7, "\"Lab\"", "02:00:00:00:00:21", "COMPLETED"))
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
