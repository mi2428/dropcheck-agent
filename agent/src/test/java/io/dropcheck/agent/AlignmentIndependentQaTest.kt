package io.dropcheck.agent

import io.dropcheck.agent.grpc.*
import org.junit.Assert.*
import org.junit.Test

class AlignmentIndependentQaTest {
    @Test fun diagnosticIdentityKeepsCredentialLookingSsid() {
        val ssid = "password=Guest"
        val block = AgentPresentationBlock.create(listOf(AgentBlockPart.Field("ssid", ssid)), 0)
        assertTrue("A typed diagnostic identity was reinterpreted as a credential", block.fullText.contains(ssid))
    }

    @Test fun multilineTableKeepsSafeCopyValuesWithoutParsingRawContinuations() {
        val raw = "first\n2001:db8::1\nRaw:    preserve:    spaces\npassword=reqa-synthetic-secret"
        val columns = listOf(AgentBlockPart.Column("HOST"), AgentBlockPart.Column("ERROR"))
        val rows = listOf(listOf("日本語e\u0301👩\u200d🔬", raw))
        val block = AgentPresentationBlock.create(listOf(AgentBlockPart.Table(columns, rows)), 0)
        assertEquals(raw, rows.single().last())
        assertTrue(block.fullText.contains("ERROR: first\n       2001:db8::1\n       Raw:    preserve:    spaces"))
        assertTrue(block.safeValues().any { it.second.contains("first\n2001:db8::1\nRaw:    preserve:    spaces") })
        val filtered = AgentShellPipeline.parse(listOf("except never-match")).apply(AgentShellResult(false, block = block))
        assertFalse(filtered.ok)
        assertTrue(filtered.block!!.fullText.contains("       Raw:    preserve:    spaces"))
        assertFalse(filtered.block.parts.filterIsInstance<AgentBlockPart.Field>().any { it.label == "Raw" })
        val json = AgentShellPipeline.parse(listOf("display json")).apply(AgentShellResult(false, block = block)).block!!.fullText
        assertTrue(json.contains("       Raw:    preserve:    spaces"))
        for (text in listOf(block.fullText, filtered.block.fullText, json)) assertFalse(text.contains("reqa-synthetic-secret"))
    }
}
