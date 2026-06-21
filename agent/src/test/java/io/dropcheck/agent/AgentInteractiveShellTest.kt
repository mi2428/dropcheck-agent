package io.dropcheck.agent

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class AgentInteractiveShellTest {
    @Test
    fun parsesInteractiveCommands() {
        assertEquals(AgentShellCommand.ShowVersion, AgentShellParser.parse("show version"))
        assertEquals(AgentShellCommand.ShowVersion, AgentShellParser.parse("sh v"))
        assertEquals(AgentShellCommand.ShowWifiStatus, AgentShellParser.parse("show wifi status"))
        assertEquals(AgentShellCommand.ShowWifiStatus, AgentShellParser.parse("sh wi sta"))
        assertEquals(AgentShellCommand.ShowWifiEht(), AgentShellParser.parse("show wifi eht"))
        assertEquals(AgentShellCommand.ShowWifiEht(), AgentShellParser.parse("sh wi e"))
        assertEquals(AgentShellCommand.ShowWifiEht(brief = true), AgentShellParser.parse("show wifi eht brief"))
        assertEquals(AgentShellCommand.ShowWifiEht(fresh = true), AgentShellParser.parse("show wifi eht fresh"))
        assertEquals(AgentShellCommand.ShowWifiEht(fresh = true, timeoutMs = 9000), AgentShellParser.parse("show wifi eht fresh timeout 9000"))
        assertEquals(AgentShellCommand.ShowWifiEht(fresh = true, timeoutMs = 9000), AgentShellParser.parse("show wifi eht fresh 9000"))
        assertEquals(AgentShellCommand.ShowWifiEht(ssid = "temp-life26"), AgentShellParser.parse("show wifi eht ssid temp-life26"))
        assertEquals(AgentShellCommand.ShowWifiEht(bssid = "aa:bb:cc:dd:ee:ff"), AgentShellParser.parse("show wifi eht bssid aa:bb:cc:dd:ee:ff"))
        assertEquals(AgentShellCommand.ShowWifiEht(fresh = true, timeoutMs = 9000, ssid = "temp-life26"), AgentShellParser.parse("show wifi eht fresh timeout 9000 ssid temp-life26"))
        assertEquals(AgentShellCommand.ShowWifiScan(band = "6ghz"), AgentShellParser.parse("show wifi scan 6ghz"))
        assertEquals(AgentShellCommand.ShowWifiScan(brief = true, mlo = true, band = "5ghz"), AgentShellParser.parse("show wifi scan brief mlo 5ghz"))
        assertEquals(
            AgentShellCommand.ShowWifiScan(brief = true, mlo = true, fresh = true, timeoutMs = 9000, band = "6ghz"),
            AgentShellParser.parse("show wifi scan fresh brief mlo timeout 9000 6ghz"),
        )
        assertEquals(AgentShellCommand.Ping("1.1.1.1"), AgentShellParser.parse("ping 1.1.1.1"))
        assertEquals(AgentShellCommand.Ping("1.1.1.1", count = 3, sizeBytes = 64, timeoutMs = 7000), AgentShellParser.parse("p count 3 size 64 timeout 7000 1.1.1.1"))
        assertEquals(AgentShellCommand.Traceroute("1.1.1.1"), AgentShellParser.parse("traceroute 1.1.1.1"))
        assertEquals(AgentShellCommand.Traceroute("example.test", maxHops = 12, sizeBytes = 80, timeoutMs = 30000), AgentShellParser.parse("tr max-hops 12 size 80 timeout 30000 example.test"))
        assertEquals(AgentShellCommand.Help(), AgentShellParser.parse("help"))
        assertEquals(AgentShellCommand.Help(), AgentShellParser.parse("h"))
        assertEquals(AgentShellCommand.Help(), AgentShellParser.parse("he"))
        assertEquals(AgentShellCommand.Help(), AgentShellParser.parse("hel"))
        assertEquals(AgentShellCommand.Help("show"), AgentShellParser.parse("help show"))
        assertEquals(AgentShellCommand.Help("ping"), AgentShellParser.parse("help p"))
        assertEquals(AgentShellCommand.Help("traceroute"), AgentShellParser.parse("help tr"))
        assertEquals(AgentShellCommand.Help("show"), AgentShellParser.parse("h sh"))
    }

    @Test
    fun rejectsMalformedCommands() {
        assertEquals(AgentShellCommand.Invalid("usage: show (version|wifi status|wifi eht|wifi scan)"), AgentShellParser.parse("show"))
        assertEquals(AgentShellCommand.Invalid("usage: show (version|wifi status|wifi eht|wifi scan)"), AgentShellParser.parse("sh"))
        assertEquals(AgentShellCommand.Invalid("usage: show wifi (status|eht|scan)"), AgentShellParser.parse("show wifi"))
        assertEquals(AgentShellCommand.Invalid("usage: show wifi status"), AgentShellParser.parse("show wifi status extra"))
        assertEquals(AgentShellCommand.Invalid("usage: show wifi (status|eht|scan)"), AgentShellParser.parse("show wifi networks"))
        assertEquals(AgentShellCommand.Invalid("usage: show wifi eht [fresh [timeout MS]] [ssid SSID|bssid BSSID]"), AgentShellParser.parse("show wifi eht current"))
        assertEquals(AgentShellCommand.Invalid("usage: show wifi eht [fresh [timeout MS]] [ssid SSID|bssid BSSID]"), AgentShellParser.parse("show wifi eht fresh timeout"))
        assertEquals(AgentShellCommand.Invalid("ssid and bssid filters cannot be used together"), AgentShellParser.parse("show wifi eht ssid Lab bssid aa:bb:cc:dd:ee:ff"))
        assertEquals(AgentShellCommand.Invalid("usage: show wifi (status|eht|scan)"), AgentShellParser.parse("show wifi mlo"))
        assertEquals(AgentShellCommand.Invalid("mlo is supported only with wifi scan brief"), AgentShellParser.parse("show wifi scan mlo"))
        assertEquals(AgentShellCommand.Invalid("usage: show wifi scan fresh [brief [mlo]] [timeout MS] [all|2.4ghz|5ghz|6ghz|60ghz]"), AgentShellParser.parse("show wifi scan fresh timeout"))
        assertEquals(AgentShellCommand.Invalid("usage: ping HOST [count N] [size BYTES] [timeout MS]"), AgentShellParser.parse("ping"))
        assertEquals(AgentShellCommand.Invalid("count requires a value"), AgentShellParser.parse("ping 1.1.1.1 count"))
        assertEquals(AgentShellCommand.Invalid("size must be a positive integer"), AgentShellParser.parse("ping 1.1.1.1 size 0"))
        assertEquals(AgentShellCommand.Invalid("timeout specified twice"), AgentShellParser.parse("ping 1.1.1.1 timeout 100 timeout 200"))
        assertEquals(AgentShellCommand.Invalid("usage: traceroute HOST [max-hops N] [size BYTES] [timeout MS]"), AgentShellParser.parse("traceroute"))
        assertEquals(AgentShellCommand.Invalid("max-hops must be a positive integer"), AgentShellParser.parse("traceroute 1.1.1.1 max-hops nope"))
        assertEquals(AgentShellCommand.Invalid("list: command not found"), AgentShellParser.parse("list"))
        assertEquals(AgentShellCommand.Invalid("usage: help [NAME]"), AgentShellParser.parse("help show extra"))
        assertEquals(AgentShellCommand.Invalid("u: command not found"), AgentShellParser.parse("u"))
        assertEquals(AgentShellCommand.Invalid("missing: command not found"), AgentShellParser.parse("missing"))
        assertEquals(AgentShellCommand.Invalid("set: command not found"), AgentShellParser.parse("set default passphrase secret"))
        assertTrue(AgentShellParser.parse("show wifi eht \"ap1") is AgentShellCommand.Invalid)
    }
}
