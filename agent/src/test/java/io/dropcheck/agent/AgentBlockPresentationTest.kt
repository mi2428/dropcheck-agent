package io.dropcheck.agent

import io.dropcheck.agent.grpc.CommandResult
import io.dropcheck.agent.grpc.DiagnosticField
import io.dropcheck.agent.grpc.WifiScan
import io.dropcheck.agent.grpc.WifiScanResult
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class AgentBlockPresentationTest {
    @Test fun fullSafeCopyHasNoDisplayArtifactsOrSecrets() {
        val source = "東京Ｇ e\u0301 👩\u200d🔬 Lab Case\u200B\u001B[31m\u0000\nhttps://user:credential@example.test/Case?token=credential&ordinary=Keep\npassword=credential"
        val block = AgentPresentationBlock.create(listOf(AgentBlockPart.Text(source)), 0)
        for (value in listOf("東京Ｇ", "e\u0301", "👩\u200d🔬", "/Case", "ordinary=Keep")) assertTrue(block.fullText.contains(value))
        for (value in listOf("credential", "\u200B", "\u001B", "\u0000", "\t")) assertFalse(block.fullText.contains(value))
    }

    @Test fun typedIdentityDoesNotBecomeACredentialJustBecauseItLooksLikeAKey() {
        val block = AgentPresentationBlock.create(listOf(AgentBlockPart.Field("SSID", "password=Guest Case"), AgentBlockPart.Table(listOf(AgentBlockPart.Column("SSID")), listOf(listOf("token=Guest Case")))), 0)
        assertTrue(block.fullText.contains("password=Guest Case"))
        assertTrue(block.fullText.contains("token=Guest Case"))
        assertFalse(AgentSafePresentation.text("Authorization: Bearer credential").contains("credential"))
        assertFalse(AgentSafePresentation.cell("https://example.test/Case?%74oken=credential&ordinary=Keep").contains("credential"))
    }

    @Test fun scanKeepsTransitionUnknownEnumsAndCollisionMacs() {
        val macs = listOf("02:00:00:11:22:33", "06:00:00:11:22:33")
        val scan = WifiScan.newBuilder().apply {
            macs.forEach { mac -> addResults(WifiScanResult.newBuilder().setSsid("東京Ｇ e\u0301 👩\u200d🔬 Lab Case").setBssid(mac).setRssiDbm(-48).setBand("6ghz").setFrequencyMhz(6135).setChannelWidth("320MHz").setWifiStandard("802.11be").addSecurityTypes("wpa2_psk").addSecurityTypes("wpa3_sae").addSecurityTypes("future_Mode")) }
        }.build()
        val result = CommandResult.newBuilder().setStatus(CommandResult.Status.STATUS_FAILED).setMessage("fresh scan timed out").setWifiScan(scan).build()
        val block = AgentShellTextFormatter.block(result, AgentWifiScanRenderer.presentation(scan), 0)
        for (value in macs + listOf("psk+sae+future_Mode", "fresh scan timed out", "FAILED", "Source: android", "Target: self")) assertTrue(block.fullText.contains(value))
        assertTrue(block.fullText.indexOf("FAILED") < block.fullText.indexOf("Source"))
        assertEquals("future_MODE", AgentWifiScanRenderer.security(listOf("future_MODE")))
        assertEquals("psk+sae", AgentWifiScanRenderer.security(listOf("wpa2_psk", "wpa3_sae")))
        assertEquals("東京Ｇ e\u0301 👩\u200d🔬 Lab Case", scan.resultsList[0].ssid)
    }

    @Test fun historyEvictsWholeBlocksAndRejectsOversizeWithoutLosingCurrentResult() {
        val history = AgentBlockHistory(maxBlocks = 3, maxChars = 512)
        repeat(12) { index ->
            history.append(AgentPresentationBlock.create(listOf(AgentBlockPart.Text("Result $index FAILED\nTarget: self\nError: reason $index")), 0))
        }
        assertEquals(3, history.blocks.size)
        assertEquals(9, history.evictedBlocks)
        assertTrue(history.blocks.first().fullText.startsWith("Result 9"))
        assertTrue(history.blocks.last().fullText.contains("Error: reason 11"))
        try {
            history.append(AgentPresentationBlock.create(listOf(AgentBlockPart.Text("x".repeat(513))), 0))
            throw AssertionError("oversize accepted")
        } catch (_: IllegalArgumentException) {
            assertTrue(history.blocks.last().fullText.contains("Error: reason 11"))
        }
    }

    @Test fun floorNeverShrinksMoreThanTenPercentOrBelowUserMinimum() {
        assertEquals(10f, AgentNativeBlockLayout.shrinkFloor(10f, 10f), 0f)
        assertEquals(10.8f, AgentNativeBlockLayout.shrinkFloor(12f, 10f), 0.0001f)
        assertEquals(13f, AgentNativeBlockLayout.shrinkFloor(14f, 13f), 0f)
        assertEquals(18f, AgentNativeBlockLayout.shrinkFloor(20f, 10f), 0f)
    }

    @Test fun observationPresenceDistinguishesUnknownEmptyFalseAndZero() {
        val fields = listOf(DiagnosticField.newBuilder().setKey("field.state").setValue("available").build())
        for (value in listOf("", "0", "false")) assertTrue(AgentObservationPresentation.value(emptyList(), "field", value).startsWith("?"))
        assertEquals("none", AgentObservationPresentation.value(fields, "field", ""))
        assertEquals("0", AgentObservationPresentation.value(fields, "field", "0"))
        assertEquals("no", AgentObservationPresentation.bool(fields, "field", false))
        val unavailable = listOf(DiagnosticField.newBuilder().setKey("field.state").setValue("unavailable").build(), DiagnosticField.newBuilder().setKey("field.reason").setValue("permission denied").build())
        assertTrue(AgentObservationPresentation.value(unavailable, "field", "0").contains("permission denied"))
    }
}
