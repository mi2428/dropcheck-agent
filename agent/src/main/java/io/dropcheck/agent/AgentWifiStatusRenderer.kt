package io.dropcheck.agent

import io.dropcheck.agent.grpc.IpStatus
import io.dropcheck.agent.grpc.MloLinkInfo
import io.dropcheck.agent.grpc.WifiConnection
import io.dropcheck.agent.grpc.WifiStatus

/** Text renderer used by the on-device shell for `show wifi status`. */
internal object AgentWifiStatusRenderer {
    fun render(status: WifiStatus): List<String> {
        val out = mutableListOf<String>()
        section(out, "Wi-Fi")
        kv(out,
            "enabled" to status.enabled.toString(),
            "state" to empty(status.state, "unknown"),
            "active" to empty(status.activeNetwork, "none"),
            "networks" to status.wifiNetworkCount.toString(),
        )
        if (status.permissionsList.isNotEmpty()) renderPermissions(out, status.permissionsList)
        if (status.hasConnection() && status.connection.ssid.isNotBlank()) renderConnection(out, status.connection)
        if (status.hasIpStatus()) renderIPStatus(out, status.ipStatus)
        return out
    }

    private fun renderPermissions(out: MutableList<String>, permissions: List<String>) {
        section(out, "Permissions")
        permissions.forEach { permission ->
            val parts = permission.split("=", limit = 2)
            if (parts.size == 2) {
                out += "  ${parts[0]} ${parts[1]}"
            } else {
                out += "  $permission -"
            }
        }
    }

    private fun renderConnection(out: MutableList<String>, conn: WifiConnection) {
        section(out, "Connection")
        kv(out,
            "ssid" to conn.ssid,
            "bssid" to empty(conn.bssid, "unknown"),
            "rssi" to "${conn.rssiDbm}dBm",
            "security" to empty(conn.securityType, "unknown"),
            "band" to wifiBandFromFrequency(conn.frequencyMhz),
            "channel" to wifiChannelFromFrequency(conn.frequencyMhz),
            "frequency" to "${conn.frequencyMhz}MHz",
            "bandwidth" to empty(formatWifiChannelWidth(conn.channelWidth), "unknown"),
            "link" to "${conn.linkSpeedMbps}Mbps",
            "ip" to empty(conn.ipv4Address, "none"),
        )
        if (conn.wifiStandard.isNotBlank() || conn.supplicantState.isNotBlank() || conn.detailedState.isNotBlank()) {
            section(out, "Connection State")
            kv(out,
                "supplicant" to empty(conn.supplicantState, "unknown"),
                "detailed" to empty(conn.detailedState, "unknown"),
                "standard" to empty(conn.wifiStandard, "unknown"),
                "signal" to "${conn.signalLevel}/${conn.maxSignalLevel}",
            )
        }
        renderMLO(out, conn)
    }

    private fun renderMLO(out: MutableList<String>, conn: WifiConnection) {
        val present = wifiConnectionHasMLO(conn)
        section(out, "MLO")
        kv(out,
            "present" to present.toString(),
            "ap_mld" to empty(conn.apMldMacAddress, "<none>"),
            "ap_link_id" to mloLinkID(conn.apMloLinkId, present),
            "affiliated" to conn.affiliatedMloLinksCount.toString(),
            "associated" to conn.associatedMloLinksCount.toString(),
        )
        renderMLOLinks(out, "Affiliated MLO Links", conn.affiliatedMloLinksList)
        renderMLOLinks(out, "Associated MLO Links", conn.associatedMloLinksList)
    }

    private fun wifiConnectionHasMLO(conn: WifiConnection): Boolean {
        return conn.apMldMacAddress.isNotBlank() ||
            conn.affiliatedMloLinksCount > 0 ||
            conn.associatedMloLinksCount > 0 ||
            (conn.wifiStandard.equals("802.11be", ignoreCase = true) && conn.apMloLinkId >= 0)
    }

    private fun renderMLOLinks(out: MutableList<String>, title: String, links: List<MloLinkInfo>) {
        if (links.isEmpty()) return
        section(out, title)
        table(out,
            listOf("ID", "STATE", "BAND", "CHANNEL", "RSSI", "TX", "RX", "AP_MAC", "STA_MAC"),
            links.map { link ->
                listOf(
                    link.linkId.toString(),
                    empty(link.state, "unknown"),
                    empty(link.band, "unknown"),
                    link.channel.toString(),
                    link.rssiDbm.toString(),
                    link.txLinkSpeedMbps.toString(),
                    link.rxLinkSpeedMbps.toString(),
                    empty(link.apMacAddress, "unknown"),
                    empty(link.staMacAddress, "unknown"),
                )
            },
        )
    }

    private fun renderIPStatus(out: MutableList<String>, status: IpStatus) {
        section(out, "Network")
        kv(out,
            "id" to empty(status.networkId, "unknown"),
            "transports" to status.transportsList.joinToString(","),
            "validated" to status.validated.toString(),
            "internet" to status.internet.toString(),
            "interface" to empty(status.interfaceName, "none"),
            "mtu" to status.mtu.toString(),
        )
        listSection(out, "Addresses", status.addressesList)
        listSection(out, "DNS", status.dnsServersList)
        if (status.dhcpServer.isNotBlank()) {
            section(out, "DHCP")
            kv(out, "server" to status.dhcpServer)
        }
        if (status.privateDnsActive || status.privateDnsServerName.isNotBlank()) {
            section(out, "Private DNS")
            kv(out,
                "active" to status.privateDnsActive.toString(),
                "server" to empty(status.privateDnsServerName, "none"),
            )
        }
    }

    private fun section(out: MutableList<String>, title: String) {
        if (out.isNotEmpty() && out.last().isNotBlank()) out += ""
        out += title
    }

    private fun kv(out: MutableList<String>, vararg rows: Pair<String, String>) {
        val filtered = rows.filter { it.first.isNotBlank() && it.second.isNotBlank() }
        val width = filtered.maxOfOrNull { it.first.length } ?: 0
        filtered.forEach { (key, value) ->
            out += "  ${key.padEnd(width)}  $value"
        }
    }

    private fun listSection(out: MutableList<String>, title: String, values: List<String>) {
        if (values.isEmpty()) return
        section(out, title)
        values.forEach { out += "  $it" }
    }

    private fun table(out: MutableList<String>, headers: List<String>, rows: List<List<String>>) {
        val widths = headers.indices.map { index ->
            (listOf(headers[index]) + rows.map { it.getOrElse(index) { "" } }).maxOf { it.length }
        }
        out += headers.mapIndexed { index, value -> value.padEnd(widths[index]) }.joinToString("  ").trimEnd()
        rows.forEach { row ->
            out += headers.indices
                .map { index -> row.getOrElse(index) { "" }.padEnd(widths[index]) }
                .joinToString("  ")
                .trimEnd()
        }
    }

    private fun empty(value: String, fallback: String): String = value.ifBlank { fallback }

    private fun mloLinkID(id: Int, present: Boolean): String {
        return if (id < 0 || (!present && id == 0)) "<none>" else id.toString()
    }

    private fun wifiBandFromFrequency(freq: Int): String = when (freq) {
        in 2400 until 2500 -> "2.4ghz"
        in 4900 until 5900 -> "5ghz"
        in 5925 until 7125 -> "6ghz"
        in 57000 until 71000 -> "60ghz"
        else -> "unknown"
    }

    private fun wifiChannelFromFrequency(freq: Int): String {
        val channel = when {
            freq == 2484 -> 14
            freq in 2412..2472 -> (freq - 2407) / 5
            freq in 5000..5895 -> (freq - 5000) / 5
            freq in 5955..7115 -> (freq - 5950) / 5
            else -> 0
        }
        return if (channel == 0) "unknown" else channel.toString()
    }

    private fun formatWifiChannelWidth(value: String): String {
        val trimmed = value.trim().trim(',', ';')
        if (trimmed.isBlank()) return ""
        var normalized = trimmed.lowercase()
        normalized = normalized.removePrefix("channel_width_")
        normalized = normalized.removePrefix("width_")
        normalized = normalized.replace("_", "").replace(" ", "")
        val core = normalized.removeSuffix("mhz")
        return if (core.matches(Regex("^[0-9+]+$"))) "${core}MHz" else trimmed
    }
}
