package io.dropcheck.agent

import android.Manifest
import android.app.Activity
import android.app.AlertDialog
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.graphics.Canvas
import android.graphics.Color
import android.graphics.Paint
import android.graphics.Typeface
import android.graphics.drawable.ColorDrawable
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
import android.view.ViewConfiguration
import android.view.ViewTreeObserver
import android.view.WindowInsets
import android.view.WindowInsetsController
import android.view.WindowManager
import android.view.inputmethod.EditorInfo
import android.view.inputmethod.InputConnection
import android.view.inputmethod.InputConnectionWrapper
import android.view.inputmethod.InputMethodManager
import android.widget.EditText
import android.widget.FrameLayout
import android.widget.ImageView
import android.widget.LinearLayout
import android.widget.ScrollView
import android.widget.TextView
import io.dropcheck.agent.grpc.CommandLog
import io.dropcheck.agent.grpc.CommandResult
import io.dropcheck.agent.grpc.GetWifiStatus
import io.dropcheck.agent.grpc.RunCommand
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
private const val SHELL_PROMPT = "dropcheck# "
private const val SHELL_BLANK_LINE = "\u00A0"
private const val SHELL_ERROR_COLOR = -44976
private const val SHELL_CURSOR_COLOR = -1
private const val SHELL_TEXT_SIZE_SP = 11f
private const val SHELL_CURSOR_BLINK_MS = 530L
private const val SHELL_CURSOR_WIDTH_SCALE = 0.5f
private const val SHELL_CURSOR_MIN_WIDTH_DP = 4f
private const val SHELL_CURSOR_TEXT_GAP_DP = 1.5f

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
    private var shellInputRowView: LinearLayout? = null
    private var shellImeBottomInset = 0
    private var statusIconViews: List<ImageView> = emptyList()
    private var controllerHeartbeatConnected = false
    private var standaloneActive = false
    private var standaloneRunning = false
    private var screenDimmed = false
    private var shellVisible = false
    private var shellBusy = false
    private var swipeStartX = 0f
    private var swipeStartY = 0f
    private val shellTapSlopPx: Int by lazy { ViewConfiguration.get(this).scaledTouchSlop }
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
    private var shellTranscriptSeeded = false

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
            isFocusableInTouchMode = true
            excludeFromContentCapture()
            addView(scroll, matchParentLayout())
            addView(shellScroll, matchParentLayout())
            addView(statusIconRow(), statusIconLayout())
        }
        root.setOnApplyWindowInsetsListener { _, insets ->
            val imeBottom = insets.getInsets(WindowInsets.Type.ime()).bottom
            if (shellImeBottomInset != imeBottom) {
                shellImeBottomInset = imeBottom
                updateShellContentPadding()
                if (shellVisible) scrollShellToInput()
            }
            insets
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
            setBackgroundColor(Color.BLACK)
            excludeFromContentCapture()
        }
        updateShellContentPadding()
        renderShell()
        return ScrollView(this).apply {
            setBackgroundColor(Color.BLACK)
            isFillViewport = true
            addView(shellContent)
            setOnTouchListener { _, event ->
                captureSwipeStart(event)
                if (event.actionMasked == MotionEvent.ACTION_UP) {
                    if (maybeHandleSwipe(event, SwipeDirection.LEFT)) {
                        return@setOnTouchListener true
                    }
                    if (isTapGesture(event)) {
                        focusShellInput(forceIme = true)
                        return@setOnTouchListener true
                    }
                }
                false
            }
        }
    }

    private fun renderShell() {
        if (!::shellContent.isInitialized) return
        seedShellTranscript()
        val useController = StandaloneWifiUseController(applicationContext)
        val inputRow = ensureShellInputRow()
        if (shellContent.indexOfChild(inputRow) < 0) {
            shellContent.removeAllViews()
            shellContent.addView(inputRow)
        } else {
            while (shellContent.indexOfChild(inputRow) > 0) {
                shellContent.removeViewAt(0)
            }
            while (shellContent.indexOfChild(inputRow) < shellContent.childCount - 1) {
                shellContent.removeViewAt(shellContent.childCount - 1)
            }
        }
        var insertAt = 0
        fun addShellView(view: View) {
            shellContent.addView(view, insertAt)
            insertAt += 1
        }
        addShellView(shellText("dropcheck shell", AgentLogStyle.TEXT_COLOR))
        addShellView(shellText("mode=idle runtime=${standaloneRunningLabel()} ${useController.statusText()}", AgentLogStyle.TEXT_COLOR))
        val liveNames = useController.liveWifiNames()
        if (liveNames.isNotEmpty()) {
            addShellView(shellText("live_wifi=${liveNames.joinToString(" ")}", AgentLogStyle.TEXT_COLOR))
        }
        addShellView(shellSpacer(8))
        shellTranscript.takeLast(SHELL_TRANSCRIPT_MAX_LINES).forEach {
            addShellView(shellText(it.text, it.color))
        }
    }

    private fun standaloneRunningLabel(): String {
        return when {
            StandaloneRuntimeState.running.get() -> "running"
            StandaloneRuntimeState.active.get() -> "active"
            else -> "inactive"
        }
    }

    private fun shellText(text: CharSequence, color: Int): TextView {
        return TextView(this).apply {
            this.text = if (text.isEmpty()) SHELL_BLANK_LINE else text
            setTextColor(color)
            setBackgroundColor(Color.BLACK)
            typeface = Typeface.MONOSPACE
            textSize = SHELL_TEXT_SIZE_SP
            includeFontPadding = false
            minHeight = 0
            minimumHeight = 0
            setLineSpacing(0f, 1.05f)
            setPadding(0, dp(2), 0, dp(2))
            excludeFromContentCapture()
        }
    }

    private fun ensureShellInputRow(): LinearLayout {
        return shellInputRowView ?: shellInputRow().also { shellInputRowView = it }
    }

    private fun shellInputRow(): LinearLayout {
        val input = ShellInputEditText(this).apply {
            setTextColor(AgentLogStyle.TEXT_COLOR)
            setHintTextColor(Color.DKGRAY)
            setBackgroundColor(Color.BLACK)
            typeface = Typeface.MONOSPACE
            textSize = SHELL_TEXT_SIZE_SP
            includeFontPadding = false
            minHeight = 0
            minimumHeight = 0
            setLineSpacing(0f, 1.05f)
            setPadding(0, dp(2), 0, dp(2))
            setSingleLine(true)
            inputType = InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS
            imeOptions = EditorInfo.IME_ACTION_GO
            useBlockCursor()
            excludeFromContentCapture()
            setOnEditorActionListener { view, actionId, event ->
                val enterKey = event?.keyCode == KeyEvent.KEYCODE_ENTER && event.action == KeyEvent.ACTION_UP
                if (actionId == EditorInfo.IME_ACTION_GO || actionId == EditorInfo.IME_ACTION_DONE || enterKey) {
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
            addView(input, LinearLayout.LayoutParams(LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.WRAP_CONTENT))
        }
    }

    private fun submitShellInput(raw: String) {
        val line = raw.trim()
        if (line.isBlank()) {
            shellInput?.setText("")
            focusShellInput()
            return
        }
        if (shellBusy) {
            focusShellInput()
            return
        }
        shellInput?.setText("")
        appendShellLine(shellCommandLine(line))
        when (val command = AgentShellParser.parse(line)) {
            AgentShellCommand.Noop -> Unit
            is AgentShellCommand.Help -> {
                appendShellLines(shellHelpLines(command.topic))
            }
            AgentShellCommand.ShowUse -> {
                appendShellLine(StandaloneWifiUseController(applicationContext).statusText())
            }
            AgentShellCommand.ShowWifiStatus -> runShellLinesCommand {
                val result = CommandExecutor(applicationContext, agentShellLogger()).execute(
                    RunCommand.newBuilder()
                        .setGetWifiStatus(GetWifiStatus.getDefaultInstance())
                        .build(),
                )
                if (result.status == CommandResult.Status.STATUS_OK && result.hasWifiStatus()) {
                    ShellCommandResult(ok = true, lines = AgentWifiStatusRenderer.render(result.wifiStatus))
                } else {
                    val message = result.message.ifBlank { result.status.name }
                    ShellCommandResult(ok = false, lines = listOf("show wifi status failed: $message"))
                }
            }
            AgentShellCommand.List -> {
                appendShellLines(StandaloneWifiUseController(applicationContext).liveWifiListText())
            }
            AgentShellCommand.ClearUse -> runShellCommand {
                StandaloneWifiUseController(applicationContext).clearUse()
            }
            is AgentShellCommand.Use -> runShellCommand {
                StandaloneWifiUseController(applicationContext).use(command.name)
            }
            is AgentShellCommand.Invalid -> appendShellLine(command.message, SHELL_ERROR_COLOR)
        }
    }

    private fun runShellCommand(action: () -> StandaloneUseResult) {
        runShellLinesCommand {
            val result = action()
            ShellCommandResult(result.ok, listOf(result.message))
        }
    }

    private fun runShellLinesCommand(action: () -> ShellCommandResult) {
        shellBusy = true
        renderShell()
        shellExecutor.submit {
            val result = runCatching { action() }.getOrElse {
                ShellCommandResult(false, listOf(it.message ?: it.toString()))
            }
            runOnUiThread {
                shellBusy = false
                appendShellLines(result.lines, if (result.ok) AgentLogStyle.TEXT_COLOR else SHELL_ERROR_COLOR)
                syncStatusIcons()
            }
        }
    }

    private fun agentShellLogger(): CommandLogger {
        return object : CommandLogger {
            override fun log(level: CommandLog.Level, message: String, scope: CommandLogScope) {
                TerminalLog.log(applicationContext, terminalLevelName(level), CommandTerminalLog.agentShell(scope, message))
            }
        }
    }

    private fun terminalLevelName(level: CommandLog.Level): String = when (level) {
        CommandLog.Level.LEVEL_DEBUG -> "DEBUG"
        CommandLog.Level.LEVEL_INFO -> "INFO"
        CommandLog.Level.LEVEL_WARN -> "WARN"
        CommandLog.Level.LEVEL_ERROR -> "ERROR"
        else -> "INFO"
    }

    private fun shellCommandLine(command: String): CharSequence {
        val text = "$SHELL_PROMPT$command"
        return SpannableString(text).apply {
            setSpan(
                ForegroundColorSpan(Color.YELLOW),
                0,
                SHELL_PROMPT.length,
                Spanned.SPAN_EXCLUSIVE_EXCLUSIVE,
            )
        }
    }

    private fun appendShellLine(text: CharSequence, color: Int = AgentLogStyle.TEXT_COLOR) {
        appendShellLines(listOf(text), color)
    }

    private fun appendShellLines(lines: Iterable<CharSequence>, color: Int = AgentLogStyle.TEXT_COLOR) {
        lines.forEach { shellTranscript.addLast(ShellTranscriptLine(it, color)) }
        trimShellTranscript()
        renderShell()
        scrollShellToInput()
        focusShellInput()
    }

    private fun seedShellTranscript() {
        if (shellTranscriptSeeded) return
        shellTranscriptSeeded = true
        shellHelpLines("").forEach {
            shellTranscript.addLast(ShellTranscriptLine(it, AgentLogStyle.TEXT_COLOR))
        }
        trimShellTranscript()
    }

    private fun trimShellTranscript() {
        while (shellTranscript.size > SHELL_TRANSCRIPT_MAX_LINES) shellTranscript.removeFirst()
    }

    private fun scrollShellToInput() {
        shellScroll.post {
            val inputRow = shellInputRowView ?: return@post
            shellScroll.requestChildFocus(inputRow, shellInput ?: inputRow)
            shellScroll.smoothScrollTo(0, inputRow.bottom)
        }
    }

    private fun updateShellContentPadding() {
        if (!::shellContent.isInitialized) return
        shellContent.setPadding(
            dp(SHELL_PANEL_PADDING_DP),
            dp(SHELL_PANEL_TOP_PADDING_DP),
            dp(SHELL_PANEL_PADDING_DP),
            dp(SHELL_PANEL_PADDING_DP) + shellImeBottomInset,
        )
    }

    private fun focusShellInput(forceIme: Boolean = false) {
        val input = shellInput ?: return
        input.requestFocus()
        input.post {
            if (!shellVisible || shellInput !== input) return@post
            input.requestFocus()
            if (forceIme && Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
                window.insetsController?.show(WindowInsets.Type.ime())
            }
            getSystemService(InputMethodManager::class.java)
                ?.showSoftInput(input, InputMethodManager.SHOW_IMPLICIT)
        }
    }

    private fun hideShellInputIme() {
        val input = shellInput ?: return
        getSystemService(InputMethodManager::class.java)
            ?.hideSoftInputFromWindow(input.windowToken, 0)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
            window.insetsController?.hide(WindowInsets.Type.ime())
        }
        input.clearFocus()
        root.requestFocus()
    }

    private fun shellHelpLines(topic: String): List<String> {
        return when (topic) {
            "" -> listOf(
                "Shell builtins:",
                "  clear use",
                "  help [NAME]",
                "  list",
                "  show use",
                "  show wifi status",
                "  use NAME",
                "",
                "Type 'help NAME' for more information.",
            )
            "clear", "clear use" -> listOf(
                "clear use: clear use",
                "    Clear the active Wi-Fi use override and restore standalone mode.",
            )
            "help" -> listOf(
                "help: help [NAME]",
                "    Display information about shell builtins.",
            )
            "list" -> listOf(
                "list: list",
                "    List live Wi-Fi targets available to use.",
            )
            "show" -> listOf(
                "show: show (use|wifi status)",
                "    show use displays the current Wi-Fi use override state.",
                "    show wifi status displays local Wi-Fi, IP, and MLO state.",
            )
            "use" -> listOf(
                "use: use NAME",
                "    Connect to the live Wi-Fi target NAME.",
            )
            else -> listOf("dropcheck: help: no help topics match '$topic'")
        }
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

    private fun isTapGesture(event: MotionEvent): Boolean {
        val dx = event.x - swipeStartX
        val dy = event.y - swipeStartY
        return kotlin.math.abs(dx) <= shellTapSlopPx && kotlin.math.abs(dy) <= shellTapSlopPx
    }

    private fun showShell() {
        if (shellVisible) return
        shellVisible = true
        renderShell()
        shellScroll.visibility = View.VISIBLE
        scroll.visibility = View.GONE
        scrollShellToInput()
        focusShellInput()
        resetIdleDimTimer()
    }

    private fun showViewer() {
        if (!shellVisible) return
        shellVisible = false
        hideShellInputIme()
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
        val text: CharSequence,
        val color: Int,
    )

    private data class ShellCommandResult(
        val ok: Boolean,
        val lines: List<CharSequence>,
    )

    private companion object {
        const val SHELL_TRANSCRIPT_MAX_LINES = 80
    }
}

private class ShellInputEditText(context: Context) : EditText(context) {
    private val cursorBlinkHandler = Handler(Looper.getMainLooper())
    private var cursorBlinkReady = false
    private var blockCursorVisible = true
    private val cursorBlink = object : Runnable {
        override fun run() {
            blockCursorVisible = !blockCursorVisible
            invalidate()
            cursorBlinkHandler.postDelayed(this, SHELL_CURSOR_BLINK_MS)
        }
    }
    private val cursorPaint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        color = SHELL_CURSOR_COLOR
        style = Paint.Style.FILL
    }
    private val terminalTextPaint = Paint(Paint.ANTI_ALIAS_FLAG)
    private val promptTextPaint = Paint(Paint.ANTI_ALIAS_FLAG)

    init {
        highlightColor = Color.TRANSPARENT
        useBlockCursor()
        cursorBlinkReady = true
    }

    fun useBlockCursor() {
        textCursorDrawable = ColorDrawable(Color.TRANSPARENT)
        isCursorVisible = false
    }

    override fun onAttachedToWindow() {
        super.onAttachedToWindow()
        restartCursorBlink()
    }

    override fun onDetachedFromWindow() {
        cursorBlinkHandler.removeCallbacks(cursorBlink)
        super.onDetachedFromWindow()
    }

    override fun onCreateInputConnection(outAttrs: EditorInfo): InputConnection? {
        val connection = super.onCreateInputConnection(outAttrs) ?: return null
        return object : InputConnectionWrapper(connection, true) {
            override fun deleteSurroundingText(beforeLength: Int, afterLength: Int): Boolean {
                if (beforeLength == 1 && afterLength == 0) return deleteBackwards()
                return super.deleteSurroundingText(beforeLength, afterLength)
            }

            override fun deleteSurroundingTextInCodePoints(beforeLength: Int, afterLength: Int): Boolean {
                if (beforeLength == 1 && afterLength == 0) return deleteBackwards()
                return super.deleteSurroundingTextInCodePoints(beforeLength, afterLength)
            }

            override fun sendKeyEvent(event: KeyEvent): Boolean {
                if (event.keyCode == KeyEvent.KEYCODE_DEL) {
                    if (event.action == KeyEvent.ACTION_DOWN) deleteBackwards()
                    return true
                }
                return super.sendKeyEvent(event)
            }
        }
    }

    override fun onKeyDown(keyCode: Int, event: KeyEvent): Boolean {
        if (keyCode == KeyEvent.KEYCODE_DEL) return deleteBackwards()
        return super.onKeyDown(keyCode, event)
    }

    override fun onKeyUp(keyCode: Int, event: KeyEvent): Boolean {
        if (keyCode == KeyEvent.KEYCODE_DEL) return true
        return super.onKeyUp(keyCode, event)
    }

    override fun onFocusChanged(focused: Boolean, direction: Int, previouslyFocusedRect: android.graphics.Rect?) {
        super.onFocusChanged(focused, direction, previouslyFocusedRect)
        restartCursorBlink()
    }

    override fun onSelectionChanged(selStart: Int, selEnd: Int) {
        super.onSelectionChanged(selStart, selEnd)
        restartCursorBlink()
    }

    override fun onTextChanged(text: CharSequence?, start: Int, lengthBefore: Int, lengthAfter: Int) {
        super.onTextChanged(text, start, lengthBefore, lengthAfter)
        restartCursorBlink()
    }

    override fun onDraw(canvas: Canvas) {
        terminalTextPaint.set(paint)
        terminalTextPaint.color = currentTextColor
        promptTextPaint.set(paint)
        promptTextPaint.color = Color.YELLOW

        val content = text?.toString().orEmpty()
        val cursor = selectionStart.coerceIn(0, content.length)
        val before = content.substring(0, cursor)
        val startX = compoundPaddingLeft.toFloat() - scrollX
        val baselineY = baseline.toFloat()
        val inputStartX = startX + promptTextPaint.measureText(SHELL_PROMPT)

        canvas.drawText(SHELL_PROMPT, startX, baselineY, promptTextPaint)
        if (content.isNotEmpty()) {
            canvas.drawText(content, inputStartX, baselineY, terminalTextPaint)
        }

        if (!blockCursorVisible) return
        val cursorX = inputStartX + terminalTextPaint.measureText(before) + resources.displayMetrics.density * SHELL_CURSOR_TEXT_GAP_DP
        val charEnd = if (cursor < content.length) {
            cursor + Character.charCount(Character.codePointAt(content, cursor))
        } else {
            cursor
        }
        val cursorText = content.substring(cursor, charEnd)
        val cursorWidth = maxOf(
            terminalTextPaint.measureText(cursorText.ifEmpty { "M" }) * SHELL_CURSOR_WIDTH_SCALE,
            resources.displayMetrics.density * SHELL_CURSOR_MIN_WIDTH_DP,
        )
        val metrics = terminalTextPaint.fontMetrics
        val cursorCenter = baselineY + (metrics.ascent + metrics.descent) / 2f
        val cursorHeight = terminalTextPaint.textSize
        val cursorTop = cursorCenter - cursorHeight / 2f
        val cursorBottom = cursorCenter + cursorHeight / 2f
        canvas.drawRect(cursorX, cursorTop, cursorX + cursorWidth, cursorBottom, cursorPaint)
    }

    private fun restartCursorBlink() {
        if (!cursorBlinkReady) return
        cursorBlinkHandler.removeCallbacks(cursorBlink)
        blockCursorVisible = true
        invalidate()
        if (isAttachedToWindow) {
            cursorBlinkHandler.postDelayed(cursorBlink, SHELL_CURSOR_BLINK_MS)
        }
    }

    private fun deleteBackwards(): Boolean {
        val editable = text ?: return true
        val start = selectionStart.coerceIn(0, editable.length)
        val end = selectionEnd.coerceIn(0, editable.length)
        if (start != end) {
            val from = minOf(start, end)
            val to = maxOf(start, end)
            editable.delete(from, to)
            setSelection(from)
            return true
        }
        if (start == 0) return true
        val deleteFrom = if (
            start >= 2 &&
            Character.isHighSurrogate(editable[start - 2]) &&
            Character.isLowSurrogate(editable[start - 1])
        ) {
            start - 2
        } else {
            start - 1
        }
        editable.delete(deleteFrom, start)
        setSelection(deleteFrom)
        return true
    }
}
