package io.dropcheck.agent

import io.dropcheck.agent.grpc.DnsRecordType
import io.dropcheck.agent.grpc.IpFamily
import io.dropcheck.agent.grpc.CommandResult
import io.dropcheck.agent.grpc.RunCommand
import io.dropcheck.agent.grpc.TracerouteHop
import io.dropcheck.agent.grpc.TracerouteResult
import io.dropcheck.agent.grpc.WifiBand
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class AgentInteractiveShellTest {
    @Test fun controllerCanonicalCommandsMatchAndroidWireCases() {
        val portable = mapOf(
            "E2E-015" to RunCommand.CommandCase.GET_WIFI_STATUS,
            "E2E-016" to RunCommand.CommandCase.GET_IP_STATUS,
            "E2E-017" to RunCommand.CommandCase.GET_WIFI_DIAGNOSTICS,
            "E2E-018" to RunCommand.CommandCase.GET_WIFI_CAPABILITIES,
            "E2E-019" to RunCommand.CommandCase.GET_WIFI_SCAN,
            "E2E-020" to RunCommand.CommandCase.GET_WIFI_SCAN,
            "E2E-021" to RunCommand.CommandCase.GET_FRESH_WIFI_SCAN,
            "E2E-022" to RunCommand.CommandCase.GET_WIFI_SCAN_DETAIL,
            "E2E-023" to RunCommand.CommandCase.CONNECT_WIFI,
            "E2E-024" to RunCommand.CommandCase.WAIT_WIFI_CONNECTED,
            "E2E-025" to RunCommand.CommandCase.ASSERT_WIFI,
            "E2E-026" to RunCommand.CommandCase.RECONNECT_WIFI,
            "E2E-027" to RunCommand.CommandCase.MONITOR_WIFI,
            "E2E-028" to RunCommand.CommandCase.CYCLE_WIFI,
            "E2E-029" to RunCommand.CommandCase.DISCONNECT_WIFI,
            "E2E-030" to RunCommand.CommandCase.FORGET_WIFI,
            "E2E-031" to RunCommand.CommandCase.PING,
            "E2E-032" to RunCommand.CommandCase.TRACEROUTE,
            "E2E-033" to RunCommand.CommandCase.PATH_MTU,
            "E2E-034" to RunCommand.CommandCase.GLOBAL_IP,
            "E2E-035" to RunCommand.CommandCase.RESOLVE_DNS,
            "E2E-036" to RunCommand.CommandCase.HTTP_CHECK,
            "E2E-037" to RunCommand.CommandCase.WGET,
        )
        val rejected = setOf("E2E-050", "E2E-053", "E2E-055", "E2E-056", "E2E-059", "E2E-060", "E2E-067", "E2E-081")
        val matrix = checkNotNull(javaClass.getResourceAsStream("/e2e_cases.tsv"))
            .bufferedReader().use { reader -> reader.readLines() }
        var checked = 0
        for (line in matrix.drop(1)) {
            val columns = line.split('\t', limit = 6)
            val expected = portable[columns[0]]
            if (expected == null && columns[0] !in rejected) continue
            val shellLine = columns[3].removeSurrounding("\"").replace("\"\"", "\"")
                .replace("<ssid>", "Example Lab").replace("<psk>", "test-only")
            if (expected == null) invalid(shellLine)
            else assertEquals(columns[0], expected, live(shellLine).request.commandCase)
            checked++
        }
        assertEquals(portable.size + rejected.size, checked)
    }

    private fun live(input: String): AgentShellCommand.Execute {
        val parsed = AgentShellParser.parse(input)
        assertTrue("$input: $parsed", parsed is AgentShellCommand.Execute)
        return parsed as AgentShellCommand.Execute
    }

    private fun invalid(input: String): String {
        val parsed = AgentShellParser.parse(input)
        assertTrue("$input: $parsed", parsed is AgentShellCommand.Invalid)
        return (parsed as AgentShellCommand.Invalid).message
    }

    @Test fun registryInventoryAndEhtComposite() {
        val commands = listOf(
            "show wifi status", "show wifi diagnostics", "show wifi scan", "show wifi scan fresh", "show wifi scan detail Lab",
            "show wifi capabilities", "wifi connect Lab passphrase test-only", "wifi disconnect", "wifi forget Lab",
            "wifi wait connected", "wifi assert", "wifi monitor", "wifi reconnect", "wifi cycle Lab passphrase test-only",
            "show ip status", "ping example.test", "traceroute example.test", "path-mtu example.test", "global-ip",
            "download https://example.test", "dns example.test", "http https://example.test",
        )
        assertEquals(AgentCommandRegistry.entries.map { it.commandCase }.toSet(), commands.map { live(it).request.commandCase }.toSet())
        assertEquals(22, commands.size)
        assertEquals(AgentShellCommand.ShowWifiEht(), AgentShellParser.parse("show wifi eht"))
        assertEquals(AgentShellCommand.ShowWifiEht(detail = true, fresh = true, timeoutMs = 9000),
            AgentShellParser.parse("show wifi eht detail fresh timeout 9000"))
        assertEquals(AgentShellCommand.ShowVersion, AgentShellParser.parse("show version"))
    }

    @Test fun builderDefaultsAreExplicitAndSelectNetwork() {
        assertEquals(10000, live("show wifi scan fresh").request.getFreshWifiScan.timeoutMs)
        assertEquals(WifiBand.WIFI_BAND_ALL, live("show wifi scan").request.getWifiScan.band)
        assertEquals(45000, live("wifi connect Lab passphrase test-only").request.connectWifi.timeoutMs)
        assertEquals(30000, live("wifi wait connected").request.waitWifiConnected.timeoutMs)
        assertEquals(0, live("wifi assert").request.assertWifi.timeoutMs)
        assertFalse(live("wifi assert").request.assertWifi.requireIp)
        assertEquals(10000, live("wifi monitor").request.monitorWifi.durationMs)
        assertEquals(1000, live("wifi monitor").request.monitorWifi.intervalMs)
        assertEquals(30000, live("wifi reconnect").request.reconnectWifi.timeoutMs)
        assertEquals(3, live("wifi cycle Lab passphrase test-only").request.cycleWifi.count)
        assertEquals(1000, live("wifi cycle Lab passphrase test-only").request.cycleWifi.pauseMs)
        val ping = live("ping example.test").request.ping
        assertEquals(3, ping.count); assertEquals(9000, ping.timeoutMs); assertEquals(0, ping.sizeBytes)
        assertEquals(IpFamily.IP_FAMILY_UNSPECIFIED, ping.family)
        assertEquals(30, live("traceroute example.test").request.traceroute.maxHops)
        assertEquals(60000, live("traceroute example.test").request.traceroute.timeoutMs)
        assertEquals(30000, live("path-mtu example.test").request.pathMtu.timeoutMs)
        assertEquals(0, live("path-mtu example.test").request.pathMtu.minMtuBytes)
        assertEquals(5000, live("global-ip").request.globalIp.timeoutMs)
        assertEquals(IpFamily.IP_FAMILY_ALL, live("global-ip").request.globalIp.family)
        assertEquals(Int.MAX_VALUE, live("ping example.test timeout 2147483647").request.ping.timeoutMs)
        assertEquals(5000, live("dns example.test").request.resolveDns.timeoutMs)
        assertEquals(5000, live("http example.test").request.httpCheck.timeoutMs)
        assertEquals(200, live("http example.test").request.httpCheck.expectedStatus)
        assertEquals("https://example.test", live("http example.test").request.httpCheck.url)
        assertEquals(60000, live("download https://example.test").request.wget.timeoutMs)
        assertEquals(listOf(DnsRecordType.DNS_RECORD_TYPE_A, DnsRecordType.DNS_RECORD_TYPE_AAAA), live("dns example.test").request.resolveDns.qtypesList)
        assertEquals("Lab", live("ping example.test ssid Lab").request.ping.selector.ssid)
        assertEquals("Lab", live("show ip status ssid Lab").request.getIpStatus.selector.ssid)
    }

    @Test fun literalsQuotingSwitchesAndRepeatingVia() {
        assertEquals("count", live("p count count 3").request.ping.host)
        assertEquals(3, live("p count count 3").request.ping.count)
        val connect = live("wifi connect 'scan | NAME' passphrase '  MiXeD \\\\ A  '").request.connectWifi
        assertEquals("scan | NAME", connect.ssid)
        assertEquals("  MiXeD \\ A  ", connect.passphrase)
        assertEquals("scan", live("wifi connect scan passphrase test-only").request.connectWifi.ssid)
        assertEquals(listOf("192.0.2.1", "2001:db8::1"), live("tr example.test via 192.0.2.1 via 2001:db8::1").via)
        assertTrue(live("show wifi scan fresh brief mlo band 6ghz").mlo)
        assertEquals(WifiBand.WIFI_BAND_6_GHZ, live("show wifi scan fresh brief mlo band 6ghz").request.getFreshWifiScan.band)
        assertEquals("lab\\path", live("wifi connect \"lab\\\\path\" passphrase test-only").request.connectWifi.ssid)
        assertEquals("lab\"scan", live("wifi connect \"lab\\\"scan\" passphrase test-only").request.connectWifi.ssid)
        assertEquals("a|b", (AgentShellParser.parse("use 'a|b' test-only") as AgentShellCommand.Use).ssid)
        assertEquals("|", live("wifi connect '|' passphrase test-only").request.connectWifi.ssid)
        assertEquals(AgentShellCommand.SetDefaultPassphrase(""), AgentShellParser.parse("set default passphrase ''"))
        assertEquals(listOf("show", "version"), shellSplitWords("show version").getOrThrow())
    }

    @Test fun familiesEnumsAndControlOptions() {
        assertEquals(IpFamily.IP_FAMILY_UNSPECIFIED, live("pm example.test family auto").request.pathMtu.family)
        assertEquals(IpFamily.IP_FAMILY_ALL, live("gip family all").request.globalIp.family)
        assertEquals(2, live("dns example.test record ALL").request.resolveDns.qtypesCount)
        assertEquals(DnsRecordType.DNS_RECORD_TYPE_AAAA, live("dns example.test record AAAA").request.resolveDns.qtypesList.single())
        val cycle = live("wifi cycle Lab passphrase test-only security wpa3 bssid aa:bb:cc:dd:ee:ff band 5ghz mac-randomization non-persistent count 2 ping example.test http https://example.test forget-after-each true pause 500").request.cycleWifi
        assertTrue(cycle.forgetAfterEach); assertEquals(2, cycle.count); assertEquals(500, cycle.pauseMs)
        assertEquals(WifiBand.WIFI_BAND_5_GHZ, cycle.connect.band)
        assertEquals("example.test", cycle.pingHost)
        assertEquals("https://example.test", cycle.httpUrl)
        assertTrue(live("wifi wait connected require-ip true require-validated false").request.waitWifiConnected.requireIp)
    }

    @Test fun requiredHopsAreEvaluatedFromTypedObservationsNotOutput() {
        val trace = TracerouteResult.newBuilder().setOutput("1 192.0.2.1 1ms\n2 192.0.2.2 2ms")
            .setReachedTarget(true).addHops(TracerouteHop.newBuilder().setIndex(1).addAddresses("192.0.2.1"))
            .addHops(TracerouteHop.newBuilder().setIndex(2).addHostnames("hop.example.test")).build()
        val ok = CommandResult.newBuilder().setStatus(CommandResult.Status.STATUS_OK).setTraceroute(trace).build()
        assertEquals(CommandResult.Status.STATUS_OK, evaluateShellTraceroute(ok, listOf("192.0.2.1", "hop.example.test")).status)
        val missing = evaluateShellTraceroute(ok, listOf("192.0.2.1", "192.0.2.2"))
        assertEquals(CommandResult.Status.STATUS_FAILED, missing.status)
        assertTrue(missing.message.contains("192.0.2.2"))
        assertEquals(trace, missing.traceroute)
        val unobserved = ok.toBuilder().setTraceroute(trace.toBuilder().clearReachedTarget()).build()
        assertEquals("typed hop observations unavailable", evaluateShellTraceroute(unobserved, listOf("192.0.2.1")).message)
        assertEquals(CommandResult.Status.STATUS_FAILED, evaluateShellTraceroute(ok.toBuilder().setStatus(CommandResult.Status.STATUS_FAILED).build(), listOf("192.0.2.2")).status)
    }

    @Test fun rejectsInvalidInputBeforeAnyCommand() {
        for (input in listOf(
            "", // noop is checked separately below
            "show ''", "show wifi s", "s", "show wifi scan ''", "show wifi eht brief",
            "show wifi eht fresh 9000", "show wifi eht timeout 9000", "show wifi scan fresh timeout 0",
            "show wifi scan mlo", "show wifi scan fresh fresh", "show wifi scan band nope",
            "wifi connect Lab passphrase", "wifi connect Lab passphrase ''", "wifi connect Lab passphrase test-only security unknown",
            "wifi connect Lab passphrase test-only mac-randomization unknown", "wifi connect Lab passphrase test-only bssid wrong",
            "wifi wait connected require-ip maybe", "wifi cycle Lab passphrase test-only count 101", "wifi cycle Lab passphrase test-only pause 60001",
            "wifi cycle Lab passphrase test-only http fixture.invalid", "wifi cycle Lab passphrase test-only http ftp://fixture.invalid",
            "ping", "ping example.test count 0", "ping example.test count -1", "ping example.test count 2147483648",
            "ping example.test count 4294967295", "ping example.test count 1073742", "ping example.test count 1 cou 2",
            "ping example.test family all", "ping example.test size 0", "traceroute example.test max-hops 256",
            "path-mtu example.test min-mtu 1500 max-mtu 1280", "global-ip family nonsense",
            "global-ip family auto",
            "dns example.test record B", "http ftp://example.test", "download https://u:secret@example.test",
            "show version\\", "wifi connect Lab passphrase 'test-only", "show wifi status extra",
        ).drop(1)) invalid(input)
        assertEquals(AgentShellCommand.Noop, AgentShellParser.parse(""))
        assertTrue(invalid("show wifi s").contains("status, scan"))
        assertTrue(invalid("s").contains("show, set"))
        assertEquals("trailing escape", invalid("show version\\"))
        assertEquals("unterminated quote", invalid("wifi connect Lab passphrase 'test-only"))
        assertEquals("<command submitted>", redactAgentShellCommandLine("wifi c Lab p test-only security unknown"))
        assertFalse(invalid("wifi connect Lab passphrase test-only extra extra").contains("test-only"))
        for (input in listOf("wifi connect Lab passphrase test-only", "wifi cycle Lab passphrase test-only",
            "wi c Lab pass test-only", "use Lab test-only", "u Lab test-only", "set default passphrase test-only",
            "wifi connect Lab passphrase 'test-only", "wifi connect Lab passphrase test-only\\")) {
            assertFalse(redactAgentShellCommandLine(input).contains("test-only"))
            assertFalse((AgentShellParser.parse(input) as? AgentShellCommand.Invalid)?.message.orEmpty().contains("test-only"))
        }
        invalid("wifi connect Lab passphrase short")
        invalid("set default passphrase short")
    }

    @Test fun reservesUnsupportedCommandsAndHelpUsesSyntaxRows() {
        for (input in listOf("show devices", "check", "show checks", "show check last detail", "clear default passphrase", "adb shell", "show wifi status | json")) {
            assertTrue(invalid(input).contains("unsupported"))
        }
        assertFalse(AgentShellParser.help().joinToString().contains("eht brief"))
        assertTrue(AgentShellParser.help().any { it.contains("show wifi scan detail TARGET") })
        assertEquals(AgentShellCommand.Help(), AgentShellParser.parse("?"))
        assertEquals(AgentShellCommand.Help("ping"), AgentShellParser.parse("h p"))
        assertEquals(RunCommand.CommandCase.PATH_MTU, live("pm example.test").request.commandCase)
        assertEquals(RunCommand.CommandCase.PING, live("pi example.test").request.commandCase) // alias is exact; pi is canonical prefix
    }
}
