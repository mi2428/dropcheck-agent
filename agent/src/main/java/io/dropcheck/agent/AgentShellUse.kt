package io.dropcheck.agent

import android.content.Context
import io.dropcheck.agent.grpc.CommandResult
import io.dropcheck.agent.grpc.ConnectWifi
import io.dropcheck.agent.grpc.ConnectWifiResult
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

    fun renderConnect(
        result: ConnectWifiResult,
        status: CommandResult.Status,
        message: String,
        source: AgentShellUsePassphraseSource,
    ): List<String> {
        val resolvedMessage = message.ifBlank { result.message.ifBlank { "-" } }
        return buildList {
            add("Wi-Fi Use")
            add(row("ssid", result.ssid))
            add(row("connected", result.connected.toString()))
            add(row("passphrase_source", source.name.lowercase()))
            add(row("message", resolvedMessage))
            if (result.hasIpStatus()) {
                add(row("interface", result.ipStatus.interfaceName.ifBlank { "unknown" }))
                add(row("validated", result.ipStatus.validated.toString()))
                add(row("internet", result.ipStatus.internet.toString()))
            }
        }
    }

    private fun row(key: String, value: String): String = "  ${key.padEnd(18)} $value"
}

internal fun formatAgentShellToken(value: String): String {
    if (value.isEmpty()) return "\"\""
    val safe = value.all { it.isLetterOrDigit() || it in ".:_/@%+-" }
    if (safe) return value
    return buildString {
        append('"')
        value.forEach { ch ->
            when (ch) {
                '\\' -> append("\\\\")
                '"' -> append("\\\"")
                else -> append(ch)
            }
        }
        append('"')
    }
}

internal class AgentShellUseDefaultsStore(context: Context) {
    private val prefs = context.applicationContext.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
    private val legacyPrefs = context.applicationContext.getSharedPreferences(LEGACY_PREFS_NAME, Context.MODE_PRIVATE)

    fun load(): AgentShellUseDefaults {
        val passphrase = when {
            prefs.contains(KEY_DEFAULT_PASSPHRASE) -> prefs.getString(KEY_DEFAULT_PASSPHRASE, "").orEmpty()
            legacyPrefs.contains(KEY_DEFAULT_PASSPHRASE) -> legacyPrefs.getString(KEY_DEFAULT_PASSPHRASE, "").orEmpty()
            else -> ""
        }
        return AgentShellUseDefaults(defaultPassphrase = passphrase)
    }

    fun setDefaultPassphrase(passphrase: String) {
        prefs.edit().apply {
            if (passphrase.isEmpty()) {
                remove(KEY_DEFAULT_PASSPHRASE)
            } else {
                putString(KEY_DEFAULT_PASSPHRASE, passphrase)
            }
        }.apply()
        legacyPrefs.edit().remove(KEY_DEFAULT_PASSPHRASE).apply()
    }

    private companion object {
        const val PREFS_NAME = "agent-shell-use-defaults"
        const val LEGACY_PREFS_NAME = "standalone-use-defaults"
        const val KEY_DEFAULT_PASSPHRASE = "default_passphrase"
    }
}
