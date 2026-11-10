package io.dropcheck.agent

import io.dropcheck.agent.grpc.*
import org.junit.Assert.*
import org.junit.Test

class WifiScanPresentationTest {
    @Test fun sharedApMarkCases() {
        val cases = checkNotNull(javaClass.getResourceAsStream("/scan_mark_cases.tsv")).bufferedReader().use { it.readLines() }.drop(1)
        for (line in cases) {
            val (evidence, mac, connected, observation, expected) = line.split('\t')
            val ap = WifiScanResult.newBuilder().setBssid(mac)
            when (evidence) {
                "be" -> ap.wifiStandard = "802.11be"
                "cap", "truncated" -> ap.setEhtCapabilities(WifiEhtCapabilities.newBuilder().setMac(WifiEhtMacCapabilities.getDefaultInstance()).setPhy(WifiEhtPhyCapabilities.getDefaultInstance()).setTruncated(evidence == "truncated"))
                "empty-cap" -> ap.setEhtCapabilities(WifiEhtCapabilities.getDefaultInstance())
                "operation" -> ap.setEhtOperation(WifiEhtOperation.newBuilder().setBasicMcsNssSetHex("00000000"))
                "mlo-only" -> ap.apMldMacAddress = "00:11:22:33:44:55"
                "bandwidth-only" -> { ap.band = "6ghz"; ap.channelWidth = "320MHz" }
                "none" -> Unit
                else -> error(evidence)
            }
            val connections = WifiScanConnections.newBuilder().setComplete(observation != "partial")
                .setObservedAtUnixMs(if (observation == "missing-time") 0 else 1234)
            if (connected != "-") connections.addAllBssids(connected.split(','))
            if (observation in listOf("partial", "contradictory")) connections.addErrors("synthetic unavailable identity")
            assertEquals(line, if (expected == "blank") " " else expected, scanApMark(ap.build(), connections.build()))
        }
    }

    @Test fun semanticParagraphsHaveOneBoundaryAndKeepLiteralText() {
        val literal = "native:  unchanged\n\n\nreference: value"
        val parts = listOf(AgentBlockPart.Text("Summary", section = true), AgentBlockPart.Field("A", "0"), AgentBlockPart.Field("B", "false"),
            AgentBlockPart.Text("Child", section = true), AgentBlockPart.Field("C", "x\ny"),
            AgentBlockPart.Text("", section = true), AgentBlockPart.Text("Next", section = true), AgentBlockPart.Field("D", ""),
            AgentBlockPart.Text("Reference", section = true), AgentBlockPart.Text(literal))
        val block = AgentPresentationBlock.create(parts, 0)
        assertEquals("Summary\nA: 0\nB: false\n\nChild\nC: x\n   y\n\nNext\nD: \n\nReference\n$literal", block.fullText)
        assertTrue(block.safeValues().any { it.first == "C" && it.second == "x\ny" })
        val source = AgentPresentationBlock.commandLine("self>  malformed \n\n  raw", 0, 5)
        assertEquals("self>  malformed \n\n  raw", source.fullText)
        val ip = AgentPresentationBlock.create(AgentWifiStatusRenderer.ipPresentation(IpStatus.getDefaultInstance()), 0).fullText
        for (heading in listOf("IPv4", "IPv6", "Routing and DNS", "Network capabilities", "Link properties")) assertTrue(heading, ip.contains("\n\n$heading\n"))
    }

    @Test fun filteredScanRetainsRecordedConnectionEvidenceWithoutAnotherAcquisition() {
        val ap = WifiScanResult.newBuilder().setSsid("Lab").setBssid("00:11:22:33:44:55").setWifiStandard("802.11be")
        val scan = WifiScan.newBuilder().addResults(ap).setConnections(WifiScanConnections.newBuilder().setObservedAtUnixMs(1234).setComplete(true).addBssids(ap.bssid)).build()
        val result = CommandResult.newBuilder().setStatus(CommandResult.Status.STATUS_OK).setWifiScan(scan).build()
        var calls = 0
        val rendered = AgentShellAdapter { calls++; result }.execute(AgentShellParser.parse("show wifi scan bssid 00:11:22:33:44:55 | display json")).block!!.fullText
        assertEquals(1, calls)
        assertTrue(rendered.contains("[*] AP 1"))
        assertTrue(rendered.contains("observed (802.11be)"))
        assertTrue(rendered.contains("1970-01-01T00:00:01.234Z"))
        assertTrue(scanConnectionsSummary(WifiScanConnections.newBuilder().setObservedAtUnixMs(1234).setComplete(true).build()).startsWith("disconnected"))
        assertTrue(scanConnectionsSummary(WifiScanConnections.getDefaultInstance()).contains("not captured"))
    }
}
