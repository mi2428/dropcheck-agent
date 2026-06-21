package io.dropcheck.agent

import io.dropcheck.agent.grpc.CommandResult
import io.dropcheck.agent.grpc.PingResult
import io.dropcheck.agent.grpc.TracerouteResult

/** Text renderer used by Agent Shell for network probe commands. */
internal object AgentProbeRenderer {
    fun renderPing(result: PingResult, status: CommandResult.Status, message: String): List<String> {
        val out = mutableListOf(
            "Ping",
            row("host", result.host),
            row("status", statusName(status)),
            row("message", message.ifBlank { "-" }),
            row("count", result.count.toString()),
            row("payload_size", result.sizeBytes.toString()),
            row("interface", result.interfaceName.ifBlank { "default" }),
            row("exit_code", result.exitCode.toString()),
            row("elapsed", "${result.elapsedMs}ms"),
        )
        appendOutput(out, result.output)
        return out
    }

    fun renderTraceroute(result: TracerouteResult, status: CommandResult.Status, message: String): List<String> {
        val out = mutableListOf(
            "Traceroute",
            row("host", result.host),
            row("status", statusName(status)),
            row("message", message.ifBlank { "-" }),
            row("max_hops", result.maxHops.toString()),
            row("payload_size", result.sizeBytes.toString()),
            row("interface", result.interfaceName.ifBlank { "default" }),
            row("exit_code", result.exitCode.toString()),
            row("elapsed", "${result.elapsedMs}ms"),
            row("executable", result.executable.ifBlank { "unknown" }),
        )
        if (result.error.isNotBlank()) {
            out += row("error", result.error)
        }
        appendOutput(out, result.output)
        return out
    }

    private fun appendOutput(out: MutableList<String>, output: String) {
        val trimmed = output.trimEnd()
        if (trimmed.isBlank()) return
        out += ""
        out += "Output"
        out += trimmed.lineSequence().toList()
    }

    private fun row(key: String, value: String): String = "  ${key.padEnd(14)} $value"

    private fun statusName(status: CommandResult.Status): String =
        status.name.removePrefix("STATUS_").lowercase()
}
