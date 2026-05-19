package io.dropcheck.agent

import io.dropcheck.agent.grpc.DnsRecordType
import io.dropcheck.agent.grpc.IpFamily
import java.net.Inet4Address
import java.net.Inet6Address
import java.net.InetAddress

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
    private const val PING_PACKET_INTERVAL_SECONDS = "0.2"
    private const val PING_REPLY_WAIT_SECONDS = 1

    /** Local process and HTTP probes are bounded so cancellation remains responsive. */
    const val DEFAULT_TRACEROUTE_TIMEOUT_MS = 60_000
    const val DEFAULT_TRACEROUTE_MAX_HOPS = 30
    const val MAX_TRACEROUTE_HOPS = 255
    const val DEFAULT_DOWNLOAD_TIMEOUT_MS = 60_000
    const val DEFAULT_DNS_TIMEOUT_MS = 5_000
    const val DEFAULT_HTTP_TIMEOUT_MS = 5_000
    const val DEFAULT_HTTP_STATUS = 200
    const val DEFAULT_GLOBAL_IP_TIMEOUT_MS = 5_000
    const val DEFAULT_PATH_MTU_TIMEOUT_MS = 30_000
    const val DEFAULT_PATH_MTU_MAX_BYTES = 1500
    const val IPV4_PING_OVERHEAD_BYTES = 28
    const val IPV6_PING_OVERHEAD_BYTES = 48

    private const val DEFAULT_IPV4_PATH_MTU_MIN_BYTES = 576
    private const val DEFAULT_IPV6_PATH_MTU_MIN_BYTES = 1280

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

    /**
     * Picks the most specific ping binding target for the selected network.
     *
     * On Android, `ping -I <iface>` can still choose a stale source address after
     * Wi-Fi teardown/reconnect. Binding to the link address pins the probe to
     * the selected network's address family while preserving interface names in
     * the result payload.
     */
    fun pingBindTarget(interfaceName: String, addresses: List<String>, host: String): String {
        return sourceAddressForHost(addresses, host) ?: interfaceName
    }

    fun sourceAddressForHost(addresses: List<String>, host: String): String? {
        val ipv6 = host.contains(":")
        return addresses.firstNotNullOfOrNull { raw ->
            val candidate = raw.substringBefore('/').substringBefore('%').trim()
            val parsed = runCatching { InetAddress.getByName(candidate) }.getOrNull() ?: return@firstNotNullOfOrNull null
            val matches = if (ipv6) parsed is Inet6Address else parsed is Inet4Address
            if (matches && isUsableSourceAddress(parsed)) candidate else null
        }
    }

    private fun isUsableSourceAddress(address: InetAddress): Boolean {
        return !address.isAnyLocalAddress &&
            !address.isLoopbackAddress &&
            !address.isLinkLocalAddress &&
            !address.isMulticastAddress
    }

    /** Builds ping argv without shell interpolation, so host/bind target values are not shell-expanded. */
    fun pingArgs(
        binary: String,
        bindTarget: String,
        sizeBytes: Int,
        count: Int,
        host: String,
    ): List<String> {
        return buildList {
            add(binary)
            if (bindTarget.isNotBlank()) {
                add("-I")
                add(bindTarget)
            }
            if (sizeBytes > 0) {
                add("-s")
                add(sizeBytes.toString())
            }
            add("-i")
            add(PING_PACKET_INTERVAL_SECONDS)
            add("-W")
            add(PING_REPLY_WAIT_SECONDS.toString())
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
        bindTarget: String,
        sizeBytes: Int,
        ttl: Int,
        waitSeconds: Int,
        host: String,
    ): List<String> {
        return buildList {
            add(binary)
            if (bindTarget.isNotBlank()) {
                add("-I")
                add(bindTarget)
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

    /** Use protocol MTU floors so discovery never probes below a packet size the IP version must support. */
    fun pathMtuMinBytes(value: Int, ipv6: Boolean): Int {
        if (value > 0) return value
        return if (ipv6) DEFAULT_IPV6_PATH_MTU_MIN_BYTES else DEFAULT_IPV4_PATH_MTU_MIN_BYTES
    }

    /** Prefer Android's link MTU as the search ceiling, falling back to Ethernet MTU when unavailable. */
    fun pathMtuMaxBytes(value: Int, interfaceMtu: Int, minMtu: Int): Int {
        val fallback = if (interfaceMtu > 0) interfaceMtu else DEFAULT_PATH_MTU_MAX_BYTES
        val maxMtu = if (value > 0) value else fallback
        return maxMtu.coerceAtLeast(minMtu)
    }

    /** PMTU is an IP packet size; ping -s takes only ICMP payload, so header overhead is subtracted later. */
    fun pathMtuOverheadBytes(host: String): Int {
        return if (host.contains(":")) IPV6_PING_OVERHEAD_BYTES else IPV4_PING_OVERHEAD_BYTES
    }

    /** Convert target IP MTU to ping payload bytes: IPv4 overhead is 20+8, IPv6 overhead is 40+8. */
    fun pathMtuPayloadBytes(mtuBytes: Int, overheadBytes: Int): Int {
        return (mtuBytes - overheadBytes).coerceAtLeast(0)
    }

    /** Android ping accepts "-M do" to set DF/no-fragmentation; a failed probe means the MTU is too large. */
    fun pathMtuPingArgs(
        binary: String,
        bindTarget: String,
        payloadSizeBytes: Int,
        waitSeconds: Int,
        host: String,
    ): List<String> {
        return buildList {
            add(binary)
            if (bindTarget.isNotBlank()) {
                add("-I")
                add(bindTarget)
            }
            add("-M")
            add("do")
            add("-s")
            add(payloadSizeBytes.toString())
            add("-i")
            add(PING_PACKET_INTERVAL_SECONDS)
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

    /** Empty/unspecified global-IP family means query both IPv4 and IPv6. */
    fun globalIpFamilies(family: IpFamily): List<IpFamily> {
        return when (family) {
            IpFamily.IP_FAMILY_IPV4 -> listOf(IpFamily.IP_FAMILY_IPV4)
            IpFamily.IP_FAMILY_IPV6 -> listOf(IpFamily.IP_FAMILY_IPV6)
            else -> listOf(IpFamily.IP_FAMILY_IPV4, IpFamily.IP_FAMILY_IPV6)
        }
    }

    fun addressMatchesFamily(address: InetAddress, family: IpFamily): Boolean {
        return when (family) {
            IpFamily.IP_FAMILY_IPV4 -> address is Inet4Address
            IpFamily.IP_FAMILY_IPV6 -> address is Inet6Address
            else -> true
        }
    }

    fun parseIpLiteral(value: String): InetAddress? {
        val trimmed = value.trim()
        if (trimmed.isBlank() || !trimmed.all { it.isDigit() || it == '.' || it == ':' || it.lowercaseChar() in 'a'..'f' }) {
            return null
        }
        return runCatching { InetAddress.getByName(trimmed) }.getOrNull()
    }

    fun isGlobalUnicast(address: InetAddress): Boolean {
        if (
            address.isAnyLocalAddress ||
            address.isLoopbackAddress ||
            address.isLinkLocalAddress ||
            address.isSiteLocalAddress ||
            address.isMulticastAddress
        ) {
            return false
        }
        val bytes = address.address.map { it.toInt() and 0xff }
        if (address is Inet4Address) {
            val first = bytes.getOrElse(0) { 0 }
            val second = bytes.getOrElse(1) { 0 }
            if (first == 0 || first == 127 || first >= 224) return false
            if (first == 100 && second in 64..127) return false
        }
        if (address is Inet6Address) {
            val first = bytes.getOrElse(0) { 0 }
            if ((first and 0xfe) == 0xfc) return false
        }
        return true
    }

    /** Process-backed probes pass only when the process exits cleanly before timeout. */
    fun processSucceeded(finished: Boolean, exitCode: Int): Boolean = finished && exitCode == 0

    /** Download accepts any successful 2xx/3xx HTTP response with no local read error. */
    fun downloadSucceeded(error: String, status: Int): Boolean = error.isBlank() && status in 200..399

    /** Embedded HTTP probes without an explicit expected status accept any successful response. */
    fun httpStatusSucceeded(error: String, status: Int): Boolean = error.isBlank() && status in 200..399

    /** HTTP check pass/fail is exact status match plus no connection error. */
    fun httpSucceeded(matched: Boolean, error: String): Boolean = matched && error.isBlank()
}
