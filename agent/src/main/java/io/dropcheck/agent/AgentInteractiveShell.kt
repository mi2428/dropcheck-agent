package io.dropcheck.agent

/** Commands supported by the on-device interactive shell. */
internal sealed class AgentShellCommand {
    data object Noop : AgentShellCommand()
    data class Help(val topic: String = "") : AgentShellCommand()
    data object ClearUse : AgentShellCommand()
    data object ShowUse : AgentShellCommand()
    data object ShowWifiStatus : AgentShellCommand()
    data class ShowWifiMlo(val fresh: Boolean = false, val timeoutMs: Int = 0) : AgentShellCommand()
    data class Use(val name: String) : AgentShellCommand()
    data class Invalid(val message: String) : AgentShellCommand()
}

/** Parser for the small on-device shell command surface. */
internal object AgentShellParser {
    private val commandNames = listOf("clear", "help", "show", "use")

    fun parse(line: String): AgentShellCommand {
        val tokens = splitWords(line).getOrElse { return AgentShellCommand.Invalid(it.message ?: "invalid command") }
        if (tokens.isEmpty()) return AgentShellCommand.Noop
        val command = if (tokens.first() == "?") "help" else resolveCommandName(tokens.first())
        return when (command) {
            "help" -> {
                if (tokens.size <= 2) {
                    val topic = tokens.drop(1).firstOrNull().orEmpty()
                    AgentShellCommand.Help(resolveCommandName(topic) ?: topic)
                } else {
                    AgentShellCommand.Invalid("usage: help [NAME]")
                }
            }
            "clear" -> {
                if (tokens.size == 2 && "use".startsWith(tokens[1])) {
                    AgentShellCommand.ClearUse
                } else {
                    AgentShellCommand.Invalid("usage: clear use")
                }
            }
            "show" -> parseShow(tokens)
            "use" -> if (tokens.size == 2) AgentShellCommand.Use(tokens[1]) else AgentShellCommand.Invalid("usage: use NAME")
            else -> AgentShellCommand.Invalid("${tokens.first()}: command not found")
        }
    }

    private fun parseShow(tokens: List<String>): AgentShellCommand {
        return when {
            tokens.size == 2 && "use".startsWith(tokens[1]) -> AgentShellCommand.ShowUse
            tokens.size == 2 && "wifi".startsWith(tokens[1]) -> AgentShellCommand.Invalid("usage: show wifi (status|mlo)")
            tokens.size >= 3 && "wifi".startsWith(tokens[1]) -> parseShowWifi(tokens.drop(2))
            else -> AgentShellCommand.Invalid("usage: show (use|wifi status|wifi mlo)")
        }
    }

    private fun parseShowWifi(tokens: List<String>): AgentShellCommand {
        return when (resolveKeyword(tokens.first(), listOf("status", "mlo"))) {
            "status" -> {
                if (tokens.size == 1) AgentShellCommand.ShowWifiStatus else AgentShellCommand.Invalid("usage: show wifi status")
            }
            "mlo" -> parseShowWifiMlo(tokens.drop(1))
            else -> AgentShellCommand.Invalid("usage: show wifi (status|mlo)")
        }
    }

    private fun parseShowWifiMlo(args: List<String>): AgentShellCommand {
        if (args.isEmpty()) return AgentShellCommand.ShowWifiMlo()
        if (resolveKeyword(args.first(), listOf("fresh")) != "fresh") {
            return AgentShellCommand.Invalid("usage: show wifi mlo [fresh [timeout MS]]")
        }
        val rest = args.drop(1)
        if (rest.isEmpty()) return AgentShellCommand.ShowWifiMlo(fresh = true)
        if (rest.size == 1) {
            val timeoutMs = rest[0].toIntOrNull()
                ?: return AgentShellCommand.Invalid("usage: show wifi mlo [fresh [timeout MS]]")
            return AgentShellCommand.ShowWifiMlo(fresh = true, timeoutMs = timeoutMs)
        }
        if (rest.size == 2 && "timeout".startsWith(rest[0])) {
            val timeoutMs = rest[1].toIntOrNull()
                ?: return AgentShellCommand.Invalid("usage: show wifi mlo [fresh [timeout MS]]")
            return AgentShellCommand.ShowWifiMlo(fresh = true, timeoutMs = timeoutMs)
        }
        return AgentShellCommand.Invalid("usage: show wifi mlo [fresh [timeout MS]]")
    }

    private fun resolveKeyword(value: String, options: List<String>): String? {
        if (value.isBlank()) return null
        return options.firstOrNull { it.startsWith(value) }
    }

    private fun resolveCommandName(value: String): String? {
        if (value.isBlank()) return null
        return commandNames.firstOrNull { it.startsWith(value) }
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
