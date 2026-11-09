package io.dropcheck.agent

import java.io.File
import javax.xml.parsers.DocumentBuilderFactory
import org.w3c.dom.Element
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class AgentManifestTest {
    @Test
    fun exportedServiceRequiresPlatformDumpPermissionForCrossUidCallers() {
        val service = component("service", ".AgentService")
        assertEquals("true", service.getAttributeNS(ANDROID_NS, "exported"))
        assertEquals("android.permission.DUMP", service.getAttributeNS(ANDROID_NS, "permission"))
        // The agent itself needs no DUMP grant; Android permits calls by the owning UID.
        assertFalse("android.permission.DUMP" in permissions())
    }

    @Test
    fun agentServiceUsesConnectedDeviceAndLocationForegroundTypes() {
        val types = component("service", ".AgentService").getAttributeNS(ANDROID_NS, "foregroundServiceType").split('|').toSet()
        assertEquals(setOf("connectedDevice", "location"), types)
    }

    @Test
    fun manifestDeclaresBackgroundLocationAndLocationForegroundServicePermissions() {
        val permissions = permissions()
        assertTrue("android.permission.ACCESS_BACKGROUND_LOCATION" in permissions)
        assertTrue("android.permission.FOREGROUND_SERVICE_CONNECTED_DEVICE" in permissions)
        assertTrue("android.permission.FOREGROUND_SERVICE_LOCATION" in permissions)
        assertFalse("android.permission.FOREGROUND_SERVICE_DATA_SYNC" in permissions)
        val nearby = component("uses-permission", "android.permission.NEARBY_WIFI_DEVICES")
        assertFalse("neverForLocation" in nearby.getAttributeNS(ANDROID_NS, "usesPermissionFlags").split('|'))
    }

    @Test
    fun clockWidgetDeclaresNetworkCallbackAction() {
        val receiver = component("receiver", ".AgentClockWidgetProvider")
        val actions = receiver.getElementsByTagName("action")
        assertTrue((0 until actions.length).any {
            (actions.item(it) as Element).getAttributeNS(ANDROID_NS, "name") == "io.dropcheck.agent.action.CLOCK_WIDGET_NETWORK_CALLBACK_UPDATE"
        })
    }

    private fun permissions() = elements("uses-permission").map { it.getAttributeNS(ANDROID_NS, "name") }.toSet()

    private fun component(tag: String, name: String) = elements(tag).single { it.getAttributeNS(ANDROID_NS, "name") == name }

    private fun elements(tag: String) = parseManifest().getElementsByTagName(tag).let { nodes ->
        (0 until nodes.length).map { nodes.item(it) as Element }
    }

    private fun parseManifest() = DocumentBuilderFactory.newInstance().apply {
        isNamespaceAware = true
    }.newDocumentBuilder().parse(manifestFile())

    private fun manifestFile(): File {
        return listOf(
            File("src/main/AndroidManifest.xml"),
            File("agent/src/main/AndroidManifest.xml"),
        ).first { it.isFile }
    }

    private companion object {
        const val ANDROID_NS = "http://schemas.android.com/apk/res/android"
    }
}
