package io.dropcheck.agent

import io.dropcheck.agent.grpc.GetNetwork
import org.junit.Assert.*
import org.junit.Test

class AgentShellContractTest {
    private fun rows(name: String) = checkNotNull(javaClass.getResourceAsStream("/$name")).bufferedReader().use { it.readLines() }.drop(1).map { it.split('\t') }

    @Test fun portableInventoryHasExecutableCases() {
        val cases = rows("command_cases.tsv")
        assertEquals(AgentShellContract.rows.map { it.path }.toSet(), cases.map { it[0] }.toSet())
        cases.forEach { row -> assertFalse("${row[0]}: ${AgentShellParser.parse(row[1])}", AgentShellParser.parse(row[1]) is AgentShellCommand.Invalid) }
        rows("invalid_cases.tsv").forEach { row -> assertTrue(row[0], AgentShellParser.parse(row[0]) is AgentShellCommand.Invalid) }
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
        assertTrue(AgentShellParser.complete("show wifi scan ").contains("show wifi scan detail"))
        assertFalse(shellHelpPosition("wifi connect 'literal"))
        assertFalse(shellHelpPosition("show wifi status\\"))
        assertFalse(AgentShellParser.contextHelp("wifi connect Lab passphrase synthetic-private-psk ").joinToString().contains("synthetic-private-psk"))
        assertEquals(listOf("  <value>"), AgentShellParser.contextHelp("wifi connect Lab passphrase synthetic-private-psk"))
        val session = AgentShellSession()
        session.setPassphrase("test-only")
        session.remember("set default passphrase test-only")
        session.remember("wifi connect Lab passphrase test-only")
        session.remember("http https://example.test/?token=test-only")
        session.remember("show networks | match private-value")
        session.remember("show networks")
        assertEquals("show networks", session.recall("show permissions", -1))
        assertEquals("show networks", session.recall("", -1))
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
