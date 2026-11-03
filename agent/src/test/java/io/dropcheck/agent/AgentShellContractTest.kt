package io.dropcheck.agent

import io.dropcheck.agent.grpc.GetNetwork
import org.junit.Assert.*
import org.junit.Test

class AgentShellContractTest {
    private fun rows(name: String) = checkNotNull(javaClass.getResourceAsStream("/$name")).bufferedReader().use { it.readLines() }.drop(1).map { it.split('\t') }

    @Test fun helpSeparatesCommandSyntaxFromDescriptions() {
        for (topic in listOf("", "show version")) {
            val lines = AgentShellParser.help(topic)
            val index = lines.indexOf("  show version")
            assertTrue("syntax has a description on the same line", index >= 0)
            assertEquals("    Description: Show local application version", lines[index + 1])
            assertEquals("", lines[index + 2])
        }
        val candidates = AgentShellParser.contextHelp("show wifi status ")
        assertTrue("  <Enter>  - Execute the requested subject" in candidates)
        assertTrue("  eht  - Show focused EHT/MLO connection, AP and device evidence" in candidates)
        assertEquals(listOf("  <value>"), AgentShellParser.contextHelp("request wifi connect Lab passphrase "))
    }

    @Test fun portableInventoryHasExecutableCases() {
        val cases = rows("command_cases.tsv")
        assertEquals(AgentShellContract.rows.map { it.path }.toSet(), cases.map { it[0] }.toSet())
        cases.forEach { row -> assertFalse("${row[0]}: ${AgentShellParser.parse(row[1])}", AgentShellParser.parse(row[1]) is AgentShellCommand.Invalid) }
        rows("invalid_cases.tsv").forEach { row -> assertTrue(row[0], AgentShellParser.parse(row[0]) is AgentShellCommand.Invalid) }
    }

    @Test fun wifiStatusUsesLightweightAcquisition() {
        val status = AgentShellParser.parse("show wifi status") as AgentShellCommand.Execute
        val full = AgentShellParser.parse("sh wi st f") as AgentShellCommand.Execute
        val connection = AgentShellParser.parse("show wifi status connection") as AgentShellCommand.Execute
        assertEquals(status.request, connection.request)
        assertTrue(status.request.hasGetWifiStatus())
        assertTrue(full.request.hasGetWifiDiagnostics())
        assertTrue(status.summary)
        assertFalse(connection.summary)
        assertFalse(full.summary)
        assertFalse(AgentShellParser.historySafe("show wifi status"))
        assertFalse(AgentShellParser.historySafe("show wifi status full"))
        assertTrue("show wifi status full" in AgentShellParser.complete("show wifi status "))
        assertTrue("  full  - Show all passive Wi-Fi diagnostics including safe raw dumps" in AgentShellParser.contextHelp("show wifi status "))
        for (line in listOf("show wifi status full full", "show wifi status full ssid Lab", "show wifi status full connection")) {
            assertTrue(line, AgentShellParser.parse(line) is AgentShellCommand.Invalid)
        }
    }

    @Test fun portablePipelinesMatchGoFixtures() {
        rows("pipeline_cases.tsv").forEach { row ->
            val parsed = AgentShellParser.parse("show version | ${row[0]}")
            if (row[3] == "false") assertTrue(row[0], parsed is AgentShellCommand.Invalid)
            else {
                assertTrue(row[0], parsed is AgentShellCommand.Piped)
                assertEquals(row[0], row[2].replace("\\n", "\n"), (parsed as AgentShellCommand.Piped).pipeline.apply(row[1].replace("\\n", "\n")))
            }
        }
    }

    @Test fun contextHistoryAndSessionBoundaries() {
        assertEquals(listOf("show"), AgentShellParser.complete("sho"))
        assertTrue(AgentShellParser.complete("show ").contains("show wifi"))
        assertTrue(AgentShellParser.complete("show wifi status | ").contains("show wifi status | find"))
        assertTrue(AgentShellParser.complete("show wifi scan ").contains("show wifi scan ssid"))
        assertFalse(shellHelpPosition("request wifi connect 'literal"))
        assertFalse(shellHelpPosition("show wifi status\\"))
        assertFalse(AgentShellParser.contextHelp("request wifi connect Lab passphrase synthetic-private-psk ").joinToString().contains("synthetic-private-psk"))
        assertEquals(listOf("  <value>"), AgentShellParser.contextHelp("request wifi connect Lab passphrase synthetic-private-psk"))
        val session = AgentShellSession()
        session.setPassphrase("test-only")
        session.remember("set cli default-passphrase test-only")
        session.remember("request wifi connect Lab passphrase test-only")
        session.remember("request http https://example.test/?token=test-only")
        session.remember("show network | match private-value")
        session.remember("show network")
        assertEquals("show network", session.recall("show permissions", -1))
        assertEquals("show network", session.recall("", -1))
        assertEquals("show permissions", session.recall("", 1))
        assertEquals("", AgentShellSession().defaults.defaultPassphrase)
        session.setPassphrase("")
        assertEquals("", session.defaults.defaultPassphrase)
    }

    @Test fun filteredAndJsonOutputsPreserveTypedSafeIdentityAndFailure() {
        val source = AgentShellResult(false, block = AgentPresentationBlock.create(listOf(AgentBlockPart.Field("SSID", "password=Guest")), 0))
        val result = AgentShellPipeline.parse(listOf("display json")).apply(source)
        assertFalse(result.ok)
        assertEquals("{\"ok\":false,\"lines\":[\"SSID: password=Guest\"]}", result.block!!.withStyle(10f, 10f).fullText)
        val none = AgentShellPipeline.parse(listOf("match absent")).apply(source)
        assertFalse(none.ok)
        assertEquals("Command FAILED (output filtered)\n", none.block!!.fullText)
    }

    @Test fun explicitNetworkNeverFallsBack() {
        assertEquals(listOf("1", "2"), snapshotNetworkIds(listOf("2", "1"), "2", null))
        assertEquals(listOf("2"), snapshotNetworkIds(listOf("1", "2"), "2", GetNetwork.newBuilder().setDefaultNetwork(true).build()))
        listOf(GetNetwork.getDefaultInstance(), GetNetwork.newBuilder().setNetworkId("3").build(),
            GetNetwork.newBuilder().setNetworkId("01").build(), GetNetwork.newBuilder().setNetworkId("1").setDefaultNetwork(true).build()).forEach {
            assertTrue(runCatching { snapshotNetworkIds(listOf("1", "2"), "2", it) }.isFailure)
        }
        assertTrue(runCatching { snapshotNetworkIds(listOf("1"), null, GetNetwork.newBuilder().setDefaultNetwork(true).build()) }.isFailure)
    }
}
