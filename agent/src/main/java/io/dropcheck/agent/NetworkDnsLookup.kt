package io.dropcheck.agent

import android.net.DnsResolver
import android.net.Network
import android.os.CancellationSignal
import io.dropcheck.agent.grpc.DnsRecordType
import java.net.InetAddress
import java.net.SocketTimeoutException
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executor
import java.util.concurrent.TimeUnit

internal data class DnsLookup(
    val qtype: DnsRecordType,
    val addresses: List<InetAddress> = emptyList(),
    val error: String = "",
)

internal fun awaitDnsAnswer(latch: CountDownLatch, deadlineNanos: Long, cancel: () -> Unit): Boolean {
    var completed = false
    try {
        completed = latch.await((deadlineNanos - System.nanoTime()).coerceAtLeast(0), TimeUnit.NANOSECONDS)
        return completed
    } catch (e: InterruptedException) {
        Thread.currentThread().interrupt()
        throw e
    } finally {
        if (!completed) cancel()
    }
}

@Suppress("DEPRECATION")
internal fun resolveDnsQtype(
    network: Network,
    name: String,
    qtype: DnsRecordType,
    deadlineNanos: Long,
    logger: CommandLogger,
): DnsLookup {
    val resolverType = when (qtype) {
        DnsRecordType.DNS_RECORD_TYPE_A -> DnsResolver.TYPE_A
        DnsRecordType.DNS_RECORD_TYPE_AAAA -> DnsResolver.TYPE_AAAA
        else -> return DnsLookup(qtype, error = "unsupported_qtype")
    }
    throwIfInterrupted()
    val timeoutMs = try {
        probeTimeoutMs(deadlineNanos)
    } catch (e: SocketTimeoutException) {
        return DnsLookup(qtype, error = "dns_deadline_exceeded")
    }
    val latch = CountDownLatch(1)
    val cancellation = CancellationSignal()
    val executor = Executor { command -> command.run() }
    var addresses = emptyList<InetAddress>()
    var error = ""
    DnsResolver.getInstance().query(
        network,
        name,
        resolverType,
        DnsResolver.FLAG_EMPTY,
        executor,
        cancellation,
        object : DnsResolver.Callback<List<InetAddress>> {
            override fun onAnswer(answer: List<InetAddress>, rcode: Int) {
                addresses = answer
                if (rcode != 0) {
                    error = "dns_rcode=$rcode"
                }
                latch.countDown()
            }

            override fun onError(e: DnsResolver.DnsException) {
                error = "dns_error=${e.code}:${e.message.orEmpty()}"
                latch.countDown()
            }
        },
    )
    val completed = awaitDnsAnswer(latch, deadlineNanos) { cancellation.cancel() }
    if (!completed) {
        return DnsLookup(qtype, error = "dns_timeout=${timeoutMs}ms")
    }
    logger.debug("dns qtype result name=$name qtype=${qtype.name} count=${addresses.size} values=${addresses.joinToString(",") { it.hostAddress.orEmpty() }} error=${error.ifBlank { "none" }}")
    return DnsLookup(qtype, addresses, error)
}
