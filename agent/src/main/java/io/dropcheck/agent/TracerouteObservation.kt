package io.dropcheck.agent

import io.dropcheck.agent.grpc.TracerouteHop

internal data class TracerouteObservation(
    val hops: List<TracerouteHop>,
    val reached: Boolean?,
    val error: String = "",
)

/** Parse the complete native result at collection, before display truncation. */
internal fun tracerouteObservation(output: String, destination: String): TracerouteObservation {
    val hopLine = Regex("^\\s*(\\d+)\\s+(.+)$")
    val rtt = Regex("(?:time=)?(\\d+(?:\\.\\d+)?)\\s*ms", RegexOption.IGNORE_CASE)
    val hops = output.lineSequence().mapNotNull { line ->
        val match = hopLine.matchEntire(line) ?: return@mapNotNull null
        val index = match.groupValues[1].toIntOrNull()?.takeIf { it > 0 } ?: return@mapNotNull null
        val body = match.groupValues[2]
        if (body.startsWith("/") || body.startsWith("traceroute", ignoreCase = true)) return@mapNotNull null
        val addresses = mutableListOf<String>()
        val hostnames = mutableListOf<String>()
        val identity = rtt.replace(body, " ").replace(Regex("!\\S+"), " ")
        identity.split(Regex("[\\s(),\\[\\]]+")).filter { it.isNotBlank() && it != "*" }.forEach { token ->
            val address = NetworkCheckPolicy.parseIpLiteral(token)
            if (address != null) {
                addresses += address.hostAddress.orEmpty()
            } else if (token.any { it.isLetter() } && token.all { it.isLetterOrDigit() || it in ".-_" }) {
                hostnames += token
            }
        }
        TracerouteHop.newBuilder()
            .setIndex(index)
            .addAllAddresses(addresses.distinct())
            .addAllHostnames(hostnames.distinct())
            .addAllRttMs(rtt.findAll(body).mapNotNull { it.groupValues[1].toDoubleOrNull() }.toList())
            .setTimedOut(addresses.isEmpty() && hostnames.isEmpty())
            .build()
    }.toList()
    if (hops.isEmpty()) return TracerouteObservation(emptyList(), null, "native hop observations unavailable")
    val target = NetworkCheckPolicy.parseIpLiteral(destination)?.hostAddress ?: destination
    return TracerouteObservation(hops, hops.any { target in it.addressesList || target in it.hostnamesList })
}
