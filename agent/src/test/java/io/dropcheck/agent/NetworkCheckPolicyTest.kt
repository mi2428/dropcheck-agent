package io.dropcheck.agent

import io.dropcheck.agent.grpc.DnsRecordType
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class NetworkCheckPolicyTest {
    @Test
    fun appliesPingDefaultsAndBuildsProcessArguments() {
        val count = NetworkCheckPolicy.pingCount(0)

        assertEquals(3, count)
        assertEquals(9000, NetworkCheckPolicy.pingTimeoutMs(0, count))
        assertEquals(1234, NetworkCheckPolicy.pingTimeoutMs(1234, count))
        assertEquals("/system/bin/ping", NetworkCheckPolicy.pingBinary("example.com"))
        assertEquals("/system/bin/ping6", NetworkCheckPolicy.pingBinary("2001:db8::1"))
        assertEquals(
            listOf("/system/bin/ping", "-I", "wlan0", "-s", "128", "-c", "3", "example.com"),
            NetworkCheckPolicy.pingArgs("/system/bin/ping", "wlan0", 128, 3, "example.com"),
        )
        assertEquals(
            listOf("/system/bin/ping", "-c", "1", "example.com"),
            NetworkCheckPolicy.pingArgs("/system/bin/ping", "", 0, 1, "example.com"),
        )
    }

    @Test
    fun classifiesProcessAndProbeResults() {
        assertTrue(NetworkCheckPolicy.processSucceeded(finished = true, exitCode = 0))
        assertFalse(NetworkCheckPolicy.processSucceeded(finished = false, exitCode = 0))
        assertFalse(NetworkCheckPolicy.processSucceeded(finished = true, exitCode = 1))

        assertTrue(NetworkCheckPolicy.downloadSucceeded(error = "", status = 204))
        assertFalse(NetworkCheckPolicy.downloadSucceeded(error = "timeout", status = 204))
        assertFalse(NetworkCheckPolicy.downloadSucceeded(error = "", status = 404))

        assertTrue(NetworkCheckPolicy.httpSucceeded(matched = true, error = ""))
        assertFalse(NetworkCheckPolicy.httpSucceeded(matched = false, error = ""))
        assertFalse(NetworkCheckPolicy.httpSucceeded(matched = true, error = "boom"))
    }

    @Test
    fun appliesTracerouteHttpAndDnsDefaults() {
        assertEquals(30, NetworkCheckPolicy.tracerouteMaxHops(0))
        assertEquals(255, NetworkCheckPolicy.tracerouteMaxHops(999))
        assertEquals(12, NetworkCheckPolicy.tracerouteMaxHops(12))
        assertEquals(200, NetworkCheckPolicy.httpExpectedStatus(0))
        assertEquals(204, NetworkCheckPolicy.httpExpectedStatus(204))
        assertEquals(
            listOf(DnsRecordType.DNS_RECORD_TYPE_A, DnsRecordType.DNS_RECORD_TYPE_AAAA),
            NetworkCheckPolicy.dnsRecordTypes(emptyList()),
        )
        assertEquals(
            listOf(DnsRecordType.DNS_RECORD_TYPE_AAAA),
            NetworkCheckPolicy.dnsRecordTypes(listOf(DnsRecordType.DNS_RECORD_TYPE_AAAA)),
        )
    }

    @Test
    fun buildsTraceroutePingFallbackArguments() {
        assertEquals(
            listOf("/system/bin/ping", "-I", "wlan0", "-t", "3", "-s", "80", "-c", "1", "-W", "2", "8.8.8.8"),
            NetworkCheckPolicy.traceroutePingArgs(
                binary = "/system/bin/ping",
                interfaceName = "wlan0",
                sizeBytes = 80,
                ttl = 3,
                waitSeconds = 2,
                host = "8.8.8.8",
            ),
        )
        assertEquals(
            listOf("/system/bin/ping", "-t", "1", "-c", "1", "-W", "1", "8.8.8.8"),
            NetworkCheckPolicy.traceroutePingArgs(
                binary = "/system/bin/ping",
                interfaceName = "",
                sizeBytes = 0,
                ttl = 0,
                waitSeconds = 0,
                host = "8.8.8.8",
            ),
        )
    }

    @Test
    fun appliesPathMtuDefaultsAndBuildsProbeArguments() {
        assertEquals(576, NetworkCheckPolicy.pathMtuMinBytes(0, ipv6 = false))
        assertEquals(1280, NetworkCheckPolicy.pathMtuMinBytes(0, ipv6 = true))
        assertEquals(1200, NetworkCheckPolicy.pathMtuMinBytes(1200, ipv6 = false))
        assertEquals(1400, NetworkCheckPolicy.pathMtuMaxBytes(0, interfaceMtu = 1400, minMtu = 576))
        assertEquals(1500, NetworkCheckPolicy.pathMtuMaxBytes(0, interfaceMtu = 0, minMtu = 576))
        assertEquals(1280, NetworkCheckPolicy.pathMtuMaxBytes(1200, interfaceMtu = 0, minMtu = 1280))
        assertEquals(NetworkCheckPolicy.IPV4_PING_OVERHEAD_BYTES, NetworkCheckPolicy.pathMtuOverheadBytes("8.8.8.8"))
        assertEquals(NetworkCheckPolicy.IPV6_PING_OVERHEAD_BYTES, NetworkCheckPolicy.pathMtuOverheadBytes("2001:db8::1"))
        assertEquals(1472, NetworkCheckPolicy.pathMtuPayloadBytes(1500, NetworkCheckPolicy.IPV4_PING_OVERHEAD_BYTES))
        assertEquals(
            listOf("/system/bin/ping", "-I", "wlan0", "-M", "do", "-s", "1472", "-c", "1", "-W", "2", "example.com"),
            NetworkCheckPolicy.pathMtuPingArgs(
                binary = "/system/bin/ping",
                interfaceName = "wlan0",
                payloadSizeBytes = 1472,
                waitSeconds = 2,
                host = "example.com",
            ),
        )
    }
}
