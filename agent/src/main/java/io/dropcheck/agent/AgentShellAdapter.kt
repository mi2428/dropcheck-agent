package io.dropcheck.agent

import android.content.Context
import android.net.wifi.ScanResult
import android.net.wifi.WifiManager
import android.os.Build
import io.dropcheck.agent.grpc.CommandResult
import io.dropcheck.agent.grpc.RunCommand

/** Matches the Go core's typed trace.via evaluator: never infer a pass from text output. */
internal fun evaluateShellTraceroute(result: CommandResult, required: List<String>): CommandResult {
    if (required.isEmpty() || !result.hasTraceroute()) return result
    val trace = result.traceroute
    val unavailable = trace.observationError.isNotEmpty() || !trace.hasReachedTarget()
    val missing = required.filter { hop ->
        unavailable || trace.hopsList.none { hop in it.addressesList || hop in it.hostnamesList }
    }
    if (missing.isEmpty() || result.status != CommandResult.Status.STATUS_OK) return result
    return result.toBuilder().setStatus(CommandResult.Status.STATUS_FAILED)
        .setMessage(if (unavailable) "typed hop observations unavailable" else "required hop not observed: ${missing.joinToString(", ")}")
        .build()
}

internal data class AgentShellResult(
    val ok: Boolean,
    val lines: List<CharSequence> = emptyList(),
    val block: AgentPresentationBlock? = null,
)

/** A thin local-command adapter. All device operations still enter CommandExecutor.execute. */
internal class AgentShellAdapter(private val context: Context, private val logger: CommandLogger) {
    private val executor by lazy { CommandExecutor(context, logger) }

    fun execute(command: AgentShellCommand): AgentShellResult = when (command) {
        is AgentShellCommand.Use -> use(command)
        is AgentShellCommand.Check -> AgentLinkProfile.run(command) { executor.execute(it) }.let { AgentShellResult(it.outcome == LinkOutcome.PASS, it.lines()) }
        AgentShellCommand.ShowChecks -> AgentShellResult(true, AgentLinkProfile.catalogue())
        is AgentShellCommand.ShowCheckLast -> AgentShellResult(true, AgentLinkProfile.lastReport()?.lines(command.detail, historical = true) ?: listOf("No last check report (no check attempted in this process)."))
        is AgentShellCommand.ShowWifiEht -> eht(command)
        is AgentShellCommand.Execute -> render(command, executor.execute(command.request))
        else -> error("not an executable Shell command")
    }

    private fun use(command: AgentShellCommand.Use): AgentShellResult {
        val decision = AgentShellUsePolicy.resolveUseRequest(command.ssid, command.passphrase, AgentShellUseDefaultsStore(context).load())
        val request = decision.request ?: return AgentShellResult(false, listOf(decision.error))
        val wire = AgentShellUsePolicy.connectCommand(request)
        return render(AgentShellCommand.Execute(wire, secret = request.passphrase), executor.execute(wire), request.passphraseSource)
    }

    private fun eht(command: AgentShellCommand.ShowWifiEht): AgentShellResult {
        val result = runWifiEhtDiagnostics(command, executor::execute)
        if (!result.hasWifiDiagnostics() || !result.wifiDiagnostics.hasStatus()) return failure("Wi-Fi EHT", result)
        val diagnostics = result.wifiDiagnostics
        val context = AgentWifiMloContext(
            scanSource = diagnostics.scan.fieldsList.lastOrNull { it.key == "scan_source" }?.value ?: "diagnostics",
            sdkInt = Build.VERSION.SDK_INT,
            wifi7Supported = wifi7StandardSupported(),
            wifiCapabilities = diagnostics.capabilities.takeIf { diagnostics.hasCapabilities() },
            ssidFilter = command.ssid, bssidFilter = command.bssid,
        )
        return block(result, AgentWifiMloRenderer.presentation(diagnostics.status, diagnostics.scan, context, command.detail, true), "Wi-Fi EHT")
    }

    private fun wifi7StandardSupported(): Boolean? {
        if (Build.VERSION.SDK_INT < 33) return null
        return runCatching { context.getSystemService(WifiManager::class.java)?.isWifiStandardSupported(ScanResult.WIFI_STANDARD_11BE) }.getOrNull()
    }

    private fun render(command: AgentShellCommand.Execute, result: CommandResult, source: AgentShellUsePassphraseSource? = null): AgentShellResult {
        val request = command.request
        val hasPayload = when (request.commandCase) {
            RunCommand.CommandCase.GET_WIFI_STATUS -> result.hasWifiStatus()
            RunCommand.CommandCase.GET_WIFI_DIAGNOSTICS -> result.hasWifiDiagnostics()
            RunCommand.CommandCase.GET_WIFI_CAPABILITIES -> result.hasWifiCapabilities()
            RunCommand.CommandCase.GET_WIFI_SCAN, RunCommand.CommandCase.GET_FRESH_WIFI_SCAN -> result.hasWifiScan()
            RunCommand.CommandCase.GET_WIFI_SCAN_DETAIL -> result.hasWifiScanDetail()
            RunCommand.CommandCase.GET_IP_STATUS -> result.hasIpStatus()
            RunCommand.CommandCase.CONNECT_WIFI -> result.hasConnectWifi()
            RunCommand.CommandCase.DISCONNECT_WIFI, RunCommand.CommandCase.FORGET_WIFI, RunCommand.CommandCase.RECONNECT_WIFI -> result.hasWifiOperation()
            RunCommand.CommandCase.WAIT_WIFI_CONNECTED, RunCommand.CommandCase.ASSERT_WIFI -> result.hasWifiAssert()
            RunCommand.CommandCase.MONITOR_WIFI -> result.hasWifiMonitor()
            RunCommand.CommandCase.CYCLE_WIFI -> result.hasWifiCycle()
            RunCommand.CommandCase.PING -> result.hasPing()
            RunCommand.CommandCase.TRACEROUTE -> result.hasTraceroute()
            RunCommand.CommandCase.PATH_MTU -> result.hasPathMtu()
            RunCommand.CommandCase.GLOBAL_IP -> result.hasGlobalIp()
            RunCommand.CommandCase.RESOLVE_DNS -> result.hasResolveDns()
            RunCommand.CommandCase.HTTP_CHECK -> result.hasHttpCheck()
            RunCommand.CommandCase.WGET -> result.hasWget()
            else -> false
        }
        val secrets = listOf(command.secret) + when (request.commandCase) {
            RunCommand.CommandCase.HTTP_CHECK -> listOf(request.httpCheck.url)
            RunCommand.CommandCase.WGET -> listOf(request.wget.url)
            RunCommand.CommandCase.CYCLE_WIFI -> listOf(request.cycleWifi.httpUrl)
            else -> emptyList()
        }
        if (!hasPayload) return failure(request.commandCase.name, result, secrets)
        val evaluated = if (request.commandCase == RunCommand.CommandCase.TRACEROUTE) evaluateShellTraceroute(result, command.via) else result
        val parts = when (request.commandCase) {
            RunCommand.CommandCase.GET_WIFI_STATUS -> AgentWifiStatusRenderer.presentation(result.wifiStatus, command.detail, true, true)
            RunCommand.CommandCase.GET_WIFI_DIAGNOSTICS -> AgentWifiDiagnosticsRenderer.presentation(result.wifiDiagnostics, command.detail, true, true)
            RunCommand.CommandCase.GET_WIFI_CAPABILITIES -> AgentWifiCapabilitiesRenderer.presentation(result.wifiCapabilities, command.detail, true)
            RunCommand.CommandCase.GET_WIFI_SCAN, RunCommand.CommandCase.GET_FRESH_WIFI_SCAN -> AgentWifiScanRenderer.presentation(result.wifiScan,
                AgentWifiScanContext(command.brief, command.mlo, command.fresh))
            RunCommand.CommandCase.GET_WIFI_SCAN_DETAIL -> buildList {
                add(AgentBlockPart.Field("Target", result.wifiScanDetail.target))
                result.wifiScanDetail.fieldsList.forEach { add(AgentBlockPart.Field(it.key, it.value)) }
                addAll(AgentWifiScanRenderer.presentation(io.dropcheck.agent.grpc.WifiScan.newBuilder().addAllResults(result.wifiScanDetail.resultsList)
                    .addAllErrors(result.wifiScanDetail.errorsList).build()))
            }
            RunCommand.CommandCase.GET_IP_STATUS -> AgentWifiStatusRenderer.ipPresentation(result.ipStatus, command.detail, true)
            else -> textRows(request, evaluated, source, command.via).map { AgentBlockPart.Text(it) }
        }
        return block(evaluated, parts, if (source != null) "Wi-Fi Use" else request.commandCase.name.replace('_', ' ').lowercase(), secrets)
    }

    private fun textRows(request: RunCommand, result: CommandResult, source: AgentShellUsePassphraseSource?, via: List<String>): List<String> = buildList {
        fun row(label: String, value: Any?) { add("$label: $value") }
        when (request.commandCase) {
            RunCommand.CommandCase.CONNECT_WIFI -> {
                row("SSID", result.connectWifi.ssid)
                row("Connected", result.connectWifi.connected)
                if (source != null) row("Passphrase source", source.name.lowercase())
                if (result.connectWifi.hasIpStatus()) {
                    row("Interface", result.connectWifi.ipStatus.interfaceName)
                    row("Validated", result.connectWifi.ipStatus.validated)
                    row("Addresses", result.connectWifi.ipStatus.addressesList.joinToString(", "))
                }
            }
            RunCommand.CommandCase.DISCONNECT_WIFI, RunCommand.CommandCase.FORGET_WIFI, RunCommand.CommandCase.RECONNECT_WIFI -> {
                row("Operation", result.wifiOperation.operation); row("OK", result.wifiOperation.ok)
                row("Message", result.wifiOperation.message)
                result.wifiOperation.fieldsList.forEach { row(it.key, it.value) }
                result.wifiOperation.errorsList.forEach { row("Error", it) }
            }
            RunCommand.CommandCase.WAIT_WIFI_CONNECTED, RunCommand.CommandCase.ASSERT_WIFI -> {
                row("Passed", result.wifiAssert.passed)
                result.wifiAssert.checksList.forEach { row(it.key, "expected=${it.expected} actual=${it.actual} passed=${it.passed}") }
                result.wifiAssert.errorsList.forEach { row("Error", it) }
            }
            RunCommand.CommandCase.MONITOR_WIFI -> {
                result.wifiMonitor.eventsList.forEach { row(it.type, it.message) }
                result.wifiMonitor.errorsList.forEach { row("Error", it) }
                row("Events", result.wifiMonitor.eventsCount)
            }
            RunCommand.CommandCase.CYCLE_WIFI -> {
                row("Passed", "${result.wifiCycle.passedCount}/${result.wifiCycle.requestedCount}")
                result.wifiCycle.stepsList.forEach { step ->
                    row("Step ${step.index}", "connected=${step.connected} ping=${step.pingOk} http=${step.httpOk} elapsed=${step.elapsedMs}ms")
                    step.errorsList.forEach { row("Error", it) }
                }
                result.wifiCycle.errorsList.forEach { row("Error", it) }
            }
            RunCommand.CommandCase.PING -> addAll(AgentProbeRenderer.renderPing(result.ping, result.status, result.message))
            RunCommand.CommandCase.TRACEROUTE -> {
                addAll(AgentProbeRenderer.renderTraceroute(result.traceroute, result.status, result.message))
                if (via.isNotEmpty()) row("Required hops", via.joinToString(", "))
            }
            RunCommand.CommandCase.PATH_MTU -> {
                row("Host", result.pathMtu.host); row("Discovered", result.pathMtu.discovered)
                row("Path MTU", result.pathMtu.pathMtuBytes); row("Error", result.pathMtu.error)
                result.pathMtu.probesList.forEach { row("Probe", "${it.mtuBytes} bytes passed=${it.passed} exit=${it.exitCode}") }
            }
            RunCommand.CommandCase.GLOBAL_IP -> {
                row("Family", result.globalIp.requestedFamily); row("Error", result.globalIp.error)
                result.globalIp.addressesList.forEach { row("${it.family}", "${it.ip} global=${it.global} status=${it.status} error=${it.error}") }
            }
            RunCommand.CommandCase.RESOLVE_DNS -> {
                row("Name", result.resolveDns.name); row("Error", result.resolveDns.error)
                result.resolveDns.answersList.forEach { row(it.type.name, it.address) }
            }
            RunCommand.CommandCase.HTTP_CHECK -> {
                row("URL", result.httpCheck.url); row("Status", result.httpCheck.status)
                row("Expected", result.httpCheck.expectedStatus); row("Matched", result.httpCheck.matched); row("Error", result.httpCheck.error)
            }
            RunCommand.CommandCase.WGET -> {
                row("URL", result.wget.url); row("Status", result.wget.status)
                row("Bytes read", result.wget.bytesRead); row("Throughput bps", result.wget.throughputBps); row("Error", result.wget.error)
            }
            else -> error("unhandled result")
        }
    }

    private fun block(result: CommandResult, parts: List<AgentBlockPart>, title: String, secrets: List<String> = emptyList()): AgentShellResult {
        val safe = result.toBuilder().setMessage(result.message.replaceSecrets(secrets)).build()
        val sanitized = parts.map { part -> when (part) {
            is AgentBlockPart.Text -> part.copy(value = part.value.replaceSecrets(secrets))
            is AgentBlockPart.Field -> part.copy(value = part.value.replaceSecrets(secrets), fullValue = part.fullValue.replaceSecrets(secrets))
            is AgentBlockPart.Table -> part.copy(rows = part.rows.map { row -> row.map { it.replaceSecrets(secrets) } })
            is AgentBlockPart.Scan -> part.copy(rows = part.rows.map { row -> row.map { it.replaceSecrets(secrets) } })
        } }
        return AgentShellResult(safe.status == CommandResult.Status.STATUS_OK, block = AgentShellTextFormatter.block(safe, sanitized,
            if (safe.status == CommandResult.Status.STATUS_OK) AgentLogStyle.TEXT_COLOR else -44976, title))
    }

    private fun failure(title: String, result: CommandResult, secrets: List<String> = emptyList()): AgentShellResult = AgentShellResult(false,
        listOf("$title failed (${if (result.status == CommandResult.Status.STATUS_OK) "STATUS_FAILED" else result.status.name}): " +
            if (result.status == CommandResult.Status.STATUS_OK) "payload unavailable" else result.message.ifBlank { "payload unavailable" }.replaceSecrets(secrets)))

    private fun String.replaceSecrets(secrets: List<String>): String = secrets.filter(String::isNotEmpty).fold(this) { safe, secret -> safe.replace(secret, "<redacted>") }
}
