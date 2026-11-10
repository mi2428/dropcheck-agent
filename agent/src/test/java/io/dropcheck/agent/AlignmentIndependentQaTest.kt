package io.dropcheck.agent

import io.dropcheck.agent.grpc.*
import org.junit.Assert.*
import org.junit.Test

class AlignmentIndependentQaTest {
    @Test fun tracerouteCopyAndJsonRetainTypedObservationFailure() {
        val trace = TracerouteResult.newBuilder().setHost("2001:db8::1").setReachedTarget(false)
            .setObservationError("independent typed observation unavailable").setExitCode(73).build()
        val block = AgentShellTextFormatter.block(CommandResult.newBuilder().setStatus(CommandResult.Status.STATUS_FAILED).build(),
            AgentProbeRenderer.renderTraceroute(trace, CommandResult.Status.STATUS_FAILED, "partial acquisition"), 0)
        val json = AgentShellPipeline.parse(listOf("display json")).apply(AgentShellResult(false, block = block)).block!!.fullText
        val missing = mapOf("Copy" to block.fullText, "JSON" to json, "Inspect" to block.safeValues().joinToString { it.second })
            .filterValues { !it.contains(trace.observationError) }.keys
        assertTrue("Typed observation failure missing from $missing", missing.isEmpty())
    }

    @Test fun tracerouteDistinguishesUnknownFromObservedFalse() {
        fun text(trace: TracerouteResult) = AgentPresentationBlock.create(
            AgentProbeRenderer.renderTraceroute(trace, CommandResult.Status.STATUS_FAILED, "partial acquisition"), 0).fullText
        val unknown = TracerouteResult.newBuilder().setHost("2001:db8::1").build()
        assertNotEquals("Missing reached_target became indistinguishable from false", text(unknown), text(unknown.toBuilder().setReachedTarget(false).build()))
    }

    @Test fun diagnosticIdentityKeepsCredentialLookingSsid() {
        val ssid = "password=Guest"
        val block = AgentPresentationBlock.create(listOf(AgentBlockPart.Field("ssid", ssid)), 0)
        assertTrue("A typed diagnostic identity was reinterpreted as a credential", block.fullText.contains(ssid))
    }

    @Test fun scanCopyInspectAndPipesRetainAllLongIdentitiesAndExactAges() {
        val identity = "password=Guest-日本語e\u0301👩\u200d🔬-" + "long".repeat(30)
        val ages = listOf(0L, 1L, 999L, 1000L, 1001L, 9007199254740993L, -1L)
        val scan = WifiScan.newBuilder().apply {
            repeat(27) { index -> addResults(WifiScanResult.newBuilder().setSsid("$identity-$index")
                .setBssid("02:00:00:11:22:%02x".format(index)).setObservationAgeMs(ages[index % ages.size])
                .addSecurityTypes("eap_wpa3_enterprise").addSecurityTypes("future_Mode").setRssiDbm(0)) }
            addErrors("partial acquisition\nsecond line\u001b[31m\u0000")
        }.build()
        val block = AgentShellTextFormatter.block(CommandResult.newBuilder().setStatus(CommandResult.Status.STATUS_FAILED).build(),
            AgentWifiScanRenderer.presentation(scan, AgentWifiScanContext(brief = true)), 0)
        val values = block.safeValues().map { it.second }.toList()
        for (index in 0 until 27) {
            assertTrue(block.fullText.contains("$identity-$index"))
            assertTrue(values.contains("$identity-$index"))
        }
        assertTrue(block.fullText.contains("wpa3-eap+future_Mode"))
        assertTrue(block.fullText.contains("9007199254740.993"))
        assertTrue(block.fullText.contains("18446744073709551.615"))
        val filtered = AgentShellPipeline.parse(listOf("match '^SSID:'", "last 1")).apply(AgentShellResult(false, block = block))
        assertFalse(filtered.ok)
        assertTrue(filtered.block!!.fullText.contains("$identity-9"))
        val json = AgentShellPipeline.parse(listOf("display json")).apply(AgentShellResult(false, block = block)).block!!.fullText
        assertTrue(json.startsWith("{\"ok\":false,"))
        assertTrue(json.contains("$identity-26"))
        assertFalse(json.contains('\u001b'))
        assertFalse(json.contains('\u0000'))
    }

    @Test fun literalSsidFilterRunsBeforeDisplaySanitization() {
        val aps = listOf("Guest\t東京", "Guest 東京").mapIndexed { index, ssid ->
            WifiScanResult.newBuilder().setSsid(ssid).setBssid("02:00:00:11:22:0${index + 1}").setWifiStandard("802.11be").build()
        }
        aps.forEach { ap ->
            val selected = AgentWifiMloRenderer.MloFilter(ap.ssid, "").scanResults(aps)
            assertEquals(listOf(ap.bssid), selected.map { it.bssid })
            val block = AgentPresentationBlock.create(AgentWifiScanRenderer.presentation(WifiScan.newBuilder().addAllResults(selected).build()), 0)
            assertTrue(block.fullText.contains(ap.bssid))
            assertFalse(block.fullText.contains(aps.first { it != ap }.bssid))
        }
    }

    @Test fun mloReportAndHelpKeepLongSafeValuesWithoutNativeMeasurement() {
        val identity = "password=Guest-日本語e\u0301👩\u200d🔬-" + "long".repeat(30)
        val marker = "証拠e\u0301👩\u200d🔬-" + "reference".repeat(20)
        val scan = WifiScan.newBuilder().addResults(WifiScanResult.newBuilder().setSsid(identity).setBssid("02:00:00:11:22:33")
            .setWifiStandard("802.11be").setBand(marker).addSecurityTypes("eap_wpa3_enterprise")).build()
        for (brief in listOf(false, true)) {
            val block = AgentPresentationBlock.create(AgentWifiMloRenderer.presentation(WifiStatus.getDefaultInstance(), scan,
                AgentWifiMloContext(brief = brief, sdkInt = 37)), 0)
            for (value in listOf(identity, marker, "02:00:00:11:22:33")) assertTrue(block.fullText.contains(value))
        }
        val report = LinkReport("link", identity, IpFamily.IP_FAMILY_ALL, 0, 0, LinkOutcome.MISSING,
            listOf(LinkStage(marker, LinkOutcome.MISSING, "partial acquisition", listOf(LinkMetric("dns", null), LinkMetric("route", 0)))), validated = false)
        val block = AgentPresentationBlock.create(report.presentation(), 0)
        for (value in listOf(identity, marker, "MISSING", "ACTUAL: 0", "Validated (observed): false")) assertTrue(block.fullText.contains(value))
        val help = AgentPresentationBlock.create(AgentShellParser.help().map { AgentBlockPart.Text(it) }, 0)
        assertTrue(help.fullText.contains("display json"))
        assertTrue(AgentShellPipeline.parse(listOf("match 'Description:'", "count")).apply(AgentShellResult(true, block = help)).block!!.fullText.startsWith("Count: "))
    }

    @Test fun mloMetadataKeepsTypedFieldsForBothNativeEntryPoints() {
        fun field(key: String, value: String) = DiagnosticField.newBuilder().setKey(key).setValue(value).build()
        val status = WifiStatus.newBuilder().setEnabled(true).setConnection(WifiConnection.newBuilder()
            .setSsid("Lab").setBssid("02:00:00:11:22:33").setWifiStandard("802.11be").setApMloLinkId(0)
            .addObservationFields(field("identity.state", "available")).addObservationFields(field("ap_mlo_link_id.state", "available"))
            .addAssociatedMloLinks(MloLinkInfo.newBuilder().setLinkId(0).setState("false").setBand("6ghz")
                .setApMacAddress("02:00:00:11:22:33").setStaMacAddress("02:00:00:11:22:44"))).build()
        val scan = WifiScan.newBuilder().addFields(field("fresh_scan_results_updated", "false"))
            .addFields(field("fresh_scan_elapsed_ms", "0")).build()
        val key = "mlo_日本語e\u0301👩\u200d🔬"
        val value = "2001:db8:ffff:ffff:ffff:ffff:ffff:ffff " + "long".repeat(40)
        val context = AgentWifiMloContext(sdkInt = 37, wifiCapabilities = WifiCapabilities.newBuilder().addFields(field(key, value)).build())
        val presentation = AgentWifiMloRenderer.presentation(status, scan, context)
        val fields = presentation.filterIsInstance<AgentBlockPart.Field>().associate { it.label to it.value }
        assertEquals("false", fields["fresh_scan_results_updated"])
        assertEquals("0", fields["fresh_scan_elapsed_ms"])
        assertEquals("0", fields["ap_link_id"])
        assertEquals(value, fields[key])
        val links = presentation.filterIsInstance<AgentBlockPart.Table>().single { it.columns.first().label == "ID" }
        assertEquals(listOf("0", "false", "6ghz", "0", "0", "0", "0", "0", "0", "02:00:00:11:22:33", "02:00:00:11:22:44"), links.rows.single())
        for (parts in listOf(presentation, AgentWifiMloRenderer.interpretation(status, scan, context))) {
            val metadata = parts.filterIsInstance<AgentBlockPart.Field>().associate { it.label to it.value }
            assertEquals("02:00:00:11:22:33", metadata["connected_bssid"])
            assertEquals("false", metadata["current_bssid_seen"])
            assertEquals("0", metadata["same_mld_results"])
            assertEquals("<unknown>", metadata["missing_associated"])
            assertFalse(parts.filterIsInstance<AgentBlockPart.Text>().any { it.value.contains("same_mld_results") })
        }
    }

    @Test fun mloComparisonTablesRetainRowsAndSharedSafePipelines() {
        val identity = "password=Guest-日本語e\u0301👩\u200d🔬-" + "long".repeat(30)
        val marker = "Raw:    preserve:    spaces-日本語e\u0301👩\u200d🔬-" + "reference".repeat(15)
        val scan = WifiScan.newBuilder().apply {
            for (mac in listOf("02:00:00:11:22:33", "06:00:00:11:22:33")) addResults(WifiScanResult.newBuilder()
                .setSsid(identity).setBssid(mac).setApMldMacAddress("02:00:00:00:00:01").setWifiStandard("802.11be")
                .setBand(marker).setRssiDbm(0).addSecurityTypes("eap_wpa3_enterprise").addSecurityTypes("future_Mode"))
        }.build()
        val caps = WifiCapabilities.newBuilder().addFields(DiagnosticField.newBuilder().setKey("mlo_note")
            .setValue("Raw:    preserve:    spaces\nhttps://example.test/a:b\npassword=mlo-synthetic-secret")).build()
        for (brief in listOf(false, true)) {
            val parts = AgentWifiMloRenderer.presentation(WifiStatus.getDefaultInstance(), scan, AgentWifiMloContext(brief = brief, sdkInt = 37, wifiCapabilities = caps))
            val columns = if (brief) listOf("M", "L", "RSSI", "B", "FL", "ST", "SEC", "EHT", "MAC")
                else listOf("SSID", "BANDS", "RSSI", "SEC", "STANDARD", "COLOR", "EHT_W", "PUNCT")
            val table = parts.filterIsInstance<AgentBlockPart.Table>().single { it.columns.map { column -> column.label } == columns }
            assertTrue(table.rows.all { it[columns.indexOf("RSSI")] == "0" })
            assertTrue(table.rows.all { it[if (brief) 3 else 1] == marker })
            if (brief) assertEquals(listOf("02:00:00:11:22:33", "06:00:00:11:22:33"), table.rows.map { it.last() })
            else assertEquals(identity, table.rows.single().first())
            val block = AgentPresentationBlock.create(parts, 0)
            assertTrue(block.safeValues().any { it.second == marker })
            assertTrue(block.fullText.contains(identity))
            assertFalse(block.fullText.contains("mlo-synthetic-secret"))
            val filtered = AgentShellPipeline.parse(listOf("match '^SSID:|^RSSI:'")).apply(AgentShellResult(false, block = block))
            assertFalse(filtered.ok)
            assertTrue(filtered.block!!.parts.filterIsInstance<AgentBlockPart.Field>().any { it.label == "RSSI" && it.value == "0" })
            assertTrue(filtered.block.fullText.contains(identity))
            val json = AgentShellPipeline.parse(listOf("display json")).apply(AgentShellResult(false, block = block)).block!!.fullText
            assertTrue(json.contains(identity))
            assertFalse(json.contains("mlo-synthetic-secret"))
            if (!brief) {
                val note = block.parts.filterIsInstance<AgentBlockPart.Field>().single { it.label == "mlo_note" }
                assertTrue(note.value.contains("Raw:    preserve:    spaces\nhttps://example.test/a:b"))
            }
        }
    }

    @Test fun mloFiltersKeepOriginalIdentityBeforeTypedLayout() {
        val scan = WifiScan.newBuilder().apply {
            listOf("Guest\t東京", "Guest 東京").forEachIndexed { index, ssid -> addResults(WifiScanResult.newBuilder()
                .setSsid(ssid).setBssid("02:00:00:11:22:0${index + 1}").setWifiStandard("802.11be")) }
        }.build()
        for (brief in listOf(false, true)) for (selected in scan.resultsList) {
            for (context in listOf(AgentWifiMloContext(brief = brief, sdkInt = 37, ssidFilter = selected.ssid),
                AgentWifiMloContext(brief = brief, sdkInt = 37, bssidFilter = selected.bssid))) {
                val block = AgentPresentationBlock.create(AgentWifiMloRenderer.presentation(WifiStatus.getDefaultInstance(), scan, context), 0)
                assertTrue(block.fullText.contains(selected.bssid))
                assertFalse(block.fullText.contains(scan.resultsList.first { it != selected }.bssid))
            }
        }
        assertEquals("Guest\t東京", scan.resultsList.first().ssid)
    }

    @Test fun multilineTableKeepsSafeCopyValuesWithoutParsingRawContinuations() {
        val raw = "first\n2001:db8::1\nRaw:    preserve:    spaces\npassword=reqa-synthetic-secret"
        val columns = listOf(AgentBlockPart.Column("HOST"), AgentBlockPart.Column("ERROR"))
        val rows = listOf(listOf("日本語e\u0301👩\u200d🔬", raw))
        val block = AgentPresentationBlock.create(listOf(AgentBlockPart.Table(columns, rows)), 0)
        assertEquals(raw, rows.single().last())
        assertTrue(block.fullText.contains("ERROR: first\n       2001:db8::1\n       Raw:    preserve:    spaces"))
        assertTrue(block.safeValues().any { it.second.contains("first\n2001:db8::1\nRaw:    preserve:    spaces") })
        val filtered = AgentShellPipeline.parse(listOf("except never-match")).apply(AgentShellResult(false, block = block))
        assertFalse(filtered.ok)
        assertTrue(filtered.block!!.fullText.contains("       Raw:    preserve:    spaces"))
        assertFalse(filtered.block.parts.filterIsInstance<AgentBlockPart.Field>().any { it.label == "Raw" })
        val json = AgentShellPipeline.parse(listOf("display json")).apply(AgentShellResult(false, block = block)).block!!.fullText
        assertTrue(json.contains("       Raw:    preserve:    spaces"))
        for (text in listOf(block.fullText, filtered.block.fullText, json)) assertFalse(text.contains("reqa-synthetic-secret"))
    }
}
