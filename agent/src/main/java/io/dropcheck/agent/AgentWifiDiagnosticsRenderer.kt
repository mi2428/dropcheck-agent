package io.dropcheck.agent

import io.dropcheck.agent.grpc.WifiDiagnostics

internal object AgentWifiDiagnosticsRenderer {
    fun presentation(diagnostics: WifiDiagnostics, detail: Boolean = false, detailAvailable: Boolean = false, ipAvailable: Boolean = false): List<AgentBlockPart> = buildList {
        fun heading(title: String) {
            add(AgentBlockPart.Text(title, section = true))
        }
        heading("Summary")
        addAll(nestedPresentation(listOf(AgentBlockPart.Field("Constituent status", "? (shared acquisition-result binding required); not an atomic snapshot"))))
        heading("Wi-Fi")
        addAll(nestedPresentation(if (diagnostics.hasStatus()) AgentWifiStatusRenderer.presentation(diagnostics.status, detail, detailAvailable, ipAvailable, includeIpSnapshot = false) else listOf(AgentBlockPart.Field("Note", "? (Wi-Fi payload unavailable)"))))
        heading("IP")
        addAll(nestedPresentation(if (diagnostics.status.hasIpStatus()) AgentWifiStatusRenderer.ipPresentation(diagnostics.status.ipStatus, detail, detailAvailable) else listOf(AgentBlockPart.Field("Note", "? (IP payload unavailable)"))))
        heading("Scan summary")
        addAll(nestedPresentation(if (diagnostics.hasScan()) AgentWifiScanRenderer.presentation(diagnostics.scan, AgentWifiScanContext(requestedFresh = false, detail = true)) else listOf(AgentBlockPart.Field("Note", "? (scan payload unavailable)"))))
        heading("Device")
        addAll(nestedPresentation(if (diagnostics.hasCapabilities()) AgentWifiCapabilitiesRenderer.presentation(diagnostics.capabilities, detail, detailAvailable) else listOf(AgentBlockPart.Field("Note", "? (device payload unavailable)"))))
        if (detail || !detailAvailable) diagnostics.networksList.forEachIndexed { index, network ->
            heading("Network ${index + 1}")
            val parts = buildList {
                add(AgentBlockPart.Field("Active/default at bundle enumeration", network.active.toString()))
                if (network.hasIpStatus()) addAll(AgentWifiStatusRenderer.ipPresentation(network.ipStatus, detail, detailAvailable))
            }
            addAll(nestedPresentation(parts))
        }
        heading("EHT/MLO interpretation (references the observations above)")
        addAll(nestedPresentation(AgentWifiMloRenderer.interpretation(diagnostics.status, diagnostics.scan, AgentWifiMloContext(wifiCapabilities = diagnostics.capabilities))))
    }
}
