package io.dropcheck.agent

import io.dropcheck.agent.grpc.CommandResult

internal object AgentShellTextFormatter {
    fun block(result: CommandResult, parts: List<AgentBlockPart>, color: Int, commandTitle: String = title(result)): AgentPresentationBlock {
        val header = buildList {
            add(AgentBlockPart.Text("$commandTitle  ${statusName(result.status).uppercase()}"))
            if (result.status != CommandResult.Status.STATUS_OK) {
                add(AgentBlockPart.Field("Status", statusName(result.status)))
                if (result.message.isNotBlank()) add(AgentBlockPart.Field("Message", result.message))
            }
            add(AgentBlockPart.Field("Latency", "${result.elapsedMs}ms"))
            add(AgentBlockPart.Field("Source", "android"))
            add(AgentBlockPart.Field("Target", "self"))
            add(AgentBlockPart.Field("Observation age", "? (metadata unavailable)"))
            add(AgentBlockPart.Field("Received locally", "${java.time.Instant.now()} (not observation time)"))
        }
        return AgentPresentationBlock.create(header + parts, color)
    }

    private fun title(result: CommandResult): String = when {
        result.hasWifiStatus() -> "Wi-Fi status"
        result.hasIpStatus() -> "IP status"
        result.hasWifiScan() -> "Wi-Fi scan"
        result.hasWifiScanDetail() -> "Wi-Fi scan detail"
        result.hasWifiDiagnostics() -> "Wi-Fi diagnostics"
        result.hasWifiCapabilities() -> "Wi-Fi capabilities"
        result.hasPing() -> "Ping"
        result.hasTraceroute() -> "Traceroute"
        result.hasResolveDns() -> "DNS"
        result.hasHttpCheck() -> "HTTP"
        else -> "Command"
    }

    private fun statusName(status: CommandResult.Status): String =
        status.name.removePrefix("STATUS_").lowercase()
}
