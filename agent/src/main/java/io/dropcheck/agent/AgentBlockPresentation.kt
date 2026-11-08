package io.dropcheck.agent

import android.graphics.Paint
import android.graphics.Color
import android.icu.text.BreakIterator
import android.text.SpannableStringBuilder
import android.text.Spanned
import android.text.style.TabStopSpan
import android.text.style.ScaleXSpan
import android.text.style.ForegroundColorSpan
import java.util.Locale

/** Result parts are sanitized on creation; local command echoes explicitly retain verbatim input. */
internal sealed interface AgentBlockPart {
    data class Text(val value: String) : AgentBlockPart
    data class Field(val label: String, val value: String, val fullValue: String = value) : AgentBlockPart
    data class Table(val columns: List<Column>, val rows: List<List<String>>) : AgentBlockPart
    /** SSID/BSSID first; radio/security and optional age columns follow. */
    data class Scan(val rows: List<List<String>>, val columns: List<Column> = scanColumns) : AgentBlockPart
    data class Column(val label: String, val numeric: Boolean = false)
}

internal class AgentPresentationBlock private constructor(
    val parts: List<AgentBlockPart>,
    val color: Int,
    val baseSizeSp: Float,
    val minimumSizeSp: Float,
    val safeValueFile: AgentSafeValueFile?,
    val promptLength: Int = 0,
) {
    val fullText: String get() = logicalText()

    private fun logicalText(fields: MutableMap<Int, AgentBlockPart.Field>? = null): String = buildString {
        var afterTable = false
        parts.forEachIndexed { index, part ->
            val owned = if (fields == null) null else mutableMapOf<Int, AgentBlockPart.Field>()
            val text = when (part) {
                is AgentBlockPart.Text -> part.value
                is AgentBlockPart.Field -> {
                    owned?.set(0, part)
                    fieldText(part)
                }
                is AgentBlockPart.Table -> tableRecords(part.columns, part.rows, owned)
                is AgentBlockPart.Scan -> tableRecords(part.columns, part.rows, owned)
            }
            fun appendOwned(value: String) {
                owned?.forEach { (offset, field) -> fields?.set(length + offset, field) }
                append(value)
            }
            if (part is AgentBlockPart.Table || part is AgentBlockPart.Scan) {
                tableBoundary()
                appendOwned(text)
                tableBoundary()
                afterTable = true
            } else {
                if (afterTable) appendOwned(text.trimStart('\n')) else {
                    if (index > 0) append('\n')
                    appendOwned(text)
                }
                if (text.trimStart('\n').isNotEmpty()) afterTable = false
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
            is AgentBlockPart.Scan -> part.rows.forEachIndexed { row, values -> part.columns.forEachIndexed { column, label -> yield("AP ${row + 1} ${label.label}" to values[column]) } }
        }
    }

    /** Parts have already crossed their presentation boundary. Styling and line filtering must not redact
     * literal identities a second time (an SSID may itself contain password=). */
    fun withStyle(baseSizeSp: Float, minimumSizeSp: Float): AgentPresentationBlock {
        require(baseSizeSp.isFinite() && minimumSizeSp.isFinite() && baseSizeSp >= minimumSizeSp && minimumSizeSp >= 10f)
        return AgentPresentationBlock(parts, color, baseSizeSp, minimumSizeSp, safeValueFile, promptLength)
    }

    fun mapLogicalLines(transform: (List<IndexedValue<String>>) -> List<IndexedValue<String>>): AgentPresentationBlock {
        // Keep provenance through line filters; identical raw text cannot acquire a field's role.
        val offsets = mutableMapOf<Int, AgentBlockPart.Field>()
        val original = logicalText(offsets).split('\n').withIndex().toList()
        val fields = mutableMapOf<Int, AgentBlockPart.Field>()
        var offset = 0
        original.forEach { line ->
            offsets[offset]?.let { fields[line.index] = it }
            offset += line.value.length + 1
        }
        val lines = transform(original)
        val retained = buildList {
            var index = 0
            while (index < lines.size) {
                val line = lines[index]
                val field = fields[line.index]
                val size = field?.let { fieldText(it).count { ch -> ch == '\n' } + 1 } ?: 0
                if (field != null && index + size <= lines.size &&
                    lines.subList(index, index + size) == original.subList(line.index, line.index + size)) {
                    add(field)
                    index += size
                } else {
                    add(AgentBlockPart.Text(line.value))
                    index++
                }
            }
        }
        return AgentPresentationBlock(retained, color, baseSizeSp, minimumSizeSp, null)
    }

    fun overflowSummary(values: AgentSafeValueFile): AgentPresentationBlock {
        val labels = setOf("Source", "Target", "Status", "Message", "Latency", "Availability", "Observation age", "Received locally", "Error", "Reason", "Note")
        val important = parts.filterIndexed { index, part ->
            when (part) {
                is AgentBlockPart.Text -> index == 0 || labels.any { part.value.startsWith("$it:") }
                is AgentBlockPart.Field -> part.label in labels
                else -> false
            }
        }.take(64).map { part ->
            when (part) {
                is AgentBlockPart.Text -> if (part.value.length > 2048) AgentBlockPart.Text("${part.value.take(2048)}\nDisplay note truncated; long press and choose Full result for the full safe value") else part
                // Wire values are already in the file; retaining them here defeats the history budget.
                is AgentBlockPart.Field -> AgentBlockPart.Field(part.label, if (part.value.length > 2048) "${part.value.take(2048)} (display note abbreviated; long press and choose Full result)" else part.value)
                else -> part
            }
        }
        return AgentPresentationBlock(important + AgentBlockPart.Text(
            "shown=0; ${fullText.length} characters exceed in-memory history. Long press and choose Full result for all ${values.valueCount} retained safe values; no identity was clipped.",
        ), color, baseSizeSp, minimumSizeSp, values, promptLength)
    }

    companion object {
        /** Local input is intentionally echoed exactly, including credentials and malformed syntax. */
        fun commandLine(text: String, color: Int, promptLength: Int): AgentPresentationBlock {
            require(promptLength in 0..text.length)
            return AgentPresentationBlock(listOf(AgentBlockPart.Text(text)), color, 10f, 10f, null, promptLength)
        }

        fun create(parts: List<AgentBlockPart>, color: Int, baseSizeSp: Float = 10f, minimumSizeSp: Float = 10f, safeValueFile: AgentSafeValueFile? = null): AgentPresentationBlock {
            require(baseSizeSp.isFinite() && minimumSizeSp.isFinite() && baseSizeSp >= minimumSizeSp && minimumSizeSp >= 10f)
            return AgentPresentationBlock(parts.map { part ->
                when (part) {
                    is AgentBlockPart.Text -> AgentBlockPart.Text(AgentSafePresentation.text(part.value))
                    is AgentBlockPart.Field -> AgentBlockPart.Field(AgentSafePresentation.cell(part.label).trimEnd(), AgentSafePresentation.field(part.label, part.value), AgentSafePresentation.field(part.label, part.fullValue))
                    is AgentBlockPart.Table -> AgentBlockPart.Table(
                        part.columns.also { require(it.isNotEmpty() && part.rows.all { row -> row.size == it.size }) { "Invalid presentation table shape" } }.map { it.copy(label = AgentSafePresentation.cell(it.label).trimEnd()) },
                        part.rows.map { row -> row.mapIndexed { index, value -> AgentSafePresentation.field(part.columns.getOrNull(index)?.label.orEmpty(), value) } },
                    )
                    is AgentBlockPart.Scan -> {
                        require(part.columns.take(2).map { it.label } == listOf("SSID", "BSSID")) { "Invalid presentation scan identity columns" }
                        part.copy(
                            columns = part.columns.map { it.copy(label = AgentSafePresentation.cell(it.label).trimEnd()) },
                            rows = part.rows.map { row ->
                                require(row.size == part.columns.size) { "Invalid presentation scan record shape" }
                                row.mapIndexed { index, value -> AgentSafePresentation.field(part.columns[index].label, value) }
                            },
                        )
                    }
                }
            }, color, baseSizeSp, minimumSizeSp, safeValueFile)
        }
    }
}

private fun fieldText(label: String, value: String): String {
    val prefix = "${label.trimEnd()}: "
    return prefix + value.replace("\n", "\n" + " ".repeat(prefix.length))
}

private fun fieldText(field: AgentBlockPart.Field): String = fieldText(field.label, field.value) +
    if (field.fullValue != field.value) "\n${fieldText("${field.label} wire value (availability above)", field.fullValue)}" else ""

private fun tableRecords(columns: List<AgentBlockPart.Column>, rows: List<List<String>>, fields: MutableMap<Int, AgentBlockPart.Field>?): String = buildString {
    if (rows.isEmpty()) append(columns.joinToString("  ") { it.label })
    rows.forEachIndexed { rowIndex, row ->
        if (rowIndex > 0) append("\n\n")
        columns.forEachIndexed { index, column ->
            if (index > 0) append('\n')
            val field = AgentBlockPart.Field(column.label, row[index])
            fields?.set(length, field)
            append(fieldText(field))
        }
    }
}

private fun StringBuilder.tableBoundary() {
    while (isNotEmpty() && last() == '\n') setLength(length - 1)
    append(if (isEmpty()) "\n" else "\n\n")
}

internal val scanColumns = listOf(
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
        val labelKey = clean(label).trim().lowercase(Locale.ROOT)
        val key = when {
            labelKey.startsWith("expected ") -> labelKey.removePrefix("expected ")
            labelKey.startsWith("actual ") -> labelKey.removePrefix("actual ")
            else -> labelKey
        }
        return when {
            key in secretKeys -> "<redacted>"
            key in listOf("ssid", "bssid", "mld", "ap mac", "sta mac", "phy", "sec", "band", "interface", "network", "operator", "venue") -> clean(value).replace('\n', ' ')
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
        var afterTable = false
        val fieldWidths = FloatArray(block.parts.size)
        var start = 0
        while (start < block.parts.size) {
            var end = start
            var width = 0f
            while (end < block.parts.size && block.parts[end] is AgentBlockPart.Field) {
                width = maxOf(width, paint.measureText(fieldLabel((block.parts[end] as AgentBlockPart.Field).label) + ":"))
                end++
            }
            for (index in start until end) fieldWidths[index] = width
            start = maxOf(start + 1, end)
        }
        block.parts.forEachIndexed { index, part ->
            when (part) {
                is AgentBlockPart.Text -> {
                    val text = if (afterTable) part.value.trimStart('\n') else part.value
                    if (text.isNotEmpty() || !afterTable) appendWrapped(out, text, contentWidthPx, paint)
                    if (text.isNotEmpty()) afterTable = false
                }
                is AgentBlockPart.Field -> {
                    appendField(out, part.label, part.value, fieldWidths[index], contentWidthPx, paint)
                    afterTable = false
                }
                is AgentBlockPart.Table -> {
                    totalRecords += part.rows.size
                    val rows = part.rows.take(minOf(ROW_BUDGET - visibleRecords, LINE_BUDGET - out.count { it == '\n' } - tableBoundaryCost(out) - 3).coerceAtLeast(0))
                    if (!appendTable(out, part.columns, rows, contentWidthPx, paint)) {
                        val shown = appendRecords(out, part.columns, rows, contentWidthPx, paint)
                        visibleRecords += shown
                        afterTable = out.endsWith("\n\n")
                    } else {
                        visibleRecords += rows.size
                        afterTable = true
                    }
                }
                is AgentBlockPart.Scan -> {
                    totalRecords += part.rows.size
                    val rows = part.rows.take(minOf(ROW_BUDGET - visibleRecords, LINE_BUDGET - out.count { it == '\n' } - tableBoundaryCost(out) - 4).coerceAtLeast(0))
                    var shown = 0
                    if (!appendTable(out, part.columns, rows, contentWidthPx, paint)) {
                        if (rows.isEmpty()) appendRecords(out, part.columns, rows, contentWidthPx, paint)
                        else appendTableBoundary(out)
                        rows.groupBy { it[0] }.forEach groupLoop@ { (ssid, group) ->
                            val heading = SpannableStringBuilder()
                            appendField(heading, "SSID", ssid, paint.measureText(fieldLabel("SSID") + ":"), contentWidthPx, paint)
                            if (out.count { it == '\n' } + heading.count { it == '\n' } + 2 >= LINE_BUDGET) return@groupLoop
                            out.append(heading)
                            if (!appendTable(out, part.columns.drop(1), group.map { it.drop(1) }, contentWidthPx, paint)) {
                                group.forEach { row ->
                                    val record = SpannableStringBuilder()
                                    val labelWidth = (part.columns.drop(2).map { it.label } + "BSSID").maxOf { paint.measureText(fieldLabel(it) + ":") }
                                    if (!appendTable(record, part.columns.drop(2), listOf(row.drop(2)), contentWidthPx, paint)) {
                                        part.columns.drop(2).forEachIndexed { i, column -> appendField(record, column.label, row[i + 2], labelWidth, contentWidthPx, paint) }
                                    }
                                    appendField(record, "BSSID", row[1], labelWidth, contentWidthPx, paint)
                                    if (out.count { it == '\n' } + record.count { it == '\n' } + 2 <= LINE_BUDGET) {
                                        out.append(record)
                                        shown++
                                    }
                                }
                            } else shown += group.size
                        }
                        if (rows.isNotEmpty()) appendTableBoundary(out)
                    } else shown = rows.size
                    visibleRecords += shown
                    appendWrapped(out, "AP display: shown=$shown total=${part.rows.size}", contentWidthPx, paint)
                    afterTable = false
                }
            }
        }
        if (totalRecords > visibleRecords) appendWrapped(out, "shown=$visibleRecords total=$totalRecords; 96-row / 240-line display budget; long press and choose Full result for all retained safe values", contentWidthPx, paint)
        if (block.promptLength > 0) {
            var end = 0
            var remaining = block.promptLength
            // Reflow adds line breaks; the prompt keeps its color even across those breaks.
            while (remaining > 0 && end < out.length) if (out[end++] != '\n') remaining--
            out.setSpan(ForegroundColorSpan(Color.YELLOW), 0, end, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE)
        }
        return AgentBlockLayout(out, block.baseSizeSp)
    }

    private fun appendRecords(out: SpannableStringBuilder, columns: List<AgentBlockPart.Column>, rows: List<List<String>>, width: Int, paint: Paint): Int {
        if (rows.isEmpty()) {
            val header = SpannableStringBuilder()
            columns.forEach { appendWrapped(header, it.label, width, paint) }
            if (out.count { it == '\n' } + tableBoundaryCost(out) + header.count { it == '\n' } + 1 <= LINE_BUDGET) {
                appendTableBoundary(out)
                out.append(header)
                appendTableBoundary(out)
            }
            return 0
        }
        var shown = 0
        val labelWidth = columns.maxOf { paint.measureText(fieldLabel(it.label) + ":") }
        for (row in rows) {
            val record = SpannableStringBuilder()
            columns.forEachIndexed { i, column -> appendField(record, column.label, row.getOrElse(i) { "?" }, labelWidth, width, paint) }
            if (out.count { it == '\n' } + record.count { it == '\n' } + (if (shown == 0) tableBoundaryCost(out) else 0) + 2 > LINE_BUDGET) break
            if (shown == 0) appendTableBoundary(out)
            out.append(record)
            shown++
        }
        if (shown > 0) appendTableBoundary(out)
        return shown
    }

    private fun fieldLabel(label: String): String = if (label.startsWith(' ')) label else "  $label"

    private fun appendField(out: SpannableStringBuilder, key: String, value: String, labelWidth: Float, width: Int, paint: Paint) {
        val label = fieldLabel(key)
        val valueStart = labelWidth + paint.measureText(" ")
        if (valueStart + paint.measureText("MMMMMMMM") > width) {
            val depth = label.takeWhile { it == ' ' }.length + 2
            val fitting = ((width - paint.measureText("MM")) / paint.measureText(" ")).toInt().coerceAtLeast(0)
            val indent = " ".repeat(minOf(depth, fitting))
            val heading = SpannableStringBuilder()
            appendWrapped(heading, label, (width - paint.measureText(":")).toInt().coerceAtLeast(1), paint, indent)
            heading.delete(heading.length - 1, heading.length).append(":\n")
            out.append(heading)
            appendWrapped(out, indent + value.replace("\n", "\n$indent"), width, paint, indent)
            return
        }
        val wrapped = SpannableStringBuilder()
        appendWrapped(wrapped, value, (width - valueStart).toInt(), paint)
        val start = out.length
        wrapped.toString().removeSuffix("\n").split('\n').forEachIndexed { index, line ->
            if (index == 0) out.append(label).append(':')
            out.append('\t')
            out.append(line).append('\n')
        }
        // Native tab stops keep values aligned across fallback fonts, like table cells.
        out.setSpan(TabStopSpan.Standard(kotlin.math.ceil(valueStart).toInt()), start, out.length, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE)
    }

    fun shrinkFloor(baseSizeSp: Float, minimumSizeSp: Float): Float = maxOf(baseSizeSp * 0.9f, 10f, minimumSizeSp)

    private fun appendTable(out: SpannableStringBuilder, columns: List<AgentBlockPart.Column>, rows: List<List<String>>, width: Int, paint: Paint): Boolean {
        if (columns.isEmpty()) return false
        if (rows.any { row -> row.any { '\n' in it } }) return false
        if (out.count { it == '\n' } + tableBoundaryCost(out) + rows.size + 2 > LINE_BUDGET) return false
        val widths = columns.indices.map { i ->
            kotlin.math.ceil((listOf(columns[i].label) + rows.map { it.getOrElse(i) { "?" } }).maxOf { paint.measureText(it) }).toFloat()
        }
        val space = paint.measureText(" ")
        val gap = kotlin.math.ceil(space * 2).toFloat()
        if (widths.sum() + gap * (columns.size - 1) > width) return false
        val stops = mutableListOf<Int>()
        var x = 0f
        widths.dropLast(1).forEach { w -> x += w + gap; stops += kotlin.math.ceil(x).toInt() }
        appendTableBoundary(out)
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
        appendTableBoundary(out)
        return true
    }

    private fun tableBoundaryCost(out: CharSequence): Int {
        val contentEnd = out.indexOfLast { it != '\n' } + 1
        return (if (contentEnd == 0) 1 else 2) - (out.length - contentEnd)
    }

    private fun appendTableBoundary(out: SpannableStringBuilder) {
        val contentEnd = out.indexOfLast { it != '\n' } + 1
        out.delete(contentEnd, out.length)
        out.append(if (contentEnd == 0) "\n" else "\n\n")
    }

    private fun appendWrapped(out: SpannableStringBuilder, value: String, width: Int, paint: Paint, continuationIndent: String = "") {
        value.split('\n').forEach { line ->
            val requestedIndent = continuationIndent.ifEmpty { line.takeWhile { it == ' ' } }
            val hanging = requestedIndent.takeIf { paint.measureText(it) < width }.orEmpty()
            val continuationWidth = if (hanging.isEmpty()) 0f else paint.measureText(hanging)
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
                    indent = hanging
                    indentWidth = continuationWidth
                }
                end = next
                next = boundaries.next()
            }
            out.append(indent).append(line.substring(start)).append('\n')
        }
    }
}
