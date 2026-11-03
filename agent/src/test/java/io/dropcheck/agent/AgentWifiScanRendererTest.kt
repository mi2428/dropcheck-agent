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
    @Test fun briefKeepsAgesInScanRowsWithoutSupplementalApBlocks() {
        val scan = WifiScan.newBuilder().addErrors("reference cache; refresh failed")
        repeat(26) { index ->
            val ap = WifiScanResult.newBuilder().setSsid("password=Guest").setBssid("02:00:00:11:22:%02x".format(index)).setRssiDbm(-40 - index)
            if (index == 0) ap.setObservationAgeMs(0)
            if (index == 1) ap.setObservationAgeMs(51845845)
            if (index == 2) ap.setObservationAgeMs(-1L)
            scan.addResults(ap)
        }
        val parts = AgentWifiScanRenderer.presentation(scan.build(), AgentWifiScanContext(brief = true))
        val rows = parts.filterIsInstance<AgentBlockPart.Scan>().single().rows
        assertTrue(rows.all { it.size == 9 })
        for ((index, age) in listOf("0", "51845.845", "18446744073709551.615", "?").withIndex()) {
            assertTrue("age lost its AP row", rows.single { it[1].endsWith("%02x".format(index)) }.last() == age)
        }
        assertFalse(parts.filterIsInstance<AgentBlockPart.Text>().any { Regex("AP [0-9]+").matches(it.value) })
        val block = AgentPresentationBlock.create(parts, 0)
        val out = block.fullText
        assertTrue("AGE(s): 51845.845" in out)
        assertTrue("password=Guest" in out)
        assertTrue("refresh failed" in out)
        assertTrue("timestamp unavailable" in out)
        assertTrue("Full: show wifi scan" in out)
        assertFalse("Observation age at collection:" in out)
        assertTrue(block.safeValues().any { it.first == "AP 2 AGE(s)" && it.second == "51845.845" })
        val json = AgentShellPipeline.parse(listOf("display json")).apply(AgentShellResult(false, block = block)).block!!.fullText
        assertTrue("\"ok\":false" in json && "AGE(s): 51845.845" in json)
        val full = AgentWifiScanRenderer.render(scan.build()).joinToString("\n")
        assertTrue("Observation age at collection: 51845845ms" in full)
    }

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

    @Test fun briefChangesDepthWithoutRepeatingIdentities() {
        val scan = WifiScan.newBuilder().addResults(WifiScanResult.newBuilder().setSsid("Long safe SSID that stays intact")
            .setBssid("02:00:00:11:22:33").setFrequencyMhz(6135)).build()
        val brief = AgentWifiScanRenderer.render(scan, AgentWifiScanContext(brief = true)).joinToString("\n")
        val full = AgentWifiScanRenderer.render(scan).joinToString("\n")
        assertFalse(brief.contains("Centers MHz"))
        assertTrue(full.contains("Centers MHz"))
        assertTrue(Regex("02:00:00:11:22:33").findAll(full).count() == 1)
        assertTrue(Regex("Long safe SSID that stays intact").findAll(full).count() == 1)
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
