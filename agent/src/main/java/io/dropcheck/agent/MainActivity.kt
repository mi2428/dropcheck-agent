package io.dropcheck.agent

import android.Manifest
import android.app.Activity
import android.app.AlertDialog
import android.content.Intent
import android.content.pm.PackageManager
import android.graphics.Color
import android.graphics.Typeface
import android.graphics.text.LineBreakConfig
import android.graphics.text.LineBreaker
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.text.Layout
import android.text.InputType
import android.text.SpannableString
import android.text.SpannableStringBuilder
import android.text.Spanned
import android.text.style.ForegroundColorSpan
import android.view.Gravity
import android.view.KeyEvent
import android.view.MotionEvent
import android.view.View
import android.view.ViewTreeObserver
import android.view.WindowInsets
import android.view.WindowInsetsController
import android.view.WindowManager
import android.view.inputmethod.EditorInfo
import android.view.inputmethod.InputMethodManager
import android.widget.EditText
import android.widget.FrameLayout
import android.widget.ImageView
import android.widget.LinearLayout
import android.widget.ScrollView
import android.widget.TextView
import java.util.concurrent.Executors

private const val TERMINAL_BREAK_OPPORTUNITY = "\u200B"
private const val STATUS_ICON_WIDTH_DP = 31
private const val STATUS_ICON_HEIGHT_DP = 26
private const val STATUS_ICON_GAP_DP = 4
private const val STATUS_ICON_LEFT_DP = 24
private const val STATUS_ICON_TOP_DP = 14
private const val IDLE_DIM_DELAY_MS = 60_000L
private const val IDLE_DIM_BRIGHTNESS = 0.03f
private const val WIFI_PERMISSION_REQUEST_CODE = 7101
private const val SHELL_SWIPE_MIN_DISTANCE_DP = 96
private const val SHELL_SWIPE_MAX_OFF_AXIS_DP = 72
private const val SHELL_PANEL_PADDING_DP = 12
private const val SHELL_PANEL_TOP_PADDING_DP = 48

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
 * It keeps the main screen as a local log tail and exposes a small swipe-in
 * shell for lab-only Wi-Fi target switching.
 */
class MainActivity : Activity() {
    private lateinit var logView: TextView
    private lateinit var scroll: ScrollView
    private lateinit var root: FrameLayout
    private lateinit var shellScroll: ScrollView
    private lateinit var shellContent: LinearLayout
    private var shellInput: EditText? = null
    private var statusIconViews: List<ImageView> = emptyList()
    private var controllerHeartbeatConnected = false
    private var standaloneActive = false
    private var standaloneRunning = false
    private var screenDimmed = false
    private var shellVisible = false
    private var shellBusy = false
    private var swipeStartX = 0f
    private var swipeStartY = 0f
    private var backgroundLocationPromptShown = false
    private val statusRefreshHandler = Handler(Looper.getMainLooper())
    private val statusRefresh = object : Runnable {
        override fun run() {
            syncStatusIcons()
            statusRefreshHandler.postDelayed(this, 1_000)
        }
    }
    private val idleDim = Runnable {
        setScreenDimmed(true)
    }

    private val displayLineLengths = ArrayDeque<Int>()
    private var displayLogChars = 0
    private var logStartIndex = 0
    private val autoScroll = TerminalAutoScrollState()
    private val autoScrollSlopPx: Int by lazy {
        (TerminalDisplayPolicy.AUTO_SCROLL_SLOP_DP * resources.displayMetrics.density).toInt()
    }

    private val terminalLogListener: (String) -> Unit = { line ->
        runOnUiThread { append(line) }
    }
    private val statusListener: () -> Unit = {
        runOnUiThread { syncStatusIcons() }
    }
    private val shellExecutor = Executors.newSingleThreadExecutor()
    private val shellTranscript = ArrayDeque<ShellTranscriptLine>()

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        hideSystemBars()
        TerminalLog.compactIfNeeded(this)
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
            includeFontPadding = false
            setLineSpacing(0f, 1.05f)
            setPadding(0, 0, 0, 0)
            setHorizontallyScrolling(false)
            breakStrategy = LineBreaker.BREAK_STRATEGY_SIMPLE
            hyphenationFrequency = Layout.HYPHENATION_FREQUENCY_NONE
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
                lineBreakWordStyle = LineBreakConfig.LINE_BREAK_WORD_STYLE_NONE
            }
            excludeFromContentCapture()
            setText(initialText, TextView.BufferType.SPANNABLE)
        }
        if (tail.isNotBlank()) {
            tail.lineSequence().forEach { appendLogLine(it, followBottom = false) }
        }
        scroll = ScrollView(this).apply {
            setBackgroundColor(Color.BLACK)
            isFillViewport = true
            addView(logView)
            setOnTouchListener { _, event ->
                captureSwipeStart(event)
                when (event.actionMasked) {
                    MotionEvent.ACTION_MOVE -> {
                        autoScroll.onUserScrollMove()
                    }
                    MotionEvent.ACTION_UP, MotionEvent.ACTION_CANCEL -> {
                        post {
                            autoScroll.onUserScrollEnd(isScrolledToBottom())
                            if (autoScroll.isFollowingTail) requestScrollToBottom()
                        }
                    }
                }
                if (event.actionMasked == MotionEvent.ACTION_UP && maybeHandleSwipe(event, SwipeDirection.RIGHT)) {
                    return@setOnTouchListener true
                }
                false
            }
            setOnScrollChangeListener { _, _, _, _, _ ->
                val atBottom = isScrolledToBottom()
                autoScroll.onScrollChanged(atBottom)
                if (atBottom && trimDisplayIfNeeded() > 0) {
                    requestScrollToBottom()
                }
            }
        }
        shellScroll = shellScreen().apply {
            visibility = View.GONE
        }
        root = FrameLayout(this).apply {
            setBackgroundColor(Color.BLACK)
            excludeFromContentCapture()
            addView(scroll, matchParentLayout())
            addView(shellScroll, matchParentLayout())
            addView(statusIconRow(), statusIconLayout())
        }
        setContentView(root)
        controllerHeartbeatConnected = ControllerSessionRuntimeState.heartbeatConnected()
        standaloneActive = isStandaloneActive()
        standaloneRunning = StandaloneRuntimeState.running.get()
        updateStatusIcons()
        resetIdleDimTimer()
        requestScrollToBottom()
    }

    override fun onStart() {
        super.onStart()
        TerminalLog.addListener(terminalLogListener)
        AgentStatusBroadcast.addListener(statusListener)
        statusRefreshHandler.post(statusRefresh)
    }

    override fun onResume() {
        super.onResume()
        hideSystemBars()
        syncStatusIcons()
        resetIdleDimTimer()
        ensureWifiLocationPermissions()
    }

    override fun onRequestPermissionsResult(requestCode: Int, permissions: Array<out String>, grantResults: IntArray) {
        super.onRequestPermissionsResult(requestCode, permissions, grantResults)
        if (requestCode == WIFI_PERMISSION_REQUEST_CODE) {
            ensureWifiLocationPermissions()
        }
    }

    override fun onUserInteraction() {
        super.onUserInteraction()
        resetIdleDimTimer()
    }

    override fun onWindowFocusChanged(hasFocus: Boolean) {
        super.onWindowFocusChanged(hasFocus)
        if (hasFocus) hideSystemBars()
    }

    override fun onStop() {
        statusRefreshHandler.removeCallbacks(statusRefresh)
        stopIdleDimTimer()
        TerminalLog.removeListener(terminalLogListener)
        AgentStatusBroadcast.removeListener(statusListener)
        super.onStop()
    }

    override fun onDestroy() {
        shellExecutor.shutdownNow()
        super.onDestroy()
    }

    /** Appends one terminal line and trims the view to a bounded size. */
    private fun append(line: String) {
        syncStatusIcons()
        appendLogLine(line, followBottom = shouldFollowLogTail())
    }

    private fun appendLogLine(line: String, followBottom: Boolean) {
        val preservedScrollY = if (!followBottom && ::scroll.isInitialized) scroll.scrollY else null
        val displayLine = boundedLine(line)
        val terminalLine = terminalDisplayText(displayLine)
        logView.append(colored(displayLine, terminalLine))
        displayLineLengths.addLast(terminalLine.length)
        displayLogChars += terminalLine.length
        trimDisplayIfNeeded()
        if (followBottom) {
            autoScroll.resumeFollowingTail()
            requestScrollToBottom()
        } else if (preservedScrollY != null) {
            scroll.post {
                if (!autoScroll.isFollowingTail) {
                    scroll.scrollTo(0, preservedScrollY.coerceAtMost(bottomScrollY()))
                }
            }
        }
    }

    private fun shouldFollowLogTail(): Boolean {
        return autoScroll.shouldFollowTail(!::scroll.isInitialized || isScrolledToBottom())
    }

    private fun ensureWifiLocationPermissions() {
        val missing = requiredRuntimePermissions().filter { checkSelfPermission(it) != PackageManager.PERMISSION_GRANTED }
        if (missing.isNotEmpty()) {
            requestPermissions(missing.toTypedArray(), WIFI_PERMISSION_REQUEST_CODE)
            return
        }
        if (!hasBackgroundLocationAccess()) {
            promptBackgroundLocationAccess()
        }
    }

    private fun requiredRuntimePermissions(): List<String> = buildList {
        add(Manifest.permission.ACCESS_FINE_LOCATION)
        add(Manifest.permission.ACCESS_COARSE_LOCATION)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            add(Manifest.permission.NEARBY_WIFI_DEVICES)
        }
    }

    private fun hasBackgroundLocationAccess(): Boolean {
        return checkSelfPermission(Manifest.permission.ACCESS_BACKGROUND_LOCATION) == PackageManager.PERMISSION_GRANTED
    }

    private fun promptBackgroundLocationAccess() {
        if (backgroundLocationPromptShown || isFinishing || isDestroyed) return
        backgroundLocationPromptShown = true
        AlertDialog.Builder(this)
            .setTitle(R.string.background_location_title)
            .setMessage(R.string.background_location_message)
            .setPositiveButton(R.string.background_location_open_settings) { _, _ -> openAppSettings() }
            .setNegativeButton(R.string.background_location_later, null)
            .show()
    }

    private fun openAppSettings() {
        val uri = Uri.fromParts("package", packageName, null)
        val intent = Intent(android.provider.Settings.ACTION_APPLICATION_DETAILS_SETTINGS, uri)
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        startActivity(intent)
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
        if (!::scroll.isInitialized || !autoScroll.markScrollToBottomPending()) return

        val observer = scroll.viewTreeObserver
        observer.addOnPreDrawListener(object : ViewTreeObserver.OnPreDrawListener {
            override fun onPreDraw(): Boolean {
                val currentObserver = scroll.viewTreeObserver
                if (observer.isAlive) {
                    observer.removeOnPreDrawListener(this)
                } else if (currentObserver.isAlive) {
                    currentObserver.removeOnPreDrawListener(this)
                }
                autoScroll.finishScrollToBottomPending()
                if (autoScroll.isFollowingTail) scrollToBottomNow()
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

    private fun statusIconRow(): LinearLayout {
        val size = dp(STATUS_ICON_WIDTH_DP)
        val height = dp(STATUS_ICON_HEIGHT_DP)
        val gap = dp(STATUS_ICON_GAP_DP)
        val icons = mutableListOf<ImageView>()
        return LinearLayout(this).apply {
            orientation = LinearLayout.HORIZONTAL
            gravity = Gravity.CENTER_VERTICAL
            isClickable = false
            isFocusable = false
            importantForAccessibility = View.IMPORTANT_FOR_ACCESSIBILITY_NO
            repeat(2) { index ->
                val icon = ImageView(this@MainActivity).apply {
                    setImageResource(R.drawable.shownet)
                    scaleType = ImageView.ScaleType.FIT_CENTER
                    isClickable = false
                    isFocusable = false
                    importantForAccessibility = View.IMPORTANT_FOR_ACCESSIBILITY_NO
                    visibility = View.GONE
                }
                val params = LinearLayout.LayoutParams(size, height).apply {
                    if (index > 0) marginStart = gap
                }
                addView(icon, params)
                icons += icon
            }
            statusIconViews = icons
        }
    }

    private fun statusIconLayout(): FrameLayout.LayoutParams {
        return FrameLayout.LayoutParams(
            FrameLayout.LayoutParams.WRAP_CONTENT,
            FrameLayout.LayoutParams.WRAP_CONTENT,
            Gravity.START or Gravity.TOP,
        ).apply {
            leftMargin = dp(STATUS_ICON_LEFT_DP)
            topMargin = dp(STATUS_ICON_TOP_DP)
        }
    }

    private fun updateStatusIcons() {
        val count = when {
            standaloneActive || standaloneRunning -> 2
            controllerHeartbeatConnected -> 1
            else -> 0
        }
        statusIconViews.forEachIndexed { index, icon ->
            icon.visibility = if (index < count) View.VISIBLE else View.GONE
        }
    }

    private fun syncStatusIcons() {
        controllerHeartbeatConnected = ControllerSessionRuntimeState.heartbeatConnected()
        standaloneActive = isStandaloneActive()
        standaloneRunning = StandaloneRuntimeState.running.get()
        updateStatusIcons()
    }

    private fun isStandaloneActive(): Boolean {
        return StandaloneRuntimeState.active.get() || StandaloneConfigStore(this).load().enabled
    }

    private fun resetIdleDimTimer() {
        setScreenDimmed(false)
        statusRefreshHandler.removeCallbacks(idleDim)
        statusRefreshHandler.postDelayed(idleDim, IDLE_DIM_DELAY_MS)
    }

    private fun stopIdleDimTimer() {
        statusRefreshHandler.removeCallbacks(idleDim)
        setScreenDimmed(false)
    }

    private fun setScreenDimmed(dimmed: Boolean) {
        if (screenDimmed == dimmed) return
        screenDimmed = dimmed
        window.attributes = window.attributes.apply {
            screenBrightness = if (dimmed) {
                IDLE_DIM_BRIGHTNESS
            } else {
                WindowManager.LayoutParams.BRIGHTNESS_OVERRIDE_NONE
            }
        }
    }

    private fun dp(value: Int): Int {
        return (value * resources.displayMetrics.density).toInt()
    }

    private fun matchParentLayout(): FrameLayout.LayoutParams {
        return FrameLayout.LayoutParams(
            FrameLayout.LayoutParams.MATCH_PARENT,
            FrameLayout.LayoutParams.MATCH_PARENT,
        )
    }

    private fun shellScreen(): ScrollView {
        shellContent = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(
                dp(SHELL_PANEL_PADDING_DP),
                dp(SHELL_PANEL_TOP_PADDING_DP),
                dp(SHELL_PANEL_PADDING_DP),
                dp(SHELL_PANEL_PADDING_DP),
            )
            setBackgroundColor(Color.BLACK)
            excludeFromContentCapture()
        }
        renderShell()
        return ScrollView(this).apply {
            setBackgroundColor(Color.BLACK)
            isFillViewport = true
            addView(shellContent)
            setOnTouchListener { _, event ->
                captureSwipeStart(event)
                if (event.actionMasked == MotionEvent.ACTION_UP && maybeHandleSwipe(event, SwipeDirection.LEFT)) {
                    return@setOnTouchListener true
                }
                false
            }
        }
    }

    private fun renderShell() {
        if (!::shellContent.isInitialized) return
        val useController = StandaloneWifiUseController(applicationContext)
        shellContent.removeAllViews()
        shellContent.addView(shellText("dropcheck shell", 16f, AgentLogStyle.TEXT_COLOR))
        shellContent.addView(shellText("mode=idle standalone=${standaloneRunningLabel()} ${useController.statusText()}", 10f, AgentLogStyle.TEXT_COLOR))
        val liveNames = useController.liveWifiNames()
        if (liveNames.isNotEmpty()) {
            shellContent.addView(shellText("live wifi: ${liveNames.joinToString(" ")}", 10f, AgentLogStyle.TEXT_COLOR))
        }
        shellContent.addView(shellSpacer(8))
        if (shellTranscript.isEmpty()) {
            shellHelpLines(liveNames).forEach { shellContent.addView(shellText(it, 11f, AgentLogStyle.TEXT_COLOR)) }
            shellContent.addView(shellSpacer(4))
        } else {
            shellTranscript.takeLast(SHELL_TRANSCRIPT_MAX_LINES).forEach {
                shellContent.addView(shellText(it.text, 11f, it.color))
            }
            shellContent.addView(shellSpacer(4))
        }
        shellContent.addView(shellInputRow())
    }

    private fun standaloneRunningLabel(): String {
        return when {
            StandaloneRuntimeState.running.get() -> "running"
            StandaloneRuntimeState.active.get() -> "active"
            else -> "inactive"
        }
    }

    private fun shellText(text: String, sizeSp: Float, color: Int): TextView {
        return TextView(this).apply {
            this.text = text
            setTextColor(color)
            setBackgroundColor(Color.BLACK)
            typeface = Typeface.MONOSPACE
            textSize = sizeSp
            includeFontPadding = false
            setLineSpacing(0f, 1.05f)
            setPadding(0, dp(2), 0, dp(2))
            excludeFromContentCapture()
        }
    }

    private fun shellInputRow(): LinearLayout {
        val input = EditText(this).apply {
            setTextColor(AgentLogStyle.TEXT_COLOR)
            setHintTextColor(Color.DKGRAY)
            setBackgroundColor(Color.BLACK)
            typeface = Typeface.MONOSPACE
            textSize = 12f
            includeFontPadding = false
            setSingleLine(true)
            inputType = InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS
            imeOptions = EditorInfo.IME_ACTION_DONE
            hint = if (shellBusy) "running..." else "use <name>"
            isEnabled = !shellBusy
            excludeFromContentCapture()
            setOnEditorActionListener { view, actionId, event ->
                val enterKey = event?.keyCode == KeyEvent.KEYCODE_ENTER && event.action == KeyEvent.ACTION_UP
                if (actionId == EditorInfo.IME_ACTION_DONE || enterKey) {
                    submitShellInput(view.text.toString())
                    true
                } else {
                    false
                }
            }
        }
        shellInput = input
        return LinearLayout(this).apply {
            orientation = LinearLayout.HORIZONTAL
            gravity = Gravity.CENTER_VERTICAL
            addView(shellText("dropcheck# ", 12f, Color.YELLOW))
            addView(input, LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f))
        }
    }

    private fun submitShellInput(raw: String) {
        val line = raw.trim()
        shellInput?.setText("")
        if (line.isBlank() || shellBusy) return
        appendShellLine("dropcheck# $line", Color.YELLOW)
        when (val command = AgentShellParser.parse(line)) {
            AgentShellCommand.Noop -> Unit
            AgentShellCommand.Help -> {
                shellHelpLines(StandaloneWifiUseController(applicationContext).liveWifiNames()).forEach { appendShellLine(it) }
            }
            AgentShellCommand.ShowUse -> {
                appendShellLine(StandaloneWifiUseController(applicationContext).statusText())
            }
            AgentShellCommand.ClearUse -> runShellCommand {
                StandaloneWifiUseController(applicationContext).clearUse()
            }
            is AgentShellCommand.Use -> runShellCommand {
                StandaloneWifiUseController(applicationContext).use(command.name)
            }
            is AgentShellCommand.Invalid -> appendShellLine(command.message, Color.rgb(255, 180, 80))
        }
    }

    private fun runShellCommand(action: () -> StandaloneUseResult) {
        shellBusy = true
        renderShell()
        shellExecutor.submit {
            val result = runCatching { action() }.getOrElse {
                StandaloneUseResult(false, it.message ?: it.toString())
            }
            runOnUiThread {
                shellBusy = false
                appendShellLine(result.message, if (result.ok) AgentLogStyle.TEXT_COLOR else Color.rgb(255, 180, 80))
                syncStatusIcons()
            }
        }
    }

    private fun appendShellLine(text: String, color: Int = AgentLogStyle.TEXT_COLOR) {
        shellTranscript.addLast(ShellTranscriptLine(text, color))
        while (shellTranscript.size > SHELL_TRANSCRIPT_MAX_LINES) shellTranscript.removeFirst()
        renderShell()
        shellScroll.post { shellScroll.fullScroll(View.FOCUS_DOWN) }
        shellInput?.requestFocus()
    }

    private fun shellHelpLines(liveNames: List<String>): List<String> = buildList {
        add("commands:")
        add("  use <name>    connect to standalone festa live wifi <name>")
        add("  clear use     restore standalone enabled state")
        add("  show use      show current use override")
        add("  help          show this help")
        if (liveNames.isNotEmpty()) add("names: ${liveNames.joinToString(" ")}")
    }

    private fun shellSpacer(heightDp: Int): View {
        return View(this).apply {
            layoutParams = LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT,
                dp(heightDp),
            )
        }
    }

    private enum class SwipeDirection {
        LEFT,
        RIGHT,
    }

    private fun captureSwipeStart(event: MotionEvent) {
        if (event.actionMasked == MotionEvent.ACTION_DOWN) {
            swipeStartX = event.x
            swipeStartY = event.y
        }
    }

    private fun maybeHandleSwipe(event: MotionEvent, direction: SwipeDirection): Boolean {
        val dx = event.x - swipeStartX
        val dy = event.y - swipeStartY
        if (kotlin.math.abs(dy) > dp(SHELL_SWIPE_MAX_OFF_AXIS_DP)) return false
        val min = dp(SHELL_SWIPE_MIN_DISTANCE_DP)
        return when {
            direction == SwipeDirection.RIGHT && dx >= min -> {
                showShell()
                true
            }
            direction == SwipeDirection.LEFT && dx <= -min -> {
                showViewer()
                true
            }
            else -> false
        }
    }

    private fun showShell() {
        if (shellVisible) return
        shellVisible = true
        renderShell()
        shellScroll.visibility = View.VISIBLE
        scroll.visibility = View.GONE
        shellInput?.requestFocus()
        shellInput?.post {
            getSystemService(InputMethodManager::class.java)?.showSoftInput(shellInput, InputMethodManager.SHOW_IMPLICIT)
        }
        resetIdleDimTimer()
    }

    private fun showViewer() {
        if (!shellVisible) return
        shellVisible = false
        shellScroll.visibility = View.GONE
        scroll.visibility = View.VISIBLE
        requestScrollToBottom()
        resetIdleDimTimer()
    }

    private fun View.excludeFromContentCapture() {
        importantForContentCapture = View.IMPORTANT_FOR_CONTENT_CAPTURE_NO_EXCLUDE_DESCENDANTS
    }

    private fun hideSystemBars() {
        @Suppress("DEPRECATION")
        window.setFlags(WindowManager.LayoutParams.FLAG_FULLSCREEN, WindowManager.LayoutParams.FLAG_FULLSCREEN)
        window.attributes = window.attributes.apply {
            layoutInDisplayCutoutMode = WindowManager.LayoutParams.LAYOUT_IN_DISPLAY_CUTOUT_MODE_SHORT_EDGES
        }
        @Suppress("DEPRECATION")
        window.setDecorFitsSystemWindows(false)

        val decorView = window.decorView
        decorView.windowInsetsController?.let { controller ->
            controller.hide(WindowInsets.Type.systemBars())
            controller.systemBarsBehavior = WindowInsetsController.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE
        }
    }

    private data class ShellTranscriptLine(
        val text: String,
        val color: Int,
    )

    private companion object {
        const val SHELL_TRANSCRIPT_MAX_LINES = 80
    }
}
