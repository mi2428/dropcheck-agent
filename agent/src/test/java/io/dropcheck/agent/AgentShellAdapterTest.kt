package io.dropcheck.agent

import io.dropcheck.agent.grpc.*
import org.junit.Assert.*
import org.junit.Test

/** Real parser/session/adapter; acquisition is synthetic and never opens a Network. */
class AgentShellAdapterTest {
    private val psk = "synthetic-private-psk"
    private val target = "Current Lab"

    private fun run(adapter: AgentShellAdapter, line: String): AgentShellResult {
        val command = AgentShellParser.parse(line)
        assertFalse("Unexpected parser rejection: $line", command is AgentShellCommand.Invalid)
        return adapter.execute(command)
    }

    @Test fun monitorEventTimestampTypeAndMetadataBelongToTheirEventBlock() {
        val event = WifiEvent.newBuilder().setUnixTimeMs(1234).setType("connected").setMessage("joined")
            .addFields(DiagnosticField.newBuilder().setKey("network_id").setValue("7")).build()
        val request = RunCommand.newBuilder().setMonitorWifi(MonitorWifi.getDefaultInstance()).build()
        val result = CommandResult.newBuilder().setStatus(CommandResult.Status.STATUS_OK)
            .setWifiMonitor(WifiMonitorResult.newBuilder().addEvents(event)).build()
        val block = AgentShellAdapter { result }.execute(AgentShellCommand.Execute(request)).block!!
        val eventTitle = block.parts.indexOf(AgentBlockPart.Text("Event 1", section = true))
        val fields = block.parts.drop(eventTitle + 1).filterIsInstance<AgentBlockPart.Field>().take(3)
        assertEquals(listOf("Event unix ms", "connected", "network_id"), fields.map { it.label })
        assertEquals(listOf("1234", "joined", "7"), fields.map { it.value })
        assertTrue(fields.all { it.indent == 2 })
        assertTrue(block.fullText.contains("Event 1\n  Event unix ms: 1234"))
    }

    @Test fun statusCommandsKeepTheirDistinctRelatedIpScope() {
        val status = WifiStatus.newBuilder().setConnection(WifiConnection.newBuilder().setSsid("Lab").setIpv4Address("192.0.2.4"))
            .setIpStatus(IpStatus.newBuilder().setNetworkId("7")).build()
        val adapter = AgentShellAdapter { request ->
            CommandResult.newBuilder().setStatus(CommandResult.Status.STATUS_OK).apply {
                when (request.commandCase) {
                    RunCommand.CommandCase.GET_WIFI_STATUS -> setWifiStatus(status)
                    RunCommand.CommandCase.GET_WIFI_DIAGNOSTICS -> setWifiDiagnostics(WifiDiagnostics.newBuilder().setStatus(status))
                    RunCommand.CommandCase.MONITOR_WIFI -> setWifiMonitor(WifiMonitorResult.newBuilder().addEvents(WifiEvent.newBuilder().setStatus(status)))
                    else -> error("Unexpected acquisition")
                }
            }.build()
        }
        fun fields(result: AgentShellResult) = result.block!!.parts.filterIsInstance<AgentBlockPart.Field>()
        val connection = fields(run(adapter, "show wifi status connection"))
        assertEquals(listOf("192.0.2.4"), connection.filter { it.label == "Related IP snapshot" }.map { it.value })
        assertFalse(connection.any { it.label == "Network" })
        val diagnostics = fields(run(adapter, "show wifi status full"))
        assertFalse(diagnostics.any { it.label == "Related IP snapshot" })
        assertEquals(listOf("7"), diagnostics.filter { it.label == "Network" }.map { it.value })
        val monitor = fields(adapter.execute(AgentShellCommand.Execute(RunCommand.newBuilder().setMonitorWifi(MonitorWifi.getDefaultInstance()).build())))
        assertEquals(listOf("7"), monitor.filter { it.label == "Network" }.map { it.value })
        assertTrue(monitor.any { it.label == "Related IP snapshot" && it.value == "independently acquired Android Network observation" })
    }

    private fun ip(network: String = "7"): CommandResult = CommandResult.newBuilder().setStatus(CommandResult.Status.STATUS_OK)
        .setIpStatus(IpStatus.newBuilder().setNetworkId(network).setInterfaceName("wlan0").addTransports("wifi")
            .setWifi(WifiConnection.newBuilder().setSsid(target).addAllObservationFields(observationAvailability("identity", true)))
            .addAllObservationFields(observationAvailability("capabilities", true))
            .addAllObservationFields(observationAvailability("link_properties", true))
            .addAddresses("192.0.2.4/24").addRoutes("0.0.0.0/0 -> 192.0.2.1 wlan0").addDnsServers("192.0.2.2")).build()

    @Test fun failedConnectThenReconnectUsesFreshExplicitTargetAndKeepsHistoricalNonPass() {
        for (failedConnect in listOf(true, false)) {
            val session = AgentShellSession()
            val calls = mutableListOf<RunCommand>()
            val adapter = AgentShellAdapter(session) { request ->
                calls += request
                when (request.commandCase) {
                    RunCommand.CommandCase.CONNECT_WIFI -> CommandResult.newBuilder()
                        .setStatus(if (failedConnect) CommandResult.Status.STATUS_FAILED else CommandResult.Status.STATUS_OK)
                        .setConnectWifi(ConnectWifiResult.newBuilder().setSsid("Old Lab").setConnected(!failedConnect)).build()
                    RunCommand.CommandCase.RECONNECT_WIFI -> CommandResult.newBuilder().setStatus(CommandResult.Status.STATUS_OK)
                        .setWifiOperation(WifiOperationResult.newBuilder().setOk(true).setOperation("reconnect")).build()
                    RunCommand.CommandCase.GET_IP_STATUS -> {
                        assertEquals(target, request.getIpStatus.selector.ssid)
                        ip()
                    }
                    else -> error("Unexpected acquisition")
                }
            }
            run(adapter, "set cli default-passphrase $psk")
            // A rejected test must remain historical NON-PASS even after a successful mutation.
            assertFalse(adapter.execute(AgentShellParser.parse("test link")).ok)
            val historical = session.lastTest
            assertEquals(!failedConnect, run(adapter, "request wifi connect 'Old Lab'").ok)
            assertTrue(run(adapter, "request wifi reconnect").ok)
            assertSame(historical, session.lastTest)
            assertTrue(run(adapter, "show test last").block!!.fullText.contains("outcome=NON-PASS"))
            val result = run(adapter, "test link ssid '$target'")
            assertTrue(result.ok)
            assertTrue(result.block!!.fullText.startsWith("Check link PASS"))
            assertSame(result, session.lastTest)
            assertEquals(listOf(RunCommand.CommandCase.CONNECT_WIFI, RunCommand.CommandCase.RECONNECT_WIFI,
                RunCommand.CommandCase.GET_IP_STATUS, RunCommand.CommandCase.GET_IP_STATUS), calls.map { it.commandCase })
            assertEquals(psk, calls.first().connectWifi.passphrase)
            assertTrue(run(adapter, "show test last").block!!.fullText.contains("outcome=PASS"))
        }
    }

    @Test fun freshNetworkChangeIsStillNonPassThroughAdapter() {
        var reads = 0
        val adapter = AgentShellAdapter { request ->
            assertEquals(target, request.getIpStatus.selector.ssid)
            ip(if (++reads == 1) "7" else "8")
        }
        val result = run(adapter, "test link ssid '$target'")
        assertEquals(2, reads)
        assertFalse(result.ok)
        assertTrue(result.block!!.fullText.contains("selected Network/interface changed"))
        assertTrue(run(adapter, "show test last").block!!.fullText.contains("outcome=NON-PASS"))
    }

    @Test fun httpAndDownloadKeepSafeDestinationsAndRedactOnlySecretComponents() {
        for (requestUrl in listOf("https://example.test/health?ordinary=Keep",
            "https://example.test/health?token=synthetic-query-secret&ordinary=Keep")) {
            // Input rejects userinfo; an untrusted collector payload must still redact it.
            val url = if ("token=" in requestUrl) requestUrl.replace("https://", "https://user:synthetic-user-secret@") else requestUrl
            for (download in listOf(false, true)) for (ok in listOf(false, true)) {
                val adapter = AgentShellAdapter { request ->
                    assertEquals(requestUrl, if (download) request.wget.url else request.httpCheck.url)
                    CommandResult.newBuilder().setStatus(if (ok) CommandResult.Status.STATUS_OK else CommandResult.Status.STATUS_FAILED)
                        .setMessage("request $url failed").apply {
                            if (download) setWget(WgetResult.newBuilder().setUrl(url).setStatus(200).setError("request $url"))
                            else setHttpCheck(HttpCheckResult.newBuilder().setUrl(url).setStatus(200).setMatched(ok).setError("request $url"))
                        }.build()
                }
                val result = run(adapter, "request http ${if (download) "download" else "check"} '$requestUrl'")
                assertEquals(ok, result.ok)
                val block = result.block!!
                val shownUrl = block.parts.filterIsInstance<AgentBlockPart.Field>().single { it.label == "URL" }.value
                assertEquals(AgentSafePresentation.text(url), shownUrl)
                assertTrue("example.test/health" in shownUrl && "ordinary=Keep" in shownUrl)
                val json = AgentShellPipeline.parse(listOf("display json")).apply(result).block!!.fullText
                for (text in listOf(block.fullText, block.safeValues().joinToString { it.second }, json)) {
                    assertFalse("synthetic-user-secret" in text)
                    assertFalse("synthetic-query-secret" in text)
                    assertTrue("ordinary=Keep" in text)
                }
            }
        }
    }

    @Test fun connectAndCycleStillReplaceActualPassphraseAndKeepSafeUrlInErrors() {
        val url = "https://example.test/health?token=synthetic-query-secret&ordinary=Keep"
        val session = AgentShellSession()
        val adapter = AgentShellAdapter(session) { request ->
            CommandResult.newBuilder().setStatus(CommandResult.Status.STATUS_FAILED).setMessage("failure $psk $url").apply {
                when (request.commandCase) {
                    RunCommand.CommandCase.CONNECT_WIFI -> setConnectWifi(ConnectWifiResult.newBuilder().setSsid(target).setConnected(false))
                    RunCommand.CommandCase.CYCLE_WIFI -> {
                        assertEquals(psk, request.cycleWifi.connect.passphrase)
                        setWifiCycle(WifiCycleResult.newBuilder().setRequestedCount(1).addErrors("failure $psk $url"))
                    }
                    else -> error("Unexpected acquisition")
                }
            }.build()
        }
        run(adapter, "set cli default-passphrase $psk")
        for (line in listOf("request wifi connect '$target'", "request wifi cycle '$target' passphrase $psk count 1 http '$url'")) {
            val result = run(adapter, line)
            assertFalse(result.ok)
            val text = result.block!!.fullText
            assertFalse(psk in text)
            assertFalse("synthetic-query-secret" in text)
            assertTrue("<redacted>" in text)
            assertTrue("example.test/health" in text && "ordinary=Keep" in text)
        }
    }

    @Test fun localCommandsNeverAcquireAndRawKeepsItsDedicatedPresentation() {
        val local = AgentShellAdapter { error("Local command acquired device state") }
        for (line in listOf("show cli", "show test profiles", "show test last", "help", "set cli default-passphrase $psk", "clear cli default-passphrase")) {
            assertTrue(run(local, line).ok)
        }
        val raw = "native\nTarget: literal password=synthetic-raw-secret"
        val result = CommandResult.newBuilder().setStatus(CommandResult.Status.STATUS_CANCELED).setMessage("partial capture")
            .setWifiStatus(WifiStatus.newBuilder().setConnection(WifiConnection.newBuilder().setRaw(raw).setSsid("structured-only"))).build()
        val adapter = AgentShellAdapter { result }
        val output = run(adapter, "debug android wifi info")
        val expected = AgentRawDebugRenderer.render(AgentShellParser.parse("debug android wifi info") as AgentShellCommand.Execute, result)
        assertEquals(expected.block!!.parts, output.block!!.parts)
        assertFalse(output.ok)
        assertTrue("native\nTarget: literal" in output.block.fullText)
        assertFalse("structured-only" in output.block.fullText)
    }
}
