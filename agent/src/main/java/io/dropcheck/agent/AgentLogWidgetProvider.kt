package io.dropcheck.agent

import android.appwidget.AppWidgetManager
import android.appwidget.AppWidgetProvider
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Bundle
import android.os.SystemClock
import android.widget.RemoteViews
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicLong

@Suppress("DEPRECATION")
/**
 * App widget provider for the bounded terminal log tail.
 *
 * Widget rows are backed by [AgentLogWidgetService] so large logs can be
 * refreshed without rebuilding every RemoteViews item in this provider.
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
            val lineCount = AgentLogWidgetLines.count(context)
            appWidgetIds.forEach { appWidgetId ->
                val serviceIntent = Intent(context, AgentLogWidgetService::class.java).apply {
                    putExtra(AppWidgetManager.EXTRA_APPWIDGET_ID, appWidgetId)
                    data = Uri.parse(toUri(Intent.URI_INTENT_SCHEME))
                }
                val views = RemoteViews(context.packageName, R.layout.agent_log_widget).apply {
                    setRemoteAdapter(R.id.agentLogWidgetList, serviceIntent)
                    setEmptyView(R.id.agentLogWidgetList, R.id.agentLogWidgetEmpty)
                    if (lineCount > 0) {
                        setScrollPosition(R.id.agentLogWidgetList, lineCount - 1)
                    }
                }
                appWidgetManager.updateAppWidget(appWidgetId, views)
                appWidgetManager.notifyAppWidgetViewDataChanged(appWidgetId, R.id.agentLogWidgetList)
            }
        }

        private const val UPDATE_DEBOUNCE_MS = 500L
        private const val MIN_UPDATE_INTERVAL_MS = 2_000L
    }
}
