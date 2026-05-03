package io.dropcheck.agent

import java.io.File
import javax.xml.parsers.DocumentBuilderFactory
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class AgentManifestTest {
    @Test
    fun agentServiceUsesConnectedDeviceForegroundTypeOnly() {
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

        assertEquals("connectedDevice", serviceType)
    }

    @Test
    fun manifestDeclaresConnectedDeviceForegroundServicePermissionOnly() {
        val manifestText = manifestFile().readText()

        assertTrue(manifestText.contains("android.permission.FOREGROUND_SERVICE_CONNECTED_DEVICE"))
        assertFalse(manifestText.contains("android.permission.FOREGROUND_SERVICE_DATA_SYNC"))
        assertFalse(manifestText.contains("android.permission.FOREGROUND_SERVICE_LOCATION"))
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
