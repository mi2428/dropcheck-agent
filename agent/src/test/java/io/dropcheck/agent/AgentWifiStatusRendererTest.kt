package io.dropcheck.agent

import io.dropcheck.agent.grpc.IpStatus
import io.dropcheck.agent.grpc.MloLinkInfo
import io.dropcheck.agent.grpc.WifiConnection
import io.dropcheck.agent.grpc.WifiInformationElement
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
                .addInformationElements(WifiInformationElement.newBuilder()
                    .setId(54)
                    .setByteCount(3)
                    .setBytesHex("010203")
                    .build())
                .addInformationElements(WifiInformationElement.newBuilder()
                    .setId(70)
                    .setByteCount(5)
                    .setBytesHex("0100000000")
                    .build())
                .addInformationElements(WifiInformationElement.newBuilder()
                    .setId(127)
                    .setByteCount(3)
                    .setBytesHex("000008")
                    .build())
                .addInformationElements(WifiInformationElement.newBuilder()
                    .setId(255)
                    .setIdExt(108)
                    .setByteCount(2)
                    .setBytesHex("beef")
                    .build())
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
                .addCapabilities("internet")
                .addCapabilities("validated")
                .addCapabilities("not_metered")
                .setDownstreamKbps(1200000)
                .setUpstreamKbps(600000)
                .setSignalStrength(-48)
                .setRawCapabilities("raw_caps")
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
            "Connection Capabilities",
            "CAPABILITY",
            "11r",
            "11k",
            "11v_bss_transition",
            "eht_capabilities",
            "id=255 ext=108 bytes=2",
            "MLO",
            "present     true",
            "ap_mld      02:00:00:00:00:01",
            "Associated MLO Links",
            "active",
            "1200",
            "Network",
            "Network Capabilities",
            "not_metered",
            "wlan0",
        ).forEach { want ->
            assertTrue("rendered output missing $want:\n$out", out.contains(want))
        }
        listOf(
            "Connection Information Elements",
            "raw_caps",
            "internet,validated,not_metered",
            "security=wpa3",
            "signal_strength",
            "802.11k",
            "802.11r",
            "802.11v_bss_transition",
        ).forEach { unwanted ->
            assertTrue("rendered output included $unwanted:\n$out", !out.contains(unwanted))
        }
        assertTrue(
            "capability rows are not sorted:\n$out",
            out.indexOf("11k") < out.indexOf("11r") &&
                out.indexOf("11r") < out.indexOf("11v_bss_transition"),
        )
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
