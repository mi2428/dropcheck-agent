package io.dropcheck.agent

import org.junit.Assert.*
import org.junit.Test
import io.dropcheck.agent.grpc.WifiScan
import io.dropcheck.agent.grpc.WifiScanResult

class AdversarialShellQaTest {
    @Test fun malformedCorpusDoesNotThrow() {
        val rng = kotlin.random.Random(20261103)
        val alphabet = "show wifi network passphrase count ?|\\\"' \t\r\n[]()日本語"
        repeat(2000) {
            val line = (0 until rng.nextInt(150)).map { alphabet[rng.nextInt(alphabet.length)] }.joinToString("")
            AgentShellParser.parse(line)
            AgentShellParser.complete(line)
            AgentShellParser.contextHelp(line)
        }
    }
    @Test fun quotedSecretNeverLeaksThroughArgumentHelp() {
        assertFalse(AgentShellParser.contextHelp("request wifi connect Lab passphrase 'qa-synthetic-secret'").joinToString().contains("qa-synthetic-secret"))
    }

    @Test fun historyRestoresEveryDraftAcrossRecall() {
        val session = AgentShellSession()
        session.remember("show permissions")
        for (draft in listOf("request wifi connect Lab passphrase qa-synthetic-secret", "set cli default-passphrase 'unfinished", "show wifi s")) {
            assertEquals("show permissions", session.recall(draft, -1))
            assertEquals(draft, session.recall("show permissions", 1))
        }
    }

    @Test fun finalBlankLogicalLineSurvivesFilterComposition() {
        val once = AgentShellPipeline.parse(listOf("match '^$'")).apply("a\n\nb\n")
        val retained = AgentShellPipeline.parse(listOf("match '^$'")).apply(AgentShellResult(true, block = AgentPresentationBlock.create(listOf(AgentBlockPart.Text("a\n\nb\n")), 0)))
        assertEquals("\n", once)
        assertEquals("Count: 1 lines\n", AgentShellPipeline.parse(listOf("count")).apply(retained.block!!.fullText))
    }

    @Test fun requiredWhitespacePskMatchesPcBoundary() {
        assertTrue("valid 8-byte whitespace PSK rejected", AgentShellParser.parse("request wifi connect Lab passphrase '        '") is AgentShellCommand.Connect)
    }

    @Test fun midTokenCompletionCannotDuplicateSuffix() {
        val line = "show wifi status"
        val cursor = "show wi".length
        val choices = AgentShellParser.complete(line.take(cursor)).filterNot { it.endsWith('>') }
        assertEquals(1, choices.size)
        assertEquals("show wifi status", line.replaceRange(0, shellCompletionEnd(line, cursor)!!, choices.single()))
    }

    @Test fun typedSafeMetadataCannotLeakDiagnosticCredentials() {
        val block = AgentPresentationBlock.create(listOf(AgentBlockPart.Field("fresh_scan_error", "password=qa-synthetic-secret")), 0)
        assertFalse("credential in generic metadata field was not redacted", block.fullText.contains("qa-synthetic-secret"))
    }

    @Test fun uniqueCheckKeywordPrefixMatchesPcBoundary() {
        assertEquals(AgentShellCommand.Check("link", "Lab"), AgentShellParser.parse("test link s Lab"))
    }

    @Test fun filteringCannotReduceFailureToOnlyACountForCopyAndAccessibility() {
        val result = AgentShellPipeline.parse(listOf("count")).apply(AgentShellResult(false, listOf("operation failed")))
        assertFalse(result.ok)
        assertTrue("copy/accessibility text lost every failure indicator", Regex("(?i)fail|error|cancel").containsMatchIn(result.block!!.fullText))
    }

    @Test fun safeVenueFieldsAreReachableInScanDetail() {
        val scan = WifiScan.newBuilder().addResults(WifiScanResult.newBuilder().setSsid("fixture").setBssid("02:00:00:11:22:33")
            .setOperatorFriendlyName("qa-operator-name").setVenueName("qa-venue-name")).build()
        val detail = AgentWifiScanRenderer.render(scan, AgentWifiScanContext(detail = true)).joinToString("\n")
        assertTrue("safe operator field dropped", detail.contains("qa-operator-name"))
        assertTrue("safe venue field dropped", detail.contains("qa-venue-name"))
    }
}
