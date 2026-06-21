package io.dropcheck.agent

import io.dropcheck.agent.grpc.AgentFrame
import io.dropcheck.agent.grpc.AssertWifi
import io.dropcheck.agent.grpc.CommandResult
import io.dropcheck.agent.grpc.ConnectWifi
import io.dropcheck.agent.grpc.ControllerFrame
import io.dropcheck.agent.grpc.CycleWifi
import io.dropcheck.agent.grpc.DnsAnswer
import io.dropcheck.agent.grpc.GetFreshWifiScan
import io.dropcheck.agent.grpc.GetWifiScan
import io.dropcheck.agent.grpc.GetWifiScanDetail
import io.dropcheck.agent.grpc.GlobalIp
import io.dropcheck.agent.grpc.GlobalIpResult
import io.dropcheck.agent.grpc.HttpCheck
import io.dropcheck.agent.grpc.HttpCheckResult
import io.dropcheck.agent.grpc.NetworkSelector
import io.dropcheck.agent.grpc.PathMtu
import io.dropcheck.agent.grpc.PathMtuResult
import io.dropcheck.agent.grpc.Ping
import io.dropcheck.agent.grpc.PingResult
import io.dropcheck.agent.grpc.ResolveDns
import io.dropcheck.agent.grpc.ResolveDnsResult
import io.dropcheck.agent.grpc.RunCommand
import io.dropcheck.agent.grpc.Traceroute
import io.dropcheck.agent.grpc.TracerouteResult
import io.dropcheck.agent.grpc.WaitWifiConnected
import io.dropcheck.agent.grpc.Wget
import io.dropcheck.agent.grpc.WgetResult
import java.net.URL
import java.util.Locale

internal fun ControllerFrame.logFields(): List<Pair<String, Any?>> {
    return buildList {
        add("seq" to seq)
        add("frame_session_id" to sessionId)
        add("command_id" to commandId)
        add("body_case" to bodyCase.name)
        when (bodyCase) {
            ControllerFrame.BodyCase.RUN_COMMAND -> addAll(runCommand.logFields())
            ControllerFrame.BodyCase.CANCEL_COMMAND -> add("cancel_reason" to cancelCommand.reason)
            ControllerFrame.BodyCase.HEARTBEAT -> add("heartbeat_unix_time_ms" to heartbeat.unixTimeMs)
            ControllerFrame.BodyCase.BODY_NOT_SET -> Unit
        }
    }
}

internal fun AgentFrame.logFields(): List<Pair<String, Any?>> {
    return buildList {
        add("seq" to seq)
        add("frame_session_id" to sessionId)
        add("command_id" to commandId)
        add("body_case" to bodyCase.name)
        when (bodyCase) {
            AgentFrame.BodyCase.HELLO -> {
                add("token_present" to hello.token.isNotBlank())
                add("package_name" to hello.packageName)
                add("app_version" to hello.appVersion)
                add("controller_agent_id" to hello.controllerAgentId)
                add("adb_serial" to hello.adbSerial)
                add("capabilities_count" to hello.capabilitiesCount)
                add("capabilities" to hello.capabilitiesList)
                if (hello.hasDevice()) {
                    add("device_manufacturer" to hello.device.manufacturer)
                    add("device_model" to hello.device.model)
                    add("device_name" to hello.device.device)
                    add("device_sdk" to hello.device.sdk)
                    add("device_release" to hello.device.release)
                }
            }
            AgentFrame.BodyCase.ACCEPTED -> add("accepted_command_name" to accepted.commandName)
            AgentFrame.BodyCase.LOG -> {
                add("log_level" to log.level.name)
                add("log_unix_time_ms" to log.unixTimeMs)
                add("log_message_len" to log.message.length)
                add("log_message" to StructuredLog.preview(log.message, 800))
            }
            AgentFrame.BodyCase.RESULT -> addAll(result.logFields())
            AgentFrame.BodyCase.ERROR -> {
                add("error_message" to error.message)
                add("error_detail_len" to error.detail.length)
                add("error_detail" to StructuredLog.preview(error.detail, 800))
            }
            AgentFrame.BodyCase.HEARTBEAT -> add("heartbeat_unix_time_ms" to heartbeat.unixTimeMs)
            AgentFrame.BodyCase.BODY_NOT_SET -> Unit
        }
    }
}

internal fun RunCommand.logFields(): List<Pair<String, Any?>> {
    return buildList {
        add("label" to safeLabel())
        add("command_case" to commandCase.name)
        when (commandCase) {
            RunCommand.CommandCase.GET_WIFI_STATUS -> Unit
            RunCommand.CommandCase.GET_WIFI_DIAGNOSTICS -> Unit
            RunCommand.CommandCase.GET_WIFI_CAPABILITIES -> Unit
            RunCommand.CommandCase.DISCONNECT_WIFI -> Unit
            RunCommand.CommandCase.GET_WIFI_SCAN -> addAll(getWifiScan.logFields())
            RunCommand.CommandCase.GET_FRESH_WIFI_SCAN -> addAll(getFreshWifiScan.logFields())
            RunCommand.CommandCase.GET_WIFI_SCAN_DETAIL -> addAll(getWifiScanDetail.logFields())
            RunCommand.CommandCase.CONNECT_WIFI -> addAll(connectWifi.logFields())
            RunCommand.CommandCase.FORGET_WIFI -> add("target" to forgetWifi.target)
            RunCommand.CommandCase.WAIT_WIFI_CONNECTED -> addAll(waitWifiConnected.logFields())
            RunCommand.CommandCase.ASSERT_WIFI -> addAll(assertWifi.logFields("assert_"))
            RunCommand.CommandCase.MONITOR_WIFI -> {
                add("duration_ms" to monitorWifi.durationMs)
                add("interval_ms" to monitorWifi.intervalMs)
            }
            RunCommand.CommandCase.RECONNECT_WIFI -> add("timeout_ms" to reconnectWifi.timeoutMs)
            RunCommand.CommandCase.CYCLE_WIFI -> addAll(cycleWifi.logFields())
            RunCommand.CommandCase.GET_IP_STATUS -> addAll(getIpStatus.selector.logFields())
            RunCommand.CommandCase.PING -> addAll(ping.logFields())
            RunCommand.CommandCase.TRACEROUTE -> addAll(traceroute.logFields())
            RunCommand.CommandCase.PATH_MTU -> addAll(pathMtu.logFields())
            RunCommand.CommandCase.GLOBAL_IP -> addAll(globalIp.logFields())
            RunCommand.CommandCase.WGET -> addAll(wget.logFields())
            RunCommand.CommandCase.RESOLVE_DNS -> addAll(resolveDns.logFields())
            RunCommand.CommandCase.HTTP_CHECK -> addAll(httpCheck.logFields())
            RunCommand.CommandCase.EDIT_STANDALONE_CONFIG,
            RunCommand.CommandCase.GET_STANDALONE_CONFIG,
            RunCommand.CommandCase.GET_STANDALONE_STATUS,
            RunCommand.CommandCase.LIST_STANDALONE_RUNS,
            RunCommand.CommandCase.GET_STANDALONE_RUN,
            RunCommand.CommandCase.CLEAR_STANDALONE_RUNS,
            RunCommand.CommandCase.RUN_STANDALONE_ONCE -> add("legacy_command" to true)
            RunCommand.CommandCase.COMMAND_NOT_SET -> Unit
        }
    }
}

internal fun CommandResult.logFields(): List<Pair<String, Any?>> {
    return buildList {
        add("status" to status.name)
        add("payload_case" to payloadCase.name)
        add("message" to message)
        add("elapsed_ms" to elapsedMs)
        when (payloadCase) {
            CommandResult.PayloadCase.WIFI_STATUS -> {
                add("wifi_enabled" to wifiStatus.enabled)
                add("wifi_state" to wifiStatus.state)
                add("wifi_active_network" to wifiStatus.activeNetwork)
                add("wifi_network_count" to wifiStatus.wifiNetworkCount)
                if (wifiStatus.hasConnection()) {
                    add("wifi_ssid" to wifiStatus.connection.ssid)
                    add("wifi_bssid" to wifiStatus.connection.bssid)
                    add("wifi_rssi_dbm" to wifiStatus.connection.rssiDbm)
                    add("wifi_frequency_mhz" to wifiStatus.connection.frequencyMhz)
                    add("wifi_standard" to wifiStatus.connection.wifiStandard)
                    add("wifi_channel_width" to wifiStatus.connection.channelWidth)
                    add("wifi_security" to wifiStatus.connection.securityType)
                    add("wifi_ipv4" to wifiStatus.connection.ipv4Address)
                }
                if (wifiStatus.hasIpStatus()) {
                    add("iface" to wifiStatus.ipStatus.interfaceName)
                    add("addresses" to wifiStatus.ipStatus.addressesList)
                    add("dns_servers" to wifiStatus.ipStatus.dnsServersList)
                    add("validated" to wifiStatus.ipStatus.validated)
                    add("internet" to wifiStatus.ipStatus.internet)
                }
            }
            CommandResult.PayloadCase.CONNECT_WIFI -> {
                add("ssid" to connectWifi.ssid)
                add("connected" to connectWifi.connected)
                add("connect_message" to connectWifi.message)
                if (connectWifi.hasIpStatus()) {
                    add("iface" to connectWifi.ipStatus.interfaceName)
                    add("addresses" to connectWifi.ipStatus.addressesList)
                    add("dns_servers" to connectWifi.ipStatus.dnsServersList)
                }
            }
            CommandResult.PayloadCase.IP_STATUS -> {
                add("network_id" to ipStatus.networkId)
                add("iface" to ipStatus.interfaceName)
                add("addresses" to ipStatus.addressesList)
                add("dns_servers" to ipStatus.dnsServersList)
                add("routes_count" to ipStatus.routesCount)
                add("validated" to ipStatus.validated)
                add("internet" to ipStatus.internet)
            }
            CommandResult.PayloadCase.PING -> addAll(ping.logFields())
            CommandResult.PayloadCase.RESOLVE_DNS -> addAll(resolveDns.logFields())
            CommandResult.PayloadCase.HTTP_CHECK -> addAll(httpCheck.logFields())
            CommandResult.PayloadCase.WIFI_DIAGNOSTICS -> {
                add("networks_count" to wifiDiagnostics.networksCount)
                add("scan_results_count" to wifiDiagnostics.scan.resultsCount)
                add("capability_fields_count" to wifiDiagnostics.capabilities.fieldsCount)
                add("capability_errors_count" to wifiDiagnostics.capabilities.errorsCount)
                add("scan_errors_count" to wifiDiagnostics.scan.errorsCount)
            }
            CommandResult.PayloadCase.WIFI_SCAN -> {
                add("scan_results_count" to wifiScan.resultsCount)
                add("scan_fields_count" to wifiScan.fieldsCount)
                add("scan_errors_count" to wifiScan.errorsCount)
                add("scan_errors" to wifiScan.errorsList)
            }
            CommandResult.PayloadCase.WIFI_CAPABILITIES -> {
                add("capability_fields_count" to wifiCapabilities.fieldsCount)
                add("supported_bands" to wifiCapabilities.supportedBandsList)
                add("supported_standards" to wifiCapabilities.supportedStandardsList)
                add("supported_security_modes" to wifiCapabilities.supportedSecurityModesList)
                add("supported_features_count" to wifiCapabilities.supportedFeaturesCount)
                add("errors_count" to wifiCapabilities.errorsCount)
                add("errors" to wifiCapabilities.errorsList)
            }
            CommandResult.PayloadCase.WIFI_OPERATION -> {
                add("operation" to wifiOperation.operation)
                add("ok" to wifiOperation.ok)
                add("operation_message" to wifiOperation.message)
                add("fields_count" to wifiOperation.fieldsCount)
                add("errors_count" to wifiOperation.errorsCount)
                add("errors" to wifiOperation.errorsList)
            }
            CommandResult.PayloadCase.WIFI_ASSERT -> {
                add("passed" to wifiAssert.passed)
                add("checks_count" to wifiAssert.checksCount)
                add("errors_count" to wifiAssert.errorsCount)
                add("errors" to wifiAssert.errorsList)
                add("elapsed_ms" to wifiAssert.elapsedMs)
            }
            CommandResult.PayloadCase.WIFI_MONITOR -> {
                add("events_count" to wifiMonitor.eventsCount)
                add("errors_count" to wifiMonitor.errorsCount)
                add("errors" to wifiMonitor.errorsList)
            }
            CommandResult.PayloadCase.WIFI_SCAN_DETAIL -> {
                add("target" to wifiScanDetail.target)
                add("scan_results_count" to wifiScanDetail.resultsCount)
                add("scan_fields_count" to wifiScanDetail.fieldsCount)
                add("scan_errors_count" to wifiScanDetail.errorsCount)
                add("scan_errors" to wifiScanDetail.errorsList)
            }
            CommandResult.PayloadCase.WIFI_CYCLE -> {
                add("requested_count" to wifiCycle.requestedCount)
                add("completed_count" to wifiCycle.completedCount)
                add("passed_count" to wifiCycle.passedCount)
                add("steps_count" to wifiCycle.stepsCount)
                add("errors_count" to wifiCycle.errorsCount)
                add("errors" to wifiCycle.errorsList)
            }
            CommandResult.PayloadCase.TRACEROUTE -> addAll(traceroute.logFields())
            CommandResult.PayloadCase.PATH_MTU -> addAll(pathMtu.logFields())
            CommandResult.PayloadCase.GLOBAL_IP -> addAll(globalIp.logFields())
            CommandResult.PayloadCase.WGET -> addAll(wget.logFields())
            CommandResult.PayloadCase.STANDALONE_CONFIG,
            CommandResult.PayloadCase.STANDALONE_STATUS,
            CommandResult.PayloadCase.STANDALONE_RUNS,
            CommandResult.PayloadCase.STANDALONE_RUN,
            CommandResult.PayloadCase.STANDALONE_CLEAR -> add("legacy_payload" to true)
            CommandResult.PayloadCase.PAYLOAD_NOT_SET -> Unit
        }
    }
}

internal fun RunCommand.safeLabel(): String {
    val fallback = commandCase.name.lowercase(Locale.US)
    var rendered = label.ifBlank { fallback }
    val secrets = buildList {
        if (commandCase == RunCommand.CommandCase.CONNECT_WIFI) add(connectWifi.passphrase)
        if (commandCase == RunCommand.CommandCase.CYCLE_WIFI) add(cycleWifi.connect.passphrase)
    }
    for (secret in secrets) {
        if (secret.isNotBlank()) rendered = rendered.replace(secret, "<redacted>")
    }
    return rendered
}

internal fun Ping.logFields(): List<Pair<String, Any?>> = buildList {
    add("host" to host)
    add("count" to count)
    add("timeout_ms" to timeoutMs)
    add("size_bytes" to sizeBytes)
    add("family" to family.name)
    addAll(selector.logFields())
}

internal fun Traceroute.logFields(): List<Pair<String, Any?>> = buildList {
    add("host" to host)
    add("max_hops" to maxHops)
    add("timeout_ms" to timeoutMs)
    add("size_bytes" to sizeBytes)
    add("family" to family.name)
    addAll(selector.logFields())
}

internal fun PathMtu.logFields(): List<Pair<String, Any?>> = buildList {
    add("host" to host)
    add("timeout_ms" to timeoutMs)
    add("min_mtu_bytes" to minMtuBytes)
    add("max_mtu_bytes" to maxMtuBytes)
    add("family" to family.name)
    addAll(selector.logFields())
}

internal fun GlobalIp.logFields(): List<Pair<String, Any?>> = buildList {
    add("family" to family.name)
    add("timeout_ms" to timeoutMs)
    addAll(selector.logFields())
}

internal fun Wget.logFields(): List<Pair<String, Any?>> = buildList {
    add("url" to url)
    addAll(urlFields(url))
    add("timeout_ms" to timeoutMs)
    addAll(selector.logFields())
}

internal fun ResolveDns.logFields(): List<Pair<String, Any?>> = buildList {
    add("name" to name)
    add("qtypes" to qtypesList.map { it.name })
    add("timeout_ms" to timeoutMs)
    addAll(selector.logFields())
}

internal fun HttpCheck.logFields(): List<Pair<String, Any?>> = buildList {
    add("url" to url)
    addAll(urlFields(url))
    add("expected_status" to expectedStatus)
    add("timeout_ms" to timeoutMs)
    addAll(selector.logFields())
}

internal fun PingResult.logFields(): List<Pair<String, Any?>> = buildList {
    add("host" to host)
    add("count" to count)
    add("transmitted" to transmitted)
    add("received" to received)
    add("packet_loss_percent" to packetLossPercent)
    add("min_ms" to minMs)
    add("avg_ms" to avgMs)
    add("max_ms" to maxMs)
    add("elapsed_ms" to elapsedMs)
    add("exit_code" to exitCode)
    add("iface" to interfaceName)
    add("size_bytes" to sizeBytes)
    add("output_bytes" to output.length)
    add("output_preview" to StructuredLog.preview(output))
}

internal fun TracerouteResult.logFields(): List<Pair<String, Any?>> = buildList {
    add("host" to host)
    add("max_hops" to maxHops)
    add("size_bytes" to sizeBytes)
    add("elapsed_ms" to elapsedMs)
    add("exit_code" to exitCode)
    add("iface" to interfaceName)
    add("executable" to executable)
    add("error" to error)
    add("output_bytes" to output.length)
    add("output_preview" to StructuredLog.preview(output))
}

internal fun PathMtuResult.logFields(): List<Pair<String, Any?>> = buildList {
    add("host" to host)
    add("discovered" to discovered)
    add("path_mtu_bytes" to pathMtuBytes)
    add("payload_size_bytes" to payloadSizeBytes)
    add("min_mtu_bytes" to minMtuBytes)
    add("max_mtu_bytes" to maxMtuBytes)
    add("ip_overhead_bytes" to ipOverheadBytes)
    add("elapsed_ms" to elapsedMs)
    add("iface" to interfaceName)
    add("probes_count" to probesCount)
    add("error" to error)
}

internal fun GlobalIpResult.logFields(): List<Pair<String, Any?>> = buildList {
    add("service" to service)
    add("requested_family" to requestedFamily.name)
    add("elapsed_ms" to elapsedMs)
    add("iface" to interfaceName)
    add("addresses_count" to addressesCount)
    add("addresses" to addressesList.map { "${it.family.name}:${it.ip}:${it.global}:${it.status}:${it.error}" })
    add("error" to error)
}

internal fun WgetResult.logFields(): List<Pair<String, Any?>> = buildList {
    add("url" to url)
    addAll(urlFields(url))
    add("http_status" to status)
    add("content_type" to contentType)
    add("content_length" to contentLength)
    add("bytes_read" to bytesRead)
    add("elapsed_ms" to elapsedMs)
    add("throughput_bps" to throughputBps)
    add("iface" to interfaceName)
    add("error" to error)
}

internal fun ResolveDnsResult.logFields(): List<Pair<String, Any?>> = buildList {
    add("name" to name)
    add("answers_count" to answersCount)
    add("answers" to answersList.map { it.logText() })
    add("elapsed_ms" to elapsedMs)
    add("error" to error)
}

internal fun HttpCheckResult.logFields(): List<Pair<String, Any?>> = buildList {
    add("url" to url)
    addAll(urlFields(url))
    add("http_status" to status)
    add("expected_status" to expectedStatus)
    add("matched" to matched)
    add("elapsed_ms" to elapsedMs)
    add("error" to error)
}

private fun GetWifiScan.logFields(): List<Pair<String, Any?>> = listOf("band" to band.name)

private fun GetFreshWifiScan.logFields(): List<Pair<String, Any?>> = listOf(
    "band" to band.name,
    "timeout_ms" to timeoutMs,
)

private fun GetWifiScanDetail.logFields(): List<Pair<String, Any?>> = listOf(
    "target" to target,
    "band" to band.name,
)

private fun ConnectWifi.logFields(prefix: String = ""): List<Pair<String, Any?>> = buildList {
    add("${prefix}ssid" to ssid)
    add("${prefix}ssid_len" to ssid.length)
    add("${prefix}passphrase_present" to passphrase.isNotBlank())
    add("${prefix}passphrase_len" to passphrase.length)
    add("${prefix}security" to security.name)
    add("${prefix}security_number" to securityValue)
    add("${prefix}timeout_ms" to timeoutMs)
    add("${prefix}bssid" to bssid)
    add("${prefix}band" to band.name)
    add("${prefix}mac_randomization" to macRandomization.name)
}

private fun WaitWifiConnected.logFields(prefix: String = ""): List<Pair<String, Any?>> = buildList {
    add("${prefix}ssid" to ssid)
    add("${prefix}bssid" to bssid)
    add("${prefix}security" to security.name)
    add("${prefix}band" to band.name)
    add("${prefix}require_ip" to requireIp)
    add("${prefix}require_validated" to requireValidated)
    add("${prefix}timeout_ms" to timeoutMs)
}

private fun AssertWifi.logFields(prefix: String = ""): List<Pair<String, Any?>> = buildList {
    add("${prefix}ssid" to ssid)
    add("${prefix}bssid" to bssid)
    add("${prefix}security" to security.name)
    add("${prefix}band" to band.name)
    add("${prefix}require_ip" to requireIp)
    add("${prefix}require_validated" to requireValidated)
    add("${prefix}timeout_ms" to timeoutMs)
}

private fun CycleWifi.logFields(): List<Pair<String, Any?>> = buildList {
    add("count" to count)
    add("forget_after_each" to forgetAfterEach)
    add("pause_ms" to pauseMs)
    add("ping_host" to pingHost)
    add("http_url" to httpUrl)
    if (httpUrl.isNotBlank()) addAll(urlFields(httpUrl, "http_url_"))
    addAll(connect.logFields("connect_"))
}

internal fun NetworkSelector.logFields(): List<Pair<String, Any?>> {
    return listOf("selector_ssid" to ssid.ifBlank { "*" })
}

internal fun urlFields(rawUrl: String, prefix: String = "url_"): List<Pair<String, Any?>> {
    if (rawUrl.isBlank()) {
        return listOf("${prefix}parse_ok" to false)
    }
    val parsed = runCatching { URL(rawUrl) }
    val url = parsed.getOrNull()
    if (url == null) {
        return listOf(
            "${prefix}parse_ok" to false,
            "${prefix}parse_error" to (parsed.exceptionOrNull()?.javaClass?.simpleName ?: "unknown"),
        )
    }
    return listOf(
        "${prefix}parse_ok" to true,
        "${prefix}scheme" to url.protocol,
        "${prefix}host" to url.host,
        "${prefix}port" to url.port,
        "${prefix}default_port" to url.defaultPort,
        "${prefix}path" to url.path,
        "${prefix}query_present" to !url.query.isNullOrBlank(),
    )
}

private fun DnsAnswer.logText(): String {
    return "${type.name}:${address}"
}
