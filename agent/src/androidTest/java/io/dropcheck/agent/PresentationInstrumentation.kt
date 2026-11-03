package io.dropcheck.agent

import android.app.Activity
import android.app.Instrumentation
import android.content.res.Configuration
import android.graphics.Typeface
import android.os.Bundle
import android.view.View
import android.view.ViewGroup
import android.widget.TextView
import kotlin.math.ceil

/** Native-only synthetic fitting check. No service, network, scan or probe invocation. */
class PresentationInstrumentation : Instrumentation() {
    override fun onCreate(arguments: Bundle?) { start() }

    override fun onStart() {
        val result = Bundle()
        var checked = 0
        try {
            runOnMainSync {
                val block = AgentPresentationBlock.create(listOf(
                    AgentBlockPart.Text("Wi-Fi scan FAILED 9000ms\nSource: android Target: self\nReason: fresh scan timed out\nData: cached (refresh failed)"),
                    AgentBlockPart.Scan(listOf(
                        listOf("東京Ｇ e\u0301 👩\u200d🔬 Lab Case", "02:00:00:11:22:33", "-48", "6G", "37", "320", "be", "psk+sae+future_Mode"),
                        listOf("東京Ｇ e\u0301 👩\u200d🔬 Lab Case", "06:00:00:11:22:33", "-55", "5G", "36", "80", "ax", "psk+sae"),
                    )),
                    AgentBlockPart.Field("IPv6", "ffff:ffff:ffff:ffff:ffff:ffff:255.255.255.255%long-interface-name/128"),
                    AgentBlockPart.Text("Connection\n  Identity"),
                    AgentBlockPart.Field("    SSID".padEnd(32), "password=Guest 東京 Case"),
                    AgentBlockPart.Field("    IPv6".padEnd(32), "2001:db8:ffff:ffff:ffff:ffff:ffff:ffff/64"),
                    AgentBlockPart.Field("    Servers".padEnd(32), "192.0.2.53\n2001:db8::53"),
                ), 0, 12f, 10f)
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
                        view.text = rendered.text
                        view.measure(View.MeasureSpec.makeMeasureSpec(content + 56, View.MeasureSpec.EXACTLY), View.MeasureSpec.makeMeasureSpec(0, View.MeasureSpec.UNSPECIFIED))
                        view.layout(0, 0, view.measuredWidth, view.measuredHeight)
                        for (line in 0 until view.layout.lineCount) check(view.layout.getLineWidth(line) <= actualContent + 0.5f) { "native line overflow scale=$scale columns=$columns line=$line" }
                        check(rendered.text.toString().contains("FAILED"))
                        check(block.fullText.contains("02:00:00:11:22:33") && block.fullText.contains("06:00:00:11:22:33"))
                        check(block.fullText.contains("psk+sae+future_Mode"))
                        check(rendered.text.toString().filterNot(Char::isWhitespace).contains("password=Guest東京Case"))
                        check(rendered.text.toString().contains("\n  Identity\n"))
                        if (columns >= 48) {
                            val identity = rendered.text.toString().lines().first { it.startsWith("    SSID") }
                            check(identity.indexOf(": ") == 32) { "summary value column moved" }
                        }
                        checked++
                    }
                }
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
                for (line in listOf("request ping 999.0.0.1", "request traceroute 256.1.1.1", "request path-mtu 2001:db8:::1",
                    "request http https://example.test:65536/", "request download https://example.test:2147483648/")) {
                    check(AgentShellParser.parse(line) is AgentShellCommand.Invalid) { "invalid endpoint accepted on ART" }
                }
            }
            result.putString("stream", "PASS: native synthetic fitting cases=$checked; ART credential cases=3 identity boundary=1 rejected endpoints=5; zero acquisition entry points. Clipboard/TalkBack/Activity rotation/IME checks remain separate.\n")
            finish(Activity.RESULT_OK, result)
        } catch (failure: Throwable) {
            result.putString("stream", "FAIL: ${failure.javaClass.simpleName}: ${failure.message}; completed=$checked\n")
            finish(Activity.RESULT_CANCELED, result)
        }
    }
}
