package io.dropcheck.agent

import android.app.Activity
import android.app.Instrumentation
import android.content.Intent
import android.os.Bundle
import android.view.KeyEvent
import android.view.Gravity
import android.view.View
import android.view.ViewGroup
import android.view.inputmethod.BaseInputConnection
import android.view.inputmethod.EditorInfo
import android.widget.Button
import android.widget.EditText
import android.widget.FrameLayout
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

/** Synthetic editing only. Never submits a device command or changes connectivity. */
class ShellInputInstrumentation : Instrumentation() {
    override fun onCreate(arguments: Bundle?) { super.onCreate(arguments); start() }

    override fun onStart() {
        var activity: MainActivity? = null
        val report = Bundle()
        val release = CountDownLatch(1)
        try {
            activity = startActivitySync(Intent(targetContext, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)) as MainActivity
            val main = activity
            waitForIdleSync()
            val inputField = MainActivity::class.java.getDeclaredField("shellInput").apply { isAccessible = true }
            runOnMainSync {
                val input = checkNotNull(inputField.get(main) as EditText?)
                input.requestFocus()
                input.setText("show "); input.setSelection(5)
                val connection = checkNotNull(input.onCreateInputConnection(EditorInfo()))
                check(connection.commitText("?", 1))
                check(input.text.toString() == "show " && input.selectionStart == 5) { "help changed input" }

                input.setText("request wifi connect 'Lab"); input.setSelection(input.text.length)
                connection.commitText("?", 1)
                check(input.text.toString() == "request wifi connect 'Lab?") { "quoted question mark was intercepted" }

                input.setText("show "); input.setSelection(5)
                connection.setComposingText("?", 1)
                check(input.text.toString() == "show ?") { "IME composition was intercepted" }
                connection.finishComposingText()

                input.setText("show  status"); input.setSelection(5)
                connection.commitText("?", 1)
                check(input.text.toString() == "show  status" && input.selectionStart == 5) { "cursor help destroyed the suffix" }

                input.setText("sho"); input.setSelection(3)
                input.dispatchKeyEvent(KeyEvent(KeyEvent.ACTION_DOWN, KeyEvent.KEYCODE_TAB))
                check(input.text.toString() == "show") { "hardware completion failed" }

                for (line in listOf("show wifi status", "show wix status")) {
                    input.setText(line); input.setSelection("show wi".length)
                    input.dispatchKeyEvent(KeyEvent(KeyEvent.ACTION_DOWN, KeyEvent.KEYCODE_TAB))
                    check(input.text.toString() == "show wifi status" && input.selectionStart == "show wifi".length) { "mid-token completion corrupted input" }
                }

                val session = MainActivity::class.java.getDeclaredField("shellSession").apply { isAccessible = true }.get(main) as AgentShellSession
                session.remember("show version")
                session.remember("show wifi scan")
                session.remember("show wifi status")
                val secret = "set cli default-passphrase input-test-only-secret"
                session.remember(secret)
                for ((previous, next, modifiers) in listOf(
                    Triple(KeyEvent.KEYCODE_DPAD_UP, KeyEvent.KEYCODE_DPAD_DOWN, 0),
                    Triple(KeyEvent.KEYCODE_P, KeyEvent.KEYCODE_N, KeyEvent.META_CTRL_ON),
                    Triple(KeyEvent.KEYCODE_P, KeyEvent.KEYCODE_N, KeyEvent.META_CTRL_LEFT_ON),
                    Triple(KeyEvent.KEYCODE_P, KeyEvent.KEYCODE_N, KeyEvent.META_CTRL_RIGHT_ON),
                )) {
                    fun press(code: Int) {
                        for (action in listOf(KeyEvent.ACTION_DOWN, KeyEvent.ACTION_UP)) {
                            input.dispatchKeyEvent(KeyEvent(0L, 0L, action, code, 0, modifiers))
                        }
                    }
                    input.setText("show cli"); input.setSelection(input.text.length)
                    press(previous)
                    check(input.text.toString() == secret && input.selectionStart == input.text.length) { "previous command failed" }
                    press(previous)
                    check(input.text.toString() == "show wifi status") { "Wi-Fi status missing from history" }
                    press(previous)
                    check(input.text.toString() == "show wifi scan") { "Wi-Fi scan missing from history" }
                    press(previous); press(previous)
                    check(input.text.toString() == "show version") { "oldest command boundary failed" }
                    press(next)
                    check(input.text.toString() == "show wifi scan") { "next command failed" }
                    press(next); press(next)
                    check(input.text.toString() == secret) { "credential command missing from history" }
                    press(next); press(next)
                    check(input.text.toString() == "show cli" && input.selectionStart == input.text.length) { "safe draft was not restored" }
                    input.setText(secret); input.setSelection(input.text.length)
                    press(previous); press(next)
                    check(input.text.toString() == secret) { "draft was not restored" }
                }
                for (code in listOf(KeyEvent.KEYCODE_P, KeyEvent.KEYCODE_N)) {
                    input.setText("")
                    input.dispatchKeyEvent(KeyEvent(KeyEvent.ACTION_DOWN, code))
                    input.dispatchKeyEvent(KeyEvent(KeyEvent.ACTION_UP, code))
                    check(input.text.toString() == if (code == KeyEvent.KEYCODE_P) "p" else "n") { "plain letter was intercepted as history" }
                }

                val bar = field<View>(main, "shellKeyBar")
                val panel = field<View>(main, "shellInputPanel")
                check(panel.parent === field<View>(main, "root") && bar.parent === panel && bar.isShown) { "extra keys are not docked outside the transcript" }
                check((panel.layoutParams as FrameLayout.LayoutParams).gravity and Gravity.BOTTOM == Gravity.BOTTOM)
                for (label in listOf("Esc", "Tab", "Ctrl-C", "?", "↑", "\"", "/", "-", "|", "←", "↓", "→")) {
                    val button = key(main, label)
                    check(!button.contentDescription.isNullOrEmpty()) { "extra key has no accessible name" }
                    check(button.minimumWidth >= (48 * main.resources.displayMetrics.density).toInt())
                    check(button.minimumHeight >= (48 * main.resources.displayMetrics.density).toInt())
                }
                input.setText("sho"); input.setSelection(3)
                key(main, "Tab").performClick()
                check(input.text.toString() == "show" && input.hasFocus()) { "touch completion lost input/focus" }
                key(main, "↑").performClick()
                check(input.text.toString() == secret) { "touch history did not recall the last command" }
                key(main, "↓").performClick()
                check(input.text.toString() == "show") { "touch history did not restore the draft" }
                input.setText("show "); input.setSelection(5)
                key(main, "?").performClick()
                check(input.text.toString() == "show " && input.selectionStart == 5) { "touch help modified input" }

                input.setText("A\uD83D\uDE00B"); input.setSelection(3)
                key(main, "←").performClick()
                check(input.selectionStart == 1) { "left key split a Unicode character" }
                key(main, "→").performClick()
                check(input.selectionStart == 3) { "right key split a Unicode character" }
                for (symbol in listOf("/", "-", "|", "\"")) {
                    input.setText("日本語 tail"); input.setSelection(3, 0)
                    key(main, symbol).performClick()
                    check(input.text.toString() == "$symbol tail" && input.selectionStart == 1 && input.hasFocus()) { "symbol key did not replace the selection" }
                }
                input.setText("show "); input.setSelection(5)
                connection.setComposingText("日本", 1)
                key(main, "/").performClick()
                check(input.text.toString() == "show 日本/" && BaseInputConnection.getComposingSpanStart(input.text) == -1) { "symbol key corrupted IME composition" }
                connection.commitText("語", 1)
                check(input.text.toString() == "show 日本/語") { "IME could not continue after an extra key" }

                val beforeCancel = field<AgentBlockHistory>(main, "shellTranscript").blocks.size
                input.setText(secret); input.setSelection(input.text.length)
                key(main, "Ctrl-C").performClick()
                check(input.text.isEmpty() && input.hasFocus()) { "Ctrl-C did not clear idle input" }
                check(field<AgentBlockHistory>(main, "shellTranscript").blocks.size == beforeCancel) { "cleared input was echoed to the transcript" }
                for (modifiers in listOf(KeyEvent.META_CTRL_LEFT_ON, KeyEvent.META_CTRL_RIGHT_ON)) {
                    input.setText("unfinished")
                    input.dispatchKeyEvent(KeyEvent(0L, 0L, KeyEvent.ACTION_DOWN, KeyEvent.KEYCODE_C, 0, modifiers))
                    check(input.text.isEmpty()) { "hardware Ctrl-C differs from the extra key" }
                }
                key(main, "Esc").performClick()
                check(input.hasFocus()) { "Esc lost physical keyboard input focus" }
                key(main, "-").performClick()
                check(input.text.toString() == "-" && input.hasFocus()) { "editing did not resume after Esc" }
            }

            val started = CountDownLatch(1)
            val interrupted = CountDownLatch(1)
            val owner = field<ShellActionOwner>(main, "shellOwner")
            runOnMainSync {
                val action: () -> AgentShellResult = {
                    started.countDown()
                    try {
                        check(release.await(10, TimeUnit.SECONDS)) { "extra-key cancellation timed out" }
                        AgentShellResult(true)
                    } catch (failure: InterruptedException) {
                        interrupted.countDown()
                        throw failure
                    }
                }
                MainActivity::class.java.getDeclaredMethod("runShellLinesCommand", String::class.java, Function0::class.java)
                    .apply { isAccessible = true }.invoke(main, "synthetic.keys", action)
            }
            check(started.await(5, TimeUnit.SECONDS)) { "synthetic action did not start" }
            runOnMainSync {
                val input = inputField.get(main) as EditText
                input.setText("next draft"); input.setSelection(input.text.length)
                key(main, "↑").performClick()
                check(input.text.toString() == "next draft") { "busy history replaced the next draft" }
                key(main, "Ctrl-C").performClick()
                check(input.text.toString() == "next draft") { "busy cancellation discarded the next draft" }
            }
            check(interrupted.await(5, TimeUnit.SECONDS)) { "Ctrl-C did not interrupt the active action" }
            for (attempt in 0 until 100) {
                waitForIdleSync()
                if (!owner.busy) break
                Thread.sleep(20)
            }
            check(!owner.busy) { "canceled action remained busy" }
            waitForIdleSync()
            runOnMainSync {
                check(field<AgentBlockHistory>(main, "shellTranscript").blocks.last().fullText.contains("Command canceled"))
                check((inputField.get(main) as EditText).text.toString() == "next draft")
                check(key(main, "Tab").isShown) { "extra keys disappeared after action completion" }
                MainActivity::class.java.getDeclaredMethod("showViewer").apply { isAccessible = true }.invoke(main)
                check(!field<View>(main, "shellKeyBar").isShown) { "extra keys leaked into the Log Viewer" }
                MainActivity::class.java.getDeclaredMethod("showShell").apply { isAccessible = true }.invoke(main)
                check(field<View>(main, "shellKeyBar").isShown) { "extra keys were not restored with the Shell" }
            }
            report.putString("result", "PASS: IME editing/composition, hardware and docked completion/history, Unicode cursor movement, selected symbol insertion, accessible extra keys, Esc, idle Ctrl-C and synthetic in-flight cancellation with draft retention; zero device commands")
            finish(Activity.RESULT_OK, report)
        } catch (error: Throwable) {
            report.putString("error", error.javaClass.simpleName + ": " + error.message)
            finish(Activity.RESULT_CANCELED, report)
        } finally {
            release.countDown()
            activity?.let { main -> runOnMainSync { main.finish() } }
        }
    }

    private fun key(activity: MainActivity, label: String): Button {
        val rows = field<ViewGroup>(activity, "shellKeyBar").getChildAt(0) as ViewGroup
        return (0 until rows.childCount).flatMap { index ->
            val row = rows.getChildAt(index) as ViewGroup
            (0 until row.childCount).map { row.getChildAt(it) as Button }
        }.single { it.text.toString() == label }
    }

    @Suppress("UNCHECKED_CAST")
    private fun <T> field(activity: MainActivity, name: String): T = MainActivity::class.java.getDeclaredField(name).apply { isAccessible = true }.get(activity) as T
}
