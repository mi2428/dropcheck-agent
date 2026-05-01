package io.dropcheck.agent

import io.dropcheck.agent.grpc.CommandResult
import io.dropcheck.agent.grpc.DnsAnswer
import io.dropcheck.agent.grpc.DnsRecordType
import io.dropcheck.agent.grpc.HttpCheck
import io.dropcheck.agent.grpc.HttpCheckResult
import io.dropcheck.agent.grpc.NetworkSelector
import io.dropcheck.agent.grpc.Ping
import io.dropcheck.agent.grpc.PingResult
import io.dropcheck.agent.grpc.ResolveDns
import io.dropcheck.agent.grpc.ResolveDnsResult
import io.dropcheck.agent.grpc.Traceroute
import io.dropcheck.agent.grpc.TracerouteResult
import io.dropcheck.agent.grpc.Wget
import io.dropcheck.agent.grpc.WgetResult
import java.io.File
import java.net.HttpURLConnection
import java.net.Inet4Address
import java.net.Inet6Address
import java.net.URL
import java.time.Duration
import java.util.Locale
import java.util.concurrent.TimeUnit

/**
 * Executes local network probes on a selected Android [android.net.Network].
 *
 * Android binding and I/O stay here; deterministic defaults and success rules
 * are delegated to [NetworkCheckPolicy] for plain JVM coverage.
 */
class NetworkCheckExecutor(
    private val networks: NetworkRepository,
    private val logger: CommandLogger,
) {
    /** Returns IP/link state for the selected Wi-Fi network without running a probe. */
    fun getIpStatus(selector: NetworkSelector): CommandResult {
        logger.info("ip status requested selector_ssid=${selector.ssid.ifBlank { "*" }}")
        logger.debugEvent("network.ip_status.request", selector.logFields())
        val ip = networks.ipStatusFor(selector, 0)
            ?: return failed("wifi network not available")
        logger.debug("ip status observed network=${ip.networkId} transports=${ip.transportsList.joinToString(",")} iface=${ip.interfaceName} addresses=${ip.addressesList.joinToString(",")} dns=${ip.dnsServersList.joinToString(",")} routes=${ip.routesList.size} validated=${ip.validated} internet=${ip.internet}")
        logger.debugEvent("network.ip_status.result", listOf(
            "network_id" to ip.networkId,
            "transports" to ip.transportsList,
            "iface" to ip.interfaceName,
            "addresses" to ip.addressesList,
            "dns_servers" to ip.dnsServersList,
            "routes_count" to ip.routesCount,
            "validated" to ip.validated,
            "internet" to ip.internet,
        ))
        return CommandResult.newBuilder()
            .setStatus(CommandResult.Status.STATUS_OK)
            .setIpStatus(ip)
            .build()
    }

    /**
     * Runs Android's ping binary bound to the selected Wi-Fi interface.
     *
     * Process output is included for diagnostics but intentionally truncated
     * before sending it back over gRPC.
     */
    fun ping(command: Ping): CommandResult {
        val count = NetworkCheckPolicy.pingCount(command.count)
        val timeoutMs = NetworkCheckPolicy.pingTimeoutMs(command.timeoutMs, count)
        logger.debug("ping parameters host=${command.host} count=$count size_bytes=${command.sizeBytes} timeout_ms=$timeoutMs selector_ssid=${command.selector.ssid.ifBlank { "*" }}")
        logger.debugEvent("network.probe.request", listOf(
            "probe" to "ping",
            "effective_count" to count,
            "effective_timeout_ms" to timeoutMs,
        ) + command.logFields())
        val network = networks.waitForNetwork(command.selector, 0)
            ?: return failed("wifi network not available for ping")
        val ip = networks.ipStatus(network)
        val iface = ip.interfaceName
        logger.debug("ping network selected network=${ip.networkId} transports=${ip.transportsList.joinToString(",")} iface=$iface addresses=${ip.addressesList.joinToString(",")}")
        logger.debugEvent("network.selected", listOf(
            "probe" to "ping",
            "network_id" to ip.networkId,
            "transports" to ip.transportsList,
            "iface" to iface,
            "addresses" to ip.addressesList,
            "dns_servers" to ip.dnsServersList,
            "validated" to ip.validated,
            "internet" to ip.internet,
        ))
        val binary = NetworkCheckPolicy.pingBinary(command.host)
        val args = NetworkCheckPolicy.pingArgs(binary, iface, command.sizeBytes, count, command.host)
        logger.info("ping start command=${args.joinToString(" ")} timeout_ms=$timeoutMs")
        logger.debug("ping process start binary=$binary interface=${iface.ifBlank { "default" }} args=${args.drop(1).joinToString(" ")}")
        logger.debugEvent("process.start", listOf(
            "probe" to "ping",
            "binary" to binary,
            "argv" to args,
            "command_line" to args.joinToString(" "),
            "host" to command.host,
            "iface" to iface.ifBlank { "default" },
            "timeout_ms" to timeoutMs,
        ))

        val started = System.nanoTime()
        val run = runProcess(args, timeoutMs)
        val elapsedMs = Duration.ofNanos(System.nanoTime() - started).toMillis()
        val ok = NetworkCheckPolicy.processSucceeded(run.finished, run.exitCode)
        logger.debug("ping finished host=${command.host} finished=${run.finished} exit=${run.exitCode} elapsed_ms=$elapsedMs output_bytes=${run.output.length}")
        logger.debug("ping output preview=${run.output.lineSequence().take(4).joinToString(" / ").take(500)}")
        logger.debugEvent("process.end", listOf(
            "probe" to "ping",
            "binary" to binary,
            "host" to command.host,
            "finished" to run.finished,
            "exit_code" to run.exitCode,
            "elapsed_ms" to elapsedMs,
            "output_bytes" to run.output.length,
            "output_preview" to StructuredLog.preview(run.output),
            "error" to run.error,
        ))

        val result = PingResult.newBuilder()
            .setHost(command.host)
            .setCount(count)
            .setElapsedMs(elapsedMs)
            .setExitCode(run.exitCode)
            .setInterfaceName(iface)
            .setOutput(run.output.take(4000))
            .setSizeBytes(command.sizeBytes)
            .build()

        logger.debugEvent("network.probe.result", listOf(
            "probe" to "ping",
            "ok" to ok,
        ) + result.logFields())
        return CommandResult.newBuilder()
            .setStatus(if (ok) CommandResult.Status.STATUS_OK else CommandResult.Status.STATUS_FAILED)
            .setMessage(if (ok) "ping passed" else "ping failed")
            .setPing(result)
            .build()
    }

    /**
     * Runs a device-local traceroute implementation when one is available.
     *
     * Some Android images ship only toybox/busybox applets, so executable
     * discovery is part of this command rather than a startup capability.
     */
    fun traceroute(command: Traceroute): CommandResult {
        val timeoutMs = NetworkCheckPolicy.effectiveTimeoutMs(
            command.timeoutMs,
            NetworkCheckPolicy.DEFAULT_TRACEROUTE_TIMEOUT_MS,
        )
        val maxHops = NetworkCheckPolicy.tracerouteMaxHops(command.maxHops)
        logger.debug("traceroute parameters host=${command.host} max_hops=$maxHops size_bytes=${command.sizeBytes} timeout_ms=$timeoutMs selector_ssid=${command.selector.ssid.ifBlank { "*" }}")
        logger.debugEvent("network.probe.request", listOf(
            "probe" to "traceroute",
            "effective_max_hops" to maxHops,
            "effective_timeout_ms" to timeoutMs,
        ) + command.logFields())
        val network = networks.waitForNetwork(command.selector, 0)
            ?: return failed("wifi network not available for traceroute")
        val ip = networks.ipStatus(network)
        val iface = ip.interfaceName
        logger.debugEvent("network.selected", listOf(
            "probe" to "traceroute",
            "network_id" to ip.networkId,
            "transports" to ip.transportsList,
            "iface" to iface,
            "addresses" to ip.addressesList,
            "dns_servers" to ip.dnsServersList,
            "validated" to ip.validated,
            "internet" to ip.internet,
        ))
        val commandArgs = tracerouteArgs(command.host, maxHops, command.sizeBytes, iface)
        if (commandArgs == null) {
            logger.warn("traceroute binary not available host=${command.host} iface=${iface.ifBlank { "default" }}; running ping TTL fallback")
            logger.warnEvent("network.probe.fallback", listOf(
                "probe" to "traceroute",
                "fallback" to "ping_ttl",
                "host" to command.host,
                "iface" to iface.ifBlank { "default" },
                "max_hops" to maxHops,
                "timeout_ms" to timeoutMs,
            ))
            return tracerouteWithPing(command, maxHops, timeoutMs, iface)
        }
        logger.info("traceroute start command=${commandArgs.joinToString(" ")} timeout_ms=$timeoutMs")
        logger.debugEvent("process.start", listOf(
            "probe" to "traceroute",
            "binary" to commandArgs.firstOrNull().orEmpty(),
            "argv" to commandArgs,
            "command_line" to commandArgs.joinToString(" "),
            "host" to command.host,
            "iface" to iface.ifBlank { "default" },
            "timeout_ms" to timeoutMs,
        ))
        val started = System.nanoTime()
        val run = runProcess(commandArgs, timeoutMs)
        val elapsedMs = Duration.ofNanos(System.nanoTime() - started).toMillis()
        val ok = NetworkCheckPolicy.processSucceeded(run.finished, run.exitCode)
        val executable = commandArgs
            .take(if (commandArgs.first().endsWith("toybox") || commandArgs.first().endsWith("busybox")) 2 else 1)
            .joinToString(" ")
        val result = TracerouteResult.newBuilder()
            .setHost(command.host)
            .setMaxHops(maxHops)
            .setSizeBytes(command.sizeBytes)
            .setElapsedMs(elapsedMs)
            .setExitCode(run.exitCode)
            .setInterfaceName(iface)
            .setOutput(run.output.take(12000))
            .setError(run.error)
            .setExecutable(executable)
            .build()
        logger.debug("traceroute finished host=${command.host} finished=${run.finished} exit=${run.exitCode} elapsed_ms=$elapsedMs output_bytes=${run.output.length} error=${run.error.ifBlank { "none" }}")
        logger.debugEvent("process.end", listOf(
            "probe" to "traceroute",
            "binary" to commandArgs.firstOrNull().orEmpty(),
            "host" to command.host,
            "finished" to run.finished,
            "exit_code" to run.exitCode,
            "elapsed_ms" to elapsedMs,
            "output_bytes" to run.output.length,
            "output_preview" to StructuredLog.preview(run.output),
            "error" to run.error,
        ))
        logger.debugEvent("network.probe.result", listOf(
            "probe" to "traceroute",
            "ok" to ok,
        ) + result.logFields())
        return CommandResult.newBuilder()
            .setStatus(if (ok) CommandResult.Status.STATUS_OK else CommandResult.Status.STATUS_FAILED)
            .setMessage(if (ok) "traceroute passed" else "traceroute failed")
            .setTraceroute(result)
            .build()
    }

    private fun tracerouteWithPing(command: Traceroute, maxHops: Int, timeoutMs: Int, iface: String): CommandResult {
        val started = System.nanoTime()
        val binary = NetworkCheckPolicy.pingBinary(command.host)
        val output = StringBuilder()
        output.append("traceroute to ")
            .append(command.host)
            .append(", ")
            .append(maxHops)
            .append(" hops max")
        if (command.sizeBytes > 0) {
            output.append(", ").append(command.sizeBytes).append(" byte packets")
        }
        output.append(" (ping TTL fallback)\n")

        var reached = false
        var error = ""
        for (ttl in 1..maxHops) {
            throwIfInterrupted()
            val elapsedBeforeHop = Duration.ofNanos(System.nanoTime() - started).toMillis()
            val remainingMs = timeoutMs - elapsedBeforeHop.toInt()
            if (remainingMs <= 0) {
                error = "process_timeout=${timeoutMs}ms"
                break
            }
            val hopTimeoutMs = remainingMs.coerceAtMost(PING_TRACE_HOP_TIMEOUT_MS).coerceAtLeast(1)
            val waitSeconds = ((hopTimeoutMs + 999) / 1000).coerceAtLeast(1)
            val args = NetworkCheckPolicy.traceroutePingArgs(
                binary = binary,
                interfaceName = iface,
                sizeBytes = command.sizeBytes,
                ttl = ttl,
                waitSeconds = waitSeconds,
                host = command.host,
            )
            logger.debug("traceroute ping fallback hop=$ttl command=${args.joinToString(" ")} timeout_ms=$hopTimeoutMs")
            logger.debugEvent("process.start", listOf(
                "probe" to "traceroute_ping_ttl",
                "ttl" to ttl,
                "binary" to binary,
                "argv" to args,
                "command_line" to args.joinToString(" "),
                "host" to command.host,
                "iface" to iface.ifBlank { "default" },
                "timeout_ms" to hopTimeoutMs,
                "remaining_timeout_ms" to remainingMs,
            ))
            val hopStarted = System.nanoTime()
            val run = runProcess(args, hopTimeoutMs)
            val hopElapsedMs = Duration.ofNanos(System.nanoTime() - hopStarted).toMillis()
            val probe = parsePingTraceProbe(run.output, command.host, hopElapsedMs)
            logger.debugEvent("process.end", listOf(
                "probe" to "traceroute_ping_ttl",
                "ttl" to ttl,
                "host" to command.host,
                "finished" to run.finished,
                "exit_code" to run.exitCode,
                "elapsed_ms" to hopElapsedMs,
                "output_bytes" to run.output.length,
                "output_preview" to StructuredLog.preview(run.output),
                "error" to run.error,
                "hop_host" to probe.host,
                "hop_address" to probe.address,
                "hop_reached" to probe.reached,
                "hop_timed_out" to probe.timedOut,
                "hop_rtt_ms" to probe.rttMs,
            ))
            output.append(formatPingTraceHop(ttl, probe))
            if (run.error.isNotBlank() && error.isBlank()) {
                error = run.error
            }
            if (probe.reached) {
                reached = true
                error = ""
                break
            }
        }

        if (!reached && error.isBlank()) {
            error = "target_not_reached"
        }
        val elapsedMs = Duration.ofNanos(System.nanoTime() - started).toMillis()
        val result = TracerouteResult.newBuilder()
            .setHost(command.host)
            .setMaxHops(maxHops)
            .setSizeBytes(command.sizeBytes)
            .setElapsedMs(elapsedMs)
            .setExitCode(if (reached) 0 else -1)
            .setInterfaceName(iface)
            .setOutput(output.toString().take(12000))
            .setError(error)
            .setExecutable("ping TTL fallback")
            .build()
        logger.debug("traceroute ping fallback finished host=${command.host} reached=$reached elapsed_ms=$elapsedMs output_bytes=${output.length} error=${error.ifBlank { "none" }}")
        logger.debugEvent("network.probe.result", listOf(
            "probe" to "traceroute",
            "fallback" to "ping_ttl",
            "ok" to reached,
        ) + result.logFields())
        return CommandResult.newBuilder()
            .setStatus(if (reached) CommandResult.Status.STATUS_OK else CommandResult.Status.STATUS_FAILED)
            .setMessage(if (reached) "traceroute ping fallback passed" else "traceroute ping fallback failed")
            .setTraceroute(result)
            .build()
    }

    /**
     * Downloads the response body through the selected Android Network.
     *
     * The agent discards bytes after counting them; content inspection belongs
     * on the controller side if needed.
     */
    fun download(command: Wget): CommandResult {
        val timeoutMs = NetworkCheckPolicy.effectiveTimeoutMs(
            command.timeoutMs,
            NetworkCheckPolicy.DEFAULT_DOWNLOAD_TIMEOUT_MS,
        )
        logger.debugEvent("network.probe.request", listOf(
            "probe" to "download",
            "effective_timeout_ms" to timeoutMs,
        ) + command.logFields())
        val network = networks.waitForNetwork(command.selector, 0)
            ?: return failed("wifi network not available for download")
        val ip = networks.ipStatus(network)
        val iface = ip.interfaceName
        logger.info("download start url=${command.url} timeout_ms=$timeoutMs iface=${iface.ifBlank { "default" }}")
        logger.debugEvent("network.selected", listOf(
            "probe" to "download",
            "network_id" to ip.networkId,
            "transports" to ip.transportsList,
            "iface" to iface,
            "addresses" to ip.addressesList,
            "dns_servers" to ip.dnsServersList,
            "validated" to ip.validated,
            "internet" to ip.internet,
        ))
        val started = System.nanoTime()
        val result = WgetResult.newBuilder()
            .setUrl(command.url)
            .setInterfaceName(iface)
        var conn: HttpURLConnection? = null
        try {
            conn = network.openConnection(URL(command.url)) as HttpURLConnection
            conn.connectTimeout = timeoutMs
            conn.readTimeout = timeoutMs
            conn.instanceFollowRedirects = true
            conn.requestMethod = "GET"
            logger.debugEvent("network.http.request", listOf(
                "probe" to "download",
                "method" to conn.requestMethod,
                "follow_redirects" to conn.instanceFollowRedirects,
                "connect_timeout_ms" to conn.connectTimeout,
                "read_timeout_ms" to conn.readTimeout,
                "iface" to iface.ifBlank { "default" },
            ) + command.logFields())
            val status = conn.responseCode
            logger.debugEvent("network.http.response", listOf(
                "probe" to "download",
                "url" to command.url,
                "http_status" to status,
                "content_type" to conn.contentType.orEmpty(),
                "content_length" to conn.contentLengthLong,
            ) + urlFields(command.url))
            result.status = status
            result.contentType = conn.contentType.orEmpty()
            result.contentLength = conn.contentLengthLong
            val input = if (status >= 400) conn.errorStream ?: conn.inputStream else conn.inputStream
            val buffer = ByteArray(64 * 1024)
            var bytes = 0L
            input.use { stream ->
                while (true) {
                    throwIfInterrupted()
                    if (timeoutMs > 0 && Duration.ofNanos(System.nanoTime() - started).toMillis() > timeoutMs) {
                        result.error = "download_timeout=${timeoutMs}ms"
                        break
                    }
                    val read = stream.read(buffer)
                    if (read < 0) break
                    bytes += read.toLong()
                }
            }
            result.bytesRead = bytes
            if (status >= 400 && result.error.isBlank()) {
                result.error = "http_status=$status"
            }
        } catch (e: Exception) {
            result.error = e.toString()
            logger.warn("download error url=${command.url} error=$e")
        } finally {
            conn?.disconnect()
        }
        val elapsedMs = Duration.ofNanos(System.nanoTime() - started).toMillis()
        result.elapsedMs = elapsedMs
        if (elapsedMs > 0) {
            result.throughputBps = result.bytesRead.toDouble() * 1000.0 / elapsedMs.toDouble()
        }
        val built = result.build()
        val ok = NetworkCheckPolicy.downloadSucceeded(built.error, built.status)
        logger.debug("download finished url=${command.url} status=${built.status} bytes=${built.bytesRead} elapsed_ms=${built.elapsedMs} throughput_bps=${built.throughputBps} error=${built.error.ifBlank { "none" }}")
        logger.debugEvent("network.probe.result", listOf(
            "probe" to "download",
            "ok" to ok,
        ) + built.logFields())
        return CommandResult.newBuilder()
            .setStatus(if (ok) CommandResult.Status.STATUS_OK else CommandResult.Status.STATUS_FAILED)
            .setMessage(if (ok) "download passed" else "download failed")
            .setWget(built)
            .build()
    }

    /** Resolves DNS via the selected Android Network so per-network DNS settings are honored. */
    fun dns(command: ResolveDns): CommandResult {
        logger.debugEvent("network.probe.request", listOf(
            "probe" to "dns",
        ) + command.logFields())
        val network = networks.waitForNetwork(command.selector, 0)
            ?: return failed("wifi network not available for DNS resolution")
        val ip = networks.ipStatus(network)
        logger.debug("dns network selected network=${ip.networkId} transports=${ip.transportsList.joinToString(",")} iface=${ip.interfaceName} dns=${ip.dnsServersList.joinToString(",")}")
        logger.debug("dns parameters name=${command.name} qtypes=${command.qtypesList.joinToString(",")} timeout_ms=${command.timeoutMs} selector_ssid=${command.selector.ssid.ifBlank { "*" }}")
        logger.debugEvent("network.selected", listOf(
            "probe" to "dns",
            "network_id" to ip.networkId,
            "transports" to ip.transportsList,
            "iface" to ip.interfaceName,
            "addresses" to ip.addressesList,
            "dns_servers" to ip.dnsServersList,
            "validated" to ip.validated,
            "internet" to ip.internet,
        ))
        val qtypes = NetworkCheckPolicy.dnsRecordTypes(command.qtypesList)

        val started = System.nanoTime()
        val result = ResolveDnsResult.newBuilder()
            .setName(command.name)
        try {
            logger.info("dns start name=${command.name} qtypes=${qtypes.joinToString(",")}")
            logger.debugEvent("network.dns.lookup", listOf(
                "name" to command.name,
                "qtypes" to qtypes.map { it.name },
                "timeout_ms" to command.timeoutMs,
                "iface" to ip.interfaceName.ifBlank { "default" },
                "dns_servers" to ip.dnsServersList,
            ))
            val addresses = network.getAllByName(command.name).toList()
            logger.debug("dns raw addresses name=${command.name} count=${addresses.size} values=${addresses.joinToString(",") { it.hostAddress.orEmpty() }}")
            logger.debugEvent("network.dns.raw_result", listOf(
                "name" to command.name,
                "addresses_count" to addresses.size,
                "addresses" to addresses.map { it.hostAddress.orEmpty() },
            ))
            for (address in addresses) {
                when {
                    address is Inet4Address && DnsRecordType.DNS_RECORD_TYPE_A in qtypes -> {
                        result.addAnswers(DnsAnswer.newBuilder()
                            .setType(DnsRecordType.DNS_RECORD_TYPE_A)
                            .setAddress(address.hostAddress.orEmpty())
                            .build())
                    }
                    address is Inet6Address && DnsRecordType.DNS_RECORD_TYPE_AAAA in qtypes -> {
                        result.addAnswers(DnsAnswer.newBuilder()
                            .setType(DnsRecordType.DNS_RECORD_TYPE_AAAA)
                            .setAddress(address.hostAddress.orEmpty())
                            .build())
                    }
                }
            }
        } catch (e: Exception) {
            result.error = e.toString()
            logger.warn("dns error name=${command.name} error=$e")
        }
        result.elapsedMs = Duration.ofNanos(System.nanoTime() - started).toMillis()
        val built = result.build()
        logger.debug("dns finished name=${command.name} answers=${built.answersList.joinToString(",") { "${it.type}:${it.address}" }} error=${built.error.ifBlank { "none" }} elapsed_ms=${built.elapsedMs}")
        val ok = built.error.isBlank() && built.answersCount > 0
        logger.debugEvent("network.probe.result", listOf(
            "probe" to "dns",
            "ok" to ok,
        ) + built.logFields())
        return CommandResult.newBuilder()
            .setStatus(if (ok) CommandResult.Status.STATUS_OK else CommandResult.Status.STATUS_FAILED)
            .setMessage(if (ok) "dns resolved" else "dns resolution failed")
            .setResolveDns(built)
            .build()
    }

    /**
     * Performs a simple status-code check through the selected Android Network.
     *
     * Redirects are not followed here so captive portal and redirect behavior is
     * visible to the controller.
     */
    fun http(command: HttpCheck): CommandResult {
        logger.debugEvent("network.probe.request", listOf(
            "probe" to "http",
        ) + command.logFields())
        val network = networks.waitForNetwork(command.selector, 0)
            ?: return failed("wifi network not available for HTTP check")
        val ip = networks.ipStatus(network)
        logger.debug("http network selected network=${ip.networkId} transports=${ip.transportsList.joinToString(",")} iface=${ip.interfaceName} addresses=${ip.addressesList.joinToString(",")}")
        logger.debugEvent("network.selected", listOf(
            "probe" to "http",
            "network_id" to ip.networkId,
            "transports" to ip.transportsList,
            "iface" to ip.interfaceName,
            "addresses" to ip.addressesList,
            "dns_servers" to ip.dnsServersList,
            "validated" to ip.validated,
            "internet" to ip.internet,
        ))
        val expected = NetworkCheckPolicy.httpExpectedStatus(command.expectedStatus)
        val timeoutMs = NetworkCheckPolicy.effectiveTimeoutMs(
            command.timeoutMs,
            NetworkCheckPolicy.DEFAULT_HTTP_TIMEOUT_MS,
        )
        val parsedUrl = runCatching { URL(command.url) }.getOrNull()
        logger.debug("http parameters scheme=${parsedUrl?.protocol.orEmpty()} host=${parsedUrl?.host.orEmpty()} port=${parsedUrl?.port ?: -1} path=${parsedUrl?.path.orEmpty()} expected=$expected timeout_ms=$timeoutMs selector_ssid=${command.selector.ssid.ifBlank { "*" }}")
        val started = System.nanoTime()
        val result = HttpCheckResult.newBuilder()
            .setUrl(command.url)
            .setExpectedStatus(expected)

        try {
            logger.info("http start url=${command.url} expected=$expected timeout_ms=$timeoutMs")
            val conn = network.openConnection(URL(command.url)) as HttpURLConnection
            conn.connectTimeout = timeoutMs
            conn.readTimeout = timeoutMs
            conn.instanceFollowRedirects = false
            logger.debug("http connection prepared method=${conn.requestMethod} follow_redirects=${conn.instanceFollowRedirects} connect_timeout=${conn.connectTimeout} read_timeout=${conn.readTimeout}")
            logger.debugEvent("network.http.request", listOf(
                "probe" to "http",
                "method" to conn.requestMethod,
                "effective_expected_status" to expected,
                "follow_redirects" to conn.instanceFollowRedirects,
                "connect_timeout_ms" to conn.connectTimeout,
                "read_timeout_ms" to conn.readTimeout,
                "iface" to ip.interfaceName.ifBlank { "default" },
            ) + command.logFields())
            val status = conn.responseCode
            logger.debug("http response headers status=$status content_type=${conn.contentType.orEmpty()} content_length=${conn.contentLengthLong}")
            logger.debugEvent("network.http.response", listOf(
                "probe" to "http",
                "url" to command.url,
                "http_status" to status,
                "expected_status" to expected,
                "matched" to (status == expected),
                "content_type" to conn.contentType.orEmpty(),
                "content_length" to conn.contentLengthLong,
            ) + urlFields(command.url))
            conn.disconnect()
            result.status = status
            result.matched = status == expected
        } catch (e: Exception) {
            result.error = e.toString()
            logger.warn("http error url=${command.url} error=$e")
        }
        result.elapsedMs = Duration.ofNanos(System.nanoTime() - started).toMillis()
        val built = result.build()
        logger.debug("http finished url=${command.url} status=${built.status} expected=${built.expectedStatus} matched=${built.matched} error=${built.error.ifBlank { "none" }} elapsed_ms=${built.elapsedMs}")
        logger.debugEvent("network.probe.result", listOf(
            "probe" to "http",
            "ok" to NetworkCheckPolicy.httpSucceeded(built.matched, built.error),
        ) + built.logFields())
        return CommandResult.newBuilder()
            .setStatus(if (NetworkCheckPolicy.httpSucceeded(built.matched, built.error)) CommandResult.Status.STATUS_OK else CommandResult.Status.STATUS_FAILED)
            .setMessage(if (built.matched) "http check passed" else "http check failed")
            .setHttpCheck(built)
            .build()
    }

    /** Builds traceroute argv for the first installed implementation that supports the applet. */
    private fun tracerouteArgs(host: String, maxHops: Int, sizeBytes: Int, iface: String): List<String>? {
        val candidates = listOf(
            listOf("/system/bin/traceroute") to true,
            listOf("/system/xbin/traceroute") to true,
            listOf("/system/bin/toybox", "traceroute") to supportsApplet("/system/bin/toybox", "traceroute"),
            listOf("/system/bin/busybox", "traceroute") to supportsApplet("/system/bin/busybox", "traceroute"),
        )
        val prefix = candidates.firstOrNull { (args, supported) -> supported && File(args.first()).exists() }?.first ?: return null
        return buildList {
            addAll(prefix)
            if (iface.isNotBlank()) {
                add("-i")
                add(iface)
            }
            add("-m")
            add(maxHops.toString())
            add("-w")
            add("2")
            add(host)
            if (sizeBytes > 0) {
                add(sizeBytes.toString())
            }
        }
    }

    private fun supportsApplet(binary: String, applet: String): Boolean {
        if (!File(binary).exists()) return false
        return runCatching {
            val run = runProcess(listOf(binary, "--list"), 1000)
            run.output.lineSequence().any { it.trim() == applet }
        }.getOrDefault(false)
    }

    /**
     * Starts a process without a shell and collects merged stdout/stderr after it exits.
     *
     * On timeout, the process is forcibly killed and reported as exit -1.
     */
    private fun runProcess(args: List<String>, timeoutMs: Int): ProcessRun {
        val process = ProcessBuilder(args)
            .redirectErrorStream(true)
            .start()
        val finished = process.waitFor(timeoutMs.toLong(), TimeUnit.MILLISECONDS)
        if (!finished) {
            process.destroyForcibly()
            process.waitFor(500, TimeUnit.MILLISECONDS)
        }
        val output = runCatching { process.inputStream.bufferedReader().readText() }.getOrDefault("")
        val exitCode = if (finished) process.exitValue() else -1
        val error = if (finished) "" else "process_timeout=${timeoutMs}ms"
        return ProcessRun(finished = finished, exitCode = exitCode, output = output, error = error)
    }

    private fun parsePingTraceProbe(output: String, target: String, elapsedMs: Long): PingTraceProbe {
        pingReachedRegex.find(output)?.let { match ->
            val identity = match.groupValues[1]
            val parenthesized = match.groupValues.getOrNull(2).orEmpty()
            val rtt = match.groupValues[3].toDoubleOrNull() ?: elapsedMs.toDouble()
            val (host, address) = splitPingIdentity(identity, parenthesized)
            return PingTraceProbe(
                host = host,
                address = address.ifBlank { target },
                rttMs = rtt,
                reached = true,
            )
        }
        pingTtlExceededRegex.find(output)?.let { match ->
            val identity = match.groupValues[1]
            val (host, address) = splitPingIdentity(identity, "")
            return PingTraceProbe(
                host = host,
                address = address,
                rttMs = elapsedMs.toDouble(),
                reached = false,
            )
        }
        return PingTraceProbe(timedOut = true)
    }

    private fun splitPingIdentity(identity: String, parenthesized: String): Pair<String, String> {
        val cleaned = identity.trim().trim(',', ';', ':')
        if (parenthesized.isNotBlank()) {
            return cleaned to parenthesized.trim()
        }
        val match = pingIdentityWithAddressRegex.matchEntire(cleaned)
        if (match != null) {
            return match.groupValues[1].trim() to match.groupValues[2].trim()
        }
        return if (isLikelyNumericAddress(cleaned)) "" to cleaned else cleaned to ""
    }

    private fun formatPingTraceHop(ttl: Int, probe: PingTraceProbe): String {
        if (probe.timedOut) {
            return "%2d  *\n".format(Locale.US, ttl)
        }
        val identity = when {
            probe.host.isNotBlank() && probe.address.isNotBlank() && probe.host != probe.address -> "${probe.host} (${probe.address})"
            probe.address.isNotBlank() -> probe.address
            else -> probe.host
        }
        val rtt = probe.rttMs ?: 0.0
        return "%2d  %s  %.3f ms\n".format(Locale.US, ttl, identity, rtt)
    }

    private fun isLikelyNumericAddress(value: String): Boolean {
        if (value.count { it == '.' } == 3 && value.all { it.isDigit() || it == '.' }) {
            return true
        }
        return ':' in value
    }

    private fun failed(message: String): CommandResult {
        logger.warn("command failed before execution result message=$message")
        return CommandResult.newBuilder()
            .setStatus(CommandResult.Status.STATUS_FAILED)
            .setMessage(message)
            .build()
    }

    private fun throwIfInterrupted() {
        if (Thread.currentThread().isInterrupted) {
            throw InterruptedException("command interrupted")
        }
    }

    private data class ProcessRun(
        val finished: Boolean,
        val exitCode: Int,
        val output: String,
        val error: String,
    )

    private data class PingTraceProbe(
        val host: String = "",
        val address: String = "",
        val rttMs: Double? = null,
        val reached: Boolean = false,
        val timedOut: Boolean = false,
    )

    companion object {
        private const val PING_TRACE_HOP_TIMEOUT_MS = 2_000
        private val pingReachedRegex = Regex(
            """(?im)(?:\d+\s+bytes\s+from|from)\s+([^\s:]+)(?:\s+\(([^)]+)\))?:.*time[=<]?(\d+(?:\.\d+)?)\s*ms""",
        )
        private val pingTtlExceededRegex = Regex(
            """(?im)^From\s+(.+?)(?::|\s+icmp_seq=).*Time to live exceeded""",
        )
        private val pingIdentityWithAddressRegex = Regex("""^(.+?)\s+\(([^)]+)\)$""")
    }
}
