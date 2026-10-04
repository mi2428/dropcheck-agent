package io.dropcheck.agent

import io.dropcheck.agent.grpc.DiagnosticField

internal object AgentObservationPresentation {
    fun available(fields: List<DiagnosticField>, field: String): Boolean = fields.associate { it.key to it.value }["$field.state"] == "available"
    fun value(fields: List<DiagnosticField>, field: String, value: String): String {
        val metadata = fields.associate { it.key to it.value }
        return when (val state = metadata["$field.state"]) {
            "available" -> value.ifEmpty { "none" }
            "unavailable" -> "? ($field: ${AgentSafePresentation.text(metadata["$field.reason"] ?: "unavailable; reason not supplied")})"
            null -> "? ($field: observation metadata unavailable)"
            else -> "? (invalid $field.state=$state)"
        }
    }

    fun bool(fields: List<DiagnosticField>, field: String, observed: Boolean): String = value(fields, field, if (observed) "yes" else "no")
    fun list(fields: List<DiagnosticField>, field: String, values: List<String>): String = value(fields, field, values.joinToString("\n"))
}
