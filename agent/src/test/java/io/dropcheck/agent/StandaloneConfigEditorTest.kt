package io.dropcheck.agent

import io.dropcheck.agent.grpc.ConnectWifi
import io.dropcheck.agent.grpc.DnsRecordType
import io.dropcheck.agent.grpc.StandaloneConfig
import io.dropcheck.agent.grpc.StandaloneEdit
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
    fun appliesWifiMatchAndDeleteEdits() {
        val configured = StandaloneConfigEditor.apply(
            StandaloneConfig.getDefaultInstance(),
            listOf(
                setEdit("Lab SSID", "festa", "smoke", "wifi", "lab", "match", "essid"),
                deleteEdit("festa", "smoke", "wifi", "lab"),
            ),
        )

        assertNull(configured.error)
        assertEquals(1, configured.config.festasCount)
        assertEquals(0, configured.config.getFestas(0).wifiGroupsCount)
    }

    @Test
    fun appliesFestaWifiMatchEditFromShellSyntax() {
        val result = StandaloneConfigEditor.apply(
            StandaloneConfig.getDefaultInstance(),
            listOf(setEdit("SHIZK RADIO MOBILE", "festa", "smoke", "wifi", "lab2", "match", "essid")),
        )

        assertNull(result.error)
        val group = result.config.getFestas(0).getWifiGroups(0)
        assertEquals("lab2", group.name)
        assertEquals("SHIZK RADIO MOBILE", group.essid)
    }

    @Test
    fun appliesScalarStandaloneAndWifiEdits() {
        val result = StandaloneConfigEditor.apply(
            StandaloneConfig.getDefaultInstance(),
            listOf(
                setEdit("true", "enabled"),
                setEdit("60000", "retention_ms"),
                setEdit("1048576", "max_bytes"),
                setEdit("30000", "festa", "smoke", "interval_ms"),
                setEdit("secret", "festa", "smoke", "wifi", "lab", "passphrase"),
                setEdit("wpa3", "festa", "smoke", "wifi", "lab", "security"),
                setEdit("5ghz", "festa", "smoke", "wifi", "lab", "band"),
                setEdit("non-persistent", "festa", "smoke", "wifi", "lab", "mac_randomization"),
                setEdit("45000", "festa", "smoke", "wifi", "lab", "timeout_ms"),
            ),
        )

        assertNull(result.error)
        assertTrue(result.config.enabled)
        assertEquals(60000, result.config.retentionMs)
        assertEquals(1048576L, result.config.maxBytes)
        val festa = result.config.getFestas(0)
        assertEquals(30000, festa.intervalMs)
        val group = festa.getWifiGroups(0)
        assertEquals("secret", group.passphrase)
        assertEquals(ConnectWifi.Security.SECURITY_WPA3_SAE, group.security)
        assertEquals(WifiBand.WIFI_BAND_5_GHZ, group.band)
        assertEquals(ConnectWifi.MacRandomization.MAC_RANDOMIZATION_NON_PERSISTENT, group.macRandomization)
        assertEquals(45000, group.timeoutMs)
    }

    @Test
    fun appliesNamedCheckEdits() {
        val result = StandaloneConfigEditor.apply(
            StandaloneConfig.getDefaultInstance(),
            listOf(
                setEdit("ping", "festa", "smoke", "check", "cloudflare", "test"),
                setEdit("1.1.1.1", "festa", "smoke", "check", "cloudflare", "host"),
                setEdit("1", "festa", "smoke", "check", "cloudflare", "count"),
                setEdit("8000", "festa", "smoke", "check", "cloudflare", "timeout_ms"),
                setEdit("dns", "festa", "smoke", "check", "dns-main", "test"),
                setEdit("example.test", "festa", "smoke", "check", "dns-main", "name"),
                setEdit("AAAA", "festa", "smoke", "check", "dns-main", "qtypes"),
            ),
        )

        assertNull(result.error)
        val checks = result.config.getFestas(0).checksList
        assertEquals(listOf("cloudflare", "dns-main"), checks.map { it.name })
        assertTrue(checks[0].hasPing())
        assertEquals("1.1.1.1", checks[0].ping.host)
        assertEquals(1, checks[0].ping.count)
        assertEquals(8000, checks[0].ping.timeoutMs)
        assertTrue(checks[1].hasDns())
        assertEquals("example.test", checks[1].dns.name)
        assertEquals(listOf(DnsRecordType.DNS_RECORD_TYPE_AAAA), checks[1].dns.qtypesList)
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
