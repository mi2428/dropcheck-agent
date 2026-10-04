package io.dropcheck.agent

import android.content.Context
import io.dropcheck.agent.grpc.ConnectWifi
import io.dropcheck.agent.grpc.RunCommand

internal data class AgentShellUseDefaults(
    val defaultPassphrase: String = "",
)

internal enum class AgentShellUsePassphraseSource {
    DEFAULT,
    EXPLICIT,
}

internal data class AgentShellUseRequest(
    val ssid: String,
    val passphrase: String,
    val passphraseSource: AgentShellUsePassphraseSource,
)

internal data class AgentShellUseDecision(
    val request: AgentShellUseRequest? = null,
    val error: String = "",
)

internal object AgentShellUsePolicy {
    fun validPassphrase(value: String): Boolean = !value.any { Character.isISOControl(it) } &&
        (value.toByteArray(Charsets.UTF_8).size in 8..63 ||
            (value.length == 64 && value.all { it in '0'..'9' || it.lowercaseChar() in 'a'..'f' }))

    fun statusText(defaults: AgentShellUseDefaults): String {
        val defaultPassphrase = if (defaults.defaultPassphrase.isNotEmpty()) "present" else "unset"
        return "default_passphrase=$defaultPassphrase"
    }

    fun setDefaultPassphraseMessage(passphrase: String): String {
        return if (passphrase.isEmpty()) {
            "default passphrase cleared"
        } else {
            "default passphrase updated"
        }
    }

    fun resolveUseRequest(
        ssid: String,
        explicitPassphrase: String?,
        defaults: AgentShellUseDefaults,
    ): AgentShellUseDecision {
        if (ssid.isBlank()) return AgentShellUseDecision(error = "wifi ssid is required")
        if (explicitPassphrase != null) {
            if (explicitPassphrase.isEmpty()) {
                return AgentShellUseDecision(error = "use passphrase cannot be empty")
            }
            if (!validPassphrase(explicitPassphrase)) return AgentShellUseDecision(error = "invalid passphrase length or encoding")
            return AgentShellUseDecision(
                request = AgentShellUseRequest(
                    ssid = ssid,
                    passphrase = explicitPassphrase,
                    passphraseSource = AgentShellUsePassphraseSource.EXPLICIT,
                ),
            )
        }
        if (defaults.defaultPassphrase.isEmpty()) {
            return AgentShellUseDecision(error = "use requires a passphrase; set default passphrase or pass one explicitly")
        }
        if (!validPassphrase(defaults.defaultPassphrase)) return AgentShellUseDecision(error = "invalid default passphrase length or encoding")
        return AgentShellUseDecision(
            request = AgentShellUseRequest(
                ssid = ssid,
                passphrase = defaults.defaultPassphrase,
                passphraseSource = AgentShellUsePassphraseSource.DEFAULT,
            ),
        )
    }

    fun connectCommand(request: AgentShellUseRequest): RunCommand {
        return RunCommand.newBuilder()
            .setLabel("use ${request.ssid}")
            .setConnectWifi(ConnectWifi.newBuilder()
                .setSsid(request.ssid)
                .setPassphrase(request.passphrase)
                .setTimeoutMs(WifiCommandPolicy.DEFAULT_CONNECT_TIMEOUT_MS))
            .build()
    }

}

internal class AgentShellUseDefaultsStore(context: Context) {
    private val prefs = context.applicationContext.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)

    fun load(): AgentShellUseDefaults {
        return AgentShellUseDefaults(defaultPassphrase = prefs.getString(KEY_DEFAULT_PASSPHRASE, "").orEmpty())
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
        const val PREFS_NAME = "agent-shell-use-defaults"
        const val KEY_DEFAULT_PASSPHRASE = "default_passphrase"
    }
}
