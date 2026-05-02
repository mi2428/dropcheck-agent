package io.dropcheck.agent

import android.content.Context
import io.dropcheck.agent.grpc.FestivalConfig
import java.io.File

/** Persists the Dropcheck Festival standalone configuration inside app storage. */
internal class FestivalConfigStore(context: Context) {
    private val file = File(context.filesDir, "festival/config.pb")

    @Synchronized
    fun load(): FestivalConfig {
        if (!file.exists()) return FestivalConfig.getDefaultInstance()
        return runCatching { FestivalConfig.parseFrom(file.readBytes()) }
            .getOrDefault(FestivalConfig.getDefaultInstance())
    }

    @Synchronized
    fun save(config: FestivalConfig) {
        file.parentFile?.mkdirs()
        file.writeBytes(config.toByteArray())
    }
}
