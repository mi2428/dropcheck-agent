package io.dropcheck.agent

import io.dropcheck.agent.grpc.WifiDiagnostics

internal object AgentWifiDiagnosticsRenderer {
    fun presentation(diagnostics: WifiDiagnostics, detail: Boolean = false, detailAvailable: Boolean = false, ipAvailable: Boolean = false): List<AgentBlockPart> = buildList {
        fun heading(title: String) {
            if (isNotEmpty()) add(AgentBlockPart.Text(""))
            add(AgentBlockPart.Text(title))
        }
        heading("Summary")
        add(AgentBlockPart.Field("Constituent status", "? (shared acquisition-result binding required); not an atomic snapshot"))
        heading("Wi-Fi")
        if (diagnostics.hasStatus()) addAll(AgentWifiStatusRenderer.presentation(diagnostics.status, detail, detailAvailable, ipAvailable, includeIpSnapshot = false)) else add(AgentBlockPart.Field("Note", "? (Wi-Fi payload unavailable)"))
        heading("IP")
        if (diagnostics.status.hasIpStatus()) addAll(AgentWifiStatusRenderer.ipPresentation(diagnostics.status.ipStatus, detail, detailAvailable)) else add(AgentBlockPart.Field("Note", "? (IP payload unavailable)"))
        heading("Scan summary")
        if (diagnostics.hasScan()) {
            addAll(AgentWifiScanRenderer.presentation(diagnostics.scan, AgentWifiScanContext(requestedFresh = false, detail = true)))
        } else add(AgentBlockPart.Field("Note", "? (scan payload unavailable)"))
        heading("Device")
        if (diagnostics.hasCapabilities()) addAll(AgentWifiCapabilitiesRenderer.presentation(diagnostics.capabilities, detail, detailAvailable)) else add(AgentBlockPart.Field("Note", "? (device payload unavailable)"))
        if (detail || !detailAvailable) diagnostics.networksList.forEach {
            add(AgentBlockPart.Field("Active/default at bundle enumeration", it.active.toString()))
            if (it.hasIpStatus()) addAll(AgentWifiStatusRenderer.ipPresentation(it.ipStatus, detail, detailAvailable))
        }
        heading("EHT/MLO interpretation (references the observations above)")
        addAll(AgentWifiMloRenderer.interpretation(diagnostics.status, diagnostics.scan, AgentWifiMloContext(wifiCapabilities = diagnostics.capabilities)))
    }
}
