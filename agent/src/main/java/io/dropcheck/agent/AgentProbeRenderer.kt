package io.dropcheck.agent

import io.dropcheck.agent.grpc.CommandResult
import io.dropcheck.agent.grpc.PingResult
import io.dropcheck.agent.grpc.TracerouteResult

/** Text renderer used by Agent Shell for network probe commands. */
internal object AgentProbeRenderer {
    fun renderPing(result: PingResult, status: CommandResult.Status, message: String): List<AgentBlockPart> {
        val out = mutableListOf<AgentBlockPart>(
            AgentBlockPart.Text("Ping", section = true),
            row("host", result.host),
            row("status", statusName(status)),
            row("message", message.ifBlank { "-" }),
            row("count", result.count.toString()),
            row("transmitted", if (result.hasTransmitted()) result.transmitted.toString() else "?"),
            row("received", if (result.hasReceived()) result.received.toString() else "?"),
            row("packet_loss", if (result.hasPacketLossPercent()) "${result.packetLossPercent}%" else "?"),
            row("min/avg/max", listOf(
                if (result.hasMinMs()) result.minMs.toString() else "?",
                if (result.hasAvgMs()) result.avgMs.toString() else "?",
                if (result.hasMaxMs()) result.maxMs.toString() else "?",
            ).joinToString("/") + "ms"),
            row("payload_size", result.sizeBytes.toString()),
            row("interface", result.interfaceName.ifBlank { "default" }),
            row("exit_code", result.exitCode.toString()),
            row("elapsed", "${result.elapsedMs}ms"),
        )
        appendOutput(out, result.output)
        return out
    }

    fun renderTraceroute(result: TracerouteResult, status: CommandResult.Status, message: String): List<AgentBlockPart> {
        val out = mutableListOf<AgentBlockPart>(
            AgentBlockPart.Text("Traceroute", section = true),
            row("host", result.host),
            row("status", statusName(status)),
            row("message", message.ifBlank { "-" }),
            row("max_hops", result.maxHops.toString()),
            row("payload_size", result.sizeBytes.toString()),
            row("interface", result.interfaceName.ifBlank { "default" }),
            row("exit_code", result.exitCode.toString()),
            row("elapsed", "${result.elapsedMs}ms"),
            row("executable", result.executable.ifBlank { "unknown" }),
            row("reached_target", if (result.hasReachedTarget()) result.reachedTarget.toString() else "?"),
            row("observation_error", result.observationError.ifEmpty { "none" }),
        )
        if (result.error.isNotBlank()) {
            out += row("error", result.error)
        }
        if (result.hopsCount > 0) out += AgentBlockPart.Table(listOf("HOP", "HOST", "ADDRESS", "RTT(ms)", "TIMEOUT").map { AgentBlockPart.Column(it) },
            result.hopsList.map { listOf(it.index.toString(), it.hostnamesList.joinToString(","), it.addressesList.joinToString(","), it.rttMsList.joinToString(","), it.timedOut.toString()) })
        appendOutput(out, result.output)
        return out
    }

    private fun appendOutput(out: MutableList<AgentBlockPart>, output: String) {
        val trimmed = output.trimEnd()
        if (trimmed.isBlank()) return
        out += AgentBlockPart.Text("Output (native reference)", section = true)
        out += AgentBlockPart.Text(trimmed)
    }

    private fun row(key: String, value: String) = AgentBlockPart.Field(key, value, indent = 2)

    private fun statusName(status: CommandResult.Status): String =
        status.name.removePrefix("STATUS_").lowercase()
}
