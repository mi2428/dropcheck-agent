package io.dropcheck.agent

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Test

class AgentShellUsePolicyTest {
    @Test
    fun resolvesUseRequestFromDefaultPassphrase() {
        val decision = AgentShellUsePolicy.resolveUseRequest(
            ssid = "hp1",
            explicitPassphrase = null,
            defaults = AgentShellUseDefaults(defaultPassphrase = "hogehoge"),
        )

        assertEquals("", decision.error)
        assertNotNull(decision.request)
        assertEquals("hp1", decision.request?.ssid)
        assertEquals("hogehoge", decision.request?.passphrase)
        assertEquals(AgentShellUsePassphraseSource.DEFAULT, decision.request?.passphraseSource)
    }

    @Test
    fun prefersExplicitPassphraseOverDefault() {
        val decision = AgentShellUsePolicy.resolveUseRequest(
            ssid = "hp1",
            explicitPassphrase = "fugafuga",
            defaults = AgentShellUseDefaults(defaultPassphrase = "hogehoge"),
        )

        assertEquals("", decision.error)
        assertEquals("fugafuga", decision.request?.passphrase)
        assertEquals(AgentShellUsePassphraseSource.EXPLICIT, decision.request?.passphraseSource)
    }

    @Test
    fun rejectsMissingOrEmptyPassphrase() {
        assertEquals(
            "use requires a passphrase; set default passphrase or pass one explicitly",
            AgentShellUsePolicy.resolveUseRequest(
                ssid = "hp1",
                explicitPassphrase = null,
                defaults = AgentShellUseDefaults(),
            ).error,
        )
        assertEquals(
            "use passphrase cannot be empty",
            AgentShellUsePolicy.resolveUseRequest(
                ssid = "hp1",
                explicitPassphrase = "",
                defaults = AgentShellUseDefaults(defaultPassphrase = "hogehoge"),
            ).error,
        )
    }

    @Test
    fun buildsConnectCommandForUseWorkflow() {
        val request = AgentShellUseRequest(
            ssid = "hp1",
            passphrase = "hogehoge",
            passphraseSource = AgentShellUsePassphraseSource.DEFAULT,
        )

        val command = AgentShellUsePolicy.connectCommand(request)
        val connect = command.connectWifi

        assertEquals("use hp1", command.label)
        assertEquals("hp1", connect.ssid)
        assertEquals("hogehoge", connect.passphrase)
        assertEquals(WifiCommandPolicy.DEFAULT_CONNECT_TIMEOUT_MS, connect.timeoutMs)
    }

    @Test
    fun formatsShellTokensForQuotedDisplay() {
        assertEquals("hp1", formatAgentShellToken("hp1"))
        assertEquals("\"hp 1\"", formatAgentShellToken("hp 1"))
        assertEquals("\"fuga\\\"fuga\"", formatAgentShellToken("fuga\"fuga"))
        assertEquals("\"path\\\\name\"", formatAgentShellToken("path\\name"))
    }
}
