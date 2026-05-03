package io.dropcheck.agent

import android.content.Context
import android.content.Intent
import android.widget.RemoteViews
import android.widget.RemoteViewsService

/** RemoteViews service that adapts [TerminalLog] tail lines into widget rows. */
class AgentLogWidgetService : RemoteViewsService() {
    override fun onGetViewFactory(intent: Intent): RemoteViewsFactory {
        return AgentLogWidgetFactory(applicationContext)
    }
}

private class AgentLogWidgetFactory(
    private val context: Context,
) : RemoteViewsService.RemoteViewsFactory {
    private var lines: List<WidgetLogLine> = emptyList()

    override fun onCreate() {
        loadLines()
    }

    override fun onDataSetChanged() {
        loadLines()
    }

    override fun onDestroy() {
        lines = emptyList()
    }

    override fun getCount(): Int = lines.size

    override fun getViewAt(position: Int): RemoteViews {
        val line = lines.getOrNull(position)?.text.orEmpty()
        return RemoteViews(context.packageName, R.layout.agent_log_widget_item).apply {
            setTextViewText(R.id.agentLogWidgetLine, terminalDisplayText(line))
            setTextColor(R.id.agentLogWidgetLine, AgentLogStyle.colorForLine(line))
        }
    }

    override fun getLoadingView(): RemoteViews? = null

    override fun getViewTypeCount(): Int = 1

    override fun getItemId(position: Int): Long = lines.getOrNull(position)?.id ?: position.toLong()

    override fun hasStableIds(): Boolean = true

    private fun loadLines() {
        lines = AgentLogWidgetLines.load(context)
    }
}

/** Loads and bounds terminal log rows for the RemoteViews-backed log widget. */
internal object AgentLogWidgetLines {
    fun load(context: Context): List<WidgetLogLine> {
        return fromTail(TerminalLog.tail(context, TerminalDisplayPolicy.MAX_DISPLAY_LINES))
    }

    fun count(context: Context): Int = load(context).size

    fun fromTail(tail: String): List<WidgetLogLine> {
        val displayLines = ArrayDeque<String>()
        var displayChars = 0

        tail.lineSequence()
            .forEach { rawLine ->
                val line = TerminalDisplayPolicy.boundedLine(rawLine, appendNewline = false)
                val lineChars = TerminalDisplayPolicy.displayLength(line)
                displayLines.addLast(line)
                displayChars += lineChars

                while (
                    displayLines.isNotEmpty() &&
                    (displayLines.size > TerminalDisplayPolicy.MAX_DISPLAY_LINES ||
                        displayChars > TerminalDisplayPolicy.MAX_DISPLAY_CHARS)
                ) {
                    displayChars -= TerminalDisplayPolicy.displayLength(displayLines.removeFirst())
                }
            }
        return displayLines.map { WidgetLogLine(id = it.hashCode().toLong(), text = it) }
    }
}

/** Stable row model rendered by the log widget collection adapter. */
internal data class WidgetLogLine(
    val id: Long,
    val text: String,
)
