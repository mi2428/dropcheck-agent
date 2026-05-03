package io.dropcheck.agent

import android.net.Network
import io.dropcheck.agent.grpc.CommandResult
import io.dropcheck.agent.grpc.DnsAnswer
import io.dropcheck.agent.grpc.DnsRecordType
import io.dropcheck.agent.grpc.GlobalIp
import io.dropcheck.agent.grpc.GlobalIpAddress
import io.dropcheck.agent.grpc.GlobalIpResult
import io.dropcheck.agent.grpc.HttpCheck
import io.dropcheck.agent.grpc.HttpCheckResult
import io.dropcheck.agent.grpc.IpFamily
import io.dropcheck.agent.grpc.IpStatus
import io.dropcheck.agent.grpc.NetworkSelector
import io.dropcheck.agent.grpc.PathMtu
import io.dropcheck.agent.grpc.PathMtuProbe
import io.dropcheck.agent.grpc.PathMtuResult
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
import java.net.InetAddress
import java.net.InetSocketAddress
import java.net.URL
import java.nio.charset.StandardCharsets
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
        val ip = waitForProbeSourceIpStatus(network, command.host, timeoutMs, "ping")
        val iface = ip.interfaceName
        val bindTarget = NetworkCheckPolicy.pingBindTarget(iface, ip.addressesList, command.host)
        logger.debug("ping network selected network=${ip.networkId} transports=${ip.transportsList.joinToString(",")} iface=$iface bind_target=${bindTarget.ifBlank { "default" }} addresses=${ip.addressesList.joinToString(",")}")
        logger.debugEvent("network.selected", listOf(
            "probe" to "ping",
            "network_id" to ip.networkId,
            "transports" to ip.transportsList,
            "iface" to iface,
            "bind_target" to bindTarget,
            "addresses" to ip.addressesList,
            "dns_servers" to ip.dnsServersList,
            "validated" to ip.validated,
            "internet" to ip.internet,
        ))
        val binary = NetworkCheckPolicy.pingBinary(command.host)
        val args = NetworkCheckPolicy.pingArgs(binary, bindTarget, command.sizeBytes, count, command.host)
        logger.execEvent("probe.exec", listOf(
            "probe" to "ping",
            "command_line" to args.joinToString(" "),
            "timeout_ms" to timeoutMs,
        ))
        logger.debug("ping process start binary=$binary interface=${iface.ifBlank { "default" }} bind_target=${bindTarget.ifBlank { "default" }} args=${args.drop(1).joinToString(" ")}")
        logger.debugEvent("process.start", listOf(
            "probe" to "ping",
            "binary" to binary,
            "argv" to args,
            "command_line" to args.joinToString(" "),
            "host" to command.host,
            "iface" to iface.ifBlank { "default" },
            "bind_target" to bindTarget.ifBlank { "default" },
            "timeout_ms" to timeoutMs,
        ))

        val started = System.nanoTime()
        val run = networks.withBoundNetwork(network) {
            runProcess(args, timeoutMs)
        }
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
        val ip = waitForProbeSourceIpStatus(network, command.host, timeoutMs, "traceroute")
        val iface = ip.interfaceName
        val pingBindTarget = NetworkCheckPolicy.pingBindTarget(iface, ip.addressesList, command.host)
        logger.debugEvent("network.selected", listOf(
            "probe" to "traceroute",
            "network_id" to ip.networkId,
            "transports" to ip.transportsList,
            "iface" to iface,
            "ping_bind_target" to pingBindTarget,
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
            return tracerouteWithPing(command, maxHops, timeoutMs, iface, pingBindTarget, network)
        }
        logger.execEvent("probe.exec", listOf(
            "probe" to "traceroute",
            "command_line" to commandArgs.joinToString(" "),
            "timeout_ms" to timeoutMs,
        ))
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
        val run = networks.withBoundNetwork(network) {
            runProcess(commandArgs, timeoutMs)
        }
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

    /**
     * Discovers path MTU by probing ping payloads with DF/no-fragmentation set.
     *
     * The search bounds are full IP MTU values. Each probe converts that MTU
     * to ICMP payload bytes before invoking ping, then binary-searches the
     * largest MTU that exits successfully within the command timeout.
     */
    fun pathMtu(command: PathMtu): CommandResult {
        val timeoutMs = NetworkCheckPolicy.effectiveTimeoutMs(
            command.timeoutMs,
            NetworkCheckPolicy.DEFAULT_PATH_MTU_TIMEOUT_MS,
        )
        val ipv6 = command.host.contains(":")
        val overheadBytes = NetworkCheckPolicy.pathMtuOverheadBytes(command.host)
        logger.debug("path mtu parameters host=${command.host} min_mtu=${command.minMtuBytes} max_mtu=${command.maxMtuBytes} timeout_ms=$timeoutMs selector_ssid=${command.selector.ssid.ifBlank { "*" }}")
        logger.debugEvent("network.probe.request", listOf(
            "probe" to "path_mtu",
            "effective_timeout_ms" to timeoutMs,
            "ip_overhead_bytes" to overheadBytes,
        ) + command.logFields())
        val network = networks.waitForNetwork(command.selector, 0)
            ?: return failed("wifi network not available for path mtu")
        val ip = waitForProbeSourceIpStatus(network, command.host, timeoutMs, "path_mtu")
        val iface = ip.interfaceName
        val bindTarget = NetworkCheckPolicy.pingBindTarget(iface, ip.addressesList, command.host)
        val minMtu = NetworkCheckPolicy.pathMtuMinBytes(command.minMtuBytes, ipv6)
        val maxMtu = NetworkCheckPolicy.pathMtuMaxBytes(command.maxMtuBytes, ip.mtu, minMtu)
        val binary = NetworkCheckPolicy.pingBinary(command.host)
        logger.debugEvent("network.selected", listOf(
            "probe" to "path_mtu",
            "network_id" to ip.networkId,
            "transports" to ip.transportsList,
            "iface" to iface,
            "bind_target" to bindTarget,
            "addresses" to ip.addressesList,
            "dns_servers" to ip.dnsServersList,
            "validated" to ip.validated,
            "internet" to ip.internet,
            "interface_mtu" to ip.mtu,
            "min_mtu" to minMtu,
            "max_mtu" to maxMtu,
        ))

        val started = System.nanoTime()
        val probes = mutableListOf<PathMtuProbe>()
        var low = minMtu
        var high = maxMtu
        var discovered = false
        var error = ""

        // Validate the lower bound first; binary search is meaningful only if the floor can pass.
        val firstProbe = runPathMtuProbe(network, command.host, minMtu, overheadBytes, binary, iface, bindTarget, timeoutMs, started)
        probes += firstProbe
        if (!firstProbe.passed) {
            error = "minimum_mtu_failed"
            low = 0
        } else {
            discovered = true
            while (low < high) {
                throwIfInterrupted()
                val remainingMs = remainingTimeoutMs(timeoutMs, started)
                if (remainingMs <= 0) {
                    error = "process_timeout=${timeoutMs}ms"
                    break
                }
                // Bias upward so adjacent low/high values converge and low remains the best passing MTU.
                val mid = low + (high - low + 1) / 2
                val probe = runPathMtuProbe(network, command.host, mid, overheadBytes, binary, iface, bindTarget, remainingMs, started)
                probes += probe
                if (probe.passed) {
                    low = mid
                } else {
                    high = mid - 1
                }
            }
        }

        val elapsedMs = Duration.ofNanos(System.nanoTime() - started).toMillis()
        val pathMtuBytes = if (discovered) low else 0
        val payloadSizeBytes = if (discovered) {
            NetworkCheckPolicy.pathMtuPayloadBytes(pathMtuBytes, overheadBytes)
        } else {
            0
        }
        val result = PathMtuResult.newBuilder()
            .setHost(command.host)
            .setDiscovered(discovered && error.isBlank())
            .setPathMtuBytes(pathMtuBytes)
            .setPayloadSizeBytes(payloadSizeBytes)
            .setMinMtuBytes(minMtu)
            .setMaxMtuBytes(maxMtu)
            .setIpOverheadBytes(overheadBytes)
            .setElapsedMs(elapsedMs)
            .setInterfaceName(iface)
            .setError(error)
            .addAllProbes(probes)
            .build()
        val ok = result.discovered
        logger.debug("path mtu finished host=${command.host} discovered=$ok mtu=${result.pathMtuBytes} payload=${result.payloadSizeBytes} probes=${result.probesCount} elapsed_ms=$elapsedMs error=${error.ifBlank { "none" }}")
        logger.debugEvent("network.probe.result", listOf(
            "probe" to "path_mtu",
            "ok" to ok,
        ) + result.logFields())
        return CommandResult.newBuilder()
            .setStatus(if (ok) CommandResult.Status.STATUS_OK else CommandResult.Status.STATUS_FAILED)
            .setMessage(if (ok) "path mtu discovered" else "path mtu discovery failed")
            .setPathMtu(result)
            .build()
    }

    /**
     * Queries ifconfig.me over IPv4 and/or IPv6 and returns the public address seen by that service.
     *
     * Network.openConnection() does not let us force an address family. This command resolves A/AAAA
     * on the selected Android Network, opens a raw socket to the chosen literal address, and sends an
     * HTTP Host header for ifconfig.me so each probe is pinned to the requested family.
     */
    fun globalIp(command: GlobalIp): CommandResult {
        val timeoutMs = NetworkCheckPolicy.effectiveTimeoutMs(
            command.timeoutMs,
            NetworkCheckPolicy.DEFAULT_GLOBAL_IP_TIMEOUT_MS,
        )
        val requestedFamily = if (command.family == IpFamily.IP_FAMILY_UNSPECIFIED) {
            IpFamily.IP_FAMILY_ALL
        } else {
            command.family
        }
        val families = NetworkCheckPolicy.globalIpFamilies(requestedFamily)
        logger.debugEvent("network.probe.request", listOf(
            "probe" to "global_ip",
            "service" to GLOBAL_IP_SERVICE_HOST,
            "requested_family" to requestedFamily.name,
            "families" to families.map { it.name },
            "effective_timeout_ms" to timeoutMs,
        ) + command.logFields())
        val network = networks.waitForNetwork(command.selector, 0)
            ?: return failed("wifi network not available for global IP check")
        val ip = networks.ipStatus(network)
        logger.debugEvent("network.selected", listOf(
            "probe" to "global_ip",
            "network_id" to ip.networkId,
            "transports" to ip.transportsList,
            "iface" to ip.interfaceName,
            "addresses" to ip.addressesList,
            "dns_servers" to ip.dnsServersList,
            "validated" to ip.validated,
            "internet" to ip.internet,
        ))
        logger.execEvent("probe.exec", listOf(
            "probe" to "global_ip",
            "service" to GLOBAL_IP_SERVICE_HOST,
            "requested_family" to requestedFamily.name,
            "families" to families.map { it.name },
            "timeout_ms" to timeoutMs,
            "iface" to ip.interfaceName.ifBlank { "default" },
        ))

        val started = System.nanoTime()
        val result = GlobalIpResult.newBuilder()
            .setService(GLOBAL_IP_SERVICE_HOST)
            .setRequestedFamily(requestedFamily)
            .setInterfaceName(ip.interfaceName)
        val resolved = try {
            network.getAllByName(GLOBAL_IP_SERVICE_HOST).toList()
        } catch (e: Exception) {
            result.error = e.toString()
            emptyList()
        }
        logger.debug("global ip resolved host=$GLOBAL_IP_SERVICE_HOST addresses=${resolved.joinToString(",") { it.hostAddress.orEmpty() }}")
        for (family in families) {
            throwIfInterrupted()
            val address = resolved.firstOrNull { NetworkCheckPolicy.addressMatchesFamily(it, family) }
            if (address == null) {
                result.addAddresses(GlobalIpAddress.newBuilder()
                    .setFamily(family)
                    .setError("no_dns_address_for_family")
                    .build())
                continue
            }
            result.addAddresses(runGlobalIpProbe(network, family, address, timeoutMs))
        }
        result.elapsedMs = Duration.ofNanos(System.nanoTime() - started).toMillis()
        val built = result.build()
        val ok = built.error.isBlank() &&
            built.addressesCount == families.size &&
            built.addressesList.all { it.error.isBlank() && it.global }
        if (!ok && built.error.isBlank()) {
            result.error = "one_or_more_families_failed"
        }
        val finalResult = result.build()
        logger.debug("global ip finished service=$GLOBAL_IP_SERVICE_HOST requested_family=$requestedFamily ok=$ok addresses=${finalResult.addressesList.joinToString(",") { "${it.family}:${it.ip}:${it.error.ifBlank { "ok" }}" }} elapsed_ms=${finalResult.elapsedMs}")
        logger.debugEvent("network.probe.result", listOf(
            "probe" to "global_ip",
            "ok" to ok,
        ) + finalResult.logFields())
        return CommandResult.newBuilder()
            .setStatus(if (ok) CommandResult.Status.STATUS_OK else CommandResult.Status.STATUS_FAILED)
            .setMessage(if (ok) "global IP check passed" else "global IP check failed")
            .setGlobalIp(finalResult)
            .build()
    }

    private fun tracerouteWithPing(command: Traceroute, maxHops: Int, timeoutMs: Int, iface: String, bindTarget: String, network: Network): CommandResult {
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
        var observedHop = false
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
                bindTarget = bindTarget,
                sizeBytes = command.sizeBytes,
                ttl = ttl,
                waitSeconds = waitSeconds,
                host = command.host,
            )
            logger.execDebugEvent("probe.exec", listOf(
                "probe" to "traceroute",
                "fallback" to "ping_ttl",
                "hop" to ttl,
                "command_line" to args.joinToString(" "),
                "timeout_ms" to hopTimeoutMs,
            ))
            logger.debugEvent("process.start", listOf(
                "probe" to "traceroute_ping_ttl",
                "ttl" to ttl,
                "binary" to binary,
                "argv" to args,
                "command_line" to args.joinToString(" "),
                "host" to command.host,
                "iface" to iface.ifBlank { "default" },
                "bind_target" to bindTarget.ifBlank { "default" },
                "timeout_ms" to hopTimeoutMs,
                "remaining_timeout_ms" to remainingMs,
            ))
            val hopStarted = System.nanoTime()
            val run = networks.withBoundNetwork(network) {
                runProcess(args, hopTimeoutMs)
            }
            val hopElapsedMs = Duration.ofNanos(System.nanoTime() - hopStarted).toMillis()
            val probe = parsePingTraceProbe(run.output, command.host, hopElapsedMs)
            if (!probe.timedOut && (probe.address.isNotBlank() || probe.host.isNotBlank())) {
                observedHop = true
            }
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

        val completed = reached || observedHop
        if (!completed && error.isBlank()) {
            error = "target_not_reached"
        }
        val elapsedMs = Duration.ofNanos(System.nanoTime() - started).toMillis()
        val result = TracerouteResult.newBuilder()
            .setHost(command.host)
            .setMaxHops(maxHops)
            .setSizeBytes(command.sizeBytes)
            .setElapsedMs(elapsedMs)
            .setExitCode(if (completed) 0 else -1)
            .setInterfaceName(iface)
            .setOutput(output.toString().take(12000))
            .setError(if (completed) "" else error)
            .setExecutable("ping TTL fallback")
            .build()
        logger.debug("traceroute ping fallback finished host=${command.host} reached=$reached observed_hop=$observedHop elapsed_ms=$elapsedMs output_bytes=${output.length} error=${error.ifBlank { "none" }}")
        logger.debugEvent("network.probe.result", listOf(
            "probe" to "traceroute",
            "fallback" to "ping_ttl",
            "ok" to completed,
        ) + result.logFields())
        return CommandResult.newBuilder()
            .setStatus(if (completed) CommandResult.Status.STATUS_OK else CommandResult.Status.STATUS_FAILED)
            .setMessage(if (completed) "traceroute ping fallback completed" else "traceroute ping fallback failed")
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
        logger.execEvent("probe.exec", listOf(
            "probe" to "download",
            "url" to command.url,
            "timeout_ms" to timeoutMs,
            "iface" to iface.ifBlank { "default" },
        ))
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
            logger.execEvent("probe.exec", listOf(
                "probe" to "dns",
                "name" to command.name,
                "qtypes" to qtypes.map { it.name },
                "timeout_ms" to command.timeoutMs,
                "iface" to ip.interfaceName.ifBlank { "default" },
            ))
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
            logger.execEvent("probe.exec", listOf(
                "probe" to "http",
                "url" to command.url,
                "expected_status" to expected,
                "timeout_ms" to timeoutMs,
            ))
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

    private fun waitForProbeSourceIpStatus(
        network: Network,
        host: String,
        timeoutMs: Int,
        probe: String,
    ): IpStatus {
        var ip = networks.ipStatus(network)
        if (NetworkCheckPolicy.sourceAddressForHost(ip.addressesList, host) != null) {
            return ip
        }
        val waitMs = timeoutMs.coerceAtMost(PROBE_SOURCE_ADDRESS_WAIT_MS)
        if (waitMs <= 0) {
            return ip
        }
        val started = System.nanoTime()
        var attempts = 0
        logger.debug("waiting for $probe source address host=$host iface=${ip.interfaceName.ifBlank { "default" }} addresses=${ip.addressesList.joinToString(",")} wait_ms=$waitMs")
        while (remainingTimeoutMs(waitMs, started) > 0) {
            throwIfInterrupted()
            val sleepMs = remainingTimeoutMs(waitMs, started)
                .coerceAtMost(PROBE_SOURCE_ADDRESS_POLL_MS)
                .coerceAtLeast(1)
            Thread.sleep(sleepMs.toLong())
            attempts += 1
            ip = networks.ipStatus(network)
            val source = NetworkCheckPolicy.sourceAddressForHost(ip.addressesList, host)
            if (source != null) {
                val elapsedMs = Duration.ofNanos(System.nanoTime() - started).toMillis()
                logger.debug("source address ready probe=$probe host=$host source=$source iface=${ip.interfaceName.ifBlank { "default" }} wait_elapsed_ms=$elapsedMs attempts=$attempts")
                return ip
            }
        }
        logger.warn("source address unavailable probe=$probe host=$host iface=${ip.interfaceName.ifBlank { "default" }} addresses=${ip.addressesList.joinToString(",")} wait_ms=$waitMs; using interface bind")
        return ip
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

    /**
     * Runs one DF ping probe for a candidate IP MTU.
     *
     * mtuBytes includes IP and ICMP headers, while ping -s expects only the
     * ICMP payload. The process timeout is capped per probe so a single lost
     * packet cannot consume the entire PMTU discovery budget.
     */
    private fun runPathMtuProbe(
        network: Network,
        host: String,
        mtuBytes: Int,
        overheadBytes: Int,
        binary: String,
        iface: String,
        bindTarget: String,
        remainingTimeoutMs: Int,
        startedAt: Long,
    ): PathMtuProbe {
        val payloadSizeBytes = NetworkCheckPolicy.pathMtuPayloadBytes(mtuBytes, overheadBytes)
        val timeoutMs = remainingTimeoutMs
            .coerceAtMost(PATH_MTU_PROBE_TIMEOUT_MS)
            .coerceAtLeast(1)
        // ping -W is specified in whole seconds, but runProcess still enforces the millisecond cap.
        val waitSeconds = ((timeoutMs + 999) / 1000).coerceAtLeast(1)
        val args = NetworkCheckPolicy.pathMtuPingArgs(binary, bindTarget, payloadSizeBytes, waitSeconds, host)
        logger.execEvent("probe.exec", listOf(
            "probe" to "path_mtu",
            "mtu_bytes" to mtuBytes,
            "payload_size_bytes" to payloadSizeBytes,
            "command_line" to args.joinToString(" "),
            "timeout_ms" to timeoutMs,
        ))
        logger.debugEvent("process.start", listOf(
            "probe" to "path_mtu",
            "binary" to binary,
            "argv" to args,
            "command_line" to args.joinToString(" "),
            "host" to host,
            "iface" to iface.ifBlank { "default" },
            "bind_target" to bindTarget.ifBlank { "default" },
            "mtu_bytes" to mtuBytes,
            "payload_size_bytes" to payloadSizeBytes,
            "timeout_ms" to timeoutMs,
        ))
        val probeStarted = System.nanoTime()
        val run = networks.withBoundNetwork(network) {
            runProcess(args, timeoutMs)
        }
        val elapsedMs = Duration.ofNanos(System.nanoTime() - probeStarted).toMillis()
        val ok = NetworkCheckPolicy.processSucceeded(run.finished, run.exitCode)
        logger.debugEvent("process.end", listOf(
            "probe" to "path_mtu",
            "binary" to binary,
            "host" to host,
            "mtu_bytes" to mtuBytes,
            "payload_size_bytes" to payloadSizeBytes,
            "finished" to run.finished,
            "exit_code" to run.exitCode,
            "elapsed_ms" to elapsedMs,
            "total_elapsed_ms" to Duration.ofNanos(System.nanoTime() - startedAt).toMillis(),
            "output_bytes" to run.output.length,
            "output_preview" to StructuredLog.preview(run.output),
            "error" to run.error,
        ))
        return PathMtuProbe.newBuilder()
            .setMtuBytes(mtuBytes)
            .setPayloadSizeBytes(payloadSizeBytes)
            .setPassed(ok)
            .setExitCode(run.exitCode)
            .setElapsedMs(elapsedMs)
            .setOutput(run.output.take(1000))
            .build()
    }

    private fun runGlobalIpProbe(
        network: Network,
        family: IpFamily,
        address: InetAddress,
        timeoutMs: Int,
    ): GlobalIpAddress {
        val endpoint = globalIpEndpoint(address)
        val result = GlobalIpAddress.newBuilder()
            .setFamily(family)
            .setEndpoint(endpoint)
        val started = System.nanoTime()
        logger.execEvent("probe.exec", listOf(
            "probe" to "global_ip",
            "family" to family.name,
            "endpoint" to endpoint,
            "timeout_ms" to timeoutMs,
        ))
        try {
            val socket = network.socketFactory.createSocket()
            socket.use {
                it.connect(InetSocketAddress(address, GLOBAL_IP_SERVICE_PORT), timeoutMs)
                it.soTimeout = timeoutMs
                val request = buildString {
                    append("GET $GLOBAL_IP_SERVICE_PATH HTTP/1.1\r\n")
                    append("Host: $GLOBAL_IP_SERVICE_HOST\r\n")
                    append("User-Agent: dropcheck-agent\r\n")
                    append("Accept: text/plain\r\n")
                    append("Connection: close\r\n")
                    append("\r\n")
                }
                it.getOutputStream().write(request.toByteArray(StandardCharsets.US_ASCII))
                it.getOutputStream().flush()
                val response = String(it.getInputStream().readBytes(), StandardCharsets.UTF_8)
                val parsed = parseHttpResponse(response)
                val publicIp = firstIpLiteralLine(parsed.body).ifBlank {
                    firstIpLiteralLine(response)
                }
                result.status = parsed.status
                result.ip = publicIp
                if (parsed.status != 200) {
                    result.error = "http_status=${parsed.status}"
                } else {
                    val parsedAddress = NetworkCheckPolicy.parseIpLiteral(publicIp)
                    when {
                        parsedAddress == null -> result.error = "invalid_ip_response"
                        !NetworkCheckPolicy.addressMatchesFamily(parsedAddress, family) -> result.error = "unexpected_ip_family"
                        else -> {
                            result.global = NetworkCheckPolicy.isGlobalUnicast(parsedAddress)
                            if (!result.global) result.error = "not_global_unicast"
                        }
                    }
                }
            }
        } catch (e: Exception) {
            result.error = e.toString()
        }
        result.elapsedMs = Duration.ofNanos(System.nanoTime() - started).toMillis()
        logger.debugEvent("network.global_ip.probe", listOf(
            "family" to family.name,
            "endpoint" to endpoint,
            "status" to result.status,
            "ip" to result.ip,
            "global" to result.global,
            "elapsed_ms" to result.elapsedMs,
            "error" to result.error,
        ))
        return result.build()
    }

    private fun firstIpLiteralLine(text: String): String {
        return text.lineSequence()
            .map { it.trim() }
            .firstOrNull { NetworkCheckPolicy.parseIpLiteral(it) != null }
            .orEmpty()
    }

    private fun parseHttpResponse(response: String): HttpResponse {
        val separator = when {
            "\r\n\r\n" in response -> "\r\n\r\n"
            "\n\n" in response -> "\n\n"
            else -> ""
        }
        val headerText: String
        val bodyText: String
        if (separator.isBlank()) {
            headerText = response
            bodyText = ""
        } else {
            val index = response.indexOf(separator)
            headerText = response.take(index)
            bodyText = response.drop(index + separator.length)
        }
        val headerLines = headerText.lineSequence().toList()
        val status = httpStatusRegex.find(headerLines.firstOrNull().orEmpty())
            ?.groupValues
            ?.getOrNull(1)
            ?.toIntOrNull() ?: 0
        val chunked = headerLines.any { it.startsWith("Transfer-Encoding:", ignoreCase = true) && it.contains("chunked", ignoreCase = true) }
        return HttpResponse(
            status = status,
            body = if (chunked) decodeChunkedBody(bodyText) else bodyText,
        )
    }

    private fun decodeChunkedBody(body: String): String {
        val out = StringBuilder()
        var cursor = 0
        while (cursor < body.length) {
            val lineEnd = body.indexOf("\r\n", cursor).takeIf { it >= 0 } ?: body.indexOf('\n', cursor)
            if (lineEnd < 0) break
            val sizeText = body.substring(cursor, lineEnd).substringBefore(";").trim()
            val size = sizeText.toIntOrNull(16) ?: break
            cursor = lineEnd + if (body.startsWith("\r\n", lineEnd)) 2 else 1
            if (size == 0) break
            if (cursor + size > body.length) break
            out.append(body.substring(cursor, cursor + size))
            cursor += size
            if (body.startsWith("\r\n", cursor)) {
                cursor += 2
            } else if (body.startsWith("\n", cursor)) {
                cursor += 1
            }
        }
        return out.toString()
    }

    private fun globalIpEndpoint(address: InetAddress): String {
        val host = if (address is Inet6Address) {
            "[${address.hostAddress.orEmpty()}]"
        } else {
            address.hostAddress.orEmpty()
        }
        return "http://$host$GLOBAL_IP_SERVICE_PATH"
    }

    private fun remainingTimeoutMs(timeoutMs: Int, startedAt: Long): Int {
        val elapsedMs = Duration.ofNanos(System.nanoTime() - startedAt).toMillis()
        return timeoutMs - elapsedMs.toInt()
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

    private data class HttpResponse(
        val status: Int,
        val body: String,
    )

    companion object {
        private const val PING_TRACE_HOP_TIMEOUT_MS = 2_000
        private const val PATH_MTU_PROBE_TIMEOUT_MS = 2_500
        private const val PROBE_SOURCE_ADDRESS_WAIT_MS = 5_000
        private const val PROBE_SOURCE_ADDRESS_POLL_MS = 250
        private const val GLOBAL_IP_SERVICE_HOST = "ifconfig.me"
        private const val GLOBAL_IP_SERVICE_PATH = "/ip"
        private const val GLOBAL_IP_SERVICE_PORT = 80
        private val httpStatusRegex = Regex("""^HTTP/\S+\s+(\d{3})""")
        private val pingReachedRegex = Regex(
            """(?im)(?:\d+\s+bytes\s+from|from)\s+([^\s:]+)(?:\s+\(([^)]+)\))?:.*time[=<]?(\d+(?:\.\d+)?)\s*ms""",
        )
        private val pingTtlExceededRegex = Regex(
            """(?im)^From\s+(.+?)(?::|\s+icmp_seq=).*Time to live exceeded""",
        )
        private val pingIdentityWithAddressRegex = Regex("""^(.+?)\s+\(([^)]+)\)$""")
    }
}
