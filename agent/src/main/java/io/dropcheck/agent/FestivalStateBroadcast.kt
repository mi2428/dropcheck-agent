package io.dropcheck.agent

import android.content.Context
import android.content.Intent

/** Broadcasts standalone mode state to the on-device activity. */
internal object FestivalStateBroadcast {
    const val ACTION = "io.dropcheck.agent.FESTIVAL_STATE"
    const val EXTRA_ENABLED = "enabled"

    fun send(context: Context, enabled: Boolean) {
        context.sendBroadcast(Intent(ACTION).apply {
            setPackage(context.packageName)
            putExtra(EXTRA_ENABLED, enabled)
        })
    }
}
