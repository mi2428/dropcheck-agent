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
