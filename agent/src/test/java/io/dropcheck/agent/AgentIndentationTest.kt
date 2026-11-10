package io.dropcheck.agent

import io.dropcheck.agent.grpc.WifiInformationElement
import io.dropcheck.agent.grpc.DiagnosticField
import io.dropcheck.agent.grpc.WifiScan
import io.dropcheck.agent.grpc.WifiScanResult
import io.dropcheck.agent.grpc.WifiSecurityDetails
import io.dropcheck.agent.grpc.WifiConnection
import io.dropcheck.agent.grpc.WifiStatus
import io.dropcheck.agent.grpc.IpStatus
import io.dropcheck.agent.grpc.WifiCapabilities
import io.dropcheck.agent.grpc.PermissionSnapshot
import io.dropcheck.agent.grpc.WifiMonitorResult
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class AgentIndentationTest {
    private fun assertSectionFields(parts: List<AgentBlockPart>, title: String, labels: List<String>) {
        val start = parts.indexOf(AgentBlockPart.Text(title, section = true))
        assertTrue("missing section $title", start >= 0)
        val end = (start + 1 until parts.size).firstOrNull { index -> (parts[index] as? AgentBlockPart.Text)?.section == true } ?: parts.size
        val fields = parts.subList(start + 1, end).filterIsInstance<AgentBlockPart.Field>()
        assertEquals(title, labels, fields.map { it.label })
        assertTrue("$title children must be exactly two spaces deep", fields.all { it.indent == 2 })
    }

    @Test fun ipAddressFamiliesAreChildrenWhileRoutesRemainSiblings() {
        val parts = AgentWifiStatusRenderer.ipPresentation(IpStatus.newBuilder()
            .addAddresses("192.0.2.1/24")
            .addAddresses("192.0.2.2/24")
            .addAddresses("2001:db8::1/64")
            .addRoutes("::/0 -> fe80::1")
            .build())
        val ipv4 = parts.indexOf(AgentBlockPart.Text("IPv4", section = true))
        val ipv6 = parts.indexOf(AgentBlockPart.Text("IPv6", section = true))
        assertTrue(ipv4 >= 0 && ipv6 > ipv4)
        val ipv4Field = parts[ipv4 + 1] as AgentBlockPart.Field
        val ipv6Field = parts[ipv6 + 1] as AgentBlockPart.Field
        assertEquals("Addr", ipv4Field.label)
        assertEquals(2, ipv4Field.indent)
        assertEquals("192.0.2.1/24\n192.0.2.2/24", ipv4Field.fullValue)
        assertEquals("2001:db8::1/64", ipv6Field.fullValue)
        assertTrue(parts.any { it == AgentBlockPart.Field("Routes", "? (link_properties: observation metadata unavailable)", "::/0 -> fe80::1", indent = 2) })
        assertTrue(AgentPresentationBlock.create(parts, 0).safeValues().any { it.first == "Addr wire value (availability above)" && it.second == "192.0.2.1/24\n192.0.2.2/24" })
    }

    @Test fun scanDetailsRetainApAndSubsectionOwnershipAsTypedParts() {
        val scan = WifiScan.newBuilder().addResults(
            WifiScanResult.newBuilder()
                .setSsid("Lab")
                .setRssiDbm(-30)
                .setBssid("02:00:00:00:00:01")
                .setOperatorFriendlyName("owner-property")
                .setApMloLinkId(2)
                .setSecurityDetails(WifiSecurityDetails.newBuilder().setRsnPresent(true).setRsnVersion(1))
                .addInformationElements(WifiInformationElement.newBuilder().setId(48).setByteCount(2).setBytesHex("0102"))
                .addObservationFields(DiagnosticField.newBuilder().setKey("ap_mlo_link_id.state").setValue("available"))
                .build(),
        ).addResults(WifiScanResult.newBuilder().setSsid("Guest").setRssiDbm(-60).setBssid("02:00:00:00:00:02").build()).build()
        val parts = AgentWifiScanRenderer.presentation(scan, AgentWifiScanContext(detail = true))
        val block = AgentPresentationBlock.create(parts, 0)
        val firstAp = parts.indexOf(AgentBlockPart.Text("[ ] AP 1", section = true))
        val secondAp = parts.indexOf(AgentBlockPart.Text("[ ] AP 2", section = true))
        assertTrue(block.fullText.contains("\n\n[ ] AP 2\n"))
        assertFalse(block.fullText.contains("\n\n\n[ ] AP 2\n"))
        val firstChildren = parts.subList(firstAp + 1, secondAp)
        assertEquals("SSID", (firstChildren.first() as AgentBlockPart.Field).label)
        assertTrue(firstChildren.any { it == AgentBlockPart.Text("  AP Capabilities", section = true) })
        assertTrue(firstChildren.any { it is AgentBlockPart.Field && it.label == "security" && it.indent == 4 && it.value == "rsn" })
        assertTrue(firstChildren.any { it == AgentBlockPart.Text("  Information Elements", section = true) })
        assertTrue(firstChildren.any { it is AgentBlockPart.Field && it.label == "IE 1" && it.indent == 4 && it.value.contains("extension=0 bytes=2 hex=0102") })
        assertTrue(firstChildren.any { it is AgentBlockPart.Field && it.label == "ap_mlo_link_id.state" && it.indent == 4 })
        assertEquals(2, (firstChildren.first() as AgentBlockPart.Field).indent)
        assertEquals(2, (parts[secondAp + 1] as AgentBlockPart.Field).indent)
        assertTrue(block.fullText.contains("[ ] AP 1\n  SSID: Lab"))
        assertTrue(block.fullText.contains("  AP Capabilities\n    security:"))
        assertTrue(block.fullText.contains("[ ] AP 2\n  SSID: Guest"))
        assertTrue(parts.filterIsInstance<AgentBlockPart.Scan>().single().rows.size == 2)
    }

    @Test fun propertiesAfterSecurityAndIeSectionsRemainSiblings() {
        val parts = AgentWifiStatusRenderer.presentation(WifiStatus.newBuilder().setConnection(
            WifiConnection.newBuilder()
                .setSsid("Lab")
                .setSecurityDetails(WifiSecurityDetails.newBuilder().setRsnPresent(true).setRsnVersion(1))
                .setApMloLinkId(2)
                .addInformationElements(WifiInformationElement.newBuilder().setId(48).setByteCount(2).setBytesHex("0102"))
                .addObservationFields(DiagnosticField.newBuilder().setKey("link_id.state").setValue("available"))
                .build(),
        ).build())
        val securityTitle = parts.indexOf(AgentBlockPart.Text("  Wi-Fi Security Details", section = true))
        val ieTitle = parts.indexOf(AgentBlockPart.Text("Information Elements", section = true))
        assertTrue(securityTitle >= 0 && ieTitle > securityTitle)
        assertTrue(parts.any { it is AgentBlockPart.Field && it.label == "rsn" && it.indent == 4 })
        assertTrue(parts.any { it is AgentBlockPart.Field && it.label == "AP link ID" && it.indent == 0 })
        assertTrue(parts.any { it is AgentBlockPart.Field && it.label == "IE 1" && it.indent == 2 })
        assertTrue(parts.any { it is AgentBlockPart.Field && it.label == "link_id.state" && it.indent == 2 })
    }

    @Test fun nestedScanAndDiagnosticTablesKeepTypedRowsAndLogicalValues() {
        val columns = listOf(AgentBlockPart.Column("SSID"), AgentBlockPart.Column("BSSID"))
        val scanRows = listOf(listOf("Lab", "02:00:00:00:00:01"))
        val scan = AgentBlockPart.Scan(scanRows, columns)
        val diagnosticRows = listOf(listOf("6GHz", "yes"))
        val table = AgentBlockPart.Table(listOf(AgentBlockPart.Column("Band"), AgentBlockPart.Column("Support")), diagnosticRows)
        val parts = listOf(AgentBlockPart.Text("Scan summary")) + nestedPresentation(listOf(scan, table))
        val nestedScan = parts.filterIsInstance<AgentBlockPart.Scan>().single()
        val nestedTable = parts.filterIsInstance<AgentBlockPart.Table>().single()
        assertEquals(2, nestedScan.indent)
        assertEquals(2, nestedTable.indent)
        assertEquals(columns, nestedScan.columns)
        assertEquals(scanRows, nestedScan.rows)
        assertEquals(diagnosticRows, nestedTable.rows)
        val nestedField = nestedPresentation(listOf(AgentBlockPart.Field("Field", "value"))).single() as AgentBlockPart.Field
        assertEquals(2, nestedField.indent)
        val block = AgentPresentationBlock.create(parts, 0)
        assertTrue(block.fullText.contains("  SSID: Lab"))
        assertTrue(block.fullText.contains("  Band: 6GHz"))
        assertTrue(block.safeValues().any { it.first == "AP 1 SSID" && it.second == "Lab" })
        assertTrue(block.safeValues().any { it.first == "Row 1 Band" && it.second == "6GHz" })
        assertTrue(AgentNativeBlockLayout.tableFitsWidth(100, 0f, listOf(40f, 40f), 4f))
        assertTrue(AgentNativeBlockLayout.tableFitsWidth(100, 8f, listOf(40f, 40f), 4f))
        assertTrue(AgentNativeBlockLayout.tableFitsWidth(100, 16f, listOf(40f, 40f), 4f))
        assertFalse(AgentNativeBlockLayout.tableFitsWidth(100, 17f, listOf(40f, 40f), 4f))
    }

    @Test fun headerIndentIsPartOfLogicalFieldDepth() {
        val block = AgentShellTextFormatter.block(
            io.dropcheck.agent.grpc.CommandResult.newBuilder().setStatus(io.dropcheck.agent.grpc.CommandResult.Status.STATUS_OK).build(),
            emptyList(), 0,
        )
        val source = block.parts.filterIsInstance<AgentBlockPart.Field>().single { it.label == "Source" }
        assertEquals(2, source.indent)
        assertTrue(block.fullText.lines().contains("  Source: android"))
    }

    @Test fun connectionDetailsAndMloLinkChildrenKeepExplicitDepth() {
        val status = WifiStatus.newBuilder().setConnection(WifiConnection.newBuilder()
            .addAssociatedMloLinks(io.dropcheck.agent.grpc.MloLinkInfo.newBuilder().setLinkId(1))).build()
        val parts = AgentWifiStatusRenderer.presentation(status)
        val connectionDetails = parts.indexOf(AgentBlockPart.Text("Connection detail", section = true))
        assertTrue(connectionDetails >= 0)
        assertEquals(2, (parts[connectionDetails + 1] as AgentBlockPart.Field).indent)
        val links = parts.indexOf(AgentBlockPart.Text("Associated links", section = true))
        assertTrue(links >= 0)
        assertEquals(2, (parts[links + 1] as AgentBlockPart.Text).value.takeWhile { it == ' ' }.length)
        assertTrue(parts.drop(links + 1).filterIsInstance<AgentBlockPart.Field>().any { it.label == "Link ID" && it.indent == 4 })
    }

    @Test fun sectionChildrenUseExplicitTwoSpaceDepthAcrossRenderers() {
        val ip = IpStatus.newBuilder().addAddresses("other-family").addRoutes("default").addDnsServers("192.0.2.53")
            .addCapabilities("INTERNET").addObservationFields(DiagnosticField.newBuilder().setKey("ip.field").setValue("yes"))
            .setWifi(WifiConnection.newBuilder().setNetworkId(4).setSsid("Lab").addObservationFields(DiagnosticField.newBuilder().setKey("link.field").setValue("yes")))
            .build()
        val ipParts = AgentWifiStatusRenderer.ipPresentation(ip)
        assertSectionFields(ipParts, "Other addresses", listOf("Other address"))
        assertSectionFields(ipParts, "Routing and DNS", listOf("Routes", "DNS", "DHCP server", "Private DNS", "NAT64"))
        assertSectionFields(ipParts, "Network capabilities", listOf("Capabilities", "Bandwidth", "Signal strength", "Specifier", "Owner uid", "Enterprise IDs", "Subscription IDs"))
        assertSectionFields(ipParts, "Link properties", listOf("Domains", "Proxy", "Wake on LAN"))
        assertSectionFields(ipParts, "Connection fields", listOf("link.field"))
        assertSectionFields(ipParts, "Fields", listOf("ip.field"))
        assertTrue(ipParts.any { it is AgentBlockPart.Field && it.label == "Network configuration ID" && it.indent == 2 })

        val statusParts = AgentWifiStatusRenderer.presentation(WifiStatus.newBuilder().setConnection(WifiConnection.newBuilder()
            .setSsid("Lab").addObservationFields(DiagnosticField.newBuilder().setKey("conn.field").setValue("yes")))
            .addPermissions("permission=granted")
            .addObservationFields(DiagnosticField.newBuilder().setKey("status.field").setValue("yes"))
            .setIpStatus(ip).build())
        assertSectionFields(statusParts, "Connection fields", listOf("conn.field"))
        assertSectionFields(statusParts, "Permissions", listOf("Permission"))
        assertSectionFields(statusParts, "Fields", listOf("status.field"))
        assertTrue(statusParts.any { it is AgentBlockPart.Field && it.label == "Related IP snapshot" && it.indent == 2 })
        assertTrue(statusParts.any { it is AgentBlockPart.Field && it.label == "Network" && it.indent == 2 })

        val capabilities = AgentWifiCapabilitiesRenderer.presentation(WifiCapabilities.newBuilder()
            .addFields(DiagnosticField.newBuilder().setKey("cap.field").setValue("yes")).addErrors("failed").build())
        assertSectionFields(capabilities, "Fields", listOf("cap.field"))
        assertSectionFields(capabilities, "Errors", listOf("Error"))

        val scan = AgentWifiScanRenderer.presentation(WifiScan.getDefaultInstance(), AgentWifiScanContext(mloOnly = true))
        listOf("Requested", "Data source", "Updated", "Band", "Update elapsed", "APs", "Connections at collection", "Filter").forEach { label ->
            assertTrue("scan field $label", scan.any { it is AgentBlockPart.Field && it.label == label && it.indent == 2 })
        }
        assertEquals(0, scan.filterIsInstance<AgentBlockPart.Scan>().single().indent)

        val adapter = AgentShellAdapter { request ->
            when (request.commandCase) {
                io.dropcheck.agent.grpc.RunCommand.CommandCase.GET_PERMISSIONS -> io.dropcheck.agent.grpc.CommandResult.newBuilder()
                    .setStatus(io.dropcheck.agent.grpc.CommandResult.Status.STATUS_OK)
                    .setPermissionSnapshot(PermissionSnapshot.getDefaultInstance()).build()
                io.dropcheck.agent.grpc.RunCommand.CommandCase.MONITOR_WIFI -> io.dropcheck.agent.grpc.CommandResult.newBuilder()
                    .setStatus(io.dropcheck.agent.grpc.CommandResult.Status.STATUS_OK)
                    .setWifiMonitor(WifiMonitorResult.newBuilder().addErrors("monitor error")).build()
                else -> error("unexpected synthetic command")
            }
        }
        val permissions = adapter.execute(AgentShellParser.parse("show permissions") as AgentShellCommand.Execute).block!!.parts
        assertSectionFields(permissions, "Permissions", listOf("Observed at unix ms", "Device Owner"))
        val monitor = adapter.execute(AgentShellParser.parse("monitor wifi") as AgentShellCommand.Execute).block!!.parts
        assertSectionFields(monitor, "Errors", listOf("Error"))
        assertSectionFields(monitor, "Monitor summary", listOf("Events"))
    }
}
