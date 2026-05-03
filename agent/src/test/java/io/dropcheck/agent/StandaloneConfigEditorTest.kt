package io.dropcheck.agent

import io.dropcheck.agent.grpc.StandaloneConfig
import io.dropcheck.agent.grpc.StandaloneEdit
import io.dropcheck.agent.grpc.ConnectWifi
import io.dropcheck.agent.grpc.WifiBand
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class StandaloneConfigEditorTest {
    @Test
    fun appliesBooleanFestaEnableEdit() {
        val result = StandaloneConfigEditor.apply(
            StandaloneConfig.getDefaultInstance(),
            listOf(setEdit("true", "festa", "smoke", "enabled")),
        )

        assertNull(result.error)
        assertEquals(1, result.config.festasCount)
        assertEquals("smoke", result.config.getFestas(0).name)
        assertTrue(result.config.getFestas(0).enabled)
    }

    @Test
    fun appliesWifiGroupMatchAndDeleteEdits() {
        val configured = StandaloneConfigEditor.apply(
            StandaloneConfig.getDefaultInstance(),
            listOf(
                setEdit("Lab SSID", "festa", "smoke", "wifi-group", "lab", "match", "essid"),
                deleteEdit("festa", "smoke", "wifi-group", "lab"),
            ),
        )

        assertNull(configured.error)
        assertEquals(1, configured.config.festasCount)
        assertEquals(0, configured.config.getFestas(0).wifiGroupsCount)
    }

    @Test
    fun appliesScalarStandaloneAndWifiGroupEdits() {
        val result = StandaloneConfigEditor.apply(
            StandaloneConfig.getDefaultInstance(),
            listOf(
                setEdit("true", "enabled"),
                setEdit("60000", "retention_ms"),
                setEdit("1048576", "max_bytes"),
                setEdit("30000", "festa", "smoke", "interval_ms"),
                setEdit("wpa3", "festa", "smoke", "wifi-group", "lab", "security"),
                setEdit("5ghz", "festa", "smoke", "wifi-group", "lab", "band"),
                setEdit("45000", "festa", "smoke", "wifi-group", "lab", "timeout_ms"),
            ),
        )

        assertNull(result.error)
        assertTrue(result.config.enabled)
        assertEquals(60000, result.config.retentionMs)
        assertEquals(1048576L, result.config.maxBytes)
        val festa = result.config.getFestas(0)
        assertEquals(30000, festa.intervalMs)
        val group = festa.getWifiGroups(0)
        assertEquals(ConnectWifi.Security.SECURITY_WPA3_SAE, group.security)
        assertEquals(WifiBand.WIFI_BAND_5_GHZ, group.band)
        assertEquals(45000, group.timeoutMs)
    }

    @Test
    fun appliesUploadEdits() {
        val result = StandaloneConfigEditor.apply(
            StandaloneConfig.getDefaultInstance(),
            listOf(
                setEdit("http://192.168.50.10:8080/dropcheck/incoming", "upload", "url"),
                setEdit("NOC", "upload", "wifi", "ssid"),
                setEdit("secret", "upload", "wifi", "passphrase"),
                setEdit("wpa3", "upload", "wifi", "security"),
                setEdit("6ghz", "upload", "wifi", "band"),
                setEdit("non-persistent", "upload", "wifi", "mac_randomization"),
                setEdit("5000", "upload", "wifi", "timeout_ms"),
            ),
        )

        assertNull(result.error)
        assertEquals("http://192.168.50.10:8080/dropcheck/incoming", result.config.upload.url)
        val wifi = result.config.upload.wifi
        assertEquals("NOC", wifi.ssid)
        assertEquals("secret", wifi.passphrase)
        assertEquals(ConnectWifi.Security.SECURITY_WPA3_SAE, wifi.security)
        assertEquals(WifiBand.WIFI_BAND_6_GHZ, wifi.band)
        assertEquals(ConnectWifi.MacRandomization.MAC_RANDOMIZATION_NON_PERSISTENT, wifi.macRandomization)
        assertEquals(5000, wifi.timeoutMs)
    }

    private fun setEdit(value: String, vararg path: String): StandaloneEdit {
        return StandaloneEdit.newBuilder()
            .setAction(StandaloneEdit.Action.ACTION_SET)
            .addAllPath(path.asList())
            .setValue(value)
            .build()
    }

    private fun deleteEdit(vararg path: String): StandaloneEdit {
        return StandaloneEdit.newBuilder()
            .setAction(StandaloneEdit.Action.ACTION_DELETE)
            .addAllPath(path.asList())
            .build()
    }
}
