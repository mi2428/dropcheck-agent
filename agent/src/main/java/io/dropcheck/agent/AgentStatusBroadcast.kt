package io.dropcheck.agent

import android.content.Context
import android.content.Intent

/** Broadcasts process-local status used by the on-device activity chrome. */
internal object AgentStatusBroadcast {
    const val ACTION = "io.dropcheck.agent.AGENT_STATUS"
    const val EXTRA_CONTROLLER_HEARTBEAT_CONNECTED = "controller_heartbeat_connected"
    const val EXTRA_STANDALONE_RUNNING = "standalone_running"

    fun send(context: Context) {
        context.sendBroadcast(Intent(ACTION).apply {
            setPackage(context.packageName)
            putExtra(EXTRA_CONTROLLER_HEARTBEAT_CONNECTED, ControllerSessionRuntimeState.heartbeatConnected())
            putExtra(EXTRA_STANDALONE_RUNNING, StandaloneRuntimeState.running.get())
        })
    }
}
