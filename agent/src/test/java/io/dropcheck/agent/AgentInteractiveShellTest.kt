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
        assertEquals(AgentShellCommand.ShowVersion, AgentShellParser.parse("show version"))
        assertEquals(AgentShellCommand.ShowVersion, AgentShellParser.parse("sh v"))
        assertEquals(AgentShellCommand.ShowWifiStatus, AgentShellParser.parse("show wifi status"))
        assertEquals(AgentShellCommand.ShowWifiStatus, AgentShellParser.parse("sh wi sta"))
        assertEquals(AgentShellCommand.ShowWifiMlo(), AgentShellParser.parse("show wifi mlo"))
        assertEquals(AgentShellCommand.ShowWifiMlo(), AgentShellParser.parse("sh wi m"))
        assertEquals(AgentShellCommand.ShowWifiMlo(fresh = true), AgentShellParser.parse("show wifi mlo fresh"))
        assertEquals(AgentShellCommand.ShowWifiMlo(fresh = true, timeoutMs = 9000), AgentShellParser.parse("show wifi mlo fresh timeout 9000"))
        assertEquals(AgentShellCommand.ShowWifiMlo(fresh = true, timeoutMs = 9000), AgentShellParser.parse("show wifi mlo fresh 9000"))
        assertEquals(AgentShellCommand.ShowWifiMlo(ssid = "temp-life26"), AgentShellParser.parse("show wifi mlo ssid temp-life26"))
        assertEquals(AgentShellCommand.ShowWifiMlo(bssid = "aa:bb:cc:dd:ee:ff"), AgentShellParser.parse("show wifi mlo bssid aa:bb:cc:dd:ee:ff"))
        assertEquals(AgentShellCommand.ShowWifiMlo(fresh = true, timeoutMs = 9000, ssid = "temp-life26"), AgentShellParser.parse("show wifi mlo fresh timeout 9000 ssid temp-life26"))
        assertEquals(AgentShellCommand.Ping("1.1.1.1"), AgentShellParser.parse("ping 1.1.1.1"))
        assertEquals(AgentShellCommand.Ping("1.1.1.1", count = 3, sizeBytes = 64, timeoutMs = 7000), AgentShellParser.parse("p count 3 size 64 timeout 7000 1.1.1.1"))
        assertEquals(AgentShellCommand.Traceroute("1.1.1.1"), AgentShellParser.parse("traceroute 1.1.1.1"))
        assertEquals(AgentShellCommand.Traceroute("example.test", maxHops = 12, sizeBytes = 80, timeoutMs = 30000), AgentShellParser.parse("tr max-hops 12 size 80 timeout 30000 example.test"))
        assertEquals(AgentShellCommand.Help(), AgentShellParser.parse("help"))
        assertEquals(AgentShellCommand.Help(), AgentShellParser.parse("h"))
        assertEquals(AgentShellCommand.Help(), AgentShellParser.parse("he"))
        assertEquals(AgentShellCommand.Help(), AgentShellParser.parse("hel"))
        assertEquals(AgentShellCommand.Help("use"), AgentShellParser.parse("help use"))
        assertEquals(AgentShellCommand.Help("ping"), AgentShellParser.parse("help p"))
        assertEquals(AgentShellCommand.Help("traceroute"), AgentShellParser.parse("help tr"))
        assertEquals(AgentShellCommand.Help("use"), AgentShellParser.parse("h u"))
    }

    @Test
    fun rejectsMalformedUseCommands() {
        assertEquals(AgentShellCommand.Invalid("usage: use NAME"), AgentShellParser.parse("use"))
        assertEquals(AgentShellCommand.Invalid("usage: clear use"), AgentShellParser.parse("clear"))
        assertEquals(AgentShellCommand.Invalid("usage: show (version|use|wifi status|wifi mlo)"), AgentShellParser.parse("show"))
        assertEquals(AgentShellCommand.Invalid("usage: show (version|use|wifi status|wifi mlo)"), AgentShellParser.parse("sh"))
        assertEquals(AgentShellCommand.Invalid("usage: show wifi (status|mlo)"), AgentShellParser.parse("show wifi"))
        assertEquals(AgentShellCommand.Invalid("usage: show wifi status"), AgentShellParser.parse("show wifi status extra"))
        assertEquals(AgentShellCommand.Invalid("usage: show wifi (status|mlo)"), AgentShellParser.parse("show wifi networks"))
        assertEquals(AgentShellCommand.Invalid("usage: show wifi mlo [fresh [timeout MS]] [ssid SSID|bssid BSSID]"), AgentShellParser.parse("show wifi mlo current"))
        assertEquals(AgentShellCommand.Invalid("usage: show wifi mlo [fresh [timeout MS]] [ssid SSID|bssid BSSID]"), AgentShellParser.parse("show wifi mlo fresh timeout"))
        assertEquals(AgentShellCommand.Invalid("ssid and bssid filters cannot be used together"), AgentShellParser.parse("show wifi mlo ssid Lab bssid aa:bb:cc:dd:ee:ff"))
        assertEquals(AgentShellCommand.Invalid("usage: ping HOST [count N] [size BYTES] [timeout MS]"), AgentShellParser.parse("ping"))
        assertEquals(AgentShellCommand.Invalid("count requires a value"), AgentShellParser.parse("ping 1.1.1.1 count"))
        assertEquals(AgentShellCommand.Invalid("size must be a positive integer"), AgentShellParser.parse("ping 1.1.1.1 size 0"))
        assertEquals(AgentShellCommand.Invalid("timeout specified twice"), AgentShellParser.parse("ping 1.1.1.1 timeout 100 timeout 200"))
        assertEquals(AgentShellCommand.Invalid("usage: traceroute HOST [max-hops N] [size BYTES] [timeout MS]"), AgentShellParser.parse("traceroute"))
        assertEquals(AgentShellCommand.Invalid("max-hops must be a positive integer"), AgentShellParser.parse("traceroute 1.1.1.1 max-hops nope"))
        assertEquals(AgentShellCommand.Invalid("list: command not found"), AgentShellParser.parse("list"))
        assertEquals(AgentShellCommand.Invalid("usage: help [NAME]"), AgentShellParser.parse("help use extra"))
        assertEquals(AgentShellCommand.Invalid("usage: use NAME"), AgentShellParser.parse("u"))
        assertEquals(AgentShellCommand.Invalid("missing: command not found"), AgentShellParser.parse("missing"))
        assertTrue(AgentShellParser.parse("use \"ap1") is AgentShellCommand.Invalid)
    }
}
