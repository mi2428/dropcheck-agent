package io.dropcheck.agent

import android.annotation.SuppressLint
import android.app.Activity
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.graphics.Color
import android.graphics.Typeface
import android.graphics.text.LineBreakConfig
import android.graphics.text.LineBreaker
import android.os.Build
import android.os.Bundle
import android.text.Layout
import android.text.SpannableString
import android.text.SpannableStringBuilder
import android.text.Spanned
import android.text.style.ForegroundColorSpan
import android.view.Gravity
import android.view.View
import android.view.ViewTreeObserver
import android.view.WindowManager
import android.widget.FrameLayout
import android.widget.ScrollView
import android.widget.TextView

private const val TERMINAL_BREAK_OPPORTUNITY = "\u200B"

/** Adds invisible break opportunities so terminal logs can wrap at any code point. */
internal fun terminalDisplayText(line: String): String {
    val display = StringBuilder(line.length * 2)
    var index = 0
    while (index < line.length) {
        val codePoint = Character.codePointAt(line, index)
        display.appendCodePoint(codePoint)
        if (codePoint != '\n'.code && codePoint != '\r'.code) {
            display.append(TERMINAL_BREAK_OPPORTUNITY)
        }
        index += Character.charCount(codePoint)
    }
    return display.toString()
}

/**
 * Minimal on-device terminal view for lab/debug sessions.
 *
 * It intentionally avoids app navigation or controls; controller interaction is
 * driven through adb/gRPC, while this screen exposes the local log tail.
 */
class MainActivity : Activity() {
    private lateinit var logView: TextView
    private lateinit var scroll: ScrollView
    private lateinit var root: FrameLayout
    private lateinit var standaloneLeft: View
    private lateinit var standaloneRight: View

    private val displayLineLengths = ArrayDeque<Int>()
    private var displayLogChars = 0
    private var logStartIndex = 0
    private var followLogTail = true
    private var scrollToBottomPending = false
    private val autoScrollSlopPx: Int by lazy {
        (TerminalDisplayPolicy.AUTO_SCROLL_SLOP_DP * resources.displayMetrics.density).toInt()
    }

    private val receiver = object : BroadcastReceiver() {
        override fun onReceive(context: Context, intent: Intent) {
            if (intent.action == StandaloneStateBroadcast.ACTION) {
                updateStandaloneIndicator(intent.getBooleanExtra(StandaloneStateBroadcast.EXTRA_ENABLED, false))
                return
            }
            val line = intent.getStringExtra(TerminalLog.EXTRA_LINE) ?: return
            append(line)
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        TerminalLog.info(this, "activity onCreate")

        val tail = TerminalLog.tail(
            this,
            TerminalDisplayPolicy.STARTUP_TAIL_LINES.coerceAtMost(TerminalDisplayPolicy.MAX_DISPLAY_LINES),
        )
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
            setTextColor(AgentLogStyle.TEXT_COLOR)
            setBackgroundColor(Color.BLACK)
            typeface = Typeface.MONOSPACE
            textSize = 8f
            includeFontPadding = true
            setLineSpacing(0f, 1.05f)
            setPadding(18, 18, 18, 18)
            setHorizontallyScrolling(false)
            breakStrategy = LineBreaker.BREAK_STRATEGY_SIMPLE
            hyphenationFrequency = Layout.HYPHENATION_FREQUENCY_NONE
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
                lineBreakWordStyle = LineBreakConfig.LINE_BREAK_WORD_STYLE_NONE
            }
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
        root = FrameLayout(this).apply {
            setBackgroundColor(Color.BLACK)
            addView(scroll)
            standaloneLeft = standaloneIndicatorView()
            standaloneRight = standaloneIndicatorView()
            addView(standaloneLeft, standaloneIndicatorLayout(Gravity.START))
            addView(standaloneRight, standaloneIndicatorLayout(Gravity.END))
        }
        setContentView(root)
        updateStandaloneIndicator(StandaloneConfigStore(this).load().enabled)
        requestScrollToBottom()
    }

    @SuppressLint("UnspecifiedRegisterReceiverFlag")
    override fun onStart() {
        super.onStart()
        val filter = IntentFilter(TerminalLog.ACTION_LINE)
        filter.addAction(StandaloneStateBroadcast.ACTION)
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
        val terminalLine = terminalDisplayText(displayLine)
        logView.append(colored(displayLine, terminalLine))
        displayLineLengths.addLast(terminalLine.length)
        displayLogChars += terminalLine.length
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
        if (
            displayLineLengths.size <= TerminalDisplayPolicy.MAX_DISPLAY_LINES &&
            displayLogChars <= TerminalDisplayPolicy.MAX_DISPLAY_CHARS
        ) return 0

        val text = mutableDisplayText()
        var removedLines = 0
        while (
            displayLineLengths.isNotEmpty() &&
            (displayLineLengths.size > TerminalDisplayPolicy.MAX_DISPLAY_LINES ||
                displayLogChars > TerminalDisplayPolicy.MAX_DISPLAY_CHARS)
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
        return TerminalDisplayPolicy.boundedLine(line, appendNewline = true)
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
        append(colored(line, terminalDisplayText(line)))
    }

    /** Colors notable lines while leaving log text unparsed. */
    private fun colored(line: String, terminalLine: String): CharSequence {
        val color = AgentLogStyle.colorForLine(line)
        val span = SpannableString(terminalLine)
        span.setSpan(ForegroundColorSpan(color), 0, terminalLine.length, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE)
        return span
    }

    private fun standaloneIndicatorView(): View {
        return View(this).apply {
            setBackgroundColor(Color.YELLOW)
            visibility = View.GONE
        }
    }

    private fun standaloneIndicatorLayout(gravity: Int): FrameLayout.LayoutParams {
        return FrameLayout.LayoutParams(standaloneIndicatorWidthPx(), FrameLayout.LayoutParams.MATCH_PARENT, gravity)
    }

    private fun standaloneIndicatorWidthPx(): Int {
        return (4 * resources.displayMetrics.density).toInt().coerceAtLeast(2)
    }

    private fun updateStandaloneIndicator(enabled: Boolean) {
        if (!::root.isInitialized || !::standaloneLeft.isInitialized || !::standaloneRight.isInitialized) return
        val visibility = if (enabled) View.VISIBLE else View.GONE
        standaloneLeft.visibility = visibility
        standaloneRight.visibility = visibility
        val sidePadding = if (enabled) standaloneIndicatorWidthPx() else 0
        scroll.setPadding(sidePadding, 0, sidePadding, 0)
    }
}
