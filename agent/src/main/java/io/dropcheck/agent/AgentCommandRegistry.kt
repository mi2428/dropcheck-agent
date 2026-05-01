package io.dropcheck.agent

import io.dropcheck.agent.grpc.RunCommand

/**
 * Single source of truth for controller-visible agent commands.
 *
 * The Android process should stay a transport/device proxy: it advertises only
 * the gRPC commands it can dispatch locally, while higher-level orchestration
 * remains with the controller.
 */
internal object AgentCommandRegistry {
    /**
     * Pairing between a protobuf oneof case and the capability string reported
     * in [io.dropcheck.agent.grpc.AgentHello].
     */
    data class Entry(
        val commandCase: RunCommand.CommandCase,
        val capability: String,
    )

    /**
     * Ordered command table used both for capability advertisement and drift
     * tests. Add new controller commands here when [CommandExecutor] learns to
     * dispatch them.
     */
    val entries: List<Entry> = listOf(
        Entry(RunCommand.CommandCase.GET_WIFI_STATUS, "wifi.status"),
        Entry(RunCommand.CommandCase.GET_WIFI_DIAGNOSTICS, "wifi.diagnostics"),
        Entry(RunCommand.CommandCase.GET_WIFI_SCAN, "wifi.scan"),
        Entry(RunCommand.CommandCase.GET_FRESH_WIFI_SCAN, "wifi.scan.fresh"),
        Entry(RunCommand.CommandCase.GET_WIFI_SCAN_DETAIL, "wifi.scan.detail"),
        Entry(RunCommand.CommandCase.GET_WIFI_CAPABILITIES, "wifi.capabilities"),
        Entry(RunCommand.CommandCase.CONNECT_WIFI, "wifi.connect"),
        Entry(RunCommand.CommandCase.DISCONNECT_WIFI, "wifi.disconnect"),
        Entry(RunCommand.CommandCase.FORGET_WIFI, "wifi.forget"),
        Entry(RunCommand.CommandCase.WAIT_WIFI_CONNECTED, "wifi.wait"),
        Entry(RunCommand.CommandCase.ASSERT_WIFI, "wifi.assert"),
        Entry(RunCommand.CommandCase.WATCH_WIFI, "wifi.watch"),
        Entry(RunCommand.CommandCase.MONITOR_WIFI, "wifi.monitor"),
        Entry(RunCommand.CommandCase.RECONNECT_WIFI, "wifi.reconnect"),
        Entry(RunCommand.CommandCase.CYCLE_WIFI, "wifi.cycle"),
        Entry(RunCommand.CommandCase.GET_IP_STATUS, "ip.status"),
        Entry(RunCommand.CommandCase.PING, "ping"),
        Entry(RunCommand.CommandCase.TRACEROUTE, "traceroute"),
        Entry(RunCommand.CommandCase.PATH_MTU, "path.mtu"),
        Entry(RunCommand.CommandCase.GLOBAL_IP, "global.ip"),
        Entry(RunCommand.CommandCase.WGET, "download"),
        Entry(RunCommand.CommandCase.RESOLVE_DNS, "dns"),
        Entry(RunCommand.CommandCase.HTTP_CHECK, "http.check"),
    )

    /** Capability strings sent during hello. The controller treats this as the agent's local feature set. */
    val capabilities: List<String> = entries.map { it.capability }

    /** Protobuf command cases supported by the current APK. */
    val supportedCommandCases: Set<RunCommand.CommandCase> = entries.mapTo(linkedSetOf()) {
        it.commandCase
    }
}
