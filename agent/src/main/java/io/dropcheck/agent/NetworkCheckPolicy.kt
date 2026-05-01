package io.dropcheck.agent

import io.dropcheck.agent.grpc.DnsRecordType

/**
 * Pure policy for local network probes.
 *
 * The executor owns Android Network binding and process/HTTP I/O; this object
 * owns defaults, argument construction, and pass/fail classification.
 */
internal object NetworkCheckPolicy {
    /** Three packets gives a small signal without making every failed ping slow. */
    private const val DEFAULT_PING_COUNT = 3
    private const val PING_PER_PACKET_TIMEOUT_MS = 2_000L
    private const val PING_BASE_TIMEOUT_MS = 3_000L

    /** Local process and HTTP probes are bounded so cancellation remains responsive. */
    const val DEFAULT_TRACEROUTE_TIMEOUT_MS = 60_000
    const val DEFAULT_TRACEROUTE_MAX_HOPS = 30
    const val MAX_TRACEROUTE_HOPS = 255
    const val DEFAULT_DOWNLOAD_TIMEOUT_MS = 60_000
    const val DEFAULT_HTTP_TIMEOUT_MS = 5_000
    const val DEFAULT_HTTP_STATUS = 200

    /** Applies proto3-style zero-as-unspecified timeout defaults. */
    fun effectiveTimeoutMs(value: Int, fallback: Int): Int = if (value > 0) value else fallback

    /** Applies the controller protocol's default ping packet count. */
    fun pingCount(value: Int): Int = if (value > 0) value else DEFAULT_PING_COUNT

    /** Computes total process timeout from packet count when the controller does not provide one. */
    fun pingTimeoutMs(requestedTimeoutMs: Int, count: Int): Int {
        return effectiveTimeoutMs(requestedTimeoutMs, defaultPingTimeoutMs(count))
    }

    /** Default ping timeout is linear in packet count with startup slack for slow devices. */
    fun defaultPingTimeoutMs(count: Int): Int {
        val boundedCount = count.coerceAtLeast(1)
        val millis = boundedCount.toLong() * PING_PER_PACKET_TIMEOUT_MS + PING_BASE_TIMEOUT_MS
        return millis.coerceAtMost(Int.MAX_VALUE.toLong()).toInt()
    }

    /** Android exposes separate ping binaries for IPv4 and IPv6. */
    fun pingBinary(host: String): String {
        return if (host.contains(":")) "/system/bin/ping6" else "/system/bin/ping"
    }

    /** Builds ping argv without shell interpolation, so host/iface values are not shell-expanded. */
    fun pingArgs(
        binary: String,
        interfaceName: String,
        sizeBytes: Int,
        count: Int,
        host: String,
    ): List<String> {
        return buildList {
            add(binary)
            if (interfaceName.isNotBlank()) {
                add("-I")
                add(interfaceName)
            }
            if (sizeBytes > 0) {
                add("-s")
                add(sizeBytes.toString())
            }
            add("-c")
            add(count.toString())
            add(host)
        }
    }

    /** Bounds traceroute hops to the protocol maximum accepted by common implementations. */
    fun tracerouteMaxHops(value: Int): Int {
        return if (value > 0) value.coerceAtMost(MAX_TRACEROUTE_HOPS) else DEFAULT_TRACEROUTE_MAX_HOPS
    }

    /** Builds one hop probe argv for a traceroute fallback implemented with ping TTL. */
    fun traceroutePingArgs(
        binary: String,
        interfaceName: String,
        sizeBytes: Int,
        ttl: Int,
        waitSeconds: Int,
        host: String,
    ): List<String> {
        return buildList {
            add(binary)
            if (interfaceName.isNotBlank()) {
                add("-I")
                add(interfaceName)
            }
            add("-t")
            add(ttl.coerceAtLeast(1).toString())
            if (sizeBytes > 0) {
                add("-s")
                add(sizeBytes.toString())
            }
            add("-c")
            add("1")
            add("-W")
            add(waitSeconds.coerceAtLeast(1).toString())
            add(host)
        }
    }

    /** HTTP check defaults to a normal 200 OK expectation. */
    fun httpExpectedStatus(value: Int): Int = if (value > 0) value else DEFAULT_HTTP_STATUS

    /** Empty qtype means "resolve both address families". */
    fun dnsRecordTypes(qtypes: List<DnsRecordType>): List<DnsRecordType> {
        return qtypes.ifEmpty {
            listOf(DnsRecordType.DNS_RECORD_TYPE_A, DnsRecordType.DNS_RECORD_TYPE_AAAA)
        }
    }

    /** Process-backed probes pass only when the process exits cleanly before timeout. */
    fun processSucceeded(finished: Boolean, exitCode: Int): Boolean = finished && exitCode == 0

    /** Download accepts any successful 2xx/3xx HTTP response with no local read error. */
    fun downloadSucceeded(error: String, status: Int): Boolean = error.isBlank() && status in 200..399

    /** HTTP check pass/fail is exact status match plus no connection error. */
    fun httpSucceeded(matched: Boolean, error: String): Boolean = matched && error.isBlank()
}
