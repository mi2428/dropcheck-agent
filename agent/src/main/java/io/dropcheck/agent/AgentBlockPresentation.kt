package io.dropcheck.agent

import android.graphics.Paint
import android.icu.text.BreakIterator
import android.text.SpannableStringBuilder
import android.text.Spanned
import android.text.style.TabStopSpan
import android.text.style.ScaleXSpan
import java.util.Locale

/** Only sanitized presentation values; never RunCommand, credentials or raw log history. */
internal sealed interface AgentBlockPart {
    data class Text(val value: String) : AgentBlockPart
    data class Field(val label: String, val value: String, val fullValue: String = value) : AgentBlockPart
    data class Table(val columns: List<Column>, val rows: List<List<String>>) : AgentBlockPart
    /** SSID/BSSID/dBm/BAND/CH/BW/PHY/SEC, in documented order. */
    data class Scan(val rows: List<List<String>>) : AgentBlockPart
    data class Column(val label: String, val numeric: Boolean = false)
}

internal class AgentPresentationBlock private constructor(
    val parts: List<AgentBlockPart>,
    val color: Int,
    val baseSizeSp: Float,
    val minimumSizeSp: Float,
    val safeValueFile: AgentSafeValueFile?,
) {
    val fullText: String get() = parts.joinToString("\n") { part ->
        when (part) {
            is AgentBlockPart.Text -> part.value
            is AgentBlockPart.Field -> fieldText(part.label, part.value) + if (part.fullValue != part.value) "\n${fieldText("${part.label} wire value (availability above)", part.fullValue)}" else ""
            is AgentBlockPart.Table -> part.rows.joinToString("\n\n") { row ->
                part.columns.indices.joinToString("\n") { i -> "${part.columns[i].label}: ${row.getOrElse(i) { "?" }}" }
            }
            is AgentBlockPart.Scan -> part.rows.joinToString("\n\n") { row ->
                scanColumns.indices.joinToString("\n") { i -> "${scanColumns[i].label}: ${row.getOrElse(i) { "?" }}" }
            }
        }
    }

    fun safeValues(): Sequence<Pair<String, String>> = sequence {
        for (part in parts) when (part) {
            is AgentBlockPart.Text -> yield("Result: ${part.value.lineSequence().first().take(80)}" to part.value)
            is AgentBlockPart.Field -> {
                yield(part.label to part.value)
                if (part.fullValue != part.value) yield("${part.label} wire value (availability above)" to part.fullValue)
            }
            is AgentBlockPart.Table -> part.rows.forEachIndexed { row, values -> part.columns.forEachIndexed { column, label -> yield("Row ${row + 1} ${label.label}" to values[column]) } }
            is AgentBlockPart.Scan -> part.rows.forEachIndexed { row, values -> scanColumns.forEachIndexed { column, label -> yield("AP ${row + 1} ${label.label}" to values[column]) } }
        }
    }

    /** Parts are already sanitized. Styling and line filtering must not redact
     * literal identities a second time (an SSID may itself contain password=). */
    fun withStyle(baseSizeSp: Float, minimumSizeSp: Float): AgentPresentationBlock {
        require(baseSizeSp.isFinite() && minimumSizeSp.isFinite() && baseSizeSp >= minimumSizeSp && minimumSizeSp >= 10f)
        return AgentPresentationBlock(parts, color, baseSizeSp, minimumSizeSp, safeValueFile)
    }

    fun mapLogicalText(transform: (String) -> String): AgentPresentationBlock =
        AgentPresentationBlock(listOf(AgentBlockPart.Text(transform(fullText))), color, baseSizeSp, minimumSizeSp, null)

    companion object {
        fun create(parts: List<AgentBlockPart>, color: Int, baseSizeSp: Float = 10f, minimumSizeSp: Float = 10f, safeValueFile: AgentSafeValueFile? = null): AgentPresentationBlock {
            require(baseSizeSp.isFinite() && minimumSizeSp.isFinite() && baseSizeSp >= minimumSizeSp && minimumSizeSp >= 10f)
            return AgentPresentationBlock(parts.map { part ->
                when (part) {
                    is AgentBlockPart.Text -> AgentBlockPart.Text(AgentSafePresentation.text(part.value))
                    is AgentBlockPart.Field -> AgentBlockPart.Field(AgentSafePresentation.cell(part.label), AgentSafePresentation.field(part.label, part.value), AgentSafePresentation.field(part.label, part.fullValue))
                    is AgentBlockPart.Table -> AgentBlockPart.Table(
                        part.columns.also { require(it.isNotEmpty() && part.rows.all { row -> row.size == it.size }) { "Invalid presentation table shape" } }.map { it.copy(label = AgentSafePresentation.cell(it.label)) },
                        part.rows.map { row -> row.mapIndexed { index, value -> AgentSafePresentation.field(part.columns.getOrNull(index)?.label.orEmpty(), value) } },
                    )
                    is AgentBlockPart.Scan -> AgentBlockPart.Scan(part.rows.map { row ->
                        require(row.size == scanColumns.size) { "Invalid presentation scan record shape" }
                        row.mapIndexed { index, value -> AgentSafePresentation.field(scanColumns[index].label, value) }
                    })
                }
            }, color, baseSizeSp, minimumSizeSp, safeValueFile)
        }
    }
}

private fun fieldText(label: String, value: String): String {
    val hanging = if (label.startsWith(' ')) " ".repeat(label.length + 2) else ""
    return "$label: ${value.replace("\n", "\n$hanging")}"
}

private val scanColumns = listOf(
    AgentBlockPart.Column("SSID"), AgentBlockPart.Column("BSSID"), AgentBlockPart.Column("dBm", true),
    AgentBlockPart.Column("BAND"), AgentBlockPart.Column("CH", true), AgentBlockPart.Column("BW", true),
    AgentBlockPart.Column("PHY"), AgentBlockPart.Column("SEC"),
)

internal object AgentSafePresentation {
    private val ansi = Regex("\u001B(?:\\[[0-?]*[ -/]*[@-~]|\\][^\u0007\u001B]*(?:\u0007|\u001B\\\\))")
    private val credential = com.google.re2j.Pattern.compile("""(?im)(^|[\s;,=({\[])(["']?(?:passphrase|password|authorization|access_token|refresh_token|token|psk|api_key|apikey|api-key|client_secret|x-amz-signature|x-amz-credential|x-amz-security-token)["']?)(\s*[:=]\s*)("(?:\\.|[^"\\])*(?:"|$)|'(?:\\.|[^'\\])*(?:'|$)|(?:Bearer|Basic)\s+[^\s,;}\]]+|[^\s,;}\]]+)""")
    private val userInfo = Regex("(https?://)[^/\\s@]+@", RegexOption.IGNORE_CASE)
    private val urls = Regex("https?://[^\\s<>]+", RegexOption.IGNORE_CASE)
    private val secretKeys = setOf("api_key", "apikey", "api-key", "client_secret", "x-amz-signature", "x-amz-credential", "x-amz-security-token", "token", "access_token", "refresh_token", "password", "passphrase", "psk", "authorization")

    fun text(value: String): String {
        var safe = credential.matcher(clean(value)).replaceAll("\$1\$2\$3<redacted>")
        safe = userInfo.replace(safe) { "${it.groupValues[1]}redacted@" }
        return safeUrls(safe)
    }

    private fun safeUrls(value: String): String = urls.replace(value) { match ->
        val raw = userInfo.replace(match.value) { "${it.groupValues[1]}redacted@" }
        try {
            val uri = java.net.URI(raw)
            val query = uri.rawQuery
            var safe = if (query == null) raw else raw.replaceFirst("?$query", "?${redactURLParameters(query)}")
            val fragment = uri.rawFragment
            if (fragment != null && '=' in fragment) safe = safe.replaceFirst("#$fragment", "#${redactURLParameters(fragment)}")
            safe
        } catch (_: java.net.URISyntaxException) {
            "<invalid URL redacted>"
        } catch (_: IllegalArgumentException) {
            "<invalid URL query redacted>"
        }
    }

    private fun redactURLParameters(value: String): String = value.split('&').joinToString("&") { part ->
        val key = part.substringBefore('=')
        val decoded = java.net.URLDecoder.decode(key, "UTF-8").lowercase(Locale.ROOT)
        if (decoded in secretKeys) "$key=%3Credacted%3E" else part
    }

    private fun clean(value: String): String {
        var safe = value
        safe = ansi.replace(safe, "")
        return buildString {
            safe.codePoints().forEach { cp ->
                when {
                    cp == '\n'.code -> append('\n')
                    Character.isISOControl(cp) || cp == 0x061C || cp == 0x200B || cp == 0x200E || cp == 0x200F || cp == 0xFEFF || cp == 0x2028 || cp == 0x2029 || cp in 0x202A..0x202E || cp in 0x2060..0x2069 -> append(' ')
                    else -> appendCodePoint(cp)
                }
            }
        }
    }

    fun cell(value: String): String = safeUrls(clean(value)).replace('\n', ' ')

    fun field(label: String, value: String): String {
        val key = clean(label).trim()
        return when {
            key.lowercase(Locale.ROOT) in secretKeys -> "<redacted>"
            key in listOf("SSID", "BSSID", "MLD", "AP MAC", "STA MAC", "PHY", "SEC", "BAND", "Interface", "Network", "Operator", "Venue") -> clean(value).replace('\n', ' ')
            else -> text(value)
        }
    }
}

/** Explicit history budgets; an oversized result is rejected, never silently clipped in copy. */
internal class AgentBlockHistory(
    private val maxBlocks: Int = 32,
    private val maxChars: Int = 262_144,
) {
    init { require(maxBlocks > 0 && maxChars > 0) { "Invalid history budget" } }
    // ponytail: bounded 32-block linear budget check; no duplicate full-text archive/cache.
    val blocks = ArrayDeque<AgentPresentationBlock>()
    var evictedBlocks = 0
        private set

    fun append(block: AgentPresentationBlock) {
        require(block.fullText.length <= maxChars) { "Result exceeds safe history budget; filter the command before retrying" }
        blocks.addLast(block)
        while (blocks.size > maxBlocks || blocks.sumOf { it.fullText.length } > maxChars) {
            blocks.removeFirst().safeValueFile?.delete()
            evictedBlocks++
        }
    }

    fun fits(block: AgentPresentationBlock): Boolean = block.fullText.length <= maxChars

    fun evictValueFileBlocks() {
        val old = blocks.filter { it.safeValueFile != null }
        old.forEach { blocks.remove(it); it.safeValueFile?.delete(); evictedBlocks++ }
    }
}

internal data class AgentBlockLayout(val text: CharSequence, val textSizeSp: Float)

internal object AgentNativeBlockLayout {
    private const val ROW_BUDGET = 96
    private const val LINE_BUDGET = 240
    fun render(block: AgentPresentationBlock, contentWidthPx: Int, paint: Paint): AgentBlockLayout? {
        if (contentWidthPx <= 0) return null
        // Structure first. Records preserve all values, so shrinking is unnecessary here.
        // The permitted floor is exposed for table-oriented blocks, not applied to other blocks/input.
        val out = SpannableStringBuilder()
        var visibleRecords = 0
        var totalRecords = 0
        block.parts.forEach { part ->
            when (part) {
                is AgentBlockPart.Text -> appendWrapped(out, part.value, contentWidthPx, paint)
                is AgentBlockPart.Field -> {
                    val indent = part.label.takeWhile { it == ' ' }
                    val hanging = " ".repeat(part.label.length + 2)
                    if (indent.isEmpty()) {
                        appendWrapped(out, fieldText(part.label, part.value), contentWidthPx, paint)
                    } else if (paint.measureText(hanging + "MMMMMMMM") > contentWidthPx) {
                        appendWrapped(out, part.label.trimEnd() + ":", contentWidthPx, paint, indent + "  ")
                        appendWrapped(out, indent + "  " + part.value.replace("\n", "\n$indent  "), contentWidthPx, paint, indent + "  ")
                    } else {
                        appendWrapped(out, fieldText(part.label, part.value), contentWidthPx, paint, hanging)
                    }
                }
                is AgentBlockPart.Table -> {
                    totalRecords += part.rows.size
                    val rows = part.rows.take(minOf(ROW_BUDGET - visibleRecords, LINE_BUDGET - out.count { it == '\n' } - 1).coerceAtLeast(0))
                    if (!appendTable(out, part.columns, rows, contentWidthPx, paint)) {
                        visibleRecords += appendRecords(out, part.columns, rows, contentWidthPx, paint)
                    } else visibleRecords += rows.size
                }
                is AgentBlockPart.Scan -> {
                    totalRecords += part.rows.size
                    val rows = part.rows.take(minOf(ROW_BUDGET - visibleRecords, LINE_BUDGET - out.count { it == '\n' } - 2).coerceAtLeast(0))
                    var shown = 0
                    if (!appendTable(out, scanColumns, rows, contentWidthPx, paint)) {
                        rows.groupBy { it[0] }.forEach groupLoop@ { (ssid, group) ->
                            val heading = SpannableStringBuilder()
                            appendWrapped(heading, "SSID: $ssid", contentWidthPx, paint)
                            if (out.count { it == '\n' } + heading.count { it == '\n' } >= LINE_BUDGET) return@groupLoop
                            out.append(heading)
                            if (!appendTable(out, scanColumns.drop(1), group.map { it.drop(1) }, contentWidthPx, paint)) {
                                group.forEach { row ->
                                    val record = SpannableStringBuilder()
                                    if (!appendTable(record, scanColumns.drop(2), listOf(row.drop(2)), contentWidthPx, paint)) {
                                        scanColumns.drop(2).forEachIndexed { i, column -> appendWrapped(record, "${column.label}: ${row[i + 2]}", contentWidthPx, paint) }
                                    }
                                    appendWrapped(record, "BSSID: ${row[1]}", contentWidthPx, paint)
                                    if (out.count { it == '\n' } + record.count { it == '\n' } <= LINE_BUDGET) {
                                        out.append(record)
                                        shown++
                                    }
                                }
                            } else shown += group.size
                        }
                    } else shown = rows.size
                    visibleRecords += shown
                    appendWrapped(out, "AP display: shown=$shown total=${part.rows.size}", contentWidthPx, paint)
                }
            }
        }
        if (totalRecords > visibleRecords) appendWrapped(out, "shown=$visibleRecords total=$totalRecords; 96-row / 240-line display budget; Copy full result retains all safe values", contentWidthPx, paint)
        return AgentBlockLayout(out, block.baseSizeSp)
    }

    private fun appendRecords(out: SpannableStringBuilder, columns: List<AgentBlockPart.Column>, rows: List<List<String>>, width: Int, paint: Paint): Int {
        var shown = 0
        for (row in rows) {
            val record = SpannableStringBuilder()
            columns.forEachIndexed { i, column -> appendWrapped(record, "${column.label}: ${row.getOrElse(i) { "?" }}", width, paint) }
            if (out.count { it == '\n' } + record.count { it == '\n' } > LINE_BUDGET) break
            out.append(record)
            shown++
        }
        return shown
    }

    fun shrinkFloor(baseSizeSp: Float, minimumSizeSp: Float): Float = maxOf(baseSizeSp * 0.9f, 10f, minimumSizeSp)

    private fun appendTable(out: SpannableStringBuilder, columns: List<AgentBlockPart.Column>, rows: List<List<String>>, width: Int, paint: Paint): Boolean {
        if (rows.isEmpty()) return true
        if (out.count { it == '\n' } + rows.size + 1 > LINE_BUDGET) return false
        val widths = columns.indices.map { i ->
            kotlin.math.ceil((listOf(columns[i].label) + rows.map { it.getOrElse(i) { "?" } }).maxOf { paint.measureText(it) }).toFloat()
        }
        val space = paint.measureText(" ")
        val gap = kotlin.math.ceil(space).toFloat()
        if (widths.sum() + gap * (columns.size - 1) > width) return false
        val stops = mutableListOf<Int>()
        var x = 0f
        widths.dropLast(1).forEach { w -> x += w + gap; stops += kotlin.math.ceil(x).toInt() }
        val start = out.length
        fun appendRow(row: List<String>) {
            columns.indices.forEach { i ->
                if (i > 0) out.append('\t')
                // Native tab stops handle fallback fonts; never count spaces as glyph widths.
                val value = row.getOrElse(i) { "?" }
                if (columns[i].numeric) {
                    // Native span width, not a guessed number of fallback-font spaces.
                    val padding = (widths[i] - paint.measureText(value)).coerceAtLeast(0f)
                    if (padding > 0f) {
                        val cellStart = out.length
                        out.append(" ")
                        out.setSpan(ScaleXSpan(padding / space), cellStart, out.length, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE)
                    }
                }
                out.append(value)
            }
            out.append('\n')
        }
        appendRow(columns.map { it.label })
        rows.forEach(::appendRow)
        stops.forEach { stop -> out.setSpan(TabStopSpan.Standard(stop), start, out.length, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE) }
        return true
    }

    private fun appendWrapped(out: SpannableStringBuilder, value: String, width: Int, paint: Paint, continuationIndent: String = "") {
        val continuationWidth = if (continuationIndent.isEmpty()) 0f else paint.measureText(continuationIndent)
        value.split('\n').forEach { line ->
            val boundaries = BreakIterator.getCharacterInstance(Locale.ROOT)
            boundaries.setText(line)
            var start = 0
            var indent = ""
            var indentWidth = 0f
            var end = boundaries.first()
            var next = boundaries.next()
            while (next != BreakIterator.DONE) {
                if (indentWidth + paint.measureText(line, start, next) > width && end > start) {
                    out.append(indent).append(line.substring(start, end)).append('\n')
                    start = end
                    indent = continuationIndent
                    indentWidth = continuationWidth
                }
                end = next
                next = boundaries.next()
            }
            out.append(indent).append(line.substring(start)).append('\n')
        }
    }
}
