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
            }
            waitForIdleSync()
            report.putString("result", "PASS: no-Enter help, literal quotes, IME composition, cursor suffix and hardware Tab; zero device commands")
            finish(Activity.RESULT_OK, report)
        } catch (error: Throwable) {
            report.putString("error", error.javaClass.simpleName + ": " + error.message)
            finish(Activity.RESULT_CANCELED, report)
        } finally {
            activity?.let { main -> runOnMainSync { main.finish() } }
        }
    }
}
