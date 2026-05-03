package io.dropcheck.agent

import io.dropcheck.agent.grpc.AssertWifi
import io.dropcheck.agent.grpc.ConnectWifi
import io.dropcheck.agent.grpc.CycleWifi
import io.dropcheck.agent.grpc.WaitWifiConnected
import io.dropcheck.agent.grpc.WifiCycleResult

/**
 * Pure policy for Wi-Fi command defaults and result classification.
 *
 * Keeping these decisions out of Android framework wrappers lets unit tests
 * protect the agent/controller contract without needing a device or Robolectric.
 */
internal object WifiCommandPolicy {
    /** Scan broadcasts are normally prompt, but Android may throttle or delay them on test devices. */
    const val DEFAULT_FRESH_SCAN_TIMEOUT_MS = 10_000

    /** Connection waits need to include association, DHCP, and framework propagation time. */
    const val DEFAULT_CONNECT_TIMEOUT_MS = 45_000

    /** Reconnect is shorter than first connect because credentials already exist. */
    const val DEFAULT_RECONNECT_TIMEOUT_MS = 30_000

    /** Default wait/assert values mirror controller command semantics. */
    const val DEFAULT_WAIT_TIMEOUT_MS = 30_000
    const val DEFAULT_ASSERT_TIMEOUT_MS = 0

    /** Monitor is a diagnostic event window, not a long-running stream. */
    const val DEFAULT_MONITOR_DURATION_MS = 10_000
    const val DEFAULT_MONITOR_INTERVAL_MS = 1_000

    /** Hard caps prevent one controller command from monopolizing the single command worker. */
    const val MAX_CYCLE_COUNT = 100
    const val MAX_CYCLE_PAUSE_MS = 60_000

    /** Applies proto3-style zero-as-unspecified timeout defaults. */
    fun effectiveTimeoutMs(value: Int, fallback: Int): Int = if (value > 0) value else fallback

    /**
     * Fresh scan may still return cached results; only start/broadcast failures
     * mark the command failed at this layer.
     */
    fun freshScanCompleted(errors: List<String>): Boolean {
        return errors.none { it.startsWith("start_scan=false") || it.startsWith("scan_broadcast_timeout") }
    }

    /** A scan detail command succeeds only when at least one filtered scan result remains. */
    fun scanDetailMatched(resultCount: Int, errors: List<String>): Boolean {
        return resultCount > 0 && errors.isEmpty()
    }

    /**
     * Connect requests always require IP acquisition because later probe
     * commands need a selectable Android Network.
     */
    fun connectExpectation(command: ConnectWifi): WifiExpectation {
        return WifiExpectation(
            ssid = command.ssid,
            bssid = command.bssid,
            security = command.security,
            band = command.band,
            requireIp = true,
            requireValidated = false,
        )
    }

    /** Converts controller wait parameters into the pure Wi-Fi assertion shape. */
    fun waitExpectation(command: WaitWifiConnected): WifiExpectation {
        return WifiExpectation(
            ssid = command.ssid,
            bssid = command.bssid,
            security = command.security,
            band = command.band,
            requireIp = command.requireIp,
            requireValidated = command.requireValidated,
        )
    }

    /** Converts immediate assertion parameters into the pure Wi-Fi assertion shape. */
    fun assertExpectation(command: AssertWifi): WifiExpectation {
        return WifiExpectation(
            ssid = command.ssid,
            bssid = command.bssid,
            security = command.security,
            band = command.band,
            requireIp = command.requireIp,
            requireValidated = command.requireValidated,
        )
    }

    /** Applies a safe default and upper bound to cycle count. */
    fun cycleCount(command: CycleWifi): Int {
        return if (command.count <= 0) 1 else command.count.coerceAtMost(MAX_CYCLE_COUNT)
    }

    /** Clamps inter-cycle pause so a malformed command cannot sleep for an arbitrary duration. */
    fun cyclePauseMs(command: CycleWifi): Int = command.pauseMs.coerceIn(0, MAX_CYCLE_PAUSE_MS)

    /** A cycle step must connect and all requested probes must pass. */
    fun cycleStepPassed(
        connected: Boolean,
        pingRequested: Boolean,
        pingOk: Boolean,
        httpRequested: Boolean,
        httpOk: Boolean,
    ): Boolean {
        return connected && (!pingRequested || pingOk) && (!httpRequested || httpOk)
    }

    /** The whole cycle passes only if every requested step completed and passed. */
    fun cyclePassed(result: WifiCycleResult): Boolean {
        return result.completedCount == result.requestedCount && result.passedCount == result.requestedCount
    }
}
