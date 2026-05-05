package io.dropcheck.agent

/** Commands supported by the on-device interactive shell. */
internal sealed class AgentShellCommand {
    data object Noop : AgentShellCommand()
    data object Help : AgentShellCommand()
    data object ClearUse : AgentShellCommand()
    data object ShowUse : AgentShellCommand()
    data class Use(val name: String) : AgentShellCommand()
    data class Invalid(val message: String) : AgentShellCommand()
}

/** Parser for the small on-device shell command surface. */
internal object AgentShellParser {
    fun parse(line: String): AgentShellCommand {
        val tokens = splitWords(line).getOrElse { return AgentShellCommand.Invalid(it.message ?: "invalid command") }
        if (tokens.isEmpty()) return AgentShellCommand.Noop
        return when {
            tokens == listOf("help") || tokens == listOf("?") -> AgentShellCommand.Help
            tokens == listOf("clear", "use") -> AgentShellCommand.ClearUse
            tokens == listOf("show", "use") -> AgentShellCommand.ShowUse
            tokens.first() == "use" && tokens.size == 2 -> AgentShellCommand.Use(tokens[1])
            tokens.first() == "use" -> AgentShellCommand.Invalid("usage: use <name>")
            tokens.first() == "clear" -> AgentShellCommand.Invalid("usage: clear use")
            tokens.first() == "show" -> AgentShellCommand.Invalid("usage: show use")
            else -> AgentShellCommand.Invalid("unknown command: ${tokens.first()}")
        }
    }

    private fun splitWords(line: String): Result<List<String>> {
        val words = mutableListOf<String>()
        val current = StringBuilder()
        var quote: Char? = null
        var escaped = false
        var inToken = false
        for (ch in line) {
            when {
                escaped -> {
                    current.append(ch)
                    escaped = false
                    inToken = true
                }
                ch == '\\' -> {
                    escaped = true
                    inToken = true
                }
                quote != null && ch == quote -> {
                    quote = null
                    inToken = true
                }
                quote != null -> {
                    current.append(ch)
                    inToken = true
                }
                ch == '"' || ch == '\'' -> {
                    quote = ch
                    inToken = true
                }
                ch.isWhitespace() -> {
                    if (inToken) {
                        words += current.toString()
                        current.clear()
                        inToken = false
                    }
                }
                else -> {
                    current.append(ch)
                    inToken = true
                }
            }
        }
        if (escaped) current.append('\\')
        if (quote != null) return Result.failure(IllegalArgumentException("unterminated quote"))
        if (inToken) words += current.toString()
        return Result.success(words)
    }
}
