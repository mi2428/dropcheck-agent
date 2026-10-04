package io.dropcheck.agent

import android.app.Activity
import android.app.Instrumentation
import android.content.ComponentName
import android.content.Intent
import android.os.Bundle
import android.os.Process

private const val AGENT_PACKAGE = "io.dropcheck.agent"
private const val SERVICE = "$AGENT_PACKAGE.AgentService"
private const val ACTION_PREFIX = "$AGENT_PACKAGE.action."

/** Run in the agent UID to cover the widget path without creating a controller connection. */
class ServiceSameUidInstrumentation : Instrumentation() {
    override fun onCreate(arguments: Bundle?) {
        super.onCreate(arguments)
        start()
    }

    override fun onStart() {
        val result = Bundle()
        try {
            check(Process.myUid() == targetContext.applicationInfo.uid)
            check(targetContext.packageName == AGENT_PACKAGE)
            val component = ComponentName(AGENT_PACKAGE, SERVICE)
            for (action in listOf("WIDGET_REFRESH_OBSERVER", "WIDGET_REFRESH_STOP")) {
                check(targetContext.startForegroundService(
                    Intent().setComponent(component).setAction(ACTION_PREFIX + action),
                ) == component)
                waitForIdleSync()
            }
            result.putString("stream", "PASS: same-UID widget start/stop accepted\n")
            finish(Activity.RESULT_OK, result)
        } catch (failure: Throwable) {
            result.putString("stream", "FAIL: ${failure.javaClass.simpleName}: ${failure.message}\n")
            finish(Activity.RESULT_CANCELED, result)
        }
    }
}
