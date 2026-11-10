package io.dropcheck.agent

import io.dropcheck.agent.grpc.CommandResult
import io.dropcheck.agent.grpc.GetIpStatus
import io.dropcheck.agent.grpc.Ping
import io.dropcheck.agent.grpc.ResolveDns
import io.dropcheck.agent.grpc.DnsRecordType
import io.dropcheck.agent.grpc.HttpCheck
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
    fun presentation(): List<AgentBlockPart> = buildList {
        add(AgentBlockPart.Text("Check $profile ${outcome.name} (self; ${if (profile == "lab") "explicit active probes" else "observed only"})"))
        add(AgentBlockPart.Field("SSID", ssid))
        add(AgentBlockPart.Field("Family", family.name))
        add(AgentBlockPart.Field("Started / ended", "$startedMs / $endedMs"))
        add(AgentBlockPart.Field("Source", "Android selected physical Wi-Fi"))
        add(AgentBlockPart.Field("Network", networkId ?: "?"))
        add(AgentBlockPart.Field("Interface", interfaceName ?: "?"))
        add(AgentBlockPart.Field("Validated (observed)", validated?.toString() ?: "?"))
        add(AgentBlockPart.Table(listOf("STAGE", "OUTCOME", "REASON").map { AgentBlockPart.Column(it) }, stages.map { listOf(it.name, it.outcome.name, it.reason) }))
        val metrics = stages.flatMap { stage -> stage.metrics.map { listOf(stage.name, it.name, it.outcome.name, it.actual?.toString() ?: "?", ">=${it.required}") } }
        if (metrics.isNotEmpty()) add(AgentBlockPart.Table(listOf("STAGE", "METRIC", "OUTCOME", "ACTUAL", "EXPECTED").map { AgentBlockPart.Column(it) }, metrics))
        add(AgentBlockPart.Text(if (profile == "lab") "No connect, disconnect or forget; snapshots cannot prove uninterrupted Network continuity or HTTP hostname address family."
            else "No probe, connect, disconnect or forget; same-Network checks are point-in-time, not uninterrupted roam/BSSID pinning."))
    }
}

/** Runs the link snapshot check and explicitly configured lab probes. */
internal object AgentLinkProfile {
    private val linkStages = listOf("ip_before", "ip_after")

    fun catalogue(): List<String> = listOf(
        "No default profile; show test profiles lists candidates (zero operations).",
        "link: supported self; test link ssid SSID [family ipv4|ipv6|all] [bssid BSSID]; default family ipv4.",
        "  Example: test link ssid \"Example Lab\" family all",
        "  SSID-selected physical Wi-Fi/Network/interface plus IP family address>=1, default route>=1, DNS>=1, repeated on the same Network/interface.",
        "  2 read-only IP snapshots, not continuous roam proof; validated observed only; no endpoints, probes, connect or cleanup. bssid strict pin unsupported.",
        "lab: configured only; test lab ssid SSID family ipv4|ipv6 gateway IP dns NAME resolver selected http URL expected-status STATUS timeout MS.",
        "  Example: test lab ssid \"Example Lab\" family ipv4 gateway 192.0.2.1 dns lab.example.test resolver selected http https://lab.example.test/health expected-status 204 timeout 5000",
        "  2 IP snapshots, 3 ICMP packets, 1 selected-resolver DNS query, 1 HTTP request; no Wi-Fi mutation; HTTP hostname address family not pinned.",
        "internet, eht, Go-only callbacks: unsupported on Android (zero operations; no partial run).",
    )


    @Synchronized fun run(command: AgentShellCommand.Check, execute: (RunCommand) -> CommandResult): LinkReport {
        val started = System.currentTimeMillis()
        val stages = mutableListOf<LinkStage>()
        var network: LinkIdentity? = null
        val lab = command.lab?.takeIf { command.profile == "lab" }
        val stageNames = if (lab != null) listOf("ip_before", "gateway_ping", "dns", "http", "ip_after") else linkStages
        fun finish(outcome: LinkOutcome): LinkReport {
            while (stages.size < stageNames.size) stages += LinkStage(stageNames[stages.size], LinkOutcome.SKIP, "required predecessor did not pass")
            return LinkReport(command.profile.take(96), command.ssid.take(96), command.family, started, System.currentTimeMillis(),
                outcome, stages.toList(), network?.networkId?.take(96), network?.interfaceName?.take(96), network?.validated)
        }
        if (command.profile != "link" && command.profile != "lab" || command.profile == "lab" && lab == null) {
            stages += LinkStage(stageNames.first(), LinkOutcome.UNSUPPORTED, "profile is not portable on Android")
            return finish(LinkOutcome.UNSUPPORTED)
        }
        if (command.bssid.isNotEmpty()) {
            stages += LinkStage(stageNames.first(), LinkOutcome.UNSUPPORTED, "strict BSSID pinning cannot be guaranteed")
            return finish(LinkOutcome.UNSUPPORTED)
        }
        if (command.ssid.isEmpty() || command.family !in listOf(IpFamily.IP_FAMILY_IPV4, IpFamily.IP_FAMILY_IPV6, IpFamily.IP_FAMILY_ALL)) {
            stages += LinkStage(stageNames.first(), LinkOutcome.MISSING, "SSID and ipv4/ipv6 family required")
            return finish(LinkOutcome.MISSING)
        }
        if (command.profile == "lab" && !lab!!.valid(command.family)) {
            stages += LinkStage(stageNames.first(), LinkOutcome.MISSING, "lab configuration incomplete")
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
        if (lab != null) {
            val selector = NetworkSelector.newBuilder().setSsid(command.ssid).build()
            fun probe(name: String, request: RunCommand, valid: (CommandResult) -> Boolean): LinkOutcome {
                val read = read(request, execute)
                val outcome = when {
                    read.failure == LinkOutcome.CANCELED -> LinkOutcome.CANCELED
                    read.result == null || !valid(read.result) -> LinkOutcome.MISSING
                    read.result.status == CommandResult.Status.STATUS_OK && name == "gateway_ping" && !read.result.ping.hasReceived() -> LinkOutcome.MISSING
                    read.failure != null || read.result.status != CommandResult.Status.STATUS_OK || when (name) {
                        "gateway_ping" -> !read.result.ping.hasReceived() || read.result.ping.received == 0
                        "dns" -> read.result.resolveDns.answersCount == 0
                        else -> !read.result.httpCheck.matched || read.result.httpCheck.status != lab.expectedStatus
                    } -> LinkOutcome.FAIL
                    else -> LinkOutcome.PASS
                }
                stages += LinkStage(name, outcome, read.reason.ifEmpty { if (outcome == LinkOutcome.MISSING) "typed result unavailable" else if (outcome == LinkOutcome.FAIL) "required probe failed" else "" })
                return outcome
            }
            val ping = RunCommand.newBuilder().setPing(Ping.newBuilder().setHost(lab.gateway).setFamily(command.family).setCount(3).setTimeoutMs(lab.timeoutMs).setSelector(selector)).build()
            val dns = RunCommand.newBuilder().setResolveDns(ResolveDns.newBuilder().setName(lab.dns).addQtypes(if (command.family == IpFamily.IP_FAMILY_IPV6) DnsRecordType.DNS_RECORD_TYPE_AAAA else DnsRecordType.DNS_RECORD_TYPE_A).setTimeoutMs(lab.timeoutMs).setSelector(selector)).build()
            val http = RunCommand.newBuilder().setHttpCheck(HttpCheck.newBuilder().setUrl(lab.http).setExpectedStatus(lab.expectedStatus).setTimeoutMs(lab.timeoutMs).setSelector(selector)).build()
            for ((name, request, valid) in listOf<Triple<String, RunCommand, (CommandResult) -> Boolean>>(
                Triple("gateway_ping", ping, { it.hasPing() }),
                Triple("dns", dns, { it.hasResolveDns() }),
                Triple("http", http, { it.hasHttpCheck() }),
            )) {
                val outcome = probe(name, request, valid)
                if (outcome != LinkOutcome.PASS) return finish(outcome)
            }
        }
        return finish(snapshot(stageNames.last(), network))
    }

    private data class LinkRead(val result: CommandResult? = null, val failure: LinkOutcome? = null, val reason: String = "")
    private fun read(command: RunCommand, execute: (RunCommand) -> CommandResult): LinkRead = try {
        val result = execute(command)
        when (result.status) {
            CommandResult.Status.STATUS_OK -> LinkRead(result)
            CommandResult.Status.STATUS_CANCELED -> LinkRead(failure = LinkOutcome.CANCELED, reason = "acquisition canceled")
            else -> LinkRead(result = result, failure = LinkOutcome.MISSING, reason = "acquisition failed")
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
        if (family == IpFamily.IP_FAMILY_ALL) return listOf(IpFamily.IP_FAMILY_IPV4, IpFamily.IP_FAMILY_IPV6).flatMap { selected ->
            metrics(ip, selected).map { it.copy(name = (if (selected == IpFamily.IP_FAMILY_IPV4) "ipv4." else "ipv6.") + it.name) }
        }
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
