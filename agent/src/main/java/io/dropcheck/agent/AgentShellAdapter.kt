package io.dropcheck.agent

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

/** A collector's OK flag cannot contradict typed evidence or known acquisition errors. */
internal fun evaluateShellPayload(request: RunCommand, result: CommandResult): CommandResult {
    if (result.status != CommandResult.Status.STATUS_OK) return result
    val consistent = when (request.commandCase) {
        RunCommand.CommandCase.GET_WIFI_DIAGNOSTICS -> result.hasWifiDiagnostics() && WifiCommandPolicy.diagnosticsComplete(result.wifiDiagnostics)
        RunCommand.CommandCase.GET_WIFI_CAPABILITIES -> result.hasWifiCapabilities() && result.wifiCapabilities.errorsCount == 0
        RunCommand.CommandCase.GET_WIFI_SCAN -> result.hasWifiScan() && result.wifiScan.errorsCount == 0
        RunCommand.CommandCase.GET_FRESH_WIFI_SCAN -> result.hasWifiScan() && WifiCommandPolicy.freshScanEvidence(result.wifiScan)
        RunCommand.CommandCase.CONNECT_WIFI -> result.hasConnectWifi() && result.connectWifi.connected
        RunCommand.CommandCase.DISCONNECT_WIFI, RunCommand.CommandCase.FORGET_WIFI, RunCommand.CommandCase.RECONNECT_WIFI -> result.hasWifiOperation() && result.wifiOperation.ok && result.wifiOperation.errorsCount == 0
        RunCommand.CommandCase.ASSERT_WIFI, RunCommand.CommandCase.WAIT_WIFI_CONNECTED -> result.hasWifiAssert() && result.wifiAssert.passed && result.wifiAssert.errorsCount == 0 && result.wifiAssert.checksList.all { it.passed }
        else -> true
    }
    return if (consistent) result else result.toBuilder().setStatus(CommandResult.Status.STATUS_FAILED)
        .setMessage("typed result is incomplete or inconsistent; retained data is reference").build()
}

internal data class AgentShellResult(
    val ok: Boolean,
    val lines: List<CharSequence> = emptyList(),
    val block: AgentPresentationBlock? = null,
)

/** A thin local-command adapter. All device operations still enter CommandExecutor.execute. */
internal class AgentShellAdapter(private val session: AgentShellSession = AgentShellSession(), private val executeCommand: (RunCommand) -> CommandResult) {

    fun execute(command: AgentShellCommand): AgentShellResult = when (command) {
        is AgentShellCommand.Piped -> command.pipeline.apply(execute(command.command))
        AgentShellCommand.Noop -> AgentShellResult(true)
        is AgentShellCommand.Help -> AgentShellResult(true, AgentShellParser.help(command.topic))
        is AgentShellCommand.SetDefaultPassphrase -> { session.setPassphrase(command.passphrase); AgentShellResult(true, listOf(AgentShellUsePolicy.setDefaultPassphraseMessage(command.passphrase))) }
        AgentShellCommand.ClearDefaultPassphrase -> { session.setPassphrase(""); AgentShellResult(true, listOf(AgentShellUsePolicy.setDefaultPassphraseMessage(""))) }
        AgentShellCommand.ShowVersion -> AgentShellResult(true, block = AgentPresentationBlock.create(
            listOf(AgentBlockPart.Field("Android agent version", BuildConfig.VERSION_NAME)), AgentLogStyle.TEXT_COLOR))
        AgentShellCommand.ShowCli -> AgentShellResult(true, block = AgentPresentationBlock.create(listOf(
            AgentBlockPart.Field("Owner", "self"), AgentBlockPart.Field("Target", "self"), AgentBlockPart.Field("Layout", "measured Android content width"),
            AgentBlockPart.Field("History", "all commands; session-only"), AgentBlockPart.Field("Default passphrase", if (session.defaults.defaultPassphrase.isEmpty()) "unset" else "present")), AgentLogStyle.TEXT_COLOR))
        is AgentShellCommand.Invalid -> AgentShellResult(false, listOf(command.message)).also { if (command.testProfile.isNotEmpty()) session.recordTest(it) }
        is AgentShellCommand.Connect -> connect(command)
        is AgentShellCommand.Check -> {
            session.recordTest(AgentShellResult(false, listOf("Test did not return a valid result")))
            AgentLinkProfile.run(command, executeCommand).let { AgentShellResult(it.outcome == LinkOutcome.PASS, block = AgentPresentationBlock.create(it.presentation(), AgentLogStyle.TEXT_COLOR)) }.also(session::recordTest)
        }
        AgentShellCommand.ShowChecks -> AgentShellResult(true, AgentLinkProfile.catalogue())
        is AgentShellCommand.ShowCheckLast -> lastTest()
        is AgentShellCommand.ShowWifiEht -> eht(command)
        is AgentShellCommand.Execute -> {
            if (command.wifiTest) session.recordTest(AgentShellResult(false, listOf("Test did not return a valid result")))
            render(command, executeCommand(command.request)).also { if (command.wifiTest) session.recordTest(it) }
        }
    }

    private fun connect(command: AgentShellCommand.Connect): AgentShellResult {
        val decision = AgentShellUsePolicy.resolveUseRequest(command.options.ssid, command.options.passphrase.takeIf(String::isNotEmpty), session.defaults)
        val request = decision.request ?: return AgentShellResult(false, listOf(decision.error))
        val wire = AgentShellUsePolicy.connectCommand(command.options, request)
        val result = executeCommand(wire)
        return render(AgentShellCommand.Execute(wire, secret = request.passphrase), result, request.passphraseSource)
    }

    private fun lastTest(): AgentShellResult {
        val previous = session.lastTest ?: return AgentShellResult(true, listOf("No test attempted in this Shell session."))
        val header = listOf(AgentBlockPart.Text("Historical test at ${session.lastTestAtMs} (not current readiness); outcome=${if (previous.ok) "PASS" else "NON-PASS"}"))
        val parts = previous.block?.parts ?: previous.lines.map { AgentBlockPart.Text(it.toString()) }
        return AgentShellResult(true, block = AgentPresentationBlock.create(header + parts, AgentLogStyle.TEXT_COLOR))
    }
    private fun eht(command: AgentShellCommand.ShowWifiEht): AgentShellResult {
        val result = runWifiEhtDiagnostics(command, executeCommand)
        if (!result.hasWifiDiagnostics() || !result.wifiDiagnostics.hasStatus()) return failure("Wi-Fi EHT", result)
        val diagnostics = result.wifiDiagnostics
        val parts = AgentWifiMloRenderer.presentation(diagnostics.status, diagnostics.scan, AgentWifiMloContext(
            wifiCapabilities = diagnostics.capabilities, ssidFilter = command.ssid, bssidFilter = command.bssid,
            scanSource = if (command.fresh) diagnostics.scan.fieldsList.lastOrNull { it.key == "scan_source" }?.value
                ?: "? (scan source metadata unavailable)" else "cached",
        ))
        return block(result, parts, "Wi-Fi EHT")
    }

    private fun render(command: AgentShellCommand.Execute, result: CommandResult, source: AgentShellUsePassphraseSource? = null): AgentShellResult {
        val request = command.request
        val hasPayload = when (request.commandCase) {
            RunCommand.CommandCase.GET_NETWORKS, RunCommand.CommandCase.GET_NETWORK -> result.hasNetworkSnapshot() &&
                (result.status != CommandResult.Status.STATUS_OK || validNetworkSnapshot(result.networkSnapshot, request.getNetwork.takeIf { request.hasGetNetwork() }))
            RunCommand.CommandCase.GET_PERMISSIONS -> result.hasPermissionSnapshot()
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
        val secrets = listOf(command.secret)
        if (!hasPayload) return failure(request.commandCase.name, result, secrets)
        val evaluated = if (request.commandCase == RunCommand.CommandCase.TRACEROUTE) evaluateShellTraceroute(result, command.via) else evaluateShellPayload(request, result)
        if (command.rawView.isNotEmpty()) return AgentRawDebugRenderer.render(command, evaluated)
        val parts = when (request.commandCase) {
            RunCommand.CommandCase.GET_NETWORKS, RunCommand.CommandCase.GET_NETWORK -> buildList {
                add(AgentBlockPart.Field("Observed at unix ms", result.networkSnapshot.observedAtUnixMs.toString()))
                result.networkSnapshot.networksList.forEach { network ->
                    add(AgentBlockPart.Field("Active/default at collection", network.active.toString()))
                    addAll(AgentWifiStatusRenderer.ipPresentation(network.ipStatus, true, true))
                }
                result.networkSnapshot.errorsList.forEach { add(AgentBlockPart.Field("Error", it)) }
            }
            RunCommand.CommandCase.GET_PERMISSIONS -> buildList {
                add(AgentBlockPart.Field("Observed at unix ms", result.permissionSnapshot.observedAtUnixMs.toString()))
                add(AgentBlockPart.Field("Device Owner", result.permissionSnapshot.deviceOwner.toString()))
                result.permissionSnapshot.permissionsList.forEach { add(AgentBlockPart.Field(it.key, it.value)) }
            }
            RunCommand.CommandCase.GET_WIFI_STATUS -> if (command.summary) AgentWifiStatusRenderer.summary(result.wifiStatus) else AgentWifiStatusRenderer.presentation(result.wifiStatus, true, true, true)
            RunCommand.CommandCase.GET_WIFI_DIAGNOSTICS -> AgentWifiDiagnosticsRenderer.presentation(result.wifiDiagnostics, true, true, true)
            RunCommand.CommandCase.GET_WIFI_CAPABILITIES -> AgentWifiCapabilitiesRenderer.presentation(result.wifiCapabilities, true, true)
            RunCommand.CommandCase.GET_WIFI_SCAN, RunCommand.CommandCase.GET_FRESH_WIFI_SCAN -> {
                val selected = AgentWifiMloRenderer.MloFilter(command.scanSsid, command.scanBssid).scanResults(result.wifiScan.resultsList)
                val scan = result.wifiScan.toBuilder().clearResults().addAllResults(selected).build()
                AgentWifiScanRenderer.presentation(scan, AgentWifiScanContext(command.brief, command.mlo, command.fresh, detail = !command.brief))
            }
            RunCommand.CommandCase.GET_WIFI_SCAN_DETAIL -> buildList {
                add(AgentBlockPart.Field("Target", result.wifiScanDetail.target))
                result.wifiScanDetail.fieldsList.forEach { add(AgentBlockPart.Field(it.key, it.value)) }
                addAll(AgentWifiScanRenderer.presentation(io.dropcheck.agent.grpc.WifiScan.newBuilder().addAllResults(result.wifiScanDetail.resultsList)
                    .addAllErrors(result.wifiScanDetail.errorsList).build(), AgentWifiScanContext(detail = true)))
            }
            RunCommand.CommandCase.GET_IP_STATUS -> AgentWifiStatusRenderer.ipPresentation(result.ipStatus, true, true)
            else -> textRows(request, evaluated, source, command.via)
        }
        return block(evaluated, parts, if (source != null) "Wi-Fi connect" else request.commandCase.name.replace('_', ' ').lowercase(), secrets)
    }

    private fun textRows(request: RunCommand, result: CommandResult, source: AgentShellUsePassphraseSource?, via: List<String>): List<AgentBlockPart> = buildList {
        fun row(label: String, value: Any?) { add(AgentBlockPart.Field(label, value.toString())) }
        fun table(labels: List<String>, rows: List<List<String>>) {
            if (rows.isNotEmpty()) add(AgentBlockPart.Table(labels.map { AgentBlockPart.Column(it) }, rows))
        }
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
                row("Already absent", result.wifiOperation.alreadyAbsent)
                if (result.wifiOperation.hasStatus()) addAll(AgentWifiStatusRenderer.presentation(result.wifiOperation.status, detail = true))
            }
            RunCommand.CommandCase.WAIT_WIFI_CONNECTED, RunCommand.CommandCase.ASSERT_WIFI -> {
                row("Passed", result.wifiAssert.passed)
                addAll(AgentWifiStatusRenderer.assertionChecks(result.wifiAssert.checksList))
                result.wifiAssert.errorsList.forEach { row("Error", it) }
                row("Evaluation elapsed ms", result.wifiAssert.elapsedMs)
                if (result.wifiAssert.hasStatus()) addAll(AgentWifiStatusRenderer.presentation(result.wifiAssert.status, detail = true))
                else row("Observation", "? (snapshot unavailable)")
            }
            RunCommand.CommandCase.MONITOR_WIFI -> {
                result.wifiMonitor.eventsList.forEach {
                    row("Event unix ms", it.unixTimeMs)
                    row(it.type, it.message)
                    it.fieldsList.forEach { field -> row(field.key, field.value) }
                    if (it.hasStatus()) addAll(AgentWifiStatusRenderer.presentation(it.status, detail = true))
                }
                result.wifiMonitor.errorsList.forEach { row("Error", it) }
                row("Events", result.wifiMonitor.eventsCount)
            }
            RunCommand.CommandCase.CYCLE_WIFI -> {
                row("Passed", "${result.wifiCycle.passedCount}/${result.wifiCycle.requestedCount}")
                table(listOf("STEP", "CONNECTED", "PING", "HTTP", "ELAPSED(ms)", "ERRORS"), result.wifiCycle.stepsList.map { step ->
                    listOf(step.index.toString(), step.connected.toString(), step.pingOk.toString(), step.httpOk.toString(), step.elapsedMs.toString(), step.errorsList.joinToString("; ")) })
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
                table(listOf("MTU", "PAYLOAD", "PASS", "EXIT", "ELAPSED(ms)"), result.pathMtu.probesList.map { listOf(it.mtuBytes.toString(), it.payloadSizeBytes.toString(), it.passed.toString(), it.exitCode.toString(), it.elapsedMs.toString()) })
            }
            RunCommand.CommandCase.GLOBAL_IP -> {
                row("Family", result.globalIp.requestedFamily); row("Error", result.globalIp.error)
                table(listOf("FAMILY", "IP", "GLOBAL", "STATUS", "ERROR"), result.globalIp.addressesList.map { listOf(it.family.toString(), it.ip, it.global.toString(), it.status.toString(), it.error) })
            }
            RunCommand.CommandCase.RESOLVE_DNS -> {
                row("Name", result.resolveDns.name); row("Error", result.resolveDns.error)
                table(listOf("TYPE", "ADDRESS"), result.resolveDns.answersList.map { listOf(it.type.name, it.address) })
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

    private fun failure(title: String, result: CommandResult, secrets: List<String> = emptyList()): AgentShellResult {
        val failed = result.toBuilder()
            .setStatus(if (result.status == CommandResult.Status.STATUS_OK) CommandResult.Status.STATUS_FAILED else result.status)
            .setMessage(if (result.status == CommandResult.Status.STATUS_OK) "wrong, missing or inconsistent payload" else result.message.ifBlank { "payload unavailable" })
            .build()
        return block(failed, emptyList(), title, secrets)
    }

    private fun String.replaceSecrets(secrets: List<String>): String = secrets.filter(String::isNotEmpty).fold(this) { safe, secret -> safe.replace(secret, "<redacted>") }
}
