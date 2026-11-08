package io.dropcheck.agent

import android.app.PendingIntent
import android.appwidget.AppWidgetManager
import android.appwidget.AppWidgetProvider
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.os.Bundle
import android.text.SpannableString
import android.text.SpannableStringBuilder
import android.text.Spanned
import android.text.style.ForegroundColorSpan
import android.widget.RemoteViews

/** A bounded latest-log snapshot; full scrollback belongs to the app. */
class AgentLogWidgetProvider : AppWidgetProvider() {
    override fun onUpdate(context: Context, appWidgetManager: AppWidgetManager, appWidgetIds: IntArray) {
        requestUpdate(context)
    }

    override fun onDisabled(context: Context) {
        updates.cancel()
        super.onDisabled(context)
    }

    override fun onAppWidgetOptionsChanged(
        context: Context,
        appWidgetManager: AppWidgetManager,
        appWidgetId: Int,
        newOptions: Bundle,
    ) {
        requestUpdate(context)
    }

    companion object {
        private val updates = WidgetUpdateQueue("dropcheck-log-widget-update", UPDATE_DEBOUNCE_MS, MIN_UPDATE_INTERVAL_MS)

        fun requestUpdate(context: Context) {
            val appContext = context.applicationContext
            val manager = AppWidgetManager.getInstance(appContext)
            if (manager.getAppWidgetIds(ComponentName(appContext, AgentLogWidgetProvider::class.java)).isEmpty()) return
            updates.request {
                try { updateAll(appContext) } catch (failure: RuntimeException) {
                    // TerminalLog would request this widget again and recurse on a persistent failure.
                    android.util.Log.e("dropcheck", "Log widget update failed", failure)
                    throw failure
                }
            }
        }

        private fun updateAll(context: Context) {
            val appContext = context.applicationContext
            val manager = AppWidgetManager.getInstance(appContext)
            val component = ComponentName(appContext, AgentLogWidgetProvider::class.java)
            val appWidgetIds = manager.getAppWidgetIds(component)
            if (appWidgetIds.isEmpty()) return

            val lines = AgentLogWidgetLines.load(appContext)
            appWidgetIds.forEach { manager.updateAppWidget(it, remoteViews(appContext, it, lines)) }
        }

        internal fun remoteViews(
            context: Context,
            appWidgetId: Int,
            lines: List<String> = AgentLogWidgetLines.load(context),
        ): RemoteViews = RemoteViews(context.packageName, R.layout.agent_log_widget).apply {
            setTextViewText(R.id.agentLogWidgetText, if (lines.isEmpty()) context.getString(R.string.agent_log_widget_placeholder) else
                SpannableStringBuilder().apply {
                    lines.forEachIndexed { index, line ->
                        if (index > 0) append('\n')
                        append(SpannableString(terminalDisplayText(line)).apply {
                            setSpan(ForegroundColorSpan(AgentLogStyle.colorForLine(line)), 0, length, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE)
                        })
                    }
                })
            setOnClickPendingIntent(R.id.agentLogWidgetRoot, logViewerPendingIntent(context, appWidgetId))
        }

        private fun logViewerPendingIntent(context: Context, appWidgetId: Int): PendingIntent {
            val intent = Intent(context, MainActivity::class.java).apply {
                action = ACTION_OPEN_LOG_VIEWER
                addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP or Intent.FLAG_ACTIVITY_SINGLE_TOP)
            }
            return PendingIntent.getActivity(
                context,
                appWidgetId,
                intent,
                PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
            )
        }

        private const val UPDATE_DEBOUNCE_MS = 500L
        internal const val MIN_UPDATE_INTERVAL_MS = 5_000L
    }
}
