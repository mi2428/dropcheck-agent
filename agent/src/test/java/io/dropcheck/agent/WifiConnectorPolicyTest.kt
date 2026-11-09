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
    fun connectPreflightRejectsInvalidAndUnsupportedSettings() {
        val valid = ConnectWifi.newBuilder().setSsid("Lab").setPassphrase("synthetic-passphrase").build()
        assertNull(WifiConnectorPolicy.connectError(valid, 31))
        assertNull(WifiConnectorPolicy.connectError(valid, 33))
        for (command in listOf(
            valid.toBuilder().setSsid("").build(),
            valid.toBuilder().setPassphrase("").build(),
            valid.toBuilder().setPassphrase("short").build(),
            valid.toBuilder().setPassphrase("z".repeat(64)).build(),
            valid.toBuilder().setBssid("bad").build(),
            valid.toBuilder().setSecurityValue(999).build(),
            valid.toBuilder().setBandValue(999).build(),
            valid.toBuilder().setMacRandomizationValue(999).build(),
            valid.toBuilder().setTimeoutMs(-1).build(),
        )) assertTrue(WifiConnectorPolicy.connectError(command, 37) != null)
        for (mode in ConnectWifi.MacRandomization.values().filter { it != ConnectWifi.MacRandomization.MAC_RANDOMIZATION_UNSPECIFIED && it != ConnectWifi.MacRandomization.UNRECOGNIZED }) {
            val command = valid.toBuilder().setMacRandomization(mode).build()
            assertTrue(WifiConnectorPolicy.connectError(command, 31) != null)
            assertNull(WifiConnectorPolicy.connectError(command, 33))
        }
    }

    @Test
    fun requestedMacPolicyCannotReuseAnUnverifiedSavedConfiguration() {
        val current = WifiConnectorPolicy.CurrentConnectionRef(7, "Lab", "aa:bb:cc:dd:ee:ff", 2412, "psk")
        for (mode in ConnectWifi.MacRandomization.values()) {
            assertEquals(mode == ConnectWifi.MacRandomization.MAC_RANDOMIZATION_UNSPECIFIED,
                WifiConnectorPolicy.currentConnectionSatisfiesConnect(current, "Lab", "", ConnectWifi.Security.SECURITY_WPA2_PSK, WifiBand.WIFI_BAND_ALL, mode))
        }
    }
    @Test
    fun alwaysQuotesLiteralWifiConfigurationStrings() {
        assertEquals("\"Lab\"", WifiConnectorPolicy.quoteWifi("Lab"))
        assertEquals("\"\"Lab\"\"", WifiConnectorPolicy.quoteWifi("\"Lab\""))
        assertEquals("\" Lab \"", WifiConnectorPolicy.quoteWifi(" Lab "))
    }

    @Test fun validatedCredentialsKeepLiteralSpacesAndRawHexEncoding() {
        val credentials = listOf(
            "synthetic-passphrase" to "\"synthetic-passphrase\"",
            " ".repeat(8) to "\"${" ".repeat(8)}\"",
            "a".repeat(63) to "\"${"a".repeat(63)}\"",
            "0123456789abcdef".repeat(4) to "0123456789abcdef".repeat(4),
            "0123456789ABCDEF".repeat(4) to "0123456789ABCDEF".repeat(4),
            "0123456789aBcDeF".repeat(4) to "0123456789aBcDeF".repeat(4),
        )
        for ((passphrase, configured) in credentials) {
            val command = ConnectWifi.newBuilder().setSsid("Lab").setPassphrase(passphrase).build()
            for (sdk in listOf(31, 33)) assertNull(WifiConnectorPolicy.connectError(command, sdk))
            assertEquals(configured, WifiConnectorPolicy.preSharedKey(passphrase))
        }
    }

    @Test fun invalidConnectNamesAreRejectedButLiteralSpacesAndQuotesRemainValid() {
        for (ssid in listOf("Lab", " Lab ", " ", "\"Lab\"", "La\"b")) assertTrue(WifiConnectorPolicy.validConnectSsid(ssid))
        for (ssid in listOf("", "\n", "Lab\u0000", "Lab\u001b", "x".repeat(33), "あ".repeat(11), "<unknown ssid>")) {
            assertFalse(WifiConnectorPolicy.validConnectSsid(ssid))
        }
    }

    @Test fun typedForgetKeepsNumericSsidAndConfigurationIdSeparate() {
        val configs = listOf(WifiConnectorPolicy.ConfiguredNetworkRef(123, "\"other\""), WifiConnectorPolicy.ConfiguredNetworkRef(7, "\"123\""))
        val ssid = io.dropcheck.agent.grpc.ForgetWifi.newBuilder().setSsid("123").build()
        val id = io.dropcheck.agent.grpc.ForgetWifi.newBuilder().setConfigId(123).build()
        assertEquals(listOf(7), WifiConnectorPolicy.forgetNetworkIds(ssid, configs))
        assertEquals(listOf(123), WifiConnectorPolicy.forgetNetworkIds(id, configs))
        assertEquals(listOf(123, 7), WifiConnectorPolicy.forgetNetworkIds("123", configs))
        val current = WifiConnectorPolicy.CurrentConnectionRef(123, "other", "02:00:00:00:00:01", 2412, "psk")
        assertFalse(WifiConnectorPolicy.currentConnectionMatchesForgetTarget(ssid, current))
        assertTrue(WifiConnectorPolicy.currentConnectionMatchesForgetTarget(id, current))
        assertTrue(WifiConnectorPolicy.validForget(io.dropcheck.agent.grpc.ForgetWifi.newBuilder().setConfigId(0).build()))
        for (invalid in listOf(ssid.toBuilder().setTarget("123").build(), io.dropcheck.agent.grpc.ForgetWifi.newBuilder().setConfigId(-1).build(),
            io.dropcheck.agent.grpc.ForgetWifi.newBuilder().setSsid("").build(), io.dropcheck.agent.grpc.ForgetWifi.newBuilder().setSsid("bad\nname").build())) {
            assertFalse(WifiConnectorPolicy.validForget(invalid))
        }
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
                security = ConnectWifi.Security.SECURITY_UNSPECIFIED,
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

    @Test fun literalSsidConfigSelectionDoesNotDeleteOtherSavedNetworks() {
        val configs = listOf(
            WifiConnectorPolicy.ConfiguredNetworkRef(7, "\"Lab\""),
            WifiConnectorPolicy.ConfiguredNetworkRef(8, "\" Lab \""),
            WifiConnectorPolicy.ConfiguredNetworkRef(9, "\"\"Lab\"\""),
            WifiConnectorPolicy.ConfiguredNetworkRef(10, "\" \""),
        )
        for ((ssid, id) in listOf("Lab" to 7, " Lab " to 8, "\"Lab\"" to 9, " " to 10)) {
            assertEquals(listOf(id), WifiConnectorPolicy.forgetNetworkIds(ssid, configs))
            assertEquals(listOf(id), WifiConnectorPolicy.bssidPinCleanupNetworkIds(ssid, "aa:bb:cc:dd:ee:ff", configs))
        }
        assertFalse(WifiConnectorPolicy.disconnectSettled(7, "\" \"", "aa:bb:cc:dd:ee:ff", "COMPLETED"))
        val current = WifiConnectorPolicy.CurrentConnectionRef(10, " ", "aa:bb:cc:dd:ee:ff", 2412, "sae")
        assertTrue(WifiConnectorPolicy.currentConnectionMatchesForgetTarget(" ", current))
        assertTrue(WifiConnectorPolicy.currentConnectionSatisfiesConnect(current, " ", "", ConnectWifi.Security.SECURITY_UNSPECIFIED, WifiBand.WIFI_BAND_ALL))
        assertFalse(WifiConnectorPolicy.currentConnectionSatisfiesConnect(current, "Lab", "", ConnectWifi.Security.SECURITY_UNSPECIFIED, WifiBand.WIFI_BAND_ALL))
        val quoted = current.copy(ssid = "\"Lab\"")
        assertTrue(WifiConnectorPolicy.currentConnectionSatisfiesConnect(quoted, "\"Lab\"", "", ConnectWifi.Security.SECURITY_UNSPECIFIED, WifiBand.WIFI_BAND_ALL))
        assertFalse(WifiConnectorPolicy.currentConnectionSatisfiesConnect(quoted, "Lab", "", ConnectWifi.Security.SECURITY_UNSPECIFIED, WifiBand.WIFI_BAND_ALL))
    }

    @Test fun redactedIdentityNeverMatchesForgetOrConnectedShortcut() {
        val unknown = WifiConnectorPolicy.CurrentConnectionRef(7, "<unknown ssid>", "02:00:00:00:00:00", 2412, "sae")
        assertFalse(WifiConnectorPolicy.currentConnectionSatisfiesConnect(unknown, "<unknown ssid>", "", ConnectWifi.Security.SECURITY_UNSPECIFIED, WifiBand.WIFI_BAND_ALL))
        assertFalse(WifiConnectorPolicy.currentConnectionMatchesForgetTarget("<unknown ssid>", unknown))
        assertFalse(WifiConnectorPolicy.currentConnectionMatchesForgetTarget("02:00:00:00:00:00", unknown))
        assertTrue(WifiConnectorPolicy.currentConnectionMatchesForgetTarget("7", unknown))
        val configs = listOf(WifiConnectorPolicy.ConfiguredNetworkRef(7, "<unknown ssid>"))
        assertTrue(WifiConnectorPolicy.forgetNetworkIds("<unknown ssid>", configs).isEmpty())
        assertTrue(WifiConnectorPolicy.bssidPinCleanupNetworkIds("<unknown ssid>", "aa:bb:cc:dd:ee:ff", configs).isEmpty())
    }

    @Test
    fun selectsConflictingProfilesBeforeBssidPinnedConnect() {
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
                configs = configs,
            ),
        )
    }

    @Test
    fun keepsProfilesWithMatchingBssid() {
        val configs = listOf(
            WifiConnectorPolicy.ConfiguredNetworkRef(networkId = 8, ssid = "\"Lab\"", bssid = "02:00:00:00:00:11"),
        )

        assertEquals(
            emptyList<Int>(),
            WifiConnectorPolicy.bssidPinCleanupNetworkIds(
                ssid = "Lab",
                bssid = "02:00:00:00:00:11",
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
        assertFalse(WifiConnectorPolicy.forgetSucceeded(listOf("remove_7" to "true", "remove_8" to "false"), emptyList()))
        assertTrue(WifiConnectorPolicy.forgetSucceeded(listOf("remove_7" to "true", "remove_8" to "true"), emptyList()))
    }
}
