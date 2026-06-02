package io.dropcheck.agent

import io.dropcheck.agent.grpc.DiagnosticField
import io.dropcheck.agent.grpc.MloLinkInfo
import io.dropcheck.agent.grpc.WifiCapabilities
import io.dropcheck.agent.grpc.WifiConnection
import io.dropcheck.agent.grpc.WifiEhtCapabilities
import io.dropcheck.agent.grpc.WifiEhtMacCapabilities
import io.dropcheck.agent.grpc.WifiEhtOperation
import io.dropcheck.agent.grpc.WifiEhtPhyCapabilities
import io.dropcheck.agent.grpc.WifiHeCapabilities
import io.dropcheck.agent.grpc.WifiHeMacCapabilities
import io.dropcheck.agent.grpc.WifiHeOperation
import io.dropcheck.agent.grpc.WifiHePhyCapabilities
import io.dropcheck.agent.grpc.WifiHeSpatialReuseParameterSet
import io.dropcheck.agent.grpc.WifiScan
import io.dropcheck.agent.grpc.WifiScanResult
import io.dropcheck.agent.grpc.WifiStatus
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class AgentWifiMloRendererTest {
    @Test
    fun rendersConnectedAndNearbyMloState() {
        val status = WifiStatus.newBuilder()
            .setEnabled(true)
            .setState("enabled")
            .setConnection(mloConnection())
            .addPermissions("ACCESS_FINE_LOCATION=granted")
            .build()
        val scan = WifiScan.newBuilder()
            .addFields(field("scan_result_count", "3"))
            .addFields(field("scan_result_total_count", "4"))
            .addResults(mloScanResult("Lab", "aa:bb:cc:dd:ee:ff", -45, "02:00:00:00:00:01", 2).toBuilder()
                .setResponder80211Mc(true)
                .setResponder80211AzNtb(true)
                .setRangingFrameProtectionRequired(true)
                .setSecureHeLtfSupported(true)
                .setTwtResponder(true)
                .addInformationElements(rnrIe())
                .addInformationElements(rmEnabledCapabilitiesIe())
                .addInformationElements(extendedCapabilitiesBssTransitionIe())
                .addInformationElements(mobilityDomainIe())
                .addInformationElements(multipleBssidIe())
                .build())
            .addResults(mloScanResult("Lab", "aa:bb:cc:dd:ee:01", -55, "02:00:00:00:00:01", 1))
            .addResults(WifiScanResult.newBuilder()
                .setSsid("Legacy")
                .setBssid("11:22:33:44:55:66")
                .setRssiDbm(-60)
                .setBand("5ghz")
                .setFrequencyMhz(5200)
                .setWifiStandard("802.11ax")
                .setApMloLinkId(-1)
                .build())
            .build()

        val out = AgentWifiMloRenderer.render(
            status,
            scan,
            AgentWifiMloContext(scanSource = "cached", sdkInt = 36, wifi7Supported = true, wifiCapabilities = wifiCapabilities()),
        ).joinToString("\n")

        listOf(
            "Connected MLO",
            "ap_mld      02:00:00:00:00:01",
            "Associated MLO Links",
            "EHT Scan",
            "eht_candidates",
            "Nearby EHT APs",
            "*",
            "02:00:00:00:00:01",
            "EHT Scan Links",
            "[*] Lab",
            "type=ap ap_mld=02:00:00:00:00:01 link=2 bssid=aa:bb:cc:dd:ee:ff",
            "band=6ghz ch=5 freq=5975MHz width=80MHz eht_width=320MHz puncture=1,3 rssi=-45dBm",
            "ie rsn=false rsnxe=false ext_cap=true rnr=true mbssid=true noninherit=false eht_mle=true ap_mld=true link_id=true",
            "sdk_flags twt=true 11az_ntb=true ranging_prot=true secure_he_ltf=true 11mc=true",
            "  affiliated_links",
            "    [+] type=aff link=1 ap_mac=02:00:00:00:00:01",
            "Connected Roaming / Transition",
            "summary 11k=true 11v_bss_transition=true 11r=true ft_akm=<none> rnr=true",
            "Scan Roaming / Transition",
            "Connected BSS Coloring",
            "Scan BSS Coloring",
            "he_operation bss_color=17 disabled=false partial=true",
            "srg_bss_color_bitmap=0x0102030405060708",
            "COLOR",
            "17(part)",
            "Connected HE Details",
            "Scan HE Details",
            "he_mac flags twt_responder,om_control,punctured_sounding",
            "he features twt_responder,punctured_sounding",
            "Connected EHT Details",
            "Scan EHT Details",
            "mac flags om_control,triggered_txop_mode1,scs_traffic_description",
            "eht features 320mhz_in_6ghz,rx_4096qam_wider_dl_ofdma",
            "oper flags operation_information_present",
            "oper flags disabled_subchannel_bitmap_present",
            "Scan RNR Details",
            "rnr band=6ghz width=80MHz channel=5 freq=5975MHz op_class=133",
            "mld ap_mld_id=7 link_id=2",
            "Scan Multiple BSSID Details",
            "profile #1",
            "noninherit=ids:48/ext:106",
            "profile_security #1",
            "Connected EHT Multi-Link Elements",
            "ml_control raw=0x07f0 type=basic(0) presence=link_id_info,bss_parameters_change_count,medium_synchronization_delay,eml_capabilities,mld_capabilities_and_operations,ap_mld_id,extended_mld_capabilities_and_operations bytes=37",
            "common_info len=18 mld_mac=02:00:00:00:00:01 link_id=2 bss_param_change_count=7 medium_sync_delay=0x1032 eml_capabilities=0x8f08 mld_capabilities=0x3370 ap_mld_id=5 ext_mld_capabilities=0x6100",
            "medium_sync raw=0x1032 duration=16 ofdm_ed_threshold=2 max_txop=3",
            "eml raw=0x8f08 flags=emlsr,emlmr",
            "mld raw=0x3370 flags=srs,aar,link_reconfig,aligned_twt",
            "ext_mld raw=0x6100 flags=op_param_update,nstr_update,emlsr_enabled_one_link",
            "per_link link_id=2 control=0x0972 complete=true",
            "sta_info_len=12 sta_mac=02:00:00:00:00:02 beacon_interval_tu=100",
            "dtim=1/3",
            "Scan EHT Multi-Link Elements",
            "Current AP Relation",
            "same_mld_results",
            "visible_links",
            "Wi-Fi 7 Device Readiness",
            "band_6ghz",
            "wpa3_sae_h2e",
            "dual_band_simultaneous",
            "MLO Capability Signals",
            "mlo_features",
            "Diagnostics / Warnings",
            "none",
        ).forEach { want ->
            assertTrue("rendered output missing $want:\n$out", out.contains(want))
        }
        assertTrue(out.indexOf("Current AP Relation") < out.indexOf("Connected MLO"))
        assertTrue(out.indexOf("Connected MLO") < out.indexOf("EHT Scan"))
        assertFalse(out.contains("MARK"))
        assertFalse(out.contains("Legacy"))
        assertFalse(out.contains("scan Lab"))
        assertFalse(out.contains("connected_ap_mld_not_seen_in_scan"))
    }

    @Test
    fun rendersBriefNearbyTables() {
        val longSsid = "Lab-SSID-That-Should-Not-Truncate"
        val status = WifiStatus.newBuilder()
            .setEnabled(true)
            .setState("enabled")
            .setConnection(mloConnection())
            .build()
        val scan = WifiScan.newBuilder()
            .addFields(field("scan_result_count", "2"))
            .addFields(field("scan_result_total_count", "2"))
            .addResults(mloScanResult(longSsid, "aa:bb:cc:dd:ee:ff", -45, "02:00:00:00:00:01", 2))
            .addResults(mloScanResult(longSsid, "aa:bb:cc:dd:ee:01", -55, "02:00:00:00:00:01", 1))
            .build()

        val lines = AgentWifiMloRenderer.render(
            status,
            scan,
            AgentWifiMloContext(brief = true, scanSource = "fresh"),
        )
        val out = lines.joinToString("\n")

        assertTrue(out.contains("EHT Scan"))
        assertTrue(out.contains("Nearby EHT MLDs"))
        assertFalse(out.contains("Nearby EHT Links"))
        assertFalse(out.contains("Nearby EHT APs / Links"))
        assertTrue("missing full SSID in brief table:\n$out", out.contains(longSsid))
        val tableHeaderIndex = lines.indexOf("Nearby EHT MLDs")
        assertTrue("missing Nearby EHT MLDs header:\n$out", tableHeaderIndex >= 0 && tableHeaderIndex + 1 < lines.size)
        assertTrue(
            "missing MLD tree header:\n$out",
            lines[tableHeaderIndex + 1].contains("ITEM") &&
                lines[tableHeaderIndex + 1].contains("MLD") &&
                lines[tableHeaderIndex + 1].contains("EHT"),
        )
        val tableRows = lines.drop(tableHeaderIndex + 2).takeWhile { it.isNotBlank() }
        assertTrue("missing parent MLD row:\n$out", tableRows.any { it.contains(longSsid) && it.contains("02:00:00:00:00:01") })
        assertTrue("missing current link row:\n$out", tableRows.any { it.contains("[*] 2") })
        assertTrue("missing peer link row:\n$out", tableRows.any { it.contains("[+] 1") })
        assertTrue("unexpected link row count:\n$out", tableRows.count { it.contains("[*]") || it.contains("[+]") || it.contains("[-]") } == 2)
        assertFalse(out.contains("Current AP Relation"))
        assertFalse(out.contains("Connected MLO"))
        assertFalse(out.contains("EHT Scan Links"))
        assertFalse(out.contains("Diagnostics / Warnings"))
    }

    @Test
    fun rendersMloDiagnostics() {
        val status = WifiStatus.newBuilder()
            .setEnabled(false)
            .setState("disabled")
            .setConnection(WifiConnection.newBuilder()
                .setSsid("Lab")
                .setBssid("aa:bb:cc:dd:ee:ff")
                .build())
            .addPermissions("ACCESS_FINE_LOCATION=denied")
            .build()
        val scan = WifiScan.newBuilder()
            .addFields(field("wifi_enabled", "false"))
            .addFields(field("scan_throttle_enabled", "true"))
            .addErrors("get_scan_results=SecurityException")
            .build()

        val out = AgentWifiMloRenderer.render(
            status,
            scan,
            AgentWifiMloContext(
                scanSource = "fresh",
                sdkInt = 32,
                wifi7Supported = false,
                scanCommandStatus = "STATUS_FAILED",
                scanCommandMessage = "fresh scan incomplete",
            ),
        ).joinToString("\n")

        listOf(
            "android_api_unavailable sdk=32 required_sdk=33",
            "wifi_7_standard_unsupported",
            "wifi_disabled",
            "wifi_state=disabled",
            "permission ACCESS_FINE_LOCATION=denied",
            "scan_wifi_enabled=false",
            "scan_throttle_enabled=true",
            "scan_error=get_scan_results=SecurityException",
            "scan_command_status=STATUS_FAILED",
            "scan_command_message=fresh scan incomplete",
            "connected_mlo_present=false",
            "eht_scan_results=0",
        ).forEach { want ->
            assertTrue("rendered output missing $want:\n$out", out.contains(want))
        }
    }

    @Test
    fun filtersScanAndConnectionOutput() {
        val status = WifiStatus.newBuilder()
            .setEnabled(true)
            .setState("enabled")
            .setConnection(mloScanResult("Other", "22:22:22:22:22:22", -50, "02:00:00:00:00:02", 2).let {
                WifiConnection.newBuilder()
                    .setSsid(it.ssid)
                    .setBssid(it.bssid)
                    .setWifiStandard(it.wifiStandard)
                    .setApMldMacAddress(it.apMldMacAddress)
                    .setApMloLinkId(it.apMloLinkId)
                    .build()
            })
            .build()
        val scan = WifiScan.newBuilder()
            .addResults(mloScanResult("Lab", "aa:bb:cc:dd:ee:ff", -45, "02:00:00:00:00:01", 1))
            .addResults(mloScanResult("Other", "22:22:22:22:22:22", -50, "02:00:00:00:00:02", 2))
            .build()

        val out = AgentWifiMloRenderer.render(
            status,
            scan,
            AgentWifiMloContext(ssidFilter = "Lab"),
        ).joinToString("\n")

        listOf(
            "filter            ssid=Lab",
            "filtered_results  1",
            "Lab",
            "aa:bb:cc:dd:ee:ff",
            "no active Wi-Fi connection",
        ).forEach { want ->
            assertTrue("rendered output missing $want:\n$out", out.contains(want))
        }
        assertFalse(out.contains("Other"))
        assertFalse(out.contains("22:22:22:22:22:22"))

        val bssidOut = AgentWifiMloRenderer.render(
            status,
            scan,
            AgentWifiMloContext(bssidFilter = "aa:bb:cc:dd:ee:ff"),
        ).joinToString("\n")

        assertTrue("rendered output missing bssid filter:\n$bssidOut", bssidOut.contains("filter            bssid=aa:bb:cc:dd:ee:ff"))
        assertTrue("rendered output missing affiliated links heading:\n$bssidOut", bssidOut.contains("  affiliated_links"))
        assertTrue("rendered output missing affiliated match:\n$bssidOut", bssidOut.contains("    [-] type=aff link=1 ap_mac=02:00:00:00:00:01"))
        assertFalse(bssidOut.contains("Other"))
    }

    @Test
    fun rendersDetailedEhtMultiLinkSubelements() {
        val status = WifiStatus.newBuilder()
            .setEnabled(true)
            .setState("enabled")
            .build()
        val scan = WifiScan.newBuilder()
            .addResults(WifiScanResult.newBuilder()
                .setSsid("Lab")
                .setBssid("aa:bb:cc:dd:ee:ff")
                .setRssiDbm(-50)
                .setBand("6ghz")
                .setFrequencyMhz(5975)
                .setWifiStandard("802.11be")
                .setApMloLinkId(-1)
                .addInformationElements(detailedEhtMultiLinkIe())
                .build())
            .build()

        val out = AgentWifiMloRenderer.render(
            status,
            scan,
            AgentWifiMloContext(sdkInt = 36),
        ).joinToString("\n")

        listOf(
            "subelement id=0 name=per_sta_profile len=22 actual=22 fragments=1 reassembled=30",
            "fragment target_id=0 target=per_sta_profile bytes=8 payload=0x0102ff046c010203",
            "profile_ie link_id=2 id=0 name=ssid len=3 actual=3 body=0x4c6162",
            "profile_ie link_id=2 id=255 ext=106 name=eht_operation len=3 actual=3 body=0x6a0102",
            "profile_ie link_id=2 id=255 ext=108 name=eht_capabilities len=4 actual=4 body=0x6c010203",
            "profile_decode link_id=2",
            "eht_operation_warnings eht_operation_too_short bytes=2 required=5",
            "eht_capabilities_warnings eht_capabilities_too_short bytes=3 required=11",
            "subelement id=221 name=vendor_specific len=6 actual=6",
            "vendor oui=00:11:22 type=7 payload_bytes=2 payload=0x99aa",
        ).forEach { want ->
            assertTrue("rendered output missing $want:\n$out", out.contains(want))
        }
    }

    @Test
    fun usesEhtMultiLinkElementAsMloMetadataFallback() {
        val status = WifiStatus.newBuilder()
            .setEnabled(true)
            .setState("enabled")
            .setConnection(WifiConnection.newBuilder()
                .setSsid("Lab")
                .setBssid("aa:bb:cc:dd:ee:ff")
                .setWifiStandard("802.11ax")
                .setApMloLinkId(-1)
                .addInformationElements(detailedEhtMultiLinkIe())
                .build())
            .build()
        val scan = WifiScan.newBuilder()
            .addResults(WifiScanResult.newBuilder()
                .setSsid("Lab")
                .setBssid("aa:bb:cc:dd:ee:ff")
                .setRssiDbm(-50)
                .setBand("6ghz")
                .setFrequencyMhz(5975)
                .setWifiStandard("802.11ax")
                .setApMloLinkId(-1)
                .addInformationElements(detailedEhtMultiLinkIe())
                .build())
            .build()

        val out = AgentWifiMloRenderer.render(
            status,
            scan,
            AgentWifiMloContext(sdkInt = 36),
        ).joinToString("\n")

        listOf(
            "connected_ap_mld",
            "connected_link",
            "same_mld_results",
            "visible_links",
            "ap_mld      02:00:00:00:00:01",
            "ap_link_id  2",
            "eht_candidates  1",
            "[*] Lab",
            "ap_mld=02:00:00:00:00:01 link=2 bssid=aa:bb:cc:dd:ee:ff",
            "Diagnostics / Warnings\n  none",
        ).forEach { want ->
            assertTrue("rendered output missing $want:\n$out", out.contains(want))
        }
        assertFalse(out.contains("no EHT-capable scan results"))
        assertFalse(out.contains("scan_mlo_metadata_absent"))
    }

    @Test
    fun treatsAndroidPlaceholderWifiInfoAsNoActiveConnection() {
        val status = WifiStatus.newBuilder()
            .setEnabled(true)
            .setState("enabled")
            .setConnection(WifiConnection.newBuilder()
                .setSsid("<unknown ssid>")
                .setBssid("02:00:00:00:00:00")
                .setNetworkId(-1)
                .setWifiStandard("802.11ax")
                .build())
            .build()

        val out = AgentWifiMloRenderer.render(status, WifiScan.getDefaultInstance()).joinToString("\n")

        assertTrue(out.contains("Current AP Relation\n  no active Wi-Fi connection"))
        assertTrue(out.contains("Connected MLO\n  no active Wi-Fi connection"))
        assertFalse("rendered output included placeholder SSID:\n$out", out.contains("<unknown ssid>"))
        assertFalse("rendered output included stale standard:\n$out", out.contains("802.11ax"))
    }

    @Test
    fun treatsMloLinkZeroAsActiveMloIdentity() {
        val status = WifiStatus.newBuilder()
            .setEnabled(true)
            .setState("enabled")
            .setConnection(WifiConnection.newBuilder()
                .setSsid("<unknown ssid>")
                .setBssid("02:00:00:00:00:00")
                .setNetworkId(-1)
                .setWifiStandard("802.11be")
                .setApMloLinkId(0)
                .build())
            .build()

        val out = AgentWifiMloRenderer.render(status, WifiScan.getDefaultInstance()).joinToString("\n")

        assertTrue(out.contains("Connected MLO"))
        assertTrue(out.contains("present     true"))
        assertTrue(out.contains("ap_link_id  0"))
        assertFalse("rendered output treated link-id 0 as inactive:\n$out", out.contains("no active Wi-Fi connection"))
    }

    @Test
    fun capsNearbyTableColumnsForLongSsids() {
        val status = WifiStatus.newBuilder()
            .setEnabled(true)
            .setState("enabled")
            .build()
        val scan = WifiScan.newBuilder()
            .addResults(WifiScanResult.newBuilder()
                .setSsid("very-long-laboratory-network-name-that-would-wrap-the-table")
                .setBssid("aa:bb:cc:dd:ee:ff")
                .setRssiDbm(-55)
                .setBand("6GHz")
                .setFrequencyMhz(6295)
                .setWifiStandard("802.11be")
                .setApMloLinkId(-1)
                .addSecurityTypes("sae")
                .build())
            .addResults(WifiScanResult.newBuilder()
                .setSsid("shishimaru-shinyurigaoka-shop")
                .setBssid("aa:bb:cc:dd:ee:01")
                .setRssiDbm(-62)
                .setBand("2.4GHz")
                .setFrequencyMhz(2457)
                .setWifiStandard("802.11be")
                .setApMloLinkId(-1)
                .addSecurityTypes("psk")
                .build())
            .build()

        val lines = AgentWifiMloRenderer.render(status, scan)
        val tableLines = lines.dropWhile { it != "Nearby EHT APs" }.drop(1).take(3)
        val out = lines.joinToString("\n")

        assertTrue("rendered output missing capped SSID:\n$out", tableLines.any { it.contains("...") })
        tableLines.forEach { line ->
            assertTrue("table line too wide (${line.length}): $line\n$out", line.length <= 80)
        }
        assertTrue(out.contains("scan_mlo_metadata_absent 11be_results=2 ap_mld=0 link_id=0"))
        assertTrue(out.contains("EHT Scan Links"))
        assertFalse(out.contains("[-] 獅子丸新百合ヶ丘店"))
    }

    private fun mloConnection(): WifiConnection {
        return WifiConnection.newBuilder()
            .setSsid("Lab")
            .setBssid("aa:bb:cc:dd:ee:ff")
            .setRssiDbm(-45)
            .setFrequencyMhz(5975)
            .setWifiStandard("802.11be")
            .setApMldMacAddress("02:00:00:00:00:01")
            .setApMloLinkId(2)
            .addAssociatedMloLinks(mloLink(2, "active", "6ghz", 5, -45))
            .addAffiliatedMloLinks(mloLink(1, "idle", "5ghz", 44, -55))
            .setHeCapabilities(heCapabilities())
            .setHeOperation(heOperation())
            .setHeSpatialReuseParameterSet(heSpatialReuse())
            .setEhtCapabilities(ehtCapabilities())
            .setEhtOperation(ehtOperation())
            .addInformationElements(ehtMultiLinkIe())
            .addInformationElements(rnrIe())
            .addInformationElements(rmEnabledCapabilitiesIe())
            .addInformationElements(extendedCapabilitiesBssTransitionIe())
            .addInformationElements(mobilityDomainIe())
            .build()
    }

    private fun mloScanResult(ssid: String, bssid: String, rssi: Int, mld: String, linkId: Int): WifiScanResult {
        return WifiScanResult.newBuilder()
            .setSsid(ssid)
            .setBssid(bssid)
            .setRssiDbm(rssi)
            .setBand("6ghz")
            .setFrequencyMhz(5975)
            .setChannelWidth("80MHz")
            .setWifiStandard("802.11be")
            .setApMldMacAddress(mld)
            .setApMloLinkId(linkId)
            .addSecurityTypes("wpa3_sae")
            .setHeCapabilities(heCapabilities())
            .setHeOperation(heOperation())
            .setHeSpatialReuseParameterSet(heSpatialReuse())
            .setEhtCapabilities(ehtCapabilities())
            .setEhtOperation(ehtOperation())
            .addAffiliatedMloLinks(mloLink(1, "idle", "5ghz", 44, -55))
            .addAffiliatedMloLinks(mloLink(2, "active", "6ghz", 5, rssi))
            .addInformationElements(ehtMultiLinkIe())
            .addInformationElements(rmEnabledCapabilitiesIe())
            .addInformationElements(extendedCapabilitiesBssTransitionIe())
            .addInformationElements(mobilityDomainIe())
            .build()
    }

    private fun mloLink(id: Int, state: String, band: String, channel: Int, rssi: Int): MloLinkInfo {
        return MloLinkInfo.newBuilder()
            .setLinkId(id)
            .setState(state)
            .setBand(band)
            .setChannel(channel)
            .setRssiDbm(rssi)
            .setTxLinkSpeedMbps(1200)
            .setRxLinkSpeedMbps(1200)
            .setApMacAddress("02:00:00:00:00:0$id")
            .setStaMacAddress("02:00:00:00:00:1$id")
            .setMaxSupportedTxLinkSpeedMbps(2400)
            .setMaxSupportedRxLinkSpeedMbps(2400)
            .build()
    }

    private fun heOperation(): WifiHeOperation {
        return WifiHeOperation.newBuilder()
            .setParameters(0x40000000)
            .setBasicMcsNssSetHex("ffff")
            .setBssColor(17)
            .setBssColorDisabled(false)
            .addFlags("partial_bss_color")
            .build()
    }

    private fun heCapabilities(): WifiHeCapabilities {
        return WifiHeCapabilities.newBuilder()
            .setMac(WifiHeMacCapabilities.newBuilder()
                .setTwtResponder(true)
                .setOmControl(true)
                .setPuncturedSounding(true))
            .setPhy(WifiHePhyCapabilities.newBuilder()
                .addChannelWidthSet("40_80mhz_in_5ghz")
                .addPreamblePuncturingRx("preamble_puncturing_rx_80mhz_second_20mhz")
                .setDcmMaxConstellationTx("bpsk")
                .setDcmMaxNssTx(1)
                .setDcmMaxConstellationRx("qpsk")
                .setDcmMaxNssRx(2)
                .setSuBeamformer(true)
                .setSrpBasedSpatialReuse(true)
                .setNominalPacketPadding("16us"))
            .addFeatures("twt_responder")
            .addFeatures("punctured_sounding")
            .build()
    }

    private fun ehtCapabilities(): WifiEhtCapabilities {
        return WifiEhtCapabilities.newBuilder()
            .setMac(WifiEhtMacCapabilities.newBuilder()
                .setOmControl(true)
                .setTriggeredTxopSharingMode1(true)
                .setScsTrafficDescription(true)
                .setMaxMpduLengthBytes(7991)
                .setLinkAdaptation("no_feedback"))
            .setPhy(WifiEhtPhyCapabilities.newBuilder()
                .setSupports320MhzIn6Ghz(true)
                .setSupports242ToneRuGt20Mhz(true)
                .setMaxSupportedEhtLtf(3)
                .setExtraEhtLtfSupported(true)
                .setPsrSpatialReuse(true)
                .setRx4096QamWiderBwDlOfdma(true)
                .setCommonNominalPacketPadding("16us"))
            .addFeatures("320mhz_in_6ghz")
            .addFeatures("rx_4096qam_wider_dl_ofdma")
            .build()
    }

    private fun ehtOperation(): WifiEhtOperation {
        return WifiEhtOperation.newBuilder()
            .setParameters(0x03)
            .addFlags("operation_information_present")
            .addFlags("disabled_subchannel_bitmap_present")
            .setOperationInformationPresent(true)
            .setDisabledSubchannelBitmapPresent(true)
            .setChannelWidth("320MHz")
            .setChannelWidthMhz(320)
            .setChannelWidthCode(4)
            .setCenterFreqSegment0(31)
            .setCenterFreqSegment1(63)
            .setDisabledSubchannelBitmap(0x0a)
            .setDisabledSubchannelBitmapHex("000a")
            .addDisabledSubchannelIndices(1)
            .addDisabledSubchannelIndices(3)
            .build()
    }

    private fun heSpatialReuse(): WifiHeSpatialReuseParameterSet {
        return WifiHeSpatialReuseParameterSet.newBuilder()
            .setSrControl(0x08)
            .addFlags("srg_information_present")
            .setSrgObssPdMinOffset(20)
            .setSrgObssPdMaxOffset(30)
            .setSrgBssColorBitmapHex("0102030405060708")
            .setSrgPartialBssidBitmapHex("1112131415161718")
            .build()
    }

    private fun ehtMultiLinkIe(): io.dropcheck.agent.grpc.WifiInformationElement {
        return io.dropcheck.agent.grpc.WifiInformationElement.newBuilder()
            .setId(255)
            .setIdExt(107)
            .setByteCount(37)
            .setBytesHex("f00712020000000001020710328f083370056100000f72090c0200000000026400010305dd")
            .build()
    }

    private fun detailedEhtMultiLinkIe(): io.dropcheck.agent.grpc.WifiInformationElement {
        return io.dropcheck.agent.grpc.WifiInformationElement.newBuilder()
            .setId(255)
            .setIdExt(107)
            .setByteCount(53)
            .setBytesHex("3000090200000000010207001672090c020000000002640001030500034c6162ff036afe080102ff046c010203dd060011220799aa")
            .build()
    }

    private fun rnrIe(): io.dropcheck.agent.grpc.WifiInformationElement {
        return io.dropcheck.agent.grpc.WifiInformationElement.newBuilder()
            .setId(201)
            .setByteCount(20)
            .setBytesHex("001085050aaabbccddeeff112233448015073210")
            .build()
    }

    private fun rmEnabledCapabilitiesIe(): io.dropcheck.agent.grpc.WifiInformationElement {
        return io.dropcheck.agent.grpc.WifiInformationElement.newBuilder()
            .setId(70)
            .setByteCount(5)
            .setBytesHex("0000000000")
            .build()
    }

    private fun extendedCapabilitiesBssTransitionIe(): io.dropcheck.agent.grpc.WifiInformationElement {
        return io.dropcheck.agent.grpc.WifiInformationElement.newBuilder()
            .setId(127)
            .setByteCount(3)
            .setBytesHex("000008")
            .build()
    }

    private fun mobilityDomainIe(): io.dropcheck.agent.grpc.WifiInformationElement {
        return io.dropcheck.agent.grpc.WifiInformationElement.newBuilder()
            .setId(54)
            .setByteCount(3)
            .setBytesHex("010203")
            .build()
    }

    private fun multipleBssidIe(): io.dropcheck.agent.grpc.WifiInformationElement {
        return io.dropcheck.agent.grpc.WifiInformationElement.newBuilder()
            .setId(71)
            .setByteCount(55)
            .setBytesHex("02003400034c61625502040353023412ff05380130016a301e0100000fac090100000fac090200000fac18000fac19c0000000000fac0c")
            .build()
    }

    private fun wifiCapabilities(): WifiCapabilities {
        return WifiCapabilities.newBuilder()
            .addSupportedBands("6GHz")
            .addSupportedStandards("802.11be")
            .addSupportedSecurityModes("wpa3_sae")
            .addSupportedSecurityModes("wpa3_sae_h2e")
            .addSupportedSecurityModes("wpa3_sae_public_key")
            .addSupportedSecurityModes("owe")
            .addSupportedFeatures("tid_to_link_mapping_negotiation")
            .addSupportedFeatures("dual_band_simultaneous")
            .addSupportedFeatures("sta_concurrency_multi_internet")
            .build()
    }

    private fun field(key: String, value: String): DiagnosticField {
        return DiagnosticField.newBuilder()
            .setKey(key)
            .setValue(value)
            .build()
    }
}
