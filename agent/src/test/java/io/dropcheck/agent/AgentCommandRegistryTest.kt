package io.dropcheck.agent

import io.dropcheck.agent.grpc.RunCommand
import org.junit.Assert.assertEquals
import org.junit.Test

class AgentCommandRegistryTest {
    @Test
    fun advertisesEveryDispatchableCommandExactlyOnce() {
        val advertised = AgentCommandRegistry.entries.map { it.commandCase }
        val dispatchable = RunCommand.CommandCase.values()
            .filterNot { it == RunCommand.CommandCase.COMMAND_NOT_SET }

        assertEquals(dispatchable.toSet(), advertised.toSet())
        assertEquals(advertised.size, advertised.toSet().size)
    }

    @Test
    fun capabilitiesAreStableAndUnique() {
        assertEquals(AgentCommandRegistry.entries.map { it.capability }, AgentCommandRegistry.capabilities)
        assertEquals(AgentCommandRegistry.capabilities.size, AgentCommandRegistry.capabilities.toSet().size)
    }
}
