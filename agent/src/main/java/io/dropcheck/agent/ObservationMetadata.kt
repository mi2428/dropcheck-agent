package io.dropcheck.agent

import io.dropcheck.agent.grpc.DiagnosticField

/** Collector facts, not a heuristic inferred by a renderer from zero/false. */
internal fun observationAvailability(field: String, available: Boolean, reason: String = ""): List<DiagnosticField> {
    return buildList {
        add(DiagnosticField.newBuilder().setKey("$field.state").setValue(if (available) "available" else "unavailable").build())
        if (!available) add(DiagnosticField.newBuilder().setKey("$field.reason").setValue(reason.ifBlank { "not collected" }).build())
    }
}

internal fun observationAgeMs(timestampUs: Long, elapsedRealtimeUs: Long): Long? {
    if (timestampUs <= 0 || timestampUs > elapsedRealtimeUs) return null
    return (elapsedRealtimeUs - timestampUs) / 1000
}
