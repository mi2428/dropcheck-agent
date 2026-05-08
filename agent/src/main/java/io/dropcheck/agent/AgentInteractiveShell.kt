package io.dropcheck.agent

/** Commands supported by the on-device interactive shell. */
internal sealed class AgentShellCommand {
    data object Noop : AgentShellCommand()
    data class Help(val topic: String = "") : AgentShellCommand()
    data object ClearUse : AgentShellCommand()
    data object ShowUse : AgentShellCommand()
    data object ShowWifiStatus : AgentShellCommand()
    data class ShowWifiMlo(val fresh: Boolean = false, val timeoutMs: Int = 0) : AgentShellCommand()
    data class Ping(val host: String, val count: Int = 0, val sizeBytes: Int = 0, val timeoutMs: Int = 0) : AgentShellCommand()
    data class Traceroute(val host: String, val maxHops: Int = 0, val sizeBytes: Int = 0, val timeoutMs: Int = 0) : AgentShellCommand()
    data class Use(val name: String) : AgentShellCommand()
    data class Invalid(val message: String) : AgentShellCommand()
}

/** Parser for the small on-device shell command surface. */
internal object AgentShellParser {
    private val commandNames = listOf("clear", "help", "ping", "show", "traceroute", "use")

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
            "ping" -> parsePing(tokens.drop(1))
            "show" -> parseShow(tokens)
            "traceroute" -> parseTraceroute(tokens.drop(1))
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

    private fun parsePing(args: List<String>): AgentShellCommand {
        val parsed = parseOptionsAndHost(
            args = args,
            options = listOf("count", "size", "timeout"),
            usage = "usage: ping HOST [count N] [size BYTES] [timeout MS]",
        )
        if (parsed.error != null) return AgentShellCommand.Invalid(parsed.error)
        return AgentShellCommand.Ping(
            host = parsed.host,
            count = parsed.values["count"] ?: 0,
            sizeBytes = parsed.values["size"] ?: 0,
            timeoutMs = parsed.values["timeout"] ?: 0,
        )
    }

    private fun parseTraceroute(args: List<String>): AgentShellCommand {
        val parsed = parseOptionsAndHost(
            args = args,
            options = listOf("max-hops", "size", "timeout"),
            usage = "usage: traceroute HOST [max-hops N] [size BYTES] [timeout MS]",
        )
        if (parsed.error != null) return AgentShellCommand.Invalid(parsed.error)
        return AgentShellCommand.Traceroute(
            host = parsed.host,
            maxHops = parsed.values["max-hops"] ?: 0,
            sizeBytes = parsed.values["size"] ?: 0,
            timeoutMs = parsed.values["timeout"] ?: 0,
        )
    }

    private fun parseOptionsAndHost(args: List<String>, options: List<String>, usage: String): ParsedProbe {
        if (args.isEmpty()) return ParsedProbe(error = usage)
        var host = ""
        val values = mutableMapOf<String, Int>()
        var index = 0
        while (index < args.size) {
            val key = resolveKeyword(args[index], options)
            if (key != null) {
                if (index + 1 >= args.size) return ParsedProbe(error = "$key requires a value")
                if (values.containsKey(key)) return ParsedProbe(error = "$key specified twice")
                val value = args[index + 1].toIntOrNull()
                if (value == null || value <= 0) return ParsedProbe(error = "$key must be a positive integer")
                values[key] = value
                index += 2
                continue
            }
            if (host.isNotBlank()) return ParsedProbe(error = usage)
            host = args[index]
            index += 1
        }
        if (host.isBlank()) return ParsedProbe(error = usage)
        return ParsedProbe(host = host, values = values)
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

    private data class ParsedProbe(
        val host: String = "",
        val values: Map<String, Int> = emptyMap(),
        val error: String? = null,
    )
}
