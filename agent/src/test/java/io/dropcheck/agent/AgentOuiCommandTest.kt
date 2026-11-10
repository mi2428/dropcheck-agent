package io.dropcheck.agent

import org.junit.Assert.*
import org.junit.Test

class AgentOuiCommandTest {
    @Test fun ouiCommandsAreLocalAndPreservePipelines() {
        assertEquals(AgentShellCommand.ShowOui, AgentShellParser.parse("sh oui"))
        assertEquals(AgentShellCommand.UpdateOui, AgentShellParser.parse("req oui up"))
        val adapter = AgentShellAdapter { error("local OUI command dispatched an acquisition") }
        val result = adapter.execute(AgentShellParser.parse("show oui | display json"))
        assertTrue(result.ok)
        assertTrue(checkNotNull(result.block).fullText.contains("IEEE MA-L/MA-M/MA-S/IAB"))
        assertTrue(result.block!!.fullText.contains("\"ok\":true"))
        for (invalid in listOf("show oui extra", "request oui update https://example.test/", "request oui update timeout 999999")) {
            assertTrue(invalid, AgentShellParser.parse(invalid) is AgentShellCommand.Invalid)
        }
    }
}
