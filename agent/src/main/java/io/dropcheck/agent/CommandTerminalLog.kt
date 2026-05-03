package io.dropcheck.agent

internal object CommandTerminalLog {
    fun controller(commandId: String, scope: CommandLogScope, message: String): String {
        return format(listOf("command_id" to commandId), scope, message)
    }

    fun standalone(scope: CommandLogScope, message: String): String {
        return format(listOf("source" to "standalone"), scope, message)
    }

    private fun format(contextFields: List<Pair<String, Any?>>, scope: CommandLogScope, message: String): String {
        val fields = contextFields + ("scope" to scope.logValue)
        return if (message.startsWith("event=")) {
            "$message ${StructuredLog.fields(fields)}"
        } else {
            StructuredLog.format(
                if (scope == CommandLogScope.EXEC) "probe.exec" else "command.log",
                fields + ("msg" to message),
            )
        }
    }
}
