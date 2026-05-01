package io.dropcheck.agent

import io.dropcheck.agent.grpc.ConnectWifi
import io.dropcheck.agent.grpc.DiagnosticCheck
import io.dropcheck.agent.grpc.WifiAssertResult
import io.dropcheck.agent.grpc.WifiBand
import io.dropcheck.agent.grpc.WifiStatus

/**
 * Controller-requested Wi-Fi condition expressed as local observable checks.
 *
 * Blank values mean "do not check this field"; boolean flags add required IP or
 * internet-validation checks on top of the base connected check.
 */
internal data class WifiExpectation(
    val ssid: String = "",
    val bssid: String = "",
    val security: ConnectWifi.Security = ConnectWifi.Security.SECURITY_UNSPECIFIED,
    val band: WifiBand = WifiBand.WIFI_BAND_ALL,
    val requireIp: Boolean = false,
    val requireValidated: Boolean = false,
)

/**
 * Converts a [WifiStatus] snapshot into a detailed pass/fail assertion result.
 *
 * The evaluator is pure by design; connection polling and timeout behavior stay
 * in the command executor so this logic can be covered by plain JVM tests.
 */
internal object WifiExpectationEvaluator {
    /**
     * Evaluates every requested check and keeps both actual and expected values.
     *
     * The controller uses this detail to explain failures instead of treating a
     * false assertion as a generic timeout.
     */
    fun evaluate(status: WifiStatus, expectation: WifiExpectation, elapsedMs: Long): WifiAssertResult {
        val connection = if (status.hasConnection()) status.connection else null
        val ip = if (status.hasIpStatus()) status.ipStatus else null
        val checks = mutableListOf<DiagnosticCheck>()
        val connected = connection != null && connection.ssid.isNotBlank()
        checks += diagnosticCheck("connected", "true", connected.toString(), connected)
        if (expectation.ssid.isNotBlank()) {
            val actual = connection?.ssid.orEmpty()
            checks += diagnosticCheck("ssid", expectation.ssid, actual, actual == expectation.ssid)
        }
        if (expectation.bssid.isNotBlank()) {
            val actual = connection?.bssid.orEmpty()
            checks += diagnosticCheck("bssid", expectation.bssid, actual, actual.equals(expectation.bssid, ignoreCase = true))
        }
        if (expectation.security != ConnectWifi.Security.SECURITY_UNSPECIFIED) {
            val actual = connection?.securityType.orEmpty()
            val expected = expectedSecurityTypes(expectation.security)
            checks += diagnosticCheck("security", expected.joinToString("|"), actual, actual in expected)
        }
        if (expectation.band != WifiBand.WIFI_BAND_UNSPECIFIED && expectation.band != WifiBand.WIFI_BAND_ALL) {
            val actualFrequency = connection?.frequencyMhz ?: 0
            checks += diagnosticCheck(
                "band",
                wifiBandName(expectation.band),
                bandNameForFrequency(actualFrequency),
                frequencyMatchesWifiBand(actualFrequency, expectation.band),
            )
        }
        if (expectation.requireIp) {
            val hasIp = ip?.addressesList?.isNotEmpty() == true || connection?.ipv4Address?.isNotBlank() == true
            val actual = if (ip != null) ip.addressesList.joinToString(",") else connection?.ipv4Address.orEmpty()
            checks += diagnosticCheck("ip", "present", actual.ifBlank { "absent" }, hasIp)
        }
        if (expectation.requireValidated) {
            val validated = ip?.validated == true
            checks += diagnosticCheck("validated", "true", validated.toString(), validated)
        }
        return WifiAssertResult.newBuilder()
            .setPassed(checks.all { it.passed })
            .addAllChecks(checks)
            .setStatus(status)
            .setElapsedMs(elapsedMs)
            .build()
    }

    /** Maps requested security modes to the normalized strings emitted by [securityTypeName]. */
    fun expectedSecurityTypes(security: ConnectWifi.Security): List<String> = when (security) {
        ConnectWifi.Security.SECURITY_WPA2_PSK -> listOf("psk")
        ConnectWifi.Security.SECURITY_WPA3_SAE -> listOf("sae")
        ConnectWifi.Security.SECURITY_WPA2_WPA3_TRANSITION -> listOf("psk", "sae")
        ConnectWifi.Security.SECURITY_UNSPECIFIED,
        ConnectWifi.Security.UNRECOGNIZED -> emptyList()
    }

    private fun diagnosticCheck(key: String, expected: String, actual: String, passed: Boolean): DiagnosticCheck {
        return DiagnosticCheck.newBuilder()
            .setKey(key)
            .setExpected(expected)
            .setActual(actual)
            .setPassed(passed)
            .setMessage(if (passed) "ok" else "mismatch")
            .build()
    }
}
