package io.dropcheck.agent

import io.dropcheck.agent.grpc.ConnectWifi
import io.dropcheck.agent.grpc.StandaloneWifiGroup
import io.dropcheck.agent.grpc.WifiBand
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Test

class StandaloneWifiUsePolicyTest {
    @Test
    fun treatsUseTargetAsSsidWithoutDefaultPassphrase() {
        val group = StandaloneWifiUsePolicy.selectUseWifi(
            name = "2026shownet",
            defaults = StandaloneUseDefaults(),
        )

        assertNotNull(group)
        assertEquals("2026shownet", group.name)
        assertEquals("2026shownet", group.essid)
        assertEquals("", group.passphrase)
        assertEquals(ConnectWifi.Security.SECURITY_UNSPECIFIED, group.security)
        assertEquals("", group.bssid)
        assertEquals(WifiBand.WIFI_BAND_UNSPECIFIED, group.band)
    }

    @Test
    fun usesDefaultPassphraseForDirectSsidConnections() {
        val group = StandaloneWifiUsePolicy.selectUseWifi(
            name = "cs1",
            defaults = StandaloneUseDefaults(defaultPassphrase = "hogehoge"),
        )

        assertNotNull(group)
        assertEquals("cs1", group?.name)
        assertEquals("cs1", group?.essid)
        assertEquals("hogehoge", group?.passphrase)
        assertEquals(ConnectWifi.Security.SECURITY_UNSPECIFIED, group?.security)
        assertEquals("", group?.bssid)
        assertEquals(WifiBand.WIFI_BAND_UNSPECIFIED, group?.band)
    }

    @Test
    fun mapsWifiGroupToConnectCommand() {
        val group = StandaloneWifiGroup.newBuilder()
            .setName("ap1")
            .setPassphrase("secret")
            .setSecurity(ConnectWifi.Security.SECURITY_WPA3_SAE)
            .setBssid("aa:bb:cc:dd:ee:ff")
            .setBand(WifiBand.WIFI_BAND_5_GHZ)
            .setMacRandomization(ConnectWifi.MacRandomization.MAC_RANDOMIZATION_PERSISTENT)
            .setTimeoutMs(12_000)
            .build()

        val command = StandaloneWifiUsePolicy.connectCommand(group, "cs1")
        val connect = command.connectWifi

        assertEquals("use ap1", command.label)
        assertNotNull(connect)
        assertEquals("cs1", connect.ssid)
        assertEquals("secret", connect.passphrase)
        assertEquals(ConnectWifi.Security.SECURITY_WPA3_SAE, connect.security)
        assertEquals("aa:bb:cc:dd:ee:ff", connect.bssid)
        assertEquals(WifiBand.WIFI_BAND_5_GHZ, connect.band)
        assertEquals(ConnectWifi.MacRandomization.MAC_RANDOMIZATION_PERSISTENT, connect.macRandomization)
        assertEquals(12_000, connect.timeoutMs)
    }

    @Test
    fun preservesOriginalStandaloneStateAcrossRepeatedUseCommands() {
        val first = StandaloneUseStatePolicy.beginUse(
            wifiName = "ap1",
            currentStandaloneEnabled = true,
            active = null,
        )
        val second = StandaloneUseStatePolicy.beginUse(
            wifiName = "ap2",
            currentStandaloneEnabled = false,
            active = first,
        )

        assertEquals("ap2", second.wifiName)
        assertEquals(true, second.previousStandaloneEnabled)
    }
}
