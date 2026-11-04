package io.dropcheck.agent

import io.dropcheck.agent.grpc.*
import org.junit.Assert.*
import org.junit.Test

class RawDebugTest {
    private fun command(line: String) = AgentShellParser.parse(line) as AgentShellCommand.Execute

    @Test fun commandsReusePassiveCollectorsAndExplicitNetworkIdentity() {
        assertTrue(command("debug android wifi info").request.hasGetWifiStatus())
        val scan = command("debug android wifi scan ssid 'password=Guest' band 6ghz")
        assertTrue(scan.request.hasGetWifiScan())
        assertFalse(scan.request.hasGetFreshWifiScan())
        assertEquals("password=Guest", scan.scanSsid)
        assertEquals(WifiBand.WIFI_BAND_6_GHZ, scan.request.getWifiScan.band)
        for (view in listOf("capabilities", "link-properties", "wifi-info")) {
            assertEquals("42", command("debug android network $view network-id 42").request.getNetwork.networkId)
        }
        assertTrue(AgentShellParser.complete("debug android network ").contains("debug android network capabilities"))
        assertTrue(AgentShellParser.parse("debug adb wifi") is AgentShellCommand.Invalid)
    }

    @Test fun rawViewsKeepOnlyRequestedDumpsAndRetainPrivacyAndFailure() {
        val link = MloLinkInfo.newBuilder().setRaw("LINK-RAW zero=0 flag=false")
        val connection = WifiConnection.newBuilder().setSsid("structured-only").setRaw("WIFI-RAW password=debug-secret\nsecond-line")
            .addAssociatedMloLinks(link).addInformationElements(WifiInformationElement.newBuilder().setBytesHex("aabbcc"))
        val network = NetworkDiagnostics.newBuilder().setNetworkId("42").setIpStatus(IpStatus.newBuilder().setNetworkId("42").setMtu(1500)
            .setRawCapabilities("CAPS-RAW token=debug-secret").setRawLinkProperties("PROPERTIES-RAW").setWifi(connection)
            .addObservationFields(DiagnosticField.newBuilder().setKey("capabilities.state").setValue("available"))
            .addObservationFields(DiagnosticField.newBuilder().setKey("link_properties.state").setValue("available")))
        val cases = listOf(
            Triple("debug android wifi info", CommandResult.newBuilder().setWifiStatus(WifiStatus.newBuilder().setConnection(connection)), "WIFI-RAW"),
            Triple("debug android wifi scan ssid 'password=Guest'", CommandResult.newBuilder().setWifiScan(WifiScan.newBuilder()
                .addResults(WifiScanResult.newBuilder().setSsid("password=Guest").setRaw("SCAN-RAW token=debug-secret").addAffiliatedMloLinks(link))
                .addResults(WifiScanResult.newBuilder().setSsid("other").setRaw("WRONG-AP-RAW"))), "SCAN-RAW"),
            Triple("debug android network capabilities network-id 42", CommandResult.newBuilder().setNetworkSnapshot(NetworkSnapshot.newBuilder().setObservedAtUnixMs(1).addNetworks(network).addErrors("partial acquisition")), "CAPS-RAW"),
            Triple("debug android network wifi-info network-id 42", CommandResult.newBuilder().setNetworkSnapshot(NetworkSnapshot.newBuilder().setObservedAtUnixMs(1).addNetworks(network)), "WIFI-RAW"),
            Triple("debug android network link-properties network-id 42", CommandResult.newBuilder().setNetworkSnapshot(NetworkSnapshot.newBuilder().setObservedAtUnixMs(1).addNetworks(network)), "PROPERTIES-RAW"),
        )
        for ((line, builder, expected) in cases) {
            val captured = builder.setStatus(CommandResult.Status.STATUS_FAILED).setMessage("acquisition incomplete").build()
            val output = AgentRawDebugRenderer.render(command(line), captured)
            assertFalse(output.ok)
            val json = AgentShellPipeline.parse(listOf("display json")).apply(output).block!!.fullText
            for (text in listOf(output.block!!.fullText, json)) {
                assertTrue(expected, expected in text)
                assertTrue("FAILED" in text)
                assertTrue("acquisition incomplete" in text)
                for (absent in listOf("structured-only", "aabbcc", "MTU:", "1500", "debug-secret", "WRONG-AP-RAW")) assertFalse(absent, absent in text)
                if (expected == "CAPS-RAW") assertFalse("PROPERTIES-RAW" in text)
                if (expected == "PROPERTIES-RAW") assertFalse("CAPS-RAW" in text)
            }
            assertEquals("acquisition incomplete", captured.message)
        }
    }

    @Test fun absentOrMismatchedRawNeverFallsBackOrBecomesSuccessful() {
        val parsed = command("debug android network capabilities network-id 42")
        for (result in listOf(CommandResult.getDefaultInstance(), CommandResult.newBuilder().setNetworkSnapshot(NetworkSnapshot.newBuilder()
            .addNetworks(NetworkDiagnostics.newBuilder().setNetworkId("43").setIpStatus(IpStatus.newBuilder().setNetworkId("43").setRawCapabilities("WRONG-RAW")))).build())) {
            val output = AgentRawDebugRenderer.render(parsed, result)
            assertFalse(output.ok)
            assertFalse("WRONG-RAW" in output.block!!.fullText)
        }
        val absent = AgentRawDebugRenderer.render(command("debug android wifi info"), CommandResult.newBuilder().setWifiStatus(WifiStatus.getDefaultInstance()).build())
        assertFalse(absent.ok)
        assertTrue("requested raw snapshot unavailable" in absent.block!!.fullText)
        val canceled = AgentRawDebugRenderer.render(parsed, CommandResult.newBuilder().setStatus(CommandResult.Status.STATUS_CANCELED).setMessage("capture canceled").build())
        assertFalse(canceled.ok)
        assertTrue("CANCELED" in canceled.block!!.fullText)
        assertTrue("capture canceled" in canceled.block!!.fullText)
    }

    @Test fun disconnectedWifiInfoStillHasRawEvidenceWithoutInventingAConnection() {
        val result = CommandResult.newBuilder().setStatus(CommandResult.Status.STATUS_OK).setWifiStatus(WifiStatus.newBuilder().setConnection(WifiConnection.newBuilder()
            .setSsid("<unknown ssid>").setDetailedState("DISCONNECTED").setRaw("DISCONNECTED-RAW"))).build()
        val output = AgentRawDebugRenderer.render(command("debug android wifi info"), result)
        assertTrue(output.ok)
        assertTrue("DISCONNECTED-RAW" in output.block!!.fullText)
        assertFalse("SSID:" in output.block!!.fullText)
    }
}
