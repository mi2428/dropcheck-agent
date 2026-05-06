package io.dropcheck.agent

import io.dropcheck.agent.grpc.IpStatus
import io.dropcheck.agent.grpc.MloLinkInfo
import io.dropcheck.agent.grpc.WifiConnection
import io.dropcheck.agent.grpc.WifiStatus
import org.junit.Assert.assertTrue
import org.junit.Test

class AgentWifiStatusRendererTest {
    @Test
    fun rendersWifiStatusWithMloLinks() {
        val status = WifiStatus.newBuilder()
            .setEnabled(true)
            .setState("enabled")
            .setActiveNetwork("102")
            .setWifiNetworkCount(1)
            .addPermissions("fine_location=granted")
            .setConnection(WifiConnection.newBuilder()
                .setSsid("Lab")
                .setBssid("aa:bb:cc:dd:ee:ff")
                .setRssiDbm(-48)
                .setFrequencyMhz(5975)
                .setLinkSpeedMbps(1200)
                .setSecurityType("wpa3")
                .setWifiStandard("11be")
                .setSupplicantState("COMPLETED")
                .setDetailedState("CONNECTED")
                .setChannelWidth("WIDTH_160MHZ")
                .setApMldMacAddress("02:00:00:00:00:01")
                .setApMloLinkId(2)
                .addAssociatedMloLinks(MloLinkInfo.newBuilder()
                    .setLinkId(2)
                    .setState("active")
                    .setBand("6ghz")
                    .setChannel(5)
                    .setRssiDbm(-48)
                    .setTxLinkSpeedMbps(1200)
                    .setRxLinkSpeedMbps(1200)
                    .setApMacAddress("02:00:00:00:00:02")
                    .build())
                .build())
            .setIpStatus(IpStatus.newBuilder()
                .setNetworkId("102")
                .addTransports("wifi")
                .setValidated(true)
                .setInternet(true)
                .setInterfaceName("wlan0")
                .setMtu(1500)
                .addAddresses("192.0.2.10/24")
                .addDnsServers("192.0.2.1")
                .build())
            .build()

        val out = AgentWifiStatusRenderer.render(status).joinToString("\n")

        listOf(
            "Wi-Fi",
            "Connection",
            "bandwidth  160MHz",
            "MLO",
            "present     true",
            "ap_mld      02:00:00:00:00:01",
            "Associated MLO Links",
            "active",
            "1200",
            "Network",
            "wlan0",
        ).forEach { want ->
            assertTrue("rendered output missing $want:\n$out", out.contains(want))
        }
    }

    @Test
    fun rendersEmptyMloStateWhenConnectionHasNoMloFields() {
        val status = WifiStatus.newBuilder()
            .setConnection(WifiConnection.newBuilder()
                .setSsid("Lab")
                .setFrequencyMhz(2412)
                .build())
            .build()

        val out = AgentWifiStatusRenderer.render(status).joinToString("\n")

        assertTrue(out.contains("MLO"))
        assertTrue(out.contains("present     false"))
        assertTrue(out.contains("ap_mld      <none>"))
        assertTrue(out.contains("ap_link_id  <none>"))
    }
}
