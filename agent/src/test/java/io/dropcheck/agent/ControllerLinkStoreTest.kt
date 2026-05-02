package io.dropcheck.agent

import io.dropcheck.agent.grpc.ControllerLinkConfig
import java.nio.file.Files
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ControllerLinkStoreTest {
    @Test
    fun storesControllerEndpointConfig() {
        val dir = Files.createTempDirectory("controller-link").toFile()
        try {
            val store = ControllerLinkStore(dir.resolve("config.pb"))
            assertFalse(store.load().enabled)

            val config = ControllerLinkConfig.newBuilder()
                .setEnabled(true)
                .setHost("192.168.7.1")
                .setPort(37588)
                .setToken("secret-token")
                .setAgentId("agent-1")
                .setMinBackoffMs(1000)
                .setMaxBackoffMs(30000)
                .build()

            store.save(config)

            val loaded = store.load()
            assertTrue(loaded.enabled)
            assertEquals("192.168.7.1", loaded.host)
            assertEquals(37588, loaded.port)
            assertEquals("secret-token", loaded.token)
            assertEquals("192.168.7.1:37588", loaded.endpoint())
        } finally {
            dir.deleteRecursively()
        }
    }
}
