package io.dropcheck.agent

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class AgentInteractiveShellTest {
    @Test
    fun parsesUseCommands() {
        assertEquals(AgentShellCommand.Use("ap1"), AgentShellParser.parse("use ap1"))
        assertEquals(AgentShellCommand.Use("AP1"), AgentShellParser.parse("u AP1"))
        assertEquals(AgentShellCommand.Use("lab wifi"), AgentShellParser.parse("use \"lab wifi\""))
        assertEquals(AgentShellCommand.ClearUse, AgentShellParser.parse("clear use"))
        assertEquals(AgentShellCommand.ClearUse, AgentShellParser.parse("c u"))
        assertEquals(AgentShellCommand.Show, AgentShellParser.parse("show"))
        assertEquals(AgentShellCommand.Show, AgentShellParser.parse("sh"))
        assertEquals(AgentShellCommand.List, AgentShellParser.parse("list"))
        assertEquals(AgentShellCommand.List, AgentShellParser.parse("l"))
        assertEquals(AgentShellCommand.Help(), AgentShellParser.parse("help"))
        assertEquals(AgentShellCommand.Help(), AgentShellParser.parse("h"))
        assertEquals(AgentShellCommand.Help(), AgentShellParser.parse("he"))
        assertEquals(AgentShellCommand.Help(), AgentShellParser.parse("hel"))
        assertEquals(AgentShellCommand.Help("use"), AgentShellParser.parse("help use"))
        assertEquals(AgentShellCommand.Help("use"), AgentShellParser.parse("h u"))
    }

    @Test
    fun rejectsMalformedUseCommands() {
        assertEquals(AgentShellCommand.Invalid("usage: use NAME"), AgentShellParser.parse("use"))
        assertEquals(AgentShellCommand.Invalid("usage: clear use"), AgentShellParser.parse("clear"))
        assertEquals(AgentShellCommand.Invalid("usage: show"), AgentShellParser.parse("show use"))
        assertEquals(AgentShellCommand.Invalid("usage: list"), AgentShellParser.parse("list ap1"))
        assertEquals(AgentShellCommand.Invalid("usage: help [NAME]"), AgentShellParser.parse("help use extra"))
        assertEquals(AgentShellCommand.Invalid("usage: use NAME"), AgentShellParser.parse("u"))
        assertEquals(AgentShellCommand.Invalid("missing: command not found"), AgentShellParser.parse("missing"))
        assertTrue(AgentShellParser.parse("use \"ap1") is AgentShellCommand.Invalid)
    }
}
