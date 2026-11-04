package io.dropcheck.agent

import android.app.Activity
import android.app.Instrumentation
import android.content.Intent
import android.os.Bundle
import android.view.KeyEvent
import android.view.inputmethod.EditorInfo
import android.widget.EditText

/** Synthetic editing only. Never submits a device command or changes connectivity. */
class ShellInputInstrumentation : Instrumentation() {
    override fun onCreate(arguments: Bundle?) { super.onCreate(arguments); start() }

    override fun onStart() {
        var activity: MainActivity? = null
        val report = Bundle()
        try {
            activity = startActivitySync(Intent(targetContext, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)) as MainActivity
            val main = activity
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
            }
            waitForIdleSync()
            report.putString("result", "PASS: no-Enter help, literal quotes, IME composition, cursor suffix, hardware Tab, arrows/Ctrl-P/Ctrl-N all-command history and drafts; zero device commands")
            finish(Activity.RESULT_OK, report)
        } catch (error: Throwable) {
            report.putString("error", error.javaClass.simpleName + ": " + error.message)
            finish(Activity.RESULT_CANCELED, report)
        } finally {
            activity?.let { main -> runOnMainSync { main.finish() } }
        }
    }
}
