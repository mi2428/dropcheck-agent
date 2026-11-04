package io.dropcheck.agent

import io.dropcheck.agent.grpc.*
import org.junit.Assert.*
import org.junit.Test

class JunosAdversarialQaTest {
    @Test fun compactStatusRowsRetainTheirComponents() {
        val connection = WifiConnection.newBuilder().setSsid("Lab").setNetworkId(42).setSubscriptionId(7).setHiddenSsid(true).setRestricted(false).setApplicableRedactions("3")
            .setPasspointFqdn("passpoint.example.test").setPasspointProviderFriendlyName("Provider").setPasspointUniqueId("unique-marker")
            .setFrequencyMhz(6115).setChannelWidth("80MHz").setRssiDbm(-55).setTxLinkSpeedMbps(1200).setRxLinkSpeedMbps(900).setLinkSpeedMbps(700)
        for (field in listOf("identity", "rssi", "tx_link_speed_mbps", "rx_link_speed_mbps")) {
            connection.addObservationFields(DiagnosticField.newBuilder().setKey("$field.state").setValue("available"))
        }
        val ip = IpStatus.newBuilder().setNetworkId("24").setInternet(true).setValidated(false).setPrivateDnsActive(true).setPrivateDnsServerName("resolver.example.test")
            .setOwnerUid(1001).addEnterpriseIds("alpha").addEnterpriseIds("beta").addSubscriptionIds(7).addSubscriptionIds(8)
        for ((key, value) in listOf("capabilities.state" to "available", "link_properties.state" to "available", "default_network.state" to "available", "default_network_id" to "24", "selected_is_default" to "true")) {
            ip.addObservationFields(DiagnosticField.newBuilder().setKey(key).setValue(value))
        }
        val status = WifiStatus.newBuilder().setEnabled(true).setState("enabled").setActiveNetwork("949").setWifiNetworkCount(2).setConnection(connection).setIpStatus(ip).build()
        val out = AgentPresentationBlock.create(AgentWifiStatusRenderer.summary(status), 0).fullText
        for (value in listOf("framework=enabled", "active=949; Wi-Fi=2", "network-id=42; subscription-id=7", "hidden=true; restricted=false; redactions=3", "fqdn=passpoint.example.test; provider=Provider; unique-id=unique-marker", "ch=33; width=80MHz; 6115MHz", "-55dBm", "tx=1200Mbps; rx=900Mbps; link=700Mbps", "24; selected=true", "internet=true; validated=false", "active=true; server=resolver.example.test", "uid=1001; enterprise=alpha,beta; subscriptions=7,8")) {
            assertTrue("compact row lost $value", value in out)
        }
    }

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

    @Test fun statusRawDumpsRequireFullWithoutLosingStructuredEvidence() {
        val link = MloLinkInfo.newBuilder().setLinkId(3).setRaw("link-raw-marker")
            .addObservationFields(DiagnosticField.newBuilder().setKey("identity.state").setValue("available"))
        val ie = WifiInformationElement.newBuilder().setId(127).setByteCount(3).setBytesHex("000008")
        val connection = WifiConnection.newBuilder().setSsid("password=Guest").setNetworkId(4321)
            .setWifiStandard("802.11be").setFrequencyMhz(6115).setChannelWidth("80MHz").setRssiDbm(-55).setTxLinkSpeedMbps(1200).setRxLinkSpeedMbps(900)
            .setSecurityDetails(WifiSecurityDetails.newBuilder().setRsnPresent(true).setPmfCapable(true).setPmfRequired(true))
            .setRaw("wifi-raw-marker password=qa-status-secret").addInformationElements(ie)
            .addAffiliatedMloLinks(link).addAssociatedMloLinks(link)
            .addObservationFields(DiagnosticField.newBuilder().setKey("identity.state").setValue("available"))
            .addObservationFields(DiagnosticField.newBuilder().setKey("identity.source").setValue("typed-source-marker"))
            .addObservationFields(DiagnosticField.newBuilder().setKey("rssi.state").setValue("available"))
            .addObservationFields(DiagnosticField.newBuilder().setKey("tx_link_speed_mbps.state").setValue("available"))
            .addObservationFields(DiagnosticField.newBuilder().setKey("rx_link_speed_mbps.state").setValue("available"))
        val ip = IpStatus.newBuilder().setNetworkId("24").setRawLinkProperties("ip-link-raw-marker").setRawCapabilities("ip-caps-raw-marker")
            .addAddresses("192.0.2.2/24").addAddresses("2001:db8::2/64").addRoutes("0.0.0.0/0 -> 192.0.2.1 wlan0").addDnsServers("192.0.2.53")
            .addObservationFields(DiagnosticField.newBuilder().setKey("link_properties.state").setValue("available"))
            .setWifi(WifiConnection.newBuilder().setNetworkId(6543).setRaw("ip-wifi-raw-marker").addInformationElements(ie).addAssociatedMloLinks(link))
        val status = WifiStatus.newBuilder().setConnection(connection).setIpStatus(ip)
        repeat(300) { status.addObservationFields(DiagnosticField.newBuilder().setKey("detail-only-metadata").setValue("metadata-marker")) }
        val diagnostics = WifiDiagnostics.newBuilder().setStatus(status)
            .setCapabilities(WifiCapabilities.newBuilder().addErrors("device-error-marker"))
            .setScan(WifiScan.newBuilder().addErrors("scan-error-marker").addResults(WifiScanResult.newBuilder().setSsid("AP").setVenueName("venue-marker")
                .setRaw("scan-raw-marker token=qa-status-secret").addInformationElements(ie).addAffiliatedMloLinks(link)))
            .addNetworks(NetworkDiagnostics.newBuilder().setNetworkId("42").setIpStatus(IpStatus.newBuilder().setNetworkId("42")
                .setRawLinkProperties("network-link-raw-marker").setRawCapabilities("network-caps-raw-marker")
                .setWifi(WifiConnection.newBuilder().setNetworkId(8765).setRaw("network-wifi-raw-marker").addInformationElements(ie).addAffiliatedMloLinks(link)))).build()
        for ((line, full) in listOf("show wifi status" to false, "show wifi status full" to true)) {
            val command = AgentShellParser.parse(line) as AgentShellCommand.Execute
            assertEquals(full, command.request.hasGetWifiDiagnostics())
            assertEquals(!full, command.request.hasGetWifiStatus())
            assertEquals(!full, command.summary)
            val parts = if (command.summary) AgentWifiStatusRenderer.summary(diagnostics.status) else AgentWifiDiagnosticsRenderer.presentation(diagnostics, true, true, true)
            val block = AgentPresentationBlock.create(parts, 0)
            val json = AgentShellPipeline.parse(listOf("display json")).apply(AgentShellResult(false, block = block)).block!!.fullText
            for (out in listOf(block.fullText, json)) {
                for (value in listOf("password=Guest", "4321", "11v_bss_transition", "-55dBm", "1200Mbps", "900Mbps", "capable=true required=true", "192.0.2.2/24", "2001:db8::2/64", "192.0.2.1", "192.0.2.53")) assertTrue("$line lost $value", value in out)
                for (value in listOf("6543", "8765", "venue-marker", "typed-source-marker", "device-error-marker", "scan-error-marker", "EHT/MLO interpretation", "metadata-marker")) assertEquals("$line detail visibility: $value", full, value in out)
                for (value in listOf("wifi-raw-marker", "link-raw-marker", "ip-link-raw-marker", "ip-caps-raw-marker", "ip-wifi-raw-marker", "scan-raw-marker", "network-link-raw-marker", "network-caps-raw-marker", "network-wifi-raw-marker", "000008", "Raw WifiInfo reference", "Raw ScanResult reference", "Raw MloLink reference", "Raw LinkProperties", "Raw capabilities")) assertEquals("$line raw visibility: $value", full, value in out)
                assertFalse("qa-status-secret" in out)
            }
            assertTrue(json.contains("\"ok\":false"))
            if (!full) {
                assertTrue("summary contains excessive vertical spacing", block.fullText.lines().size <= 105)
                assertFalse("blank separators between subsections", "\n\n  " in block.fullText)
                for (heading in listOf("  Identity", "  Radio", "  Security", "  MLO", "  Network capabilities", "  IPv6 RA (Android)")) assertTrue(heading in block.fullText.lines())
                parts.filterIsInstance<AgentBlockPart.Field>().forEach { assertEquals("unaligned label", 28, it.label.length) }
            }
        }
        val eht = AgentWifiMloRenderer.render(diagnostics.status, diagnostics.scan, AgentWifiMloContext(wifiCapabilities = diagnostics.capabilities)).joinToString("\n")
        assertTrue("Connected MLO" in eht)
        assertFalse("network-wifi-raw-marker" in eht)
        assertFalse("Raw WifiInfo" in eht)
    }
}
