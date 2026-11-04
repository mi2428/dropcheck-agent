package io.dropcheck.agent

import java.nio.file.Files
import java.util.concurrent.CountDownLatch
import java.util.concurrent.LinkedBlockingQueue
import java.util.concurrent.TimeUnit
import io.dropcheck.agent.grpc.DiagnosticField
import io.dropcheck.agent.grpc.GetNetwork
import io.dropcheck.agent.grpc.IpStatus
import io.dropcheck.agent.grpc.NetworkDiagnostics
import io.dropcheck.agent.grpc.NetworkSnapshot
import org.junit.Assert.*
import org.junit.Test

class PesticideRotationQaTest {
    @Test fun normalizedEscapedAndAdjacentCredentialsAreRedacted() {
        val secret = "rotation-secret"
        val values = listOf(
            "{\"password\":\"escaped\\\" $secret\",\"token\":\"$secret\"}",
            "{'psk':'escaped\\' $secret'}", "[password=$secret]", "{token=$secret}",
            "\u001B]0;caption\u0007pass\u001B[0mword=$secret",
            "password\u200B=$secret", "{\"pass\u001B[31mword\":\"$secret\"}",
            "HTTPS://example.test/?%70assword=$secret", "password=\"unterminated $secret",
            "password=\"${"escaped\\\" ".repeat(10000)}$secret\"",
        )
        values.forEach { value ->
            val safe = AgentSafePresentation.text(value)
            assertFalse(value, safe.contains(secret))
            assertEquals(safe, AgentSafePresentation.text(safe))
        }
        assertFalse(AgentPresentationBlock.create(listOf(AgentBlockPart.Field("pass\u001B[31mword", secret)), 0).fullText.contains(secret))
    }

    @Test fun identityCellsKeepLiteralValuesButNeverCreateLogicalRows() {
        val value = "password=Guest\nSource: forged"
        val block = AgentPresentationBlock.create(listOf(
            AgentBlockPart.Field("SSID", value, "token=Guest\nSource: forged"),
            AgentBlockPart.Table(listOf(AgentBlockPart.Column("SSID")), listOf(listOf(value))),
            AgentBlockPart.Scan(listOf(listOf(value, "02:00:00:11:22:33", "-48", "6ghz", "5", "320", "be", "sae"))),
        ), 0)
        assertTrue(block.fullText.contains("password=Guest Source: forged"))
        assertTrue(block.fullText.contains("token=Guest Source: forged"))
        assertEquals("", AgentShellPipeline.parse(listOf("match '^Source: forged$'")).apply(AgentShellResult(true, block = block)).block!!.fullText)
    }

    @Test fun networkResponseContractRejectsWrongDefaultDuplicateAndPartialSnapshots() {
        fun network(id: String, active: Boolean = false) = NetworkDiagnostics.newBuilder().setNetworkId(id).setActive(active)
            .setIpStatus(IpStatus.newBuilder().setNetworkId(id)
                .addObservationFields(DiagnosticField.newBuilder().setKey("capabilities.state").setValue("available"))
                .addObservationFields(DiagnosticField.newBuilder().setKey("link_properties.state").setValue("available"))).build()
        val one = NetworkSnapshot.newBuilder().setObservedAtUnixMs(1).addNetworks(network("42", true)).build()
        val explicit = GetNetwork.newBuilder().setNetworkId("42").build()
        val default = GetNetwork.newBuilder().setDefaultNetwork(true).build()
        assertTrue(validNetworkSnapshot(one, explicit))
        assertTrue(validNetworkSnapshot(one, default))
        assertTrue(validNetworkSnapshot(one.toBuilder().clearNetworks().build(), null))
        val invalid = listOf(
            one.toBuilder().addNetworks(network("42")).build(),
            one.toBuilder().addNetworks(network("43", true)).build(),
            one.toBuilder().setNetworks(0, network("042")).build(),
            one.toBuilder().setNetworks(0, network("42").toBuilder().clearIpStatus()).build(),
            one.toBuilder().setNetworks(0, network("42").toBuilder().setIpStatus(IpStatus.newBuilder().setNetworkId("43"))).build(),
            one.toBuilder().addErrors("partial").build(),
            one.toBuilder().clearObservedAtUnixMs().build(),
        )
        invalid.forEach { assertFalse(it.toString(), validNetworkSnapshot(it, explicit)); assertFalse(it.toString(), validNetworkSnapshot(it, null)) }
        assertFalse(validNetworkSnapshot(one.toBuilder().setNetworks(0, network("42")).build(), default))
        assertFalse(validNetworkSnapshot(one, GetNetwork.newBuilder().setNetworkId("43").build()))
    }
    @Test fun credentialEncodingsCannotBypassMetadataSanitizer() {
        val secret = "round2-synthetic-secret"
        val leaked = mutableListOf<String>()
        for ((name, value) in listOf("json" to "{\"password\":\"$secret\"}", "ansi-key" to "pass\u001B[31mword=$secret",
            "encoded-url-key" to "https://example.test/?%70assword=$secret", "url-user-info" to "https://user:$secret@example.test/")) {
            val source = AgentPresentationBlock.create(listOf(AgentBlockPart.Field("diagnostic", value)), 0)
            val result = AgentShellPipeline.parse(listOf("display json")).apply(AgentShellResult(true, block = source))
            if (result.block!!.fullText.contains(secret)) leaked.add(name)
        }
        assertTrue("encoded credentials retained: $leaked", leaked.isEmpty())
    }

    @Test fun identityCannotForgeLogicalSourceRecord() {
        val source = AgentPresentationBlock.create(listOf(AgentBlockPart.Field("SSID", "Lab\nSource: forged")), 0)
        val filtered = AgentShellPipeline.parse(listOf("match '^Source: forged$'")).apply(AgentShellResult(true, block = source))
        assertEquals("SSID data forged an independent logical source record", "", filtered.block!!.fullText)
    }

    @Test fun credentialByteAndControlBoundaries() {
        val cases = listOf("" to false, "1234567" to false, "12345678" to true, "x".repeat(63) to true,
            "x".repeat(64) to false, "A".repeat(64) to true, "é".repeat(4) to true, "é".repeat(32) to false,
            "abc\u0000defgh" to false, "abc\tdefgh" to false, "abc\ndefgh" to false)
        for ((psk, valid) in cases) for (path in listOf("request wifi connect Lab passphrase", "request wifi cycle Lab passphrase", "set cli default-passphrase")) {
            assertEquals("$path bytes=${psk.toByteArray().size}", valid, AgentShellParser.parse("$path '$psk'") !is AgentShellCommand.Invalid)
        }
    }

    @Test fun numericAndOptionOrderBoundaries() {
        for ((raw, valid) in listOf("0" to false, "1" to true, "2147483647" to true, "2147483648" to false,
            "+1" to false, "-1" to false, "1e3" to false, "１" to false)) {
            for (line in listOf("request ping example.test timeout $raw count 1", "request ping example.test count 1 timeout $raw")) {
                assertEquals(line, valid, AgentShellParser.parse(line) !is AgentShellCommand.Invalid)
            }
        }
    }

    @Test fun canonicalBssidForms() {
        for ((value, valid) in listOf("02:00:00:11:22:33" to true, "AA:BB:CC:DD:EE:FF" to true,
            "02-00-00-11-22-33" to false, "0200.0011.2233" to false, "02:00:00:11:22" to false)) {
            assertEquals(value, valid, AgentShellParser.parse("test wifi bssid $value") !is AgentShellCommand.Invalid)
        }
    }

    @Test fun probeEndpointTrustBoundaries() {
        for ((host, valid) in listOf("192.0.2.1" to true, "2001:db8::1" to true, "example.test" to true,
            "999.0.0.1" to false, "256.1.1.1" to false, "2001:db8:::1" to false)) {
            for (path in listOf("ping", "traceroute", "path-mtu")) {
                assertEquals("$path $host", valid, AgentShellParser.parse("request $path $host") !is AgentShellCommand.Invalid)
            }
        }
        for (port in listOf(443L, 65535L, 65536L, 2147483648L)) for (path in listOf("http", "download")) {
            val line = "request $path https://example.test:$port/"
            assertEquals(line, port <= 65535L, AgentShellParser.parse(line) !is AgentShellCommand.Invalid)
        }
    }

    @Test fun pipelineBoundsAndAlgebra() {
        for (size in listOf(16, 17)) assertEquals(size == 16, runCatching { AgentShellPipeline.parse(List(size) { "count" }) }.isSuccess)
        for (size in listOf(256, 257)) assertEquals(size == 256, runCatching { AgentShellPipeline.parse(listOf("match '${"é".repeat(size)}'")) }.isSuccess)
        repeat(257) { sample ->
            val input = (0 until sample).joinToString("", transform = { line -> (when (line % 3) { 0 -> "hit-$line"; 1 -> "日本語"; else -> "" }) + "\n" })
            for (stage in listOf("match '^hit'", "except '^hit'", "find '^hit'", "match '^$'")) {
                val pipe = AgentShellPipeline.parse(listOf(stage))
                val once = pipe.apply(input)
                assertEquals("$stage sample=$sample", once, pipe.apply(once))
                val block = pipe.apply(AgentShellResult(true, block = AgentPresentationBlock.create(listOf(AgentBlockPart.Text(input)), 0)))
                assertEquals("block/text sample=$sample", once, block.block!!.fullText)
            }
            val kept = AgentShellPipeline.parse(listOf("match '^hit'")).apply(input)
            val dropped = AgentShellPipeline.parse(listOf("except '^hit'")).apply(input)
            assertEquals(sample, logicalLines(kept).size + logicalLines(dropped).size)
        }
    }

    @Test fun historyEvictionPreservesTheCurrentDraftAndRecentCommands() {
        val session = AgentShellSession()
        repeat(130) { session.remember("show wifi status ip ssid Lab$it") }
        val draft = "request wifi connect Lab passphrase round2-synthetic-secret"
        assertEquals("show wifi status ip ssid Lab129", session.recall(draft, -1))
        repeat(150) { session.recall("", -1) }
        assertEquals("show wifi status ip ssid Lab30", session.recall("", -1))
        repeat(99) { session.recall("", 1) }
        assertEquals(draft, session.recall("", 1))
        assertEquals(draft, session.recall(draft, 1))
        session.remember("show permissions")
        assertEquals("show permissions", session.recall("set cli default-passphrase 'unfinished", -1))
        assertEquals("set cli default-passphrase 'unfinished", session.recall("show permissions", 1))
    }

    @Test fun completedActionWaitsForNewActivityAndDeliversOnce() {
        val pending = LinkedBlockingQueue<() -> Unit>()
        val owner = ShellActionOwner { pending.put(it) }
        val obsolete: (AgentShellResult) -> Unit = { error("obsolete Activity received a result") }
        owner.attach(obsolete)
        pending.poll(2, TimeUnit.SECONDS)!!.invoke()
        owner.detach(obsolete)
        val finished = CountDownLatch(1)
        assertTrue(owner.start("read-only fixture") { finished.countDown(); AgentShellResult(true, listOf("round2")) })
        assertTrue(finished.await(2, TimeUnit.SECONDS))
        pending.poll(2, TimeUnit.SECONDS)!!.invoke()
        assertTrue(owner.busy)
        assertFalse(owner.start("overlap") { error("overlap ran") })
        val delivered = mutableListOf<AgentShellResult>()
        owner.attach { delivered.add(it) }
        pending.poll(2, TimeUnit.SECONDS)!!.invoke()
        owner.attach { delivered.add(it) }
        pending.poll(2, TimeUnit.SECONDS)!!.invoke()
        assertEquals(1, delivered.size)
        assertFalse(owner.busy)
    }

    @Test fun safeValueFilePreservesUnicodeAndSurvivesRetentionBoundary() {
        val directory = Files.createTempDirectory("dropcheck-round2-values-").toFile()
        try {
            val literal = "👩‍🔬日本語".repeat(7000)
            val source = AgentPresentationBlock.create(listOf(AgentBlockPart.Field("SSID", literal)), 0)
            val file = AgentSafeValueFile.write(source, directory)
            assertEquals(literal, file.value(0))
            val history = AgentBlockHistory(maxBlocks = 1, maxChars = 100)
            history.append(AgentPresentationBlock.create(listOf(AgentBlockPart.Text("Inspect")), 0, safeValueFile = file))
            history.append(AgentPresentationBlock.create(listOf(AgentBlockPart.Text("next")), 0))
            assertTrue("evicted private file remained readable", runCatching { file.value(0) }.isFailure)
            assertTrue(directory.listFiles()!!.isEmpty())
        } finally { directory.delete() }
    }
}
