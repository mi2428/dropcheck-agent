package io.dropcheck.agent

import android.content.Context
import android.content.Intent
import android.widget.RemoteViews
import android.widget.RemoteViewsService

class AgentLogWidgetService : RemoteViewsService() {
    override fun onGetViewFactory(intent: Intent): RemoteViewsFactory {
        return AgentLogWidgetFactory(applicationContext)
    }
}

private class AgentLogWidgetFactory(
    private val context: Context,
) : RemoteViewsService.RemoteViewsFactory {
    private var lines: List<String> = emptyList()

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
        val line = lines.getOrNull(position).orEmpty()
        return RemoteViews(context.packageName, R.layout.agent_log_widget_item).apply {
            setTextViewText(R.id.agentLogWidgetLine, terminalDisplayText(line))
        }
    }

    override fun getLoadingView(): RemoteViews? = null

    override fun getViewTypeCount(): Int = 1

    override fun getItemId(position: Int): Long = position.toLong()

    override fun hasStableIds(): Boolean = false

    private fun loadLines() {
        val displayLines = ArrayDeque<String>()
        var displayChars = 0

        TerminalLog.tail(context, TerminalDisplayPolicy.MAX_DISPLAY_LINES)
            .lineSequence()
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
        lines = displayLines.toList()
    }
}
