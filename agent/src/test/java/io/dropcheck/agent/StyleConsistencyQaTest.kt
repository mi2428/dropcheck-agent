package io.dropcheck.agent

import io.dropcheck.agent.grpc.*
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class StyleConsistencyQaTest {
    private val identity = "password=Guest 東京e\u0301👩\u200d🔬".repeat(4)
    private fun fields(parts: List<AgentBlockPart>) = parts.filterIsInstance<AgentBlockPart.Field>()
    private fun requireField(parts: List<AgentBlockPart>, label: String): AgentBlockPart.Field {
        val matches = fields(parts).filter { it.label.trim() == label }
        assertEquals("Generated scalar/composite $label must retain a measured Field role", 1, matches.size)
        return matches.single()
    }

    @Test fun fullStatusDetailKeepsScalarFieldRoles() {
        val status = WifiStatus.newBuilder().setConnection(WifiConnection.newBuilder().setSsid(identity).setDetailedState("CONNECTED")).build()
        val parts = AgentWifiStatusRenderer.presentation(status)
        assertEquals("0", requireField(parts, "configuration_id").value)
        assertEquals("false", requireField(parts, "hidden").value)
    }

    @Test fun networkWifiDetailKeepsScalarFieldRoles() {
        val ip = IpStatus.newBuilder().setWifi(WifiConnection.newBuilder().setSsid(identity)).build()
        val parts = AgentWifiStatusRenderer.ipPresentation(ip)
        requireField(parts, "ssid")
        requireField(parts, "frequency")
    }

    @Test fun capabilitiesMetadataUsesMeasuredFields() {
        val parts = AgentWifiCapabilitiesRenderer.presentation(WifiCapabilities.newBuilder().addErrors("partial: key=0")
            .addFields(DiagnosticField.newBuilder().setKey("cap.field").setValue("yes")).build())
        assertEquals("device", requireField(parts, "Scope").value)
        requireField(parts, "Error")
        assertEquals("yes", requireField(parts, "cap.field").value)
    }

    @Test fun diagnosticsMetadataUsesMeasuredFields() {
        val parts = AgentWifiDiagnosticsRenderer.presentation(WifiDiagnostics.getDefaultInstance())
        requireField(parts, "Constituent status")
        assertTrue("Missing payload notes remain metadata, not headings", fields(parts).any { it.label == "Note" })
    }

    @Test fun mloCompositeUsesOwnedParentLabel() {
        val connection = WifiConnection.newBuilder().setSsid(identity)
            .setHeCapabilities(WifiHeCapabilities.newBuilder().setPpeThresholdsPresent(true).setPpeThresholdsHex("00"))
            .setHeUoraParameterSet(WifiHeUoraParameterSet.getDefaultInstance()).build()
        val parts = AgentWifiMloRenderer.presentation(WifiStatus.newBuilder().setEnabled(true).setConnection(connection).build(), WifiScan.getDefaultInstance(), AgentWifiMloContext(sdkInt = 35))
        assertEquals("nss=0 ru=<none> hex=0x00", requireField(parts, "he_ppe").value)
        assertEquals("eocw_min=0 eocw_max=0", requireField(parts, "he_uora").value)
    }

    @Test fun mloWarningsHaveFieldOrBulletListRole() {
        val parts = AgentWifiMloRenderer.interpretation(WifiStatus.getDefaultInstance(), WifiScan.getDefaultInstance(), AgentWifiMloContext(sdkInt = 35))
        val warnings = parts.dropWhile { it != AgentBlockPart.Text("Diagnostics / Warnings", section = true) }.drop(1)
        for (token in listOf("wifi_disabled", "eht_scan_results=0")) {
            assertTrue("Generated warning $token needs a Field or consistent bullet list", warnings.any { part ->
                when (part) {
                    is AgentBlockPart.Field -> token in part.value ||
                        part.label.trim() == "eht_scan_results" && part.value == "0" && token == "eht_scan_results=0" ||
                        part.label.trim() == "wifi_disabled" && part.value in listOf("true", "yes") && token == "wifi_disabled"
                    is AgentBlockPart.Text -> part.value.trimStart().let { (it.startsWith("- ") || it.startsWith("* ")) && token in it }
                    else -> false
                }
            })
        }
    }

    @Test fun heOperationColoringFixtureKeepsOwnedCompleteValues() {
        val flags = listOf("future_" + "東京e\u0301👩\u200d🔬".repeat(20), "tail_flag")
        val operation = WifiHeOperation.newBuilder().setBasicMcsNssSetHex("0000").addAllFlags(flags).setTruncated(true)
            .addWarnings("decode: key=0 password=qa-style-secret").build()
        val connection = WifiConnection.newBuilder().setSsid(identity).setHeOperation(operation)
            .setHeSpatialReuseParameterSet(WifiHeSpatialReuseParameterSet.getDefaultInstance())
            .addObservationFields(DiagnosticField.newBuilder().setKey("identity.state").setValue("available")).build()
        val parts = AgentWifiMloRenderer.presentation(WifiStatus.newBuilder().setEnabled(true).setConnection(connection).build(), WifiScan.getDefaultInstance(), AgentWifiMloContext(sdkInt = 35))
        val block = AgentPresentationBlock.create(parts, 0)
        for ((label, expected) in listOf(
            "he_oper" to "params=0x0 basic_mcs_nss=0x0000 bss_color=0 disabled=false",
            "he_oper flags" to flags.joinToString(","), "he_oper_truncated" to "true",
            "he_oper_warnings" to "decode: key=0 password=<redacted>",
            "he_operation" to "bss_color=0 disabled=false partial=false",
            "he_operation_warnings" to "decode: key=0 password=<redacted>", "spatial_reuse" to "control=0x0 flags=<none>",
        )) {
            val field = requireField(block.parts, label)
            assertEquals("    $label", field.label)
            assertEquals(expected, field.value)
            assertTrue(block.fullText.contains("${field.label}: $expected"))
            assertTrue(block.safeValues().any { it.first == field.label && it.second == expected })
        }
        assertEquals(identity, requireField(block.parts, "SSID").value)
        assertFalse(block.fullText.contains("qa-style-secret"))
    }

    @Test fun ehtOperationPuncturingFixtureKeepsOwnedCompleteValues() {
        val flags = listOf("future_" + "東京e\u0301👩\u200d🔬".repeat(20), "tail_flag")
        val operation = WifiEhtOperation.newBuilder().setDisabledSubchannelBitmapPresent(true).setDisabledSubchannelBitmapHex("0000")
            .addAllFlags(flags).addWarnings("decode: key=0 password=qa-style-secret").build()
        val connection = WifiConnection.newBuilder().setSsid(identity).setEhtOperation(operation)
            .setHeCapabilities(WifiHeCapabilities.newBuilder().setMac(WifiHeMacCapabilities.getDefaultInstance()))
            .addObservationFields(DiagnosticField.newBuilder().setKey("identity.state").setValue("available")).build()
        val parts = AgentWifiMloRenderer.presentation(WifiStatus.newBuilder().setEnabled(true).setConnection(connection).build(), WifiScan.getDefaultInstance(), AgentWifiMloContext(sdkInt = 35))
        val block = AgentPresentationBlock.create(parts, 0)
        for ((label, expected) in listOf(
            "oper" to "op_info=false width=<unknown> width_mhz=0 code=0",
            "oper channels" to "ccfs0=0 ccfs1=0 mcs15_disabled=false group_bu_exp=0", "oper flags" to flags.joinToString(","),
            "oper disabled" to "disabled=0x0000 punctured=<none>", "oper_warnings" to "decode: key=0 password=<redacted>",
            "he_preamble_puncturing_rx" to "<unknown>", "he_punctured_sounding" to "false",
            "eht_disabled_subchannel_bitmap" to "0x0000 punctured=<none>",
        )) {
            val field = requireField(block.parts, label)
            assertEquals("    $label", field.label)
            assertEquals(expected, field.value)
            assertTrue(block.fullText.contains("${field.label}: $expected"))
            assertTrue(block.safeValues().any { it.first == field.label && it.second == expected })
        }
        assertEquals(identity, requireField(block.parts, "SSID").value)
        assertFalse(block.fullText.contains("qa-style-secret"))
    }

    @Test fun checkHeadingHasNoScalarColon() {
        for (outcome in LinkOutcome.entries) {
            val parts = LinkReport("link", identity, IpFamily.IP_FAMILY_ALL, 0, 0, outcome, emptyList()).presentation()
            val heading = (parts.first() as AgentBlockPart.Text).value
            assertTrue(heading.startsWith("Check"))
            assertFalse("Check is a heading, not a scalar label", heading.startsWith("Check:"))
        }
    }

    @Test fun richMloConnectedScanKeepNamedMultilineFieldsAndTables() {
        val elements = listOf(
            Triple(255, 107, "3000090200000000010207001672090c020000000002640001030500034c6162ff036afe080102ff046c010203dd060011220799aa"),
            Triple(201, 0, "001085050aaabbccddeeff112233448015073210"),
            Triple(71, 0, "02003400034c61625502040353023412ff05380130016a301e0100000fac090100000fac090200000fac18000fac19c0000000000fac0c"),
            Triple(70, 0, "0000000000"),
        ).map { (id, extension, hex) -> WifiInformationElement.newBuilder().setId(id).setIdExt(extension).setBytesHex(hex).setByteCount(hex.length / 2).build() }
        val metadata = listOf("identity.state", "ap_mlo_link_id.state").map { DiagnosticField.newBuilder().setKey(it).setValue("available").build() }
        val security = WifiSecurityDetails.newBuilder().setRsnPresent(true).setRsnVersion(1).addWarnings("decode: key=0 password=qa-rich-secret").build()
        val connection = WifiConnection.newBuilder().setSsid(identity).setBssid("02:00:00:00:00:02").setApMldMacAddress("02:00:00:00:00:01")
            .setWifiStandard("802.11be").addAllInformationElements(elements).setSecurityDetails(security).addAllObservationFields(metadata).setRaw("RAW-ONLY-STYLE-MARKER").build()
        val ap = WifiScanResult.newBuilder().setSsid(identity).setBssid(connection.bssid).setApMldMacAddress(connection.apMldMacAddress)
            .setWifiStandard("802.11be").addAllInformationElements(elements).setSecurityDetails(security).addAllObservationFields(metadata).build()
        val status = WifiStatus.newBuilder().setEnabled(true).setConnection(connection).build()
        val scan = WifiScan.newBuilder().addResults(ap).build()
        val block = AgentPresentationBlock.create(AgentWifiMloRenderer.presentation(status, scan, AgentWifiMloContext(sdkInt = 35)), 0)
        for ((label, count) in mapOf("Security" to 2, "Roaming" to 2, "Elements" to 2, "RNR" to 1, "Multiple BSSID" to 1, "Identity" to 1, "Radio" to 1, "Information elements" to 1, "SDK flags" to 1)) {
            val matches = fields(block.parts).filter { it.label.trim() == label }
            assertEquals("Named $label role", count, matches.size)
            matches.forEach { field ->
                assertTrue(block.fullText.contains("${field.label}: "))
                assertTrue(block.safeValues().any { it.first == field.label && it.second == field.value })
                if (label in listOf("Security", "Elements", "RNR", "Multiple BSSID")) assertTrue("Complete decoded multiline $label value", '\n' in field.value)
            }
        }
        assertTrue(block.parts.any { it is AgentBlockPart.Table })
        assertEquals(identity, requireField(block.parts, "SSID").value)
        assertEquals("none", requireField(block.parts, "Warnings").value)
        assertFalse(block.fullText.contains("qa-rich-secret"))
        assertFalse(block.fullText.contains("RAW-ONLY-STYLE-MARKER"))
        val filtered = AgentShellPipeline.parse(listOf("except '^[ ]*Security:'")).apply(AgentShellResult(true, block = block)).block!!
        assertFalse(filtered.fullText.lines().any { it.trimStart().startsWith("Security:") })
        // Logical filtering projects the two identity tables into owned records too.
        assertEquals(listOf(identity, identity, identity), fields(filtered.parts).filter { it.label == "SSID" }.map { it.value })
        assertFalse(filtered.fullText.contains("qa-rich-secret"))
    }

    @Test fun stringProjectionUsesTheSameScalarColonContract() {
        // Source-only impact: production adapters use presentation/interpretation, not render.
        val lines = AgentWifiMloRenderer.render(WifiStatus.getDefaultInstance(), WifiScan.getDefaultInstance(), AgentWifiMloContext(sdkInt = 35))
        assertTrue("String projection must not remove the source Field's colon", lines.any { it.startsWith("  source:") })
    }

    @Test fun logicalCopyFilterJsonAndHistoryPreserveOwnedValues() {
        val block = AgentPresentationBlock.create(listOf(
            AgentBlockPart.Text("Heading"), AgentBlockPart.Field("A", "0"), AgentBlockPart.Field("長い項目", "false"),
            AgentBlockPart.Field("SSID", identity), AgentBlockPart.Field("IPv6", "2001:db8::1"),
            AgentBlockPart.Field("URL", "https://user:qa-synthetic-secret@example.test/a:b?token=qa-synthetic-secret&ordinary=x=y"),
            AgentBlockPart.Field("Message", "keep\nremove-this-line\nSource: literal"), AgentBlockPart.Field("Target", "acquired"),
        ), 0)
        assertEquals(identity, block.safeValues().first { it.first == "SSID" }.second)
        assertTrue(block.fullText.contains("長い項目: false"))
        assertFalse(block.fullText.contains("qa-synthetic-secret"))
        assertTrue(block.fullText.contains("2001:db8::1"))
        assertTrue(block.fullText.contains("ordinary=x=y"))
        val source = AgentShellResult(false, block = block)
        val filtered = AgentShellPipeline.parse(listOf("except 'remove-this-line|^Target:'")).apply(source).block!!
        assertFalse(filtered.fullText.contains("remove-this-line"))
        assertFalse(filtered.fullText.contains("Target:"))
        assertTrue(filtered.fullText.contains("Source: literal"))
        assertTrue(filtered.fullText.contains("Command FAILED (output filtered)"))
        val json = AgentShellPipeline.parse(listOf("display json")).apply(source).block!!.fullText
        assertTrue(json.startsWith("{\"ok\":false,\"lines\":["))
        assertTrue(json.contains("SSID: $identity"))
        val history = AgentBlockHistory(maxBlocks = 2)
        history.append(block)
        history.append(filtered)
        assertEquals(block.fullText, history.blocks.first().fullText)
        assertEquals(filtered.fullText, history.blocks.last().fullText)
    }

    @Test fun scanTableAgeAndIdentityRemainTyped() {
        val scan = WifiScan.newBuilder().addResults(WifiScanResult.newBuilder().setSsid(identity).setBssid("02:00:00:00:00:01").setObservationAgeMs(0))
            .addResults(WifiScanResult.newBuilder().setSsid("東京e\u0301👩\u200d🔬").setObservationAgeMs(12500)).build()
        val parts = AgentWifiScanRenderer.presentation(scan, AgentWifiScanContext(brief = true))
        val table = parts.filterIsInstance<AgentBlockPart.Scan>().single()
        assertEquals(AgentBlockPart.Column("AGE(s)", numeric = false), table.columns.last())
        assertEquals(listOf("0", "12.5"), table.rows.map { it.last() })
        val block = AgentPresentationBlock.create(parts, 0)
        assertTrue(block.safeValues().any { it.second == identity })
        assertTrue(block.fullText.contains("SSID: $identity"))
        assertEquals("?", AgentWifiScanRenderer.ageSeconds(WifiScanResult.getDefaultInstance()))
        // JVM verifies typed/logical roles only; Paint/pixel alignment needs instrumentation.
    }

    @Test fun probeNativeReferenceAndOptionalZeroRemainDistinct() {
        for (status in listOf(CommandResult.Status.STATUS_OK, CommandResult.Status.STATUS_FAILED, CommandResult.Status.STATUS_CANCELED)) {
            val parts = AgentProbeRenderer.renderPing(PingResult.newBuilder().setHost("2001:db8::1").setTransmitted(0).setOutput("native key=0: reference").build(), status, "partial")
            assertEquals("0", requireField(parts, "transmitted").value)
            assertEquals("?", requireField(parts, "received").value)
            assertTrue(parts.contains(AgentBlockPart.Text("native key=0: reference")))
            val result = CommandResult.newBuilder().setStatus(status).setMessage("partial\nreason: key=0").build()
            val block = AgentShellTextFormatter.block(result, parts, 0)
            assertEquals("android", requireField(block.parts, "Source").value)
            assertEquals("self", requireField(block.parts, "Target").value)
            assertTrue((block.parts.first() as AgentBlockPart.Text).value.endsWith(status.name.removePrefix("STATUS_")))
        }
    }

    @Test fun summaryHierarchyAndAvailabilityRemainTyped() {
        val parts = AgentWifiStatusRenderer.summary(WifiStatus.newBuilder().setConnection(WifiConnection.newBuilder().setSsid(identity)).build())
        assertTrue(parts.contains(AgentBlockPart.Text("  Identity", section = true)))
        assertTrue(fields(parts).any { it.label == "    SSID" })
        val available = listOf(DiagnosticField.newBuilder().setKey("field.state").setValue("available").build())
        assertEquals("0", AgentObservationPresentation.value(available, "field", "0"))
        assertEquals("no", AgentObservationPresentation.bool(available, "field", false))
        assertEquals("none", AgentObservationPresentation.value(available, "field", ""))
        assertTrue(AgentObservationPresentation.value(emptyList(), "field", "0").startsWith("?"))
    }

    @Test fun rawDebugRetainsReferenceSyntaxAndMetadataRoles() {
        val command = AgentShellParser.parse("debug android wifi info") as AgentShellCommand.Execute
        val result = CommandResult.newBuilder().setStatus(CommandResult.Status.STATUS_OK)
            .setWifiStatus(WifiStatus.newBuilder().setConnection(WifiConnection.newBuilder().setRaw("native key=0: 2001:db8::1 password=qa-synthetic-secret"))).build()
        val block = AgentRawDebugRenderer.render(command, result).block!!
        assertEquals("android", requireField(block.parts, "Source").value)
        assertEquals("self", requireField(block.parts, "Target").value)
        assertTrue(block.fullText.contains("native key=0: 2001:db8::1"))
        assertFalse(block.fullText.contains("qa-synthetic-secret"))
    }
}
