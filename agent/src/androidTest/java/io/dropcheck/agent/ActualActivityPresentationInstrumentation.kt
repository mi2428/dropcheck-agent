package io.dropcheck.agent

import android.app.Activity
import android.app.Instrumentation
import android.content.Intent
import android.os.Bundle
import android.text.Spannable
import android.text.Selection
import android.util.TypedValue
import android.view.View
import android.view.WindowInsets
import android.view.accessibility.AccessibilityNodeInfo
import android.widget.EditText
import android.widget.LinearLayout
import android.widget.ScrollView
import android.widget.TextView
import kotlin.math.abs
import kotlin.math.ceil

/** Real MainActivity views, synthetic safe results only; never submits a command. */
class ActualActivityPresentationInstrumentation : Instrumentation() {
    private var showIme = false

    override fun onCreate(arguments: Bundle?) {
        super.onCreate(arguments)
        showIme = arguments?.getString("ime") == "true"
        start()
    }

    override fun onStart() {
        val result = Bundle()
        var activity: Activity? = null
        var cases = 0
        var nodeCases = 0
        val widths = mutableListOf<String>()
        var imeVisible = false
        val clipboard = "NOT MEASURED (original clip cannot be proven safely restorable)"
        try {
            activity = startActivitySync(Intent(targetContext, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_MULTIPLE_TASK))
            runOnMainSync { activity.setShowWhenLocked(true) }
            waitForIdleSync()
            val main = activity as MainActivity
            for (attempt in 0 until 40) {
                if (main.hasWindowFocus()) break
                Thread.sleep(50)
                waitForIdleSync()
            }
            if (!main.hasWindowFocus()) {
                val window = checkNotNull(uiAutomation.rootInActiveWindow) { "Activity window not active; attached=${main.window.decorView.isAttachedToWindow}" }
                check(window.findAccessibilityNodeInfosByText(main.getString(R.string.background_location_title)).isNotEmpty()) { "Activity obscured by ${window.packageName}/${window.className}" }
                check(window.findAccessibilityNodeInfosByText(main.getString(R.string.background_location_later)).any { it.performAction(AccessibilityNodeInfo.ACTION_CLICK) }) { "Cannot dismiss optional background-location dialog" }
                waitForIdleSync()
            }
            val scroll = main.field<ScrollView>("shellScroll")
            val content = main.field<LinearLayout>("shellContent")
            val input = checkNotNull(main.field<EditText?>("shellInput"))
            val initialParams = scroll.layoutParams
            val block = AgentPresentationBlock.create(listOf(
                AgentBlockPart.Text("Wi-Fi scan FAILED 9000ms"),
                AgentBlockPart.Text("Source: android  Target: self\nReason: fresh scan timed out"),
                AgentBlockPart.Scan(listOf(
                    listOf("東京Ｇ e\u0301 👩\u200d🔬 Lab Case", "02:00:00:11:22:33", "-48", "6G", "37", "320", "be", "psk+sae+future_Mode"),
                    listOf("東京Ｇ e\u0301 👩\u200d🔬 Lab Case", "06:00:00:11:22:33", "-55", "5G", "36", "80", "ax", "psk+sae"),
                )),
                AgentBlockPart.Field("IPv6", "ffff:ffff:ffff:ffff:ffff:ffff:255.255.255.255%long-interface-name/128"),
            ), 0)
            check(block.fullText.contains("FAILED") && block.fullText.contains("psk+sae+future_Mode"))
            check(block.fullText.contains("02:00:00:11:22:33") && block.fullText.contains("06:00:00:11:22:33"))
            check(block.fullText.none { it == '\u001b' || it == '\u200b' || it == '\t' })
            try {
                runOnMainSync {
                    main.method("appendShellBlock", AgentPresentationBlock::class.java, Boolean::class.javaPrimitiveType!!).invoke(main, block, false)
                    input.setText("draft")
                    input.setSelection(2)
                    input.requestFocus()
                }
                waitForIdleSync()
                runOnMainSync { if (showIme) main.window.insetsController?.show(WindowInsets.Type.ime()) else main.window.insetsController?.hide(WindowInsets.Type.ime()) }
                Thread.sleep(250)
                waitForIdleSync()
                for (base in listOf(10f, 12f)) {
                    runOnMainSync { main.fieldSet("shellOutputSizeSp", base); main.method("appendShellBlock", AgentPresentationBlock::class.java, Boolean::class.javaPrimitiveType!!).invoke(main, block, false) }
                    waitForIdleSync()
                    for (columns in listOf(24, 32, 40, 48, 60, 80, 96, 120)) {
                        val text = (content.getChildAt(content.childCount - 2) as LinearLayout).getChildAt(0) as TextView
                        val requestedContent = ceil(columns * text.paint.measureText("0")).toInt()
                        val requestedWidth = requestedContent + content.paddingLeft + content.paddingRight
                        if (requestedWidth > main.window.decorView.width) continue
                        runOnMainSync { scroll.layoutParams = scroll.layoutParams.apply { width = requestedWidth } }
                        for (frame in 0 until 30) {
                            if (scroll.width == requestedWidth && content.width == requestedWidth) break
                            Thread.sleep(50)
                            waitForIdleSync()
                        }
                        val current = (content.getChildAt(content.childCount - 2) as LinearLayout).getChildAt(0) as TextView
                        val actualContent = content.width - content.paddingLeft - content.paddingRight
                        check(actualContent == requestedContent) { "content width mismatch base=$base columns=$columns requested=$requestedContent actual=$actualContent scroll=${scroll.width} params=${scroll.layoutParams.width} attached=${scroll.isAttachedToWindow} focus=${main.hasWindowFocus()} visible=${scroll.visibility == View.VISIBLE} parent=${scroll.parent?.javaClass?.simpleName}" }
                        check(abs(current.textSize - TypedValue.applyDimension(TypedValue.COMPLEX_UNIT_SP, base, current.resources.displayMetrics)) < 0.1f) { "output size changed base=$base actual=${current.textSize}" }
                        check(abs(input.textSize - TypedValue.applyDimension(TypedValue.COMPLEX_UNIT_SP, 10f, input.resources.displayMetrics)) < 0.1f) { "input was resized" }
                        check(input.hasFocus() && input.selectionStart == 2 && input.text.toString() == "draft") { "input focus/selection lost" }
                        val layout = checkNotNull(current.layout)
                        for (line in 0 until layout.lineCount) check(layout.getLineWidth(line) <= actualContent + 0.5f) { "Activity native line overflow base=$base columns=$columns line=$line" }
                        check(current.text.contains("FAILED") && current.text.contains("Reason:"))
                        val container = content.getChildAt(content.childCount - 2) as LinearLayout
                        check(container.childCount == 2 && (container.getChildAt(1) as TextView).text == "Copy full result")
                        val window = uiAutomation.rootInActiveWindow
                        if (window != null) {
                            val nodes = mutableListOf<String>()
                            fun collect(node: AccessibilityNodeInfo) {
                                node.text?.let { nodes += it.toString() }
                                node.contentDescription?.let { nodes += it.toString() }
                                for (child in 0 until node.childCount) node.getChild(child)?.let(::collect)
                            }
                            collect(window)
                            check(nodes.indexOfFirst { it.contains("Wi-Fi scan FAILED") } in 0 until nodes.indexOfLast { it == "Copy full safe result" }) { "AccessibilityNode order" }
                            nodeCases++
                        }
                        cases++
                        widths += "${base.toInt()}sp/$columns=${actualContent}px"
                    }
                }
                // Reflow while the reader is above the newest result: retain an older block as the scroll anchor.
                runOnMainSync {
                    scroll.layoutParams = initialParams
                    scroll.scrollTo(0, content.getChildAt(content.childCount - 3).top)
                    val old = content.getChildAt(content.childCount - 3) as LinearLayout
                    val value = (old.getChildAt(0) as TextView).text as Spannable
                    Selection.setSelection(value, 1, 5)
                }
                waitForIdleSync()
                val anchor = content.getChildAt(content.childCount - 3)
                val offset = scroll.scrollY - anchor.top
                runOnMainSync { main.method("renderShell").invoke(main) }
                waitForIdleSync()
                val selected = (anchor as LinearLayout).getChildAt(0) as TextView
                check(abs(scroll.scrollY - anchor.top - offset) <= 2) { "scroll anchor lost" }
                check(selected.selectionStart == 1 && selected.selectionEnd == 5) { "output selection lost" }
                imeVisible = main.window.decorView.rootWindowInsets?.isVisible(WindowInsets.Type.ime()) == true
                check(imeVisible == showIme) { "IME requested=$showIme visible=$imeVisible" }
            } finally {
                runOnMainSync { main.window.insetsController?.hide(WindowInsets.Type.ime()); scroll.layoutParams = initialParams }
            }
            check(cases > 0) { "No Activity width fits the device" }
            result.putString("stream", "PASS: attached MainActivity native layout cases=$cases widths=$widths; AccessibilityNode cases=$nodeCases (remaining NOT MEASURED); fontScale=${main.resources.configuration.fontScale}; orientation=${main.resources.configuration.orientation}; IME requested=$showIme visible=$imeVisible; anchor/selection/input/copy source checked; clipboard=$clipboard; probe invocations=0 (synthetic, no command dispatch). TalkBack service and human readability NOT measured.\n")
            finish(Activity.RESULT_OK, result)
        } catch (failure: Throwable) {
            result.putString("stream", "FAIL: ${failure.javaClass.simpleName}: ${failure.message}; completed=$cases; clipboard=$clipboard\n")
            finish(Activity.RESULT_CANCELED, result)
        } finally {
            activity?.let { runOnMainSync { it.setShowWhenLocked(false); it.finish() } }
        }
    }

    @Suppress("UNCHECKED_CAST")
    private fun <T> Activity.field(name: String): T = javaClass.getDeclaredField(name).apply { isAccessible = true }.get(this) as T
    private fun Activity.fieldSet(name: String, value: Any) { javaClass.getDeclaredField(name).apply { isAccessible = true }.set(this, value) }
    private fun Activity.method(name: String, vararg parameters: Class<*>): java.lang.reflect.Method = javaClass.getDeclaredMethod(name, *parameters).apply { isAccessible = true }
}
