package io.dropcheck.agent

import android.app.PendingIntent
import android.appwidget.AppWidgetManager
import android.appwidget.AppWidgetProvider
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.os.Bundle
import android.os.SystemClock
import android.text.SpannableString
import android.text.SpannableStringBuilder
import android.text.Spanned
import android.text.style.ForegroundColorSpan
import android.widget.RemoteViews
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicLong

@Suppress("DEPRECATION")
/**
 * App widget provider for the bounded terminal log tail.
 */
class AgentLogWidgetProvider : AppWidgetProvider() {
    override fun onUpdate(context: Context, appWidgetManager: AppWidgetManager, appWidgetIds: IntArray) {
        updateWidgets(context, appWidgetManager, appWidgetIds)
    }

    override fun onAppWidgetOptionsChanged(
        context: Context,
        appWidgetManager: AppWidgetManager,
        appWidgetId: Int,
        newOptions: Bundle,
    ) {
        updateWidgets(context, appWidgetManager, intArrayOf(appWidgetId))
    }

    companion object {
        private val updatePending = AtomicBoolean(false)
        private val lastUpdateElapsedMs = AtomicLong(0)
        private val updateExecutor = Executors.newSingleThreadScheduledExecutor { task ->
            Thread(task, "dropcheck-log-widget-update")
        }

        fun requestUpdate(context: Context) {
            val appContext = context.applicationContext
            if (!updatePending.compareAndSet(false, true)) return
            val elapsedMs = SystemClock.elapsedRealtime()
            val nextAllowedMs = lastUpdateElapsedMs.get() + MIN_UPDATE_INTERVAL_MS
            val delayMs = maxOf(UPDATE_DEBOUNCE_MS, nextAllowedMs - elapsedMs)
            updateExecutor.schedule(
                {
                    try {
                        updateAll(appContext)
                        lastUpdateElapsedMs.set(SystemClock.elapsedRealtime())
                    } finally {
                        updatePending.set(false)
                    }
                },
                delayMs,
                TimeUnit.MILLISECONDS,
            )
        }

        fun updateAll(context: Context) {
            val appContext = context.applicationContext
            val manager = AppWidgetManager.getInstance(appContext)
            val component = ComponentName(appContext, AgentLogWidgetProvider::class.java)
            val appWidgetIds = manager.getAppWidgetIds(component)
            if (appWidgetIds.isEmpty()) return

            updateWidgets(appContext, manager, appWidgetIds)
        }

        private fun updateWidgets(context: Context, appWidgetManager: AppWidgetManager, appWidgetIds: IntArray) {
            appWidgetIds.forEach { appWidgetId ->
                val lines = AgentLogWidgetLines.load(context)
                val views = RemoteViews(context.packageName, R.layout.agent_log_widget).apply {
                    setTextViewText(R.id.agentLogWidgetText, widgetText(context, lines))
                    setOnClickPendingIntent(R.id.agentLogWidgetRoot, logViewerPendingIntent(context, appWidgetId))
                }
                appWidgetManager.updateAppWidget(appWidgetId, views)
            }
        }

        private fun widgetText(context: Context, lines: List<WidgetLogLine>): CharSequence {
            if (lines.isEmpty()) return context.getString(R.string.agent_log_widget_placeholder)
            return SpannableStringBuilder().apply {
                lines.forEachIndexed { index, line ->
                    if (index > 0) append('\n')
                    append(coloredLine(line.text))
                }
            }
        }

        private fun coloredLine(line: String): CharSequence {
            val terminalLine = terminalDisplayText(line)
            return SpannableString(terminalLine).apply {
                setSpan(
                    ForegroundColorSpan(AgentLogStyle.colorForLine(line)),
                    0,
                    terminalLine.length,
                    Spanned.SPAN_EXCLUSIVE_EXCLUSIVE,
                )
            }
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
        private const val MIN_UPDATE_INTERVAL_MS = 2_000L
    }
}
