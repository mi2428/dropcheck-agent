package io.dropcheck.agent

import io.dropcheck.agent.grpc.*
import org.junit.Assert.*
import org.junit.Test

class AlignmentQaTest {
    @Test fun scanAgeIsExactLeftAlignedSecondsWithKnownOnlyAliases() {
        for ((ms, want) in listOf(0L to "0", 1L to "0.001", 10L to "0.01", 999L to "0.999", 1000L to "1", 1001L to "1.001", -1L to "18446744073709551.615")) {
            assertEquals(want, AgentWifiScanRenderer.ageSeconds(WifiScanResult.newBuilder().setObservationAgeMs(ms).build()))
        }
        assertEquals("?", AgentWifiScanRenderer.ageSeconds(WifiScanResult.getDefaultInstance()))
        assertEquals("wpa3-eap+future_Mode", AgentWifiScanRenderer.security(listOf("eap_wpa3_enterprise", "future_Mode")))
        val scan = WifiScan.newBuilder().addResults(WifiScanResult.newBuilder().setSsid("日本語e\u0301👩\u200d🔬").setObservationAgeMs(1001)).build()
        val table = AgentWifiScanRenderer.presentation(scan, AgentWifiScanContext(brief = true)).filterIsInstance<AgentBlockPart.Scan>().single()
        assertEquals("AGE(s)", table.columns.last().label)
        assertFalse(table.columns.last().numeric)
        assertEquals("1.001", table.rows.single().last())
    }

    @Test fun multilineFieldsAndFilteredFactsKeepTheirAlignmentRoles() {
        val raw = "Raw:    retain:    spacing\nhttps://example.test/a:b"
        val block = AgentShellTextFormatter.block(CommandResult.newBuilder().setStatus(CommandResult.Status.STATUS_FAILED).setMessage("token=alignment-secret").build(), listOf(
            AgentBlockPart.Field("DNS", "192.0.2.53\n2001:db8::53"), AgentBlockPart.Field("SSID", "password=Guest"), AgentBlockPart.Text(raw)), 0)
        assertTrue(block.fullText.contains("DNS: 192.0.2.53\n     2001:db8::53"))
        assertTrue(block.fullText.contains(raw))
        assertFalse(block.fullText.contains("alignment-secret"))
        assertTrue(block.parts.filterIsInstance<AgentBlockPart.Field>().any { it.label == "Latency" })
        val filtered = AgentShellPipeline.parse(listOf("match 'SSID|Latency'")).apply(AgentShellResult(false, block = block))
        assertFalse(filtered.ok)
        assertTrue(filtered.block!!.parts.filterIsInstance<AgentBlockPart.Field>().any { it.label == "SSID" && it.value == "password=Guest" })
        assertTrue(filtered.block.parts.filterIsInstance<AgentBlockPart.Field>().any { it.label == "Latency" })
    }
}
