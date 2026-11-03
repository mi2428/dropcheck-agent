package io.dropcheck.agent

import io.dropcheck.agent.grpc.GetNetwork
import io.dropcheck.agent.grpc.NetworkSnapshot

internal fun validNetworkId(value: String): Boolean = value.toIntOrNull()?.let { it > 0 && it.toString() == value } == true

/** An OK acquisition must describe exactly the requested subject without partial errors. */
internal fun validNetworkSnapshot(snapshot: NetworkSnapshot, query: GetNetwork?): Boolean {
    if (snapshot.observedAtUnixMs <= 0 || snapshot.errorsCount != 0 || query != null && snapshot.networksCount != 1) return false
    val seen = mutableSetOf<String>()
    for (network in snapshot.networksList) {
        val id = network.networkId
        if (!validNetworkId(id) || !seen.add(id) || !network.hasIpStatus() || network.ipStatus.networkId != id) return false
        if (query != null && (if (query.defaultNetwork) !network.active else query.networkId != id)) return false
        if (listOf("capabilities", "link_properties").any { !AgentObservationPresentation.available(network.ipStatus.observationFieldsList, it) }) return false
    }
    return snapshot.networksList.count { it.active } <= 1
}

/** Explicit selection fails rather than silently switching to another Network. */
internal fun snapshotNetworkIds(all: List<String>, active: String?, query: GetNetwork?): List<String> {
    if (query == null) return all.distinct().sortedBy { it.toLongOrNull() ?: Long.MAX_VALUE }
    require(query.defaultNetwork != query.networkId.isNotEmpty()) { "specify exactly one of default Network or a canonical Network ID" }
    require(query.defaultNetwork || validNetworkId(query.networkId)) { "invalid Android Network ID" }
    val id = if (query.defaultNetwork) active ?: throw IllegalStateException("no default Android Network") else query.networkId
    check(id in all) { "requested Android Network is no longer available" }
    return listOf(id)
}
