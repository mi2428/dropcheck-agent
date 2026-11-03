package io.dropcheck.agent

import io.dropcheck.agent.grpc.*
import java.net.URI
import java.util.Locale

internal sealed class AgentShellCommand {
    data object Noop : AgentShellCommand()
    data class Help(val topic: String = "") : AgentShellCommand()
    data class SetDefaultPassphrase(val passphrase: String) : AgentShellCommand()
    data object ClearDefaultPassphrase : AgentShellCommand()
    data class Check(val profile: String, val ssid: String = "", val family: IpFamily = IpFamily.IP_FAMILY_IPV4, val bssid: String = "", val lab: LabConfig? = null) : AgentShellCommand()
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
    data class Piped(val command: AgentShellCommand, val pipeline: AgentShellPipeline) : AgentShellCommand()
}

internal data class LabConfig(val gateway: String, val dns: String, val resolver: String, val http: String, val expectedStatus: Int, val timeoutMs: Int)

internal fun LabConfig.valid(family: IpFamily): Boolean {
    val ipv4Literal = gateway.split('.').let { parts -> parts.size == 4 && parts.all { part -> part.isNotEmpty() && part.length <= 3 && part.all { it in '0'..'9' } && part.toIntOrNull() in 0..255 } }
    val address = gateway.takeIf { ipv4Literal || ':' in it && it.all { ch -> ch in '0'..'9' || ch.lowercaseChar() in 'a'..'f' || ch == ':' || ch == '.' } }
        ?.let { runCatching { java.net.InetAddress.getByName(it) }.getOrNull() }
    val uri = runCatching { URI(http) }.getOrNull()
    return family in listOf(IpFamily.IP_FAMILY_IPV4, IpFamily.IP_FAMILY_IPV6) && address != null &&
        (address is java.net.Inet4Address) == (family == IpFamily.IP_FAMILY_IPV4) &&
        dns.isNotBlank() && dns.none { it.isWhitespace() } && !dns.startsWith('-') &&
        resolver == "selected" && uri?.scheme in listOf("http", "https") && !uri?.host.isNullOrEmpty() && uri?.port in -1..65535 &&
        uri?.rawUserInfo == null && uri?.rawQuery == null && uri?.rawFragment == null &&
        expectedStatus in 100..599 && timeoutMs in 1..30000
}

/** The same rows drive parsing and help; the registry remains the executor capability table. */
internal object AgentShellParser {
    private val syntax = AgentShellContract.rows
    private val aliases = AgentShellContract.aliases
    init {
        val roots = syntax.map { it.parts.first() }.toSet()
        require(aliases.keys.none { it in roots || it in aliases.values }) { "shell keyword/alias collision" }
        require(syntax.map { it.path }.distinct().size == syntax.size) { "duplicate shell command" }
    }

    private fun resolve(word: String, siblings: List<String>, alias: Boolean = false): String {
        require(word.isNotBlank()) { "empty keyword" }
        val token = word
        if (token in siblings) return token
        if (alias) aliases[token]?.takeIf { it in siblings }?.let { return it }
        val matches = siblings.filter { it.startsWith(token) }
        require(matches.size == 1) { if (matches.isEmpty()) "unknown keyword" else "ambiguous keyword: ${matches.joinToString(", ")}" }
        return matches.single()
    }

    fun help(topic: String = ""): List<String> {
        val rows = syntax.filter { topic.isEmpty() || it.parts.first() == topic }
        return listOf("Agent Shell (self); aliases: p tr pm gip h ?", "Options: key value; switches: fresh brief mlo detail; integers: 1..2147483647", "PSK: 8..63 UTF-8 bytes or 64 ASCII hex; PC-only ADB/device/TTY commands are not available") +
            rows.map { "  ${it.usage()}  ${it.description}" } + listOf("| find <pattern> | except <pattern> | last <lines> | match <pattern> | trim <start-column> | count", "| display json (alone; safe presentation JSON)", "auto selects one family; global-ip all queries both; dns ALL queries A+AAAA", "check link defaults to family ipv4; ipv6/all supported; bssid strict pin unsupported; lab requires all endpoints and sends active traffic", "use/default and command history are session-only; credentials are not retained in history")
    }

    fun parse(line: String): AgentShellCommand {
        return try {
            val lexed = shellLex(line)
            if (lexed.tokens.isEmpty()) return AgentShellCommand.Noop
            val parts = mutableListOf<String>()
            var start = 0
            lexed.tokens.filter { it.pipe }.forEach { token -> parts += line.substring(start, token.start).trim(); start = token.end }
            parts += line.substring(start).trim()
            require(parts.none { it.isEmpty() }) { "empty pipeline stage" }
            val pipeline = AgentShellPipeline.parse(parts.drop(1))
            val command = parseTokens(shellSplitWords(parts.first()).getOrThrow())
            if (pipeline.active) AgentShellCommand.Piped(command, pipeline) else command
        } catch (e: IllegalArgumentException) {
            AgentShellCommand.Invalid(e.message ?: "invalid command")
        }
    }

    fun contextHelp(prefix: String): List<String> = if (prefix.isBlank()) help() else complete(prefix).mapNotNull { choice -> shellLex(choice, true).tokens.lastOrNull()?.value?.let { "  $it" } }

    fun complete(line: String): List<String> {
        val lexed = shellLex(line, true)
        if (lexed.quote != null || lexed.escaped) return emptyList()
        val lastPipe = lexed.tokens.indexOfLast { it.pipe }
        val tokens = lexed.tokens.drop(lastPipe + 1)
        val current = tokens.lastOrNull()?.takeIf { it.end == line.length }
        if (current?.literal == true) return emptyList()
        val prefix = current?.value.orEmpty()
        val start = current?.start ?: line.length
        val args = (if (current != null) tokens.dropLast(1) else tokens).map { it.value }
        val choices = if (lastPipe >= 0) when {
            args.isEmpty() -> listOf("find", "except", "last", "match", "trim", "count", "display")
            args == listOf("display") -> listOf("json")
            args.size == 1 && args[0] in listOf("find", "match", "except") -> listOf("<pattern>")
            args == listOf("last") -> listOf("<lines>")
            args == listOf("trim") -> listOf("<start-column>")
            else -> emptyList()
        } else suggestions(args)
        return choices.filter { it.startsWith(prefix) || it.startsWith('<') }.map { line.substring(0, start) + it + if (it == prefix && !it.startsWith('<')) " " else "" }
    }

    private fun suggestions(args: List<String>): List<String> {
        var candidates = syntax
        val path = mutableListOf<String>()
        var index = 0
        while (index < args.size) {
            val complete = candidates.singleOrNull { it.parts == path }
            if (complete != null && !(complete.path == "show wifi scan" && "detail".startsWith(args[index]))) break
            val children = candidates.mapNotNull { it.parts.getOrNull(path.size) }.distinct()
            val key = runCatching { resolve(args[index], children, path.isEmpty()) }.getOrNull() ?: return emptyList()
            path += key; index++
            candidates = candidates.filter { it.parts.take(path.size) == path }
        }
        val row = candidates.singleOrNull { it.parts == path }
            ?: return candidates.mapNotNull { it.parts.getOrNull(path.size) }.distinct()
        val rest = args.drop(index)
        if (row.path == "show network" && rest.isEmpty()) return listOf("default", "<network-id>")
        if (row.path == "check" && rest.isEmpty()) return listOf("link", "lab")
        if (rest.size < row.positionals.size) return listOf("<${row.positionals[rest.size]}>")
        val used = mutableSetOf<String>()
        var offset = row.positionals.size
        while (offset < rest.size) {
            val key = runCatching { resolve(rest[offset++], row.keys) }.getOrNull() ?: return emptyList()
            used += key
            if (key !in row.switches) {
                if (offset == rest.size) return row.enums[key] ?: listOf("<value>")
                offset++
            }
        }
        val children = if (rest.isEmpty()) candidates.mapNotNull { it.parts.getOrNull(path.size) }.distinct() else emptyList()
        return children + row.keys.filter { (it !in used || it == "via") && !(it == "mlo" && "brief" !in used) &&
            !(it == "timeout" && row.path in listOf("show wifi scan", "show wifi eht") && "fresh" !in used) }
    }

    fun historySafe(line: String): Boolean {
        val parsed = parse(line)
        return when (parsed) {
            is AgentShellCommand.Help, AgentShellCommand.ShowVersion, AgentShellCommand.ShowChecks, is AgentShellCommand.ShowCheckLast -> true
            is AgentShellCommand.Execute -> parsed.request.commandCase in listOf(RunCommand.CommandCase.GET_WIFI_STATUS,
                RunCommand.CommandCase.GET_WIFI_CAPABILITIES, RunCommand.CommandCase.GET_NETWORKS,
                RunCommand.CommandCase.GET_PERMISSIONS, RunCommand.CommandCase.GET_IP_STATUS)
            else -> false
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
            if (remaining.any { it.parts.size == path.size } && !(path == listOf("show", "wifi", "scan") && "detail".startsWith(tokens[index]))) break
            val key = resolve(tokens[index], children, path.isEmpty())
            path += key
            remaining = remaining.filter { it.parts.take(path.size) == path }
            index++
        }
        val row = remaining.singleOrNull { it.parts == path } ?: throw IllegalArgumentException("incomplete command: ${remaining.map { it.path }.joinToString(", ")}")
        val command = row.path
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
            if (profile == "lab") {
                if (index == tokens.size) return AgentShellCommand.Check(profile)
                val fields = mutableMapOf<String, String>()
                while (index < tokens.size) {
                    val key = resolve(tokens[index++], listOf("ssid", "family", "gateway", "dns", "resolver", "http", "expected-status", "timeout"))
                    require(!fields.containsKey(key) && index < tokens.size && tokens[index].isNotEmpty()) { "invalid lab configuration" }
                    fields[key] = tokens[index++]
                }
                val family = when (fields["family"]) {
                    "ipv4" -> IpFamily.IP_FAMILY_IPV4
                    "ipv6" -> IpFamily.IP_FAMILY_IPV6
                    else -> throw IllegalArgumentException("lab family must be ipv4 or ipv6")
                }
                val status = fields["expected-status"]?.toIntOrNull()
                val timeout = fields["timeout"]?.toIntOrNull()
                val lab = LabConfig(fields["gateway"].orEmpty(), fields["dns"].orEmpty(), fields["resolver"].orEmpty(), fields["http"].orEmpty(), status ?: 0, timeout ?: 0)
                require(fields["ssid"]?.isNotBlank() == true && lab.valid(family)) { "lab requires literal SSID, family, gateway IP, DNS, resolver selected, query-free HTTP URL, expected status and timeout" }
                return AgentShellCommand.Check(profile, fields.getValue("ssid"), family, lab = lab)
            }
            if (profile != "link") {
                require(index == tokens.size) { "unsupported profile; use show checks" }
                return AgentShellCommand.Check(profile)
            }
            require(index + 1 < tokens.size && resolve(tokens[index], listOf("ssid")) == "ssid" && tokens[index + 1].isNotEmpty()) {
                "usage: check link ssid SSID [family ipv4|ipv6|all] [bssid BSSID]"
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
                        "all" -> IpFamily.IP_FAMILY_ALL
                        else -> throw IllegalArgumentException("check link family must be ipv4, ipv6 or all")
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
            require(tokens.size - index == 1 || tokens.size - index == 3) { "usage: use SSID [passphrase PSK]" }
            require(tokens[index].isNotEmpty()) { "SSID is required" }
            val passphrase = if (tokens.size - index == 3) {
                require(resolve(tokens[index + 1], listOf("passphrase")) == "passphrase")
                tokens[index + 2].also(::validPassphrase)
            } else null
            return AgentShellCommand.Use(tokens[index], passphrase)
        }
        val position = if (row.position.isNotEmpty()) {
            val value = tokens.getOrNull(index) ?: throw IllegalArgumentException("usage: ${row.usage()}")
            require(if (command in setOf("wifi connect", "wifi cycle", "wifi forget")) value.isNotEmpty() else value.isNotBlank()) { "required literal cannot be empty" }
            index++
            value
        } else ""
        val ssid = position
        val values = linkedMapOf<String, String>()
        val switches = mutableSetOf<String>()
        val via = mutableListOf<String>()
        while (index < tokens.size) {
            val key = resolve(tokens[index], row.keys)
            if (key in row.switches) {
                require(switches.add(key)) { "$key specified twice" }
                index++
            } else {
                require(index + 1 < tokens.size) { "$key requires a value" }
                val value = tokens[index + 1]
                require(if (key == "ssid" || key == "passphrase") value.isNotEmpty() else value.isNotBlank()) { "$key requires a nonempty value" }
                if (key == "via") via += address(value)
                else {
                    require(key !in values) { "$key specified twice" }
                    values[key] = value
                }
                index += 2
            }
        }
        row.required.forEach { require(!values[it].isNullOrEmpty()) { "$it is required" } }
        values.forEach { (key, value) -> require(row.enums[key]?.contains(value) != false) { "invalid $key" } }
        val passphrase = values["passphrase"].orEmpty().also { if (it.isNotEmpty()) validPassphrase(it) }
        fun value(key: String) = values[key].orEmpty()
        fun number(key: String, default: Int = 0): Int {
            val raw = values[key] ?: return default
            require(raw.all { it in '0'..'9' } && raw.isNotEmpty()) { "$key must be a positive integer" }
            val parsed = raw.toIntOrNull()
            require(parsed != null && parsed > 0) { "$key must be in 1..2147483647" }
            return parsed
        }
        fun band() = when (value("band")) {
            "", "all" -> WifiBand.WIFI_BAND_ALL
            "2.4ghz" -> WifiBand.WIFI_BAND_2_4_GHZ
            "5ghz" -> WifiBand.WIFI_BAND_5_GHZ
            "6ghz" -> WifiBand.WIFI_BAND_6_GHZ
            "60ghz" -> WifiBand.WIFI_BAND_60_GHZ
            else -> throw IllegalArgumentException("invalid band (all, 2.4ghz, 5ghz, 6ghz, 60ghz)")
        }
        fun security() = when (value("security")) {
            "", "auto" -> ConnectWifi.Security.SECURITY_UNSPECIFIED
            "wpa2" -> ConnectWifi.Security.SECURITY_WPA2_PSK
            "wpa3" -> ConnectWifi.Security.SECURITY_WPA3_SAE
            "transition" -> ConnectWifi.Security.SECURITY_WPA2_WPA3_TRANSITION
            else -> throw IllegalArgumentException("invalid security (auto, wpa2, wpa3, transition)")
        }
        fun family(global: Boolean = false) = when (value("family")) {
            "" -> if (global) IpFamily.IP_FAMILY_ALL else IpFamily.IP_FAMILY_UNSPECIFIED
            "auto" -> { require(!global) { "global-ip family must be ipv4, ipv6 or all" }; IpFamily.IP_FAMILY_UNSPECIFIED }
            "ipv4" -> IpFamily.IP_FAMILY_IPV4
            "ipv6" -> IpFamily.IP_FAMILY_IPV6
            "all" -> { require(global) { "family all is only supported for global-ip" }; IpFamily.IP_FAMILY_ALL }
            else -> throw IllegalArgumentException("invalid family (ipv4, ipv6, ${if (global) "all" else "auto"})")
        }
        fun selector(): NetworkSelector {
            value("bssid").takeIf { it.isNotEmpty() }?.let(::bssid)
            require(value("network-id").isEmpty() || validNetworkId(value("network-id"))) { "invalid Android Network ID" }
            return NetworkSelector.newBuilder().setSsid(value("ssid")).setBssid(value("bssid"))
                .setNetworkId(value("network-id")).build()
        }
        fun connect() = ConnectWifi.newBuilder().setSsid(ssid).setPassphrase(passphrase).setSecurity(security()).setBand(band())
            .setBssid(value("bssid").also { if (it.isNotEmpty()) bssid(it) }).setMacRandomization(when (value("mac-randomization")) {
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
            "show networks" -> RunCommand.newBuilder().setGetNetworks(GetNetworks.getDefaultInstance()).build()
            "show network" -> {
                require(position == "default" || validNetworkId(position)) { "expected default or a canonical positive Android Network ID" }
                RunCommand.newBuilder().setGetNetwork(GetNetwork.newBuilder().setDefaultNetwork(position == "default").setNetworkId(if (position == "default") "" else position)).build()
            }
            "show permissions" -> RunCommand.newBuilder().setGetPermissions(GetPermissions.getDefaultInstance()).build()
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
                val types = when (value("record")) {
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

    private fun boolean(value: String, key: String): Boolean = when (value) {
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
