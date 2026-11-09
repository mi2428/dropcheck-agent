package io.dropcheck.agent

import io.dropcheck.agent.grpc.IpStatus
import io.dropcheck.agent.grpc.MloLinkInfo
import io.dropcheck.agent.grpc.WifiConnection
import io.dropcheck.agent.grpc.DiagnosticField
import io.dropcheck.agent.grpc.WifiInformationElement
import io.dropcheck.agent.grpc.WifiStatus
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class AgentWifiStatusRendererTest {
    @Test
    fun rendersStatusAndIpDetailsWithoutDuplicateIdentity() {
        val status = WifiStatus.newBuilder()
            .setEnabled(true)
            .addObservationFields(DiagnosticField.newBuilder().setKey("radio.state").setValue("available"))
            .addObservationFields(DiagnosticField.newBuilder().setKey("connection.state").setValue("available"))
            .setState("enabled")
            .setActiveNetwork("102")
            .setWifiNetworkCount(1)
            .addPermissions("fine_location=granted")
            .setConnection(WifiConnection.newBuilder()
                .addObservationFields(DiagnosticField.newBuilder().setKey("identity.state").setValue("available"))
                .addObservationFields(DiagnosticField.newBuilder().setKey("rssi.state").setValue("available"))
                .addObservationFields(DiagnosticField.newBuilder().setKey("tx_link_speed_mbps.state").setValue("available"))
                .addObservationFields(DiagnosticField.newBuilder().setKey("rx_link_speed_mbps.state").setValue("available"))
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
                .addObservationFields(DiagnosticField.newBuilder().setKey("capabilities.state").setValue("available"))
                .addObservationFields(DiagnosticField.newBuilder().setKey("link_properties.state").setValue("available"))
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
                .addIpv6Ra(DiagnosticField.newBuilder().setKey("default_route").setValue("missing").build())
                .addIpv6Ra(DiagnosticField.newBuilder().setKey("default_gateways").setValue("fe80::1\nfe80::2").build())
                .addIpv6Ra(DiagnosticField.newBuilder().setKey("accept_ra").setValue("2").build())
                .addIpv6Ra(DiagnosticField.newBuilder().setKey("accept_ra_defrtr").setValue("1").build())
                .addIpv6Ra(DiagnosticField.newBuilder().setKey("accept_ra_min_lft").setValue("180").build())
                .setDomains("local")
                .setPrivateDnsActive(true)
                .build())
            .build()

        val out = AgentPresentationBlock.create(AgentWifiStatusRenderer.presentation(status, detail = true), 0).fullText

        listOf(
            "Radio: on",
            "Wi-Fi state: enabled",
            "SSID: Lab",
            "BSSID: aa:bb:cc:dd:ee:ff",
            "CH/BW: 5/160MHz",
            "Tx link rate: 900Mbps",
            "Rx link rate: 1200Mbps",
            "sta_mac",
            "02:00:00:00:00:09",
            "AP Capabilities",
            "roaming",
            "11r",
            "11k",
            "11v_bss_transition",
            "roaming: 11k\n    11r\n    11v_bss_transition",
            "phy: eht",
            "Network: 102",
            "Capabilities: internet\nvalidated\nnot_roaming\nnot_metered",
            "Bandwidth: down=1200000kbps up=600000kbps",
            "IPv4\nAddr: 192.0.2.10/24",
            "IPv6\nAddr: 2001:db8::10/64\n2001:db8::11/64",
            "DNS: 192.0.2.1\n1.1.1.1",
            "DHCP server: 192.0.2.254",
            "Private DNS: on; server=none",
            "Routes: 0.0.0.0/0 -> 192.0.2.1 wlan0\n192.0.2.0/24 -> 0.0.0.0 wlan0",
            "Domains: local",
            "Interface: wlan0",
            "IPv6 RA",
            "default_route",
            "missing",
            "default_gateways",
            "fe80::1",
            "fe80::2",
            "accept_ra",
            "accept_ra_defrtr",
            "accept_ra_min_lft",
            "180",
        ).forEach { want ->
            assertTrue("rendered output missing $want:\n$out", out.replace(Regex("\n +"), "\n").contains(want.replace(Regex("\n +"), "\n")))
        }
        val network = out.indexOf("\n  Network:")
        val ipv6Ra = out.indexOf("\n  IPv6 RA")
        assertTrue("Network section missing:\n$out", network >= 0)
        assertTrue("IPv6 RA section missing:\n$out", ipv6Ra >= 0)
        assertTrue("sections are not ordered Network -> IPv6 RA:\n$out", network < ipv6Ra)
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
            "internet,validated,not_metered",
            "all_granted fine_location",
            "not_metered,not_roaming",
            "192.0.2.1,1.1.1.1",
            "0.0.0.0/0 -> 192.0.2.1 wlan0 | 192.0.2.0/24 -> 0.0.0.0 wlan0",
            "ipv6_ra",
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
        assertTrue(Regex("SSID: Lab").findAll(out).count() == 1)
        assertTrue(Regex("BSSID: aa:bb:cc:dd:ee:ff").findAll(out).count() == 1)
    }

    @Test
    fun preservesConnectionReferenceWhenIpStatusIsMissing() {
        val status = WifiStatus.newBuilder()
            .setConnection(WifiConnection.newBuilder()
                .setSsid("Lab")
                .setNetworkId(119)
                .setIpv4Address("192.0.2.10")
                .build())
            .build()

        val out = AgentPresentationBlock.create(AgentWifiStatusRenderer.presentation(status, detail = true), 0).fullText

        listOf(
            "configuration_id",
            "119",
            "Related IP snapshot: 192.0.2.10",
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

        val out = AgentPresentationBlock.create(AgentWifiStatusRenderer.presentation(status, detail = true), 0).fullText

        assertFalse("rendered output included inactive connection:\n$out", out.contains("Connection"))
        assertFalse("rendered output included stale standard:\n$out", out.contains("802.11ax"))
    }

}
