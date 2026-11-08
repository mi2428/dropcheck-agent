package io.dropcheck.agent

import io.dropcheck.agent.grpc.WifiCapabilities

internal object AgentWifiCapabilitiesRenderer {
    fun presentation(capabilities: WifiCapabilities, detail: Boolean = false, detailAvailable: Boolean = false): List<AgentBlockPart> = buildList {
        add(AgentBlockPart.Field("Scope", "device"))
        val groups = listOf(
            Group("Band", listOf("2.4ghz", "5ghz", "6ghz", "60ghz"), capabilities.supportedBandsList, capabilities.unsupportedBandsList),
            Group("PHY", listOf("802.11n", "802.11ac", "802.11ax", "802.11be"), capabilities.supportedStandardsList, capabilities.unsupportedStandardsList),
            Group("Security", listOf("wpa2_psk", "wpa3_sae", "wpa2_wpa3_personal", "owe"), capabilities.supportedSecurityModesList, capabilities.unsupportedSecurityModesList),
            Group("Features", listOf("mlo"), capabilities.supportedFeaturesList, capabilities.unsupportedFeaturesList),
        )
        groups.forEach { group ->
            fun label(value: String): String = when (group.label) {
                "Band" -> AgentWifiScanRenderer.band(value)
                "PHY" -> AgentWifiScanRenderer.phy(value)
                "Security" -> AgentWifiScanRenderer.security(listOf(value))
                else -> value
            }
            val supported = group.supported.map(::label)
            val unsupported = group.unsupported.map(::label)
            val rows = (group.known.map(::label) + supported + unsupported).distinct().map { value ->
                val support = when {
                    value in supported && value in unsupported -> "? (conflicting lists)"
                    value in supported -> "yes"
                    value in unsupported -> "no"
                    else -> "?"
                }
                listOf(value, support)
            }
            add(AgentBlockPart.Table(listOf(AgentBlockPart.Column(group.label), AgentBlockPart.Column("Support")), rows))
        }
        if (detail || !detailAvailable) {
            if (capabilities.fieldsCount > 0) add(AgentBlockPart.Text("Fields", section = true))
            capabilities.fieldsList.forEach { add(AgentBlockPart.Field(it.key, it.value)) }
        }
        if (capabilities.errorsCount > 0) add(AgentBlockPart.Text("Errors", section = true))
        capabilities.errorsList.forEach { add(AgentBlockPart.Field("Error", it)) }
        if (detailAvailable && !detail) add(AgentBlockPart.Field("More", "show wifi capabilities detail"))
    }

    private data class Group(val label: String, val known: List<String>, val supported: List<String>, val unsupported: List<String>)
}
