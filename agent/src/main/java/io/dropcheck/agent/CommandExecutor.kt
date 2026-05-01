package io.dropcheck.agent

import android.content.Context
import io.dropcheck.agent.grpc.AssertWifi
import io.dropcheck.agent.grpc.CommandResult
import io.dropcheck.agent.grpc.ConnectWifi
import io.dropcheck.agent.grpc.ConnectWifiResult
import io.dropcheck.agent.grpc.CycleWifi
import io.dropcheck.agent.grpc.DiagnosticField
import io.dropcheck.agent.grpc.ForgetWifi
import io.dropcheck.agent.grpc.GetFreshWifiScan
import io.dropcheck.agent.grpc.GetWifiScan
import io.dropcheck.agent.grpc.GetWifiScanDetail
import io.dropcheck.agent.grpc.HttpCheck
import io.dropcheck.agent.grpc.MonitorWifi
import io.dropcheck.agent.grpc.NetworkSelector
import io.dropcheck.agent.grpc.Ping
import io.dropcheck.agent.grpc.ReconnectWifi
import io.dropcheck.agent.grpc.RunCommand
import io.dropcheck.agent.grpc.WaitWifiConnected
import io.dropcheck.agent.grpc.WatchWifi
import io.dropcheck.agent.grpc.WifiAssertResult
import io.dropcheck.agent.grpc.WifiBand
import io.dropcheck.agent.grpc.WifiCycleResult
import io.dropcheck.agent.grpc.WifiCycleStep
import io.dropcheck.agent.grpc.WifiOperationResult
import io.dropcheck.agent.grpc.WifiStatus
import java.time.Duration

/**
 * Dispatches controller gRPC commands onto local Android Wi-Fi and network APIs.
 *
 * This class should stay orchestration-only. Command defaults and result
 * classification live in pure policy objects so controller-visible behavior is
 * unit-testable without booting Android.
 */
class CommandExecutor(
    private val context: Context,
    private val logger: CommandLogger,
) {
    private val networks = NetworkRepository(context, logger)
    private val wifi = WifiConnector(context, logger)
    private val networkChecks = NetworkCheckExecutor(networks, logger)

    /**
     * Dispatches a single protobuf command and returns exactly one command result.
     *
     * Streaming progress belongs to [CommandLogger]; this method owns only the
     * final payload returned to the controller for the command ID.
     */
    fun execute(command: RunCommand): CommandResult {
        throwIfInterrupted()
        val startedAt = System.nanoTime()
        logger.debugEvent("command.executor.start", listOf(
            "thread" to Thread.currentThread().name,
        ) + command.logFields())
        val result = when (command.commandCase) {
            RunCommand.CommandCase.GET_WIFI_STATUS -> wifiStatus()
            RunCommand.CommandCase.GET_WIFI_DIAGNOSTICS -> wifiDiagnostics()
            RunCommand.CommandCase.GET_WIFI_SCAN -> wifiScan(command.getWifiScan)
            RunCommand.CommandCase.GET_WIFI_CAPABILITIES -> wifiCapabilities()
            RunCommand.CommandCase.GET_FRESH_WIFI_SCAN -> freshWifiScan(command.getFreshWifiScan)
            RunCommand.CommandCase.DISCONNECT_WIFI -> disconnectWifi()
            RunCommand.CommandCase.FORGET_WIFI -> forgetWifi(command.forgetWifi)
            RunCommand.CommandCase.WAIT_WIFI_CONNECTED -> waitWifiConnected(command.waitWifiConnected)
            RunCommand.CommandCase.ASSERT_WIFI -> assertWifi(command.assertWifi)
            RunCommand.CommandCase.WATCH_WIFI -> watchWifi(command.watchWifi)
            RunCommand.CommandCase.MONITOR_WIFI -> monitorWifi(command.monitorWifi)
            RunCommand.CommandCase.GET_WIFI_SCAN_DETAIL -> wifiScanDetail(command.getWifiScanDetail)
            RunCommand.CommandCase.RECONNECT_WIFI -> reconnectWifi(command.reconnectWifi)
            RunCommand.CommandCase.CYCLE_WIFI -> cycleWifi(command.cycleWifi)
            RunCommand.CommandCase.CONNECT_WIFI -> connectWifi(command.connectWifi)
            RunCommand.CommandCase.GET_IP_STATUS -> networkChecks.getIpStatus(command.getIpStatus.selector)
            RunCommand.CommandCase.PING -> networkChecks.ping(command.ping)
            RunCommand.CommandCase.TRACEROUTE -> networkChecks.traceroute(command.traceroute)
            RunCommand.CommandCase.PATH_MTU -> networkChecks.pathMtu(command.pathMtu)
            RunCommand.CommandCase.GLOBAL_IP -> networkChecks.globalIp(command.globalIp)
            RunCommand.CommandCase.WGET -> networkChecks.download(command.wget)
            RunCommand.CommandCase.RESOLVE_DNS -> networkChecks.dns(command.resolveDns)
            RunCommand.CommandCase.HTTP_CHECK -> networkChecks.http(command.httpCheck)
            RunCommand.CommandCase.COMMAND_NOT_SET -> failed("command is not set")
        }
        val elapsedMs = Duration.ofNanos(System.nanoTime() - startedAt).toMillis()
        val timedResult = result.toBuilder()
            .setElapsedMs(elapsedMs)
            .build()
        logger.debugEvent("command.executor.end", listOf(
            "command_case" to command.commandCase.name,
            "executor_elapsed_ms" to elapsedMs,
        ) + timedResult.logFields())
        return timedResult
    }

    private fun wifiStatus(): CommandResult {
        logger.info("wifi status requested")
        val status = networks.wifiStatus()
        logger.debug("wifi status observed enabled=${status.enabled} state=${status.state} active=${status.activeNetwork.ifBlank { "none" }} wifi_networks=${status.wifiNetworkCount} permissions=${status.permissionsList.joinToString(",")}")
        if (status.hasConnection()) {
            logger.debug("wifi connection ssid=${status.connection.ssid} bssid=${status.connection.bssid} rssi=${status.connection.rssiDbm} freq=${status.connection.frequencyMhz} link=${status.connection.linkSpeedMbps} tx=${status.connection.txLinkSpeedMbps} rx=${status.connection.rxLinkSpeedMbps} standard=${status.connection.wifiStandard} security=${status.connection.securityType} supplicant=${status.connection.supplicantState} network_id=${status.connection.networkId} ipv4=${status.connection.ipv4Address.ifBlank { "none" }}")
        }
        if (status.hasIpStatus()) {
            logger.debug("wifi status ip network=${status.ipStatus.networkId} iface=${status.ipStatus.interfaceName} mtu=${status.ipStatus.mtu} addresses=${status.ipStatus.addressesList.joinToString(",")} dns=${status.ipStatus.dnsServersList.joinToString(",")} dhcp=${status.ipStatus.dhcpServer.ifBlank { "none" }} routes=${status.ipStatus.routesList.joinToString(" | ")}")
        }
        return CommandResult.newBuilder()
            .setStatus(CommandResult.Status.STATUS_OK)
            .setWifiStatus(status)
            .build()
    }

    private fun wifiDiagnostics(): CommandResult {
        val diagnostics = networks.wifiDiagnostics()
        logger.debug("wifi diagnostics observed networks=${diagnostics.networksCount} scan_results=${diagnostics.scan.resultsCount} capability_fields=${diagnostics.capabilities.fieldsCount} capability_errors=${diagnostics.capabilities.errorsCount} scan_errors=${diagnostics.scan.errorsCount}")
        return CommandResult.newBuilder()
            .setStatus(CommandResult.Status.STATUS_OK)
            .setWifiDiagnostics(diagnostics)
            .build()
    }

    private fun wifiScan(command: GetWifiScan): CommandResult {
        logger.info("wifi scan requested band=${command.band}")
        val scan = networks.wifiScan(command.band)
        logger.debug("wifi scan observed results=${scan.resultsCount} fields=${scan.fieldsList.joinToString(",") { "${it.key}=${it.value}" }} errors=${scan.errorsList.joinToString(",")}")
        return CommandResult.newBuilder()
            .setStatus(CommandResult.Status.STATUS_OK)
            .setWifiScan(scan)
            .build()
    }

    private fun freshWifiScan(command: GetFreshWifiScan): CommandResult {
        val timeoutMs = WifiCommandPolicy.effectiveTimeoutMs(
            command.timeoutMs,
            WifiCommandPolicy.DEFAULT_FRESH_SCAN_TIMEOUT_MS,
        )
        logger.info("wifi fresh scan requested band=${command.band} timeout_ms=$timeoutMs")
        val scan = networks.wifiFreshScan(command.band, timeoutMs)
        val ok = WifiCommandPolicy.freshScanCompleted(scan.errorsList)
        logger.debug("wifi fresh scan observed results=${scan.resultsCount} fields=${scan.fieldsList.joinToString(",") { "${it.key}=${it.value}" }} errors=${scan.errorsList.joinToString(",")}")
        return CommandResult.newBuilder()
            .setStatus(if (ok) CommandResult.Status.STATUS_OK else CommandResult.Status.STATUS_FAILED)
            .setMessage(if (ok) "fresh scan completed" else "fresh scan incomplete")
            .setWifiScan(scan)
            .build()
    }

    private fun wifiScanDetail(command: GetWifiScanDetail): CommandResult {
        logger.info("wifi scan detail requested target=${command.target} band=${command.band}")
        val detail = networks.wifiScanDetail(command.target, command.band)
        val ok = WifiCommandPolicy.scanDetailMatched(detail.resultsCount, detail.errorsList)
        logger.debug("wifi scan detail observed target=${command.target} results=${detail.resultsCount} fields=${detail.fieldsList.joinToString(",") { "${it.key}=${it.value}" }} errors=${detail.errorsList.joinToString(",")}")
        return CommandResult.newBuilder()
            .setStatus(if (ok) CommandResult.Status.STATUS_OK else CommandResult.Status.STATUS_FAILED)
            .setMessage(if (ok) "scan detail matched" else "scan detail not matched")
            .setWifiScanDetail(detail)
            .build()
    }

    private fun wifiCapabilities(): CommandResult {
        logger.info("wifi capabilities requested")
        val capabilities = networks.wifiCapabilities()
        logger.debug("wifi capabilities observed bands=${capabilities.supportedBandsList.joinToString(",")} standards=${capabilities.supportedStandardsList.joinToString(",")} security=${capabilities.supportedSecurityModesList.joinToString(",")} features=${capabilities.supportedFeaturesCount} errors=${capabilities.errorsList.joinToString(",")}")
        return CommandResult.newBuilder()
            .setStatus(CommandResult.Status.STATUS_OK)
            .setWifiCapabilities(capabilities)
            .build()
    }

    /**
     * Configures Wi-Fi, then waits until Android reports a matching network with IP.
     *
     * Setup success alone is not enough: addNetwork/enableNetwork can succeed
     * before association or DHCP are complete.
     */
    private fun connectWifi(command: ConnectWifi): CommandResult {
        val timeoutMs = WifiCommandPolicy.effectiveTimeoutMs(
            command.timeoutMs,
            WifiCommandPolicy.DEFAULT_CONNECT_TIMEOUT_MS,
        )
        logger.info("wifi connect requested ssid=${command.ssid} bssid=${command.bssid.ifBlank { "*" }} security=${command.security} band=${command.band} mac_randomization=${command.macRandomization} timeout_ms=$timeoutMs passphrase_present=${command.passphrase.isNotBlank()}")
        logger.debug("wifi connect parameters ssid_len=${command.ssid.length} passphrase_len=${command.passphrase.length} security_number=${command.securityValue} bssid=${command.bssid.ifBlank { "*" }} band=${command.band} mac_randomization=${command.macRandomization} timeout_ms=$timeoutMs")

        val setup = wifi.connect(command)
        if (setup.error != null) {
            logger.warn("wifi setup failed ssid=${command.ssid} error=${setup.error}")
            return CommandResult.newBuilder()
                .setStatus(CommandResult.Status.STATUS_FAILED)
                .setMessage(setup.error)
                .setConnectWifi(ConnectWifiResult.newBuilder()
                    .setSsid(command.ssid)
                    .setConnected(false)
                    .setMessage(setup.error)
                    .build())
            .build()
        }
        logger.debug("wifi setup accepted ssid=${command.ssid} network_id=${setup.networkId} previous_network_id=${setup.previousNetworkId}")

        val assertion = waitForExpectation(
            WifiCommandPolicy.connectExpectation(command),
            timeoutMs,
        )
        val connected = assertion.passed
        val message = if (connected) "connected" else "network not available or did not match requested parameters"
        val ip = if (assertion.hasStatus() && assertion.status.hasIpStatus()) assertion.status.ipStatus else null
        if (ip != null) {
            logger.debug("wifi connect network selected ssid=${command.ssid} network=${ip.networkId} iface=${ip.interfaceName} addresses=${ip.addressesList.joinToString(",")} dns=${ip.dnsServersList.joinToString(",")} validated=${ip.validated} internet=${ip.internet}")
            logger.debug("wifi connect routes ssid=${command.ssid} routes=${ip.routesList.joinToString(" | ")} dhcp=${ip.dhcpServer.ifBlank { "none" }} mtu=${ip.mtu}")
        } else {
            logger.warn("wifi connect timeout or assertion failed ssid=${command.ssid} timeout_ms=$timeoutMs checks=${assertion.checksList.joinToString(",") { "${it.key}:${it.passed}:${it.actual}" }}")
        }
        val result = ConnectWifiResult.newBuilder()
            .setSsid(command.ssid)
            .setConnected(connected)
            .setMessage(message)
        if (ip != null) {
            result.ipStatus = ip
        }

        return CommandResult.newBuilder()
            .setStatus(if (connected) CommandResult.Status.STATUS_OK else CommandResult.Status.STATUS_FAILED)
            .setMessage(message)
            .setConnectWifi(result)
            .build()
    }

    private fun disconnectWifi(): CommandResult {
        logger.info("wifi disconnect requested")
        val operation = wifi.disconnect()
        val result = wifiOperationResult(operation, networks.wifiStatus())
        return CommandResult.newBuilder()
            .setStatus(if (result.ok) CommandResult.Status.STATUS_OK else CommandResult.Status.STATUS_FAILED)
            .setMessage(result.message)
            .setWifiOperation(result)
            .build()
    }

    private fun forgetWifi(command: ForgetWifi): CommandResult {
        logger.info("wifi forget requested target=${command.target}")
        val operation = wifi.forget(command.target)
        val result = wifiOperationResult(operation, networks.wifiStatus())
        return CommandResult.newBuilder()
            .setStatus(if (result.ok) CommandResult.Status.STATUS_OK else CommandResult.Status.STATUS_FAILED)
            .setMessage(result.message)
            .setWifiOperation(result)
            .build()
    }

    private fun reconnectWifi(command: ReconnectWifi): CommandResult {
        val timeoutMs = WifiCommandPolicy.effectiveTimeoutMs(
            command.timeoutMs,
            WifiCommandPolicy.DEFAULT_RECONNECT_TIMEOUT_MS,
        )
        logger.info("wifi reconnect command requested timeout_ms=$timeoutMs")
        val operation = wifi.reconnect()
        val assertion = waitForExpectation(WifiExpectation(requireIp = true), timeoutMs)
        val status = if (assertion.hasStatus()) assertion.status else networks.wifiStatus()
        val result = wifiOperationResult(
            operation.copy(
                ok = operation.ok && assertion.passed,
                message = if (operation.ok && assertion.passed) "reconnected" else "reconnect did not reach connected state",
                fields = operation.fields + assertion.checksList.map { "check_${it.key}" to "${it.passed}:${it.actual}" },
                errors = operation.errors + assertion.errorsList,
            ),
            status,
        )
        return CommandResult.newBuilder()
            .setStatus(if (result.ok) CommandResult.Status.STATUS_OK else CommandResult.Status.STATUS_FAILED)
            .setMessage(result.message)
            .setWifiOperation(result)
            .build()
    }

    private fun waitWifiConnected(command: WaitWifiConnected): CommandResult {
        val timeoutMs = WifiCommandPolicy.effectiveTimeoutMs(
            command.timeoutMs,
            WifiCommandPolicy.DEFAULT_WAIT_TIMEOUT_MS,
        )
        logger.info("wifi wait connected requested ssid=${command.ssid.ifBlank { "*" }} bssid=${command.bssid.ifBlank { "*" }} security=${command.security} band=${command.band} require_ip=${command.requireIp} require_validated=${command.requireValidated} timeout_ms=$timeoutMs")
        val result = waitForExpectation(
            WifiCommandPolicy.waitExpectation(command),
            timeoutMs,
        )
        return CommandResult.newBuilder()
            .setStatus(if (result.passed) CommandResult.Status.STATUS_OK else CommandResult.Status.STATUS_FAILED)
            .setMessage(if (result.passed) "wifi condition reached" else "wifi condition timeout")
            .setWifiAssert(result)
            .build()
    }

    private fun assertWifi(command: AssertWifi): CommandResult {
        val timeoutMs = WifiCommandPolicy.effectiveTimeoutMs(
            command.timeoutMs,
            WifiCommandPolicy.DEFAULT_ASSERT_TIMEOUT_MS,
        )
        logger.info("wifi assert requested ssid=${command.ssid.ifBlank { "*" }} bssid=${command.bssid.ifBlank { "*" }} security=${command.security} band=${command.band} require_ip=${command.requireIp} require_validated=${command.requireValidated} timeout_ms=$timeoutMs")
        val result = waitForExpectation(
            WifiCommandPolicy.assertExpectation(command),
            timeoutMs,
        )
        return CommandResult.newBuilder()
            .setStatus(if (result.passed) CommandResult.Status.STATUS_OK else CommandResult.Status.STATUS_FAILED)
            .setMessage(if (result.passed) "wifi assertion passed" else "wifi assertion failed")
            .setWifiAssert(result)
            .build()
    }

    private fun watchWifi(command: WatchWifi): CommandResult {
        val durationMs = WifiCommandPolicy.effectiveTimeoutMs(
            command.durationMs,
            WifiCommandPolicy.DEFAULT_WATCH_DURATION_MS,
        )
        val intervalMs = WifiCommandPolicy.effectiveTimeoutMs(
            command.intervalMs,
            WifiCommandPolicy.DEFAULT_WATCH_INTERVAL_MS,
        )
        logger.info("wifi watch requested duration_ms=$durationMs interval_ms=$intervalMs")
        val result = networks.wifiWatch(durationMs, intervalMs)
        return CommandResult.newBuilder()
            .setStatus(if (result.errorsCount == 0) CommandResult.Status.STATUS_OK else CommandResult.Status.STATUS_FAILED)
            .setMessage("wifi watch samples=${result.samplesCount}")
            .setWifiWatch(result)
            .build()
    }

    private fun monitorWifi(command: MonitorWifi): CommandResult {
        val durationMs = WifiCommandPolicy.effectiveTimeoutMs(
            command.durationMs,
            WifiCommandPolicy.DEFAULT_WATCH_DURATION_MS,
        )
        val intervalMs = WifiCommandPolicy.effectiveTimeoutMs(
            command.intervalMs,
            WifiCommandPolicy.DEFAULT_WATCH_INTERVAL_MS,
        )
        logger.info("wifi monitor requested duration_ms=$durationMs interval_ms=$intervalMs")
        val result = networks.wifiMonitor(durationMs, intervalMs)
        return CommandResult.newBuilder()
            .setStatus(if (result.errorsCount == 0) CommandResult.Status.STATUS_OK else CommandResult.Status.STATUS_FAILED)
            .setMessage("wifi monitor events=${result.eventsCount}")
            .setWifiMonitor(result)
            .build()
    }

    /**
     * Runs repeated connect/probe/disconnect steps as one controller command.
     *
     * This is intentionally still local orchestration because each step depends
     * on fresh Android network state, but pass/fail rules are delegated to
     * [WifiCommandPolicy].
     */
    private fun cycleWifi(command: CycleWifi): CommandResult {
        val count = WifiCommandPolicy.cycleCount(command)
        val pauseMs = WifiCommandPolicy.cyclePauseMs(command)
        val builder = WifiCycleResult.newBuilder()
            .setRequestedCount(count)
        var passed = 0
        logger.info("wifi cycle requested ssid=${command.connect.ssid} count=$count security=${command.connect.security} bssid=${command.connect.bssid.ifBlank { "*" }} band=${command.connect.band} mac_randomization=${command.connect.macRandomization} ping=${command.pingHost.ifBlank { "none" }} http=${command.httpUrl.ifBlank { "none" }} forget_after_each=${command.forgetAfterEach} pause_ms=$pauseMs")
        for (index in 1..count) {
            throwIfInterrupted()
            val started = System.nanoTime()
            logger.info("wifi cycle step begin index=$index/$count ssid=${command.connect.ssid}")
            val step = WifiCycleStep.newBuilder().setIndex(index)
            var stepConnected = false
            var pingOk = false
            var httpOk = false
            val setup = wifi.connect(command.connect)
            if (setup.error != null) {
                step.addErrors("setup=${setup.error}")
                step.setConnect(ConnectWifiResult.newBuilder()
                    .setSsid(command.connect.ssid)
                    .setConnected(false)
                    .setMessage(setup.error)
                    .build())
            } else {
                val assertion = waitForExpectation(
                    WifiCommandPolicy.connectExpectation(command.connect),
                    WifiCommandPolicy.effectiveTimeoutMs(
                        command.connect.timeoutMs,
                        WifiCommandPolicy.DEFAULT_CONNECT_TIMEOUT_MS,
                    ),
                )
                stepConnected = assertion.passed
                step.connected = stepConnected
                val connectResult = ConnectWifiResult.newBuilder()
                    .setSsid(command.connect.ssid)
                    .setConnected(assertion.passed)
                    .setMessage(if (assertion.passed) "connected" else "connect assertion failed")
                if (assertion.hasStatus() && assertion.status.hasIpStatus()) {
                    connectResult.ipStatus = assertion.status.ipStatus
                }
                step.connect = connectResult.build()
                if (!assertion.passed) {
                    assertion.checksList.filterNot { it.passed }.forEach {
                        step.addErrors("check_${it.key}=expected:${it.expected} actual:${it.actual}")
                    }
                }
                if (assertion.passed && command.pingHost.isNotBlank()) {
                    val pingResult = networkChecks.ping(Ping.newBuilder()
                        .setHost(command.pingHost)
                        .setCount(3)
                        .setTimeoutMs(9000)
                        .setSelector(NetworkSelector.newBuilder()
                            .setSsid(command.connect.ssid)
                            .build())
                        .build())
                    pingOk = pingResult.status == CommandResult.Status.STATUS_OK
                    step.pingOk = pingOk
                    if (pingResult.hasPing()) step.ping = pingResult.ping
                    if (!pingOk) step.addErrors("ping=${pingResult.message}")
                }
                if (assertion.passed && command.httpUrl.isNotBlank()) {
                    val httpResult = networkChecks.http(HttpCheck.newBuilder()
                        .setUrl(command.httpUrl)
                        .setExpectedStatus(200)
                        .setTimeoutMs(5000)
                        .setSelector(NetworkSelector.newBuilder()
                            .setSsid(command.connect.ssid)
                            .build())
                        .build())
                    httpOk = httpResult.status == CommandResult.Status.STATUS_OK
                    step.httpOk = httpOk
                    if (httpResult.hasHttpCheck()) step.http = httpResult.httpCheck
                    if (!httpOk) step.addErrors("http=${httpResult.message}")
                }
            }
            val stepPassed = WifiCommandPolicy.cycleStepPassed(
                connected = stepConnected,
                pingRequested = command.pingHost.isNotBlank(),
                pingOk = pingOk,
                httpRequested = command.httpUrl.isNotBlank(),
                httpOk = httpOk,
            )
            if (stepPassed) passed += 1
            if (command.forgetAfterEach) {
                val forget = wifi.forget(command.connect.ssid)
                if (!forget.ok) step.addErrors("forget=${forget.message}")
            } else {
                val disconnect = wifi.disconnect()
                if (!disconnect.ok) step.addErrors("disconnect=${disconnect.message}")
            }
            step.elapsedMs = Duration.ofNanos(System.nanoTime() - started).toMillis()
            builder.addSteps(step)
            builder.completedCount = index
            builder.passedCount = passed
            logger.info("wifi cycle step end index=$index connected=$stepConnected ping_ok=$pingOk http_ok=$httpOk elapsed_ms=${step.elapsedMs} errors=${step.errorsList.joinToString(",")}")
            if (index < count && pauseMs > 0) {
                Thread.sleep(pauseMs.toLong())
            }
        }
        val result = builder.build()
        val ok = WifiCommandPolicy.cyclePassed(result)
        return CommandResult.newBuilder()
            .setStatus(if (ok) CommandResult.Status.STATUS_OK else CommandResult.Status.STATUS_FAILED)
            .setMessage("wifi cycle passed=${result.passedCount}/${result.requestedCount}")
            .setWifiCycle(result)
            .build()
    }

    private fun wifiOperationResult(operation: WifiConnector.Operation, status: WifiStatus): WifiOperationResult {
        return WifiOperationResult.newBuilder()
            .setOperation(operation.operation)
            .setOk(operation.ok)
            .setMessage(operation.message)
            .addAllFields(operation.fields.map { diagnosticField(it.first, it.second) })
            .setStatus(status)
            .addAllErrors(operation.errors)
            .build()
    }

    /**
     * Polls Wi-Fi state until the expectation passes or the timeout expires.
     *
     * The first evaluation always happens immediately, which makes assert
     * commands deterministic when timeout is zero.
     */
    private fun waitForExpectation(expectation: WifiExpectation, timeoutMs: Int): WifiAssertResult {
        val started = System.nanoTime()
        val deadline = System.currentTimeMillis() + timeoutMs
        var last = evaluateExpectation(networks.wifiStatus(), expectation, started)
        while (!last.passed && timeoutMs > 0 && System.currentTimeMillis() < deadline) {
            Thread.sleep(500)
            last = evaluateExpectation(networks.wifiStatus(), expectation, started)
        }
        if (!last.passed && timeoutMs > 0) {
            return last.toBuilder()
                .addErrors("timeout=${timeoutMs}ms")
                .build()
        }
        return last
    }

    private fun evaluateExpectation(status: WifiStatus, expectation: WifiExpectation, started: Long): WifiAssertResult {
        val result = WifiExpectationEvaluator.evaluate(
            status = status,
            expectation = expectation,
            elapsedMs = Duration.ofNanos(System.nanoTime() - started).toMillis(),
        )
        logger.debug("wifi expectation evaluated passed=${result.passed} checks=${result.checksList.joinToString(",") { "${it.key}:${it.passed}:${it.actual}" }}")
        return result
    }

    private fun diagnosticField(key: String, value: Any?): DiagnosticField {
        return DiagnosticField.newBuilder()
            .setKey(key)
            .setValue(value?.toString().orEmpty())
            .build()
    }

    private fun failed(message: String): CommandResult {
        logger.warn("command failed before execution result message=$message")
        return CommandResult.newBuilder()
            .setStatus(CommandResult.Status.STATUS_FAILED)
            .setMessage(message)
            .build()
    }

    private fun throwIfInterrupted() {
        if (Thread.currentThread().isInterrupted) {
            throw InterruptedException("command interrupted")
        }
    }

}
