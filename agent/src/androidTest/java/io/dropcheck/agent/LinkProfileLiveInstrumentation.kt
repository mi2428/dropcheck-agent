package io.dropcheck.agent

import android.app.Activity
import android.app.Instrumentation
import android.os.Bundle
import io.dropcheck.agent.grpc.CommandLog
import io.dropcheck.agent.grpc.CommandResult
import io.dropcheck.agent.grpc.GetIpStatus
import io.dropcheck.agent.grpc.RunCommand

/** Existing physical Wi-Fi only; no permission, network configuration or external probe changes. */
class LinkProfileLiveInstrumentation : Instrumentation() {
    override fun onCreate(arguments: Bundle?) { super.onCreate(arguments); start() }

    override fun onStart() {
        var initialReads = 0
        var linkReads = 0
        var wrongReads = 0
        var rejected = 0
        var shellAdapterPassed = false
        fun done(outcome: LinkOutcome, category: String, wrong: String = "SKIP", stages: String = "SKIP,SKIP") {
            val result = Bundle().apply {
                putString("stream", "link=$outcome category=$category stages=$stages wrong=$wrong shell_adapter=$shellAdapterPassed reads=initial:$initialReads,link:$linkReads,wrong:$wrongReads rejected:$rejected; mutation=0 external_probe=0 (GET_IP_STATUS only)\n")
            }
            finish(if (outcome == LinkOutcome.PASS && wrong == "MISSING,SKIP" && rejected == 0) Activity.RESULT_OK else Activity.RESULT_CANCELED, result)
        }
        try {
            // Production logger serializes identities; discard every line before it reaches a log sink.
            val logger = object : CommandLogger {
                override fun log(level: CommandLog.Level, message: String, scope: CommandLogScope) = Unit
            }
            val executor = CommandExecutor(targetContext.applicationContext, logger)
            val initialCommand = RunCommand.newBuilder().setGetIpStatus(GetIpStatus.getDefaultInstance()).build()
            check(initialCommand.commandCase == RunCommand.CommandCase.GET_IP_STATUS && initialCommand.getIpStatus.selector.ssid.isEmpty())
            initialReads++
            val initial = executor.execute(initialCommand)
            if (initial.status != CommandResult.Status.STATUS_OK || !initial.hasIpStatus()) {
                done(LinkOutcome.MISSING, "bootstrap_status_unavailable")
                return
            }
            val ip = initial.ipStatus
            if ("wifi" !in ip.transportsList || "vpn" in ip.transportsList || ip.networkId.isBlank() || ip.interfaceName.isBlank()) {
                done(LinkOutcome.MISSING, "physical_network_unavailable")
                return
            }
            if (!ip.hasWifi() || !AgentObservationPresentation.available(ip.wifi.observationFieldsList, "identity") || !isKnownWifiSsid(ip.wifi.ssid)) {
                done(LinkOutcome.MISSING, "ssid_identity_unavailable")
                return
            }
            val observedSsid = ip.wifi.ssid // Private process memory only; never emit report.lines or command/result.toString().
            val report = AgentLinkProfile.run(AgentShellCommand.Check("link", observedSsid)) { request ->
                if (request.commandCase != RunCommand.CommandCase.GET_IP_STATUS || request.getIpStatus.selector.ssid != observedSsid) {
                    rejected++
                    throw SecurityException("read-only command boundary")
                }
                linkReads++
                check(linkReads <= 2) { "unexpected snapshot count" }
                executor.execute(request)
            }
            check(rejected == 0 && linkReads in 1..2 && report.stages.size == 2)
            check(report.stages[0].outcome == LinkOutcome.PASS || report.stages[1].outcome == LinkOutcome.SKIP)
            if (report.outcome == LinkOutcome.PASS) {
                check(linkReads == 2 && report.stages.all { it.outcome == LinkOutcome.PASS })
                check(report.stages.all { stage -> stage.metrics.size == 3 && stage.metrics.all { it.outcome == LinkOutcome.PASS } })
                val adapter = AgentShellAdapter(targetContext.applicationContext, logger)
                check(adapter.execute(AgentShellCommand.ShowChecks).lines.any { it.toString().contains("link: supported") })
                val escapedSsid = observedSsid.replace("\\", "\\\\").replace("\"", "\\\"")
                val parsed = AgentShellParser.parse("check link ssid \"$escapedSsid\"")
                check(parsed is AgentShellCommand.Check && parsed.ssid == observedSsid)
                val shellResult = adapter.execute(parsed)
                check(shellResult.ok && shellResult.lines.any { it.toString().contains("PASS") })
                val historical = adapter.execute(AgentShellCommand.ShowCheckLast(detail = true))
                check(historical.ok && historical.lines.firstOrNull()?.toString()?.startsWith("Historical last check") == true)
                shellAdapterPassed = true
            }
            val missing = if (report.outcome == LinkOutcome.FAIL) report.stages.flatMap { it.metrics }
                .filter { it.outcome == LinkOutcome.FAIL }.map { metric -> when (metric.name) {
                    "addresses" -> "ipv4_address"
                    "default_routes" -> "ipv4_default_route"
                    "dns_servers" -> "ipv4_dns"
                    else -> "unknown_metric"
                } }.distinct().joinToString("+").ifEmpty { "required_provisioning" }
            else if (report.outcome == LinkOutcome.MISSING) "identity_or_snapshot_unavailable" else "none"
            var wrong = "SKIP"
            if (report.outcome == LinkOutcome.PASS) {
                val wrongTarget = AgentLinkProfile.run(AgentShellCommand.Check("link", observedSsid + "\u0000")) { request ->
                    if (request.commandCase != RunCommand.CommandCase.GET_IP_STATUS || request.getIpStatus.selector.ssid != observedSsid + "\u0000") {
                        rejected++
                        throw SecurityException("read-only command boundary")
                    }
                    wrongReads++
                    check(wrongReads <= 1) { "unexpected wrong-target snapshot count" }
                    executor.execute(request)
                }
                check(rejected == 0 && wrongReads == 1 && wrongTarget.outcome == LinkOutcome.MISSING)
                check(wrongTarget.stages.map { it.outcome } == listOf(LinkOutcome.MISSING, LinkOutcome.SKIP))
                val latest = AgentShellAdapter(targetContext.applicationContext, logger).execute(AgentShellCommand.ShowCheckLast(detail = false))
                check(latest.lines.firstOrNull()?.toString()?.contains("MISSING") == true) { "last check retained a stale PASS" }
                wrong = "MISSING,SKIP"
            }
            done(report.outcome, missing, wrong, report.stages.joinToString(",") { it.outcome.name })
        } catch (_: Exception) {
            done(LinkOutcome.FAIL, "boundary_or_execution_error")
        }
    }
}
