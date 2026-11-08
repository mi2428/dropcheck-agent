package io.dropcheck.agent

import android.app.Activity
import android.app.Instrumentation
import android.content.res.Configuration
import android.graphics.Typeface
import android.os.Bundle
import android.view.View
import android.view.ViewGroup
import android.widget.TextView
import io.dropcheck.agent.grpc.CommandResult
import io.dropcheck.agent.grpc.DiagnosticCheck
import io.dropcheck.agent.grpc.WifiAssertResult
import kotlin.math.ceil

/** Native-only synthetic fitting check. No service, network, scan or probe invocation. */
class PresentationInstrumentation : Instrumentation() {
    override fun onCreate(arguments: Bundle?) { start() }

    override fun onStart() {
        val result = Bundle()
        var checked = 0
        try {
            var nativeFailure: Throwable? = null
            runOnMainSync {
                try {
                val block = AgentPresentationBlock.create(listOf(
                    AgentBlockPart.Text("Wi-Fi scan FAILED 9000ms"),
                    AgentBlockPart.Field("Source", "android"),
                    AgentBlockPart.Field("Target", "self"),
                    AgentBlockPart.Field("Reason", "fresh scan timed out"),
                    AgentBlockPart.Field("Data", "cached (refresh failed)"),
                    AgentBlockPart.Scan(listOf(
                        listOf("東京Ｇ e\u0301 👩\u200d🔬 Lab Case", "02:00:00:11:22:33", "-48", "6G", "37", "320", "be", "psk+sae+future_Mode", "51845.845"),
                        listOf("東京Ｇ e\u0301 👩\u200d🔬 Lab Case", "06:00:00:11:22:33", "-55", "5G", "36", "80", "ax", "psk+sae", "?"),
                    ), scanColumns + AgentBlockPart.Column("AGE(s)")),
                    AgentBlockPart.Field("IPv6", "ffff:ffff:ffff:ffff:ffff:ffff:255.255.255.255%long-interface-name/128"),
                    AgentBlockPart.Text("Connection\n  Identity"),
                    AgentBlockPart.Field("    SSID".padEnd(32), "password=Guest 東京 Case"),
                    AgentBlockPart.Field("    IPv6".padEnd(32), "2001:db8:ffff:ffff:ffff:ffff:ffff:ffff/64"),
                    AgentBlockPart.Field("    Servers".padEnd(32), "192.0.2.53\n2001:db8::53"),
                    AgentBlockPart.Text("Aligned metadata"),
                    AgentBlockPart.Field("Requested", "cached"),
                    AgentBlockPart.Field("更新経過", "0ms"),
                    AgentBlockPart.Field("    mac flags", "epcs,om_control,triggered_txop_mode1,restricted_twt,scs_traffic_description,trs,txop_return,two_bqrs,unsol_epcs"),
                    AgentBlockPart.Table(listOf(AgentBlockPart.Column("ITEM"), AgentBlockPart.Column("DETAIL")),
                        listOf(listOf("line-break", "first-line\nsecond-line"))),
                    AgentBlockPart.Text("Indent check"),
                    AgentBlockPart.Text("    " + "continuation ".repeat(16)),
                    AgentBlockPart.Text("Indent end"),
                    AgentBlockPart.Table(listOf("CJK", "VALUE", "AGE(s)").map { AgentBlockPart.Column(it) },
                        listOf(listOf("日本語", "same", "0"), listOf("ascii", "longer", "123.456"))),
                ), 0, 12f, 10f)
                val metadata = AgentShellTextFormatter.block(CommandResult.newBuilder().setStatus(CommandResult.Status.STATUS_CANCELED)
                    .setMessage("partial\nTarget: literal").build(), emptyList(), 0)
                for (scale in listOf(1f, 1.3f, 2f)) {
                    val configuration = Configuration(targetContext.resources.configuration).apply { fontScale = scale }
                    val context = targetContext.createConfigurationContext(configuration)
                    for (columns in listOf(24, 32, 40, 48, 60, 80, 96, 120)) {
                        val view = TextView(context).apply {
                            typeface = Typeface.MONOSPACE
                            textSize = block.baseSizeSp
                            includeFontPadding = false
                            textDirection = View.TEXT_DIRECTION_LTR
                            setPadding(24, 0, 32, 0) // Own padding already includes synthetic safe insets.
                        }
                        val content = ceil(columns * view.paint.measureText("0")).toInt()
                        view.layoutParams = ViewGroup.LayoutParams(content + 56, ViewGroup.LayoutParams.WRAP_CONTENT)
                        view.measure(View.MeasureSpec.makeMeasureSpec(content + 56, View.MeasureSpec.EXACTLY), View.MeasureSpec.makeMeasureSpec(0, View.MeasureSpec.UNSPECIFIED))
                        view.layout(0, 0, view.measuredWidth, view.measuredHeight)
                        val actualContent = view.width - view.paddingLeft - view.paddingRight
                        check(actualContent == content)
                        check(AgentNativeBlockLayout.render(block, 0, view.paint) == null)
                        val rendered = checkNotNull(AgentNativeBlockLayout.render(block, actualContent, view.paint))
                        val header = checkNotNull(AgentNativeBlockLayout.render(metadata, actualContent, view.paint)).text.toString()
                        check(header.filterNot(Char::isWhitespace).contains("Source:androidTarget:self")) { "native generated metadata conflated or lost" }
                        check(header.filterNot(Char::isWhitespace).contains("Message:partialTarget:literal")) { "native multiline message lost" }
                        view.text = rendered.text
                        view.measure(View.MeasureSpec.makeMeasureSpec(content + 56, View.MeasureSpec.EXACTLY), View.MeasureSpec.makeMeasureSpec(0, View.MeasureSpec.UNSPECIFIED))
                        view.layout(0, 0, view.measuredWidth, view.measuredHeight)
                        for (line in 0 until view.layout.lineCount) check(view.layout.getLineWidth(line) <= actualContent + 0.5f) { "native line overflow scale=$scale columns=$columns line=$line" }
                        check(rendered.text.toString().contains("FAILED"))
                        check(block.fullText.contains("02:00:00:11:22:33") && block.fullText.contains("06:00:00:11:22:33"))
                        check(block.fullText.contains("psk+sae+future_Mode"))
                        check(rendered.text.toString().contains("AGE(s)"))
                        check(rendered.text.toString().contains("51845.845"))
                        check(rendered.text.toString().contains("second-line"))
                        val continuation = rendered.text.toString().substringAfter("\nIndent check\n").substringBefore("\nIndent end")
                        check(continuation.lines().all { it.startsWith("    ") }) { "wrapped reference escaped its indentation" }
                        check(rendered.text.toString().filterNot(Char::isWhitespace).contains("password=Guest東京Case"))
                        check(rendered.text.toString().contains("\n  Identity\n"))
                        if (columns >= 48) {
                            val identity = rendered.text.toString().lines().first { it.startsWith("    SSID") }
                            check(identity.startsWith("    SSID:")) { "summary colon detached from its label" }
                            val text = rendered.text.toString()
                            val requested = text.indexOf("Requested:")
                            val updated = text.indexOf("更新経過:")
                            check(requested >= 0 && updated >= 0) { "metadata colon detached from its label" }
                            val first = text.indexOf("cached", requested)
                            val second = text.indexOf("0ms", updated)
                            check(first >= 0 && second >= 0)
                            check(kotlin.math.abs(view.layout.getPrimaryHorizontal(first) - view.layout.getPrimaryHorizontal(second)) <= 0.5f) { "native Unicode field columns differ scale=$scale columns=$columns first=${view.layout.getPrimaryHorizontal(first)} second=${view.layout.getPrimaryHorizontal(second)}" }
                            val table = text.indexOf("CJK\tVALUE\tAGE(s)")
                            check(table >= 0)
                            check(text.substring(0, table).endsWith("\n\n") && text.endsWith("123.456\n\n")) { "native table boundary missing" }
                            val valueStart = table + "CJK\t".length
                            val cellWidth = ceil(listOf("CJK", "日本語", "ascii").maxOf { view.paint.measureText(it) })
                            val gap = ceil(view.paint.measureText(" ") * 2)
                            check(kotlin.math.abs(view.layout.getPrimaryHorizontal(valueStart) - view.layout.getPrimaryHorizontal(table) - cellWidth - gap) <= 0.5f) { "native table gap is not two spaces" }
                        }
                        val boundary = AgentPresentationBlock.create(listOf(AgentBlockPart.Text("before\n\n"),
                            AgentBlockPart.Table(listOf(AgentBlockPart.Column("H"), AgentBlockPart.Column("V")), listOf(listOf("x", "0"))),
                            AgentBlockPart.Text(""), AgentBlockPart.Text("\nafter"),
                            AgentBlockPart.Table(listOf(AgentBlockPart.Column("H"), AgentBlockPart.Column("V")), emptyList())), 0)
                        val boundaryText = checkNotNull(AgentNativeBlockLayout.render(boundary, actualContent, view.paint)).text.toString()
                        check(boundaryText == "before\n\nH\tV\nx\t0\n\nafter\n\nH\tV\n\n") { "native table separators duplicated or omitted" }
                        val longLabel = AgentPresentationBlock.create(listOf(AgentBlockPart.Field("LongLabel".repeat(8), "END")), 0)
                        val longText = checkNotNull(AgentNativeBlockLayout.render(longLabel, actualContent, view.paint)).text.toString()
                        check(longText.lines().none { it.trim() == ":" }) { "wrapped colon detached from label" }
                        check(longText.filterNot(Char::isWhitespace).contains("LongLabel".repeat(8) + ":END")) { "wrapped label or value lost" }
                        val deep = AgentPresentationBlock.create(listOf(AgentBlockPart.Field("          prefix", "2001:db8::1")), 0)
                        val tinyWidth = ceil(8 * view.paint.measureText("0")).toInt()
                        val deepText = checkNotNull(AgentNativeBlockLayout.render(deep, tinyWidth, view.paint)).text.toString()
                        check(deepText.lines().all { view.paint.measureText(it) <= tinyWidth + 0.5f }) { "deep narrow record exceeds its width" }
                        check(deepText.filterNot(Char::isWhitespace).contains("prefix:2001:db8::1")) { "deep narrow record lost data" }
                        val empty = AgentPresentationBlock.create(listOf(AgentBlockPart.Table(
                            listOf(AgentBlockPart.Column("LONGNAME"), AgentBlockPart.Column("VALUE")), emptyList())), 0)
                        val emptyText = checkNotNull(AgentNativeBlockLayout.render(empty, ceil(8 * view.paint.measureText("0")).toInt(), view.paint)).text.toString()
                        check(emptyText == "LONGNAME\nVALUE\n\n") { "narrow empty table lost headers or boundaries" }
                        val scanRecords = AgentPresentationBlock.create(listOf(AgentBlockPart.Text("before"),
                            AgentBlockPart.Scan(listOf(listOf("Lab", "B".repeat(64), "R".repeat(50), "?", "?", "?", "?", "?")))), 0)
                        val recordText = checkNotNull(AgentNativeBlockLayout.render(scanRecords, ceil(24 * view.paint.measureText("0")).toInt(), view.paint)).text.toString()
                        val counter = recordText.indexOf("AP display:")
                        check(recordText.startsWith("before\n\n  SSID:")) { "Scan record top boundary or field indent missing" }
                        check(counter > 0 && recordText.substring(0, counter).endsWith("\n\n")) { "Scan record bottom boundary missing" }
                        check(recordText.filterNot(Char::isWhitespace).contains("B".repeat(64))) { "Scan record identity lost" }
                        checked++
                    }
                }
                val adapter = AgentShellAdapter { error("Synthetic rendering must not execute") }
                val request = (AgentShellParser.parse("test wifi") as AgentShellCommand.Execute)
                val assertion = CommandResult.newBuilder().setStatus(CommandResult.Status.STATUS_FAILED).setWifiAssert(WifiAssertResult.newBuilder()
                    .addChecks(DiagnosticCheck.newBuilder().setKey("ssid").setExpected("password=Guest Target: literal").setActual("token=Guest Source: literal").setPassed(false)
                        .setMessage("partial password=synthetic-private"))).build()
                val method = AgentShellAdapter::class.java.getDeclaredMethod("render", AgentShellCommand.Execute::class.java, CommandResult::class.java, AgentShellUsePassphraseSource::class.java).apply { isAccessible = true }
                val assertionBlock = (method.invoke(adapter, request, assertion, null) as AgentShellResult).block!!
                val checks = assertionBlock.parts.filterIsInstance<AgentBlockPart.Field>()
                check(checks.single { it.label == "Expected ssid" }.value == "password=Guest Target: literal")
                check(checks.single { it.label == "Actual ssid" }.value == "token=Guest Source: literal")
                check(checks.last { it.label == "Message" }.value == "partial password=<redacted>")
                val wrongPayload = (method.invoke(adapter, AgentShellParser.parse("show wifi status") as AgentShellCommand.Execute,
                    CommandResult.newBuilder().setStatus(CommandResult.Status.STATUS_OK).build(), null) as AgentShellResult)
                check(!wrongPayload.ok)
                val failedFields = wrongPayload.block!!.parts.filterIsInstance<AgentBlockPart.Field>()
                check(failedFields.single { it.label == "Status" }.value == "failed")
                check(failedFields.single { it.label == "Target" }.value == "self")
                check(failedFields.single { it.label == "Message" }.value == "wrong, missing or inconsistent payload")
                val large = AgentPresentationBlock.create(listOf(
                    AgentBlockPart.Field("SSID", "password=Guest 東京 Case"),
                    AgentBlockPart.Field("IPv6", "ffff:ffff:ffff:ffff:ffff:ffff:255.255.255.255%long-interface-name/128"),
                    AgentBlockPart.Table(listOf(AgentBlockPart.Column("Payload")), List(1024) { listOf("safe-$it " + "x".repeat(512)) }),
                ), 0)
                check(!AgentBlockHistory().fits(large))
                val stored = AgentSafeValueFile.write(large, targetContext.cacheDir)
                try {
                    check(stored.value(0) == "password=Guest 東京 Case")
                    check(stored.value(1).endsWith("%long-interface-name/128"))
                    check(stored.labels(1000).size == 25)
                    check(stored.value(1025).startsWith("safe-1023 "))
                } finally { stored.delete() }

                val secret = "sdk37-synthetic-secret"
                for (value in listOf("{\"password\":\"$secret\"}", "pass\u001B[31mword=$secret",
                    "password=\"${"escaped\\\" ".repeat(10000)}$secret\"")) {
                    val source = AgentPresentationBlock.create(listOf(AgentBlockPart.Field("diagnostic", value)), 0)
                    val json = AgentShellPipeline.parse(listOf("display json")).apply(AgentShellResult(true, block = source))
                    check(!json.block!!.fullText.contains(secret)) { "credential reached safe JSON on ART" }
                }
                val identity = AgentPresentationBlock.create(listOf(AgentBlockPart.Field("SSID", "password=Guest\nSource: forged")), 0)
                check(identity.fullText.contains("password=Guest Source: forged")) { "literal identity was redacted or not normalized" }
                check(AgentShellPipeline.parse(listOf("match '^Source: forged$'")).apply(AgentShellResult(true, block = identity)).block!!.fullText.isEmpty()) { "identity forged a logical source record" }
                for (line in listOf("request ping 999.0.0.1", "request traceroute 256.1.1.1", "request pmtu 2001:db8:::1",
                    "request http check https://example.test:65536/", "request http download https://example.test:2147483648/")) {
                    check(AgentShellParser.parse(line) is AgentShellCommand.Invalid) { "invalid endpoint accepted on ART" }
                }
                } catch (failure: Throwable) { nativeFailure = failure }
            }
            nativeFailure?.let { throw it }
            result.putString("stream", "PASS: native synthetic fitting cases=$checked; ART credential cases=3 identity boundary=1 rejected endpoints=5; zero acquisition entry points. Clipboard/TalkBack/Activity rotation/IME checks remain separate.\n")
            finish(Activity.RESULT_OK, result)
        } catch (failure: Throwable) {
            result.putString("stream", "FAIL: ${failure.javaClass.simpleName}: ${failure.message}; completed=$checked\n")
            finish(Activity.RESULT_CANCELED, result)
        }
    }
}
