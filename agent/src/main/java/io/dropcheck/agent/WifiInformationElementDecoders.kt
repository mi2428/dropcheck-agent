package io.dropcheck.agent

import io.dropcheck.agent.grpc.WifiEhtCapabilities
import io.dropcheck.agent.grpc.WifiEhtOperation
import io.dropcheck.agent.grpc.WifiHeCapabilities
import io.dropcheck.agent.grpc.WifiHeMuEdcaAcRecord
import io.dropcheck.agent.grpc.WifiHeMuEdcaParameterSet
import io.dropcheck.agent.grpc.WifiHeOperation
import io.dropcheck.agent.grpc.WifiHeSpatialReuseParameterSet
import io.dropcheck.agent.grpc.WifiHeUoraParameterSet
import io.dropcheck.agent.grpc.WifiInformationElement
import io.dropcheck.agent.grpc.WifiMcsNssSupport

private const val EXTENSION_ELEMENT_ID = 255
private const val HE_CAPABILITIES_ID_EXT = 35
private const val HE_OPERATION_ID_EXT = 36
private const val HE_UORA_ID_EXT = 37
private const val HE_MU_EDCA_ID_EXT = 38
private const val HE_SPATIAL_REUSE_ID_EXT = 39
private const val EHT_OPERATION_ID_EXT = 106
private const val EHT_CAPABILITIES_ID_EXT = 108

internal data class WifiInformationElementDecodes(
    val heCapabilities: WifiHeCapabilities? = null,
    val heOperation: WifiHeOperation? = null,
    val ehtCapabilities: WifiEhtCapabilities? = null,
    val ehtOperation: WifiEhtOperation? = null,
    val heUoraParameterSet: WifiHeUoraParameterSet? = null,
    val heMuEdcaParameterSet: WifiHeMuEdcaParameterSet? = null,
    val heSpatialReuseParameterSet: WifiHeSpatialReuseParameterSet? = null,
)

internal fun decodeWifiInformationElements(elements: List<WifiInformationElement>): WifiInformationElementDecodes {
    val heCapabilitiesElement = elements.firstExtension(HE_CAPABILITIES_ID_EXT)
    val heCapabilities = heCapabilitiesElement?.let { parseHeCapabilities(it.bytes()) }
    return WifiInformationElementDecodes(
        heCapabilities = heCapabilities,
        heOperation = elements.firstExtension(HE_OPERATION_ID_EXT)?.let { parseHeOperation(it.bytes()) },
        ehtCapabilities = elements.firstExtension(EHT_CAPABILITIES_ID_EXT)?.let {
            parseEhtCapabilities(it.bytes(), heCapabilities)
        },
        ehtOperation = elements.firstExtension(EHT_OPERATION_ID_EXT)?.let { parseEhtOperation(it.bytes()) },
        heUoraParameterSet = elements.firstExtension(HE_UORA_ID_EXT)?.let { parseHeUoraParameterSet(it.bytes()) },
        heMuEdcaParameterSet = elements.firstExtension(HE_MU_EDCA_ID_EXT)?.let { parseHeMuEdcaParameterSet(it.bytes()) },
        heSpatialReuseParameterSet = elements.firstExtension(HE_SPATIAL_REUSE_ID_EXT)?.let {
            parseHeSpatialReuseParameterSet(it.bytes())
        },
    )
}

private fun List<WifiInformationElement>.firstExtension(idExt: Int): WifiInformationElement? {
    return firstOrNull { it.id == EXTENSION_ELEMENT_ID && it.idExt == idExt }
}

private fun WifiInformationElement.bytes(): ByteArray = hexToBytes(bytesHex)

private fun parseHeCapabilities(bytes: ByteArray): WifiHeCapabilities {
    val builder = WifiHeCapabilities.newBuilder()
        .setRawHex(bytes.toHex())
    if (bytes.size < 17) {
        return builder
            .setTruncated(true)
            .addWarnings("he_capabilities_too_short bytes=${bytes.size} required=17")
            .build()
    }

    val mac = bytes.copyOfRange(0, 6)
    val phy = bytes.copyOfRange(6, 17)
    builder
        .setMacCapabilitiesHex(mac.toHex())
        .setPhyCapabilitiesHex(phy.toHex())
        .addAllFeatures(heCapabilityFeatures(mac, phy))

    val mcsSize = heMcsNssSize(phy)
    val mcsStart = 17
    if (bytes.size < mcsStart + mcsSize) {
        builder
            .setTruncated(true)
            .addWarnings("he_mcs_nss_too_short bytes=${(bytes.size - mcsStart).coerceAtLeast(0)} required=$mcsSize")
    } else {
        builder.addAllMcsNss(parseHeMcsNss(bytes.copyOfRange(mcsStart, mcsStart + mcsSize), phy))
    }

    val ppeStart = mcsStart + mcsSize
    if (phy[6].u8() and 0x80 != 0) {
        builder.ppeThresholdsPresent = true
        if (bytes.size <= ppeStart) {
            builder
                .setTruncated(true)
                .addWarnings("he_ppe_thresholds_missing")
        } else {
            val ppe = parseHePpe(bytes, ppeStart, phy)
            builder
                .setPpeNssCount(ppe.nssCount)
                .addAllPpeRuIndices(ppe.ruIndices)
                .setPpeThresholdsHex(ppe.hex)
            if (ppe.truncated) {
                builder
                    .setTruncated(true)
                    .addWarnings("he_ppe_thresholds_truncated bytes=${ppe.actualBytes} required=${ppe.requiredBytes}")
            }
        }
    }
    return builder.build()
}

private fun parseEhtCapabilities(bytes: ByteArray, heCapabilities: WifiHeCapabilities?): WifiEhtCapabilities {
    val builder = WifiEhtCapabilities.newBuilder()
        .setRawHex(bytes.toHex())
    if (bytes.size < 11) {
        return builder
            .setTruncated(true)
            .addWarnings("eht_capabilities_too_short bytes=${bytes.size} required=11")
            .build()
    }

    val mac = bytes.copyOfRange(0, 2)
    val phy = bytes.copyOfRange(2, 11)
    builder
        .setMacCapabilitiesHex(mac.toHex())
        .setPhyCapabilitiesHex(phy.toHex())
        .addAllFeatures(ehtCapabilityFeatures(mac, phy))

    val mcsSize = ehtMcsNssSize(phy, heCapabilities)
    val mcsStart = 11
    if (bytes.size < mcsStart + mcsSize) {
        builder
            .setTruncated(true)
            .addWarnings("eht_mcs_nss_too_short bytes=${(bytes.size - mcsStart).coerceAtLeast(0)} required=$mcsSize")
    } else {
        builder.addAllMcsNss(parseEhtMcsNss(bytes.copyOfRange(mcsStart, mcsStart + mcsSize), mcsSize))
    }

    val ppeStart = mcsStart + mcsSize
    if (phy[5].u8() and 0x08 != 0) {
        builder.ppeThresholdsPresent = true
        if (bytes.size < ppeStart + 2) {
            builder
                .setTruncated(true)
                .addWarnings("eht_ppe_thresholds_missing")
        } else {
            val ppe = parseEhtPpe(bytes, ppeStart, phy)
            builder
                .setPpeNssCount(ppe.nssCount)
                .addAllPpeRuIndices(ppe.ruIndices)
                .setPpeThresholdsHex(ppe.hex)
            if (ppe.truncated) {
                builder
                    .setTruncated(true)
                    .addWarnings("eht_ppe_thresholds_truncated bytes=${ppe.actualBytes} required=${ppe.requiredBytes}")
            }
        }
    }
    return builder.build()
}

private fun parseHeOperation(bytes: ByteArray): WifiHeOperation {
    val builder = WifiHeOperation.newBuilder()
        .setRawHex(bytes.toHex())
    if (bytes.size < 6) {
        return builder
            .setTruncated(true)
            .addWarnings("he_operation_too_short bytes=${bytes.size} required=6")
            .build()
    }

    val parameters = bytes.u32le(0)
    builder
        .setParameters(parameters)
        .setBssColor((parameters ushr 24) and 0x3f)
        .setBssColorDisabled(parameters and 0x80000000u.toInt() != 0)
        .setBasicMcsNssSetHex(bytes.copyOfRange(4, 6).toHex())
        .addAllFlags(heOperationFlags(parameters))

    var offset = 6
    if (parameters and 0x00004000 != 0) {
        offset += 3
    }
    if (parameters and 0x00008000 != 0) {
        offset += 1
    }
    if (parameters and 0x00020000 != 0) {
        if (bytes.size < offset + 5) {
            builder
                .setTruncated(true)
                .addWarnings("he_6ghz_operation_too_short bytes=${(bytes.size - offset).coerceAtLeast(0)} required=5")
        } else {
            val primary = bytes[offset].u8()
            val control = bytes[offset + 1].u8()
            builder
                .setPrimaryChannel(primary)
                .setChannelWidth(he6GhzChannelWidth(control and 0x03))
                .setCenterFreqSegment0(bytes[offset + 2].u8())
                .setCenterFreqSegment1(bytes[offset + 3].u8())
        }
    }
    return builder.build()
}

private fun parseEhtOperation(bytes: ByteArray): WifiEhtOperation {
    val builder = WifiEhtOperation.newBuilder()
        .setRawHex(bytes.toHex())
    if (bytes.size < 5) {
        return builder
            .setTruncated(true)
            .addWarnings("eht_operation_too_short bytes=${bytes.size} required=5")
            .build()
    }

    val parameters = bytes[0].u8()
    builder
        .setParameters(parameters)
        .setBasicMcsNssSetHex(bytes.copyOfRange(1, 5).toHex())
        .addAllFlags(ehtOperationFlags(parameters))

    var offset = 5
    if (parameters and 0x01 != 0) {
        if (bytes.size < offset + 3) {
            builder
                .setTruncated(true)
                .addWarnings("eht_operation_info_too_short bytes=${(bytes.size - offset).coerceAtLeast(0)} required=3")
            return builder.build()
        }
        val control = bytes[offset].u8()
        builder
            .setChannelWidth(ehtChannelWidth(control and 0x07))
            .setCenterFreqSegment0(bytes[offset + 1].u8())
            .setCenterFreqSegment1(bytes[offset + 2].u8())
        offset += 3
        if (parameters and 0x02 != 0) {
            if (bytes.size < offset + 2) {
                builder
                    .setTruncated(true)
                    .addWarnings("eht_disabled_subchannel_bitmap_too_short bytes=${(bytes.size - offset).coerceAtLeast(0)} required=2")
            } else {
                builder.disabledSubchannelBitmap = bytes.u16le(offset)
            }
        }
    } else if (parameters and 0x02 != 0) {
        builder.addWarnings("eht_disabled_subchannel_bitmap_without_operation_information")
    }
    return builder.build()
}

private fun parseHeUoraParameterSet(bytes: ByteArray): WifiHeUoraParameterSet {
    val builder = WifiHeUoraParameterSet.newBuilder()
        .setRawHex(bytes.toHex())
    if (bytes.isEmpty()) {
        return builder
            .setTruncated(true)
            .addWarnings("he_uora_parameter_set_empty")
            .build()
    }
    val value = bytes[0].u8()
    return builder
        .setEocwMin(value and 0x07)
        .setEocwMax((value ushr 3) and 0x07)
        .build()
}

private fun parseHeMuEdcaParameterSet(bytes: ByteArray): WifiHeMuEdcaParameterSet {
    val builder = WifiHeMuEdcaParameterSet.newBuilder()
        .setRawHex(bytes.toHex())
    if (bytes.size < 13) {
        builder
            .setTruncated(true)
            .addWarnings("he_mu_edca_parameter_set_too_short bytes=${bytes.size} required=13")
    }
    if (bytes.isEmpty()) return builder.build()
    builder.qosInfo = bytes[0].u8()
    val acNames = listOf("be", "bk", "vi", "vo")
    var offset = 1
    for (index in 0 until 4) {
        if (bytes.size < offset + 3) break
        val aciAifsn = bytes[offset].u8()
        val ecw = bytes[offset + 1].u8()
        builder.addAc(
            WifiHeMuEdcaAcRecord.newBuilder()
                .setAc(acNames[index])
                .setAci((aciAifsn ushr 5) and 0x03)
                .setAifsn(aciAifsn and 0x0f)
                .setAcm(aciAifsn and 0x10 != 0)
                .setEcwMin(ecw and 0x0f)
                .setEcwMax((ecw ushr 4) and 0x0f)
                .setTimer(bytes[offset + 2].u8())
                .setRawHex(bytes.copyOfRange(offset, offset + 3).toHex())
                .build(),
        )
        offset += 3
    }
    return builder.build()
}

private fun parseHeSpatialReuseParameterSet(bytes: ByteArray): WifiHeSpatialReuseParameterSet {
    val builder = WifiHeSpatialReuseParameterSet.newBuilder()
        .setRawHex(bytes.toHex())
    if (bytes.isEmpty()) {
        return builder
            .setTruncated(true)
            .addWarnings("he_spatial_reuse_parameter_set_empty")
            .build()
    }
    val control = bytes[0].u8()
    builder
        .setSrControl(control)
        .addAllFlags(heSpatialReuseFlags(control))
    var offset = 1
    if (control and 0x04 != 0) {
        if (bytes.size <= offset) {
            builder
                .setTruncated(true)
                .addWarnings("he_non_srg_obss_pd_max_offset_missing")
            return builder.build()
        }
        builder.nonSrgObssPdMaxOffset = bytes[offset].u8()
        offset += 1
    }
    if (control and 0x08 != 0) {
        if (bytes.size < offset + 18) {
            builder
                .setTruncated(true)
                .addWarnings("he_srg_information_too_short bytes=${(bytes.size - offset).coerceAtLeast(0)} required=18")
            return builder.build()
        }
        builder
            .setSrgObssPdMinOffset(bytes[offset].u8())
            .setSrgObssPdMaxOffset(bytes[offset + 1].u8())
            .setSrgBssColorBitmapHex(bytes.copyOfRange(offset + 2, offset + 10).toHex())
            .setSrgPartialBssidBitmapHex(bytes.copyOfRange(offset + 10, offset + 18).toHex())
    }
    return builder.build()
}

private fun heCapabilityFeatures(mac: ByteArray, phy: ByteArray): List<String> = buildList {
    if (mac[0].u8() and 0x01 != 0) add("htc_he")
    if (mac[0].u8() and 0x02 != 0) add("twt_requester")
    if (mac[0].u8() and 0x04 != 0) add("twt_responder")
    if (mac[2].u8() and 0x02 != 0) add("all_ack")
    if (mac[2].u8() and 0x04 != 0) add("trs")
    if (mac[2].u8() and 0x08 != 0) add("bsr")
    if (mac[2].u8() and 0x10 != 0) add("broadcast_twt")
    if (mac[2].u8() and 0x20 != 0) add("32bit_ba_bitmap")
    if (mac[2].u8() and 0x40 != 0) add("mu_cascading")
    if (mac[2].u8() and 0x80 != 0) add("ack_enabled")
    if (mac[3].u8() and 0x02 != 0) add("om_control")
    if (mac[3].u8() and 0x04 != 0) add("ofdma_random_access")
    if (mac[3].u8() and 0x20 != 0) add("amsdu_fragmentation")
    if (mac[3].u8() and 0x40 != 0) add("flexible_twt_schedule")
    if (mac[3].u8() and 0x80 != 0) add("rx_control_frame_to_multibss")
    if (mac[4].u8() and 0x01 != 0) add("bsrp_bqrp_ampdu_aggregation")
    if (mac[4].u8() and 0x02 != 0) add("qtp")
    if (mac[4].u8() and 0x04 != 0) add("bqr")
    if (mac[4].u8() and 0x08 != 0) add("srp_responder")
    if (mac[4].u8() and 0x10 != 0) add("ndp_feedback_report")
    if (mac[4].u8() and 0x20 != 0) add("ops")
    if (mac[4].u8() and 0x40 != 0) add("amsdu_in_ampdu")
    if (mac[5].u8() and 0x04 != 0) add("subchannel_selective_transmission")
    if (mac[5].u8() and 0x08 != 0) add("ul_2x996_tone_ru")
    if (mac[5].u8() and 0x10 != 0) add("om_control_ul_mu_data_disable_rx")
    if (mac[5].u8() and 0x20 != 0) add("he_dynamic_sm_power_save")
    if (mac[5].u8() and 0x40 != 0) add("punctured_sounding")
    if (mac[5].u8() and 0x80 != 0) add("ht_vht_trigger_frame_rx")

    val phy0 = phy[0].u8()
    if (phy0 and 0x02 != 0) add("40mhz_in_2ghz")
    if (phy0 and 0x04 != 0) add("40_80mhz_in_5ghz")
    if (phy0 and 0x08 != 0) add("160mhz_in_5ghz")
    if (phy0 and 0x10 != 0) add("80plus80mhz_in_5ghz")
    if (phy0 and 0x20 != 0) add("242_tone_ru_in_2ghz")
    if (phy0 and 0x40 != 0) add("242_tone_ru_in_5ghz")
    if (phy[1].u8() and 0x01 != 0) add("preamble_puncturing_rx_80mhz_second_20mhz")
    if (phy[1].u8() and 0x02 != 0) add("preamble_puncturing_rx_80mhz_second_40mhz")
    if (phy[1].u8() and 0x04 != 0) add("preamble_puncturing_rx_160mhz_second_20mhz")
    if (phy[1].u8() and 0x08 != 0) add("preamble_puncturing_rx_160mhz_second_40mhz")
    if (phy[1].u8() and 0x10 != 0) add("device_class_a")
    if (phy[1].u8() and 0x20 != 0) add("ldpc_coding")
    if (phy[1].u8() and 0x40 != 0) add("he_ltf_gi_0_8us")
    if (phy[2].u8() and 0x02 != 0) add("ndp_4x_ltf_3_2us")
    if (phy[2].u8() and 0x04 != 0) add("stbc_tx_lt_80mhz")
    if (phy[2].u8() and 0x08 != 0) add("stbc_rx_lt_80mhz")
    if (phy[2].u8() and 0x10 != 0) add("doppler_tx")
    if (phy[2].u8() and 0x20 != 0) add("doppler_rx")
    if (phy[2].u8() and 0x40 != 0) add("full_bw_ul_mu_mimo")
    if (phy[2].u8() and 0x80 != 0) add("partial_bw_ul_mu_mimo")
    if (phy[3].u8() and 0x80 != 0) add("su_beamformer")
    if (phy[4].u8() and 0x01 != 0) add("su_beamformee")
    if (phy[4].u8() and 0x02 != 0) add("mu_beamformer")
    if (phy[5].u8() and 0x40 != 0) add("ng16_su_feedback")
    if (phy[5].u8() and 0x80 != 0) add("ng16_mu_feedback")
    if (phy[6].u8() and 0x01 != 0) add("codebook_4_2_su_feedback")
    if (phy[6].u8() and 0x02 != 0) add("codebook_7_5_mu_feedback")
    if (phy[6].u8() and 0x04 != 0) add("triggered_su_beamformer_feedback")
    if (phy[6].u8() and 0x08 != 0) add("triggered_mu_beamformer_feedback")
    if (phy[6].u8() and 0x10 != 0) add("triggered_cqi_feedback")
    if (phy[6].u8() and 0x20 != 0) add("partial_bw_extended_range")
    if (phy[6].u8() and 0x40 != 0) add("partial_bw_dl_mu_mimo")
    if (phy[6].u8() and 0x80 != 0) add("ppe_thresholds_present")
    if (phy[7].u8() and 0x01 != 0) add("srp_based_spatial_reuse")
    if (phy[7].u8() and 0x02 != 0) add("power_boost_factor_ar")
    if (phy[7].u8() and 0x04 != 0) add("he_su_mu_ppdu_4x_ltf_0_8us_gi")
    if (phy[7].u8() and 0x40 != 0) add("stbc_tx_gt_80mhz")
    if (phy[7].u8() and 0x80 != 0) add("stbc_rx_gt_80mhz")
    if (phy[8].u8() and 0x01 != 0) add("he_er_su_ppdu_4x_ltf_0_8us_gi")
    if (phy[8].u8() and 0x02 != 0) add("20mhz_in_40mhz_he_ppdu_2ghz")
    if (phy[8].u8() and 0x04 != 0) add("20mhz_in_160mhz_he_ppdu")
    if (phy[8].u8() and 0x08 != 0) add("80mhz_in_160mhz_he_ppdu")
    if (phy[8].u8() and 0x10 != 0) add("he_er_su_ppdu_1x_ltf_0_8us_gi")
    if (phy[8].u8() and 0x20 != 0) add("midamble_rx_tx_2x_and_1x_ltf")
    if (phy[9].u8() and 0x01 != 0) add("longer_than_16_sigb_ofdm_symbols")
    if (phy[9].u8() and 0x02 != 0) add("non_triggered_cqi_feedback")
    if (phy[9].u8() and 0x04 != 0) add("tx_1024qam_less_than_242_tone_ru")
    if (phy[9].u8() and 0x08 != 0) add("rx_1024qam_less_than_242_tone_ru")
    if (phy[9].u8() and 0x10 != 0) add("rx_full_bw_su_using_mu_with_compressed_sigb")
    if (phy[9].u8() and 0x20 != 0) add("rx_full_bw_su_using_mu_with_non_compressed_sigb")
    add("beamformee_sts_lt_80mhz=${(phy[4].u8() ushr 2) and 0x07}")
    add("beamformee_sts_gt_80mhz=${(phy[4].u8() ushr 5) and 0x07}")
    add("sounding_dimensions_lt_80mhz=${(phy[5].u8() ushr 0) and 0x07}")
    add("sounding_dimensions_gt_80mhz=${(phy[5].u8() ushr 3) and 0x07}")
    add("max_nc=${(phy[7].u8() ushr 3) and 0x07}")
    add("dcm_max_ru=${heDcmMaxRu((phy[8].u8() ushr 6) and 0x03)}")
}

private fun ehtCapabilityFeatures(mac: ByteArray, phy: ByteArray): List<String> = buildList {
    if (mac[0].u8() and 0x01 != 0) add("epcs_priority_access")
    if (mac[0].u8() and 0x02 != 0) add("om_control")
    if (mac[0].u8() and 0x04 != 0) add("triggered_txop_sharing_mode1")
    if (mac[0].u8() and 0x08 != 0) add("triggered_txop_sharing_mode2")
    if (mac[0].u8() and 0x10 != 0) add("restricted_twt")
    if (mac[0].u8() and 0x20 != 0) add("scs_traffic_description")
    if (mac[1].u8() and 0x02 != 0) add("eht_trs")
    if (mac[1].u8() and 0x04 != 0) add("txop_return")
    if (mac[1].u8() and 0x08 != 0) add("two_bqrs")
    if (mac[1].u8() and 0x40 != 0) add("unsolicited_epcs_priority_access")

    if (phy[0].u8() and 0x02 != 0) add("320mhz_in_6ghz")
    if (phy[0].u8() and 0x04 != 0) add("242_tone_ru_gt_20mhz")
    if (phy[0].u8() and 0x08 != 0) add("ndp_4_eht_ltf_3_2us_gi")
    if (phy[0].u8() and 0x10 != 0) add("partial_bw_ul_mu_mimo")
    if (phy[0].u8() and 0x20 != 0) add("su_beamformer")
    if (phy[0].u8() and 0x40 != 0) add("su_beamformee")
    if (phy[3].u8() and 0x02 != 0) add("ng_16_su_feedback")
    if (phy[3].u8() and 0x04 != 0) add("ng_16_mu_feedback")
    if (phy[3].u8() and 0x08 != 0) add("codebook_4_2_su_feedback")
    if (phy[3].u8() and 0x10 != 0) add("codebook_7_5_mu_feedback")
    if (phy[3].u8() and 0x20 != 0) add("triggered_su_beamforming_feedback")
    if (phy[3].u8() and 0x40 != 0) add("triggered_mu_beamforming_partial_bw_feedback")
    if (phy[3].u8() and 0x80 != 0) add("triggered_cqi_feedback")
    if (phy[4].u8() and 0x01 != 0) add("partial_bw_dl_mu_mimo")
    if (phy[4].u8() and 0x02 != 0) add("psr_spatial_reuse")
    if (phy[4].u8() and 0x04 != 0) add("power_boost_factor")
    if (phy[4].u8() and 0x08 != 0) add("eht_mu_ppdu_4_eht_ltf_0_8us_gi")
    if (phy[5].u8() and 0x01 != 0) add("non_triggered_cqi_feedback")
    if (phy[5].u8() and 0x02 != 0) add("tx_less_than_242_tone_ru")
    if (phy[5].u8() and 0x04 != 0) add("rx_less_than_242_tone_ru")
    if (phy[5].u8() and 0x08 != 0) add("ppe_thresholds_present")
    if (phy[6].u8() and 0x08 != 0) add("mcs15_80mhz")
    val mcs15_160 = (phy[6].u8() ushr 4) and 0x03
    if (mcs15_160 != 0) add("mcs15_160mhz=$mcs15_160")
    if (phy[6].u8() and 0x40 != 0) add("mcs15_320mhz")
    if (phy[6].u8() and 0x80 != 0) add("eht_duplicate_6ghz")
    if (phy[7].u8() and 0x01 != 0) add("20mhz_sta_rx_ndp_wider_bw")
    if (phy[7].u8() and 0x02 != 0) add("non_ofdma_ul_mu_mimo_80mhz")
    if (phy[7].u8() and 0x04 != 0) add("non_ofdma_ul_mu_mimo_160mhz")
    if (phy[7].u8() and 0x08 != 0) add("non_ofdma_ul_mu_mimo_320mhz")
    if (phy[7].u8() and 0x10 != 0) add("mu_beamformer_80mhz")
    if (phy[7].u8() and 0x20 != 0) add("mu_beamformer_160mhz")
    if (phy[7].u8() and 0x40 != 0) add("mu_beamformer_320mhz")
    if (phy[7].u8() and 0x80 != 0) add("tb_sounding_feedback_rate_limit")
    if (phy[8].u8() and 0x01 != 0) add("rx_1024qam_wider_bw_dl_ofdma")
    if (phy[8].u8() and 0x02 != 0) add("rx_4096qam_wider_bw_dl_ofdma")
    val beamformeeSs80 = ((phy[0].u8() ushr 7) and 0x01) or ((phy[1].u8() and 0x03) shl 1)
    val soundingDimensions320 = ((phy[2].u8() ushr 6) and 0x03) or ((phy[3].u8() and 0x01) shl 2)
    add("beamformee_ss_80mhz=$beamformeeSs80")
    add("beamformee_ss_160mhz=${(phy[1].u8() ushr 2) and 0x07}")
    add("beamformee_ss_320mhz=${(phy[1].u8() ushr 5) and 0x07}")
    add("sounding_dimensions_80mhz=${phy[2].u8() and 0x07}")
    add("sounding_dimensions_160mhz=${(phy[2].u8() ushr 3) and 0x07}")
    add("sounding_dimensions_320mhz=$soundingDimensions320")
    add("max_nc=${(phy[4].u8() ushr 4) and 0x0f}")
}

private fun heOperationFlags(parameters: Int): List<String> = buildList {
    add("default_pe_duration=${parameters and 0x07}")
    if (parameters and 0x00000008 != 0) add("twt_required")
    add("rts_threshold=${(parameters ushr 4) and 0x03ff}")
    if (parameters and 0x00004000 != 0) add("vht_operation_info_present")
    if (parameters and 0x00008000 != 0) add("co_hosted_bss")
    if (parameters and 0x00010000 != 0) add("er_su_disable")
    if (parameters and 0x00020000 != 0) add("6ghz_operation_info_present")
    if (parameters and 0x40000000 != 0) add("partial_bss_color")
    if (parameters and 0x80000000u.toInt() != 0) add("bss_color_disabled")
}

private fun ehtOperationFlags(parameters: Int): List<String> = buildList {
    if (parameters and 0x01 != 0) add("operation_information_present")
    if (parameters and 0x02 != 0) add("disabled_subchannel_bitmap_present")
    if (parameters and 0x04 != 0) add("eht_default_pe_duration")
    if (parameters and 0x08 != 0) add("group_addressed_bu_indication_limit")
    add("group_addressed_bu_indication_exponent=${(parameters ushr 4) and 0x03}")
    if (parameters and 0x40 != 0) add("mcs15_disabled")
}

private fun heSpatialReuseFlags(control: Int): List<String> = buildList {
    if (control and 0x01 != 0) add("psr_disallowed")
    if (control and 0x02 != 0) add("non_srg_obss_pd_sr_disallowed")
    if (control and 0x04 != 0) add("non_srg_obss_pd_max_offset_present")
    if (control and 0x08 != 0) add("srg_information_present")
    if (control and 0x10 != 0) add("hesiga_spatial_reuse_value15_allowed")
}

private fun heMcsNssSize(phy: ByteArray): Int {
    var count = 4
    if (phy[0].u8() and 0x08 != 0) count += 4
    if (phy[0].u8() and 0x10 != 0) count += 4
    return count
}

private fun ehtMcsNssSize(phy: ByteArray, heCapabilities: WifiHeCapabilities?): Int {
    val hePhy = heCapabilities?.phyCapabilitiesHex?.let { hexToBytes(it) }
    if (hePhy != null && hePhy.size >= 1) {
        if (hePhy[0].u8() and 0x02 != 0) return 3
        var count = 0
        if (hePhy[0].u8() and 0x04 != 0) count += 3
        if (hePhy[0].u8() and 0x08 != 0) count += 3
        if (phy[0].u8() and 0x02 != 0) count += 3
        if (count > 0) return count
    }
    return if (phy[0].u8() and 0x02 != 0) 9 else 3
}

private fun parseHeMcsNss(data: ByteArray, phy: ByteArray): List<WifiMcsNssSupport> {
    val widths = mutableListOf("le_80mhz")
    if (phy[0].u8() and 0x08 != 0) widths += "160mhz"
    if (phy[0].u8() and 0x10 != 0) widths += "80plus80mhz"
    val out = mutableListOf<WifiMcsNssSupport>()
    widths.forEachIndexed { index, width ->
        val offset = index * 4
        if (offset + 4 > data.size) return@forEachIndexed
        out += parseHeMcsMap(width, "rx", data.u16le(offset))
        out += parseHeMcsMap(width, "tx", data.u16le(offset + 2))
    }
    return out
}

private fun parseHeMcsMap(width: String, direction: String, value: Int): List<WifiMcsNssSupport> {
    return (1..8).map { nss ->
        val code = (value ushr ((nss - 1) * 2)) and 0x03
        WifiMcsNssSupport.newBuilder()
            .setStandard("he")
            .setBandwidth(width)
            .setDirection(direction)
            .setMcsRange(heMcsRange(code))
            .setNss(nss)
            .setRaw(code)
            .build()
    }
}

private fun parseEhtMcsNss(data: ByteArray, size: Int): List<WifiMcsNssSupport> {
    val widths = when (size) {
        9 -> listOf("le_80mhz", "160mhz", "320mhz")
        6 -> listOf("le_80mhz", "160mhz")
        4 -> listOf("20mhz_only")
        else -> listOf("le_80mhz")
    }
    val ranges = if (size == 4) {
        listOf("0-7", "8-9", "10-11", "12-13")
    } else {
        listOf("0-9", "10-11", "12-13")
    }
    val out = mutableListOf<WifiMcsNssSupport>()
    var offset = 0
    widths.forEach { width ->
        ranges.forEach { range ->
            if (offset >= data.size) return@forEach
            val value = data[offset].u8()
            out += WifiMcsNssSupport.newBuilder()
                .setStandard("eht")
                .setBandwidth(width)
                .setDirection("rx")
                .setMcsRange(range)
                .setMaxNss(value and 0x0f)
                .setRaw(value and 0x0f)
                .build()
            out += WifiMcsNssSupport.newBuilder()
                .setStandard("eht")
                .setBandwidth(width)
                .setDirection("tx")
                .setMcsRange(range)
                .setMaxNss((value ushr 4) and 0x0f)
                .setRaw((value ushr 4) and 0x0f)
                .build()
            offset += 1
        }
    }
    return out
}

private data class PpeParse(
    val nssCount: Int,
    val ruIndices: List<String>,
    val hex: String,
    val requiredBytes: Int,
    val actualBytes: Int,
    val truncated: Boolean,
)

private fun parseHePpe(bytes: ByteArray, offset: Int, phy: ByteArray): PpeParse {
    val header = bytes[offset].u8()
    val nssCount = (header and 0x07) + 1
    val ruMask = (header ushr 3) and 0x0f
    val ruCount = bitCount(ruMask)
    val requiredBits = 7 + (nssCount * ruCount * 6)
    val requiredBytes = ceilDiv(requiredBits, 8)
    val availableBytes = (bytes.size - offset).coerceAtLeast(0)
    val actualBytes = requiredBytes.coerceAtMost(availableBytes)
    val ruLabels = listOf("242-tone", "484-tone", "996-tone", "2x996-tone")
    return PpeParse(
        nssCount = nssCount,
        ruIndices = ruLabels.filterIndexed { index, _ -> ruMask and (1 shl index) != 0 },
        hex = bytes.copyOfRange(offset, offset + actualBytes).toHex(),
        requiredBytes = requiredBytes,
        actualBytes = actualBytes,
        truncated = availableBytes < requiredBytes || phy[6].u8() and 0x80 == 0,
    )
}

private fun parseEhtPpe(bytes: ByteArray, offset: Int, phy: ByteArray): PpeParse {
    val header = bytes.u16le(offset)
    val nssCount = (header and 0x0f) + 1
    val ruMask = (header ushr 4) and 0x1f
    val ruCount = bitCount(ruMask)
    val requiredBits = 9 + (nssCount * ruCount * 6)
    val requiredBytes = ceilDiv(requiredBits, 8)
    val availableBytes = (bytes.size - offset).coerceAtLeast(0)
    val actualBytes = requiredBytes.coerceAtMost(availableBytes)
    val ruLabels = listOf("242-tone", "484-tone", "996-tone", "2x996-tone", "4x996-tone")
    return PpeParse(
        nssCount = nssCount,
        ruIndices = ruLabels.filterIndexed { index, _ -> ruMask and (1 shl index) != 0 },
        hex = bytes.copyOfRange(offset, offset + actualBytes).toHex(),
        requiredBytes = requiredBytes,
        actualBytes = actualBytes,
        truncated = availableBytes < requiredBytes || phy[5].u8() and 0x08 == 0,
    )
}

private fun heMcsRange(code: Int): String = when (code) {
    0 -> "0-7"
    1 -> "0-9"
    2 -> "0-11"
    else -> "not_supported"
}

private fun he6GhzChannelWidth(value: Int): String = when (value) {
    0 -> "20MHz"
    1 -> "40MHz"
    2 -> "80MHz"
    3 -> "160MHz"
    else -> "unknown($value)"
}

private fun ehtChannelWidth(value: Int): String = when (value) {
    0 -> "20MHz"
    1 -> "40MHz"
    2 -> "80MHz"
    3 -> "160MHz"
    4 -> "320MHz"
    else -> "unknown($value)"
}

private fun heDcmMaxRu(value: Int): String = when (value) {
    0 -> "242-tone"
    1 -> "484-tone"
    2 -> "996-tone"
    3 -> "2x996-tone"
    else -> "unknown($value)"
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

private fun ByteArray.u32le(offset: Int): Int {
    return this[offset].u8() or
        (this[offset + 1].u8() shl 8) or
        (this[offset + 2].u8() shl 16) or
        (this[offset + 3].u8() shl 24)
}

private fun Byte.u8(): Int = toInt() and 0xff

private fun bitCount(value: Int): Int = Integer.bitCount(value)

private fun ceilDiv(value: Int, divisor: Int): Int = (value + divisor - 1) / divisor
