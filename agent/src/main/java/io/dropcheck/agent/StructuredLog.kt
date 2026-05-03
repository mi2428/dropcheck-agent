package io.dropcheck.agent

import android.content.Context
import io.dropcheck.agent.grpc.CommandLog

internal object StructuredLog {
    fun format(event: String, vararg fields: Pair<String, Any?>): String {
        return format(event, fields.asList())
    }

    fun format(event: String, fields: Iterable<Pair<String, Any?>>): String {
        val parts = mutableListOf("event=${renderValue(event)}")
        val renderedFields = fields(fields)
        if (renderedFields.isNotBlank()) {
            parts += renderedFields
        }
        return parts.joinToString(" ")
    }

    fun fields(fields: Iterable<Pair<String, Any?>>): String {
        val parts = mutableListOf<String>()
        for ((key, value) in fields) {
            if (key.isBlank()) continue
            parts += "${renderKey(key)}=${renderValue(value)}"
        }
        return parts.joinToString(" ")
    }

    fun preview(value: String, maxChars: Int = 500): String {
        if (value.length <= maxChars) return value
        return value.take(maxChars) + "...<truncated:${value.length - maxChars}>"
    }

    private fun renderKey(key: String): String {
        return key.map { char ->
            when {
                char.isLetterOrDigit() || char == '_' || char == '.' || char == '-' -> char
                else -> '_'
            }
        }.joinToString("")
    }

    private fun renderValue(value: Any?): String {
        return when (value) {
            null -> "null"
            is Boolean -> value.toString()
            is Number -> value.toString()
            is Enum<*> -> value.name
            is Iterable<*> -> renderString(value.joinToString(",") { scalarText(it) })
            is Array<*> -> renderString(value.joinToString(",") { scalarText(it) })
            is IntArray -> renderString(value.joinToString(","))
            is LongArray -> renderString(value.joinToString(","))
            is BooleanArray -> renderString(value.joinToString(","))
            else -> renderString(value.toString())
        }
    }

    private fun scalarText(value: Any?): String {
        return when (value) {
            null -> "null"
            is Enum<*> -> value.name
            else -> value.toString()
        }
    }

    private fun renderString(value: String): String {
        if (value.isNotEmpty() && value.all { it.isLogfmtBareChar() }) {
            return value
        }
        return buildString {
            append('"')
            for (char in value) {
                when (char) {
                    '\\' -> append("\\\\")
                    '"' -> append("\\\"")
                    '\n' -> append("\\n")
                    '\r' -> append("\\r")
                    '\t' -> append("\\t")
                    else -> append(char)
                }
            }
            append('"')
        }
    }

    private fun Char.isLogfmtBareChar(): Boolean {
        return isLetterOrDigit() || this in setOf('_', '.', '/', '@', ':', '%', '+', '=', ',', ';', '-')
    }
}

internal fun CommandLogger.logEvent(
    level: CommandLog.Level,
    event: String,
    fields: Iterable<Pair<String, Any?>>,
    scope: CommandLogScope = CommandLogScope.COMMAND,
) {
    log(level, StructuredLog.format(event, fields), scope)
}

internal fun CommandLogger.debugEvent(event: String, vararg fields: Pair<String, Any?>) {
    logEvent(CommandLog.Level.LEVEL_DEBUG, event, fields.asList())
}

internal fun CommandLogger.debugEvent(event: String, fields: Iterable<Pair<String, Any?>>) {
    logEvent(CommandLog.Level.LEVEL_DEBUG, event, fields)
}

internal fun CommandLogger.infoEvent(event: String, vararg fields: Pair<String, Any?>) {
    logEvent(CommandLog.Level.LEVEL_INFO, event, fields.asList())
}

internal fun CommandLogger.infoEvent(event: String, fields: Iterable<Pair<String, Any?>>) {
    logEvent(CommandLog.Level.LEVEL_INFO, event, fields)
}

internal fun CommandLogger.warnEvent(event: String, vararg fields: Pair<String, Any?>) {
    logEvent(CommandLog.Level.LEVEL_WARN, event, fields.asList())
}

internal fun CommandLogger.warnEvent(event: String, fields: Iterable<Pair<String, Any?>>) {
    logEvent(CommandLog.Level.LEVEL_WARN, event, fields)
}

internal fun CommandLogger.errorEvent(event: String, vararg fields: Pair<String, Any?>) {
    logEvent(CommandLog.Level.LEVEL_ERROR, event, fields.asList())
}

internal fun CommandLogger.errorEvent(event: String, fields: Iterable<Pair<String, Any?>>) {
    logEvent(CommandLog.Level.LEVEL_ERROR, event, fields)
}

internal fun CommandLogger.execEvent(event: String, vararg fields: Pair<String, Any?>) {
    logEvent(CommandLog.Level.LEVEL_INFO, event, fields.asList(), CommandLogScope.EXEC)
}

internal fun CommandLogger.execEvent(event: String, fields: Iterable<Pair<String, Any?>>) {
    logEvent(CommandLog.Level.LEVEL_INFO, event, fields, CommandLogScope.EXEC)
}

internal fun CommandLogger.execDebugEvent(event: String, fields: Iterable<Pair<String, Any?>>) {
    logEvent(CommandLog.Level.LEVEL_DEBUG, event, fields, CommandLogScope.EXEC)
}

internal fun TerminalLog.debugEvent(
    context: Context,
    event: String,
    fields: Iterable<Pair<String, Any?>>,
) {
    debug(context, StructuredLog.format(event, fields))
}

internal fun TerminalLog.infoEvent(
    context: Context,
    event: String,
    fields: Iterable<Pair<String, Any?>>,
) {
    info(context, StructuredLog.format(event, fields))
}

internal fun TerminalLog.warnEvent(
    context: Context,
    event: String,
    fields: Iterable<Pair<String, Any?>>,
) {
    warn(context, StructuredLog.format(event, fields))
}

internal fun TerminalLog.errorEvent(
    context: Context,
    event: String,
    fields: Iterable<Pair<String, Any?>>,
) {
    error(context, StructuredLog.format(event, fields))
}
