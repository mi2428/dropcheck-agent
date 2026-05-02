package io.dropcheck.agent

import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicReference

/** Process-local status exposed through GetFestivalStatus. */
internal object FestivalRuntimeState {
    val running = AtomicBoolean(false)
    val currentRunId = AtomicReference("")
    val message = AtomicReference("")
}
