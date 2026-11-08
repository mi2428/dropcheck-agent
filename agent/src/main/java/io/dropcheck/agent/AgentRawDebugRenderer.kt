package io.dropcheck.agent

import io.dropcheck.agent.grpc.CommandResult
import io.dropcheck.agent.grpc.MloLinkInfo
import io.dropcheck.agent.grpc.WifiConnection

/** A raw-only projection of existing RPC results, without another acquisition. */
internal object AgentRawDebugRenderer {
    fun render(command: AgentShellCommand.Execute, result: CommandResult): AgentShellResult {
        val parts = mutableListOf<AgentBlockPart>()
        fun raw(label: String, value: String) {
            if (value.isNotEmpty()) {
                parts += AgentBlockPart.Text(label, section = true)
                parts += AgentBlockPart.Text(value)
            }
        }
        fun links(label: String, values: List<MloLinkInfo>) {
            values.forEachIndexed { i, link -> raw("$label MloLink ${i + 1}", link.raw) }
        }
        fun wifi(label: String, connection: WifiConnection) {
            raw("$label WifiInfo", connection.raw)
            links("$label associated", connection.associatedMloLinksList)
            links("$label affiliated", connection.affiliatedMloLinksList)
        }
        val valid = when (command.rawView) {
            "wifi info" -> result.hasWifiStatus().also { present -> if (present) {
                wifi("Wi-Fi", result.wifiStatus.connection)
            } }
            "wifi scan" -> result.hasWifiScan().also { present -> if (present) {
                AgentWifiMloRenderer.MloFilter(command.scanSsid, command.scanBssid).scanResults(result.wifiScan.resultsList).forEachIndexed { i, ap ->
                    raw("ScanResult ${i + 1}", ap.raw)
                    links("ScanResult ${i + 1} affiliated", ap.affiliatedMloLinksList)
                }
            } }
            "network capabilities", "network link-properties", "network wifi-info" -> (result.hasNetworkSnapshot() &&
                result.networkSnapshot.networksCount == 1 && validNetworkId(command.request.getNetwork.networkId) &&
                result.networkSnapshot.getNetworks(0).networkId == command.request.getNetwork.networkId &&
                result.networkSnapshot.getNetworks(0).ipStatus.networkId == command.request.getNetwork.networkId).also { present -> if (present) {
                val network = result.networkSnapshot.getNetworks(0)
                if (command.rawView == "network capabilities") raw("Network ${network.networkId} NetworkCapabilities", network.ipStatus.rawCapabilities)
                else if (command.rawView == "network link-properties") raw("Network ${network.networkId} LinkProperties", network.ipStatus.rawLinkProperties)
                else wifi("Network ${network.networkId}", network.ipStatus.wifi)
            } }
            else -> false
        }
        if (!valid) parts.clear()
        val available = parts.isNotEmpty()
        val problem = if (!valid) "wrong, missing or inconsistent raw debug payload" else "requested raw snapshot unavailable"
        var evaluated = if (valid && available) evaluateShellPayload(command.request, result) else result.toBuilder()
            .setStatus(if (result.status == CommandResult.Status.STATUS_OK) CommandResult.Status.STATUS_FAILED else result.status)
            .setMessage(listOf(result.message, problem).filter(String::isNotEmpty).joinToString("; ")).build()
        if (evaluated.status == CommandResult.Status.STATUS_OK && result.hasNetworkSnapshot() && !validNetworkSnapshot(result.networkSnapshot, command.request.getNetwork)) {
            evaluated = result.toBuilder().setStatus(CommandResult.Status.STATUS_FAILED).setMessage("incomplete raw Network capture; retained data is reference").build()
        }
        if (result.hasWifiScan()) result.wifiScan.errorsList.forEach { parts += AgentBlockPart.Field("Acquisition error", it) }
        if (result.hasNetworkSnapshot()) result.networkSnapshot.errorsList.forEach { parts += AgentBlockPart.Field("Acquisition error", it) }
        val header = buildList {
            add(AgentBlockPart.Text("Raw Android ${command.rawView}  ${evaluated.status.name.removePrefix("STATUS_")}"))
            add(AgentBlockPart.Field("Source", "android"))
            add(AgentBlockPart.Field("Target", "self"))
            add(AgentBlockPart.Field("Latency", "${evaluated.elapsedMs}ms"))
            if (evaluated.message.isNotEmpty()) add(AgentBlockPart.Field("Message", evaluated.message))
        }
        val ok = evaluated.status == CommandResult.Status.STATUS_OK
        return AgentShellResult(ok, block = AgentPresentationBlock.create(header + parts, if (ok) AgentLogStyle.TEXT_COLOR else -44976))
    }
}
