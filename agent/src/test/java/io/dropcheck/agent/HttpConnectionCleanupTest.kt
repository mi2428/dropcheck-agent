package io.dropcheck.agent

import java.net.HttpURLConnection
import java.net.URL
import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Test

class HttpConnectionCleanupTest {
    @Test
    fun closesConnectionOnSuccessFailureAndInterrupt() {
        for (failure in listOf<Exception?>(null, IllegalStateException("read"), InterruptedException("cancel"))) {
            var disconnects = 0
            val connection = object : HttpURLConnection(URL("http://localhost/")) {
                override fun connect() = Unit
                override fun disconnect() { disconnects++ }
                override fun usingProxy() = false
            }
            if (failure == null) {
                assertEquals(42, withHttpConnection(connection) { 42 })
            } else {
                val caught = assertThrows(failure.javaClass) {
                    withHttpConnection(connection) { throw failure }
                }
                assertEquals(failure, caught)
            }
            assertEquals(1, disconnects)
        }
    }
}
