package io.dropcheck.agent

import io.dropcheck.agent.grpc.WifiCapabilities
import io.dropcheck.agent.grpc.WifiInformationElement
import io.dropcheck.agent.grpc.WifiScanResult
import io.dropcheck.agent.grpc.WifiSecurityDetails

private const val RNR_ELEMENT_ID = 201
private const val MULTIPLE_BSSID_ELEMENT_ID = 71
private const val NONTRANSMITTED_BSSID_PROFILE_SUBELEMENT_ID = 0
private const val NONTRANSMITTED_BSSID_CAPABILITY_ID = 83
private const val MULTIPLE_BSSID_INDEX_ID = 85
private const val EXTENSION_ELEMENT_ID = 255
private const val NON_INHERITANCE_ID_EXT = 56

internal fun wifi7DeviceReadinessRows(
    capabilities: WifiCapabilities?,
    wifi7Supported: Boolean?,
): List<Pair<String, String>> {
    if (capabilities == null) {
        return listOfNotNull(
            wifi7Supported?.let { "standard_11be" to if (it) "supported" else "unsupported" },
        )
    }
    return listOf(
        "band_6ghz" to capabilityState(capabilities.supportedBandsList, capabilities.unsupportedBandsList, "6GHz"),
        "standard_11be" to capabilityState(capabilities.supportedStandardsList, capabilities.unsupportedStandardsList, "802.11be"),
        "wpa3_sae" to capabilityState(capabilities.supportedSecurityModesList, capabilities.unsupportedSecurityModesList, "wpa3_sae"),
        "wpa3_sae_h2e" to capabilityState(capabilities.supportedSecurityModesList, capabilities.unsupportedSecurityModesList, "wpa3_sae_h2e"),
        "wpa3_sae_public_key" to capabilityState(capabilities.supportedSecurityModesList, capabilities.unsupportedSecurityModesList, "wpa3_sae_public_key"),
        "owe" to capabilityState(capabilities.supportedSecurityModesList, capabilities.unsupportedSecurityModesList, "owe"),
        "tid_to_link_mapping" to capabilityState(capabilities.supportedFeaturesList, capabilities.unsupportedFeaturesList, "tid_to_link_mapping_negotiation"),
        "dual_band_simultaneous" to capabilityState(capabilities.supportedFeaturesList, capabilities.unsupportedFeaturesList, "dual_band_simultaneous"),
        "sta_multi_internet" to capabilityState(capabilities.supportedFeaturesList, capabilities.unsupportedFeaturesList, "sta_concurrency_multi_internet"),
    )
}

private fun capabilityState(supported: List<String>, unsupported: List<String>, name: String): String {
    return when {
        supported.any { it.equals(name, ignoreCase = true) } -> "supported"
        unsupported.any { it.equals(name, ignoreCase = true) } -> "unsupported"
        else -> "unknown"
    }
}

internal fun wifiMloInformationElementChecklist(result: WifiScanResult): String {
    val elements = result.informationElementsList
    return listOf(
        "rsn=${elements.hasElement(48)}",
        "rsnxe=${elements.hasElement(244)}",
        "ext_cap=${elements.hasElement(127)}",
        "rnr=${elements.hasElement(RNR_ELEMENT_ID)}",
        "mbssid=${elements.hasElement(MULTIPLE_BSSID_ELEMENT_ID)}",
        "noninherit=${elements.hasElement(EXTENSION_ELEMENT_ID, NON_INHERITANCE_ID_EXT)}",
        "eht_mle=${parseEhtMultiLinkElements(elements).isNotEmpty()}",
        "ap_mld=${result.apMldMacAddress.isNotBlank() || mloMldMacFromElements(elements).isNotBlank()}",
        "link_id=${result.apMloLinkId >= 0 || mloCurrentLinkIdFromElements(elements) != null}",
    ).joinToString(" ", prefix = "ie ")
}

internal fun wifiMloScanSdkFlags(result: WifiScanResult): String {
    return "sdk_flags twt=${result.twtResponder} 11az_ntb=${result.responder80211AzNtb} " +
        "ranging_prot=${result.rangingFrameProtectionRequired} secure_he_ltf=${result.secureHeLtfSupported} 11mc=${result.responder80211Mc}"
}

internal fun wifiMloRoamingSummaryLines(
    elements: List<WifiInformationElement>,
    security: WifiSecurityDetails?,
): List<String> {
    val flags = (roamingFlags(elements) + listOfNotNull(
        "11v_bss_transition".takeIf {
            security?.extendedCapabilitiesList?.any { capability -> capability.equals("bss_transition", ignoreCase = true) } == true
        },
    )).distinct()
    val ftAkms = security?.akmSuitesList
        ?.filter { it.startsWith("ft_", ignoreCase = true) }
        ?.distinct()
        .orEmpty()
    if (flags.isEmpty() && ftAkms.isEmpty()) return emptyList()
    val has11k = flags.any { it.equals("11k", ignoreCase = true) }
    val has11v = flags.any { it.equals("11v_bss_transition", ignoreCase = true) }
    val has11r = flags.any { it.equals("11r", ignoreCase = true) } ||
        flags.any { it.equals("fast_bss_transition", ignoreCase = true) } ||
        ftAkms.isNotEmpty()
    val hasRnr = flags.any { it.equals("reduced_neighbor_report", ignoreCase = true) }
    return listOf(
        "summary 11k=$has11k 11v_bss_transition=$has11v 11r=$has11r ft_akm=${joined(ftAkms)} rnr=$hasRnr",
        "flags ${joined(flags)}",
    )
}

private fun roamingFlags(elements: List<WifiInformationElement>): List<String> = buildList {
    elements.forEach { element ->
        when (element.id) {
            54 -> add("11r")
            55 -> add("fast_bss_transition")
            70 -> add("11k")
            127 -> if (informationElementBitLocal(element, 19)) add("11v_bss_transition")
            RNR_ELEMENT_ID -> add("reduced_neighbor_report")
        }
    }
}

internal fun wifiMloSecuritySummaryLines(value: WifiSecurityDetails): List<String> = buildList {
    if (value.rsnPresent) {
        add("rsn version=${value.rsnVersion} group=${empty(value.groupDataCipher, "<unknown>")} pairwise=${joined(value.pairwiseCiphersList)}")
        add("akm ${joined(value.akmSuitesList)}")
        add("pmf capable=${value.pmfCapable} required=${value.pmfRequired} group_mgmt=${empty(value.groupManagementCipher, "<none>")}")
    }
    add("wifi7 gcmp256=${value.gcmp256} sae_gdh=${value.saeGdh} ft_sae_gdh=${value.ftSaeGdh} beacon_protection=${value.beaconProtection} personal_ready=${value.wifi7PersonalReady}")
    add("wifi7_strict ${wifiSecurityStrictSummary(value)}")
    if (value.rsnxePresent) add("rsnxe ${joined(value.rsnxeCapabilitiesList)}")
    if (value.extendedCapabilitiesPresent) add("extended ${joined(value.extendedCapabilitiesList)}")
    if (value.warningsCount > 0) add("warnings ${value.warningsList.joinToString(",")}")
}

private fun wifiSecurityStrictSummary(value: WifiSecurityDetails): String {
    val pairwiseOnly = value.pairwiseCiphersCount > 0 && value.pairwiseCiphersList.all { it == "gcmp_256" }
    val akmGdhOnly = value.akmSuitesCount > 0 && value.akmSuitesList.all { it == "sae_gdh" || it == "ft_sae_gdh" }
    val fallback = (value.pairwiseCiphersList.filter { it != "gcmp_256" } +
        value.akmSuitesList.filter { it != "sae_gdh" && it != "ft_sae_gdh" })
    val groupMgmt256 = value.groupManagementCipher == "bip_gmac_256" || value.groupManagementCipher == "bip_cmac_256"
    val strictReady = value.pmfRequired && pairwiseOnly && akmGdhOnly && value.beaconProtection
    return "pairwise_gcmp256_only=$pairwiseOnly akm_gdh_only=$akmGdhOnly " +
        "group_data_gcmp256=${value.groupDataCipher == "gcmp_256"} group_mgmt_256=$groupMgmt256 " +
        "fallback=${joined(fallback)} strict_ready=$strictReady"
}

internal fun securityDetailsFromProfileElements(elements: List<EhtProfileInformationElement>): WifiSecurityDetails? {
    val converted = elements.mapNotNull { element ->
        when (element.id) {
            48, 127, 244 -> WifiInformationElement.newBuilder()
                .setId(element.id)
                .setByteCount(element.actualLength)
                .setBytesHex(element.bodyHex)
                .build()
            else -> null
        }
    }
    return parseWifiSecurityDetails(converted)
}

internal fun formatWifiMloRnrDetails(label: String, elements: List<WifiInformationElement>): List<String> {
    val neighbors = elements
        .filter { it.id == RNR_ELEMENT_ID }
        .flatMap { parseRnrNeighbors(hexToBytesLocal(it.bytesHex)) }
    if (neighbors.isEmpty()) return emptyList()
    return buildList {
        add(label)
        neighbors.forEach { neighbor ->
            val truncated = if (neighbor.truncated) " truncated=true" else ""
            val resolved = rnrResolvedChannel(neighbor.operatingClass, neighbor.channel)
            add("  rnr band=${resolved.band} width=${resolved.width} channel=${neighbor.channel} freq=${resolved.frequency} " +
                "op_class=${neighbor.operatingClass} type=${neighbor.type} " +
                "filtered=${neighbor.filtered} info_len=${neighbor.infoLength} count=${neighbor.count} " +
                "flags=${joined(rnrFieldFlags(neighbor.infoLength))} raw=0x${neighbor.headerRaw.toHex16()}$truncated")
            neighbor.tbtts.forEach { tbtt ->
                val parts = mutableListOf("tbtt offset=${tbtt.offset}")
                if (tbtt.bssid.isNotBlank()) parts += "bssid=${tbtt.bssid}"
                if (tbtt.shortSsidHex.isNotBlank()) parts += "short_ssid=0x${tbtt.shortSsidHex}"
                tbtt.bssParameters?.let { parts += "bss_params=0x${it.toString(16).padStart(2, '0')}" }
                tbtt.psd20Mhz?.let { parts += "psd20=$it" }
                tbtt.mld?.let {
                    parts += "mld ap_mld_id=${it.apMldId} link_id=${it.linkId} bss_change=${it.bssParametersChangeCount} all_updates=${it.allUpdates} disabled=${it.disabledLink}"
                }
                if (tbtt.truncated) parts += "truncated=true"
                if (tbtt.rawHex.isNotBlank()) parts += "raw=0x${tbtt.rawHex.hexPreview(24)}"
                add("  ${parts.joinToString(" ")}")
            }
        }
    }
}

private fun rnrResolvedChannel(opClass: Int, channel: Int): RnrResolvedChannel {
    val operatingClass = rnrOperatingClassLabel(opClass)
    val frequencyMhz = rnrFrequencyMhz(opClass, channel)
    return RnrResolvedChannel(
        band = empty(operatingClass.band, "<unknown>"),
        width = empty(operatingClass.width, "<unknown>"),
        frequency = if (frequencyMhz > 0) "${frequencyMhz}MHz" else "<unknown>",
    )
}

private fun rnrOperatingClassLabel(opClass: Int): RnrOperatingClass = when (opClass) {
    81, 82 -> RnrOperatingClass(band = "2.4ghz", width = "20MHz")
    83, 84 -> RnrOperatingClass(band = "2.4ghz", width = "40MHz")
    115, 118, 121, 124, 125 -> RnrOperatingClass(band = "5ghz", width = "20MHz")
    116, 117, 119, 120, 122, 123, 126, 127 -> RnrOperatingClass(band = "5ghz", width = "40MHz")
    128 -> RnrOperatingClass(band = "5ghz", width = "80MHz")
    129 -> RnrOperatingClass(band = "5ghz", width = "160MHz")
    130 -> RnrOperatingClass(band = "5ghz", width = "80+80MHz")
    131, 136 -> RnrOperatingClass(band = "6ghz", width = "20MHz")
    132 -> RnrOperatingClass(band = "6ghz", width = "40MHz")
    133 -> RnrOperatingClass(band = "6ghz", width = "80MHz")
    134 -> RnrOperatingClass(band = "6ghz", width = "160MHz")
    135 -> RnrOperatingClass(band = "6ghz", width = "80+80MHz")
    137 -> RnrOperatingClass(band = "6ghz", width = "320MHz")
    180 -> RnrOperatingClass(band = "60ghz", width = "2160MHz")
    181 -> RnrOperatingClass(band = "60ghz", width = "4320MHz")
    182 -> RnrOperatingClass(band = "60ghz", width = "6480MHz")
    183 -> RnrOperatingClass(band = "60ghz", width = "8640MHz")
    else -> RnrOperatingClass()
}

private fun rnrFrequencyMhz(opClass: Int, channel: Int): Int {
    return when {
        opClass in 81..84 && channel == 14 -> 2484
        opClass in 81..84 && channel in 1..13 -> 2407 + channel * 5
        opClass in 115..130 && channel in 1..200 -> 5000 + channel * 5
        opClass == 136 && channel == 2 -> 5935
        opClass in 131..137 && channel in 1..233 -> 5950 + channel * 5
        opClass in 180..183 && channel in 1..27 -> 56160 + channel * 2160
        else -> 0
    }
}

private fun parseRnrNeighbors(bytes: ByteArray): List<RnrNeighbor> {
    val out = mutableListOf<RnrNeighbor>()
    var offset = 0
    while (offset < bytes.size) {
        if (offset + 4 > bytes.size) {
            if (out.isNotEmpty()) out[out.lastIndex] = out.last().copy(truncated = true)
            break
        }
        val header = bytes.u16leLocal(offset)
        val count = ((header ushr 4) and 0x0f) + 1
        val infoLength = (header ushr 8) and 0xff
        val operatingClass = bytes[offset + 2].u8Local()
        val channel = bytes[offset + 3].u8Local()
        val tbtts = mutableListOf<RnrTbtt>()
        var truncated = false
        offset += 4
        if (infoLength <= 0) {
            out += RnrNeighbor(
                headerRaw = header,
                type = header and 0x03,
                filtered = header and 0x04 != 0,
                count = count,
                infoLength = infoLength,
                operatingClass = operatingClass,
                channel = channel,
                tbtts = emptyList(),
                truncated = true,
            )
            break
        }
        repeat(count) {
            if (offset >= bytes.size) {
                truncated = true
                return@repeat
            }
            val end = (offset + infoLength).coerceAtMost(bytes.size)
            tbtts += parseRnrTbtt(bytes.copyOfRange(offset, end), infoLength)
            if (end - offset != infoLength) truncated = true
            offset = end
        }
        out += RnrNeighbor(
            headerRaw = header,
            type = header and 0x03,
            filtered = header and 0x04 != 0,
            count = count,
            infoLength = infoLength,
            operatingClass = operatingClass,
            channel = channel,
            tbtts = tbtts,
            truncated = truncated,
        )
    }
    return out
}

private fun parseRnrTbtt(field: ByteArray, declaredLength: Int): RnrTbtt {
    if (field.isEmpty()) return RnrTbtt(truncated = true)
    val bssid = if (declaredLength >= 7 && field.size >= 7) field.readMac(1) else ""
    val shortSsid = if (declaredLength >= 11 && field.size >= 11) field.copyOfRange(7, 11).toHex() else ""
    val bssParameters = if (declaredLength >= 12 && field.size >= 12) field[11].u8Local() else null
    val psd20 = if (declaredLength >= 13 && field.size >= 13) field[12].u8Local() else null
    val mld = if (declaredLength >= 16 && field.size >= 16) {
        val raw = field.u16leLocal(14)
        RnrMldParameters(
            apMldId = field[13].u8Local(),
            linkId = raw and 0x0f,
            bssParametersChangeCount = (raw ushr 4) and 0xff,
            allUpdates = raw and 0x1000 != 0,
            disabledLink = raw and 0x2000 != 0,
        )
    } else {
        null
    }
    return RnrTbtt(
        offset = field[0].u8Local(),
        bssid = bssid,
        shortSsidHex = shortSsid,
        bssParameters = bssParameters,
        psd20Mhz = psd20,
        mld = mld,
        rawHex = field.toHex(),
        truncated = field.size != declaredLength,
    )
}

private fun rnrFieldFlags(infoLength: Int): List<String> = buildList {
    add("tbtt_offset")
    if (infoLength >= 7) add("bssid")
    if (infoLength >= 11) add("short_ssid")
    if (infoLength >= 12) add("bss_params")
    if (infoLength >= 13) add("psd20")
    if (infoLength >= 16) add("mld_params")
}

internal fun formatWifiMloMultipleBssidDetails(label: String, elements: List<WifiInformationElement>): List<String> {
    val values = elements
        .filter { it.id == MULTIPLE_BSSID_ELEMENT_ID }
        .mapNotNull { parseMultipleBssid(hexToBytesLocal(it.bytesHex)) }
    if (values.isEmpty()) return emptyList()
    return buildList {
        add(label)
        values.forEach { value ->
            val truncated = if (value.truncated) " truncated=true" else ""
            add("  mbssid max_indicator=${value.maxBssidIndicator} profiles=${value.profiles.size}$truncated")
            value.profiles.forEach { profile ->
                val parts = mutableListOf(
                    "profile #${profile.index}",
                    "len=${profile.declaredLength}",
                    "actual=${profile.actualLength}",
                    "ssid=${empty(profile.ssid, "<unknown>")}",
                )
                profile.bssidIndex?.let { parts += "bssid_index=$it" }
                profile.dtimPeriod?.let { parts += "dtim_period=$it" }
                if (profile.bssidCapabilityHex.isNotBlank()) parts += "cap=0x${profile.bssidCapabilityHex}"
                if (profile.nonInheritanceIds.isNotEmpty() || profile.nonInheritanceExts.isNotEmpty()) {
                    parts += "noninherit=ids:${joined(profile.nonInheritanceIds.map { it.toString() })}/ext:${joined(profile.nonInheritanceExts.map { it.toString() })}"
                }
                if (profile.truncated) parts += "truncated=true"
                parts += "ies=${joined(profile.elements.map { it.name })}"
                add("  ${parts.joinToString(" ")}")
                profile.security?.let { security ->
                    add("  profile_security #${profile.index}")
                    wifiMloSecuritySummaryLines(security).forEach { line -> add("    $line") }
                }
            }
        }
    }
}

private fun parseMultipleBssid(bytes: ByteArray): MultipleBssid? {
    if (bytes.isEmpty()) return null
    var offset = 1
    var index = 0
    var truncated = false
    val profiles = mutableListOf<MultipleBssidProfile>()
    while (offset < bytes.size) {
        if (offset + 2 > bytes.size) {
            truncated = true
            break
        }
        val id = bytes[offset].u8Local()
        val declaredLength = bytes[offset + 1].u8Local()
        val dataStart = offset + 2
        val dataEnd = (dataStart + declaredLength).coerceAtMost(bytes.size)
        val data = bytes.copyOfRange(dataStart, dataEnd)
        val subelementTruncated = data.size != declaredLength
        if (id == NONTRANSMITTED_BSSID_PROFILE_SUBELEMENT_ID) {
            index++
            profiles += parseMultipleBssidProfile(index, declaredLength, data, subelementTruncated)
        }
        truncated = truncated || subelementTruncated
        if (subelementTruncated) break
        offset = dataEnd
    }
    return MultipleBssid(bytes[0].u8Local(), profiles, truncated)
}

private fun parseMultipleBssidProfile(
    index: Int,
    declaredLength: Int,
    data: ByteArray,
    truncated: Boolean,
): MultipleBssidProfile {
    val (elements, elementsTruncated) = parseProfileInformationElements(data)
    var ssid = ""
    var bssidIndex: Int? = null
    var dtimPeriod: Int? = null
    var capHex = ""
    var noninheritIds = emptyList<Int>()
    var noninheritExts = emptyList<Int>()
    elements.forEach { element ->
        when (element.id) {
            0 -> ssid = runCatching { String(hexToBytesLocal(element.bodyHex)) }.getOrDefault("")
            MULTIPLE_BSSID_INDEX_ID -> {
                val body = hexToBytesLocal(element.bodyHex)
                if (body.isNotEmpty()) bssidIndex = body[0].u8Local()
                if (body.size > 1) dtimPeriod = body[1].u8Local()
            }
            NONTRANSMITTED_BSSID_CAPABILITY_ID -> {
                val body = hexToBytesLocal(element.bodyHex)
                if (body.size >= 2) capHex = body.copyOfRange(0, 2).toHex()
            }
            EXTENSION_ELEMENT_ID -> if (element.idExt == NON_INHERITANCE_ID_EXT) {
                val parsed = parseNonInheritance(element.bodyHex)
                noninheritIds = parsed.first
                noninheritExts = parsed.second
            }
        }
    }
    return MultipleBssidProfile(
        index = index,
        declaredLength = declaredLength,
        actualLength = data.size,
        ssid = ssid,
        bssidIndex = bssidIndex,
        dtimPeriod = dtimPeriod,
        bssidCapabilityHex = capHex,
        nonInheritanceIds = noninheritIds,
        nonInheritanceExts = noninheritExts,
        elements = elements,
        security = securityDetailsFromProfileElements(elements),
        truncated = truncated || elementsTruncated,
    )
}

private fun parseNonInheritance(bodyHex: String): Pair<List<Int>, List<Int>> {
    val body = hexToBytesLocal(bodyHex)
    if (body.isEmpty()) return emptyList<Int>() to emptyList()
    var offset = 1
    if (offset >= body.size) return emptyList<Int>() to emptyList()
    val idCount = body[offset++].u8Local()
    val ids = mutableListOf<Int>()
    repeat(idCount) {
        if (offset < body.size) ids += body[offset++].u8Local()
    }
    if (offset >= body.size) return ids to emptyList()
    val extCount = body[offset++].u8Local()
    val exts = mutableListOf<Int>()
    repeat(extCount) {
        if (offset < body.size) exts += body[offset++].u8Local()
    }
    return ids to exts
}

private fun List<WifiInformationElement>.hasElement(id: Int, idExt: Int? = null): Boolean =
    any { it.id == id && (idExt == null || it.idExt == idExt) }

private fun informationElementBitLocal(element: WifiInformationElement, bit: Int): Boolean {
    if (bit < 0) return false
    val bytes = hexToBytesLocal(element.bytesHex)
    val byteIndex = bit / 8
    if (byteIndex >= bytes.size) return false
    return bytes[byteIndex].u8Local() and (1 shl (bit % 8)) != 0
}

private fun mloMldMacFromElements(elements: List<WifiInformationElement>): String =
    parseEhtMultiLinkElements(elements)
        .firstNotNullOfOrNull { it.commonInfo?.mldMacAddress?.takeIf(String::isNotBlank) }
        .orEmpty()

private fun mloCurrentLinkIdFromElements(elements: List<WifiInformationElement>): Int? =
    parseEhtMultiLinkElements(elements).firstNotNullOfOrNull { it.commonInfo?.linkId }

private fun empty(value: String, fallback: String): String = value.ifBlank { fallback }

private fun joined(values: Iterable<String>, emptyValue: String = "<none>"): String =
    values.filter { it.isNotBlank() }.distinct().joinToString(",").ifBlank { emptyValue }

private fun hexToBytesLocal(value: String): ByteArray {
    val hex = value.trim()
    if (hex.length % 2 != 0) return ByteArray(0)
    return runCatching {
        ByteArray(hex.length / 2) { index ->
            hex.substring(index * 2, index * 2 + 2).toInt(16).toByte()
        }
    }.getOrDefault(ByteArray(0))
}

private fun ByteArray.u16leLocal(offset: Int): Int =
    this[offset].u8Local() or (this[offset + 1].u8Local() shl 8)

private fun ByteArray.readMac(offset: Int): String =
    copyOfRange(offset, offset + 6).joinToString(":") { "%02x".format(it.u8Local()) }

private fun Byte.u8Local(): Int = toInt() and 0xff

private fun Int.toHex16(): String = "%04x".format(this and 0xffff)

private fun String.hexPreview(maxBytes: Int = 32): String {
    val maxChars = maxBytes * 2
    if (length <= maxChars) return this
    val remaining = (length - maxChars) / 2
    return "${take(maxChars)}...(+${remaining}B)"
}

private data class RnrNeighbor(
    val headerRaw: Int,
    val type: Int,
    val filtered: Boolean,
    val count: Int,
    val infoLength: Int,
    val operatingClass: Int,
    val channel: Int,
    val tbtts: List<RnrTbtt>,
    val truncated: Boolean,
)

private data class RnrOperatingClass(
    val band: String = "",
    val width: String = "",
)

private data class RnrResolvedChannel(
    val band: String,
    val width: String,
    val frequency: String,
)

private data class RnrTbtt(
    val offset: Int = 0,
    val bssid: String = "",
    val shortSsidHex: String = "",
    val bssParameters: Int? = null,
    val psd20Mhz: Int? = null,
    val mld: RnrMldParameters? = null,
    val rawHex: String = "",
    val truncated: Boolean = false,
)

private data class RnrMldParameters(
    val apMldId: Int,
    val linkId: Int,
    val bssParametersChangeCount: Int,
    val allUpdates: Boolean,
    val disabledLink: Boolean,
)

private data class MultipleBssid(
    val maxBssidIndicator: Int,
    val profiles: List<MultipleBssidProfile>,
    val truncated: Boolean,
)

private data class MultipleBssidProfile(
    val index: Int,
    val declaredLength: Int,
    val actualLength: Int,
    val ssid: String,
    val bssidIndex: Int?,
    val dtimPeriod: Int?,
    val bssidCapabilityHex: String,
    val nonInheritanceIds: List<Int>,
    val nonInheritanceExts: List<Int>,
    val elements: List<EhtProfileInformationElement>,
    val security: WifiSecurityDetails?,
    val truncated: Boolean,
)
