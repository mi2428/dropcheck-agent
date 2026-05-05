package io.dropcheck.agent

import android.annotation.SuppressLint
import android.content.Context
import android.net.wifi.WifiManager
import io.dropcheck.agent.grpc.CommandLog
import io.dropcheck.agent.grpc.CommandResult
import io.dropcheck.agent.grpc.ConnectWifi
import io.dropcheck.agent.grpc.RunCommand
import io.dropcheck.agent.grpc.StandaloneConfig
import io.dropcheck.agent.grpc.StandaloneWifiGroup

/** Pure helpers for using Wi-Fi targets registered under `standalone festa live`. */
internal object StandaloneWifiUsePolicy {
    const val LIVE_FESTA_NAME = "live"
    const val DEFAULT_USE_TIMEOUT_MS = 35_000

    fun liveWifiNames(config: StandaloneConfig): List<String> {
        return liveFesta(config)?.wifiGroupsList
            .orEmpty()
            .mapNotNull { it.name.takeIf(String::isNotBlank) }
    }

    fun selectLiveWifi(config: StandaloneConfig, name: String): StandaloneWifiGroup? {
        return liveFesta(config)?.wifiGroupsList?.firstOrNull { it.name == name }
    }

    fun connectCommand(group: StandaloneWifiGroup, essid: String): RunCommand {
        return RunCommand.newBuilder()
            .setLabel("use ${group.name.ifBlank { essid }}")
            .setConnectWifi(ConnectWifi.newBuilder()
                .setSsid(essid)
                .setPassphrase(group.passphrase)
                .setSecurity(group.security)
                .setBssid(group.bssid)
                .setBand(group.band)
                .setMacRandomization(group.macRandomization)
                .setTimeoutMs(group.timeoutMs.takeIf { it > 0 } ?: DEFAULT_USE_TIMEOUT_MS))
            .build()
    }

    private fun liveFesta(config: StandaloneConfig) = config.festasList.firstOrNull { it.name == LIVE_FESTA_NAME }
}

internal data class StandaloneUseState(
    val wifiName: String,
    val previousStandaloneEnabled: Boolean,
)

/** Keeps repeated `use` commands from overwriting the original standalone state. */
internal object StandaloneUseStatePolicy {
    fun beginUse(wifiName: String, currentStandaloneEnabled: Boolean, active: StandaloneUseState?): StandaloneUseState {
        return StandaloneUseState(
            wifiName = wifiName,
            previousStandaloneEnabled = active?.previousStandaloneEnabled ?: currentStandaloneEnabled,
        )
    }
}

/** Persists the shell `use` override so `clear use` can restore standalone mode. */
internal class StandaloneUseStateStore(context: Context) {
    private val prefs = context.applicationContext.getSharedPreferences("standalone-use", Context.MODE_PRIVATE)

    fun load(): StandaloneUseState? {
        if (!prefs.getBoolean(KEY_ACTIVE, false)) return null
        return StandaloneUseState(
            wifiName = prefs.getString(KEY_WIFI_NAME, "").orEmpty(),
            previousStandaloneEnabled = prefs.getBoolean(KEY_PREVIOUS_ENABLED, false),
        )
    }

    fun save(state: StandaloneUseState) {
        prefs.edit()
            .putBoolean(KEY_ACTIVE, true)
            .putString(KEY_WIFI_NAME, state.wifiName)
            .putBoolean(KEY_PREVIOUS_ENABLED, state.previousStandaloneEnabled)
            .apply()
    }

    fun clear(): StandaloneUseState? {
        val state = load()
        prefs.edit().clear().apply()
        return state
    }

    private companion object {
        const val KEY_ACTIVE = "active"
        const val KEY_WIFI_NAME = "wifi_name"
        const val KEY_PREVIOUS_ENABLED = "previous_enabled"
    }
}

internal data class StandaloneUseResult(
    val ok: Boolean,
    val message: String,
)

/** Executes the on-device shell `use` workflow against the persisted standalone config. */
internal class StandaloneWifiUseController(context: Context) {
    private val appContext = context.applicationContext
    private val configStore = StandaloneConfigStore(appContext)
    private val useStore = StandaloneUseStateStore(appContext)

    fun liveWifiNames(): List<String> = StandaloneWifiUsePolicy.liveWifiNames(configStore.load())

    fun statusText(): String {
        val config = configStore.load()
        val state = useStore.load()
        val standalone = if (config.enabled) "enabled" else "disabled"
        return if (state == null) {
            "use=none standalone=$standalone"
        } else {
            "use=${state.wifiName} standalone=$standalone restore=${if (state.previousStandaloneEnabled) "enabled" else "disabled"}"
        }
    }

    fun use(name: String): StandaloneUseResult {
        val config = configStore.load()
        val group = StandaloneWifiUsePolicy.selectLiveWifi(config, name)
            ?: return StandaloneUseResult(false, "use $name failed: standalone festa live wifi $name not found")
        val essid = resolveEssid(group)
        if (essid.isBlank()) {
            return StandaloneUseResult(false, "use $name failed: wifi requires essid or scan-visible bssid")
        }

        val useState = StandaloneUseStatePolicy.beginUse(
            wifiName = name,
            currentStandaloneEnabled = config.enabled,
            active = useStore.load(),
        )
        useStore.save(useState)
        if (config.enabled) {
            configStore.save(config.toBuilder().setEnabled(false).build())
        }
        AgentService.requestStandaloneRefresh(appContext)

        val result = CommandExecutor(appContext, terminalLogger()).execute(StandaloneWifiUsePolicy.connectCommand(group, essid))
        return if (result.status == CommandResult.Status.STATUS_OK) {
            StandaloneUseResult(true, "use $name connected ssid=$essid")
        } else {
            StandaloneUseResult(false, "use $name failed: ${result.message.ifBlank { result.status.name }}")
        }
    }

    fun clearUse(): StandaloneUseResult {
        val state = useStore.clear()
            ?: return StandaloneUseResult(true, "no use override active; ${statusText()}")
        val config = configStore.load().toBuilder()
            .setEnabled(state.previousStandaloneEnabled)
            .build()
        configStore.save(config)
        AgentService.requestStandaloneRefresh(appContext)
        val restored = if (state.previousStandaloneEnabled) "enabled" else "disabled"
        return StandaloneUseResult(true, "cleared use ${state.wifiName}; standalone restored $restored")
    }

    @Suppress("DEPRECATION")
    @SuppressLint("MissingPermission")
    private fun resolveEssid(group: StandaloneWifiGroup): String {
        if (group.essid.isNotBlank()) return group.essid
        if (group.bssid.isBlank()) return ""
        val wifi = appContext.getSystemService(WifiManager::class.java)
        return runCatching {
            wifi.scanResults.orEmpty()
                .firstOrNull { it.BSSID.equals(group.bssid, ignoreCase = true) }
                ?.SSID
                .orEmpty()
        }.getOrDefault("")
    }

    private fun terminalLogger(): CommandLogger {
        return object : CommandLogger {
            override fun log(level: CommandLog.Level, message: String, scope: CommandLogScope) {
                TerminalLog.log(appContext, terminalLevelName(level), CommandTerminalLog.agentShell(scope, message))
            }
        }
    }

    private fun terminalLevelName(level: CommandLog.Level): String = when (level) {
        CommandLog.Level.LEVEL_DEBUG -> "DEBUG"
        CommandLog.Level.LEVEL_INFO -> "INFO"
        CommandLog.Level.LEVEL_WARN -> "WARN"
        CommandLog.Level.LEVEL_ERROR -> "ERROR"
        else -> "INFO"
    }
}
