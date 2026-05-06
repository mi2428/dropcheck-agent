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
        assertEquals(AgentShellCommand.ShowUse, AgentShellParser.parse("show use"))
        assertEquals(AgentShellCommand.ShowUse, AgentShellParser.parse("sh u"))
        assertEquals(AgentShellCommand.ShowWifiStatus, AgentShellParser.parse("show wifi status"))
        assertEquals(AgentShellCommand.ShowWifiStatus, AgentShellParser.parse("sh wi sta"))
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
        assertEquals(AgentShellCommand.Invalid("usage: show (use|wifi status)"), AgentShellParser.parse("show"))
        assertEquals(AgentShellCommand.Invalid("usage: show (use|wifi status)"), AgentShellParser.parse("sh"))
        assertEquals(AgentShellCommand.Invalid("usage: show wifi status"), AgentShellParser.parse("show wifi"))
        assertEquals(AgentShellCommand.Invalid("usage: show wifi status"), AgentShellParser.parse("show wifi status extra"))
        assertEquals(AgentShellCommand.Invalid("usage: show wifi status"), AgentShellParser.parse("show wifi networks"))
        assertEquals(AgentShellCommand.Invalid("list: command not found"), AgentShellParser.parse("list"))
        assertEquals(AgentShellCommand.Invalid("usage: help [NAME]"), AgentShellParser.parse("help use extra"))
        assertEquals(AgentShellCommand.Invalid("usage: use NAME"), AgentShellParser.parse("u"))
        assertEquals(AgentShellCommand.Invalid("missing: command not found"), AgentShellParser.parse("missing"))
        assertTrue(AgentShellParser.parse("use \"ap1") is AgentShellCommand.Invalid)
    }
}
