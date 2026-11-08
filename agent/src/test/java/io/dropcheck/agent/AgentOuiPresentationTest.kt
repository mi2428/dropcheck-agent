package io.dropcheck.agent

import io.dropcheck.agent.grpc.*
import org.junit.Assert.*
import org.junit.Test

class AgentOuiPresentationTest {
    @Test fun vendorFieldsRetainIdentityAvailabilityAndSafeValues() {
        for (available in listOf(true, false)) {
            val conn = WifiConnection.newBuilder().setSsid("Lab").setBssid("e8:0a:b9:00:00:01")
                .addAllObservationFields(observationAvailability("identity", available)).build()
            val status = WifiStatus.newBuilder().setConnection(conn).build()
            for (parts in listOf(AgentWifiStatusRenderer.summary(status), AgentWifiStatusRenderer.presentation(status, detail = true),
                AgentWifiStatusRenderer.ipPresentation(IpStatus.newBuilder().setWifi(conn).build(), detail = true),
                AgentWifiMloRenderer.presentation(status, WifiScan.getDefaultInstance(), AgentWifiMloContext(sdkInt = 37)))) {
                val block = AgentPresentationBlock.create(parts, 0)
                assertEquals(available, block.fullText.contains("Cisco"))
                assertEquals(available, block.safeValues().any { it.second.contains("Cisco") })
                val json = AgentShellPipeline.parse(listOf("display json")).apply(AgentShellResult(true, block = block)).block!!.fullText
                assertEquals(available, json.contains("Cisco"))
            }
        }
    }

    @Test fun fullScanKeepsVendorsWhileBriefKeepsItsOriginalColumns() {
        val scan = WifiScan.newBuilder().addResults(WifiScanResult.newBuilder().setSsid("Lab").setBssid("e8:0a:b9:00:00:01"))
            .addResults(WifiScanResult.newBuilder().setSsid("Local").setBssid("02:00:00:00:00:01")).build()
        for (brief in listOf(true, false)) {
            val block = AgentPresentationBlock.create(AgentWifiScanRenderer.presentation(scan, AgentWifiScanContext(brief = brief, detail = !brief)), 0)
            assertEquals(!brief, block.fullText.contains("Cisco"))
            assertEquals(!brief, block.fullText.contains("local MAC"))
            assertEquals(!brief, block.safeValues().any { it.first == "AP 1 VENDOR" && it.second.contains("Cisco") })
            if (brief) assertFalse(block.fullText.contains("VENDOR")) else {
                val filtered = AgentShellPipeline.parse(listOf("match 'VENDOR|Cisco'")).apply(AgentShellResult(true, block = block))
                assertTrue(filtered.block!!.fullText.contains("Cisco"))
            }
        }
    }
}
