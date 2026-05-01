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
import android.view.ViewTreeObserver
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
        const val STARTUP_TAIL_LINES = 600
        const val MAX_DISPLAY_LINES = 1000
        const val MAX_DISPLAY_CHARS = 300_000
        const val MAX_LINE_CHARS = 8_000
        const val AUTO_SCROLL_SLOP_DP = 2
    }

    private val warnColor = Color.rgb(255, 214, 10)
    private val errorColor = Color.rgb(255, 82, 82)

    private lateinit var logView: TextView
    private lateinit var scroll: ScrollView

    private val displayLineLengths = ArrayDeque<Int>()
    private var displayLogChars = 0
    private var logStartIndex = 0
    private var followLogTail = true
    private var scrollToBottomPending = false
    private val autoScrollSlopPx: Int by lazy {
        (AUTO_SCROLL_SLOP_DP * resources.displayMetrics.density).toInt()
    }

    private val receiver = object : BroadcastReceiver() {
        override fun onReceive(context: Context, intent: Intent) {
            val line = intent.getStringExtra(TerminalLog.EXTRA_LINE) ?: return
            append(line)
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        TerminalLog.info(this, "activity onCreate")

        val tail = TerminalLog.tail(this, STARTUP_TAIL_LINES.coerceAtMost(MAX_DISPLAY_LINES))
        val initialText = SpannableStringBuilder().apply {
            appendColored("dropcheck agent\n")
            appendColored("controller commands arrive over adb reverse + gRPC bidi\n")
            appendColored("\n")
            if (tail.isNotBlank()) {
                appendColored("-- terminal.log tail --\n")
            }
            logStartIndex = length
        }

        logView = TextView(this).apply {
            setTextColor(Color.WHITE)
            setBackgroundColor(Color.BLACK)
            typeface = Typeface.MONOSPACE
            textSize = 8f
            includeFontPadding = true
            setLineSpacing(0f, 1.05f)
            setPadding(18, 18, 18, 18)
            setText(initialText, TextView.BufferType.SPANNABLE)
        }
        if (tail.isNotBlank()) {
            tail.lineSequence().forEach { appendLogLine(it, followBottom = false) }
        }
        scroll = ScrollView(this).apply {
            setBackgroundColor(Color.BLACK)
            isFillViewport = true
            addView(logView)
            setOnScrollChangeListener { _, _, _, _, _ ->
                val atBottom = isScrolledToBottom()
                followLogTail = atBottom
                if (atBottom && trimDisplayIfNeeded() > 0) {
                    requestScrollToBottom()
                }
            }
        }
        setContentView(scroll)
        requestScrollToBottom()
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
        appendLogLine(line, followBottom = followLogTail)
    }

    private fun appendLogLine(line: String, followBottom: Boolean) {
        val displayLine = boundedLine(line)
        logView.append(colored(displayLine))
        displayLineLengths.addLast(displayLine.length)
        displayLogChars += displayLine.length
        // Do not delete old lines while the user is reading scrollback; removing text above the viewport
        // changes TextView layout and makes wrapped log lines visibly jump.
        if (followBottom || !::scroll.isInitialized) {
            trimDisplayIfNeeded()
        }
        if (followBottom) {
            followLogTail = true
            requestScrollToBottom()
        }
    }

    private fun trimDisplayIfNeeded(): Int {
        if (displayLineLengths.size <= MAX_DISPLAY_LINES && displayLogChars <= MAX_DISPLAY_CHARS) return 0

        val text = mutableDisplayText()
        var removedLines = 0
        while (
            displayLineLengths.isNotEmpty() &&
            (displayLineLengths.size > MAX_DISPLAY_LINES || displayLogChars > MAX_DISPLAY_CHARS)
        ) {
            val charsToRemove = displayLineLengths.removeFirst()
            removedLines += 1
            val start = logStartIndex.coerceAtMost(text.length)
            val end = (start + charsToRemove).coerceAtMost(text.length)
            if (end > start) {
                text.delete(start, end)
            }
            displayLogChars = (displayLogChars - charsToRemove).coerceAtLeast(0)
        }
        return removedLines
    }

    private fun mutableDisplayText(): SpannableStringBuilder {
        val current = logView.text
        if (current is SpannableStringBuilder) return current

        return SpannableStringBuilder(current).also {
            logView.setText(it, TextView.BufferType.SPANNABLE)
        }
    }

    private fun boundedLine(line: String): String {
        val withNewline = if (line.endsWith("\n")) line else "$line\n"
        if (withNewline.length <= MAX_LINE_CHARS) return withNewline

        val suffix = " ... [truncated]\n"
        return withNewline.take(MAX_LINE_CHARS - suffix.length).trimEnd('\r', '\n') + suffix
    }

    private fun isScrolledToBottom(): Boolean {
        val distanceToBottom = bottomScrollY() - scroll.scrollY
        return distanceToBottom <= autoScrollSlopPx
    }

    private fun requestScrollToBottom() {
        if (!::scroll.isInitialized || scrollToBottomPending) return

        scrollToBottomPending = true
        val observer = scroll.viewTreeObserver
        observer.addOnPreDrawListener(object : ViewTreeObserver.OnPreDrawListener {
            override fun onPreDraw(): Boolean {
                val currentObserver = scroll.viewTreeObserver
                if (observer.isAlive) {
                    observer.removeOnPreDrawListener(this)
                } else if (currentObserver.isAlive) {
                    currentObserver.removeOnPreDrawListener(this)
                }
                scrollToBottomPending = false
                if (followLogTail) scrollToBottomNow()
                return true
            }
        })
        scroll.invalidate()
    }

    private fun scrollToBottomNow() {
        scroll.scrollTo(0, bottomScrollY())
    }

    private fun bottomScrollY(): Int {
        val child = scroll.getChildAt(0) ?: return 0
        return (child.bottom - scroll.height).coerceAtLeast(0)
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
