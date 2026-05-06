package io.dropcheck.agent

import io.dropcheck.agent.grpc.DiagnosticField
import io.dropcheck.agent.grpc.MloLinkInfo
import io.dropcheck.agent.grpc.WifiScan
import io.dropcheck.agent.grpc.WifiScanDetail
import io.dropcheck.agent.grpc.WifiScanResult
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class AgentWifiScanRendererTest {
    @Test
    fun rendersScanWithControllerStyleMloColumns() {
        val scan = WifiScan.newBuilder()
            .addFields(DiagnosticField.newBuilder()
                .setKey("requested_band")
                .setValue("all")
                .build())
            .addResults(mloScanResult())
            .addResults(WifiScanResult.newBuilder()
                .setSsid("Legacy")
                .setBssid("11:22:33:44:55:66")
                .setRssiDbm(-60)
                .setBand("5ghz")
                .setFrequencyMhz(5200)
                .build())
            .build()

        val out = AgentWifiScanRenderer.render(scan).joinToString("\n")

        listOf(
            "Wi-Fi Scan",
            "FIELD",
            "requested_band",
            "SSID",
            "AP_MLD",
            "AP_LINK",
            "AFFILIATED",
            "02:00:00:00:00:01",
            "Scan Affiliated MLO Links",
            "active",
            "1200",
            "<none>",
        ).forEach { want ->
            assertTrue("rendered output missing $want:\n$out", out.contains(want))
        }
        assertFalse(out.contains("\nFields\n"))
        assertFalse(out.contains("\nScan Results\n"))
    }

    @Test
    fun rendersScanDetail() {
        val detail = WifiScanDetail.newBuilder()
            .setTarget("Lab")
            .addResults(mloScanResult())
            .build()

        val out = AgentWifiScanRenderer.renderDetail(detail).joinToString("\n")

        assertTrue(out.contains("Wi-Fi Scan Detail"))
        assertTrue(out.contains("target   Lab"))
        assertTrue(out.contains("AP_MLD"))
        assertTrue(out.contains("02:00:00:00:00:01"))
    }

    private fun mloScanResult(): WifiScanResult {
        return WifiScanResult.newBuilder()
            .setSsid("Lab")
            .setBssid("aa:bb:cc:dd:ee:ff")
            .setRssiDbm(-48)
            .setBand("6ghz")
            .setFrequencyMhz(5975)
            .setWifiStandard("802.11be")
            .setApMldMacAddress("02:00:00:00:00:01")
            .setApMloLinkId(2)
            .addSecurityTypes("wpa3_sae")
            .addAffiliatedMloLinks(MloLinkInfo.newBuilder()
                .setLinkId(2)
                .setState("active")
                .setBand("6ghz")
                .setChannel(5)
                .setRssiDbm(-48)
                .setTxLinkSpeedMbps(1200)
                .setRxLinkSpeedMbps(1200)
                .setApMacAddress("02:00:00:00:00:02")
                .setStaMacAddress("02:00:00:00:00:03")
                .build())
            .build()
    }
}
