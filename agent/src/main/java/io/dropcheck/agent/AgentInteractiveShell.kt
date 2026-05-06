package io.dropcheck.agent

/** Commands supported by the on-device interactive shell. */
internal sealed class AgentShellCommand {
    data object Noop : AgentShellCommand()
    data class Help(val topic: String = "") : AgentShellCommand()
    data object ClearUse : AgentShellCommand()
    data object ShowUse : AgentShellCommand()
    data object ShowWifiStatus : AgentShellCommand()
    data class ShowWifiScan(val band: String = "") : AgentShellCommand()
    data class ShowWifiScanFresh(val band: String = "", val timeoutMs: Int = 0) : AgentShellCommand()
    data class ShowWifiScanDetail(val target: String, val band: String = "") : AgentShellCommand()
    data class Use(val name: String) : AgentShellCommand()
    data class Invalid(val message: String) : AgentShellCommand()
}

/** Parser for the small on-device shell command surface. */
internal object AgentShellParser {
    private val commandNames = listOf("clear", "help", "show", "use")
    private val wifiBandNames = listOf("all", "2.4ghz", "5ghz", "6ghz", "60ghz")

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
            tokens.size == 2 && "wifi".startsWith(tokens[1]) -> AgentShellCommand.Invalid("usage: show wifi <status|scan>")
            tokens.size >= 3 && "wifi".startsWith(tokens[1]) -> parseShowWifi(tokens.drop(2))
            else -> AgentShellCommand.Invalid("usage: show (use|wifi status|wifi scan)")
        }
    }

    private fun parseShowWifi(tokens: List<String>): AgentShellCommand {
        return when (resolveKeyword(tokens.first(), listOf("status", "scan"))) {
            "status" -> {
                if (tokens.size == 1) AgentShellCommand.ShowWifiStatus else AgentShellCommand.Invalid("usage: show wifi status")
            }
            "scan" -> parseShowWifiScan(tokens.drop(1))
            else -> AgentShellCommand.Invalid("usage: show wifi <status|scan>")
        }
    }

    private fun parseShowWifiScan(args: List<String>): AgentShellCommand {
        if (args.isEmpty()) return AgentShellCommand.ShowWifiScan()
        return when (val first = resolveKeyword(args[0], listOf("fresh", "detail") + wifiBandNames)) {
            "fresh" -> parseShowWifiScanFresh(args.drop(1))
            "detail" -> parseShowWifiScanDetail(args.drop(1))
            in wifiBandNames -> {
                if (args.size == 1) {
                    AgentShellCommand.ShowWifiScan(first.orEmpty())
                } else {
                    AgentShellCommand.Invalid("usage: show wifi scan [all|2.4ghz|5ghz|6ghz|60ghz]")
                }
            }
            else -> AgentShellCommand.Invalid("usage: show wifi scan [all|2.4ghz|5ghz|6ghz|60ghz]")
        }
    }

    private fun parseShowWifiScanFresh(args: List<String>): AgentShellCommand {
        var band = ""
        var timeoutMs = 0
        var index = 0
        while (index < args.size) {
            val token = args[index]
            if ("timeout".startsWith(token)) {
                if (timeoutMs != 0 || index + 1 >= args.size) {
                    return AgentShellCommand.Invalid("usage: show wifi scan fresh [timeout MS] [all|2.4ghz|5ghz|6ghz|60ghz]")
                }
                timeoutMs = args[index + 1].toIntOrNull()
                    ?: return AgentShellCommand.Invalid("usage: show wifi scan fresh [timeout MS] [all|2.4ghz|5ghz|6ghz|60ghz]")
                index += 2
                continue
            }
            val parsedBand = resolveKeyword(token, wifiBandNames)
                ?: return AgentShellCommand.Invalid("usage: show wifi scan fresh [timeout MS] [all|2.4ghz|5ghz|6ghz|60ghz]")
            if (band.isNotBlank()) {
                return AgentShellCommand.Invalid("wifi scan fresh band specified twice")
            }
            band = parsedBand
            index++
        }
        return AgentShellCommand.ShowWifiScanFresh(band, timeoutMs)
    }

    private fun parseShowWifiScanDetail(args: List<String>): AgentShellCommand {
        if (args.isEmpty() || args.size > 2) {
            return AgentShellCommand.Invalid("usage: show wifi scan detail [all|2.4ghz|5ghz|6ghz|60ghz] <ssid|bssid>")
        }
        if (args.size == 1) {
            return AgentShellCommand.ShowWifiScanDetail(args[0])
        }
        val firstBand = resolveKeyword(args[0], wifiBandNames)
        val secondBand = resolveKeyword(args[1], wifiBandNames)
        return when {
            firstBand != null && secondBand == null -> AgentShellCommand.ShowWifiScanDetail(args[1], firstBand)
            firstBand == null && secondBand != null -> AgentShellCommand.ShowWifiScanDetail(args[0], secondBand)
            else -> AgentShellCommand.Invalid("usage: show wifi scan detail [all|2.4ghz|5ghz|6ghz|60ghz] <ssid|bssid>")
        }
    }

    private fun resolveCommandName(value: String): String? {
        if (value.isBlank()) return null
        return commandNames.firstOrNull { it.startsWith(value) }
    }

    private fun resolveKeyword(value: String, options: List<String>): String? {
        if (value.isBlank()) return null
        return options.firstOrNull { it.startsWith(value) }
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
