package io.dropcheck.agent

import io.dropcheck.agent.grpc.*
import org.junit.Assert.*
import org.junit.Test

class JunosAdversarialQaTest {
    @Test fun testReportsBelongOnlyToTheirShellSession() {
        val first = AgentShellSession()
        val second = AgentShellSession()
        first.recordTest(AgentShellResult(true, listOf("PASS")))
        assertTrue(first.lastTest!!.ok)
        first.recordTest(AgentShellResult(false, listOf("invalid test syntax")))
        assertFalse(first.lastTest!!.ok)
        assertEquals(listOf("invalid test syntax"), first.lastTest!!.lines)
        assertNull(second.lastTest)
        assertNull(AgentShellSession().lastTest)
    }

    @Test fun rejectedAbbreviatedTestsCanReplaceHistoricalPass() {
        for (line in listOf("te wi require-ip BAD", "te li ssid Lab family BAD", "te la ssid Lab", "te wi | trim 0", "te wi ssid 'unfinished", "test internet")) {
            val parsed = AgentShellParser.parse(line)
            assertTrue(line, parsed is AgentShellCommand.Invalid && parsed.testProfile.isNotEmpty())
            assertFalse(line, AgentShellParser.historySafe(line))
        }
        assertTrue(AgentShellParser.complete("h req wi ").any { "connect" in it })
    }

    @Test fun cancellationAndForgedFreshOkDoNotBecomeSuccess() {
        val scan = WifiScan.newBuilder().addResults(WifiScanResult.newBuilder().setSsid("reference")).build()
        val bad = CommandResult.newBuilder().setStatus(CommandResult.Status.STATUS_OK).setWifiScan(scan).build()
        val diagnostics = CommandResult.newBuilder().setStatus(CommandResult.Status.STATUS_OK).setWifiDiagnostics(WifiDiagnostics.newBuilder()
            .setStatus(WifiStatus.getDefaultInstance()).setScan(WifiScan.getDefaultInstance()).setCapabilities(WifiCapabilities.getDefaultInstance())).build()
        val result = runWifiEhtDiagnostics(AgentShellCommand.ShowWifiEht(fresh = true)) { if (it.hasGetFreshWifiScan()) bad else diagnostics }
        assertEquals(CommandResult.Status.STATUS_FAILED, result.status)
        assertEquals("reference", result.wifiDiagnostics.scan.resultsList.single().ssid)
        var calls = 0
        try {
            Thread.currentThread().interrupt()
            runWifiEhtDiagnostics(AgentShellCommand.ShowWifiEht()) { calls++; diagnostics }
            fail("interrupted acquisition continued")
        } catch (_: InterruptedException) { assertEquals(0, calls) }
        finally { Thread.interrupted() }
    }

    @Test fun fullBundleKeepsSafeEvidenceWithoutRawSecrets() {
        val connection = WifiConnection.newBuilder().setSsid("password=Guest").setNetworkId(4321).setSubscriptionId(5678)
            .setRaw("wifi-marker password=qa-new-secret").addObservationFields(DiagnosticField.newBuilder().setKey("identity.state").setValue("available"))
            .addInformationElements(WifiInformationElement.newBuilder().setId(221).setByteCount(3).setBytesHex("a1b2c3"))
        val diagnostics = WifiDiagnostics.newBuilder().setStatus(WifiStatus.newBuilder().setConnection(connection)).setCapabilities(WifiCapabilities.getDefaultInstance())
            .addNetworks(NetworkDiagnostics.newBuilder().setNetworkId("42").setIpStatus(IpStatus.newBuilder().setNetworkId("42").setWifi(WifiConnection.newBuilder().setNetworkId(8765).setSubscriptionId(7890).setRaw("network-marker"))))
            .setScan(WifiScan.newBuilder().addResults(WifiScanResult.newBuilder().setVenueName("venue-marker").setOperatorFriendlyName("operator-marker").setRaw("scan-marker token=qa-new-secret"))).build()
        val out = AgentPresentationBlock.create(AgentWifiDiagnosticsRenderer.presentation(diagnostics, true, true, true), 0).fullText
        for (value in listOf("4321", "5678", "8765", "7890", "network-marker", "a1b2c3", "wifi-marker", "scan-marker", "venue-marker", "operator-marker", "password=Guest", "EHT/MLO interpretation")) assertTrue(value, value in out)
        assertFalse("qa-new-secret" in out)
    }
}
