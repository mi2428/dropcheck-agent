package io.dropcheck.agent

import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicReference

/** Process-local status exposed through GetStandaloneStatus. */
internal object StandaloneRuntimeState {
    val active = AtomicBoolean(false)
    val running = AtomicBoolean(false)
    val currentRunId = AtomicReference("")
    val message = AtomicReference("")
}
