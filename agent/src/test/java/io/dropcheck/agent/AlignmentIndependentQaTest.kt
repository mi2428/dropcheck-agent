package io.dropcheck.agent

import io.dropcheck.agent.grpc.*
import org.junit.Assert.*
import org.junit.Test

class AlignmentIndependentQaTest {
    @Test fun diagnosticIdentityKeepsCredentialLookingSsid() {
        val ssid = "password=Guest"
        val block = AgentPresentationBlock.create(listOf(AgentBlockPart.Field("ssid", ssid)), 0)
        assertTrue("A typed diagnostic identity was reinterpreted as a credential", block.fullText.contains(ssid))
    }
}
