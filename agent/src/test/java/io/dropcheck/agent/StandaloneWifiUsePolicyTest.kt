package io.dropcheck.agent

import io.dropcheck.agent.grpc.ConnectWifi
import io.dropcheck.agent.grpc.StandaloneConfig
import io.dropcheck.agent.grpc.StandaloneFesta
import io.dropcheck.agent.grpc.StandaloneWifiGroup
import io.dropcheck.agent.grpc.WifiBand
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Test

class StandaloneWifiUsePolicyTest {
    @Test
    fun selectsOnlyWifiGroupsFromLiveFesta() {
        val config = StandaloneConfig.newBuilder()
            .addFestas(StandaloneFesta.newBuilder()
                .setName("smoke")
                .addWifiGroups(StandaloneWifiGroup.newBuilder().setName("ap1").setEssid("wrong")))
            .addFestas(StandaloneFesta.newBuilder()
                .setName("live")
                .addWifiGroups(StandaloneWifiGroup.newBuilder().setName("ap1").setEssid("cs1"))
                .addWifiGroups(StandaloneWifiGroup.newBuilder().setName("ap2").setEssid("cs2")))
            .build()

        assertEquals(listOf("ap1", "ap2"), StandaloneWifiUsePolicy.liveWifiNames(config))
        assertEquals("cs1", StandaloneWifiUsePolicy.selectLiveWifi(config, "ap1")?.essid)
        assertNull(StandaloneWifiUsePolicy.selectLiveWifi(config, "missing"))
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
