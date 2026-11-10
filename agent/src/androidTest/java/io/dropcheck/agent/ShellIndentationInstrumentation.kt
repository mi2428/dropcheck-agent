package io.dropcheck.agent

import android.app.Activity
import android.app.Instrumentation
import android.content.Intent
import android.os.Bundle
import android.os.SystemClock
import android.view.inputmethod.EditorInfo
import android.widget.EditText
import android.widget.LinearLayout
import android.widget.TextView
import java.io.File
import java.util.IdentityHashMap
import kotlin.math.roundToInt

/** Captures actual read-only Shell results, including native tab stops and wrapping. */
class ShellIndentationInstrumentation : Instrumentation() {
    private var phase = "before"
    private var verify = false

    override fun onCreate(arguments: Bundle?) {
        super.onCreate(arguments)
        phase = arguments?.getString("phase") ?: "before"
        require(phase in listOf("before", "after"))
        verify = phase == "after"
        start()
    }

    override fun onStart() {
        val report = Bundle()
        var activity: MainActivity? = null
        try {
            activity = startActivitySync(Intent(targetContext, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)) as MainActivity
            onMain { activity.setShowWhenLocked(true); activity.setTurnScreenOn(true) }
            waitForIdleSync()
            val output = StringBuilder()
            for (command in listOf("show wifi scan", "show wifi scan brief", "show network default", "show wifi status full")) {
                val block = submit(activity, command)
                waitForIdleSync()
                val deadline = SystemClock.elapsedRealtime() + 3_000
                var measured = false
                while (!measured && SystemClock.elapsedRealtime() < deadline) {
                    onMain {
                        val views = field<IdentityHashMap<AgentPresentationBlock, LinearLayout>>(activity, "shellBlockViews")
                        val view = views[block]?.getChildAt(0) as? TextView
                        measured = view != null && view.width > 0 && view.layout != null && view.text.isNotEmpty()
                    }
                    if (!measured) Thread.sleep(25)
                }
                check(measured) { "result has no measured native layout" }
                var captured = ""
                onMain {
                    val views = field<IdentityHashMap<AgentPresentationBlock, LinearLayout>>(activity, "shellBlockViews")
                    val view = checkNotNull(views[block]).getChildAt(0) as TextView
                    check(view.width > 0 && view.layout != null && view.text.isNotEmpty()) { "result has no measured native layout" }
                    captured = "COMMAND $command\nWIDTH_PX ${view.width - view.paddingLeft - view.paddingRight}\n" + ascii(view)
                    if (verify) verifyIndentation(view.text.toString())
                }
                output.append(captured).append('\n')
            }
            File(targetContext.filesDir, "indent-$phase.txt").writeText(output.toString())
            report.putString("stream", "PASS: native live Shell capture phase=$phase commands=4; mutation=0 fresh_scan=0 external_probe=0\n")
            finish(Activity.RESULT_OK, report)
        } catch (failure: Throwable) {
            report.putString("stream", "FAIL: native capture phase=$phase ${failure.javaClass.simpleName}: ${failure.message}\n")
            finish(Activity.RESULT_CANCELED, report)
        } finally {
            activity?.let { runOnMainSync { it.finish() } }
        }
    }

    private fun submit(activity: MainActivity, command: String): AgentPresentationBlock {
        var previous: AgentPresentationBlock? = null
        onMain {
            previous = field<AgentBlockHistory>(activity, "shellTranscript").blocks.lastOrNull()
            val input = field<EditText>(activity, "shellInput")
            input.requestFocus()
            input.setText("")
            val connection = checkNotNull(input.onCreateInputConnection(EditorInfo()))
            check(connection.commitText(command, 1) && connection.performEditorAction(EditorInfo.IME_ACTION_GO))
        }
        val deadline = SystemClock.elapsedRealtime() + 15_000
        while (SystemClock.elapsedRealtime() < deadline) {
            var result: AgentPresentationBlock? = null
            runOnMainSync {
                val last = field<AgentBlockHistory>(activity, "shellTranscript").blocks.lastOrNull()
                if (!field<ShellActionOwner>(activity, "shellOwner").busy && last != null && last !== previous && last.promptLength == 0) result = last
            }
            if (result != null) return result!!
            Thread.sleep(25)
        }
        error("read-only Shell command timed out: $command")
    }

    private fun ascii(view: TextView): String = buildString {
        val layout = checkNotNull(view.layout)
        val text = view.text
        val space = view.paint.measureText(" ")
        for (line in 0 until layout.lineCount) {
            var column = 0
            for (offset in layout.getLineStart(line) until layout.getLineEnd(line)) {
                val ch = text[offset]
                if (ch == '\n' || ch == '\t') continue
                val position = (layout.getPrimaryHorizontal(offset) / space).roundToInt()
                while (column < position) { append(' '); column++ }
                append(if (ch.code in 32..126) ch else '?')
                column++
            }
            append('\n')
        }
    }

    private fun verifyIndentation(text: String) {
        val lines = text.lines()
        var apDepth: Int? = null
        var networkDepth: Int? = null
        var apFields = 0
        var apSectionDepth: Int? = null
        for (line in lines) {
            val trimmed = line.trimStart()
            val depth = line.length - trimmed.length
            if (Regex("^\\[[*+ ]+] AP \\d+").containsMatchIn(trimmed)) { apDepth = depth; apSectionDepth = null }
            else if (trimmed.isNotEmpty() && apDepth != null && depth <= apDepth) { apDepth = null; apSectionDepth = null }
            if (apDepth != null && (trimmed.startsWith("SSID:") || trimmed.startsWith("BSSID:") || trimmed in listOf("AP Capabilities", "HE/EHT Details", "Information Elements", "Fields"))) {
                check(depth == apDepth + 2) { "AP sibling depth=$depth parent=$apDepth" }
                apFields++
            }
            if (apDepth != null && trimmed in listOf("AP Capabilities", "HE/EHT Details", "Information Elements", "Fields")) apSectionDepth = depth
            else if (apSectionDepth != null && trimmed.contains(':') && !trimmed.startsWith("http")) {
                check(depth == apSectionDepth + 2) { "AP section child depth=$depth parent=$apSectionDepth" }
                apSectionDepth = null
            }
            if (trimmed.startsWith("Network 1")) networkDepth = depth
            if (trimmed.startsWith("Active/default at collection:") && networkDepth != null) check(depth == networkDepth + 2)
        }
        if (lines.any { it.contains(" AP 1") }) check(apFields > 0) { "AP checks were not exercised" }
        // The grouped SSID label and BSSID table must start at the same column.
        for (index in lines.indices) {
            val line = lines[index]
            if (!line.trimStart().startsWith("SSID:")) continue
            val next = lines.drop(index + 1).firstOrNull { it.isNotBlank() } ?: continue
            if (next.trimStart().startsWith("BSSID\t")) {
                check(line.takeWhile { it == ' ' }.length == next.takeWhile { it == ' ' }.length) { "SSID/table left edges differ" }
            }
        }
    }

    private fun onMain(action: () -> Unit) {
        var result: Result<Unit>? = null
        runOnMainSync { result = runCatching(action) }
        result!!.getOrThrow()
    }

    @Suppress("UNCHECKED_CAST")
    private fun <T> field(activity: MainActivity, name: String): T = MainActivity::class.java.getDeclaredField(name).apply { isAccessible = true }.get(activity) as T
}
