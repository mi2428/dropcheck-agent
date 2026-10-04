package io.dropcheck.agent

import io.dropcheck.agent.grpc.CommandResult
import io.dropcheck.agent.grpc.DiagnosticField
import io.dropcheck.agent.grpc.GetFreshWifiScan
import io.dropcheck.agent.grpc.GetWifiDiagnostics
import io.dropcheck.agent.grpc.RunCommand
import io.dropcheck.agent.grpc.WifiBand

internal fun runWifiEhtDiagnostics(
    command: AgentShellCommand.ShowWifiEht,
    execute: (RunCommand) -> CommandResult,
): CommandResult {
    val fresh = if (command.fresh) {
        val scan = GetFreshWifiScan.newBuilder().setBand(WifiBand.WIFI_BAND_ALL)
        if (command.timeoutMs > 0) scan.timeoutMs = command.timeoutMs
        val result = execute(RunCommand.newBuilder().setGetFreshWifiScan(scan).build())
        val builder = result.toBuilder()
        if (result.status != CommandResult.Status.STATUS_OK) {
            builder.message = "wifi eht fresh scan: ${result.status.name}: ${result.message}" +
                if (result.hasWifiScan()) "; scan data is reference; refresh not confirmed" else ""
        }
        if (!result.hasWifiScan()) {
            if (result.status == CommandResult.Status.STATUS_OK) {
                builder.status = CommandResult.Status.STATUS_FAILED
                builder.message = "wifi eht fresh scan: scan unavailable"
            }
            return builder.build()
        }
        builder.wifiScan = result.wifiScan.toBuilder().addFields(
            DiagnosticField.newBuilder().setKey("scan_source").setValue(
                if (result.status == CommandResult.Status.STATUS_OK) "fresh" else "cached (refresh failed)",
            ),
        ).build()
        if (result.status == CommandResult.Status.STATUS_CANCELED) return builder.build()
        builder.build()
    } else null
    val diagnostics = execute(
        RunCommand.newBuilder().setGetWifiDiagnostics(GetWifiDiagnostics.getDefaultInstance()).build(),
    )
    return diagnostics.toBuilder().apply {
        if (!diagnostics.hasWifiDiagnostics() || !diagnostics.wifiDiagnostics.hasStatus()) {
            if (status == CommandResult.Status.STATUS_OK) status = CommandResult.Status.STATUS_FAILED
            message = "wifi eht diagnostics: diagnostics or status unavailable: ${diagnostics.message}"
        }
        if (fresh != null) {
            if (diagnostics.hasWifiDiagnostics()) {
                wifiDiagnostics = diagnostics.wifiDiagnostics.toBuilder().setScan(fresh.wifiScan).build()
            }
            elapsedMs += fresh.elapsedMs
            if (fresh.status != CommandResult.Status.STATUS_OK) {
                if (status == CommandResult.Status.STATUS_OK) {
                    status = fresh.status
                } else {
                    message = "wifi eht diagnostics: ${status.name}: $message"
                }
                message = listOf(fresh.message, message).filter { it.isNotBlank() }.joinToString("; ")
            }
        }
    }.build()
}
