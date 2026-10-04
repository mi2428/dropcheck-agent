package io.dropcheck.agent

import io.dropcheck.agent.grpc.DiagnosticField
import io.dropcheck.agent.grpc.MloLinkInfo
import io.dropcheck.agent.grpc.WifiInformationElement
import io.dropcheck.agent.grpc.WifiScan
import io.dropcheck.agent.grpc.WifiScanResult
import io.dropcheck.agent.grpc.WifiSecurityDetails
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class AgentWifiScanRendererTest {
    @Test
    fun rendersBriefMloTableScan() {
        val scan = WifiScan.newBuilder()
            .addFields(field("requested_band", "6GHz"))
            .addFields(field("scan_result_count", "3"))
            .addFields(field("scan_result_total_count", "3"))
            .addFields(field("wifi_enabled", "true"))
            .addFields(field("wifi_state", "enabled"))
            .addResults(
                WifiScanResult.newBuilder()
                    .setSsid("Lab")
                    .setBssid("aa:bb:cc:dd:ee:ff")
                    .setRssiDbm(-45)
                    .setBand("6GHz")
                    .setFrequencyMhz(6295)
                    .setWifiStandard("802.11be")
                    .addSecurityTypes("sae")
                    .setApMldMacAddress("02:00:00:00:00:01")
                    .setApMloLinkId(2)
                    .setSecurityDetails(
                        WifiSecurityDetails.newBuilder()
                            .setGcmp256(true)
                            .setSaeGdh(true)
                            .setFtSaeGdh(true)
                            .setBeaconProtection(true)
                            .addRsnxeCapabilities("sae_h2e")
                            .addRsnxeCapabilities("ssid_protection")
                            .build(),
                    )
                    .addInformationElements(ie(70, bytesHex = ""))
                    .addInformationElements(ie(127, bytesHex = "000008"))
                    .addInformationElements(ie(201, bytesHex = "0001"))
                    .addInformationElements(ie(255, idExt = 107, bytesHex = "beef"))
                    .addAffiliatedMloLinks(
                        MloLinkInfo.newBuilder()
                            .setLinkId(1)
                            .setState("unassociated")
                            .setBand("5GHz")
                            .setApMacAddress("aa:bb:cc:dd:ee:01")
                            .build(),
                    )
                    .build(),
            )
            .addResults(
                WifiScanResult.newBuilder()
                    .setSsid("GhostBE")
                    .setBssid("11:22:33:44:55:66")
                    .setRssiDbm(-60)
                    .setBand("6GHz")
                    .setFrequencyMhz(6055)
                    .setWifiStandard("802.11be")
                    .addSecurityTypes("sae")
                    .build(),
            )
            .addResults(
                WifiScanResult.newBuilder()
                    .setSsid("LegacyMLO")
                    .setBssid("22:33:44:55:66:77")
                    .setRssiDbm(-65)
                    .setBand("5GHz")
                    .setFrequencyMhz(5180)
                    .setWifiStandard("802.11ax")
                    .setApMldMacAddress("03:00:00:00:00:01")
                    .setApMloLinkId(0)
                    .addAffiliatedMloLinks(
                        MloLinkInfo.newBuilder()
                            .setLinkId(1)
                            .setState("idle")
                            .setBand("6GHz")
                            .setApMacAddress("22:33:44:55:66:78")
                            .build(),
                    )
                    .build(),
            )
            .build()

        val out = AgentWifiScanRenderer.render(
            scan,
            AgentWifiScanContext(brief = true, mloOnly = true),
        ).joinToString("\n")

        for (want in listOf(
            "Wi-Fi Scan",
            "Lab",
            "MLD: 02:00:00:00:00:01",
            "dBm: -45",
            "BAND: 6G",
            "CH: 69",
            "SEC: sae",
            "Affiliated: 1 unassociated",
            "aa:bb:cc:dd:ee:ff",
            "aa:bb:cc:dd:ee:01",
        )) {
            assertTrue("rendered output missing $want:\n$out", out.contains(want))
        }
        assertTrue(out.contains("Band: 6GHz"))
        assertTrue(out.contains("APs: acquired=2 total=3"))
        assertTrue(out.contains("LegacyMLO")) // Explicit metadata is not discarded based on PHY alone.

        for (unwanted in listOf("SSID  BSSID", "STANDARD", "GhostBE", "flags  ", "sec    ")) {
            assertFalse("rendered output included $unwanted:\n$out", out.contains(unwanted))
        }
    }

    @Test
    fun rendersBriefScanTablesForMobileWidth() {
        val scan = WifiScan.newBuilder()
            .addFields(field("requested_band", "all"))
            .addFields(field("scan_result_count", "2"))
            .addFields(field("scan_result_total_count", "2"))
            .addResults(
                WifiScanResult.newBuilder()
                    .setSsid("Lab")
                    .setBssid("aa:bb:cc:dd:ee:ff")
                    .setRssiDbm(-45)
                    .setBand("6GHz")
                    .setFrequencyMhz(6295)
                    .setWifiStandard("802.11be")
                    .addSecurityTypes("sae")
                    .setApMldMacAddress("02:00:00:00:00:01")
                    .setApMloLinkId(2)
                    .addAffiliatedMloLinks(
                        MloLinkInfo.newBuilder()
                            .setLinkId(1)
                            .setBand("5GHz")
                            .setApMacAddress("aa:bb:cc:dd:ee:01")
                            .build(),
                    )
                    .setSecurityDetails(
                        WifiSecurityDetails.newBuilder()
                            .setGcmp256(true)
                            .setBeaconProtection(true)
                            .build(),
                    )
                    .addInformationElements(ie(70, bytesHex = ""))
                    .addInformationElements(ie(255, idExt = 107, bytesHex = "beef"))
                    .build(),
            )
            .addResults(
                WifiScanResult.newBuilder()
                    .setSsid("Guest")
                    .setBssid("11:22:33:44:55:66")
                    .setRssiDbm(-62)
                    .setBand("2.4GHz")
                    .setFrequencyMhz(2462)
                    .setWifiStandard("802.11ax")
                    .addSecurityTypes("psk")
                    .build(),
            )
            .build()

        val out = AgentWifiScanRenderer.render(
            scan,
            AgentWifiScanContext(brief = true),
        ).joinToString("\n")

        for (want in listOf(
            "Wi-Fi Scan",
            "Lab",
            "Guest",
            "dBm: -45",
            "BAND: 6G",
            "CH: 69",
            "PHY: be",
            "SEC: sae",
            "BSSID: aa:bb:cc:dd:ee:ff",
            "BSSID: 11:22:33:44:55:66",
        )) {
            assertTrue("rendered output missing $want:\n$out", out.contains(want))
        }
        for (unwanted in listOf("bssid  ", "mlo    ", "flags  ", "sec    ", "STANDARD", "SEC_FEATURES", "AP_LINK", "AFFILIATED")) {
            assertFalse("rendered output included $unwanted:\n$out", out.contains(unwanted))
        }
    }

    private fun field(key: String, value: String): DiagnosticField =
        DiagnosticField.newBuilder().setKey(key).setValue(value).build()

    private fun ie(id: Int, idExt: Int = 0, bytesHex: String): WifiInformationElement =
        WifiInformationElement.newBuilder()
            .setId(id)
            .setIdExt(idExt)
            .setByteCount(bytesHex.length / 2)
            .setBytesHex(bytesHex)
            .build()
}
