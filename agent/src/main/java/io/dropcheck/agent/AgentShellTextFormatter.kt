package io.dropcheck.agent

import io.dropcheck.agent.grpc.CommandResult

internal object AgentShellTextFormatter {
    fun formatStructuredResult(result: CommandResult, body: List<String>, commandTitle: String = title(result)): List<String> {
        val out = mutableListOf<String>()
        out += "$commandTitle  ${statusName(result.status).uppercase()}  ${result.elapsedMs}ms"
        if (result.status != CommandResult.Status.STATUS_OK) {
            val line = buildString {
                append("Status: ")
                append(statusName(result.status))
                if (result.message.isNotBlank()) {
                    append("  Message: ")
                    append(result.message)
                }
            }
            out += line
        }
        out += "Latency: ${result.elapsedMs}ms"
        out += "Source: android  Target: self"
        out += "Observation age: ? (metadata unavailable)"
        out += "Received locally: ${java.time.Instant.now()} (not observation time)"
        if (body.isNotEmpty()) {
            out += ""
            out += body
        }
        return out.map(AgentSafePresentation::text)
    }

    fun block(result: CommandResult, parts: List<AgentBlockPart>, color: Int, commandTitle: String = title(result)): AgentPresentationBlock {
        val header = formatStructuredResult(result, emptyList(), commandTitle).map { AgentBlockPart.Text(it) }
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
