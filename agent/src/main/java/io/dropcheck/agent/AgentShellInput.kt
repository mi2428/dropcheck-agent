package io.dropcheck.agent

internal data class ShellToken(val value: String, val start: Int, val end: Int, val pipe: Boolean = false, val literal: Boolean = false)
internal data class ShellLexed(val tokens: List<ShellToken>, val quote: Char?, val escaped: Boolean)

/** Token spans preserve quotes, trailing whitespace and the cursor boundary. */
internal fun shellLex(line: String, partial: Boolean = false): ShellLexed {
    val tokens = mutableListOf<ShellToken>()
    val value = StringBuilder()
    var start = -1
    var quote: Char? = null
    var escaped = false
    var literal = false
    fun flush(end: Int) {
        if (start >= 0) {
            tokens += ShellToken(value.toString(), start, end, literal = literal)
            value.clear(); start = -1; literal = false
        }
    }
    line.forEachIndexed { offset, ch ->
        when {
            escaped -> { value.append(ch); escaped = false }
            ch == '\\' -> { if (start < 0) start = offset; escaped = true; literal = true }
            quote != null -> if (ch == quote) quote = null else value.append(ch)
            ch == '\'' || ch == '"' -> { if (start < 0) start = offset; quote = ch; literal = true }
            ch in " \t\r\n" -> flush(offset)
            ch == '|' -> { flush(offset); tokens += ShellToken("|", offset, offset + 1, pipe = true) }
            else -> { if (start < 0) start = offset; value.append(ch) }
        }
    }
    flush(line.length)
    if (!partial) { require(!escaped) { "trailing escape" }; require(quote == null) { "unterminated quote" } }
    return ShellLexed(tokens, quote, escaped)
}

internal fun shellHelpPosition(prefix: String): Boolean = shellLex(prefix, true).let { it.quote == null && !it.escaped }

internal fun shellSplitWords(line: String): Result<List<String>> = runCatching { shellLex(line).tokens.map { it.value } }

internal fun shellCompletionEnd(line: String, cursor: Int): Int? {
    require(cursor in 0..line.length)
    val token = shellLex(line, true).tokens.firstOrNull { it.start < cursor && cursor < it.end }
    return if (token?.literal == true) null else token?.end ?: cursor
}

/** In-memory session state is deliberately separate from display/history blocks. */
internal class AgentShellSession {
    var defaults = AgentShellUseDefaults()
        private set
    private val history = ArrayDeque<String>()
    private var index = 0
    private var draft = ""
    var connectionFailed = false
    var lastTest: AgentShellResult? = null
        private set
    var lastTestAtMs = 0L
        private set

    fun recordTest(result: AgentShellResult) { lastTest = result; lastTestAtMs = System.currentTimeMillis() }
    fun setPassphrase(value: String) { defaults = AgentShellUseDefaults(value) }
    fun remember(line: String) {
        if (AgentShellParser.historySafe(line) && history.lastOrNull() != line) {
            history.addLast(line)
            if (history.size > 100) history.removeFirst()
        }
        index = history.size; draft = ""
    }
    fun recall(current: String, direction: Int): String {
        if (index == history.size) draft = if (AgentShellParser.historySafe(current)) current else ""
        index = (index + direction).coerceIn(0, history.size)
        return if (index == history.size) draft else history.elementAt(index)
    }
}
