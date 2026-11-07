package io.dropcheck.agent

import android.content.Context
import android.content.ContextWrapper
import android.os.Build
import io.dropcheck.agent.grpc.CommandLog
import io.dropcheck.agent.grpc.CommandResult
import io.dropcheck.agent.grpc.ConnectWifi
import io.dropcheck.agent.grpc.CycleWifi
import io.dropcheck.agent.grpc.RunCommand
import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

/** Real executor/connector with a tripwire Context; no device acquisition is permitted. */
class CommandExecutorPreflightTest {
    private class NoFrameworkContext : ContextWrapper(null) {
        var accesses = 0
            private set

        override fun getApplicationContext(): Context {
            accesses++
            throw IllegalStateException("unexpected Framework access")
        }

        override fun getSystemService(name: String): Any? {
            accesses++
            throw IllegalStateException("unexpected Framework access")
        }
    }

    private val logger = object : CommandLogger {
        override fun log(level: CommandLog.Level, message: String, scope: CommandLogScope) = Unit
    }
    private val valid = ConnectWifi.newBuilder().setSsid("Lab").setPassphrase("synthetic-passphrase")
        .setSecurity(ConnectWifi.Security.SECURITY_WPA2_PSK).build()

    private fun invalidCommands(): List<Pair<ConnectWifi, String>> {
        // Local mockable Android has SDK_INT < 33; this is the actual API guard, not an injected SDK.
        assertTrue(Build.VERSION.SDK_INT < 33)
        return listOf(
            valid.toBuilder().setMacRandomization(ConnectWifi.MacRandomization.MAC_RANDOMIZATION_NONE).build() to "wifi MAC randomization option requires Android 13/API 33+",
            valid.toBuilder().setSsid("").build() to "invalid wifi ssid",
            valid.toBuilder().setSsid("x".repeat(33)).build() to "invalid wifi ssid",
            valid.toBuilder().setSsid("Lab\n").build() to "invalid wifi ssid",
            valid.toBuilder().setPassphrase("").build() to "invalid passphrase length or encoding",
            valid.toBuilder().setPassphrase("short").build() to "invalid passphrase length or encoding",
            valid.toBuilder().setPassphrase("z".repeat(64)).build() to "invalid passphrase length or encoding",
            valid.toBuilder().setBssid("bad").build() to "invalid BSSID",
            valid.toBuilder().setSecurityValue(999).build() to "unsupported wifi security",
            valid.toBuilder().setBandValue(999).build() to "unsupported wifi band",
            valid.toBuilder().setMacRandomizationValue(999).build() to "unsupported wifi MAC randomization",
            valid.toBuilder().setTimeoutMs(-1).build() to "wifi timeout exceeds Android signed int range",
        )
    }

    @Test fun connectorRejectsBeforeLookingUpWifiManager() {
        val context = NoFrameworkContext()
        val connector = WifiConnector(context, logger)
        for ((command, expected) in invalidCommands()) {
            assertEquals(expected, connector.connect(command).error)
            assertEquals(0, context.accesses)
        }
    }

    @Test fun executorRejectsConnectAndBothCycleCleanupModesBeforeFrameworkAccess() {
        val context = NoFrameworkContext()
        val executor = CommandExecutor(context, logger)
        for ((command, expected) in invalidCommands()) {
            val requests = listOf(RunCommand.newBuilder().setConnectWifi(command).build()) + listOf(false, true).map { forget ->
                RunCommand.newBuilder().setCycleWifi(CycleWifi.newBuilder().setConnect(command).setCount(1).setForgetAfterEach(forget)).build()
            }
            for (request in requests) {
                val result = executor.execute(request)
                assertEquals(CommandResult.Status.STATUS_FAILED, result.status)
                assertEquals(expected, result.message)
                assertEquals(0, result.wifiCycle.stepsCount)
                assertEquals(0, context.accesses)
            }
        }
    }

    @Test fun validConnectReachesTheFrameworkTripwire() {
        val context = NoFrameworkContext()
        val failure = assertThrows(IllegalStateException::class.java) { WifiConnector(context, logger).connect(valid) }
        assertEquals("unexpected Framework access", failure.message)
        assertEquals(1, context.accesses)
    }
}
