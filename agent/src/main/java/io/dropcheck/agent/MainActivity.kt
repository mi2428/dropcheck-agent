package io.dropcheck.agent

import android.annotation.SuppressLint
import android.app.Activity
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.graphics.Color
import android.graphics.Typeface
import android.os.Build
import android.os.Bundle
import android.text.SpannableString
import android.text.SpannableStringBuilder
import android.text.Spanned
import android.text.style.ForegroundColorSpan
import android.widget.ScrollView
import android.widget.TextView

/**
 * Minimal on-device terminal view for lab/debug sessions.
 *
 * It intentionally avoids app navigation or controls; controller interaction is
 * driven through adb/gRPC, while this screen exposes the local log tail.
 */
class MainActivity : Activity() {
    private companion object {
        const val STARTUP_TAIL_LINES = 800
        const val MAX_DISPLAY_CHARS = 600_000
        const val TRIMMED_DISPLAY_CHARS = 450_000
    }

    private val warnColor = Color.rgb(255, 214, 10)
    private val errorColor = Color.rgb(255, 82, 82)

    private lateinit var logView: TextView
    private lateinit var scroll: ScrollView

    private val receiver = object : BroadcastReceiver() {
        override fun onReceive(context: Context, intent: Intent) {
            val line = intent.getStringExtra(TerminalLog.EXTRA_LINE) ?: return
            append(line)
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        TerminalLog.info(this, "activity onCreate")

        logView = TextView(this).apply {
            setTextColor(Color.WHITE)
            setBackgroundColor(Color.BLACK)
            typeface = Typeface.MONOSPACE
            textSize = 8f
            includeFontPadding = true
            setLineSpacing(0f, 1.05f)
            setPadding(18, 18, 18, 18)
            text = SpannableStringBuilder().apply {
                appendColored("dropcheck agent\n")
                appendColored("controller commands arrive over adb reverse + gRPC bidi\n")
                appendColored("\n")
                val tail = TerminalLog.tail(this@MainActivity, STARTUP_TAIL_LINES)
                if (tail.isNotBlank()) {
                    appendColored("-- terminal.log tail --\n")
                    tail.lineSequence().forEach { appendColored(it + "\n") }
                }
            }
        }
        scroll = ScrollView(this).apply {
            setBackgroundColor(Color.BLACK)
            isFillViewport = true
            addView(logView)
        }
        setContentView(scroll)
        scroll.post { scroll.fullScroll(ScrollView.FOCUS_DOWN) }
    }

    @SuppressLint("UnspecifiedRegisterReceiverFlag")
    override fun onStart() {
        super.onStart()
        val filter = IntentFilter(TerminalLog.ACTION_LINE)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            registerReceiver(receiver, filter, Context.RECEIVER_NOT_EXPORTED)
        } else {
            @Suppress("DEPRECATION")
            registerReceiver(receiver, filter)
        }
    }

    override fun onStop() {
        unregisterReceiver(receiver)
        super.onStop()
    }

    /** Appends one broadcast terminal line and trims the view to a bounded size. */
    private fun append(line: String) {
        logView.append(colored(line))
        if (!line.endsWith("\n")) logView.append("\n")
        if (logView.text.length > MAX_DISPLAY_CHARS) {
            val current = logView.text
            logView.text = current.subSequence(current.length - TRIMMED_DISPLAY_CHARS, current.length)
        }
        scroll.post { scroll.fullScroll(ScrollView.FOCUS_DOWN) }
    }

    private fun SpannableStringBuilder.appendColored(line: String) {
        append(colored(line))
    }

    /** Colors warning/error lines while leaving log text unparsed. */
    private fun colored(line: String): CharSequence {
        val color = when {
            isLevel(line, "ERROR") -> errorColor
            isLevel(line, "WARN") -> warnColor
            else -> Color.WHITE
        }
        val span = SpannableString(line)
        span.setSpan(ForegroundColorSpan(color), 0, line.length, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE)
        return span
    }

    private fun isLevel(line: String, level: String): Boolean {
        return line.contains(" ${level.padEnd(5)} ") || line.startsWith("$level ")
    }
}
