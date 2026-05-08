package io.dropcheck.agent

import io.dropcheck.agent.grpc.IpStatus
import io.dropcheck.agent.grpc.MloLinkInfo
import io.dropcheck.agent.grpc.WifiConnection
import io.dropcheck.agent.grpc.WifiInformationElement
import io.dropcheck.agent.grpc.WifiStatus
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class AgentWifiStatusRendererTest {
    @Test
    fun rendersWifiStatusWithoutMloDetails() {
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
                .setNetworkId(102)
                .setFrequencyMhz(5975)
                .setLinkSpeedMbps(1200)
                .setTxLinkSpeedMbps(900)
                .setRxLinkSpeedMbps(1200)
                .setSecurityType("wpa3")
                .setWifiStandard("11be")
                .setMacAddress("02:00:00:00:00:09")
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
                .addCapabilities("not_roaming")
                .addCapabilities("not_metered")
                .setDownstreamKbps(1200000)
                .setUpstreamKbps(600000)
                .setSignalStrength(-48)
                .setRawCapabilities("raw_caps")
                .setInterfaceName("wlan0")
                .setMtu(1500)
                .addAddresses("192.0.2.10/24")
                .addAddresses("2001:db8::10/64")
                .addAddresses("2001:db8::11/64")
                .addDnsServers("192.0.2.1")
                .addDnsServers("1.1.1.1")
                .setDhcpServer("192.0.2.254")
                .addRoutes("0.0.0.0/0 -> 192.0.2.1 wlan0")
                .addRoutes("192.0.2.0/24 -> 0.0.0.0 wlan0")
                .setDomains("local")
                .setPrivateDnsActive(true)
                .build())
            .build()

        val out = AgentWifiStatusRenderer.render(status).joinToString("\n")

        listOf(
            "Wi-Fi",
            "permissions\n    all_granted\n    fine_location",
            "Connection",
            "bandwidth   160MHz",
            "link        1200Mbps tx=900Mbps rx=1200Mbps",
            "sta_mac     02:00:00:00:00:09",
            "AP Capabilities",
            "roaming",
            "11r",
            "11k",
            "11v_bss_transition",
            "roaming\n    11k\n    11r\n    11v_bss_transition",
            "phy      eht",
            "Network",
            "not_metered",
            "capabilities\n    not_metered\n    not_roaming",
            "bandwidth     down=1200000kbps up=600000kbps",
            "ipv4          192.0.2.10/24",
            "ipv6\n    2001:db8::10/64\n    2001:db8::11/64",
            "dns\n    192.0.2.1\n    1.1.1.1",
            "dhcp_server   192.0.2.254",
            "private_dns   active=true server=none",
            "routes\n    0.0.0.0/0 -> 192.0.2.1 wlan0\n    192.0.2.0/24 -> 0.0.0.0 wlan0",
            "domains",
            "local",
            "wlan0",
        ).forEach { want ->
            assertTrue("rendered output missing $want:\n$out", out.contains(want))
        }
        assertTrue(
            "network rows are not ordered routes -> dns -> domains:\n$out",
            out.indexOf("\n  routes") < out.indexOf("\n  dns") &&
                out.indexOf("\n  dns") < out.indexOf("\n  domains"),
        )
        listOf(
            "Connection Capabilities",
            "Connection State",
            "Connection Information Elements",
            "CAPABILITY",
            "id=255 ext=108 bytes=2",
            "Network Capabilities",
            "\nAddresses\n",
            "\nDNS\n",
            "\nDHCP\n",
            "\nPrivate DNS\n",
            "raw_caps",
            "internet,validated,not_metered",
            "all_granted fine_location",
            "not_metered,not_roaming",
            "192.0.2.1,1.1.1.1",
            "0.0.0.0/0 -> 192.0.2.1 wlan0 | 192.0.2.0/24 -> 0.0.0.0 wlan0",
            "11k,11r,11v_bss_transition",
            "security=wpa3",
            "signal_strength",
            "802.11k",
            "802.11r",
            "802.11v_bss_transition",
            "\nMLO\n",
            "\nMLO Links\n",
            "source      android",
            "present     true",
            "ap_mld      02:00:00:00:00:01",
            "ap_link_id",
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
    fun preservesConnectionNetworkFallbackWhenIpStatusIsMissing() {
        val status = WifiStatus.newBuilder()
            .setConnection(WifiConnection.newBuilder()
                .setSsid("Lab")
                .setNetworkId(119)
                .setIpv4Address("192.0.2.10")
                .build())
            .build()

        val out = AgentWifiStatusRenderer.render(status).joinToString("\n")

        listOf(
            "Network",
            "id    119",
            "ipv4  192.0.2.10",
        ).forEach { want ->
            assertTrue("rendered output missing $want:\n$out", out.contains(want))
        }
        assertTrue("rendered output included stale connection ip row:\n$out", !out.contains("\n  ip "))
    }

    @Test
    fun hidesAndroidPlaceholderConnectionWhenWifiIsNotAssociated() {
        val status = WifiStatus.newBuilder()
            .setEnabled(true)
            .setState("enabled")
            .setConnection(WifiConnection.newBuilder()
                .setSsid("<unknown ssid>")
                .setBssid("02:00:00:00:00:00")
                .setNetworkId(-1)
                .setWifiStandard("802.11ax")
                .build())
            .build()

        val out = AgentWifiStatusRenderer.render(status).joinToString("\n")

        assertFalse("rendered output included inactive connection:\n$out", out.contains("Connection"))
        assertFalse("rendered output included stale standard:\n$out", out.contains("802.11ax"))
    }

}
