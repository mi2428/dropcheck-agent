package io.dropcheck.agent

import io.dropcheck.agent.grpc.*
import java.net.URI
import java.util.Locale

private fun String.shellWords(): List<String> = if (isEmpty()) emptyList() else split(' ')

internal sealed class AgentShellCommand {
    data object Noop : AgentShellCommand()
    data class Help(val topic: String = "") : AgentShellCommand()
    data class SetDefaultPassphrase(val passphrase: String) : AgentShellCommand()
    data object ClearDefaultPassphrase : AgentShellCommand()
    data class Check(val profile: String, val ssid: String = "", val family: IpFamily = IpFamily.IP_FAMILY_IPV4, val bssid: String = "") : AgentShellCommand()
    data object ShowChecks : AgentShellCommand()
    data class ShowCheckLast(val detail: Boolean = false) : AgentShellCommand()
    data object ShowVersion : AgentShellCommand()
    data class ShowWifiEht(val detail: Boolean = false, val fresh: Boolean = false, val timeoutMs: Int = 0, val ssid: String = "", val bssid: String = "") : AgentShellCommand()
    data class Use(val ssid: String, val passphrase: String? = null) : AgentShellCommand()
    data class Execute(
        val request: RunCommand,
        val detail: Boolean = false,
        val brief: Boolean = false,
        val mlo: Boolean = false,
        val fresh: Boolean = false,
        val via: List<String> = emptyList(),
        val secret: String = "",
    ) : AgentShellCommand()
    data class Invalid(val message: String) : AgentShellCommand()
}

/** The same rows drive parsing and help; the registry remains the executor capability table. */
internal object AgentShellParser {
    private data class Syntax(val path: String, val position: String = "", val switches: String = "", val values: String = "") {
        val parts = path.split(' ')
        val keys get() = switches.shellWords() + values.shellWords()
        fun usage(): String = buildString {
            append(path)
            if (position.isNotEmpty()) append(" $position")
            switches.shellWords().forEach { append(" [$it]") }
            values.shellWords().forEach { append(" [$it VALUE]") }
            if (path == "show wifi scan") append(" (mlo requires brief)")
        }
    }

    private val syntax = listOf(
        Syntax("show version"), Syntax("show wifi status", switches = "detail"),
        Syntax("show wifi diagnostics", switches = "detail"), Syntax("show wifi capabilities", switches = "detail"),
        Syntax("show wifi scan", switches = "fresh brief mlo", values = "band timeout"),
        Syntax("show wifi scan detail", "TARGET", values = "band"),
        Syntax("show wifi eht", switches = "detail fresh", values = "ssid bssid timeout"),
        Syntax("show ip status", switches = "detail", values = "ssid"),
        Syntax("wifi connect", "SSID passphrase PSK", values = "security bssid band mac-randomization timeout"),
        Syntax("wifi disconnect"), Syntax("wifi forget", "TARGET"),
        Syntax("wifi wait connected", values = "ssid bssid security band require-ip require-validated timeout"),
        Syntax("wifi assert", values = "ssid bssid security band require-ip require-validated timeout"),
        Syntax("wifi monitor", values = "duration interval"), Syntax("wifi reconnect", values = "timeout"),
        Syntax("wifi cycle", "SSID passphrase PSK", values = "security bssid band mac-randomization timeout count ping http forget-after-each pause"),
        Syntax("ping", "HOST", values = "count size family timeout ssid"),
        Syntax("traceroute", "HOST", values = "max-hops via size family timeout ssid"),
        Syntax("path-mtu", "HOST", values = "min-mtu max-mtu family timeout ssid"),
        Syntax("global-ip", values = "family timeout ssid"),
        Syntax("dns", "NAME", values = "record timeout ssid"),
        Syntax("http", "URL", values = "expected-status timeout ssid"),
        Syntax("download", "URL", values = "timeout ssid"),
        Syntax("help", "[TOPIC]"), Syntax("set default passphrase", "PASSPHRASE"),
        Syntax("clear default passphrase"), Syntax("use", "SSID [PASSPHRASE]"),
        Syntax("check", "link ssid SSID [family ipv4|ipv6] [bssid BSSID]"), Syntax("show checks"), Syntax("show check last", switches = "detail"),
        Syntax("show devices"),
    )
    private val aliases = mapOf("p" to "ping", "tr" to "traceroute", "pm" to "path-mtu", "gip" to "global-ip", "h" to "help", "?" to "help")
    init {
        val roots = syntax.map { it.parts.first() }.toSet()
        require(aliases.keys.none { it in roots || it in aliases.values }) { "shell keyword/alias collision" }
        require(syntax.map { it.path }.distinct().size == syntax.size) { "duplicate shell command" }
    }

    private fun resolve(word: String, siblings: List<String>, alias: Boolean = false): String {
        require(word.isNotBlank()) { "empty keyword" }
        require(word != "|") { "PC pipeline: unsupported on Android" }
        val token = word.lowercase(Locale.ROOT)
        if (token in siblings) return token
        if (alias) aliases[token]?.takeIf { it in siblings }?.let { return it }
        val matches = siblings.filter { it.startsWith(token) }
        require(matches.size == 1) { if (matches.isEmpty()) "unknown keyword" else "ambiguous keyword: ${matches.joinToString(", ")}" }
        return matches.single()
    }

    fun help(topic: String = ""): List<String> {
        val rows = syntax.filter { topic.isEmpty() || it.parts.first() == topic }
        return listOf("Agent Shell (self); aliases: p tr pm gip h ?", "Options: key value; switches: fresh brief mlo detail; integers: 1..2147483647", "PSK: 8..63 UTF-8 bytes or 64 ASCII hex; PC-only ADB/device/format/pipeline/TTY: unsupported on Android") +
            rows.map { "  ${it.usage()}" } + listOf("auto selects one family; global-ip all queries both; dns ALL queries A+AAAA", "check link defaults to family ipv4; bssid strict pin unsupported; lab/internet/eht unsupported", "use/default: connect only; check link: read-only, no probes")
    }

    fun parse(line: String): AgentShellCommand {
        val tokens = shellSplitWords(line).getOrElse { return AgentShellCommand.Invalid(it.message ?: "invalid command") }
        if (tokens.isEmpty()) return AgentShellCommand.Noop
        return try { parseTokens(tokens) } catch (e: IllegalArgumentException) {
            AgentShellCommand.Invalid(e.message ?: "invalid command")
        }
    }

    private fun parseTokens(tokens: List<String>): AgentShellCommand {
        if (tokens.first().lowercase(Locale.ROOT) in
            setOf("adb", "devices", "format", "shell", "tui", "--serial", "--format", "select", "broadcast", "pipeline")) {
            return AgentShellCommand.Invalid("PC host-only command: unsupported on Android")
        }
        var remaining = syntax
        val path = mutableListOf<String>()
        var index = 0
        while (index < tokens.size) {
            val children = remaining.mapNotNull { it.parts.getOrNull(path.size) }.distinct()
            if (children.isEmpty()) break
            // A complete command has options/positionals next; only scan has a nested subcommand.
            if (remaining.any { it.parts.size == path.size } && !(path == listOf("show", "wifi", "scan") && "detail".startsWith(tokens[index].lowercase(Locale.ROOT)))) break
            val key = resolve(tokens[index], children, path.isEmpty())
            path += key
            remaining = remaining.filter { it.parts.take(path.size) == path }
            index++
        }
        val row = remaining.singleOrNull { it.parts == path } ?: throw IllegalArgumentException("incomplete command: ${remaining.map { it.path }.joinToString(", ")}")
        val command = row.path
        if (command == "show devices") {
            return AgentShellCommand.Invalid("$command: unsupported on Android")
        }
        if (command == "clear default passphrase") {
            require(index == tokens.size) { "usage: clear default passphrase" }
            return AgentShellCommand.ClearDefaultPassphrase
        }
        if (command == "show checks") {
            require(index == tokens.size) { "usage: show checks" }
            return AgentShellCommand.ShowChecks
        }
        if (command == "show check last") {
            require(index == tokens.size || index + 1 == tokens.size && resolve(tokens[index], listOf("detail")) == "detail") { "usage: show check last [detail]" }
            return AgentShellCommand.ShowCheckLast(index < tokens.size)
        }
        if (command == "check") {
            if (index == tokens.size) return AgentShellCommand.ShowChecks
            val profile = tokens[index++] // Profile names are exact literals, not keyword prefixes.
            if (profile != "link") {
                require(index == tokens.size) { "unsupported profile; use show checks" }
                return AgentShellCommand.Check(profile)
            }
            require(index + 1 < tokens.size && tokens[index] == "ssid" && tokens[index + 1].isNotEmpty()) {
                "usage: check link ssid SSID [family ipv4|ipv6] [bssid BSSID]"
            }
            val ssid = tokens[index + 1]
            index += 2
            var family = IpFamily.IP_FAMILY_IPV4
            var requestedBssid = ""
            val seen = mutableSetOf<String>()
            while (index < tokens.size) {
                val key = resolve(tokens[index], listOf("family", "bssid"))
                require(seen.add(key)) { "$key specified twice" }
                require(index + 1 < tokens.size) { "$key requires a value" }
                when (key) {
                    "family" -> family = when (tokens[index + 1]) {
                        "ipv4" -> IpFamily.IP_FAMILY_IPV4
                        "ipv6" -> IpFamily.IP_FAMILY_IPV6
                        else -> throw IllegalArgumentException("check link family must be ipv4 or ipv6")
                    }
                    "bssid" -> requestedBssid = tokens[index + 1].also(::bssid)
                }
                index += 2
            }
            return AgentShellCommand.Check(profile, ssid, family, requestedBssid)
        }
        if (command == "help") {
            require(tokens.size - index <= 1) { "usage: help [TOPIC]" }
            val topic = tokens.getOrNull(index)?.let { resolve(it, syntax.map { s -> s.parts.first() }.distinct(), true) }.orEmpty()
            return AgentShellCommand.Help(topic)
        }
        if (command == "show version") {
            require(index == tokens.size) { "usage: show version" }
            return AgentShellCommand.ShowVersion
        }
        if (command == "set default passphrase") {
            require(tokens.size - index == 1) { "usage: set default passphrase PASSPHRASE" }
            if (tokens[index].isNotEmpty()) validPassphrase(tokens[index])
            return AgentShellCommand.SetDefaultPassphrase(tokens[index])
        }
        if (command == "use") {
            require(tokens.size - index in 1..2 && tokens[index].isNotEmpty()) { "usage: use SSID [PASSPHRASE]" }
            require(tokens.getOrNull(index + 1)?.isEmpty() != true) { "use passphrase cannot be empty" }
            tokens.getOrNull(index + 1)?.let(::validPassphrase)
            return AgentShellCommand.Use(tokens[index], tokens.getOrNull(index + 1))
        }
        val position = if (row.position.isNotEmpty()) {
            val value = tokens.getOrNull(index) ?: throw IllegalArgumentException("usage: ${row.usage()}")
            require(if (command in setOf("wifi connect", "wifi cycle", "wifi forget")) value.isNotEmpty() else value.isNotBlank()) { "required literal cannot be empty" }
            index++
            value
        } else ""
        val ssid = position
        var passphrase = ""
        if (command == "wifi connect" || command == "wifi cycle") {
            require(index < tokens.size && resolve(tokens[index], listOf("passphrase")) == "passphrase") { "passphrase required" }
            passphrase = tokens.getOrNull(index + 1) ?: throw IllegalArgumentException("passphrase required")
            validPassphrase(passphrase)
            index += 2
        }
        val values = linkedMapOf<String, String>()
        val switches = mutableSetOf<String>()
        val via = mutableListOf<String>()
        while (index < tokens.size) {
            val key = resolve(tokens[index], row.keys)
            if (key in row.switches.shellWords()) {
                require(switches.add(key)) { "$key specified twice" }
                index++
            } else {
                require(index + 1 < tokens.size) { "$key requires a value" }
                val value = tokens[index + 1]
                require(if (key == "ssid") value.isNotEmpty() else value.isNotBlank()) { "$key requires a nonempty value" }
                if (key == "via") via += address(value)
                else {
                    require(key !in values) { "$key specified twice" }
                    values[key] = value
                }
                index += 2
            }
        }
        fun value(key: String) = values[key].orEmpty()
        fun number(key: String, default: Int = 0): Int {
            val raw = values[key] ?: return default
            require(raw.all { it in '0'..'9' } && raw.isNotEmpty()) { "$key must be a positive integer" }
            val parsed = raw.toIntOrNull()
            require(parsed != null && parsed > 0) { "$key must be in 1..2147483647" }
            return parsed
        }
        fun band() = when (value("band").lowercase(Locale.ROOT)) {
            "", "all" -> WifiBand.WIFI_BAND_ALL
            "2.4ghz" -> WifiBand.WIFI_BAND_2_4_GHZ
            "5ghz" -> WifiBand.WIFI_BAND_5_GHZ
            "6ghz" -> WifiBand.WIFI_BAND_6_GHZ
            "60ghz" -> WifiBand.WIFI_BAND_60_GHZ
            else -> throw IllegalArgumentException("invalid band (all, 2.4ghz, 5ghz, 6ghz, 60ghz)")
        }
        fun security() = when (value("security").lowercase(Locale.ROOT)) {
            "", "auto" -> ConnectWifi.Security.SECURITY_UNSPECIFIED
            "wpa2" -> ConnectWifi.Security.SECURITY_WPA2_PSK
            "wpa3" -> ConnectWifi.Security.SECURITY_WPA3_SAE
            "transition" -> ConnectWifi.Security.SECURITY_WPA2_WPA3_TRANSITION
            else -> throw IllegalArgumentException("invalid security (auto, wpa2, wpa3, transition)")
        }
        fun family(global: Boolean = false) = when (value("family").lowercase(Locale.ROOT)) {
            "" -> if (global) IpFamily.IP_FAMILY_ALL else IpFamily.IP_FAMILY_UNSPECIFIED
            "auto" -> { require(!global) { "global-ip family must be ipv4, ipv6 or all" }; IpFamily.IP_FAMILY_UNSPECIFIED }
            "ipv4" -> IpFamily.IP_FAMILY_IPV4
            "ipv6" -> IpFamily.IP_FAMILY_IPV6
            "all" -> { require(global) { "family all is only supported for global-ip" }; IpFamily.IP_FAMILY_ALL }
            else -> throw IllegalArgumentException("invalid family (ipv4, ipv6, ${if (global) "all" else "auto"})")
        }
        fun selector() = NetworkSelector.newBuilder().setSsid(value("ssid")).build()
        fun connect() = ConnectWifi.newBuilder().setSsid(ssid).setPassphrase(passphrase).setSecurity(security()).setBand(band())
            .setBssid(value("bssid").also { if (it.isNotEmpty()) bssid(it) }).setMacRandomization(when (value("mac-randomization").lowercase(Locale.ROOT)) {
                "" -> ConnectWifi.MacRandomization.MAC_RANDOMIZATION_UNSPECIFIED
                "auto" -> ConnectWifi.MacRandomization.MAC_RANDOMIZATION_AUTO
                "none" -> ConnectWifi.MacRandomization.MAC_RANDOMIZATION_NONE
                "persistent" -> ConnectWifi.MacRandomization.MAC_RANDOMIZATION_PERSISTENT
                "non-persistent" -> ConnectWifi.MacRandomization.MAC_RANDOMIZATION_NON_PERSISTENT
                else -> throw IllegalArgumentException("invalid mac-randomization (auto, none, persistent, non-persistent)")
            }).setTimeoutMs(number("timeout", 45000)).build()
        fun expectation(wait: Boolean): RunCommand {
            value("bssid").takeIf { it.isNotEmpty() }?.let(::bssid)
            val ip = boolean(value("require-ip"), "require-ip")
            val validated = boolean(value("require-validated"), "require-validated")
            return if (wait) RunCommand.newBuilder().setWaitWifiConnected(WaitWifiConnected.newBuilder().setSsid(value("ssid"))
                .setBssid(value("bssid")).setSecurity(security()).setBand(band()).setRequireIp(ip).setRequireValidated(validated).setTimeoutMs(number("timeout", 30000))).build()
            else RunCommand.newBuilder().setAssertWifi(AssertWifi.newBuilder().setSsid(value("ssid"))
                .setBssid(value("bssid")).setSecurity(security()).setBand(band()).setRequireIp(ip).setRequireValidated(validated).setTimeoutMs(number("timeout"))).build()
        }
        if (command == "show wifi eht") {
            require("timeout" !in values || "fresh" in switches) { "timeout is supported only with wifi eht fresh" }
            require(value("ssid").isEmpty() || value("bssid").isEmpty()) { "ssid and bssid filters cannot be used together" }
            value("bssid").takeIf { it.isNotEmpty() }?.let(::bssid)
            return AgentShellCommand.ShowWifiEht("detail" in switches, "fresh" in switches, number("timeout", if ("fresh" in switches) 10000 else 0), value("ssid"), value("bssid"))
        }
        if (command == "show wifi scan") {
            require("mlo" !in switches || "brief" in switches) { "mlo is supported only with wifi scan brief" }
            require("timeout" !in values || "fresh" in switches) { "timeout is supported only with wifi scan fresh" }
        }
        if (command == "wifi cycle") {
            require(number("count", 3) <= WifiCommandPolicy.MAX_CYCLE_COUNT) { "count exceeds cycle limit" }
            require(number("pause", 1000) <= WifiCommandPolicy.MAX_CYCLE_PAUSE_MS) { "pause exceeds cycle limit" }
            if (value("ping").isNotEmpty()) address(value("ping"))
            if (value("http").isNotEmpty()) {
                require(value("http").startsWith("http://", true) || value("http").startsWith("https://", true)) { "wifi cycle http requires an absolute HTTP URL" }
                url(value("http"))
            }
        }
        val request = when (command) {
            "show wifi status" -> RunCommand.newBuilder().setGetWifiStatus(GetWifiStatus.getDefaultInstance()).build()
            "show wifi diagnostics" -> RunCommand.newBuilder().setGetWifiDiagnostics(GetWifiDiagnostics.getDefaultInstance()).build()
            "show wifi capabilities" -> RunCommand.newBuilder().setGetWifiCapabilities(GetWifiCapabilities.getDefaultInstance()).build()
            "show wifi scan" -> if ("fresh" in switches) RunCommand.newBuilder().setGetFreshWifiScan(GetFreshWifiScan.newBuilder().setBand(band()).setTimeoutMs(number("timeout", 10000))).build()
                else RunCommand.newBuilder().setGetWifiScan(GetWifiScan.newBuilder().setBand(band())).build()
            "show wifi scan detail" -> RunCommand.newBuilder().setGetWifiScanDetail(GetWifiScanDetail.newBuilder().setTarget(position).setBand(band())).build()
            "show ip status" -> RunCommand.newBuilder().setGetIpStatus(GetIpStatus.newBuilder().setSelector(selector())).build()
            "wifi connect" -> RunCommand.newBuilder().setConnectWifi(connect()).build()
            "wifi disconnect" -> RunCommand.newBuilder().setDisconnectWifi(DisconnectWifi.getDefaultInstance()).build()
            "wifi forget" -> RunCommand.newBuilder().setForgetWifi(ForgetWifi.newBuilder().setTarget(position)).build()
            "wifi wait connected" -> expectation(true)
            "wifi assert" -> expectation(false)
            "wifi monitor" -> RunCommand.newBuilder().setMonitorWifi(MonitorWifi.newBuilder().setDurationMs(number("duration", 10000)).setIntervalMs(number("interval", 1000))).build()
            "wifi reconnect" -> RunCommand.newBuilder().setReconnectWifi(ReconnectWifi.newBuilder().setTimeoutMs(number("timeout", 30000))).build()
            "wifi cycle" -> RunCommand.newBuilder().setCycleWifi(CycleWifi.newBuilder().setConnect(connect()).setCount(number("count", 3))
                .setPauseMs(number("pause", 1000)).setForgetAfterEach(boolean(value("forget-after-each"), "forget-after-each"))
                .setPingHost(value("ping")).setHttpUrl(value("http"))).build()
            "ping" -> {
                val count = number("count", 3)
                val timeout = number("timeout", 0)
                require(timeout != 0 || count <= (Int.MAX_VALUE - 3000) / 2000) { "ping count overflows derived timeout" }
                RunCommand.newBuilder().setPing(Ping.newBuilder().setHost(address(position)).setCount(count).setSizeBytes(number("size"))
                    .setFamily(family()).setTimeoutMs(if (timeout == 0) count * 2000 + 3000 else timeout).setSelector(selector())).build()
            }
            "traceroute" -> {
                val hops = number("max-hops", 30)
                require(hops <= NetworkCheckPolicy.MAX_TRACEROUTE_HOPS) { "max-hops exceeds 255" }
                RunCommand.newBuilder().setTraceroute(Traceroute.newBuilder().setHost(address(position)).setMaxHops(hops).setSizeBytes(number("size"))
                    .setFamily(family()).setTimeoutMs(number("timeout", 60000)).setSelector(selector())).build()
            }
            "path-mtu" -> {
                val min = number("min-mtu"); val max = number("max-mtu")
                require(min == 0 || max == 0 || min <= max) { "max-mtu must be greater than or equal to min-mtu" }
                RunCommand.newBuilder().setPathMtu(PathMtu.newBuilder().setHost(address(position)).setMinMtuBytes(min).setMaxMtuBytes(max)
                    .setFamily(family()).setTimeoutMs(number("timeout", 30000)).setSelector(selector())).build()
            }
            "global-ip" -> RunCommand.newBuilder().setGlobalIp(GlobalIp.newBuilder().setFamily(family(true)).setTimeoutMs(number("timeout", 5000)).setSelector(selector())).build()
            "dns" -> {
                val types = when (value("record").uppercase(Locale.ROOT)) {
                    "", "ALL" -> listOf(DnsRecordType.DNS_RECORD_TYPE_A, DnsRecordType.DNS_RECORD_TYPE_AAAA)
                    "A" -> listOf(DnsRecordType.DNS_RECORD_TYPE_A)
                    "AAAA" -> listOf(DnsRecordType.DNS_RECORD_TYPE_AAAA)
                    else -> throw IllegalArgumentException("invalid record (A, AAAA, ALL)")
                }
                RunCommand.newBuilder().setResolveDns(ResolveDns.newBuilder().setName(address(position)).addAllQtypes(types).setTimeoutMs(number("timeout", 5000)).setSelector(selector())).build()
            }
            "http" -> {
                val status = number("expected-status", 200)
                require(status in 100..599) { "expected-status must be 100..599" }
                RunCommand.newBuilder().setHttpCheck(HttpCheck.newBuilder().setUrl(url(position)).setExpectedStatus(status).setTimeoutMs(number("timeout", 5000)).setSelector(selector())).build()
            }
            "download" -> RunCommand.newBuilder().setWget(Wget.newBuilder().setUrl(url(position)).setTimeoutMs(number("timeout", 60000)).setSelector(selector())).build()
            else -> throw IllegalArgumentException("unsupported command")
        }
        return AgentShellCommand.Execute(request, "detail" in switches, "brief" in switches, "mlo" in switches, "fresh" in switches, via, passphrase)
    }

    private fun boolean(value: String, key: String): Boolean = when (value.lowercase(Locale.ROOT)) {
        "", "false" -> false
        "true" -> true
        else -> throw IllegalArgumentException("$key must be true or false")
    }

    private fun bssid(value: String) {
        require(Regex("(?i)[0-9a-f]{2}(:[0-9a-f]{2}){5}").matches(value)) { "invalid BSSID" }
    }

    private fun validPassphrase(value: String) {
        require(AgentShellUsePolicy.validPassphrase(value)) { "invalid passphrase length or encoding" }
    }

    private fun address(value: String): String {
        require(value.isNotBlank() && value.length <= 253 && !value.startsWith('-') &&
            (Regex("[A-Za-z0-9][A-Za-z0-9._-]*").matches(value) || Regex("(?i)[0-9a-f:.]+(?:%[A-Za-z0-9_.-]+)?").matches(value))) { "invalid host/address" }
        if (value.all { it.isDigit() || it == '.' } && '.' in value) {
            require(value.split('.').size == 4 && value.split('.').all { it.isNotEmpty() && it.length <= 3 && it.toIntOrNull()?.let { n -> n in 0..255 } == true }) { "invalid IPv4 address" }
        }
        if (':' in value) {
            require(runCatching { java.net.InetAddress.getByName(value) }.getOrNull() is java.net.Inet6Address) { "invalid IPv6 address" }
        }
        return value
    }

    private fun url(value: String): String {
        val normalized = if ("://" in value) value else "https://$value"
        val parsed = runCatching { URI(normalized) }.getOrNull()
        require(parsed != null && parsed.scheme?.lowercase(Locale.ROOT) in listOf("http", "https") && !parsed.host.isNullOrBlank() &&
            parsed.rawUserInfo == null && parsed.port in -1..65535 && !normalized.any { it.isWhitespace() || Character.isISOControl(it) }) { "invalid HTTP URL" }
        return normalized
    }
}

/** No raw input is echoed: even malformed, duplicate, or keyword-looking credentials stay private. */
internal fun redactAgentShellCommandLine(line: String): String = if (line.isBlank()) "" else "<command submitted>"

internal fun shellSplitWords(line: String): Result<List<String>> {
    val words = mutableListOf<String>()
    val current = StringBuilder()
    var quote: Char? = null
    var escaped = false
    var inToken = false
    for (ch in line) {
        when {
            escaped -> { current.append(ch); escaped = false; inToken = true }
            ch == '\\' -> { escaped = true; inToken = true }
            quote != null && ch == quote -> { quote = null; inToken = true }
            quote != null -> { current.append(ch); inToken = true }
            ch == '"' || ch == '\'' -> { quote = ch; inToken = true }
            ch.isWhitespace() -> {
                if (inToken) { words += current.toString(); current.clear(); inToken = false }
            }
            else -> { current.append(ch); inToken = true }
        }
    }
    if (escaped) return Result.failure(IllegalArgumentException("trailing escape"))
    if (quote != null) return Result.failure(IllegalArgumentException("unterminated quote"))
    if (inToken) words += current.toString()
    return Result.success(words)
}
