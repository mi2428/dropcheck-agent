package io.dropcheck.agent

import java.io.File
import javax.xml.parsers.DocumentBuilderFactory
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class AgentManifestTest {
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
