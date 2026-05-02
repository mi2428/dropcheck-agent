package io.dropcheck.agent

import android.content.Context
import io.dropcheck.agent.grpc.ControllerLinkConfig
import java.io.File

/** Persists the controller direct-TCP reconnect endpoint inside app storage. */
internal class ControllerLinkStore internal constructor(private val file: File) {
    constructor(context: Context) : this(File(context.filesDir, "controller-link/config.pb"))

    /** Returns the stored endpoint, or the protobuf default when unset/corrupt. */
    @Synchronized
    fun load(): ControllerLinkConfig {
        if (!file.exists()) return ControllerLinkConfig.getDefaultInstance()
        return runCatching { ControllerLinkConfig.parseFrom(file.readBytes()) }
            .getOrDefault(ControllerLinkConfig.getDefaultInstance())
    }

    /** Atomically replaces the reconnect endpoint config in app-private storage. */
    @Synchronized
    fun save(config: ControllerLinkConfig) {
        file.parentFile?.mkdirs()
        file.writeBytes(config.toByteArray())
    }
}
