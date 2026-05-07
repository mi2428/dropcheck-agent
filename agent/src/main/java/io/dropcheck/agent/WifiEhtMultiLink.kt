package io.dropcheck.agent

import io.dropcheck.agent.grpc.WifiInformationElement

private const val EHT_MULTI_LINK_ELEMENT_ID = 255
private const val EHT_MULTI_LINK_ID_EXT = 107
private const val MULTI_LINK_TYPE_BASIC = 0
private const val PER_STA_PROFILE_SUBELEMENT_ID = 0
private const val LEGACY_FRAGMENT_SUBELEMENT_ID = 1
private const val LEGACY_VENDOR_SPECIFIC_SUBELEMENT_ID = 2
private const val VENDOR_SPECIFIC_SUBELEMENT_ID = 221
private const val FRAGMENT_SUBELEMENT_ID = 254

internal data class EhtMultiLinkElement(
    val control: Int,
    val type: Int,
    val typeName: String,
    val presence: List<String>,
    val commonInfo: EhtMultiLinkCommonInfo?,
    val subelements: List<EhtMultiLinkSubelement>,
    val rawByteCount: Int,
    val truncated: Boolean,
)

internal data class EhtMultiLinkCommonInfo(
    val length: Int,
    val mldMacAddress: String,
    val linkId: Int?,
    val bssParametersChangeCount: Int?,
    val mediumSynchronizationDelayInfoHex: String,
    val emlCapabilitiesHex: String,
    val mldCapabilitiesAndOperationsHex: String,
    val apMldId: Int?,
)

internal data class EhtMultiLinkSubelement(
    val id: Int,
    val name: String,
    val declaredLength: Int,
    val actualLength: Int,
    val perStaProfile: EhtPerStaProfile?,
    val vendorSpecific: EhtVendorSpecificSubelement?,
    val fragment: EhtFragmentSubelement?,
    val fragmentCount: Int,
    val reassembledLength: Int?,
    val rawHex: String,
    val truncated: Boolean,
)

internal data class EhtVendorSpecificSubelement(
    val oui: String,
    val vendorType: Int?,
    val payloadByteCount: Int,
    val payloadHex: String,
)

internal data class EhtFragmentSubelement(
    val targetSubelementId: Int?,
    val targetSubelementName: String,
    val byteCount: Int,
    val rawHex: String,
)

internal data class EhtPerStaProfile(
    val control: Int,
    val linkId: Int,
    val completeProfile: Boolean,
    val flags: List<String>,
    val staInfoLength: Int?,
    val staMacAddress: String,
    val beaconIntervalTu: Int?,
    val tsfOffsetHex: String,
    val dtimCount: Int?,
    val dtimPeriod: Int?,
    val nstrLinkPairHex: String,
    val bssParametersChangeCount: Int?,
    val profileByteCount: Int,
    val profileHex: String,
    val profileElements: List<EhtProfileInformationElement>,
    val profileElementsTruncated: Boolean,
    val truncated: Boolean,
)

internal data class EhtProfileInformationElement(
    val id: Int,
    val idExt: Int?,
    val name: String,
    val declaredLength: Int,
    val actualLength: Int,
    val bodyHex: String,
    val truncated: Boolean,
)

internal fun parseEhtMultiLinkElements(elements: List<WifiInformationElement>): List<EhtMultiLinkElement> {
    return elements
        .filter { it.id == EHT_MULTI_LINK_ELEMENT_ID && it.idExt == EHT_MULTI_LINK_ID_EXT }
        .mapNotNull { parseEhtMultiLinkElement(hexToBytes(it.bytesHex), it.byteCount) }
}

internal fun formatEhtMultiLinkElements(
    label: String,
    elements: List<EhtMultiLinkElement>,
): List<String> {
    if (elements.isEmpty()) return emptyList()
    val lines = mutableListOf<String>()
    elements.forEachIndexed { index, element ->
        val suffix = if (elements.size > 1) " #${index + 1}" else ""
        lines += "$label$suffix"
        lines += "  ml_control raw=0x${element.control.toHex16()} type=${element.typeName}(${element.type}) presence=${element.presence.joinedOrNone()} bytes=${element.rawByteCount}${if (element.truncated) " truncated=true" else ""}"
        element.commonInfo?.let { common ->
            lines += buildString {
                append("  common_info len=${common.length}")
                if (common.mldMacAddress.isNotBlank()) append(" mld_mac=${common.mldMacAddress}")
                common.linkId?.let { append(" link_id=$it") }
                common.bssParametersChangeCount?.let { append(" bss_param_change_count=$it") }
                if (common.mediumSynchronizationDelayInfoHex.isNotBlank()) append(" medium_sync_delay=0x${common.mediumSynchronizationDelayInfoHex}")
                if (common.emlCapabilitiesHex.isNotBlank()) append(" eml_capabilities=0x${common.emlCapabilitiesHex}")
                if (common.mldCapabilitiesAndOperationsHex.isNotBlank()) append(" mld_capabilities=0x${common.mldCapabilitiesAndOperationsHex}")
                common.apMldId?.let { append(" ap_mld_id=$it") }
            }
        }
        element.subelements.forEach { subelement ->
            lines += buildString {
                append("  subelement id=${subelement.id} name=${subelement.name} len=${subelement.declaredLength} actual=${subelement.actualLength}")
                if (subelement.fragmentCount > 0) {
                    append(" fragments=${subelement.fragmentCount}")
                    subelement.reassembledLength?.let { append(" reassembled=$it") }
                }
                if (subelement.truncated) append(" truncated=true")
            }
            subelement.fragment?.let { fragment ->
                lines += buildString {
                    append("  fragment target_id=${fragment.targetSubelementId ?: "?"} target=${fragment.targetSubelementName}")
                    append(" bytes=${fragment.byteCount}")
                    if (fragment.rawHex.isNotBlank()) append(" payload=0x${fragment.rawHex.hexPreview()}")
                }
            }
            subelement.vendorSpecific?.let { vendor ->
                lines += buildString {
                    append("  vendor oui=${vendor.oui.ifBlank { "<unknown>" }}")
                    vendor.vendorType?.let { append(" type=$it") }
                    append(" payload_bytes=${vendor.payloadByteCount}")
                    if (vendor.payloadHex.isNotBlank()) append(" payload=0x${vendor.payloadHex.hexPreview()}")
                }
            }
            val perSta = subelement.perStaProfile ?: return@forEach
            lines += buildString {
                append("  per_link link_id=${perSta.linkId} control=0x${perSta.control.toHex16()}")
                append(" complete=${perSta.completeProfile}")
                append(" flags=${perSta.flags.joinedOrNone()}")
                perSta.staInfoLength?.let { append(" sta_info_len=$it") }
                if (perSta.staMacAddress.isNotBlank()) append(" sta_mac=${perSta.staMacAddress}")
                perSta.beaconIntervalTu?.let { append(" beacon_interval_tu=$it") }
                if (perSta.tsfOffsetHex.isNotBlank()) append(" tsf_offset=0x${perSta.tsfOffsetHex}")
                if (perSta.dtimCount != null || perSta.dtimPeriod != null) {
                    append(" dtim=${perSta.dtimCount ?: "?"}/${perSta.dtimPeriod ?: "?"}")
                }
                if (perSta.nstrLinkPairHex.isNotBlank()) append(" nstr_link_pair=0x${perSta.nstrLinkPairHex}")
                perSta.bssParametersChangeCount?.let { append(" bss_param_change_count=$it") }
                append(" profile_bytes=${perSta.profileByteCount}")
                if (perSta.truncated) append(" truncated=true")
            }
            perSta.profileElements.forEach { profile ->
                lines += buildString {
                    append("  profile_ie link_id=${perSta.linkId} id=${profile.id}")
                    profile.idExt?.let { append(" ext=$it") }
                    append(" name=${profile.name}")
                    append(" len=${profile.declaredLength} actual=${profile.actualLength}")
                    if (profile.truncated) append(" truncated=true")
                    if (profile.bodyHex.isNotBlank()) append(" body=0x${profile.bodyHex.hexPreview()}")
                }
            }
            if (perSta.profileElementsTruncated && perSta.profileElements.isEmpty() && perSta.profileHex.isNotBlank()) {
                lines += "  profile_unparsed link_id=${perSta.linkId} bytes=${perSta.profileByteCount} body=0x${perSta.profileHex.hexPreview()}"
            }
        }
    }
    return lines
}

private fun parseEhtMultiLinkElement(bytes: ByteArray, fallbackByteCount: Int): EhtMultiLinkElement? {
    if (bytes.size < 2) return null
    val control = bytes.u16le(0)
    val type = control and 0x7
    var truncated = false
    var commonInfo: EhtMultiLinkCommonInfo? = null
    var offset = 2
    if (offset < bytes.size) {
        val commonLength = bytes[offset].u8()
        val commonStart = offset
        val declaredCommonEnd = commonStart + commonLength
        val commonEnd = declaredCommonEnd.coerceAtMost(bytes.size)
        if (commonLength == 0 || declaredCommonEnd > bytes.size) truncated = true
        if (type == MULTI_LINK_TYPE_BASIC) {
            val parsed = parseBasicCommonInfo(bytes, commonStart, commonEnd, control, commonLength)
            commonInfo = parsed.first
            truncated = truncated || parsed.second
        } else {
            commonInfo = EhtMultiLinkCommonInfo(commonLength, "", null, null, "", "", "", null)
        }
        offset = commonEnd
    }
    val rawSubelements = mutableListOf<RawEhtMultiLinkSubelement>()
    while (offset < bytes.size) {
        if (offset + 2 > bytes.size) {
            truncated = true
            break
        }
        val id = bytes[offset].u8()
        val declaredLength = bytes[offset + 1].u8()
        val dataStart = offset + 2
        val dataEnd = (dataStart + declaredLength).coerceAtMost(bytes.size)
        val data = bytes.copyOfRange(dataStart, dataEnd)
        val subelementTruncated = data.size != declaredLength
        truncated = truncated || subelementTruncated
        rawSubelements += RawEhtMultiLinkSubelement(
            id = id,
            declaredLength = declaredLength,
            data = data,
            truncated = subelementTruncated,
        )
        if (subelementTruncated) break
        offset = dataEnd
    }
    return EhtMultiLinkElement(
        control = control,
        type = type,
        typeName = multiLinkTypeName(type),
        presence = basicMultiLinkPresence(control),
        commonInfo = commonInfo,
        subelements = parseSubelements(rawSubelements),
        rawByteCount = fallbackByteCount.takeIf { it > 0 } ?: bytes.size,
        truncated = truncated,
    )
}

private data class RawEhtMultiLinkSubelement(
    val id: Int,
    val declaredLength: Int,
    val data: ByteArray,
    val truncated: Boolean,
)

private fun parseSubelements(rawSubelements: List<RawEhtMultiLinkSubelement>): List<EhtMultiLinkSubelement> {
    val fragmentsByAnchor = mutableMapOf<Int, MutableList<ByteArray>>()
    val fragmentTargets = mutableMapOf<Int, Int?>()
    var anchorIndex: Int? = null
    rawSubelements.forEachIndexed { index, raw ->
        if (isFragmentSubelement(raw.id)) {
            fragmentTargets[index] = anchorIndex
            anchorIndex?.let {
                fragmentsByAnchor.getOrPut(it) { mutableListOf() }.add(raw.data)
            }
        } else {
            anchorIndex = index
        }
    }
    return rawSubelements.mapIndexed { index, raw ->
        val fragments = fragmentsByAnchor[index].orEmpty()
        val reassembledData = if (fragments.isEmpty()) raw.data else concatByteArrays(listOf(raw.data) + fragments)
        val fragmentTarget = fragmentTargets[index]
        val vendorData = if (isVendorSpecificSubelement(raw.id)) parseVendorSpecificSubelement(raw.data) else null
        EhtMultiLinkSubelement(
            id = raw.id,
            name = multiLinkSubelementName(raw.id),
            declaredLength = raw.declaredLength,
            actualLength = raw.data.size,
            perStaProfile = if (raw.id == PER_STA_PROFILE_SUBELEMENT_ID) parsePerStaProfile(reassembledData, raw.truncated) else null,
            vendorSpecific = vendorData,
            fragment = if (isFragmentSubelement(raw.id)) {
                EhtFragmentSubelement(
                    targetSubelementId = fragmentTarget?.let { rawSubelements[it].id },
                    targetSubelementName = fragmentTarget?.let { multiLinkSubelementName(rawSubelements[it].id) } ?: "<none>",
                    byteCount = raw.data.size,
                    rawHex = raw.data.toHex(),
                )
            } else {
                null
            },
            fragmentCount = fragments.size,
            reassembledLength = if (fragments.isEmpty()) null else reassembledData.size,
            rawHex = raw.data.toHex(),
            truncated = raw.truncated,
        )
    }
}

private fun parseBasicCommonInfo(
    bytes: ByteArray,
    commonStart: Int,
    commonEnd: Int,
    control: Int,
    commonLength: Int,
): Pair<EhtMultiLinkCommonInfo, Boolean> {
    var offset = commonStart + 1
    var truncated = false
    val mldMac = readMac(bytes, offset, commonEnd)
    if (mldMac.isBlank()) truncated = true
    offset += 6
    val linkId = if (control.hasFlag(4)) readU8(bytes, offset++, commonEnd)?.and(0x0f).also { if (it == null) truncated = true } else null
    val bssChangeCount = if (control.hasFlag(5)) readU8(bytes, offset++, commonEnd).also { if (it == null) truncated = true } else null
    val mediumSync = if (control.hasFlag(6)) readHex(bytes, offset, 2, commonEnd).also { if (it.isBlank()) truncated = true }.also { offset += 2 } else ""
    val eml = if (control.hasFlag(7)) readHex(bytes, offset, 2, commonEnd).also { if (it.isBlank()) truncated = true }.also { offset += 2 } else ""
    val mldCaps = if (control.hasFlag(8)) readHex(bytes, offset, 2, commonEnd).also { if (it.isBlank()) truncated = true }.also { offset += 2 } else ""
    val apMldId = if (control.hasFlag(9)) readU8(bytes, offset++, commonEnd).also { if (it == null) truncated = true } else null
    return EhtMultiLinkCommonInfo(commonLength, mldMac, linkId, bssChangeCount, mediumSync, eml, mldCaps, apMldId) to truncated
}

private fun parsePerStaProfile(data: ByteArray, alreadyTruncated: Boolean): EhtPerStaProfile? {
    if (data.size < 3) return null
    val control = data.u16le(0)
    val staInfoLength = data[2].u8()
    val staInfoStart = 3
    val staInfoEnd = (2 + staInfoLength).coerceAtMost(data.size)
    var offset = staInfoStart
    var truncated = alreadyTruncated || staInfoLength == 0 || 2 + staInfoLength > data.size

    fun readOptionalBytes(bit: Int, length: Int): String {
        if (!control.hasFlag(bit)) return ""
        val value = readHex(data, offset, length, staInfoEnd)
        if (value.isBlank()) truncated = true
        offset += length
        return value
    }

    val staMac = if (control.hasFlag(5)) readMac(data, offset, staInfoEnd).also {
        if (it.isBlank()) truncated = true
        offset += 6
    } else ""
    val beaconInterval = if (control.hasFlag(6)) readU16(data, offset, staInfoEnd).also {
        if (it == null) truncated = true
        offset += 2
    } else null
    val tsfOffset = readOptionalBytes(7, 8)
    var dtimCount: Int? = null
    var dtimPeriod: Int? = null
    if (control.hasFlag(8)) {
        dtimCount = readU8(data, offset, staInfoEnd)
        dtimPeriod = readU8(data, offset + 1, staInfoEnd)
        if (dtimCount == null || dtimPeriod == null) truncated = true
        offset += 2
    }
    val nstrLinkPair = if (control.hasFlag(9)) {
        readHex(data, offset, if (control.hasFlag(10)) 2 else 1, staInfoEnd).also {
            if (it.isBlank()) truncated = true
            offset += if (control.hasFlag(10)) 2 else 1
        }
    } else ""
    val bssChangeCount = if (control.hasFlag(11)) readU8(data, offset++, staInfoEnd).also { if (it == null) truncated = true } else null
    val profileStart = staInfoEnd.coerceAtMost(data.size)
    val profile = data.copyOfRange(profileStart, data.size)
    val parsedProfile = parseProfileInformationElements(profile)
    return EhtPerStaProfile(
        control = control,
        linkId = control and 0x0f,
        completeProfile = control.hasFlag(4),
        flags = perStaProfileFlags(control),
        staInfoLength = staInfoLength,
        staMacAddress = staMac,
        beaconIntervalTu = beaconInterval,
        tsfOffsetHex = tsfOffset,
        dtimCount = dtimCount,
        dtimPeriod = dtimPeriod,
        nstrLinkPairHex = nstrLinkPair,
        bssParametersChangeCount = bssChangeCount,
        profileByteCount = profile.size,
        profileHex = profile.toHex(),
        profileElements = parsedProfile.first,
        profileElementsTruncated = parsedProfile.second,
        truncated = truncated,
    )
}

private fun parseVendorSpecificSubelement(data: ByteArray): EhtVendorSpecificSubelement {
    val oui = if (data.size >= 3) {
        data.copyOfRange(0, 3).joinToString(":") { "%02x".format(it.u8()) }
    } else {
        ""
    }
    val vendorType = if (data.size >= 4) data[3].u8() else null
    val payloadStart = if (data.size >= 4) 4 else data.size
    val payload = data.copyOfRange(payloadStart, data.size)
    return EhtVendorSpecificSubelement(
        oui = oui,
        vendorType = vendorType,
        payloadByteCount = payload.size,
        payloadHex = payload.toHex(),
    )
}

private fun parseProfileInformationElements(data: ByteArray): Pair<List<EhtProfileInformationElement>, Boolean> {
    if (data.isEmpty()) return emptyList<EhtProfileInformationElement>() to false
    val elements = mutableListOf<EhtProfileInformationElement>()
    var offset = 0
    var truncated = false
    while (offset < data.size) {
        if (offset + 2 > data.size) {
            truncated = true
            break
        }
        val id = data[offset].u8()
        val declaredLength = data[offset + 1].u8()
        val bodyStart = offset + 2
        val bodyEnd = (bodyStart + declaredLength).coerceAtMost(data.size)
        val body = data.copyOfRange(bodyStart, bodyEnd)
        val elementTruncated = body.size != declaredLength
        truncated = truncated || elementTruncated
        val idExt = if (id == EHT_MULTI_LINK_ELEMENT_ID && body.isNotEmpty()) body[0].u8() else null
        elements += EhtProfileInformationElement(
            id = id,
            idExt = idExt,
            name = profileInformationElementName(id, idExt),
            declaredLength = declaredLength,
            actualLength = body.size,
            bodyHex = body.toHex(),
            truncated = elementTruncated,
        )
        if (elementTruncated) break
        offset = bodyEnd
    }
    return elements to truncated
}

private fun basicMultiLinkPresence(control: Int): List<String> = buildList {
    if (control.hasFlag(4)) add("link_id_info")
    if (control.hasFlag(5)) add("bss_parameters_change_count")
    if (control.hasFlag(6)) add("medium_synchronization_delay")
    if (control.hasFlag(7)) add("eml_capabilities")
    if (control.hasFlag(8)) add("mld_capabilities_and_operations")
    if (control.hasFlag(9)) add("ap_mld_id")
}

private fun perStaProfileFlags(control: Int): List<String> = buildList {
    if (control.hasFlag(4)) add("complete_profile")
    if (control.hasFlag(5)) add("mac_address")
    if (control.hasFlag(6)) add("beacon_interval")
    if (control.hasFlag(7)) add("tsf_offset")
    if (control.hasFlag(8)) add("dtim_info")
    if (control.hasFlag(9)) add("nstr_link_pair")
    if (control.hasFlag(10)) add("nstr_bitmap_size")
    if (control.hasFlag(11)) add("bss_parameters_change_count")
}

private fun multiLinkTypeName(type: Int): String = when (type) {
    0 -> "basic"
    1 -> "probe_request"
    2 -> "reconfiguration"
    3 -> "tdls"
    4 -> "priority_access"
    else -> "reserved"
}

private fun multiLinkSubelementName(id: Int): String = when (id) {
    0 -> "per_sta_profile"
    LEGACY_FRAGMENT_SUBELEMENT_ID -> "fragment_legacy"
    LEGACY_VENDOR_SPECIFIC_SUBELEMENT_ID -> "vendor_specific_legacy"
    VENDOR_SPECIFIC_SUBELEMENT_ID -> "vendor_specific"
    FRAGMENT_SUBELEMENT_ID -> "fragment"
    else -> "subelement_$id"
}

private fun isFragmentSubelement(id: Int): Boolean =
    id == FRAGMENT_SUBELEMENT_ID || id == LEGACY_FRAGMENT_SUBELEMENT_ID

private fun isVendorSpecificSubelement(id: Int): Boolean =
    id == VENDOR_SPECIFIC_SUBELEMENT_ID || id == LEGACY_VENDOR_SPECIFIC_SUBELEMENT_ID

private fun profileInformationElementName(id: Int, idExt: Int?): String {
    if (id == EHT_MULTI_LINK_ELEMENT_ID) {
        return when (idExt) {
            106 -> "eht_operation"
            107 -> "eht_multi_link"
            108 -> "eht_capabilities"
            null -> "extension"
            else -> "extension_$idExt"
        }
    }
    return when (id) {
        0 -> "ssid"
        1 -> "supported_rates"
        3 -> "dsss_parameter_set"
        5 -> "tim"
        7 -> "country"
        11 -> "bss_load"
        32 -> "power_constraint"
        35 -> "tpc_report"
        45 -> "ht_capabilities"
        48 -> "rsn"
        50 -> "extended_supported_rates"
        61 -> "ht_operation"
        70 -> "radio_measurement_11k"
        74 -> "overlapping_bss_scan_parameters"
        127 -> "extended_capabilities"
        191 -> "vht_capabilities"
        192 -> "vht_operation"
        201 -> "reduced_neighbor_report"
        221 -> "vendor_specific"
        else -> "element_$id"
    }
}

private fun readMac(bytes: ByteArray, offset: Int, limit: Int): String {
    if (offset + 6 > limit || offset + 6 > bytes.size) return ""
    return bytes.copyOfRange(offset, offset + 6).joinToString(":") { "%02x".format(it.u8()) }
}

private fun readHex(bytes: ByteArray, offset: Int, length: Int, limit: Int): String {
    if (length <= 0 || offset + length > limit || offset + length > bytes.size) return ""
    return bytes.copyOfRange(offset, offset + length).toHex()
}

private fun readU8(bytes: ByteArray, offset: Int, limit: Int): Int? {
    if (offset >= limit || offset >= bytes.size) return null
    return bytes[offset].u8()
}

private fun readU16(bytes: ByteArray, offset: Int, limit: Int): Int? {
    if (offset + 2 > limit || offset + 2 > bytes.size) return null
    return bytes.u16le(offset)
}

private fun concatByteArrays(chunks: List<ByteArray>): ByteArray {
    val total = chunks.sumOf { it.size }
    val out = ByteArray(total)
    var offset = 0
    chunks.forEach { chunk ->
        chunk.copyInto(out, offset)
        offset += chunk.size
    }
    return out
}

private fun hexToBytes(value: String): ByteArray {
    val hex = value.trim()
    if (hex.length % 2 != 0) return ByteArray(0)
    return runCatching {
        ByteArray(hex.length / 2) { index ->
            hex.substring(index * 2, index * 2 + 2).toInt(16).toByte()
        }
    }.getOrDefault(ByteArray(0))
}

private fun ByteArray.u16le(offset: Int): Int {
    return this[offset].u8() or (this[offset + 1].u8() shl 8)
}

private fun Byte.u8(): Int = toInt() and 0xff

private fun Int.hasFlag(bit: Int): Boolean = this and (1 shl bit) != 0

private fun Int.toHex16(): String = "%04x".format(this and 0xffff)

private fun List<String>.joinedOrNone(): String = if (isEmpty()) "<none>" else joinToString(",")

private fun String.hexPreview(maxBytes: Int = 32): String {
    val maxChars = maxBytes * 2
    if (length <= maxChars) return this
    val remainingBytes = ((length - maxChars) / 2).coerceAtLeast(0)
    return take(maxChars) + "...(+${remainingBytes}B)"
}
