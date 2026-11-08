package io.dropcheck.agent

import java.io.File
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ClockWidgetLifecycleContractTest {
    @Test
    fun providerMaintainsWidgetServiceWithoutRestoringTheObserver() {
        val provider = source("AgentClockWidgetProvider.kt")
        val service = source("AgentService.kt")

        assertTrue(provider.contains("AgentService.requestWidgetUpdates(context)"))
        assertTrue(provider.contains("AgentService.stopWidgetUpdates(context)"))
        assertTrue(provider.contains("if (appWidgetIds.isEmpty()) return"))
        assertTrue(service.contains("io.dropcheck.agent.action.WIDGET_START"))
        assertTrue(service.contains("io.dropcheck.agent.action.WIDGET_STOP"))
        assertTrue(service.contains("ACTION_WIDGET_START"))
        assertTrue(service.contains("ACTION_WIDGET_STOP"))
        assertTrue(service.contains("return AgentClockWidgetProvider.hasClockWidgets(this)"))
        assertFalse(provider.contains("ClockWidgetRefreshObserver"))
        assertFalse(service.contains("ACTION_WIDGET_REFRESH_OBSERVER"))
        assertFalse(service.contains("ACTION_WIDGET_REFRESH_STOP"))
    }

    @Test
    fun updateQueueIsGuardedAndUpdateErrorsAreLogged() {
        val provider = source("AgentClockWidgetProvider.kt")

        assertTrue(provider.contains("if (!hasClockWidgets(appContext)) return"))
        assertTrue(provider.contains("widgetUpdateQueue.request"))
        assertTrue(provider.contains("Clock widget update failed"))
        assertTrue(provider.contains("Clock widget update could not be queued"))
    }

    private fun source(name: String): String {
        val sourceDir = listOf(
            File("src/main/java/io/dropcheck/agent"),
            File("agent/src/main/java/io/dropcheck/agent"),
        ).first { it.isDirectory }
        return File(sourceDir, name).readText()
    }
}
