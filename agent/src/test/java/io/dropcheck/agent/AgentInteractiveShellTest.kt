package io.dropcheck.agent

import io.dropcheck.agent.grpc.*
import org.junit.Assert.*
import org.junit.Test

class AgentInteractiveShellTest {
    // The legacy E2E argv corpus belongs to the independent PC one-shot interface.
    // These shared interactive fixtures verify the replacement's actual wire intent.
    private val wireCases = mapOf(
        "show network" to RunCommand.CommandCase.GET_NETWORKS,
        "show network default" to RunCommand.CommandCase.GET_NETWORK,
        "show network id" to RunCommand.CommandCase.GET_NETWORK,
        "show permissions" to RunCommand.CommandCase.GET_PERMISSIONS,
        "show wifi status" to RunCommand.CommandCase.GET_WIFI_STATUS,
        "show wifi status full" to RunCommand.CommandCase.GET_WIFI_DIAGNOSTICS,
        "show wifi status connection" to RunCommand.CommandCase.GET_WIFI_STATUS,
        "show wifi status ip" to RunCommand.CommandCase.GET_IP_STATUS,
        "show wifi status capabilities" to RunCommand.CommandCase.GET_WIFI_CAPABILITIES,
        "show wifi scan" to RunCommand.CommandCase.GET_WIFI_SCAN,
        "request wifi scan" to RunCommand.CommandCase.GET_FRESH_WIFI_SCAN,
        "request wifi connect" to RunCommand.CommandCase.CONNECT_WIFI,
        "request wifi disconnect" to RunCommand.CommandCase.DISCONNECT_WIFI,
        "request wifi reconnect" to RunCommand.CommandCase.RECONNECT_WIFI,
        "request wifi forget ssid" to RunCommand.CommandCase.FORGET_WIFI,
        "request wifi forget config-id" to RunCommand.CommandCase.FORGET_WIFI,
        "request wifi cycle" to RunCommand.CommandCase.CYCLE_WIFI,
        "test wifi" to RunCommand.CommandCase.ASSERT_WIFI,
        "monitor wifi" to RunCommand.CommandCase.MONITOR_WIFI,
        "request ping" to RunCommand.CommandCase.PING,
        "request traceroute" to RunCommand.CommandCase.TRACEROUTE,
        "request path-mtu" to RunCommand.CommandCase.PATH_MTU,
        "request global-ip" to RunCommand.CommandCase.GLOBAL_IP,
        "request dns" to RunCommand.CommandCase.RESOLVE_DNS,
        "request http" to RunCommand.CommandCase.HTTP_CHECK,
        "request download" to RunCommand.CommandCase.WGET,
    )

    @Test fun controllerCanonicalCommandsMatchAndroidWireCases() {
        val matrix = checkNotNull(javaClass.getResourceAsStream("/command_cases.tsv")).bufferedReader().use { it.readLines() }
        val checked = mutableSetOf<String>()
        matrix.drop(1).forEach { line ->
            val row = line.split('\t')
            wireCases[row[0]]?.let { expected ->
                assertEquals(row[0], expected, live(row[1]).request.commandCase)
                checked += row[0]
            }
        }
        assertEquals(wireCases.keys, checked)
    }

    private fun live(input: String): AgentShellCommand.Execute = when (val parsed = AgentShellParser.parse(input)) {
        is AgentShellCommand.Execute -> parsed
        // Test-only extraction of a deferred connection's already validated explicit options.
        is AgentShellCommand.Connect -> AgentShellCommand.Execute(RunCommand.newBuilder().setConnectWifi(parsed.options).build())
        else -> error("$input: $parsed")
    }

    private fun invalid(input: String): String {
        val parsed = AgentShellParser.parse(input)
        assertTrue("$input: $parsed", parsed is AgentShellCommand.Invalid)
        return (parsed as AgentShellCommand.Invalid).message
    }

    @Test fun registryInventoryAndEhtComposite() {
        val rows = checkNotNull(javaClass.getResourceAsStream("/command_cases.tsv")).bufferedReader().use { it.readLines() }.drop(1).map { it.split('\t') }
        val wired = rows.filter { it[0] in wireCases }.map { live(it[1]).request.commandCase }.toSet()
        val rpcOnly = setOf(RunCommand.CommandCase.WAIT_WIFI_CONNECTED, RunCommand.CommandCase.GET_WIFI_SCAN_DETAIL)
        assertEquals(AgentCommandRegistry.entries.map { it.commandCase }.toSet() - rpcOnly, wired)
        assertTrue("wifi.forget.typed" in AgentCommandRegistry.capabilities)
        assertEquals(AgentShellCommand.ShowWifiEht(), AgentShellParser.parse("show wifi status eht"))
        assertEquals(AgentShellCommand.ShowWifiEht(fresh = true, timeoutMs = 9000, band = WifiBand.WIFI_BAND_6_GHZ),
            AgentShellParser.parse("request wifi scan eht timeout 9000 band 6ghz"))
        assertEquals(AgentShellCommand.ShowVersion, AgentShellParser.parse("show version"))
        assertEquals(AgentShellCommand.ShowCli, AgentShellParser.parse("show cli"))
    }

    @Test fun builderDefaultsAreExplicitAndSelectNetwork() {
        assertEquals(10000, live("request wifi scan").request.getFreshWifiScan.timeoutMs)
        assertEquals(WifiBand.WIFI_BAND_ALL, live("show wifi scan").request.getWifiScan.band)
        assertEquals(45000, live("request wifi connect Lab passphrase test-only").request.connectWifi.timeoutMs)
        assertEquals(30000, live("test wifi timeout 30000").request.assertWifi.timeoutMs)
        assertEquals(0, live("test wifi").request.assertWifi.timeoutMs)
        assertFalse(live("test wifi").request.assertWifi.requireIp)
        assertEquals(10000, live("monitor wifi").request.monitorWifi.durationMs)
        assertEquals(1000, live("monitor wifi").request.monitorWifi.intervalMs)
        assertEquals(30000, live("request wifi reconnect").request.reconnectWifi.timeoutMs)
        assertEquals(3, live("request wifi cycle Lab passphrase test-only").request.cycleWifi.count)
        assertEquals(1000, live("request wifi cycle Lab passphrase test-only").request.cycleWifi.pauseMs)
        val ping = live("request ping example.test").request.ping
        assertEquals(3, ping.count); assertEquals(9000, ping.timeoutMs); assertEquals(0, ping.sizeBytes)
        assertEquals(IpFamily.IP_FAMILY_UNSPECIFIED, ping.family)
        assertEquals(30, live("request traceroute example.test").request.traceroute.maxHops)
        assertEquals(60000, live("request traceroute example.test").request.traceroute.timeoutMs)
        assertEquals(30000, live("request path-mtu example.test").request.pathMtu.timeoutMs)
        assertEquals(0, live("request path-mtu example.test").request.pathMtu.minMtuBytes)
        assertEquals(5000, live("request global-ip").request.globalIp.timeoutMs)
        assertEquals(IpFamily.IP_FAMILY_ALL, live("request global-ip").request.globalIp.family)
        assertEquals(Int.MAX_VALUE, live("request ping example.test timeout 2147483647").request.ping.timeoutMs)
        assertEquals(5000, live("request dns example.test").request.resolveDns.timeoutMs)
        assertEquals(5000, live("request http example.test").request.httpCheck.timeoutMs)
        assertEquals(200, live("request http example.test").request.httpCheck.expectedStatus)
        assertEquals("https://example.test", live("request http example.test").request.httpCheck.url)
        assertEquals(60000, live("request download https://example.test").request.wget.timeoutMs)
        assertEquals(listOf(DnsRecordType.DNS_RECORD_TYPE_A, DnsRecordType.DNS_RECORD_TYPE_AAAA), live("request dns example.test").request.resolveDns.qtypesList)
        assertEquals("Lab", live("request ping example.test ssid Lab").request.ping.selector.ssid)
        assertEquals("Lab", live("show wifi status ip ssid Lab").request.getIpStatus.selector.ssid)
        val selector = live("request ping example.test bssid aa:bb:cc:dd:ee:ff network-id 949").request.ping.selector
        assertEquals("aa:bb:cc:dd:ee:ff", selector.bssid); assertEquals("949", selector.networkId)
    }

    @Test fun literalsQuotingSwitchesAndRepeatingVia() {
        assertEquals("count", live("req pi count count 3").request.ping.host)
        assertEquals(3, live("req pi count count 3").request.ping.count)
        val connect = live("request wifi connect 'scan | NAME' passphrase '  MiXeD \\\\ A  '").request.connectWifi
        assertEquals("scan | NAME", connect.ssid); assertEquals("  MiXeD \\ A  ", connect.passphrase)
        assertEquals("scan", live("request wifi connect scan passphrase test-only").request.connectWifi.ssid)
        assertEquals(listOf("192.0.2.1", "2001:db8::1"), live("req tr example.test via 192.0.2.1 via 2001:db8::1").via)
        assertTrue(live("request wifi scan mlo band 6ghz").mlo)
        assertEquals(WifiBand.WIFI_BAND_6_GHZ, live("request wifi scan brief mlo band 6ghz").request.getFreshWifiScan.band)
        assertEquals("lab\\path", live("request wifi connect \"lab\\\\path\" passphrase test-only").request.connectWifi.ssid)
        assertEquals("lab\"scan", live("request wifi connect \"lab\\\"scan\" passphrase test-only").request.connectWifi.ssid)
        for (ssid in listOf("a|b", " ", "|", "123")) {
            assertEquals(ssid, live("request wifi connect '$ssid' passphrase test-only").request.connectWifi.ssid)
            val forget = live("request wifi forget ssid '$ssid'").request.forgetWifi
            assertEquals(ForgetWifi.SelectorCase.SSID, forget.selectorCase)
            assertEquals(ssid, forget.ssid); assertEquals("", forget.target)
        }
        assertEquals(ForgetWifi.SelectorCase.CONFIG_ID, live("request wifi forget config-id 0").request.forgetWifi.selectorCase)
        invalid("set cli default-passphrase ''")
        assertEquals(listOf("show", "version"), shellSplitWords("show version").getOrThrow())
    }

    @Test fun familiesEnumsAndControlOptions() {
        assertEquals(IpFamily.IP_FAMILY_UNSPECIFIED, live("req path-mtu example.test family auto").request.pathMtu.family)
        assertEquals(IpFamily.IP_FAMILY_ALL, live("request global-ip family all").request.globalIp.family)
        assertEquals(2, live("request dns example.test record ALL").request.resolveDns.qtypesCount)
        assertEquals(DnsRecordType.DNS_RECORD_TYPE_AAAA, live("request dns example.test record AAAA").request.resolveDns.qtypesList.single())
        val cycle = live("request wifi cycle Lab passphrase test-only security wpa3 bssid aa:bb:cc:dd:ee:ff band 5ghz mac-randomization non-persistent count 2 ping example.test http https://example.test forget-after-each true pause 500").request.cycleWifi
        assertTrue(cycle.forgetAfterEach); assertEquals(2, cycle.count); assertEquals(500, cycle.pauseMs)
        assertEquals(WifiBand.WIFI_BAND_5_GHZ, cycle.connect.band)
        assertEquals("example.test", cycle.pingHost); assertEquals("https://example.test", cycle.httpUrl)
        assertTrue(live("test wifi timeout 30000 require-ip true require-validated false").request.assertWifi.requireIp)
    }

    @Test fun requiredHopsAreEvaluatedFromTypedObservationsNotOutput() {
        val trace = TracerouteResult.newBuilder().setOutput("1 192.0.2.1 1ms\n2 192.0.2.2 2ms")
            .setReachedTarget(true).addHops(TracerouteHop.newBuilder().setIndex(1).addAddresses("192.0.2.1"))
            .addHops(TracerouteHop.newBuilder().setIndex(2).addHostnames("hop.example.test")).build()
        val ok = CommandResult.newBuilder().setStatus(CommandResult.Status.STATUS_OK).setTraceroute(trace).build()
        assertEquals(CommandResult.Status.STATUS_OK, evaluateShellTraceroute(ok, listOf("192.0.2.1", "hop.example.test")).status)
        val missing = evaluateShellTraceroute(ok, listOf("192.0.2.1", "192.0.2.2"))
        assertEquals(CommandResult.Status.STATUS_FAILED, missing.status)
        assertTrue(missing.message.contains("192.0.2.2")); assertEquals(trace, missing.traceroute)
        val unobserved = ok.toBuilder().setTraceroute(trace.toBuilder().clearReachedTarget()).build()
        assertEquals("typed hop observations unavailable", evaluateShellTraceroute(unobserved, listOf("192.0.2.1")).message)
        assertEquals(CommandResult.Status.STATUS_FAILED, evaluateShellTraceroute(ok.toBuilder().setStatus(CommandResult.Status.STATUS_FAILED).build(), listOf("192.0.2.2")).status)
    }

    @Test fun rejectsInvalidInputBeforeAnyCommand() {
        for (input in listOf("show ''", "show wifi s", "s", "show wifi scan ''", "show wifi status eht brief",
            "show wifi status eht timeout 9000", "request wifi scan timeout 0", "request wifi scan brief brief", "show wifi scan band nope",
            "request wifi connect Lab passphrase", "request wifi connect Lab passphrase ''", "request wifi connect Lab passphrase test-only security unknown",
            "request wifi connect Lab passphrase test-only mac-randomization unknown", "request wifi connect Lab passphrase test-only bssid wrong",
            "test wifi require-ip maybe", "request wifi cycle Lab passphrase test-only count 101", "request wifi cycle Lab passphrase test-only pause 60001",
            "request wifi cycle Lab passphrase test-only http fixture.invalid", "request wifi cycle Lab passphrase test-only http ftp://fixture.invalid",
            "request ping", "request ping example.test count 0", "request ping example.test count -1", "request ping example.test count 2147483648",
            "request ping example.test count 4294967295", "request ping example.test count 1073742", "request ping example.test count 1 cou 2",
            "request ping example.test family all", "request ping example.test size 0", "request traceroute example.test max-hops 256",
            "request path-mtu example.test min-mtu 1500 max-mtu 1280", "request global-ip family nonsense", "request global-ip family auto",
            "request dns example.test record B", "request http ftp://example.test", "request download https://u:secret@example.test",
            "show version\\", "request wifi connect Lab passphrase 'test-only", "show wifi status extra")) invalid(input)
        assertEquals(AgentShellCommand.Noop, AgentShellParser.parse(""))
        assertTrue(invalid("show wifi s").contains("status, scan")); assertTrue(invalid("s").contains("show, set"))
        assertEquals("trailing escape", invalid("show version\\"))
        assertEquals("unterminated quote", invalid("request wifi connect Lab passphrase 'test-only"))
        for (input in listOf("request wifi connect Lab passphrase test-only", "request wifi cycle Lab passphrase test-only",
            "req wi c Lab pass test-only", "use Lab test-only", "set cli default-passphrase test-only",
            "request wifi connect Lab passphrase 'test-only", "request wifi connect Lab passphrase test-only\\")) {
            assertFalse(redactAgentShellCommandLine(input).contains("test-only"))
            assertFalse((AgentShellParser.parse(input) as? AgentShellCommand.Invalid)?.message.orEmpty().contains("test-only"))
        }
        invalid("request wifi connect Lab passphrase short"); invalid("set cli default-passphrase short")
    }

    @Test fun reservesUnsupportedCommandsAndHelpUsesSyntaxRows() {
        for (input in listOf("show devices", "adb shell", "show wifi status | json", "use Lab", "p example.test", "pm example.test", "wifi disconnect", "check link ssid Lab")) invalid(input)
        assertTrue(AgentShellParser.help().any { it.contains("show wifi scan [brief]") })
        assertFalse(AgentShellParser.help().any { it.contains("show devices") })
        assertEquals(AgentShellCommand.Help(), AgentShellParser.parse("?"))
        assertEquals(AgentShellCommand.Help("request wifi"), AgentShellParser.parse("h req wi"))
        assertEquals(RunCommand.CommandCase.PATH_MTU, live("req path-mtu example.test").request.commandCase)
        assertEquals(RunCommand.CommandCase.PING, live("req pi example.test").request.commandCase)
        invalid("req p example.test")
    }

    @Test fun portableCheckGrammarIsExplicitAndZeroOperationCommandsAreDistinct() {
        assertEquals(AgentShellCommand.Help("test"), AgentShellParser.parse("test"))
        assertEquals(AgentShellCommand.ShowChecks, AgentShellParser.parse("show test profiles"))
        assertEquals(AgentShellCommand.ShowCheckLast, AgentShellParser.parse("show test last"))
        assertEquals(AgentShellCommand.ClearDefaultPassphrase, AgentShellParser.parse("clear cli default-passphrase"))
        assertEquals(AgentShellCommand.Check("link", " Lab "), AgentShellParser.parse("test link ssid ' Lab '"))
        assertEquals(AgentShellCommand.Check("link", "\"Lab\""), AgentShellParser.parse("test link ssid '\"Lab\"'"))
        assertEquals(" ", live("show wifi status ip ssid ' '").request.getIpStatus.selector.ssid)
        assertEquals(AgentShellCommand.Check("link", "Lab", IpFamily.IP_FAMILY_IPV6, "aa:bb:cc:dd:ee:ff"), AgentShellParser.parse("test link family ipv6 bssid aa:bb:cc:dd:ee:ff ssid Lab"))
        val configured = AgentShellParser.parse("test lab ssid Lab family ipv4 gateway 192.0.2.1 dns lab.example.test resolver selected http https://lab.example.test/health expected-status 204 timeout 5000") as AgentShellCommand.Check
        assertEquals(LabConfig("192.0.2.1", "lab.example.test", "selected", "https://lab.example.test/health", 204, 5000), configured.lab)
        for (input in listOf("test link ssid ' '", "test lab", "test lab ssid Lab", "test lab ssid Lab family ipv4 gateway example.test dns lab.example.test resolver selected http https://lab.example.test/health expected-status 204 timeout 5000",
            "test lab ssid Lab family ipv4 gateway 192.0.2 dns lab.example.test resolver selected http https://lab.example.test/health expected-status 204 timeout 5000",
            "test lab ssid Lab family ipv4 gateway 192.0.2.1 dns lab.example.test resolver selected http https://lab.example.test/?token=synthetic-private expected-status 204 timeout 5000",
            "test LINK", "test link", "test link ssid", "test link ssid Lab family auto", "test link ssid Lab bssid invalid", "test link ssid Lab family ipv4 family ipv6", "show test profiles extra", "clear cli default-passphrase extra")) invalid(input)
        assertTrue(AgentShellParser.help().any { it.contains("test link defaults to family ipv4") })
        assertEquals("<command submitted>", redactAgentShellCommandLine("clear cli default-passphrase"))
    }
}
