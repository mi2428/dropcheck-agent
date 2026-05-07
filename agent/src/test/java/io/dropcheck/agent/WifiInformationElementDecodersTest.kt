package io.dropcheck.agent

import io.dropcheck.agent.grpc.WifiInformationElement
import io.dropcheck.agent.grpc.WifiConnection
import io.dropcheck.agent.grpc.WifiStatus
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class WifiInformationElementDecodersTest {
    @Test
    fun decodesHeAndEhtInformationElements() {
        val decodes = decodeWifiInformationElements(listOf(
            extension(35, "0700180600481c20c0800300c000c00c00fafffafffafffafffafffaff410102"),
            extension(36, "08000211ffff05021f000c"),
            extension(37, "2d"),
            extension(38, "01039f20042730056f40069f50"),
            extension(39, "1c0a141e01020304050607081112131415161718"),
            extension(106, "4311223344041f3f0a00"),
            extension(108, "774ef61fffffff7ff8ff03214365214365214365010102"),
        ))

        val he = requireNotNull(decodes.heCapabilities)
        assertFalse(he.truncated)
        assertTrue(he.featuresList.contains("ofdma_random_access"))
        assertTrue(he.featuresList.contains("partial_bw_ul_mu_mimo"))
        assertTrue(he.featuresList.contains("ul_2x996_tone_ru"))
        assertTrue(he.featuresList.contains("partial_bw_dl_mu_mimo"))
        assertEquals(48, he.mcsNssCount)
        assertEquals(2, he.ppeNssCount)
        assertTrue(he.ppeRuIndicesList.contains("2x996-tone"))

        val heOperation = requireNotNull(decodes.heOperation)
        assertEquals("80MHz", heOperation.channelWidth)
        assertEquals(5, heOperation.primaryChannel)
        assertEquals(17, heOperation.bssColor)
        assertTrue(heOperation.flagsList.contains("twt_required"))
        assertTrue(heOperation.flagsList.contains("6ghz_operation_info_present"))

        val eht = requireNotNull(decodes.ehtCapabilities)
        assertFalse(eht.truncated)
        assertTrue(eht.featuresList.contains("242_tone_ru_gt_20mhz"))
        assertTrue(eht.featuresList.contains("su_beamformer"))
        assertTrue(eht.featuresList.contains("su_beamformee"))
        assertTrue(eht.featuresList.contains("non_ofdma_ul_mu_mimo_320mhz"))
        assertTrue(eht.featuresList.contains("mu_beamformer_320mhz"))
        assertEquals(18, eht.mcsNssCount)
        assertEquals(2, eht.ppeNssCount)
        assertTrue(eht.ppeRuIndicesList.contains("4x996-tone"))

        val ehtOperation = requireNotNull(decodes.ehtOperation)
        assertEquals("320MHz", ehtOperation.channelWidth)
        assertEquals(31, ehtOperation.centerFreqSegment0)
        assertEquals(63, ehtOperation.centerFreqSegment1)
        assertEquals(0x0a, ehtOperation.disabledSubchannelBitmap)
        assertTrue(ehtOperation.flagsList.contains("disabled_subchannel_bitmap_present"))
        assertTrue(ehtOperation.flagsList.contains("mcs15_disabled"))

        val uora = requireNotNull(decodes.heUoraParameterSet)
        assertEquals(5, uora.eocwMin)
        assertEquals(5, uora.eocwMax)

        val muEdca = requireNotNull(decodes.heMuEdcaParameterSet)
        assertEquals(4, muEdca.acCount)
        assertEquals("be", muEdca.acList[0].ac)
        assertEquals(4, muEdca.acList[1].aifsn)

        val spatialReuse = requireNotNull(decodes.heSpatialReuseParameterSet)
        assertEquals(0x1c, spatialReuse.srControl)
        assertEquals(10, spatialReuse.nonSrgObssPdMaxOffset)
        assertEquals("0102030405060708", spatialReuse.srgBssColorBitmapHex)
        assertTrue(spatialReuse.flagsList.contains("srg_information_present"))

        val rendered = AgentWifiStatusRenderer.render(
            WifiStatus.newBuilder()
                .setConnection(WifiConnection.newBuilder()
                    .setSsid("Lab")
                    .setHeCapabilities(he)
                    .setHeOperation(heOperation)
                    .setEhtCapabilities(eht)
                    .setEhtOperation(ehtOperation)
                    .setHeUoraParameterSet(uora)
                    .setHeMuEdcaParameterSet(muEdca)
                    .setHeSpatialReuseParameterSet(spatialReuse)
                    .build())
                .build(),
        ).joinToString("\n")
        assertTrue(rendered.contains("HE/EHT Details"))
        assertTrue(rendered.contains("242_tone_ru_gt_20mhz"))
        assertTrue(rendered.contains("mcs_nss eht/le_80mhz/0-9"))
        assertTrue(rendered.contains("disabled_subchannel_bitmap=0xa"))
        assertTrue(rendered.contains("uora"))
        assertTrue(rendered.contains("spatial_reuse"))
    }

    @Test
    fun usesCorrectHeAndEhtCapabilityBitPositions() {
        val decodes = decodeWifiInformationElements(listOf(
            extension(35, "0d01081a4010026040880f41811c110800fafffaff191cc771"),
            extension(108, "8700e00101001876001200222220"),
        ))

        val he = requireNotNull(decodes.heCapabilities)
        assertTrue(he.featuresList.contains("bsr"))
        assertTrue(he.featuresList.contains("om_control"))
        assertTrue(he.featuresList.contains("om_control_ul_mu_data_disable_rx"))
        assertTrue(he.featuresList.contains("ldpc_coding"))
        assertTrue(he.featuresList.contains("full_bw_ul_mu_mimo"))
        assertFalse(he.featuresList.contains("ul_2x996_tone_ru"))
        assertFalse(he.featuresList.contains("partial_bw_ul_mu_mimo"))

        val eht = requireNotNull(decodes.ehtCapabilities)
        assertTrue(eht.featuresList.contains("su_beamformer"))
        assertTrue(eht.featuresList.contains("su_beamformee"))
        assertTrue(eht.featuresList.contains("tx_less_than_242_tone_ru"))
        assertTrue(eht.featuresList.contains("rx_less_than_242_tone_ru"))
        assertTrue(eht.featuresList.contains("non_ofdma_ul_mu_mimo_80mhz"))
        assertFalse(eht.featuresList.contains("242_tone_ru_gt_20mhz"))
        assertFalse(eht.featuresList.contains("partial_bw_ul_mu_mimo"))
        assertTrue(eht.featuresList.contains("beamformee_ss_80mhz=3"))
        assertTrue(eht.featuresList.contains("sounding_dimensions_80mhz=1"))
    }

    private fun extension(idExt: Int, bytesHex: String): WifiInformationElement {
        return WifiInformationElement.newBuilder()
            .setId(255)
            .setIdExt(idExt)
            .setByteCount(bytesHex.length / 2)
            .setBytesHex(bytesHex)
            .build()
    }
}
