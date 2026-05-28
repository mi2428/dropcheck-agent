package io.dropcheck.agent

import io.dropcheck.agent.grpc.WifiInformationElement
import io.dropcheck.agent.grpc.WifiConnection
import io.dropcheck.agent.grpc.WifiHeCapabilities
import io.dropcheck.agent.grpc.WifiHeMacCapabilities
import io.dropcheck.agent.grpc.WifiHePhyCapabilities
import io.dropcheck.agent.grpc.WifiScan
import io.dropcheck.agent.grpc.WifiScanResult
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
            extension(59, "ad3e"),
            extension(106, "4311223344041f3f0a00"),
            extension(108, "774ef61fffffff7ff8ff03214365214365214365010102"),
        ))

        val he = requireNotNull(decodes.heCapabilities)
        assertFalse(he.truncated)
        assertTrue(he.hasMac())
        assertTrue(he.hasPhy())
        assertTrue(he.mac.htcHe)
        assertEquals("no_feedback", he.mac.linkAdaptation)
        assertEquals(1, he.mac.multiTidAggregationRxQos)
        assertEquals(1, he.mac.multiTidAggregationTxQos)
        assertTrue(he.mac.ul2X996ToneRu)
        assertTrue(he.phy.channelWidthSetList.contains("160mhz_in_5ghz"))
        assertTrue(he.phy.channelWidthSetList.contains("80plus80mhz_in_5ghz"))
        assertEquals("0us", he.phy.nominalPacketPadding)
        assertTrue(he.phy.fullBwUlMuMimo)
        assertTrue(he.phy.partialBwUlMuMimo)
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
        assertTrue(eht.hasMac())
        assertTrue(eht.hasPhy())
        assertEquals(7991, eht.mac.maxMpduLengthBytes)
        assertEquals("no_feedback", eht.mac.linkAdaptation)
        assertTrue(eht.phy.supports320MhzIn6Ghz)
        assertEquals("20us", eht.phy.commonNominalPacketPadding)
        assertTrue(eht.phy.mcs15Supported80Mhz)
        assertTrue(eht.phy.mcs15Supported160Mhz)
        assertTrue(eht.phy.mcs15Supported320Mhz)
        assertTrue(eht.phy.rx4096QamWiderBwDlOfdma)
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
        assertEquals(4, ehtOperation.channelWidthCode)
        assertEquals(320, ehtOperation.channelWidthMhz)
        assertEquals(31, ehtOperation.centerFreqSegment0)
        assertEquals(63, ehtOperation.centerFreqSegment1)
        assertEquals(0x0a, ehtOperation.disabledSubchannelBitmap)
        assertEquals("000a", ehtOperation.disabledSubchannelBitmapHex)
        assertEquals(listOf(1, 3), ehtOperation.disabledSubchannelIndicesList)
        assertEquals(8, ehtOperation.basicMcsNssCount)
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

        val he6Ghz = requireNotNull(decodes.he6GhzCapabilities)
        assertFalse(he6Ghz.truncated)
        assertEquals("4us", he6Ghz.minimumMpduStartSpacing)
        assertEquals(5, he6Ghz.maxAmpduLengthExponent)
        assertEquals(262143, he6Ghz.maxAmpduLengthBytes)
        assertEquals(11454, he6Ghz.maxMpduLengthBytes)
        assertEquals("disabled", he6Ghz.smPowerSave)
        assertTrue(he6Ghz.rdResponder)
        assertTrue(he6Ghz.rxAntennaPatternConsistency)
        assertTrue(he6Ghz.txAntennaPatternConsistency)
        assertTrue(he6Ghz.featuresList.contains("max_mpdu_length_bytes=11454"))

        val rendered = AgentWifiStatusRenderer.render(
            WifiStatus.newBuilder()
                .setConnection(WifiConnection.newBuilder()
                    .setSsid("Lab")
                    .setHeCapabilities(he)
                    .setHeOperation(heOperation)
                    .setHe6GhzCapabilities(he6Ghz)
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
        assertTrue(rendered.contains("he_6ghz_cap"))
        assertTrue(rendered.contains("max_mpdu=11454"))
        assertTrue(rendered.contains("uora"))
        assertTrue(rendered.contains("spatial_reuse"))

        val puncturingHe = WifiHeCapabilities.newBuilder()
            .setMac(WifiHeMacCapabilities.newBuilder()
                .setPuncturedSounding(true))
            .setPhy(WifiHePhyCapabilities.newBuilder()
                .addPreamblePuncturingRx("preamble_puncturing_rx_80mhz_second_20mhz"))
            .build()
        val mloRendered = AgentWifiMloRenderer.render(
            WifiStatus.newBuilder()
                .setConnection(WifiConnection.newBuilder()
                    .setSsid("Lab")
                    .setBssid("aa:bb:cc:dd:ee:ff")
                    .setWifiStandard("802.11be")
                    .setHeCapabilities(puncturingHe)
                    .setHe6GhzCapabilities(he6Ghz)
                    .setEhtCapabilities(eht)
                    .setEhtOperation(ehtOperation)
                    .build())
                .build(),
            WifiScan.newBuilder()
                .addResults(WifiScanResult.newBuilder()
                    .setSsid("Lab")
                    .setBssid("aa:bb:cc:dd:ee:ff")
                    .setWifiStandard("802.11be")
                    .setApMloLinkId(-1)
                    .setHeCapabilities(puncturingHe)
                    .setHe6GhzCapabilities(he6Ghz)
                    .setEhtCapabilities(eht)
                    .setEhtOperation(ehtOperation)
                    .build())
                .build(),
        ).joinToString("\n")
        assertTrue(mloRendered.contains("Connected EHT Details"))
        assertTrue(mloRendered.contains("Connected HE 6GHz Details"))
        assertTrue(mloRendered.contains("Scan HE 6GHz Details"))
        assertTrue(mloRendered.contains("Connected EHT Puncturing"))
        assertTrue(mloRendered.contains("Scan EHT Puncturing"))
        assertTrue(mloRendered.contains("he_preamble_puncturing_rx=preamble_puncturing_rx_80mhz_second_20mhz"))
        assertTrue(mloRendered.contains("he_punctured_sounding=true"))
        assertTrue(mloRendered.contains("eht_disabled_subchannel_bitmap=0x000a punctured=1,3"))
        assertTrue(mloRendered.contains("EHT_W"))
        assertTrue(mloRendered.contains("eht_width=320MHz"))
        assertTrue(mloRendered.contains("Scan EHT Details"))
        assertTrue(mloRendered.contains("max_mpdu=7991"))
        assertTrue(mloRendered.contains("max_ampdu=262143"))
        assertTrue(mloRendered.contains("320mhz=true"))
        assertTrue(mloRendered.contains("width_mhz=320"))
        assertTrue(mloRendered.contains("disabled=0x000a"))
        assertTrue(mloRendered.contains("punctured=1,3"))
        scanEhtDetailLines(mloRendered).forEach { line ->
            assertTrue("Scan EHT detail line too long (${line.length}): $line\n$mloRendered", line.length <= 92)
        }
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

    @Test
    fun usesCorrectEhtMcs15CapabilityBits() {
        val decodes = decodeWifiInformationElements(listOf(
            extension(108, "0000000000000000700000000000"),
        ))

        val eht = requireNotNull(decodes.ehtCapabilities)
        assertTrue(eht.featuresList.contains("mcs15_80mhz"))
        assertTrue(eht.featuresList.contains("mcs15_160mhz"))
        assertTrue(eht.featuresList.contains("mcs15_320mhz"))
        assertFalse(eht.featuresList.any { it.startsWith("mcs15_160mhz=") })
        assertTrue(eht.phy.mcs15Supported80Mhz)
        assertTrue(eht.phy.mcs15Supported160Mhz)
        assertTrue(eht.phy.mcs15Supported320Mhz)
    }

    @Test
    fun decodesWifi7SecurityInformationElements() {
        val decodes = decodeWifiInformationElements(listOf(
            element(48, "0100000fac090100000fac090200000fac18000fac19c0000000000fac0c"),
            element(244, "200020"),
            element(127, "0000080000000000000010"),
        ))

        val security = requireNotNull(decodes.securityDetails)
        assertTrue(security.rsnPresent)
        assertEquals(1, security.rsnVersion)
        assertEquals("gcmp_256", security.groupDataCipher)
        assertEquals(listOf("gcmp_256"), security.pairwiseCiphersList)
        assertEquals(listOf("sae_gdh", "ft_sae_gdh"), security.akmSuitesList)
        assertEquals("00c0", security.rsnCapabilitiesHex)
        assertTrue(security.pmfCapable)
        assertTrue(security.pmfRequired)
        assertEquals("bip_gmac_256", security.groupManagementCipher)
        assertTrue(security.gcmp256)
        assertTrue(security.saeGdh)
        assertTrue(security.ftSaeGdh)
        assertTrue(security.rsnxePresent)
        assertTrue(security.rsnxeCapabilitiesList.contains("sae_h2e"))
        assertTrue(security.rsnxeCapabilitiesList.contains("ssid_protection"))
        assertTrue(security.extendedCapabilitiesPresent)
        assertTrue(security.extendedCapabilitiesList.contains("bss_transition"))
        assertTrue(security.extendedCapabilitiesList.contains("beacon_protection"))
        assertTrue(security.beaconProtection)
        assertTrue(security.wifi7PersonalReady)
    }

    private fun element(id: Int, bytesHex: String): WifiInformationElement {
        return WifiInformationElement.newBuilder()
            .setId(id)
            .setByteCount(bytesHex.length / 2)
            .setBytesHex(bytesHex)
            .build()
    }

    private fun extension(idExt: Int, bytesHex: String): WifiInformationElement {
        return WifiInformationElement.newBuilder()
            .setId(255)
            .setIdExt(idExt)
            .setByteCount(bytesHex.length / 2)
            .setBytesHex(bytesHex)
            .build()
    }

    private fun scanEhtDetailLines(rendered: String): List<String> {
        return rendered.lines()
            .dropWhile { it != "Scan EHT Details" }
            .drop(1)
            .takeWhile { it != "Diagnostics / Warnings" && it != "MLO Capability Signals" }
            .filter { it.isNotBlank() }
    }
}
