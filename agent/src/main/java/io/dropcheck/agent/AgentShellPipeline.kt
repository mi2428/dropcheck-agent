package io.dropcheck.agent

import com.google.re2j.Pattern

internal class AgentShellPipeline private constructor(private val stages: List<Stage>, val json: Boolean) {
    private data class Stage(val name: String, val pattern: Pattern? = null)
    val active get() = json || stages.isNotEmpty()

    fun apply(text: String): String {
        var lines = logicalLines(text)
        for (stage in stages) {
            if (stage.name == "count") { lines = listOf("Count: ${lines.size} lines"); continue }
            var begun = false
            lines = lines.filter { line ->
                val matched = stage.pattern!!.matcher(line).find()
                when (stage.name) {
                    "begin" -> { begun = begun || matched; begun }
                    "match" -> matched
                    else -> !matched
                }
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
                    "begin", "match", "except" -> {
                        require(args.size == 2) { "usage: | ${args[0]} <quoted-pattern>" }
                        require(portablePattern(args[1])) { "${args[0]} regex: unsupported portable pattern (1..512 UTF-8 bytes)" }
                        val compiled = try { Pattern.compile(args[1]) } catch (_: com.google.re2j.PatternSyntaxException) { throw IllegalArgumentException("${args[0]} regex: invalid pattern") }
                        stages += Stage(args[0], compiled)
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

private fun jsonString(value: String): String = buildString {
    append('"')
    value.forEach { ch -> when (ch) {
        '\\' -> append("\\\\")
        '"' -> append("\\\"")
        else -> if (ch < ' ') append("\\u%04x".format(ch.code)) else append(ch)
    } }
    append('"')
}
