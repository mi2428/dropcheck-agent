package io.dropcheck.agent

import io.dropcheck.agent.grpc.DiagnosticField
import io.dropcheck.agent.grpc.MloLinkInfo
import io.dropcheck.agent.grpc.WifiConnection
import io.dropcheck.agent.grpc.WifiScan
import io.dropcheck.agent.grpc.WifiScanResult
import io.dropcheck.agent.grpc.WifiStatus
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class AgentWifiMloRendererTest {
    @Test
    fun rendersConnectedAndNearbyMloState() {
        val status = WifiStatus.newBuilder()
            .setEnabled(true)
            .setState("enabled")
            .setConnection(mloConnection())
            .addPermissions("ACCESS_FINE_LOCATION=granted")
            .build()
        val scan = WifiScan.newBuilder()
            .addFields(field("scan_result_count", "3"))
            .addFields(field("scan_result_total_count", "4"))
            .addResults(mloScanResult("Lab", "aa:bb:cc:dd:ee:ff", -45, "02:00:00:00:00:01", 2))
            .addResults(mloScanResult("Lab", "aa:bb:cc:dd:ee:01", -55, "02:00:00:00:00:01", 1))
            .addResults(WifiScanResult.newBuilder()
                .setSsid("Legacy")
                .setBssid("11:22:33:44:55:66")
                .setRssiDbm(-60)
                .setBand("5ghz")
                .setFrequencyMhz(5200)
                .build())
            .build()

        val out = AgentWifiMloRenderer.render(
            status,
            scan,
            AgentWifiMloContext(scanSource = "cached", sdkInt = 36, wifi7Supported = true),
        ).joinToString("\n")

        listOf(
            "Connected MLO",
            "ap_mld      02:00:00:00:00:01",
            "Associated MLO Links",
            "MLO Scan",
            "mlo_candidates",
            "Nearby MLO APs",
            "*",
            "AP_MLD",
            "02:00:00:00:00:01",
            "MLO Scan Links",
            "[*] scan Lab",
            "[+] affiliated Lab",
            "Current AP Relation",
            "same_mld_results",
            "visible_links",
            "Diagnostics / Warnings",
            "none",
        ).forEach { want ->
            assertTrue("rendered output missing $want:\n$out", out.contains(want))
        }
        assertTrue(out.indexOf("Current AP Relation") < out.indexOf("Connected MLO"))
        assertTrue(out.indexOf("Connected MLO") < out.indexOf("MLO Scan"))
        assertFalse(out.contains("Legacy  11:22:33:44:55:66"))
        assertFalse(out.contains("connected_ap_mld_not_seen_in_scan"))
    }

    @Test
    fun rendersMloDiagnostics() {
        val status = WifiStatus.newBuilder()
            .setEnabled(false)
            .setState("disabled")
            .setConnection(WifiConnection.newBuilder()
                .setSsid("Lab")
                .setBssid("aa:bb:cc:dd:ee:ff")
                .build())
            .addPermissions("ACCESS_FINE_LOCATION=denied")
            .build()
        val scan = WifiScan.newBuilder()
            .addFields(field("wifi_enabled", "false"))
            .addFields(field("scan_throttle_enabled", "true"))
            .addErrors("get_scan_results=SecurityException")
            .build()

        val out = AgentWifiMloRenderer.render(
            status,
            scan,
            AgentWifiMloContext(
                scanSource = "fresh",
                sdkInt = 32,
                wifi7Supported = false,
                scanCommandStatus = "STATUS_FAILED",
                scanCommandMessage = "fresh scan incomplete",
            ),
        ).joinToString("\n")

        listOf(
            "android_api_unavailable sdk=32 required_sdk=33",
            "wifi_7_standard_unsupported",
            "wifi_disabled",
            "wifi_state=disabled",
            "permission ACCESS_FINE_LOCATION=denied",
            "scan_wifi_enabled=false",
            "scan_throttle_enabled=true",
            "scan_error=get_scan_results=SecurityException",
            "scan_command_status=STATUS_FAILED",
            "scan_command_message=fresh scan incomplete",
            "connected_mlo_present=false",
            "mlo_scan_results=0",
        ).forEach { want ->
            assertTrue("rendered output missing $want:\n$out", out.contains(want))
        }
    }

    private fun mloConnection(): WifiConnection {
        return WifiConnection.newBuilder()
            .setSsid("Lab")
            .setBssid("aa:bb:cc:dd:ee:ff")
            .setRssiDbm(-45)
            .setFrequencyMhz(5975)
            .setWifiStandard("802.11be")
            .setApMldMacAddress("02:00:00:00:00:01")
            .setApMloLinkId(2)
            .addAssociatedMloLinks(mloLink(2, "active", "6ghz", 5, -45))
            .addAffiliatedMloLinks(mloLink(1, "idle", "5ghz", 44, -55))
            .build()
    }

    private fun mloScanResult(ssid: String, bssid: String, rssi: Int, mld: String, linkId: Int): WifiScanResult {
        return WifiScanResult.newBuilder()
            .setSsid(ssid)
            .setBssid(bssid)
            .setRssiDbm(rssi)
            .setBand("6ghz")
            .setFrequencyMhz(5975)
            .setChannelWidth("80MHz")
            .setWifiStandard("802.11be")
            .setApMldMacAddress(mld)
            .setApMloLinkId(linkId)
            .addSecurityTypes("wpa3_sae")
            .addAffiliatedMloLinks(mloLink(1, "idle", "5ghz", 44, -55))
            .addAffiliatedMloLinks(mloLink(2, "active", "6ghz", 5, rssi))
            .build()
    }

    private fun mloLink(id: Int, state: String, band: String, channel: Int, rssi: Int): MloLinkInfo {
        return MloLinkInfo.newBuilder()
            .setLinkId(id)
            .setState(state)
            .setBand(band)
            .setChannel(channel)
            .setRssiDbm(rssi)
            .setTxLinkSpeedMbps(1200)
            .setRxLinkSpeedMbps(1200)
            .setApMacAddress("02:00:00:00:00:0$id")
            .setStaMacAddress("02:00:00:00:00:1$id")
            .build()
    }

    private fun field(key: String, value: String): DiagnosticField {
        return DiagnosticField.newBuilder()
            .setKey(key)
            .setValue(value)
            .build()
    }
}
