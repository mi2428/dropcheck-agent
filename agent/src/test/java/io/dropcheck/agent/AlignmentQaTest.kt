package io.dropcheck.agent

import io.dropcheck.agent.grpc.*
import org.junit.Assert.*
import org.junit.Test

class AlignmentQaTest {
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
