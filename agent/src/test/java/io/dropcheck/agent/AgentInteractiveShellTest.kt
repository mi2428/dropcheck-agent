package io.dropcheck.agent

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class AgentInteractiveShellTest {
    @Test
    fun parsesUseCommands() {
        assertEquals(AgentShellCommand.Use("ap1"), AgentShellParser.parse("use ap1"))
        assertEquals(AgentShellCommand.Use("lab wifi"), AgentShellParser.parse("use \"lab wifi\""))
        assertEquals(AgentShellCommand.ClearUse, AgentShellParser.parse("clear use"))
        assertEquals(AgentShellCommand.ShowUse, AgentShellParser.parse("show use"))
        assertEquals(AgentShellCommand.Help, AgentShellParser.parse("help"))
    }

    @Test
    fun rejectsMalformedUseCommands() {
        assertEquals(AgentShellCommand.Invalid("usage: use <name>"), AgentShellParser.parse("use"))
        assertEquals(AgentShellCommand.Invalid("usage: clear use"), AgentShellParser.parse("clear"))
        assertTrue(AgentShellParser.parse("use \"ap1") is AgentShellCommand.Invalid)
    }
}
