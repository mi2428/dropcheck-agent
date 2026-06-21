package io.dropcheck.agent

import io.dropcheck.agent.grpc.RunCommand
import org.junit.Assert.assertEquals
import org.junit.Test

class AgentCommandRegistryTest {
    @Test
    fun advertisesEveryInteractiveCommandExactlyOnce() {
        val advertised = AgentCommandRegistry.entries.map { it.commandCase }
        val dispatchable = RunCommand.CommandCase.values()
            .filterNot { it == RunCommand.CommandCase.COMMAND_NOT_SET || it in removedLegacyCases }

        assertEquals(dispatchable.toSet(), advertised.toSet())
        assertEquals(advertised.size, advertised.toSet().size)
    }

    @Test
    fun capabilitiesAreStableAndUnique() {
        assertEquals(AgentCommandRegistry.entries.map { it.capability }, AgentCommandRegistry.capabilities)
        assertEquals(AgentCommandRegistry.capabilities.size, AgentCommandRegistry.capabilities.toSet().size)
    }

    @Test
    fun doesNotAdvertiseStandaloneCapabilities() {
        assertEquals(emptyList<String>(), AgentCommandRegistry.capabilities.filter { it.startsWith("standalone.") })
    }

    private companion object {
        val removedLegacyCases = setOf(
            RunCommand.CommandCase.EDIT_STANDALONE_CONFIG,
            RunCommand.CommandCase.GET_STANDALONE_CONFIG,
            RunCommand.CommandCase.GET_STANDALONE_STATUS,
            RunCommand.CommandCase.LIST_STANDALONE_RUNS,
            RunCommand.CommandCase.GET_STANDALONE_RUN,
            RunCommand.CommandCase.CLEAR_STANDALONE_RUNS,
            RunCommand.CommandCase.RUN_STANDALONE_ONCE,
        )
    }
}
