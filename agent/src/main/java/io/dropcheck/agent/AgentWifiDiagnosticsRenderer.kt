package io.dropcheck.agent

import io.dropcheck.agent.grpc.WifiDiagnostics

internal object AgentWifiDiagnosticsRenderer {
    fun presentation(diagnostics: WifiDiagnostics, detail: Boolean = false, detailAvailable: Boolean = false, ipAvailable: Boolean = false): List<AgentBlockPart> = buildList {
        add(AgentBlockPart.Text("Summary"))
        add(AgentBlockPart.Text("Constituent status: ? (shared acquisition-result binding required); not an atomic snapshot"))
        diagnostics.scan.errorsList.forEach { add(AgentBlockPart.Text("Scan note: $it")) }
        diagnostics.capabilities.errorsList.forEach { add(AgentBlockPart.Text("Device note: $it")) }
        add(AgentBlockPart.Text("Wi-Fi"))
        if (diagnostics.hasStatus()) addAll(AgentWifiStatusRenderer.presentation(diagnostics.status, detail, detailAvailable, ipAvailable, includeIpSnapshot = false)) else add(AgentBlockPart.Text("Note: ? (Wi-Fi payload unavailable)"))
        add(AgentBlockPart.Text("IP"))
        if (diagnostics.status.hasIpStatus()) addAll(AgentWifiStatusRenderer.ipPresentation(diagnostics.status.ipStatus, detail, detailAvailable)) else add(AgentBlockPart.Text("Note: ? (IP payload unavailable)"))
        add(AgentBlockPart.Text("Scan summary"))
        if (diagnostics.hasScan()) {
            val fields = diagnostics.scan.fieldsList.associate { it.key to it.value }
            add(AgentBlockPart.Text("Data source: ${fields["scan_source"] ?: "?"}"))
            add(AgentBlockPart.Text("Updated: ${fields["fresh_scan_results_updated"] ?: "?"}"))
            add(AgentBlockPart.Text("APs: ${diagnostics.scan.resultsCount}"))
        } else add(AgentBlockPart.Text("Note: ? (scan payload unavailable)"))
        add(AgentBlockPart.Text("Device"))
        if (diagnostics.hasCapabilities()) addAll(AgentWifiCapabilitiesRenderer.presentation(diagnostics.capabilities, detail, detailAvailable)) else add(AgentBlockPart.Text("Note: ? (device payload unavailable)"))
        if (detail || !detailAvailable) diagnostics.networksList.forEach {
            add(AgentBlockPart.Text("Network: ${it.networkId}"))
            if (it.hasIpStatus()) addAll(AgentWifiStatusRenderer.ipPresentation(it.ipStatus, detail, detailAvailable))
        }
    }
}
