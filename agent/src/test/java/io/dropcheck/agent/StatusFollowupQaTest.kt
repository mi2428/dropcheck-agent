package io.dropcheck.agent

import io.dropcheck.agent.grpc.*
import org.junit.Assert.*
import org.junit.Test

class StatusFollowupQaTest {
    @Test fun focusedEhtRetainsLiteralTypedIdentities() {
        val status = WifiStatus.newBuilder().setConnection(WifiConnection.newBuilder().setSsid("password=Guest").setBssid("02:00:00:11:22:33").setWifiStandard("802.11be")
            .addObservationFields(DiagnosticField.newBuilder().setKey("identity.state").setValue("available"))).build()
        val scan = WifiScan.newBuilder().addResults(WifiScanResult.newBuilder().setSsid("token=Guest").setBssid("02:00:00:44:55:66").setWifiStandard("802.11be")).build()
        val parts = AgentWifiMloRenderer.presentation(status, scan, AgentWifiMloContext())
        assertTrue(parts.filterIsInstance<AgentBlockPart.Field>().any { it.label == "SSID" && it.value == "password=Guest" })
        assertTrue(parts.filterIsInstance<AgentBlockPart.Table>().any { it.rows.any { row -> "token=Guest" in row } })
        val block = AgentShellTextFormatter.block(CommandResult.newBuilder().setStatus(CommandResult.Status.STATUS_OK).build(), parts, 0, "Wi-Fi EHT")
        for (identity in listOf("password=Guest", "token=Guest")) assertTrue("EHT lost literal identity $identity", identity in block.fullText)
        val filtered = AgentPresentationBlock.create(AgentWifiMloRenderer.presentation(status, scan, AgentWifiMloContext(ssidFilter = "Other")), 0).fullText
        assertFalse("password=Guest" in filtered)
        assertFalse("token=Guest" in filtered)
    }

    @Test fun expandedStatusDoesNotLeakCompoundFieldCredentials() {
        val connection = WifiConnection.newBuilder().setSsid("password=Guest")
            .setPasspointProviderFriendlyName("password=qa-provider-secret").setApplicableRedactions("token=qa-redaction-secret")
            .addObservationFields(DiagnosticField.newBuilder().setKey("identity.state").setValue("available"))
        val ip = IpStatus.newBuilder().setNetworkId("24").setHttpProxy("https://proxy-user:qa-proxy-secret@example.test:8443/")
            .setNetworkSpecifier("passphrase=qa-specifier-secret").setPrivateDnsActive(true).setPrivateDnsServerName("password=qa-dns-secret")
            .addIpv6Ra(DiagnosticField.newBuilder().setKey(" password ").setValue("qa-padded-key-secret"))
        for (key in listOf("link_properties.state", "capabilities.state")) ip.addObservationFields(DiagnosticField.newBuilder().setKey(key).setValue("available"))
        val status = WifiStatus.newBuilder().setState("token=qa-framework-secret").setConnection(connection).setIpStatus(ip).build()
        for (full in listOf(false, true)) {
            val parts = if (full) AgentWifiDiagnosticsRenderer.presentation(WifiDiagnostics.newBuilder().setStatus(status).build())
                else AgentWifiStatusRenderer.summary(status)
            val block = AgentPresentationBlock.create(parts, 0)
            val json = AgentShellPipeline.parse(listOf("display json")).apply(AgentShellResult(false, block = block)).block!!.fullText
            for (out in listOf(block.fullText, json)) {
                for (marker in listOf("qa-provider-secret", "qa-redaction-secret", "qa-proxy-secret", "qa-specifier-secret", "qa-dns-secret", "qa-padded-key-secret", "qa-framework-secret")) {
                    assertFalse("full=$full leaked $marker", marker in out)
                }
                assertTrue("typed identity was redacted", "password=Guest" in out)
            }
            assertTrue("typed failure was lost", "\"ok\":false" in json)
        }
    }

    @Test fun nestedCredentialAssignmentsAreRedactedBeforeCopy() {
        for (text in listOf("provider=password=qa-nested-secret", "framework=token=qa-nested-secret", "active=true; server=password=qa-nested-secret")) {
            assertFalse(text, "qa-nested-secret" in AgentSafePresentation.text(text))
        }
    }
}
