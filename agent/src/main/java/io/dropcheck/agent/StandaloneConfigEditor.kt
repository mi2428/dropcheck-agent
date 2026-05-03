package io.dropcheck.agent

import io.dropcheck.agent.grpc.ConnectWifi
import io.dropcheck.agent.grpc.DnsRecordType
import io.dropcheck.agent.grpc.StandaloneConfig
import io.dropcheck.agent.grpc.StandaloneEdit
import io.dropcheck.agent.grpc.StandaloneFesta
import io.dropcheck.agent.grpc.StandaloneWifiGroup
import io.dropcheck.agent.grpc.WifiBand

/** Applies controller-provided standalone config edits to the persisted config tree. */
internal object StandaloneConfigEditor {
    /** Result of applying a batch of standalone config edits. */
    data class Result(
        val config: StandaloneConfig,
        val error: String? = null,
    )

    /** Returns the updated config, or the partially updated config with an error message. */
    fun apply(config: StandaloneConfig, edits: List<StandaloneEdit>): Result {
        var builder = config.toBuilder()
        for (edit in edits) {
            val applied = when (edit.action) {
                StandaloneEdit.Action.ACTION_SET -> set(builder, edit.pathList, edit.value)
                StandaloneEdit.Action.ACTION_DELETE -> delete(builder, edit.pathList)
                else -> "unsupported standalone edit action: ${edit.action}"
            }
            if (applied != null) return Result(builder.build(), applied)
            builder = normalize(builder.build()).toBuilder()
        }
        return Result(normalize(builder.build()))
    }

    private fun normalize(config: StandaloneConfig): StandaloneConfig {
        val builder = config.toBuilder()
        val festas = builder.festasList
            .filter { it.name.isNotBlank() }
            .sortedBy { it.name }
        builder.clearFestas()
        builder.addAllFestas(festas.map { festa ->
            val festaBuilder = festa.toBuilder()
            val groups = festaBuilder.wifiGroupsList
                .filter { it.name.isNotBlank() }
                .sortedBy { it.name }
            festaBuilder.clearWifiGroups()
            festaBuilder.addAllWifiGroups(groups)
            festaBuilder.build()
        })
        return builder.build()
    }

    private fun set(builder: StandaloneConfig.Builder, path: List<String>, value: String): String? {
        return when {
            path == listOf("enabled") -> applyParsed(parseBool(value), "enabled must be true or false") { builder.enabled = it }
            path == listOf("retention_ms") -> applyParsed(parseUInt(value), "retention_ms must be uint32") { builder.retentionMs = it }
            path == listOf("max_bytes") -> applyParsed(parseULong(value), "max_bytes must be uint64") { builder.maxBytes = it }
            path.size >= 2 && path[0] == "upload" -> setUpload(builder, path.drop(1), value)
            path.size >= 3 && path[0] == "festa" -> setFesta(builder, path.drop(1), value)
            else -> "unsupported standalone set path: ${path.joinToString(" ")}"
        }
    }

    private fun delete(builder: StandaloneConfig.Builder, path: List<String>): String? {
        return when {
            path == listOf("standalone") -> {
                builder.clear()
                null
            }
            path == listOf("enabled") -> {
                builder.enabled = false
                null
            }
            path == listOf("retention_ms") -> {
                builder.retentionMs = 0
                null
            }
            path == listOf("max_bytes") -> {
                builder.maxBytes = 0
                null
            }
            path.size >= 1 && path[0] == "upload" -> deleteUpload(builder, path.drop(1))
            path.size >= 2 && path[0] == "festa" -> deleteFesta(builder, path.drop(1))
            else -> "unsupported standalone delete path: ${path.joinToString(" ")}"
        }
    }

    private fun setUpload(config: StandaloneConfig.Builder, path: List<String>, value: String): String? {
        val upload = config.upload.toBuilder()
        val error = when {
            path == listOf("url") -> {
                upload.url = value
                null
            }
            path.size == 2 && path[0] == "wifi" -> {
                val wifi = upload.wifi.toBuilder()
                val wifiError = setUploadWifi(wifi, path[1], value)
                if (wifiError != null) return wifiError
                upload.wifi = wifi.build()
                null
            }
            else -> "unsupported standalone upload set path: ${path.joinToString(" ")}"
        }
        if (error != null) return error
        config.upload = upload.build()
        return null
    }

    private fun deleteUpload(config: StandaloneConfig.Builder, path: List<String>): String? {
        if (path.isEmpty()) {
            config.clearUpload()
            return null
        }
        val upload = config.upload.toBuilder()
        when (path) {
            listOf("url") -> upload.clearUrl()
            listOf("wifi") -> upload.clearWifi()
            else -> return "unsupported standalone upload delete path: ${path.joinToString(" ")}"
        }
        config.upload = upload.build()
        return null
    }

    private fun setUploadWifi(wifi: ConnectWifi.Builder, field: String, value: String): String? {
        return when (field) {
            "ssid" -> {
                wifi.ssid = value
                null
            }
            "passphrase" -> {
                wifi.passphrase = value
                null
            }
            "security" -> applyParsed(parseSecurity(value), "unsupported wifi security: $value") { wifi.security = it }
            "bssid" -> {
                wifi.bssid = value
                null
            }
            "band" -> applyParsed(parseBand(value), "unsupported wifi band: $value") { wifi.band = it }
            "mac_randomization" -> applyParsed(parseMacRandomization(value), "unsupported wifi MAC randomization: $value") { wifi.macRandomization = it }
            "timeout_ms" -> applyParsed(parseUInt(value), "upload wifi timeout_ms must be uint32") { wifi.timeoutMs = it }
            else -> "unsupported standalone upload wifi set path: $field"
        }
    }

    private fun setFesta(config: StandaloneConfig.Builder, path: List<String>, value: String): String? {
        val name = path[0]
        if (name.isBlank()) return "festa name is required"
        val index = festaIndex(config, name)
        val festa = if (index >= 0) config.getFestas(index).toBuilder() else StandaloneFesta.newBuilder().setName(name)
        val error = when {
            path.size == 2 && path[1] == "enabled" -> applyParsed(parseBool(value), "festa enabled must be true or false") { festa.enabled = it }
            path.size == 2 && path[1] == "interval_ms" -> applyParsed(parseUInt(value), "festa interval_ms must be uint32") { festa.intervalMs = it }
            path.size >= 4 && path[1] == "wifi-group" -> setWifiGroup(festa, path.drop(2), value)
            path.size >= 4 && path[1] == "check" -> setCheck(festa, path.drop(2), value)
            else -> "unsupported standalone festa set path: ${path.joinToString(" ")}"
        }
        if (error != null) return error
        if (index >= 0) config.setFestas(index, festa) else config.addFestas(festa)
        return null
    }

    private fun deleteFesta(config: StandaloneConfig.Builder, path: List<String>): String? {
        val name = path[0]
        val index = festaIndex(config, name)
        if (index < 0) return null
        if (path.size == 1) {
            config.removeFestas(index)
            return null
        }
        val festa = config.getFestas(index).toBuilder()
        val error = when {
            path.size == 3 && path[1] == "wifi-group" -> {
                val groupIndex = wifiGroupIndex(festa, path[2])
                if (groupIndex >= 0) festa.removeWifiGroups(groupIndex)
                null
            }
            path.size == 3 && path[1] == "check" -> {
                when (path[2]) {
                    "dns" -> festa.checks = festa.checks.toBuilder().clearDns().build()
                    "ping" -> festa.checks = festa.checks.toBuilder().clearPing().build()
                    "http" -> festa.checks = festa.checks.toBuilder().clearHttp().build()
                    else -> return "unsupported standalone check: ${path[2]}"
                }
                null
            }
            else -> "unsupported standalone festa delete path: ${path.joinToString(" ")}"
        }
        if (error != null) return error
        config.setFestas(index, festa)
        return null
    }

    private fun setWifiGroup(festa: StandaloneFesta.Builder, path: List<String>, value: String): String? {
        val name = path[0]
        if (name.isBlank()) return "wifi-group name is required"
        val index = wifiGroupIndex(festa, name)
        val group = if (index >= 0) festa.getWifiGroups(index).toBuilder() else StandaloneWifiGroup.newBuilder().setName(name)
        val error = when {
            path == listOf(name, "match", "essid") -> {
                group.essid = value
                null
            }
            path == listOf(name, "match", "bssid") -> {
                group.bssid = value
                null
            }
            path == listOf(name, "credential", "passphrase") -> {
                group.passphrase = value
                null
            }
            path == listOf(name, "security") -> {
                applyParsed(parseSecurity(value), "unsupported wifi security: $value") { group.security = it }
            }
            path == listOf(name, "band") -> {
                applyParsed(parseBand(value), "unsupported wifi band: $value") { group.band = it }
            }
            path == listOf(name, "wait", "ip") -> {
                group.requireIp = parseBool(value) ?: true
                null
            }
            path == listOf(name, "wait", "validated") -> {
                group.requireValidated = parseBool(value) ?: true
                null
            }
            path == listOf(name, "timeout_ms") -> applyParsed(parseUInt(value), "wifi-group timeout_ms must be uint32") { group.timeoutMs = it }
            else -> "unsupported standalone wifi-group set path: ${path.joinToString(" ")}"
        }
        if (error != null) return error
        if (index >= 0) festa.setWifiGroups(index, group) else festa.addWifiGroups(group)
        return null
    }

    private fun setCheck(festa: StandaloneFesta.Builder, path: List<String>, value: String): String? {
        val checks = festa.checks.toBuilder()
        val error = when (path[0]) {
            "dns" -> {
                val dns = checks.dns.toBuilder()
                when (path.getOrNull(1)) {
                    "enabled" -> dns.enabled = parseBool(value) ?: return "dns enabled must be true or false"
                    "name" -> dns.name = value
                    "qtypes" -> dns.clearQtypes().addAllQtypes(parseQTypes(value))
                    "timeout_ms" -> dns.timeoutMs = parseUInt(value) ?: return "dns timeout_ms must be uint32"
                    else -> return "unsupported dns check path: ${path.joinToString(" ")}"
                }
                checks.dns = dns.build()
                null
            }
            "ping" -> {
                val ping = checks.ping.toBuilder()
                when (path.getOrNull(1)) {
                    "enabled" -> ping.enabled = parseBool(value) ?: return "ping enabled must be true or false"
                    "host" -> ping.host = value
                    "count" -> ping.count = parseUInt(value) ?: return "ping count must be uint32"
                    "timeout_ms" -> ping.timeoutMs = parseUInt(value) ?: return "ping timeout_ms must be uint32"
                    "size_bytes" -> ping.sizeBytes = parseUInt(value) ?: return "ping size_bytes must be uint32"
                    else -> return "unsupported ping check path: ${path.joinToString(" ")}"
                }
                checks.ping = ping.build()
                null
            }
            "http" -> {
                val http = checks.http.toBuilder()
                when (path.getOrNull(1)) {
                    "enabled" -> http.enabled = parseBool(value) ?: return "http enabled must be true or false"
                    "url" -> http.url = value
                    "expected_status" -> http.expectedStatus = parseUInt(value) ?: return "http expected_status must be uint32"
                    "timeout_ms" -> http.timeoutMs = parseUInt(value) ?: return "http timeout_ms must be uint32"
                    else -> return "unsupported http check path: ${path.joinToString(" ")}"
                }
                checks.http = http.build()
                null
            }
            else -> "unsupported standalone check: ${path[0]}"
        }
        if (error != null) return error
        festa.checks = checks.build()
        return null
    }

    private fun festaIndex(config: StandaloneConfig.Builder, name: String): Int {
        return config.festasList.indexOfFirst { it.name == name }
    }

    private fun wifiGroupIndex(festa: StandaloneFesta.Builder, name: String): Int {
        return festa.wifiGroupsList.indexOfFirst { it.name == name }
    }

    private inline fun <T> applyParsed(value: T?, error: String, apply: (T) -> Unit): String? {
        if (value == null) return error
        apply(value)
        return null
    }

    private fun parseBool(value: String): Boolean? {
        return when (value.lowercase()) {
            "true", "1", "yes", "on", "enabled" -> true
            "false", "0", "no", "off", "disabled" -> false
            else -> null
        }
    }

    private fun parseUInt(value: String): Int? {
        return value.toLongOrNull()?.takeIf { it in 0..4_294_967_295L }?.toInt()
    }

    private fun parseULong(value: String): Long? {
        return value.toLongOrNull()?.takeIf { it >= 0 }
    }

    private fun parseSecurity(value: String): ConnectWifi.Security? {
        return when (value.lowercase()) {
            "", "auto" -> ConnectWifi.Security.SECURITY_UNSPECIFIED
            "wpa2" -> ConnectWifi.Security.SECURITY_WPA2_PSK
            "wpa3" -> ConnectWifi.Security.SECURITY_WPA3_SAE
            "transition" -> ConnectWifi.Security.SECURITY_WPA2_WPA3_TRANSITION
            else -> null
        }
    }

    private fun parseBand(value: String): WifiBand? {
        return when (value.lowercase()) {
            "", "all" -> WifiBand.WIFI_BAND_ALL
            "2.4ghz" -> WifiBand.WIFI_BAND_2_4_GHZ
            "5ghz" -> WifiBand.WIFI_BAND_5_GHZ
            "6ghz" -> WifiBand.WIFI_BAND_6_GHZ
            "60ghz" -> WifiBand.WIFI_BAND_60_GHZ
            else -> null
        }
    }

    private fun parseMacRandomization(value: String): ConnectWifi.MacRandomization? {
        return when (value.lowercase()) {
            "" -> ConnectWifi.MacRandomization.MAC_RANDOMIZATION_UNSPECIFIED
            "auto" -> ConnectWifi.MacRandomization.MAC_RANDOMIZATION_AUTO
            "none" -> ConnectWifi.MacRandomization.MAC_RANDOMIZATION_NONE
            "persistent" -> ConnectWifi.MacRandomization.MAC_RANDOMIZATION_PERSISTENT
            "non-persistent" -> ConnectWifi.MacRandomization.MAC_RANDOMIZATION_NON_PERSISTENT
            else -> null
        }
    }

    private fun parseQTypes(value: String): List<DnsRecordType> {
        return when (value.uppercase()) {
            "AAAA" -> listOf(DnsRecordType.DNS_RECORD_TYPE_AAAA)
            "ALL" -> listOf(DnsRecordType.DNS_RECORD_TYPE_A, DnsRecordType.DNS_RECORD_TYPE_AAAA)
            else -> listOf(DnsRecordType.DNS_RECORD_TYPE_A)
        }
    }
}
