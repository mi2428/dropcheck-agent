package io.dropcheck.agent

import io.dropcheck.agent.grpc.CommandResult
import io.dropcheck.agent.grpc.PingResult
import io.dropcheck.agent.grpc.TracerouteResult
import org.junit.Assert.assertTrue
import org.junit.Test

class AgentProbeRendererTest {
    @Test
    fun rendersPingWithPayloadSizeAndOutput() {
        val lines = AgentProbeRenderer.renderPing(
            PingResult.newBuilder()
                .setHost("1.1.1.1")
                .setCount(3)
                .setSizeBytes(64)
                .setInterfaceName("wlan0")
                .setExitCode(0)
                .setElapsedMs(42)
                .setOutput("3 packets transmitted, 3 received\n")
                .build(),
            CommandResult.Status.STATUS_OK,
            "ping passed",
        )
        val out = lines.joinToString("\n")
        assertTrue(out.contains("Ping"))
        assertTrue(out.contains("payload_size   64"))
        assertTrue(out.contains("3 packets transmitted, 3 received"))
    }

    @Test
    fun rendersTracerouteWithPayloadSizeAndExecutable() {
        val lines = AgentProbeRenderer.renderTraceroute(
            TracerouteResult.newBuilder()
                .setHost("example.test")
                .setMaxHops(12)
                .setSizeBytes(80)
                .setInterfaceName("wlan0")
                .setExitCode(0)
                .setElapsedMs(123)
                .setExecutable("ping TTL fallback")
                .setOutput("1  192.0.2.1  1.0 ms\n")
                .build(),
            CommandResult.Status.STATUS_OK,
            "traceroute passed",
        )
        val out = lines.joinToString("\n")
        assertTrue(out.contains("Traceroute"))
        assertTrue(out.contains("max_hops       12"))
        assertTrue(out.contains("payload_size   80"))
        assertTrue(out.contains("ping TTL fallback"))
        assertTrue(out.contains("1  192.0.2.1  1.0 ms"))
    }
}
