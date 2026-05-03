package io.dropcheck.agent

internal object AgentLogStyle {
    const val TEXT_COLOR = -0x1 // #FFFFFFFF
    const val WARN_COLOR = -0x29f6 // #FFFFD60A
    const val ERROR_COLOR = -0xadae // #FFFF5252
    const val ACTIVE_PROBE_COLOR = -0x4800d6 // #FFB7FF2A

    fun colorForLine(line: String): Int {
        return when {
            isLevel(line, "ERROR") -> ERROR_COLOR
            isLevel(line, "WARN") -> WARN_COLOR
            isExecLine(line) -> ACTIVE_PROBE_COLOR
            else -> TEXT_COLOR
        }
    }

    internal fun isExecLine(line: String): Boolean {
        return hasField(line, "scope", "exec")
    }

    private fun isLevel(line: String, level: String): Boolean {
        return line.contains(" ${level.padEnd(5)} ") || line.startsWith("$level ")
    }

    private fun hasField(line: String, expectedKey: String, expectedValue: String): Boolean {
        var index = 0
        while (index < line.length) {
            while (index < line.length && line[index].isWhitespace()) index += 1
            val keyStart = index
            while (index < line.length && line[index] != '=' && !line[index].isWhitespace()) index += 1
            if (index >= line.length || line[index] != '=') {
                while (index < line.length && !line[index].isWhitespace()) index += 1
                continue
            }

            val key = line.substring(keyStart, index)
            index += 1
            val value = if (index < line.length && line[index] == '"') {
                val parsed = readQuotedValue(line, index + 1)
                index = parsed.nextIndex
                parsed.value
            } else {
                val valueStart = index
                while (index < line.length && !line[index].isWhitespace()) index += 1
                line.substring(valueStart, index)
            }
            if (key == expectedKey && value == expectedValue) return true
        }
        return false
    }

    private fun readQuotedValue(line: String, start: Int): QuotedValue {
        val value = StringBuilder()
        var index = start
        var escaped = false
        while (index < line.length) {
            val char = line[index]
            when {
                escaped -> {
                    value.append(char)
                    escaped = false
                }
                char == '\\' -> escaped = true
                char == '"' -> return QuotedValue(value.toString(), index + 1)
                else -> value.append(char)
            }
            index += 1
        }
        return QuotedValue(value.toString(), index)
    }

    private data class QuotedValue(
        val value: String,
        val nextIndex: Int,
    )
}
