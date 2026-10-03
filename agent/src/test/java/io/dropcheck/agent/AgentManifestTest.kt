package io.dropcheck.agent

import java.io.File
import javax.xml.parsers.DocumentBuilderFactory
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class AgentManifestTest {
    @Test
    fun exportedServiceRequiresPlatformDumpPermissionForCrossUidCallers() {
        val services = parseManifest().getElementsByTagName("service")
        val service = (0 until services.length).map { services.item(it) }.single {
            it.attributes.getNamedItemNS(ANDROID_NS, "name")?.nodeValue == ".AgentService"
        }
        assertEquals("true", service.attributes.getNamedItemNS(ANDROID_NS, "exported")?.nodeValue)
        assertEquals("android.permission.DUMP", service.attributes.getNamedItemNS(ANDROID_NS, "permission")?.nodeValue)
        // The agent itself needs no DUMP grant; Android permits calls by the owning UID.
        assertFalse(manifestFile().readText().contains("<uses-permission android:name=\"android.permission.DUMP\""))
    }

    @Test
    fun agentServiceUsesConnectedDeviceAndLocationForegroundTypes() {
        val manifest = parseManifest()
        val services = manifest.getElementsByTagName("service")
        var serviceType: String? = null
        for (index in 0 until services.length) {
            val node = services.item(index)
            val attrs = node.attributes
            if (attrs.getNamedItemNS(ANDROID_NS, "name")?.nodeValue == ".AgentService") {
                serviceType = attrs.getNamedItemNS(ANDROID_NS, "foregroundServiceType")?.nodeValue
                break
            }
        }

        assertEquals("connectedDevice|location", serviceType)
    }

    @Test
    fun manifestDeclaresBackgroundLocationAndLocationForegroundServicePermissions() {
        val manifestText = manifestFile().readText()

        assertTrue(manifestText.contains("android.permission.ACCESS_BACKGROUND_LOCATION"))
        assertTrue(manifestText.contains("android.permission.FOREGROUND_SERVICE_CONNECTED_DEVICE"))
        assertTrue(manifestText.contains("android.permission.FOREGROUND_SERVICE_LOCATION"))
        assertFalse(manifestText.contains("android.permission.FOREGROUND_SERVICE_DATA_SYNC"))
        assertFalse(manifestText.contains("neverForLocation"))
    }

    @Test
    fun clockWidgetDeclaresNetworkCallbackAction() {
        val manifestText = manifestFile().readText()

        assertTrue(manifestText.contains("io.dropcheck.agent.action.CLOCK_WIDGET_NETWORK_CALLBACK_UPDATE"))
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
