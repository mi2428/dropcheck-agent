package io.dropcheck.agent

import io.dropcheck.agent.grpc.IpFamily
import io.dropcheck.agent.grpc.DiagnosticCheck
import java.nio.file.Files
import org.junit.Assert.*
import org.junit.Test

class MetadataRetentionTest {
    @Test fun assertionValuesKeepIdentityRolesWithoutExposingMessages() {
        val checks = listOf(
            DiagnosticCheck.newBuilder().setKey("ssid").setExpected("password=Guest Target: literal").setActual("token=Guest\nSource: literal")
                .setPassed(false).setMessage("partial password=qa-synthetic-secret").build(),
            DiagnosticCheck.newBuilder().setKey("password").setExpected("qa-synthetic-secret").setActual("qa-synthetic-secret").build(),
        )
        val block = AgentPresentationBlock.create(AgentWifiStatusRenderer.assertionChecks(checks), 0)
        val fields = block.parts.filterIsInstance<AgentBlockPart.Field>()
        assertEquals(listOf("Check", "Expected ssid", "Actual ssid", "Passed", "Message"), fields.take(5).map { it.label })
        assertEquals("password=Guest Target: literal", fields[1].value)
        assertEquals("token=Guest Source: literal", fields[2].value)
        assertEquals("false", fields[3].value)
        assertEquals("partial password=<redacted>", fields[4].value)
        assertFalse("qa-synthetic-secret" in block.fullText)
        val filtered = AgentShellPipeline.parse(listOf("except '^absent$'")).apply(AgentShellResult(true, block = block)).block!!
        assertEquals(fields, filtered.parts.filterIsInstance<AgentBlockPart.Field>())
        val json = AgentShellPipeline.parse(listOf("display json")).apply(AgentShellResult(false, block = block)).block!!.fullText
        assertTrue("password=Guest Target: literal" in json)
        assertFalse("qa-synthetic-secret" in json)
    }

    @Test fun filteredFieldsKeepProvenanceAndCompleteAvailabilityFacts() {
        val message = AgentBlockPart.Field("Message", "partial\nTarget: literal")
        val availability = AgentBlockPart.Field("Availability", "? (unavailable)", "be\nwire")
        val source = AgentPresentationBlock.create(listOf(AgentBlockPart.Text("Target: self"), message, availability,
            AgentBlockPart.Field("Target", "self"), AgentBlockPart.Text("unrelated")), 0)
        val kept = AgentShellPipeline.parse(listOf("except '^unrelated$'")).apply(AgentShellResult(true, block = source)).block!!
        assertEquals(listOf(message, availability, AgentBlockPart.Field("Target", "self")), kept.parts.filterIsInstance<AgentBlockPart.Field>())
        assertEquals(AgentBlockPart.Text("Target: self"), kept.parts.first())
        val partial = AgentShellPipeline.parse(listOf("except 'wire'")).apply(AgentShellResult(true, block = source)).block!!
        assertFalse(partial.parts.filterIsInstance<AgentBlockPart.Field>().any { it.label == "Availability" })
        assertFalse("wire" in partial.fullText)
        assertEquals(listOf(message), partial.parts.filterIsInstance<AgentBlockPart.Field>().filter { it.label == "Message" })
        val table = AgentPresentationBlock.create(listOf(AgentBlockPart.Table(listOf(AgentBlockPart.Column("VALUE")), listOf(listOf("first\nsecond"))), message), 0)
        val afterTable = AgentShellPipeline.parse(listOf("except '^absent$'")).apply(AgentShellResult(true, block = table)).block!!
        assertEquals(listOf(AgentBlockPart.Field("VALUE", "first\nsecond"), message), afterTable.parts.filterIsInstance<AgentBlockPart.Field>())
    }

    @Test fun overflowSummaryRetainsMetadataWithoutRecreatingFilteredFields() {
        val directory = Files.createTempDirectory("metadata-retention").toFile()
        val payload = "Target: literal Source: literal\n".repeat(10_000)
        val metadata = listOf("Source" to "android", "Target" to "self", "Status" to "canceled", "Message" to "partial acquisition",
            "Latency" to "0ms", "Availability" to "?", "Reason" to "capture interrupted")
        val block = AgentPresentationBlock.create(listOf(AgentBlockPart.Text("Command CANCELED")) + metadata.map { AgentBlockPart.Field(it.first, it.second) } +
            AgentBlockPart.Text(payload), 0)
        val values = AgentSafeValueFile.write(block, directory)
        try {
            val summary = block.overflowSummary(values)
            assertTrue(AgentBlockHistory().fits(summary))
            assertSame(values, summary.safeValueFile)
            assertEquals(metadata, summary.parts.filterIsInstance<AgentBlockPart.Field>().map { it.label to it.value })
            assertEquals(payload, values.value(values.valueCount - 1))
            val filtered = AgentShellPipeline.parse(listOf("except '^Target:'")).apply(AgentShellResult(true, block = block)).block!!
            assertFalse(filtered.overflowSummary(values).parts.filterIsInstance<AgentBlockPart.Field>().any { it.label == "Target" })
            val plain = AgentPresentationBlock.create(listOf(AgentBlockPart.Text("Command CANCELED")) + metadata.map { AgentBlockPart.Text("${it.first}: ${it.second}") }, 0)
            assertTrue(metadata.all { "${it.first}: ${it.second}" in plain.overflowSummary(values).fullText })
        } finally {
            values.delete()
            assertTrue(directory.delete())
        }
    }

    @Test fun linkPresentationDoesNotCombineNamedMetadata() {
        val report = LinkReport("link", "Target: literal", IpFamily.IP_FAMILY_ALL, 0, 1, LinkOutcome.MISSING, emptyList(), validated = false)
        val fields = AgentPresentationBlock.create(report.presentation(), 0).parts.filterIsInstance<AgentBlockPart.Field>().map { it.label to it.value }
        for (field in listOf("SSID" to "Target: literal", "Family" to "IP_FAMILY_ALL", "Started / ended" to "0 / 1", "Source" to "Android selected physical Wi-Fi", "Network" to "?", "Interface" to "?", "Validated (observed)" to "false")) {
            assertTrue(field in fields)
        }
    }
}
