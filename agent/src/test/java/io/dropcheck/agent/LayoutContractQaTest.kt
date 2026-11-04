package io.dropcheck.agent

import io.dropcheck.agent.grpc.CommandResult
import io.dropcheck.agent.grpc.IpFamily
import io.dropcheck.agent.grpc.PingResult
import io.dropcheck.agent.grpc.WifiScan
import io.dropcheck.agent.grpc.WifiScanResult
import io.dropcheck.agent.grpc.WifiStatus
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class LayoutContractQaTest {
    @Test fun logicalFieldsKeepColonAtLabelWithOneSpace() {
        val block = AgentPresentationBlock.create(listOf(
            AgentBlockPart.Field("a", "1"), AgentBlockPart.Field("bc", "2"), AgentBlockPart.Field("defg", "3"),
        ), 0)
        assertEquals("a: 1\nbc: 2\ndefg: 3", block.fullText)
        block.parts.filterIsInstance<AgentBlockPart.Field>().forEach { assertEquals(it.label.trimEnd(), it.label) }
    }

    @Test fun summaryDoesNotStorePaddingInsideLabels() {
        val fields = AgentWifiStatusRenderer.summary(WifiStatus.getDefaultInstance()).filterIsInstance<AgentBlockPart.Field>()
        assertTrue(fields.isNotEmpty())
        fields.forEach { assertEquals("Label must not carry pre-colon padding", it.label.trimEnd(), it.label) }
    }

    @Test fun unicodeLogicalRecordsPreserveSafeValuesAndContinuation() {
        val block = AgentPresentationBlock.create(listOf(
            AgentBlockPart.Field("東京", "first\nnext"),
            AgentBlockPart.Field("e\u0301", "0"),
            AgentBlockPart.Field("👩\u200d🔬", "false"),
        ), 0)
        val lines = block.fullText.lines()
        assertEquals(listOf("東京: first", "next", "e\u0301: 0", "👩\u200d🔬: false"), lines.mapIndexed { index, line -> if (index == 1) line.trimStart() else line })
        assertEquals(mapOf("東京" to "first\nnext", "e\u0301" to "0", "👩\u200d🔬" to "false"), block.safeValues().toMap())
        block.parts.filterIsInstance<AgentBlockPart.Field>().forEach { assertEquals(it.label.trimEnd(), it.label) }
    }

    @Test fun tableRecordProjectionHasOuterBlankLines() {
        val table = table()
        val cases = listOf(
            listOf(table) to "\nH: x\nV: 0\n\n",
            listOf(AgentBlockPart.Text("before"), table, AgentBlockPart.Text("after")) to "before\n\nH: x\nV: 0\n\nafter",
            listOf(AgentBlockPart.Text("before\n"), table) to "before\n\nH: x\nV: 0\n\n",
            listOf(AgentBlockPart.Text("before\n\n"), table) to "before\n\nH: x\nV: 0\n\n",
            listOf(table, table) to "\nH: x\nV: 0\n\nH: x\nV: 0\n\n",
        )
        assertEquals(cases.map { it.second }, cases.map { AgentPresentationBlock.create(it.first, 0).fullText })
    }

    @Test fun emptyTableRetainsHeaderAndBoundary() {
        val block = AgentPresentationBlock.create(listOf(table().copy(rows = emptyList())), 0)
        assertEquals("\nH  V\n\n", block.fullText)
    }

    @Test fun scanRecordProjectionHasOuterBlankLines() {
        val scan = AgentBlockPart.Scan(listOf(listOf("東京e\u0301👩\u200d🔬", "02:00:00:00:00:01", "0", "?", "?", "?", "?", "wpa3-eap")))
        val text = AgentPresentationBlock.create(listOf(scan), 0).fullText
        assertTrue("Missing blank above first scan record", text.startsWith("\nSSID:"))
        assertTrue("Missing blank below final scan record", text.endsWith("\n\n"))
    }

    @Test fun tableRecordFiltersPreserveExplicitLineSemantics() {
        for ((stage, expected) in listOf(
            "match .*" to "\nH: x\nV: 0\n\n",
            "find ^H:" to "H: x\nV: 0\n\n",
            "except ^$" to "H: x\nV: 0\n",
        )) {
            val result = AgentShellPipeline.parse(listOf(stage)).apply(AgentShellResult(true, block = AgentPresentationBlock.create(listOf(table()), 0)))
            val filtered = result.block!!
            assertEquals("Explicit filter output: $stage", expected, filtered.fullText)
            assertEquals(listOf(AgentBlockPart.Field("H", "x"), AgentBlockPart.Field("V", "0")), filtered.parts.filterIsInstance<AgentBlockPart.Field>())
            assertTrue(filtered.parts.none { it is AgentBlockPart.Table || it is AgentBlockPart.Scan })
        }
    }

    @Test fun linkReportAdjacentTablesHaveOneSharedBlankLine() {
        val report = LinkReport("link", "東京", IpFamily.IP_FAMILY_IPV6, 0, 1, LinkOutcome.FAIL,
            listOf(LinkStage("ip_before", LinkOutcome.FAIL, "partial: failed", listOf(LinkMetric("addresses", 0)))), validated = false)
        val text = AgentPresentationBlock.create(report.presentation(), 0).fullText
        assertTrue("Adjacent stage/metric tables lack a shared blank", text.contains("REASON: partial: failed\n\nSTAGE:"))
        assertTrue("Final metric row lacks a blank boundary", text.contains("EXPECTED: >=1\n\nNo probe"))
    }

    @Test fun rawAndCopyValuesStaySafeAndLiteral() {
        val raw = "Raw:    untouched:    spacing\nhttps://example.test/a:b\n2001:db8::1"
        val block = AgentPresentationBlock.create(listOf(AgentBlockPart.Text(raw),
            AgentBlockPart.Field("SSID", "password=Guest 東京e\u0301👩\u200d🔬\nforged: row"),
            AgentBlockPart.Field("empty", ""), AgentBlockPart.Field("zero", "0"), AgentBlockPart.Field("flag", "false"),
            AgentBlockPart.Field("unknown", "? (partial: failed)"), AgentBlockPart.Field("control", "x\u0000y\u001B[31m"),
        ), 0)
        assertTrue(block.fullText.startsWith(raw))
        val values = block.safeValues().toMap()
        assertEquals("password=Guest 東京e\u0301👩\u200d🔬 forged: row", values["SSID"])
        assertEquals("", values["empty"])
        assertEquals("0", values["zero"])
        assertEquals("false", values["flag"])
        assertEquals("? (partial: failed)", values["unknown"])
        assertFalse(block.fullText.contains('\u0000'))
        assertFalse(block.fullText.contains('\u001B'))
    }

    @Test fun filteredFieldsKeepLogicalTextAndFieldRoles() {
        val source = AgentPresentationBlock.create(listOf(AgentBlockPart.Field("a", "1"), AgentBlockPart.Field("defg", "0")), 0)
        val filtered = AgentShellPipeline.parse(listOf("match .*")).apply(AgentShellResult(true, block = source)).block!!
        assertEquals("a: 1\ndefg: 0\n", filtered.fullText)
        assertEquals(source.parts, filtered.parts.filterIsInstance<AgentBlockPart.Field>())
        filtered.parts.filterIsInstance<AgentBlockPart.Field>().forEach { assertEquals(it.label.trimEnd(), it.label) }
    }

    @Test fun failedBriefScanRetainsZeroAgeUnknownAndSecurityAlias() {
        val scan = WifiScan.newBuilder().addResults(WifiScanResult.newBuilder().setSsid("東京e\u0301👩\u200d🔬")
            .setBssid("02:00:00:00:00:01").setObservationAgeMs(0).addSecurityTypes("eap_wpa3_enterprise"))
            .addErrors("partial: failed").build()
        val result = CommandResult.newBuilder().setStatus(CommandResult.Status.STATUS_FAILED).setMessage("partial: failed").setWifiScan(scan).build()
        val text = AgentShellTextFormatter.block(result, AgentWifiScanRenderer.presentation(scan, AgentWifiScanContext(brief = true)), 0).fullText
        for (value in listOf("FAILED", "partial: failed", "02:00:00:00:00:01", "AGE(s): 0", "wpa3-eap")) assertTrue(value, text.contains(value))
        assertEquals(0L, scan.resultsList[0].observationAgeMs)
    }

    @Test fun requestMetadataKeepsLogicalRecordsAndFieldRoles() {
        val result = CommandResult.newBuilder().setStatus(CommandResult.Status.STATUS_FAILED).setPing(PingResult.getDefaultInstance()).build()
        val block = AgentShellTextFormatter.block(result, AgentProbeRenderer.renderPing(result.ping, result.status, "partial: failed"), 0)
        val labels = listOf("Latency", "Source", "Observation age", "Received locally")
        val fields = block.parts.filterIsInstance<AgentBlockPart.Field>().filter { it.label in labels }
        assertEquals(labels, fields.map { it.label })
        fields.forEach { field ->
            assertEquals(field.label.trimEnd(), field.label)
            assertTrue(block.fullText.lines().contains("${field.label}: ${field.value}"))
        }
        val filtered = AgentShellPipeline.parse(listOf("match '^(Latency|Source|Observation age|Received locally):'")).apply(AgentShellResult(true, block = block)).block!!
        assertEquals(fields, filtered.parts.filterIsInstance<AgentBlockPart.Field>())
        assertEquals(fields.joinToString("\n", postfix = "\n") { "${it.label}: ${it.value}" }, filtered.fullText)
    }

    @Test fun mloIdentityTableHasBlankBoundaries() {
        val scan = WifiScan.newBuilder().addResults(WifiScanResult.newBuilder().setSsid("Lab").setBssid("02:00:00:00:00:01")
            .setWifiStandard("802.11be").setApMldMacAddress("02:00:00:00:00:02")).build()
        val text = AgentPresentationBlock.create(AgentWifiMloRenderer.presentation(WifiStatus.getDefaultInstance(), scan, AgentWifiMloContext(sdkInt = 37)), 0).fullText
        assertTrue("MLO index lacks a blank above its first record", text.contains("\n\nAP:"))
        assertTrue("MLO index lacks a blank below its last record", text.contains("02:00:00:00:00:01\n\nEHT/MLO analysis"))
    }

    @Test fun helpAndDisplayJsonDoNotReparseRawColonText() {
        val raw = "Raw:    untouched:    spacing\nhttps://example.test/a:b\n2001:db8::1"
        val json = AgentShellPipeline.parse(listOf("display json")).apply(AgentShellResult(false,
            block = AgentPresentationBlock.create(listOf(AgentBlockPart.Text(raw)), 0))).block!!.fullText
        assertEquals("{\"ok\":false,\"lines\":[\"Raw:    untouched:    spacing\",\"https://example.test/a:b\",\"2001:db8::1\"]}", json)
        val help = AgentPresentationBlock.create(AgentShellParser.help("show wifi status").map { AgentBlockPart.Text(it) }, 0).fullText
        assertTrue(help.contains("    Description:"))
        assertTrue(help.contains("history is not written to disk"))
    }

    @Test fun paddedAndEmptyLogicalLabelsKeepSafeValuesAndFieldRoles() {
        val rows = listOf("" to "", "a   " to "0", "東京   " to "false", "e\u0301  " to "? (unknown)",
            "👩\u200d🔬  " to "first\nlast", "LongLabel".repeat(8) to "literal: value")
        val block = AgentPresentationBlock.create(rows.map { (label, value) -> AgentBlockPart.Field(label, value) }, 0)
        val fields = block.parts.filterIsInstance<AgentBlockPart.Field>()
        assertEquals(rows.map { it.first.trimEnd() to it.second }, fields.map { it.label to it.value })
        assertEquals(rows.map { it.first.trimEnd() to it.second }, block.safeValues().toList())
        fields.forEach { field -> assertTrue(block.fullText.lines().contains("${field.label}: ${field.value.substringBefore('\n')}")) }
        assertTrue(block.fullText.lines().any { it.trimStart() == "last" })
        val filtered = AgentShellPipeline.parse(listOf("match '^a:'")).apply(AgentShellResult(true, block = block)).block!!
        assertEquals("a: 0\n", filtered.fullText)
        assertEquals(listOf(AgentBlockPart.Field("a", "0")), filtered.parts.filterIsInstance<AgentBlockPart.Field>())
    }

    private fun table() = AgentBlockPart.Table(listOf(AgentBlockPart.Column("H"), AgentBlockPart.Column("V")), listOf(listOf("x", "0")))
}
