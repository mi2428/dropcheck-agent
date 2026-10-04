package io.dropcheck.agent

import io.dropcheck.agent.grpc.CommandResult
import io.dropcheck.agent.grpc.RunCommand
import io.dropcheck.agent.grpc.WifiBand
import io.dropcheck.agent.grpc.WifiDiagnostics
import io.dropcheck.agent.grpc.WifiScan
import io.dropcheck.agent.grpc.WifiScanResult
import io.dropcheck.agent.grpc.WifiStatus
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test

class AgentWifiEhtCommandTest {
    @Test
    fun failedFreshScanKeepsReferenceDataAndFailsShellOutput() {
        val fresh = scanResult(CommandResult.Status.STATUS_FAILED)
        val commands = mutableListOf<RunCommand>()
        val result = runWifiEhtDiagnostics(AgentShellCommand.ShowWifiEht(fresh = true, timeoutMs = 9000)) {
            commands += it
            if (it.hasGetFreshWifiScan()) fresh else diagnosticsResult()
        }
        assertEquals(2, commands.size)
        assertEquals(WifiBand.WIFI_BAND_ALL, commands[0].getFreshWifiScan.band)
        assertEquals(9000, commands[0].getFreshWifiScan.timeoutMs)
        assertTrue(commands[1].hasGetWifiDiagnostics())
        assertEquals(CommandResult.Status.STATUS_FAILED, result.status)
        assertEquals("reference", result.wifiDiagnostics.scan.resultsList.single().ssid)
        assertEquals("cached (refresh failed)", scanSource(result))
        assertTrue(result.message.contains("fresh scan incomplete"))
        assertTrue(result.message.contains("refresh not confirmed"))
        assertEquals(123L, result.elapsedMs)
        assertEquals(0, fresh.wifiScan.fieldsCount)
        val out = AgentShellTextFormatter.formatStructuredResult(
            result,
            AgentWifiMloRenderer.render(
                result.wifiDiagnostics.status, result.wifiDiagnostics.scan,
                AgentWifiMloContext(scanSource = scanSource(result), sdkInt = 36),
            ),
        ).joinToString("\n")
        assertFalse(result.status == CommandResult.Status.STATUS_OK)
        assertTrue(out.startsWith("Status: failed"))
        assertTrue(out.contains("cached (refresh failed)"))
        assertFalse(out.contains("stale"))
    }

    @Test
    fun failedWithoutPayloadAndCanceledScanDoNotRunDiagnostics() {
        for (status in listOf(CommandResult.Status.STATUS_FAILED, CommandResult.Status.STATUS_CANCELED)) {
            for (payload in listOf(false, true)) {
                if (status == CommandResult.Status.STATUS_FAILED && payload) continue
                var count = 0
                val scan = if (payload) scanResult(status) else CommandResult.newBuilder()
                    .setStatus(status).setMessage("fresh scan incomplete").build()
                val result = runWifiEhtDiagnostics(AgentShellCommand.ShowWifiEht(fresh = true)) {
                    count++
                    assertTrue(it.hasGetFreshWifiScan())
                    scan
                }
                assertEquals(1, count)
                assertEquals(status, result.status)
                assertTrue(result.message.contains("fresh scan incomplete"))
                assertFalse(result.hasWifiDiagnostics())
            }
        }
    }

    @Test
    fun missingPayloadOnSuccessfulScanIsNotSuccessful() {
        var count = 0
        val result = runWifiEhtDiagnostics(AgentShellCommand.ShowWifiEht(fresh = true)) {
            count++
            CommandResult.newBuilder().setStatus(CommandResult.Status.STATUS_OK).build()
        }
        assertEquals(1, count)
        assertEquals(CommandResult.Status.STATUS_FAILED, result.status)
        assertTrue(result.message.contains("scan unavailable"))
    }

    @Test
    fun scanExecutionErrorPropagatesWithoutDiagnostics() {
        val failure = IllegalStateException("scan transport failure")
        var count = 0
        try {
            runWifiEhtDiagnostics(AgentShellCommand.ShowWifiEht(fresh = true)) {
                count++
                throw failure
            }
            fail("execution failure swallowed")
        } catch (actual: IllegalStateException) {
            assertTrue(actual === failure)
        }
        assertEquals(1, count)
    }

    @Test
    fun successfulFreshCompositeAndDiagnosticsFailureKeepTheirStatus() {
        for (status in listOf(CommandResult.Status.STATUS_OK, CommandResult.Status.STATUS_FAILED, CommandResult.Status.STATUS_CANCELED)) {
            val result = runWifiEhtDiagnostics(AgentShellCommand.ShowWifiEht(fresh = true)) {
                if (it.hasGetFreshWifiScan()) {
                    assertEquals(0, it.getFreshWifiScan.timeoutMs)
                    scanResult(CommandResult.Status.STATUS_OK)
                } else diagnosticsResult(status)
            }
            assertEquals(status, result.status)
            assertEquals("fresh", scanSource(result))
            assertEquals("reference", result.wifiDiagnostics.scan.resultsList.single().ssid)
        }
        val cached = runWifiEhtDiagnostics(AgentShellCommand.ShowWifiEht()) {
            assertTrue(it.hasGetWifiDiagnostics())
            diagnosticsResult()
        }
        assertEquals(CommandResult.Status.STATUS_OK, cached.status)
        assertEquals("stale", cached.wifiDiagnostics.scan.resultsList.single().ssid)
    }

    @Test
    fun failedScanIsNotLostWhenDiagnosticsAreUnavailableOrFailed() {
        for (diagnostics in listOf(
            diagnosticsResult(CommandResult.Status.STATUS_FAILED),
            diagnosticsResult(CommandResult.Status.STATUS_CANCELED).toBuilder().clearMessage().build(),
            CommandResult.newBuilder().setStatus(CommandResult.Status.STATUS_OK).build(),
            CommandResult.newBuilder().setStatus(CommandResult.Status.STATUS_OK).setWifiStatus(WifiStatus.getDefaultInstance()).build(),
            CommandResult.newBuilder().setStatus(CommandResult.Status.STATUS_OK).setWifiDiagnostics(WifiDiagnostics.getDefaultInstance()).build(),
        )) {
            val result = runWifiEhtDiagnostics(AgentShellCommand.ShowWifiEht(fresh = true)) {
                if (it.hasGetFreshWifiScan()) scanResult(CommandResult.Status.STATUS_FAILED) else diagnostics
            }
            assertEquals(
                if (diagnostics.status == CommandResult.Status.STATUS_CANCELED) CommandResult.Status.STATUS_CANCELED
                else CommandResult.Status.STATUS_FAILED,
                result.status,
            )
            assertTrue(result.message.contains("fresh scan incomplete"))
            assertTrue(result.message.contains("diagnostics"))
            if (diagnostics.status != CommandResult.Status.STATUS_OK) {
                assertTrue(result.message.contains("wifi eht diagnostics: ${diagnostics.status.name}"))
            }
        }
    }

    private fun scanResult(status: CommandResult.Status): CommandResult = CommandResult.newBuilder()
        .setStatus(status)
        .setMessage(if (status == CommandResult.Status.STATUS_OK) "" else "fresh scan incomplete")
        .setElapsedMs(123)
        .setWifiScan(WifiScan.newBuilder().addResults(
            WifiScanResult.newBuilder().setSsid("reference").setWifiStandard("802.11be"),
        ))
        .build()

    private fun diagnosticsResult(status: CommandResult.Status = CommandResult.Status.STATUS_OK): CommandResult =
        CommandResult.newBuilder().setStatus(status)
            .setMessage(if (status == CommandResult.Status.STATUS_OK) "" else "diagnostics failed")
            .setWifiDiagnostics(WifiDiagnostics.newBuilder()
                .setStatus(WifiStatus.newBuilder().setEnabled(true))
                .setScan(WifiScan.newBuilder().addResults(WifiScanResult.newBuilder().setSsid("stale"))))
            .build()

    private fun scanSource(result: CommandResult): String = result.wifiDiagnostics.scan.fieldsList
        .last { it.key == "scan_source" }.value
}
