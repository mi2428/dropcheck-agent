package io.dropcheck.agent

import java.io.IOException
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder

class AgentSafeValueFileTest {
    @get:Rule val temporary = TemporaryFolder()

    @Test fun oversizeValuesStayExactAndPagesAreBounded() {
        val block = AgentPresentationBlock.create(listOf(
            AgentBlockPart.Field("SSID", "password=Guest 東京 e\u0301 👩\u200d🔬 Case"),
            AgentBlockPart.Field("IPv6", "ffff:ffff:ffff:ffff:ffff:ffff:255.255.255.255%long-interface-name/128"),
            AgentBlockPart.Table(listOf(AgentBlockPart.Column("Data")), List(1024) { listOf("safe-$it " + "x".repeat(512)) }),
        ), 0)
        assertTrue(!AgentBlockHistory().fits(block))
        val values = AgentSafeValueFile.write(block, temporary.root)
        assertEquals("password=Guest 東京 e\u0301 👩\u200d🔬 Case", values.value(0))
        assertTrue(values.value(1).endsWith("%long-interface-name/128"))
        assertEquals(25, values.labels(1000).size)
        assertTrue(values.value(1025).startsWith("safe-1023 "))
        values.delete()
    }

    @Test fun evictionRemovesTheWholeSafeValueBackingFile() {
        val values = AgentSafeValueFile.write(AgentPresentationBlock.create(listOf(AgentBlockPart.Field("SSID", "Case")), 0), temporary.root)
        val history = AgentBlockHistory(maxBlocks = 1)
        history.append(AgentPresentationBlock.create(listOf(AgentBlockPart.Text("older result")), 0, safeValueFile = values))
        history.append(AgentPresentationBlock.create(listOf(AgentBlockPart.Text("current result")), 0))
        assertEquals(1, history.evictedBlocks)
        try {
            values.value(0)
            throw AssertionError("evicted values were retained")
        } catch (_: IOException) { /* Explicitly expired result, not a clipped copy. */ }
    }

    @Test fun quotaFailureDeletesPartialFileRatherThanLeavingUnsafeUncopyableData() {
        val block = AgentPresentationBlock.create(listOf(AgentBlockPart.Field("Data", "x".repeat(AgentSafeValueFile.MAX_BYTES.toInt()))), 0)
        try {
            AgentSafeValueFile.write(block, temporary.root)
            throw AssertionError("quota accepted")
        } catch (failure: IOException) {
            assertTrue(failure.message.orEmpty().contains("quota"))
            assertFalse(temporary.root.listFiles().orEmpty().any { it.name.startsWith("shell-safe-values-") })
        }
    }
}
