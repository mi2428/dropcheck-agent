package io.dropcheck.agent

import android.annotation.SuppressLint
import android.content.Context
import android.net.wifi.WifiManager
import io.dropcheck.agent.grpc.CommandLog
import io.dropcheck.agent.grpc.CommandResult
import io.dropcheck.agent.grpc.ConnectWifi
import io.dropcheck.agent.grpc.RunCommand
import io.dropcheck.agent.grpc.StandaloneWifiGroup

/** Pure helpers for synthetic `use` Wi-Fi targets built from shell defaults. */
internal object StandaloneWifiUsePolicy {
    const val DEFAULT_USE_TIMEOUT_MS = 35_000

    /*
     * DEAD CODE (2026-06-10): `use NAME` no longer resolves `standalone festa live`.
     * Keep the old live-festa helpers commented so we can restore them quickly if field
     * workflow changes back from SSID-only `use`.
     *
     * const val LIVE_FESTA_NAME = "live"
     *
     * fun liveWifiNames(config: StandaloneConfig): List<String> {
     *     return liveFesta(config)?.wifiGroupsList
     *         .orEmpty()
     *         .mapNotNull { it.name.takeIf(String::isNotBlank) }
     * }
     *
     * fun liveWifiListLines(config: StandaloneConfig): List<String> {
     *     return liveFesta(config)?.wifiGroupsList
     *         .orEmpty()
     *         .mapNotNull { group ->
     *             val name = group.name.takeIf(String::isNotBlank) ?: return@mapNotNull null
     *             "$name ${wifiMatchText(group)}"
     *         }
     * }
     *
     * fun liveWifiBannerText(config: StandaloneConfig, defaults: StandaloneUseDefaults): String? {
     *     val names = liveWifiNames(config)
     *     if (names.isEmpty()) return null
     *     return if (defaults.defaultPassphrase.isNotEmpty()) {
     *         "live_targets=${names.joinToString(" ")} inactive=direct-ssid"
     *     } else {
     *         "live_wifi=${names.joinToString(" ")}"
     *     }
     * }
     *
     * fun liveWifiStatusLines(config: StandaloneConfig, defaults: StandaloneUseDefaults): List<String> {
     *     val targets = liveWifiListLines(config)
     *     if (targets.isEmpty()) return listOf("no standalone festa live wifi entries")
     *     return if (defaults.defaultPassphrase.isNotEmpty()) {
     *         listOf("standalone festa live targets inactive while default_passphrase=present") + targets
     *     } else {
     *         targets
     *     }
     * }
     *
     * fun selectLiveWifi(config: StandaloneConfig, name: String): StandaloneWifiGroup? {
     *     return liveFesta(config)?.wifiGroupsList?.firstOrNull { it.name.equals(name, ignoreCase = true) }
     * }
     *
     * fun selectUseWifi(config: StandaloneConfig, name: String, defaults: StandaloneUseDefaults): StandaloneWifiGroup? {
     *     if (defaults.defaultPassphrase.isNotEmpty()) {
     *         return StandaloneWifiGroup.newBuilder()
     *             .setName(name)
     *             .setEssid(name)
     *             .setPassphrase(defaults.defaultPassphrase)
     *             .build()
     *     }
     *     return selectLiveWifi(config, name)
     * }
     *
     * private fun wifiMatchText(group: StandaloneWifiGroup): String {
     *     return when {
     *         group.essid.isNotBlank() -> "match essid ${quote(group.essid)}"
     *         group.bssid.isNotBlank() -> "match bssid ${group.bssid}"
     *         else -> "match <unset>"
     *     }
     * }
     *
     * private fun quote(value: String): String {
     *     return buildString {
     *         append('"')
     *         for (ch in value) {
     *             when (ch) {
     *                 '\\' -> append("\\\\")
     *                 '"' -> append("\\\"")
     *                 else -> append(ch)
     *             }
     *         }
     *         append('"')
     *     }
     * }
     *
     * private fun liveFesta(config: StandaloneConfig) = config.festasList.firstOrNull { it.name == LIVE_FESTA_NAME }
     */

    fun selectUseWifi(name: String, defaults: StandaloneUseDefaults): StandaloneWifiGroup {
        return StandaloneWifiGroup.newBuilder()
            .setName(name)
            .setEssid(name)
            .setPassphrase(defaults.defaultPassphrase)
            .build()
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
}

internal data class StandaloneUseState(
    val wifiName: String,
    val previousStandaloneEnabled: Boolean,
)

internal data class StandaloneUseDefaults(
    val defaultPassphrase: String = "",
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

/** Persists shell-only defaults used by direct `use <ssid>` connections. */
internal class StandaloneUseDefaultsStore(context: Context) {
    private val prefs = context.applicationContext.getSharedPreferences("standalone-use-defaults", Context.MODE_PRIVATE)

    fun load(): StandaloneUseDefaults {
        return StandaloneUseDefaults(
            defaultPassphrase = prefs.getString(KEY_DEFAULT_PASSPHRASE, "").orEmpty(),
        )
    }

    fun setDefaultPassphrase(passphrase: String) {
        prefs.edit().apply {
            if (passphrase.isEmpty()) {
                remove(KEY_DEFAULT_PASSPHRASE)
            } else {
                putString(KEY_DEFAULT_PASSPHRASE, passphrase)
            }
        }.apply()
    }

    private companion object {
        const val KEY_DEFAULT_PASSPHRASE = "default_passphrase"
    }
}

internal data class StandaloneUseResult(
    val ok: Boolean,
    val message: String,
)

/** Executes the on-device shell `use` workflow against the persisted standalone config and shell defaults. */
internal class StandaloneWifiUseController(context: Context) {
    private val appContext = context.applicationContext
    private val configStore = StandaloneConfigStore(appContext)
    private val useStore = StandaloneUseStateStore(appContext)
    private val defaultsStore = StandaloneUseDefaultsStore(appContext)

    /*
     * DEAD CODE (2026-06-10): live-festa wrapper methods disabled with SSID-only `use`.
     *
     * fun liveWifiNames(): List<String> = StandaloneWifiUsePolicy.liveWifiNames(configStore.load())
     *
     * fun liveWifiBannerText(): String? {
     *     val config = configStore.load()
     *     val defaults = defaultsStore.load()
     *     return StandaloneWifiUsePolicy.liveWifiBannerText(config, defaults)
     * }
     *
     * fun liveWifiListText(): List<String> {
     *     val config = configStore.load()
     *     val defaults = defaultsStore.load()
     *     return StandaloneWifiUsePolicy.liveWifiStatusLines(config, defaults)
     * }
     */

    fun statusText(): String {
        val config = configStore.load()
        val state = useStore.load()
        val defaults = defaultsStore.load()
        val standalone = if (config.enabled) "enabled" else "disabled"
        val defaultPassphrase = if (defaults.defaultPassphrase.isNotEmpty()) "present" else "unset"

        /*
         * DEAD CODE (2026-06-10): previous mode label when live-festa fallback was active.
         * val mode = if (defaults.defaultPassphrase.isNotEmpty()) "direct-ssid" else "live-festa"
         */
        return if (state == null) {
            "use=none standalone=$standalone mode=ssid default_passphrase=$defaultPassphrase"
        } else {
            "use=${state.wifiName} standalone=$standalone restore=${if (state.previousStandaloneEnabled) "enabled" else "disabled"} mode=ssid default_passphrase=$defaultPassphrase"
        }
    }

    fun setDefaultPassphrase(passphrase: String): StandaloneUseResult {
        defaultsStore.setDefaultPassphrase(passphrase)
        return if (passphrase.isEmpty()) {
            StandaloneUseResult(true, "default passphrase cleared")
        } else {
            StandaloneUseResult(true, "default passphrase updated")
        }
    }

    fun use(name: String): StandaloneUseResult {
        val config = configStore.load()
        val defaults = defaultsStore.load()

        /*
         * DEAD CODE (2026-06-10): previous live-festa lookup path.
         * val group = StandaloneWifiUsePolicy.selectUseWifi(config, name, defaults)
         *     ?: return StandaloneUseResult(false, "use $name failed: standalone festa live wifi $name not found")
         */
        val group = StandaloneWifiUsePolicy.selectUseWifi(name, defaults)
        val essid = resolveEssid(group)
        if (essid.isBlank()) {
            return StandaloneUseResult(false, "use $name failed: wifi requires essid or scan-visible bssid")
        }

        val useState = StandaloneUseStatePolicy.beginUse(
            wifiName = group.name,
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
