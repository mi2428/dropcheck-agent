package io.dropcheck.agent

import io.dropcheck.agent.grpc.CommandResult
import io.dropcheck.agent.grpc.GetIpStatus
import io.dropcheck.agent.grpc.IpFamily
import io.dropcheck.agent.grpc.IpStatus
import io.dropcheck.agent.grpc.NetworkSelector
import io.dropcheck.agent.grpc.RunCommand
import java.net.Inet6Address
import java.net.InetAddress

internal enum class LinkOutcome { PASS, FAIL, MISSING, SKIP, UNSUPPORTED, CANCELED }

internal data class LinkMetric(val name: String, val actual: Int?, val required: Int = 1) {
    val outcome: LinkOutcome get() = when { actual == null -> LinkOutcome.MISSING; actual >= required -> LinkOutcome.PASS; else -> LinkOutcome.FAIL }
}

internal data class LinkStage(val name: String, val outcome: LinkOutcome, val reason: String = "", val metrics: List<LinkMetric> = emptyList())

/** Retains only bounded, credential-free typed facts; never a RunCommand or raw collector payload. */
internal data class LinkReport(
    val profile: String,
    val ssid: String,
    val family: IpFamily,
    val startedMs: Long,
    val endedMs: Long,
    val outcome: LinkOutcome,
    val stages: List<LinkStage>,
    val networkId: String? = null,
    val interfaceName: String? = null,
    val validated: Boolean? = null,
) {
    fun lines(detail: Boolean = false, historical: Boolean = false): List<String> = buildList {
        add("${if (historical) "Historical last check" else "Check"}: ${safe(profile)} ${outcome.name} (self; observed only)")
        add("Target SSID: ${safe(ssid)}; family: ${if (family == IpFamily.IP_FAMILY_IPV6) "ipv6" else "ipv4"}")
        add("Started: $startedMs; ended: $endedMs; source: Android selected physical Wi-Fi")
        add("Network: ${networkId?.let(::safe) ?: "?"}; interface: ${interfaceName?.let(::safe) ?: "?"}; validated (observed, not required): ${validated?.toString() ?: "?"}")
        stages.forEach { stage ->
            add("${stage.name}: ${stage.outcome.name}${stage.reason.takeIf(String::isNotEmpty)?.let { " (${safe(it)})" } ?: ""}")
            if (detail) stage.metrics.forEach { metric -> add("  ${metric.name}: ${metric.outcome.name} actual=${metric.actual ?: "?"} expected=>=${metric.required}") }
        }
        if (detail) add("No probe, connect, disconnect or forget; same-Network checks are point-in-time, not uninterrupted roam/BSSID pinning.")
    }
}

private fun safe(value: String): String = AgentSafePresentation.text(value.take(96)).replace('\n', ' ')

/** One fixed portable definition matching the PC link read order; no generic scenario runner. */
internal object AgentLinkProfile {
    private val stageNames = listOf("ip_before", "ip_after")
    private var last: LinkReport? = null

    fun catalogue(): List<String> = listOf(
        "No default profile; bare check lists candidates (zero operations).",
        "link: supported self; check link ssid SSID [family ipv4|ipv6] [bssid BSSID]; default family ipv4.",
        "  SSID-selected physical Wi-Fi/Network/interface plus IP family address>=1, default route>=1, DNS>=1, repeated on the same Network/interface.",
        "  2 read-only IP snapshots, not continuous roam proof; validated observed only; no endpoints, probes, connect or cleanup. bssid strict pin unsupported.",
        "lab, internet, eht, Go-only callbacks: unsupported on Android (zero operations; no partial run).",
    )

    @Synchronized fun lastReport(): LinkReport? = last

    @Synchronized fun run(command: AgentShellCommand.Check, execute: (RunCommand) -> CommandResult): LinkReport {
        val started = System.currentTimeMillis()
        val stages = mutableListOf<LinkStage>()
        var network: LinkIdentity? = null
        fun finish(outcome: LinkOutcome): LinkReport {
            while (stages.size < stageNames.size) stages += LinkStage(stageNames[stages.size], LinkOutcome.SKIP, "required predecessor did not pass")
            return LinkReport(command.profile.take(96), command.ssid.take(96), command.family, started, System.currentTimeMillis(),
                outcome, stages.toList(), network?.networkId?.take(96), network?.interfaceName?.take(96), network?.validated).also { last = it }
        }
        if (command.profile != "link") {
            stages += LinkStage(stageNames.first(), LinkOutcome.UNSUPPORTED, "profile is not portable on Android")
            return finish(LinkOutcome.UNSUPPORTED)
        }
        if (command.bssid.isNotEmpty()) {
            stages += LinkStage(stageNames.first(), LinkOutcome.UNSUPPORTED, "strict BSSID pinning cannot be guaranteed")
            return finish(LinkOutcome.UNSUPPORTED)
        }
        if (command.ssid.isEmpty() || command.family !in listOf(IpFamily.IP_FAMILY_IPV4, IpFamily.IP_FAMILY_IPV6)) {
            stages += LinkStage(stageNames.first(), LinkOutcome.MISSING, "SSID and ipv4/ipv6 family required")
            return finish(LinkOutcome.MISSING)
        }
        val ipRequest = RunCommand.newBuilder().setGetIpStatus(GetIpStatus.newBuilder()
            .setSelector(NetworkSelector.newBuilder().setSsid(command.ssid))).build()
        fun snapshot(name: String, previous: LinkIdentity?): LinkOutcome {
            val read = read(ipRequest, execute)
            val ip = read.result?.takeIf { it.hasIpStatus() }?.ipStatus
            val identityReason = ip?.let { ipIdentityReason(it, command.ssid, previous) }
            val checks = if (ip != null && identityReason == null) metrics(ip, command.family) else emptyList()
            val outcome = read.failure ?: when {
                ip == null || identityReason != null -> LinkOutcome.MISSING
                checks.any { it.outcome == LinkOutcome.MISSING } -> LinkOutcome.MISSING
                checks.any { it.outcome == LinkOutcome.FAIL } -> LinkOutcome.FAIL
                else -> LinkOutcome.PASS
            }
            stages += LinkStage(name, outcome, read.reason.ifEmpty {
                identityReason ?: if (outcome == LinkOutcome.FAIL) "required family provisioning absent" else if (outcome == LinkOutcome.MISSING) "IP status unavailable" else ""
            }, checks)
            if (outcome == LinkOutcome.PASS) network = LinkIdentity(ip!!.networkId, ip.interfaceName, ip.validated)
            return outcome
        }
        val first = snapshot(stageNames[0], null)
        if (first != LinkOutcome.PASS) return finish(first)
        return finish(snapshot(stageNames[1], network))
    }

    private data class LinkRead(val result: CommandResult? = null, val failure: LinkOutcome? = null, val reason: String = "")
    private fun read(command: RunCommand, execute: (RunCommand) -> CommandResult): LinkRead = try {
        val result = execute(command)
        when (result.status) {
            CommandResult.Status.STATUS_OK -> LinkRead(result)
            CommandResult.Status.STATUS_CANCELED -> LinkRead(failure = LinkOutcome.CANCELED, reason = "acquisition canceled")
            else -> LinkRead(failure = LinkOutcome.MISSING, reason = "acquisition failed")
        }
    } catch (_: InterruptedException) {
        Thread.currentThread().interrupt()
        LinkRead(failure = LinkOutcome.CANCELED, reason = "acquisition interrupted")
    } catch (e: Exception) {
        LinkRead(failure = LinkOutcome.MISSING, reason = "observation unavailable: ${e.javaClass.simpleName}")
    }

    private data class LinkIdentity(val networkId: String, val interfaceName: String, val validated: Boolean?)

    private fun ipIdentityReason(ip: IpStatus, ssid: String, initial: LinkIdentity? = null): String? = when {
        ip.networkId.isBlank() || ip.interfaceName.isBlank() -> "selected Network/interface unavailable"
        "wifi" !in ip.transportsList || "vpn" in ip.transportsList -> "selected Network is not physical Wi-Fi"
        !AgentObservationPresentation.available(ip.observationFieldsList, "capabilities") ||
            !AgentObservationPresentation.available(ip.observationFieldsList, "link_properties") -> "selected Network observation unavailable"
        !ip.hasWifi() || !AgentObservationPresentation.available(ip.wifi.observationFieldsList, "identity") -> "Wi-Fi identity permission denied or unavailable"
        !ssidMatches(ssid, ip.wifi.ssid) -> "selected Network SSID unknown or different"
        initial != null && (ip.networkId != initial.networkId || ip.interfaceName != initial.interfaceName) -> "selected Network/interface changed"
        else -> null
    }

    // Proto WifiConnection.ssid is already decoded by WifiProtoMapper; never dequote twice.
    private fun ssidMatches(requested: String, actual: String): Boolean =
        isKnownWifiSsid(actual) && actual == requested

    private fun metrics(ip: IpStatus, family: IpFamily): List<LinkMetric> {
        val known = AgentObservationPresentation.available(ip.observationFieldsList, "link_properties")
        return listOf(
            LinkMetric("addresses", if (known) ip.addressesList.count { addressFamily(it.substringBefore('/'), family) } else null),
            LinkMetric("default_routes", if (known) ip.routesList.count { route ->
                val prefix = route.trim().substringBefore(' ').substringBefore(',')
                prefix.endsWith("/0") && addressFamily(prefix.substringBefore('/'), family) &&
                    runCatching { InetAddress.getByName(prefix.substringBefore('/')).isAnyLocalAddress }.getOrDefault(false)
            } else null),
            LinkMetric("dns_servers", if (known) ip.dnsServersList.count { addressFamily(it, family) } else null),
        )
    }

    private fun addressFamily(raw: String, family: IpFamily): Boolean {
        val address = raw.substringBefore('%')
        // A colon is required before parsing IPv6; InetAddress must never resolve a hostname.
        if (family == IpFamily.IP_FAMILY_IPV6) return ':' in address && runCatching { InetAddress.getByName(address) is Inet6Address }.getOrDefault(false)
        val octets = address.split('.')
        return octets.size == 4 && octets.all { it.isNotEmpty() && it.length <= 3 && it.all { ch -> ch in '0'..'9' } && it.toIntOrNull()?.let { n -> n in 0..255 } == true }
    }
}
