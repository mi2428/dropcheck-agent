package io.dropcheck.agent

import com.google.re2j.Pattern

internal class AgentShellPipeline private constructor(private val stages: List<Stage>, val json: Boolean) {
    private data class Stage(val name: String, val pattern: Pattern? = null, val number: Int? = null)
    val active get() = json || stages.isNotEmpty()

    fun apply(text: String): String {
        var lines = logicalLines(text)
        for (stage in stages) {
            when (stage.name) {
                "count" -> lines = listOf("Count: ${lines.size} lines")
                "find" -> {
                    val pattern = stage.pattern!!
                    val first = lines.indexOfFirst { pattern.matcher(it).find() }
                    lines = if (first < 0) emptyList() else lines.subList(first, lines.size)
                }
                "match" -> { val pattern = stage.pattern!!; lines = lines.filter { pattern.matcher(it).find() } }
                "except" -> { val pattern = stage.pattern!!; lines = lines.filter { !pattern.matcher(it).find() } }
                "last" -> { val n = stage.number!!; if (lines.size > n) lines = lines.subList(lines.size - n, lines.size) }
                "trim" -> { val n = stage.number!!; lines = lines.map { trimLine(it, n) } }
            }
        }
        return if (lines.isEmpty()) "" else lines.joinToString("\n", postfix = "\n")
    }

    fun apply(result: AgentShellResult): AgentShellResult {
        val source = result.block ?: AgentPresentationBlock.create(result.lines.map { AgentBlockPart.Text(it.toString()) }, if (result.ok) AgentLogStyle.TEXT_COLOR else -44976)
        return result.copy(lines = emptyList(), block = source.mapLogicalText { text ->
            val filtered = apply(text)
            if (json) "{\"ok\":${result.ok},\"lines\":[${logicalLines(filtered).joinToString(",", transform = ::jsonString)}]}"
            else filtered + if (result.ok) "" else "Command FAILED (output filtered)\n"
        })
    }

    companion object {
        fun parse(parts: List<String>): AgentShellPipeline {
            require(parts.size <= 16) { "pipeline exceeds 16 stages" }
            val stages = mutableListOf<Stage>()
            var json = false
            for (part in parts) {
                val args = shellSplitWords(part).getOrThrow()
                require(args.isNotEmpty()) { "empty pipeline stage" }
                when (args[0]) {
                    "display" -> {
                        require(args == listOf("display", "json")) { "usage: | display json" }
                        require(!json) { "display format specified twice" }; json = true
                    }
                    "find", "match", "except" -> {
                        require(args.size == 2) { "usage: | ${args[0]} <quoted-pattern>" }
                        require(portablePattern(args[1])) { "${args[0]} regex: unsupported portable pattern (1..512 UTF-8 bytes)" }
                        val compiled = try { Pattern.compile(args[1]) } catch (_: com.google.re2j.PatternSyntaxException) { throw IllegalArgumentException("${args[0]} regex: invalid pattern") }
                        stages += Stage(args[0], compiled)
                    }
                    "last", "trim" -> {
                        require(args.size == 2) { "usage: | ${args[0]} <positive-integer>" }
                        val raw = args[1]
                        require(raw.isNotEmpty() && raw.all { it in '0'..'9' }) { "| ${args[0]} requires a positive integer in 1..2147483647" }
                        val parsed = raw.toIntOrNull() ?: throw IllegalArgumentException("| ${args[0]} requires a positive integer in 1..2147483647")
                        require(parsed in 1..2147483647) { "| ${args[0]} requires a positive integer in 1..2147483647" }
                        stages += Stage(args[0], number = parsed)
                    }
                    "count" -> { require(args.size == 1) { "usage: | count" }; stages += Stage("count") }
                    else -> throw IllegalArgumentException("unsupported pipe")
                }
            }
            require(!json || stages.isEmpty()) { "display json cannot be combined with line filters" }
            return AgentShellPipeline(stages, json)
        }
    }
}

internal fun portablePattern(pattern: String): Boolean {
    val extensions = listOf("(?i)", "(?m)", "(?s)", "(?-i)", "(?-m)", "(?-s)").fold(pattern) { text, flag -> text.replace(flag, "") }
    if (pattern.isEmpty() || pattern.toByteArray(Charsets.UTF_8).size > 512 || "(?" in extensions) return false
    var escaped = false
    for (ch in pattern) {
        if (escaped) { if (ch !in "\\.^$[]()|*+?{}-dDsSwWnrtf") return false; escaped = false }
        else if (ch == '\\') escaped = true
    }
    return !escaped
}

internal fun logicalLines(text: String): List<String> = if (text.isEmpty()) emptyList() else text.removeSuffix("\n").split('\n')

/** Keeps from the 1-based starting column onward; shorter lines become empty logical lines. */
internal fun trimLine(line: String, startOneBased: Int): String {
    val skip = (startOneBased - 1).coerceAtMost(line.codePointCount(0, line.length))
    return line.substring(Character.offsetByCodePoints(line, 0, skip))
}

private fun jsonString(value: String): String = buildString {
    append('"')
    value.forEach { ch -> when (ch) {
        '\\' -> append("\\\\")
        '"' -> append("\\\"")
        else -> if (ch < ' ') append("\\u%04x".format(ch.code)) else append(ch)
    } }
    append('"')
}
