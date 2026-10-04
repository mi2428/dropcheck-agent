package io.dropcheck.agent

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
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
        assertEquals("invalid default passphrase length or encoding", AgentShellUsePolicy.resolveUseRequest(
            ssid = "hp1", explicitPassphrase = null, defaults = AgentShellUseDefaults(defaultPassphrase = "short"),
        ).error)
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
    fun passphraseBoundaryUsesUtf8BytesAndHexEncoding() {
        assertFalse(AgentShellUsePolicy.validPassphrase("x".repeat(7)))
        assertTrue(AgentShellUsePolicy.validPassphrase("x".repeat(8)))
        assertTrue(AgentShellUsePolicy.validPassphrase("x".repeat(63)))
        assertFalse(AgentShellUsePolicy.validPassphrase("z".repeat(64)))
        assertTrue(AgentShellUsePolicy.validPassphrase("a".repeat(64)))
        assertTrue(AgentShellUsePolicy.validPassphrase("あ".repeat(3))) // 9 UTF-8 bytes, 3 characters
        assertTrue(AgentShellUsePolicy.validPassphrase("あ".repeat(21))) // 63 bytes
        assertFalse(AgentShellUsePolicy.validPassphrase("あ".repeat(22))) // 66 bytes
        assertFalse(AgentShellUsePolicy.validPassphrase("abc\n12345"))
    }

    @Test fun usePreservesBlankOnlyLiteralWhileRejectingEmptySsid() {
        assertEquals(" ", AgentShellUsePolicy.resolveUseRequest(" ", "test-only", AgentShellUseDefaults()).request?.ssid)
        assertEquals("wifi ssid is required", AgentShellUsePolicy.resolveUseRequest("", "test-only", AgentShellUseDefaults()).error)
    }

    @Test fun explicitClearLeavesOnlyUnsetStatusAndFutureUseNeedsCredential() {
        val cleared = AgentShellUseDefaults()
        assertEquals("default passphrase cleared", AgentShellUsePolicy.setDefaultPassphraseMessage(""))
        assertEquals("default_passphrase=unset", AgentShellUsePolicy.statusText(cleared))
        assertEquals(null, AgentShellUsePolicy.resolveUseRequest("Example Lab", null, cleared).request)
        assertEquals(AgentShellUsePassphraseSource.EXPLICIT,
            AgentShellUsePolicy.resolveUseRequest("Example Lab", "test-only", cleared).request?.passphraseSource)
    }

}
