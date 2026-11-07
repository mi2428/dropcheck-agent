package io.dropcheck.agent

import io.dropcheck.agent.grpc.*
import java.nio.file.Files
import org.junit.Assert.*
import org.junit.Test

class MetadataSeparationQaTest {
    private fun fields(block: AgentPresentationBlock) = block.parts.filterIsInstance<AgentBlockPart.Field>()

    private fun assertHeader(block: AgentPresentationBlock) {
        val actual = fields(block).filter { it.label == "Source" || it.label == "Target" }.map { it.label to it.value }
        assertEquals("Generated metadata must have independent typed roles", listOf("Source" to "android", "Target" to "self"), actual)
        assertTrue(block.safeValues().contains("Source" to "android"))
        assertTrue(block.safeValues().contains("Target" to "self"))
    }

    @Test fun blockSourceAndTargetAreIndependent() {
        val defects = listOf(CommandResult.Status.STATUS_OK, CommandResult.Status.STATUS_FAILED, CommandResult.Status.STATUS_CANCELED).mapNotNull { status ->
            val result = CommandResult.newBuilder().setStatus(status).setMessage("partial Target: literal Source: literal").build()
            val block = AgentShellTextFormatter.block(result, emptyList(), 0, "request ip external")
            val metadata = fields(block).filter { it.label in listOf("Source", "Target") }.map { it.label to it.value }
            if (metadata == listOf("Source" to "android", "Target" to "self")) null else "$status: $metadata"
        }
        assertEquals("All typed outcomes require independent headers", emptyList<String>(), defects)
    }

    @Test fun blockSourceAndTargetAreIndependentLines() {
        val lines = AgentShellTextFormatter.block(CommandResult.newBuilder().setStatus(CommandResult.Status.STATUS_OK).build(), emptyList(), 0).fullText.lines()
        assertEquals(listOf("Source: android", "Target: self"), lines.filter { it.startsWith("Source:") || it.startsWith("Target:") })
    }

    @Test fun blockStatusAndMessageAreIndependentLines() {
        val message = "partial Target: literal Source: literal password=qa-synthetic-secret"
        val defects = listOf(CommandResult.Status.STATUS_FAILED, CommandResult.Status.STATUS_CANCELED).mapNotNull { status ->
            val result = CommandResult.newBuilder().setStatus(status).setMessage(message).build()
            val lines = AgentShellTextFormatter.block(result, emptyList(), 0).fullText.lines()
            val actual = lines.filter { it.startsWith("Status:") || it.startsWith("Message:") }
            val expected = listOf("Status: ${status.name.removePrefix("STATUS_").lowercase()}", "Message: ${AgentSafePresentation.text(message)}")
            if (actual == expected) null else "$status: $actual"
        }
        assertEquals("Status and Message must not share a value", emptyList<String>(), defects)
    }

    private fun rawFixtures(): List<Pair<String, CommandResult>> {
        val raw = "RAW Target: literal Source: literal 2001:db8::1 password=qa-synthetic-secret"
        val connection = WifiConnection.newBuilder().setSsid("structured-only").setRaw(raw)
        val network = NetworkDiagnostics.newBuilder().setNetworkId("42").setIpStatus(IpStatus.newBuilder().setNetworkId("42")
            .setRawCapabilities(raw).setRawLinkProperties(raw).setWifi(connection))
        return listOf(
            "debug android wifi info" to CommandResult.newBuilder().setWifiStatus(WifiStatus.newBuilder().setConnection(connection)),
            "debug android wifi scan" to CommandResult.newBuilder().setWifiScan(WifiScan.newBuilder().addResults(WifiScanResult.newBuilder().setRaw(raw))),
            "debug android network capabilities network-id 42" to CommandResult.newBuilder().setNetworkSnapshot(NetworkSnapshot.newBuilder().addNetworks(network)),
            "debug android network link-properties network-id 42" to CommandResult.newBuilder().setNetworkSnapshot(NetworkSnapshot.newBuilder().addNetworks(network)),
            "debug android network wifi-info network-id 42" to CommandResult.newBuilder().setNetworkSnapshot(NetworkSnapshot.newBuilder().addNetworks(network)),
        ).map { (line, builder) -> line to builder.setStatus(CommandResult.Status.STATUS_CANCELED).setMessage("partial capture").build() }
    }

    @Test fun rawDebugSourceAndTargetAreIndependent() {
        val defects = rawFixtures().mapNotNull { (line, result) ->
            val output = AgentRawDebugRenderer.render(AgentShellParser.parse(line) as AgentShellCommand.Execute, result)
            val metadata = fields(output.block!!).filter { it.label in listOf("Source", "Target") }.map { it.label to it.value }
            if (metadata == listOf("Source" to "android", "Target" to "self")) null else "$line: $metadata"
        }
        assertEquals("Each raw-debug producer must retain independent headers", emptyList<String>(), defects)
    }

    @Test fun interactiveJsonLinesKeepIndependentGeneratedMetadata() {
        val result = CommandResult.newBuilder().setStatus(CommandResult.Status.STATUS_FAILED).setMessage("partial capture").build()
        val block = AgentShellTextFormatter.block(result, emptyList(), 0, "request ip external")
        val json = AgentShellPipeline.parse(listOf("display json")).apply(AgentShellResult(false, block = block)).block!!.fullText
        assertTrue(json.startsWith("{\"ok\":false,"))
        assertEquals("JSON lines must retain both independent fields", listOf(true, true),
            listOf("\"Source: android\"" in json, "\"Target: self\"" in json))
    }

    @Test fun exceptSourceDoesNotRemoveTarget() {
        val block = AgentShellTextFormatter.block(CommandResult.newBuilder().setStatus(CommandResult.Status.STATUS_OK).build(), emptyList(), 0)
        val filtered = AgentShellPipeline.parse(listOf("except '^Source:'")).apply(AgentShellResult(true, block = block)).block!!
        assertFalse(fields(filtered).any { it.label == "Source" })
        assertEquals(listOf("self"), fields(filtered).filter { it.label == "Target" }.map { it.value })
    }

    @Test fun exceptTargetDoesNotDamageSourceOrReinsertTarget() {
        val block = AgentShellTextFormatter.block(CommandResult.newBuilder().setStatus(CommandResult.Status.STATUS_OK).build(), emptyList(), 0)
        val filtered = AgentShellPipeline.parse(listOf("except '^Target:'")).apply(AgentShellResult(true, block = block)).block!!
        assertFalse(fields(filtered).any { it.label == "Target" })
        assertEquals(listOf("android"), fields(filtered).filter { it.label == "Source" }.map { it.value })
    }

    @Test fun retainedMultilineMetadataKeepsItsTypedRoleAfterUnrelatedFilter() {
        val message = "partial acquisition\nTarget: literal Source: literal\n2001:db8::1"
        val block = AgentPresentationBlock.create(listOf(AgentBlockPart.Field("Source", "android"), AgentBlockPart.Field("Target", "self"), AgentBlockPart.Field("Message", message)), 0)
        val filtered = AgentShellPipeline.parse(listOf("except '^not-present$'")).apply(AgentShellResult(true, block = block)).block!!
        assertEquals("An unchanged owned field must not become raw Text", listOf(message), fields(filtered).filter { it.label == "Message" }.map { it.value })
        assertEquals(block.fullText, filtered.fullText.removeSuffix("\n"))
    }

    @Test fun explicitMultilineFilteringDoesNotRestoreDeletedLines() {
        val block = AgentPresentationBlock.create(listOf(AgentBlockPart.Field("Message", "keep\nremove-this-line\nkeep-again"), AgentBlockPart.Field("Target", "self")), 0)
        val filtered = AgentShellPipeline.parse(listOf("except 'remove-this-line'")).apply(AgentShellResult(true, block = block)).block!!
        assertFalse("remove-this-line" in filtered.fullText)
        assertTrue("keep-again" in filtered.fullText)
        assertEquals(listOf("self"), fields(filtered).filter { it.label == "Target" }.map { it.value })
    }

    @Test fun literalValuesAndSafeCopyAreNotReparsedAsMetadata() {
        val identity = "password=Guest Target: literal Source: literal " + "東京e\u0301👩\u200d🔬".repeat(20)
        val block = AgentPresentationBlock.create(listOf(
            AgentBlockPart.Field("Source", "android"), AgentBlockPart.Field("Target", "self"), AgentBlockPart.Field("SSID", identity),
            AgentBlockPart.Field("URL", "https://user:qa-synthetic-secret@example.test/Target:literal?token=qa-synthetic-secret&ordinary=Keep"),
            AgentBlockPart.Field("Message", "Source: free-text\nTarget: free-text\u0000\u001b[31m password=qa-synthetic-secret"),
            AgentBlockPart.Field("Status", "0"), AgentBlockPart.Field("Matched", "false"), AgentBlockPart.Field("Availability", "?"),
            AgentBlockPart.Field("IPv6", "2001:db8::1"),
        ), 0)
        assertHeader(block)
        assertEquals(identity, fields(block).single { it.label == "SSID" }.value)
        assertEquals(9, fields(block).size)
        val copied = block.fullText
        for (unsafe in listOf("qa-synthetic-secret", "\u0000", "\u001b")) assertFalse(unsafe in copied)
        for (value in listOf(identity, "2001:db8::1", "0", "false", "?")) {
            assertTrue(block.safeValues().any { it.second == value })
        }
        val filtered = AgentShellPipeline.parse(listOf("except '^not-present$'")).apply(AgentShellResult(true, block = block)).block!!
        assertEquals(identity, fields(filtered).single { it.label == "SSID" }.value)
        assertEquals(listOf("self"), fields(filtered).filter { it.label == "Target" }.map { it.value })
    }

    @Test fun rawOnlyPrivacyAndCanceledOutcomeArePreserved() {
        for ((line, result) in rawFixtures()) {
            val output = AgentRawDebugRenderer.render(AgentShellParser.parse(line) as AgentShellCommand.Execute, result)
            assertFalse(output.ok)
            val text = output.block!!.fullText
            assertTrue("CANCELED" in text)
            assertTrue("RAW Target: literal Source: literal 2001:db8::1" in text)
            assertTrue("partial capture" in text)
            assertFalse("qa-synthetic-secret" in text)
            assertFalse("structured-only" in text)
        }
    }

    @Test fun probeTypedZeroFalseUnknownAndNativeTextRemainDistinct() {
        val ping = PingResult.newBuilder().setHost("2001:db8::1").setTransmitted(0).setReceived(0)
            .setOutput("Target: literal\nSource: literal\npassword=qa-synthetic-secret").build()
        val parts = AgentProbeRenderer.renderPing(ping, CommandResult.Status.STATUS_CANCELED, "partial acquisition")
        val block = AgentPresentationBlock.create(parts, 0)
        assertEquals("0", fields(block).single { it.label == "received" }.value)
        assertEquals("?", fields(block).single { it.label == "packet_loss" }.value)
        assertFalse(fields(block).any { it.label == "Target" || it.label == "Source" })
        assertFalse("qa-synthetic-secret" in block.fullText)
        val trace = AgentPresentationBlock.create(AgentProbeRenderer.renderTraceroute(TracerouteResult.newBuilder().setReachedTarget(false).build(), CommandResult.Status.STATUS_FAILED, "partial"), 0)
        assertEquals("false", fields(trace).single { it.label == "reached_target" }.value)
    }

    @Test fun linkReportKeepsIndependentReasonAndTypedValues() {
        val reason = "partial Target: literal Source: literal password=qa-synthetic-secret"
        val report = LinkReport("link", "Target: literal Source: literal", IpFamily.IP_FAMILY_ALL, 0, 1, LinkOutcome.MISSING,
            listOf(LinkStage("ip_before", LinkOutcome.MISSING, reason, listOf(LinkMetric("zero", 0), LinkMetric("unknown", null)))), validated = false)
        val block = AgentPresentationBlock.create(report.presentation(), 0)
        assertEquals("Target: literal Source: literal", fields(block).single { it.label == "SSID" }.value)
        assertEquals("false", fields(block).single { it.label == "Validated (observed)" }.value)
        val stages = block.parts.filterIsInstance<AgentBlockPart.Table>().first()
        assertEquals(listOf("STAGE", "OUTCOME", "REASON"), stages.columns.map { it.label })
        assertEquals(AgentSafePresentation.text(reason), stages.rows.single()[2])
        assertFalse("qa-synthetic-secret" in block.fullText)
    }

    @Test fun oversizeSafeValuesKeepHeadersAndCompleteSanitizedValues() {
        val longValue = "東京e\u0301👩\u200d🔬 Target: literal Source: literal\n".repeat(10_000)
        val block = AgentPresentationBlock.create(listOf(AgentBlockPart.Field("Source", "android"), AgentBlockPart.Field("Target", "self"),
            AgentBlockPart.Field("Status", "canceled"), AgentBlockPart.Field("Message", "partial acquisition"), AgentBlockPart.Field("Latency", "0ms"), AgentBlockPart.Text(longValue)), 0)
        assertFalse(AgentBlockHistory().fits(block))
        val values = block.safeValues().toList()
        for (value in listOf("Source" to "android", "Target" to "self", "Status" to "canceled", "Message" to "partial acquisition", "Latency" to "0ms")) assertTrue(values.contains(value))
        assertEquals(longValue, values.last().second)
        // MainActivity retention/native layout require instrumentation, not JVM Paint stubs.
    }

    @Test fun indexedPipelinePreservesTheExistingLogicalLineContract() {
        val table = AgentBlockPart.Table(listOf(AgentBlockPart.Column("VALUE")), listOf(listOf("first\nsecond"), listOf("Target: literal")))
        val blocks = listOf(
            emptyList(), listOf(AgentBlockPart.Text("")), listOf(AgentBlockPart.Text("\n")),
            listOf(AgentBlockPart.Text("first\n\n")), listOf(AgentBlockPart.Field("Message", "first\nsecond\n")),
            listOf(AgentBlockPart.Field("Availability", "?", "0\nfalse")), listOf(table),
            listOf(table, AgentBlockPart.Text("\n"), AgentBlockPart.Field("Target", "self")),
            listOf(AgentBlockPart.Text("Target: self"), AgentBlockPart.Field("Target", "self"), AgentBlockPart.Text("Target: self\n\n")),
            listOf(AgentBlockPart.Scan(listOf(listOf("Target: literal", "02:00:00:11:22:33", "0", "?", "0", "0", "?", "false")))),
        ).map { AgentPresentationBlock.create(it, 0) }
        val stages = listOf(emptyList(), listOf("except '^absent$'"), listOf("find 'second'"), listOf("match '^Target:'"),
            listOf("except '^Target:'"), listOf("last 1"), listOf("last 2"), listOf("trim 1"), listOf("trim 2"),
            listOf("count"), listOf("except '^.*$'"), listOf("find 'absent'", "count"), listOf("last 2", "trim 2", "count"))
        for ((index, block) in blocks.withIndex()) for (parts in stages) for (ok in listOf(true, false)) {
            val pipeline = AgentShellPipeline.parse(parts)
            val expected = pipeline.apply(block.fullText) + if (ok) "" else "Command FAILED (output filtered)\n"
            val actual = pipeline.apply(AgentShellResult(ok, block = block))
            assertEquals("block=$index stages=$parts ok=$ok", expected, actual.block!!.fullText)
            assertEquals(ok, actual.ok)
        }
    }

    @Test fun duplicateRawLinesNeverAcquireOwnedFieldRoles() {
        val availability = AgentBlockPart.Field("Availability", "?", "0\nfalse")
        val block = AgentPresentationBlock.create(listOf(
            AgentBlockPart.Text("Target: self"), AgentBlockPart.Field("Target", "self"), AgentBlockPart.Text("Target: self"), availability,
            AgentBlockPart.Table(listOf(AgentBlockPart.Column("Target")), listOf(listOf("self"), listOf("self"))),
            AgentBlockPart.Text("Target: self"),
        ), 0)
        val kept = AgentShellPipeline.parse(listOf("except '^absent$'", "trim 1")).apply(AgentShellResult(true, block = block)).block!!
        assertEquals(listOf(AgentBlockPart.Field("Target", "self"), availability, AgentBlockPart.Field("Target", "self"), AgentBlockPart.Field("Target", "self")), fields(kept))
        assertEquals(3, kept.parts.filterIsInstance<AgentBlockPart.Text>().count { it.value == "Target: self" })
        val partial = AgentShellPipeline.parse(listOf("except '^ +false$'")).apply(AgentShellResult(false, block = block)).block!!
        assertFalse(fields(partial).any { it.label == "Availability" })
        assertFalse("false" in partial.fullText)
        assertEquals(3, fields(partial).count { it.label == "Target" })
    }

    @Test fun jsonKeepsEmptyTrailingAndDuplicateLinesWithoutFailureFooter() {
        val cases = listOf(
            emptyList<AgentBlockPart>() to "[]",
            listOf(AgentBlockPart.Text("\n")) to "[\"\"]",
            listOf(AgentBlockPart.Text("first\n\n")) to "[\"first\",\"\"]",
            listOf(AgentBlockPart.Text("Target: self"), AgentBlockPart.Field("Target", "self")) to "[\"Target: self\",\"Target: self\"]",
            listOf(AgentBlockPart.Field("Message", "first\nsecond")) to "[\"Message: first\",\"         second\"]",
        )
        for ((parts, lines) in cases) for (ok in listOf(true, false)) {
            val block = AgentPresentationBlock.create(parts, 0)
            val output = AgentShellPipeline.parse(listOf("display json")).apply(AgentShellResult(ok, block = block)).block!!
            assertEquals("{\"ok\":$ok,\"lines\":$lines}", output.fullText)
            assertTrue(fields(output).isEmpty())
        }
    }

    private fun withQaSafeValues(block: AgentPresentationBlock, check: (AgentSafeValueFile) -> Unit) {
        val directory = Files.createTempDirectory("metadata-separation-qa").toFile()
        try {
            val values = AgentSafeValueFile.write(block, directory)
            try { check(values) } finally { values.delete() }
        } finally {
            assertTrue("Temporary safe-value fixture was not removed", directory.delete())
        }
    }

    @Test fun overflowSummaryBoundsAFullWireValueEvenWhenTheVisibleValueIsShort() {
        val wire = "Target: literal Source: literal 東京e\u0301👩\u200d🔬\n".repeat(8_000) + "password=qa-synthetic-secret"
        val block = AgentPresentationBlock.create(listOf(AgentBlockPart.Text("Command CANCELED"), AgentBlockPart.Field("Source", "android"),
            AgentBlockPart.Field("Target", "self"), AgentBlockPart.Field("Availability", "? (unavailable)", wire)), 0)
        assertFalse(AgentBlockHistory().fits(block))
        withQaSafeValues(block) { values ->
            val wireIndex = values.labels(0).indexOf("Availability wire value (availability above)")
            assertTrue(wireIndex >= 0)
            assertEquals(AgentSafePresentation.text(wire), values.value(wireIndex))
            assertFalse("qa-synthetic-secret" in values.value(wireIndex))
            val summary = block.overflowSummary(values)
            assertSame(values, summary.safeValueFile)
            assertEquals(listOf("android", "self", "? (unavailable)"), fields(summary).map { it.value })
            val history = AgentBlockHistory()
            val failure = runCatching { history.append(summary) }.exceptionOrNull()
            assertTrue("Short visible value left an oversized summary: chars=${summary.fullText.length}; append=${failure?.message}", history.fits(summary) && failure == null)
        }
    }

    @Test fun overflowSummaryBoundsAnOversizedFirstTableWithoutLosingItsSafeValues() {
        val payload = "Target: literal Source: literal 東京e\u0301👩\u200d🔬\n".repeat(8_000)
        val block = AgentPresentationBlock.create(listOf(
            AgentBlockPart.Table(listOf(AgentBlockPart.Column("VALUE")), listOf(listOf(payload))),
            AgentBlockPart.Field("Source", "android"), AgentBlockPart.Field("Target", "self"),
        ), 0)
        assertFalse(AgentBlockHistory().fits(block))
        withQaSafeValues(block) { values ->
            assertEquals(payload, values.value(0))
            val summary = block.overflowSummary(values)
            assertSame(values, summary.safeValueFile)
            assertEquals(listOf("android", "self"), fields(summary).map { it.value })
            val history = AgentBlockHistory()
            val failure = runCatching { history.append(summary) }.exceptionOrNull()
            assertTrue("First table bypassed the overflow display budget: chars=${summary.fullText.length}; append=${failure?.message}", history.fits(summary) && failure == null)
        }
    }

    @Test fun assertionComparisonRolesStripExactlyOnePrefixAndKeepLiteralIdentities() {
        val checks = listOf(
            DiagnosticCheck.newBuilder().setKey("SsId").setExpected("password=Guest\nTarget: literal").setActual("token=Guest\nSource: literal")
                .setPassed(false).setMessage("password=qa-synthetic-secret").build(),
            DiagnosticCheck.newBuilder().setKey("TOKEN").setExpected("qa-synthetic-secret").setActual("qa-synthetic-secret").build(),
            DiagnosticCheck.newBuilder().setKey("unknown").setExpected("password=qa-synthetic-secret").setActual("token=qa-synthetic-secret").build(),
        )
        val block = AgentPresentationBlock.create(AgentWifiStatusRenderer.assertionChecks(checks), 0)
        val facts = fields(block)
        assertEquals(15, facts.size)
        assertEquals(15, block.parts.size)
        checks.forEachIndexed { index, check ->
            assertEquals(listOf("Check", "Expected ${check.key}", "Actual ${check.key}", "Passed", "Message"), facts.drop(index * 5).take(5).map { it.label })
        }
        assertEquals("password=Guest Target: literal", facts[1].value)
        assertEquals("token=Guest Source: literal", facts[2].value)
        assertEquals("false", facts[3].value)
        assertEquals("password=<redacted>", facts[4].value)
        assertEquals(listOf("<redacted>", "<redacted>"), facts.slice(6..7).map { it.value })
        assertEquals(listOf("password=<redacted>", "token=<redacted>"), facts.slice(11..12).map { it.value })
        assertEquals("token=Guest", AgentSafePresentation.field("aCtUaL bSsId", "token=Guest"))
        for (label in listOf("Expected Expected ssid", "Actual Expected ssid", "ExpectedSSID", "Expected Message")) {
            assertEquals(label, "password=<redacted>", AgentSafePresentation.field(label, "password=qa-synthetic-secret"))
        }
        val filtered = AgentShellPipeline.parse(listOf("except '^absent$'")).apply(AgentShellResult(false, block = block))
        assertFalse(filtered.ok)
        assertEquals(facts, fields(filtered.block!!))
        assertEquals("token=Guest Source: literal", filtered.block.safeValues().first { it.first == "Actual SsId" }.second)
        val json = AgentShellPipeline.parse(listOf("display json")).apply(AgentShellResult(false, block = block)).block!!.fullText
        assertTrue(json.startsWith("{\"ok\":false,"))
        assertTrue("\"Expected SsId: password=Guest Target: literal\"" in json)
        assertTrue("\"Actual SsId: token=Guest Source: literal\"" in json)
        for (text in listOf(block.fullText, filtered.block.fullText, json)) assertFalse("qa-synthetic-secret" in text)
    }
}
