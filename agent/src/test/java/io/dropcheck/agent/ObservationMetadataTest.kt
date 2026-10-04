package io.dropcheck.agent

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class ObservationMetadataTest {
    @Test fun scanAgeUsesBootClockAndPreservesZero() {
        assertNull(observationAgeMs(0, 1000))
        assertNull(observationAgeMs(1001, 1000))
        assertEquals(0L, observationAgeMs(1000, 1000))
        assertEquals(12L, observationAgeMs(1000, 13000))
    }

    @Test fun unavailableHasAReasonNotAZero() {
        val fields = observationAvailability("rates", false, "requires API 34")
        assertEquals("unavailable", fields.first().value)
        assertEquals("requires API 34", fields.last().value)
        assertEquals(1, observationAvailability("rates", true).size)
    }

    @Test fun nativeTraceIsTypedBeforeRawDisplayClipping() {
        val output = "1 router (192.0.2.1) 1.2 ms\n2 192.0.2.10 2 ms 192.0.2.11 3 ms\n3 * * *"
        val observed = tracerouteObservation(output, "192.0.2.10")
        assertEquals(true, observed.reached)
        assertEquals(3, observed.hops.size)
        assertEquals(listOf("192.0.2.10", "192.0.2.11"), observed.hops[1].addressesList)
        assertTrue(observed.hops[2].timedOut)
        assertNull(tracerouteObservation("traceroute header only", "192.0.2.1").reached)
    }
}
