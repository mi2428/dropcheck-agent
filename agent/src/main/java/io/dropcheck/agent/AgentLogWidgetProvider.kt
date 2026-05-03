package io.dropcheck.agent

import android.appwidget.AppWidgetManager
import android.appwidget.AppWidgetProvider
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Bundle
import android.widget.RemoteViews

@Suppress("DEPRECATION")
class AgentLogWidgetProvider : AppWidgetProvider() {
    override fun onUpdate(context: Context, appWidgetManager: AppWidgetManager, appWidgetIds: IntArray) {
        updateWidgets(context, appWidgetManager, appWidgetIds)
        recordLayoutVersion(context)
    }

    override fun onAppWidgetOptionsChanged(
        context: Context,
        appWidgetManager: AppWidgetManager,
        appWidgetId: Int,
        newOptions: Bundle,
    ) {
        updateWidgets(context, appWidgetManager, intArrayOf(appWidgetId))
        recordLayoutVersion(context)
    }

    companion object {
        private const val WIDGET_PREFS = "agent_log_widget"
        private const val WIDGET_LAYOUT_VERSION_KEY = "layout_version"
        private const val WIDGET_LAYOUT_VERSION = 2

        fun updateAll(context: Context) {
            val appContext = context.applicationContext
            val manager = AppWidgetManager.getInstance(appContext)
            val component = ComponentName(appContext, AgentLogWidgetProvider::class.java)
            val appWidgetIds = manager.getAppWidgetIds(component)
            if (appWidgetIds.isEmpty()) return

            if (layoutVersion(appContext) != WIDGET_LAYOUT_VERSION) {
                updateWidgets(appContext, manager, appWidgetIds)
                recordLayoutVersion(appContext)
                return
            }

            manager.notifyAppWidgetViewDataChanged(appWidgetIds, R.id.agentLogWidgetList)
        }

        private fun updateWidgets(context: Context, appWidgetManager: AppWidgetManager, appWidgetIds: IntArray) {
            appWidgetIds.forEach { appWidgetId ->
                val serviceIntent = Intent(context, AgentLogWidgetService::class.java).apply {
                    putExtra(AppWidgetManager.EXTRA_APPWIDGET_ID, appWidgetId)
                    data = Uri.parse(toUri(Intent.URI_INTENT_SCHEME))
                }
                val views = RemoteViews(context.packageName, R.layout.agent_log_widget).apply {
                    setRemoteAdapter(R.id.agentLogWidgetList, serviceIntent)
                    setEmptyView(R.id.agentLogWidgetList, R.id.agentLogWidgetEmpty)
                }
                appWidgetManager.updateAppWidget(appWidgetId, views)
                appWidgetManager.notifyAppWidgetViewDataChanged(appWidgetId, R.id.agentLogWidgetList)
            }
        }

        private fun layoutVersion(context: Context): Int {
            return context.getSharedPreferences(WIDGET_PREFS, Context.MODE_PRIVATE)
                .getInt(WIDGET_LAYOUT_VERSION_KEY, 0)
        }

        private fun recordLayoutVersion(context: Context) {
            context.getSharedPreferences(WIDGET_PREFS, Context.MODE_PRIVATE)
                .edit()
                .putInt(WIDGET_LAYOUT_VERSION_KEY, WIDGET_LAYOUT_VERSION)
                .apply()
        }
    }
}
