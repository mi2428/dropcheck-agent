package io.dropcheck.agent

import io.dropcheck.agent.grpc.CommandResult
import io.dropcheck.agent.grpc.IpFamily
import io.dropcheck.agent.grpc.IpStatus
import io.dropcheck.agent.grpc.RunCommand
import io.dropcheck.agent.grpc.WifiConnection
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class AgentLinkProfileTest {
    private val target = "Example Lab"
    private val wifi = WifiConnection.newBuilder().setSsid(target).setBssid("aa:bb:cc:dd:ee:ff").setDetailedState("CONNECTED").build()

    private fun ip(network: String = "7", ssid: String = target, iface: String = "wlan0", available: Boolean = true, permission: Boolean = true,
                   addresses: List<String> = listOf("192.0.2.4/24", "2001:db8::4/64"),
                   routes: List<String> = listOf("0.0.0.0/0 -> 192.0.2.1 wlan0", "::/0 -> fe80::1%wlan0 wlan0"),
                   dns: List<String> = listOf("192.0.2.2", "2001:db8::2")): IpStatus = IpStatus.newBuilder()
        .setNetworkId(network).setInterfaceName(iface).addTransports("wifi")
        .setWifi(wifi.toBuilder().setSsid(ssid).addAllObservationFields(observationAvailability("identity", permission))).setValidated(false)
        .addAllObservationFields(observationAvailability("capabilities", true))
        .addAllObservationFields(observationAvailability("link_properties", available))
        .addAllAddresses(addresses).addAllRoutes(routes).addAllDnsServers(dns).build()

    private fun ipResult(value: IpStatus = ip()): CommandResult = CommandResult.newBuilder()
        .setStatus(CommandResult.Status.STATUS_OK).setIpStatus(value).build()

    private fun run(vararg results: CommandResult, check: AgentShellCommand.Check = AgentShellCommand.Check("link", target),
                    calls: MutableList<RunCommand> = mutableListOf()): LinkReport {
        val replies = results.iterator()
        return AgentLinkProfile.run(check) { command ->
            calls += command
            replies.next()
        }
    }

    @Test fun twoReadOnlyIpSnapshotsAndTypedFamilyFixture() {
        for (family in listOf(IpFamily.IP_FAMILY_IPV4, IpFamily.IP_FAMILY_IPV6)) {
            val calls = mutableListOf<RunCommand>()
            val report = run(ipResult(), ipResult(), check = AgentShellCommand.Check("link", target, family), calls = calls)
            assertEquals(LinkOutcome.PASS, report.outcome)
            assertEquals(listOf("ip_before", "ip_after"), report.stages.map { it.name })
            assertTrue(report.stages.all { it.outcome == LinkOutcome.PASS })
            for (stage in report.stages) {
                assertEquals(listOf("addresses", "default_routes", "dns_servers"), stage.metrics.map { it.name })
                assertEquals(listOf(1, 1, 1), stage.metrics.map { it.actual })
            }
            assertEquals(listOf(RunCommand.CommandCase.GET_IP_STATUS, RunCommand.CommandCase.GET_IP_STATUS), calls.map { it.commandCase })
            assertTrue(calls.all { it.getIpStatus.selector.ssid == target })
            assertEquals(false, report.validated) // Observation, not a PASS prerequisite.
            assertTrue(report.lines(detail = true).any { it.contains("dns_servers: PASS actual=1 expected=>=1") })
            assertTrue(report.lines(historical = true).first().startsWith("Historical last check"))
            assertEquals(report, AgentLinkProfile.lastReport())
        }
    }

    @Test fun quotedAndSpacePaddedLiteralIdentitiesAreComparedWithoutDequotingTwice() {
        for (ssid in listOf(" Lab ", "\"Lab\"", " ")) {
            val selectedIp = ip(ssid = ssid)
            val calls = mutableListOf<RunCommand>()
            assertEquals(LinkOutcome.PASS, run(ipResult(selectedIp), ipResult(selectedIp),
                check = AgentShellCommand.Check("link", ssid), calls = calls).outcome)
            assertTrue(calls.all { it.getIpStatus.selector.ssid == ssid })
        }
    }

    @Test fun preflightUnsupportedAndRequiredSkipDoNotReadDevice() {
        val calls = mutableListOf<RunCommand>()
        val unknown = run(check = AgentShellCommand.Check("Go-only", target), calls = calls)
        assertEquals(LinkOutcome.UNSUPPORTED, unknown.outcome)
        assertEquals(listOf(LinkOutcome.UNSUPPORTED, LinkOutcome.SKIP), unknown.stages.map { it.outcome })
        for (profile in listOf("lab", "internet", "eht", "link")) {
            val report = run(check = AgentShellCommand.Check(profile, target, bssid = "aa:bb:cc:dd:ee:ff"), calls = calls)
            assertEquals(LinkOutcome.UNSUPPORTED, report.outcome)
        }
        assertTrue(calls.isEmpty())
        assertTrue(AgentLinkProfile.catalogue().any { it.contains("No default profile") })
        assertTrue(AgentLinkProfile.catalogue().any { it.contains("no endpoints") })
    }

    @Test fun unknownIdentityPermissionWrongNetworkAndFailedAcquisitionAreMissing() {
        val calls = mutableListOf<RunCommand>()
        assertEquals(LinkOutcome.MISSING, run(ipResult(ip(ssid = "<unknown ssid>")), calls = calls).outcome)
        assertEquals(1, calls.size)
        calls.clear()
        assertEquals(LinkOutcome.MISSING, run(ipResult(ip(ssid = "example Lab")), calls = calls).outcome)
        assertEquals(1, calls.size)
        calls.clear()
        assertEquals(LinkOutcome.MISSING, run(ipResult(ip(permission = false)), calls = calls).outcome)
        assertEquals(1, calls.size)
        calls.clear()
        assertEquals(LinkOutcome.MISSING, run(ipResult(), ipResult(ip(network = "8")), calls = calls).outcome)
        assertEquals(2, calls.size)
        calls.clear()
        val failedWithReference = ipResult().toBuilder().setStatus(CommandResult.Status.STATUS_FAILED).build()
        assertEquals(LinkOutcome.MISSING, run(failedWithReference, calls = calls).outcome)
        assertEquals(1, calls.size)
        calls.clear()
        assertEquals(LinkOutcome.MISSING, run(ipResult(ip(available = false)), calls = calls).outcome)
        assertEquals(1, calls.size)
        assertEquals(LinkOutcome.SKIP, AgentLinkProfile.lastReport()!!.stages.last().outcome)
        calls.clear()
        assertEquals(LinkOutcome.MISSING, run(ipResult(ip().toBuilder().clearWifi().build()), calls = calls).outcome)
        assertEquals(1, calls.size)
        calls.clear()
        val vpn = ip().toBuilder().addTransports("vpn").build()
        assertEquals(LinkOutcome.MISSING, run(ipResult(vpn), calls = calls).outcome)
        assertEquals(1, calls.size)
    }

    @Test fun requiredFamilyMissingAndNetworkChangeReplaceHistoricalPass() {
        run(ipResult(), ipResult())
        val calls = mutableListOf<RunCommand>()
        val failed = run(ipResult(ip(dns = listOf("2001:db8::2"))), calls = calls)
        assertEquals(LinkOutcome.FAIL, failed.outcome)
        assertEquals(listOf(LinkOutcome.FAIL, LinkOutcome.SKIP), failed.stages.map { it.outcome })
        assertEquals(0, failed.stages[0].metrics.single { it.name == "dns_servers" }.actual)
        assertEquals(1, calls.size)
        assertEquals(failed, AgentLinkProfile.lastReport())
        calls.clear()
        val noIpv6Route = run(ipResult(), ipResult(ip(routes = listOf("0.0.0.0/0 -> 192.0.2.1 wlan0"))),
            check = AgentShellCommand.Check("link", target, IpFamily.IP_FAMILY_IPV6), calls = calls)
        assertEquals(LinkOutcome.FAIL, noIpv6Route.outcome)
        assertEquals(0, noIpv6Route.stages[1].metrics.single { it.name == "default_routes" }.actual)
        assertEquals(2, calls.size)
        calls.clear()
        val changed = run(ipResult(), ipResult(ip(network = "8")), calls = calls)
        assertEquals(LinkOutcome.MISSING, changed.outcome)
        assertEquals(2, calls.size)
        assertEquals("7", changed.networkId)
        assertTrue(changed.lines(historical = true).first().contains("MISSING"))
        assertEquals(changed, AgentLinkProfile.lastReport())
        calls.clear()
        assertEquals(LinkOutcome.MISSING, run(ipResult(), ipResult(ip(iface = "wlan1")), calls = calls).outcome)
        assertEquals(2, calls.size)
        calls.clear()
        assertEquals(LinkOutcome.MISSING, run(ipResult(), ipResult(ip(ssid = "Other")), calls = calls).outcome)
        assertEquals(2, calls.size)
    }

    @Test fun incompletePayloadCancellationAndPermissionErrorNeverPass() {
        val calls = mutableListOf<RunCommand>()
        val empty = CommandResult.newBuilder().setStatus(CommandResult.Status.STATUS_OK).build()
        assertEquals(LinkOutcome.MISSING, run(empty, calls = calls).outcome)
        assertEquals(1, calls.size)
        calls.clear()
        val canceled = CommandResult.newBuilder().setStatus(CommandResult.Status.STATUS_CANCELED).build()
        assertEquals(LinkOutcome.CANCELED, run(ipResult(), canceled, calls = calls).outcome)
        assertEquals(2, calls.size)
        val exception = AgentLinkProfile.run(AgentShellCommand.Check("link", target)) { throw SecurityException("no permission") }
        assertEquals(LinkOutcome.MISSING, exception.outcome)
        assertEquals(listOf(LinkOutcome.MISSING, LinkOutcome.SKIP), exception.stages.map { it.outcome })
        assertTrue(exception.stages.first().reason.contains("SecurityException"))
        assertEquals(exception, AgentLinkProfile.lastReport())
    }
}
