package io.dropcheck.agent

import android.content.Context
import io.dropcheck.agent.grpc.StandaloneConfig
import java.io.File

/** Persists the standalone connectivity configuration inside app storage. */
internal class StandaloneConfigStore(context: Context) {
    private val file = File(context.filesDir, "standalone/config.pb")

    @Synchronized
    fun load(): StandaloneConfig {
        if (!file.exists()) return StandaloneConfig.getDefaultInstance()
        return runCatching { StandaloneConfig.parseFrom(file.readBytes()) }
            .getOrDefault(StandaloneConfig.getDefaultInstance())
    }

    @Synchronized
    fun save(config: StandaloneConfig) {
        file.parentFile?.mkdirs()
        file.writeBytes(config.toByteArray())
    }
}
