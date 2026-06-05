package io.dropcheck.agent

import io.dropcheck.agent.grpc.CommandResult

internal object AgentShellTextFormatter {
    fun formatStructuredResult(result: CommandResult, body: List<String>): List<String> {
        val out = mutableListOf<String>()
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
        if (body.isNotEmpty()) {
            out += ""
            out += body
        }
        return out
    }

    private fun statusName(status: CommandResult.Status): String =
        status.name.removePrefix("STATUS_").lowercase()
}
