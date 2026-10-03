package io.dropcheck.agent

import android.Manifest
import android.app.Activity
import android.app.Instrumentation
import android.content.ComponentName
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Bundle
import android.os.Process
import android.widget.TextView
import java.io.File

private const val AGENT_PACKAGE = "io.dropcheck.agent"
private const val SERVICE = "$AGENT_PACKAGE.AgentService"
private const val ACTION_PREFIX = "$AGENT_PACKAGE.action."

/** A separate-UID test Activity; launching it does not restart an active agent session. */
class UntrustedServiceCallerActivity : Activity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val verdict = File(filesDir, "service-caller-result")
        try {
            val component = ComponentName(AGENT_PACKAGE, SERVICE)
            @Suppress("DEPRECATION")
            val service = packageManager.getServiceInfo(component, 0)
            check(Process.myUid() != service.applicationInfo.uid) { "denial test must use a separate UID" }
            check(service.exported && service.permission == Manifest.permission.DUMP)
            check(checkSelfPermission(Manifest.permission.DUMP) == PackageManager.PERMISSION_DENIED)
            for (action in listOf("GRPC_SESSION", "WIDGET_REFRESH_OBSERVER", "WIDGET_REFRESH_STOP")) {
                val intent = Intent().setComponent(component).setAction(ACTION_PREFIX + action)
                    .putExtra("grpc_host", "127.0.0.1").putExtra("grpc_port", 43123)
                    .putExtra("grpc_token", "synthetic-denied-session")
                val denied = try {
                    startForegroundService(intent)
                    false
                } catch (expected: SecurityException) {
                    check(expected.message.orEmpty().contains(Manifest.permission.DUMP)) {
                        "denial must be the component permission, not an FGS restriction"
                    }
                    true
                }
                check(denied) { "cross-UID $action was accepted" }
            }
            val message = "PASS: separate-UID starts denied by DUMP; no shell identity adopted"
            verdict.writeText(message + "\n")
            setContentView(TextView(this).apply { text = message })
        } catch (failure: Throwable) {
            verdict.writeText("FAIL: ${failure.javaClass.simpleName}: ${failure.message}\n")
            throw failure
        }
    }
}

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
