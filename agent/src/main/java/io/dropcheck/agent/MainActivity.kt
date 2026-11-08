package io.dropcheck.agent

import android.Manifest
import android.app.Activity
import android.app.AlertDialog
import android.content.Context
import android.content.ClipData
import android.content.ClipboardManager
import android.content.Intent
import android.content.pm.PackageManager
import android.graphics.Canvas
import android.graphics.Color
import android.graphics.Paint
import android.graphics.Typeface
import android.graphics.drawable.ColorDrawable
import android.graphics.drawable.GradientDrawable
import android.graphics.drawable.RippleDrawable
import android.graphics.text.LineBreakConfig
import android.graphics.text.LineBreaker
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.os.LocaleList
import android.text.Layout
import android.text.InputType
import android.text.SpannableString
import android.text.SpannableStringBuilder
import android.text.Spanned
import android.text.Selection
import android.text.style.ForegroundColorSpan
import android.view.Gravity
import android.view.GestureDetector
import android.view.ActionMode
import android.view.KeyEvent
import android.view.Menu
import android.view.MenuItem
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
import android.widget.EditText
import android.widget.FrameLayout
import android.widget.HorizontalScrollView
import android.widget.ImageView
import android.widget.LinearLayout
import android.widget.ScrollView
import android.widget.TextView
import io.dropcheck.agent.grpc.CommandLog
import java.util.IdentityHashMap
import java.util.Locale
import kotlin.math.roundToInt

private const val TERMINAL_BREAK_OPPORTUNITY = "\u200B"
internal const val ACTION_OPEN_LOG_VIEWER = "io.dropcheck.agent.action.OPEN_LOG_VIEWER"
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
private const val SHELL_PROMPT = "self> "
private const val SHELL_BLANK_LINE = "\u00A0"
private const val SHELL_ERROR_COLOR = -44976
private const val SHELL_CURSOR_COLOR = -1
private const val TERMINAL_TEXT_SIZE_SP = 8f
private const val SHELL_TEXT_SIZE_SP = TERMINAL_TEXT_SIZE_SP + 2f
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
 * shell for interactive Wi-Fi inspection, direct connect tests, and probes.
 */
class MainActivity : Activity() {
    private lateinit var logView: TextView
    private lateinit var scroll: ScrollView
    private lateinit var root: FrameLayout
    private lateinit var shellScroll: ScrollView
    private lateinit var shellContent: LinearLayout
    private val shellAutoScroll = TerminalAutoScrollState()
    private var shellScrollTarget: (() -> Int)? = null
    private var shellInput: EditText? = null
    private lateinit var shellKeyBar: HorizontalScrollView
    private lateinit var shellInputPanel: LinearLayout
    private var shellInputPanelHeight = 0
    private var shellBottomInset = 0
    private var shellSafeLeftInset = 0
    private var shellSafeTopInset = 0
    private var shellSafeRightInset = 0
    private var statusIconViews: List<ImageView> = emptyList()
    private var controllerHeartbeatConnected = false
    private var screenDimmed = false
    private var shellVisible = false
    private val shellBusy get() = shellOwner.busy
    private var shellOwner = ShellActionOwner()
    private var shellSession = AgentShellSession()
    private val shellCompletionListener: (AgentShellResult) -> Unit = { result ->
        if (result.block != null && (result.block.fullText.isNotEmpty() || result.ok)) appendShellBlock(result.block, false) else appendShellLines(
            result.lines.ifEmpty { if (result.ok) emptyList() else listOf("Command FAILED (output filtered)") }, if (result.ok) AgentLogStyle.TEXT_COLOR else SHELL_ERROR_COLOR,
            focusInput = false,
        )
        syncStatusIcons()
    }
    private var shellActionStatus: TextView? = null
    private var viewerToggle: TextView? = null
    private var swipeStartX = 0f
    private var swipeStartY = 0f
    private val shellTapSlopPx: Int by lazy { ViewConfiguration.get(this).scaledTouchSlop }
    private var backgroundLocationPromptShown = false
    private var runtimePermissionsRequested = false
    private val statusRefreshHandler = Handler(Looper.getMainLooper())
    private val statusRefresh = object : Runnable {
        override fun run() {
            syncStatusIcons()
            shellActionStatus?.text = getString(R.string.shell_operation_status, shellStatus())
            statusRefreshHandler.postDelayed(this, 1_000)
        }
    }
    private val idleDim = Runnable {
        setScreenDimmed(true)
    }

    private val displayLineLengths = ArrayDeque<Int>()
    private var displayLogChars = 0
    private var logStartIndex = 0
    private var lastLogLine: String? = null
    private val autoScroll = TerminalAutoScrollState()

    private var logGeneration = 0L
    private var terminalLogListener: ((String) -> Unit)? = null
    private val statusListener: () -> Unit = {
        runOnUiThread { syncStatusIcons() }
    }
    private var shellTranscript = AgentBlockHistory()
    private val shellBlockViews = IdentityHashMap<AgentPresentationBlock, LinearLayout>()
    private var shellContentWidthPx = 0
    private var shellOutputSizeSp = SHELL_TEXT_SIZE_SP
    private var shellMinimumSizeSp = 10f
    private var shellTranscriptSeeded = false

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        runtimePermissionsRequested = savedInstanceState?.getBoolean("runtimePermissionsRequested") ?: false
        removePersistedShellCredential(applicationContext)
        if (lastNonConfigurationInstance == null) AgentSafeValueFile.clearAbandoned(cacheDir)
        (lastNonConfigurationInstance as? ShellDisplayState)?.let {
            shellTranscript = it.history
            shellTranscriptSeeded = true
            shellOutputSizeSp = it.baseSizeSp
            shellMinimumSizeSp = it.minimumSizeSp
            shellOwner = it.owner
            shellSession = it.session
        }
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
            textSize = TERMINAL_TEXT_SIZE_SP
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
            tail.lineSequence().forEach { appendLogLine(it) }
        }
        scroll = ScrollView(this).apply {
            setBackgroundColor(Color.BLACK)
            isFillViewport = true
            addView(logView)
            setOnTouchListener { view, event ->
                captureSwipeStart(event)
                if (event.actionMasked == MotionEvent.ACTION_MOVE) autoScroll.onUserScrollMove()
                if (event.actionMasked == MotionEvent.ACTION_UP || event.actionMasked == MotionEvent.ACTION_CANCEL) {
                    autoScroll.onUserScrollEnd(scrollY >= bottomScrollY() - 2)
                }
                if (event.actionMasked == MotionEvent.ACTION_UP && maybeHandleSwipe(event, SwipeDirection.RIGHT)) {
                    return@setOnTouchListener true
                }
                if (event.actionMasked == MotionEvent.ACTION_UP && isTapGesture(event)) {
                    view.performClick()
                }
                false
            }
            setOnScrollChangeListener { _, _, y, _, _ -> autoScroll.onScrollChanged(y >= bottomScrollY() - 2) }
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
            shellInputPanel = LinearLayout(this@MainActivity).apply {
                orientation = LinearLayout.VERTICAL
                shellKeyBar = shellKeyboard()
                addView(shellKeyBar)
                visibility = View.GONE
                addOnLayoutChangeListener { view, _, _, _, _, _, _, _, _ ->
                    if (shellInputPanelHeight != view.height) {
                        shellInputPanelHeight = view.height
                        updateShellContentPadding()
                    }
                }
            }
            addView(shellInputPanel, FrameLayout.LayoutParams(FrameLayout.LayoutParams.MATCH_PARENT, FrameLayout.LayoutParams.WRAP_CONTENT, Gravity.BOTTOM))
            addView(statusIconRow(), statusIconLayout())
            viewerToggle = shellText("Agent Shell", AgentLogStyle.TEXT_COLOR).apply {
                contentDescription = "Switch to Agent Shell"
                isFocusable = true
                minHeight = dp(48)
                setOnClickListener { showShell() }
            }
            addView(viewerToggle, FrameLayout.LayoutParams(FrameLayout.LayoutParams.WRAP_CONTENT, FrameLayout.LayoutParams.WRAP_CONTENT, Gravity.TOP or Gravity.END).apply { topMargin = dp(48); marginEnd = dp(12) })
        }
        root.setOnApplyWindowInsetsListener { _, insets ->
            val cutoutInsets = insets.getInsets(WindowInsets.Type.displayCutout())
            val systemBarInsets = insets.getInsets(WindowInsets.Type.systemBars())
            val bottomInset = maxOf(insets.getInsets(WindowInsets.Type.ime()).bottom, systemBarInsets.bottom)
            val safeLeft = maxOf(cutoutInsets.left, systemBarInsets.left)
            val safeTop = cutoutInsets.top
            val safeRight = maxOf(cutoutInsets.right, systemBarInsets.right)
            if (
                shellBottomInset != bottomInset ||
                shellSafeLeftInset != safeLeft ||
                shellSafeTopInset != safeTop ||
                shellSafeRightInset != safeRight
            ) {
                shellBottomInset = bottomInset
                shellSafeLeftInset = safeLeft
                shellSafeTopInset = safeTop
                shellSafeRightInset = safeRight
                shellInputPanel.setPadding(safeLeft, 0, safeRight, 0)
                shellInputPanel.layoutParams = (shellInputPanel.layoutParams as FrameLayout.LayoutParams).apply { bottomMargin = bottomInset }
                updateShellContentPadding()
            }
            insets
        }
        setContentView(root)
        controllerHeartbeatConnected = ControllerSessionRuntimeState.heartbeatConnected()
        updateStatusIcons()
        resetIdleDimTimer()
        showInitialScreen(intent)
        (lastNonConfigurationInstance as? ShellDisplayState)?.let { state ->
            shellAutoScroll.onUserScrollEnd(state.followTail)
            if (!state.followTail) {
                shellAutoScroll.finishScrollToBottomPending()
                requestShellScroll({ state.scrollY }, replace = true)
            }
        }
        shellOwner.attach(shellCompletionListener)
    }

    @Suppress("DEPRECATION")
    override fun onRetainNonConfigurationInstance(): Any = ShellDisplayState(
        shellTranscript, shellOutputSizeSp, shellMinimumSizeSp, if (::shellScroll.isInitialized) shellScroll.scrollY else 0, shellOwner, shellSession,
        shouldFollowShellTail(),
    )

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        showInitialScreen(intent)
    }

    override fun onStart() {
        super.onStart()
        val generation = ++logGeneration
        val listener: (String) -> Unit = { line -> runOnUiThread { if (logGeneration == generation) append(line) } }
        terminalLogListener = listener
        val snapshot = TerminalLog.addListenerWithTail(this, listener, TerminalDisplayPolicy.STARTUP_TAIL_LINES)
        terminalCatchUp(lastLogLine, snapshot).forEach(::appendLogLine)
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
        if (requestCode == WIFI_PERMISSION_REQUEST_CODE && grantResults.isNotEmpty() && grantResults.all { it == PackageManager.PERMISSION_GRANTED }) {
            ensureWifiLocationPermissions()
        }
    }

    override fun onSaveInstanceState(outState: Bundle) {
        outState.putBoolean("runtimePermissionsRequested", runtimePermissionsRequested)
        super.onSaveInstanceState(outState)
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
        logGeneration++
        statusRefreshHandler.removeCallbacks(statusRefresh)
        stopIdleDimTimer()
        terminalLogListener?.let(TerminalLog::removeListener)
        terminalLogListener = null
        AgentStatusBroadcast.removeListener(statusListener)
        super.onStop()
    }

    override fun onDestroy() {
        shellOwner.detach(shellCompletionListener)
        if (!isChangingConfigurations) shellOwner.cancel()
        super.onDestroy()
    }

    /** Appends one terminal line and trims the view to a bounded size. */
    private fun append(line: String) {
        syncStatusIcons()
        appendLogLine(line)
    }

    private fun appendLogLine(line: String) {
        lastLogLine = line
        val follow = !::scroll.isInitialized || scroll.visibility != View.VISIBLE && autoScroll.isFollowingTail ||
            ::scroll.isInitialized && scroll.visibility == View.VISIBLE && autoScroll.shouldFollowTail(scroll.scrollY >= bottomScrollY() - 2)
        val displayLine = boundedLine(line)
        val terminalLine = terminalDisplayText(displayLine)
        logView.append(colored(displayLine, terminalLine))
        displayLineLengths.addLast(terminalLine.length)
        displayLogChars += terminalLine.length
        trimDisplayIfNeeded()
        if (follow) requestScrollToBottom()
    }

    private fun ensureWifiLocationPermissions() {
        val missing = agentRuntimePermissions(Build.VERSION.SDK_INT).filter { checkSelfPermission(it) != PackageManager.PERMISSION_GRANTED }
        if (missing.isNotEmpty()) {
            if (!runtimePermissionsRequested) {
                runtimePermissionsRequested = true
                requestPermissions(missing.toTypedArray(), WIFI_PERMISSION_REQUEST_CODE)
            }
            return
        }
        if (!hasBackgroundLocationAccess()) {
            promptBackgroundLocationAccess()
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
                scrollToBottomNow()
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
            repeat(1) { index ->
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
        val count = if (controllerHeartbeatConnected) 1 else 0
        statusIconViews.forEachIndexed { index, icon ->
            icon.visibility = if (index < count) View.VISIBLE else View.GONE
        }
    }

    private fun syncStatusIcons() {
        controllerHeartbeatConnected = ControllerSessionRuntimeState.heartbeatConnected()
        updateStatusIcons()
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
            addOnLayoutChangeListener { view, _, top, _, bottom, _, oldTop, _, oldBottom ->
                val width = (view.width - view.paddingLeft - view.paddingRight).coerceAtLeast(0)
                val follow = ::shellScroll.isInitialized && shellAutoScroll.isFollowingTail &&
                    (shellAutoScroll.isScrollToBottomPending || shellScroll.scrollY >= oldBottom - oldTop - shellScroll.height - 2)
                if (follow && (bottom - top != oldBottom - oldTop || width != shellContentWidthPx)) scrollShellToTail()
                if (width != shellContentWidthPx) {
                    shellContentWidthPx = width
                    renderShell()
                }
            }
        }
        shellContent.addView(createShellInput())
        updateShellContentPadding()
        renderShell()
        return object : ScrollView(this) {
            private var tapCandidate = false
            private var dismissedImeOnDrag = false

            override fun dispatchTouchEvent(event: MotionEvent): Boolean {
                // Observe output children too, without taking over controls, long presses or drags.
                when (event.actionMasked) {
                    MotionEvent.ACTION_DOWN -> { captureSwipeStart(event); tapCandidate = true; dismissedImeOnDrag = false }
                    MotionEvent.ACTION_MOVE -> if (!isTapGesture(event)) tapCandidate = false
                    MotionEvent.ACTION_CANCEL -> tapCandidate = false
                }
                val shortTap = event.actionMasked == MotionEvent.ACTION_UP && tapCandidate && isTapGesture(event) &&
                    event.eventTime - event.downTime < ViewConfiguration.getLongPressTimeout()
                val imeVisible = window.decorView.rootWindowInsets?.isVisible(WindowInsets.Type.ime())
                val outputTap = shortTap && event.y >= 0 && event.y < height && event.x >= 0 && event.x < width &&
                    (0 until shellContent.childCount).none { index ->
                        val child = shellContent.getChildAt(index)
                        val x = event.x + scrollX - shellContent.left
                        val y = event.y + scrollY - shellContent.top
                        child.isClickable && x >= child.left && x < child.right && y >= child.top && y < child.bottom
                    }
                if (outputTap && imeVisible == true) {
                    // A short output tap is not an editor-focus action. Cancel the child's pending
                    // click before TextView can steal input focus and resize/scroll the viewport.
                    val cancel = MotionEvent.obtain(event).apply { action = MotionEvent.ACTION_CANCEL }
                    try { super.dispatchTouchEvent(cancel) } finally { cancel.recycle() }
                    tapCandidate = false
                    performClick()
                    return true
                }
                val openIme = outputTap && event.y >= height * 2f / 3f && imeVisible == false
                val previousY = scrollY
                val handled = super.dispatchTouchEvent(event)
                val dy = event.y - swipeStartY
                if (event.actionMasked == MotionEvent.ACTION_MOVE && !dismissedImeOnDrag && imeVisible == true &&
                    scrollY < previousY && dy > shellTapSlopPx && dy > kotlin.math.abs(event.x - swipeStartX)) {
                    dismissedImeOnDrag = true
                    hideShellInputIme()
                }
                if (event.actionMasked == MotionEvent.ACTION_UP) tapCandidate = false
                if (openIme) focusShellInput()
                return handled
            }

            override fun requestChildRectangleOnScreen(child: View, rectangle: android.graphics.Rect, immediate: Boolean): Boolean {
                if (shellInput?.hasFocus() == true) {
                    // Caret visibility shares the tail coordinator, never a second native smooth scroll.
                    if (shellAutoScroll.isFollowingTail) scrollShellToTail()
                    return false
                }
                return super.requestChildRectangleOnScreen(child, rectangle, immediate)
            }
        }.apply {
            setBackgroundColor(Color.BLACK)
            isSmoothScrollingEnabled = false
            isFillViewport = true
            addView(shellContent, FrameLayout.LayoutParams(
                FrameLayout.LayoutParams.MATCH_PARENT,
                FrameLayout.LayoutParams.WRAP_CONTENT,
            ))
            setOnTouchListener { view, event ->
                captureSwipeStart(event)
                if (event.actionMasked == MotionEvent.ACTION_MOVE) {
                    shellAutoScroll.onUserScrollMove()
                    // A reader gesture wins over a queued tail or anchor restoration.
                    if (shellScrollTarget != null) shellScrollTarget = { scrollY }
                }
                if (event.actionMasked == MotionEvent.ACTION_UP || event.actionMasked == MotionEvent.ACTION_CANCEL) {
                    shellAutoScroll.onUserScrollEnd(!canScrollVertically(1))
                }
                if (event.actionMasked == MotionEvent.ACTION_UP) {
                    if (maybeHandleSwipe(event, SwipeDirection.LEFT)) {
                        return@setOnTouchListener true
                    }
                    if (isTapGesture(event)) {
                        view.performClick()
                        return@setOnTouchListener true
                    }
                }
                false
            }
            setOnScrollChangeListener { _, _, _, _, _ ->
                // Honor explicit navigation, not layout-driven clamps while following.
                if (!isLayoutRequested && !shellContent.isLayoutRequested && shellScrollTarget == null) {
                    shellAutoScroll.onUserScrollEnd(!canScrollVertically(1))
                } else shellAutoScroll.onScrollChanged(!canScrollVertically(1))
            }
            addOnLayoutChangeListener { _, _, top, _, bottom, _, oldTop, _, oldBottom ->
                if (bottom - top != oldBottom - oldTop && shellAutoScroll.isFollowingTail) scrollShellToTail()
            }
        }
    }

    private fun renderShell() {
        if (!::shellContent.isInitialized) return
        val follow = shouldFollowShellTail()
        seedShellTranscript()
        val inputFocused = shellInput?.hasFocus() == true
        val focusedBlock = shellBlockViews.values.firstOrNull { (it.getChildAt(0) as TextView).hasFocus() }
        val anchor = if (::shellScroll.isInitialized) shellScroll.scrollY else 0
        val anchoredView = if (::shellScroll.isInitialized) shellTranscript.blocks.mapNotNull { shellBlockViews[it] }.firstOrNull { it.bottom > anchor } else null
        val anchorOffset = anchoredView?.let { anchor - it.top }
        // Keep the live editor attached so reflow cannot reset IME composition or focus.
        shellContent.removeViews(0, shellContent.childCount - 1)
        fun addShellView(view: View) {
            shellContent.addView(view, shellContent.childCount - 1)
        }
        addShellView(shellText("Agent Shell", AgentLogStyle.TEXT_COLOR))
        addShellView(shellText("Log Viewer", AgentLogStyle.TEXT_COLOR).apply {
            contentDescription = "Switch to Log Viewer"
            isFocusable = true
            minHeight = dp(48)
            setOnClickListener { showViewer() }
        })
        val controllerState = if (ControllerSessionRuntimeState.heartbeatConnected()) "connected" else "idle"
        val defaults = shellSession.defaults
        addShellView(shellText("surface=agent-shell controller=$controllerState ${AgentShellUsePolicy.statusText(defaults)}", AgentLogStyle.TEXT_COLOR))
        shellActionStatus = shellText(getString(R.string.shell_operation_status, shellStatus()), AgentLogStyle.TEXT_COLOR)
        addShellView(shellActionStatus!!)
        if (shellBusy) addShellView(shellText("Cancel current Shell operation", AgentLogStyle.TEXT_COLOR).apply {
            contentDescription = "Cancel current Agent Shell operation"
            isFocusable = true
            minHeight = dp(48)
            setOnClickListener { shellOwner.cancel() }
        })
        addShellView(shellText("Output size: ${shellOutputSizeSp}sp (minimum ${shellMinimumSizeSp}sp)", AgentLogStyle.TEXT_COLOR).apply {
            contentDescription = "Output text size settings. Current ${shellOutputSizeSp}sp, minimum ${shellMinimumSizeSp}sp"
            setOnClickListener { showShellOutputSizeSettings() }
            minHeight = dp(48)
            isFocusable = true
        })
        addShellView(shellSpacer(8))
        if (shellTranscript.evictedBlocks > 0) addShellView(shellText("History: ${shellTranscript.evictedBlocks} older blocks evicted", AgentLogStyle.TEXT_COLOR))
        shellBlockViews.keys.toList().filter { block -> shellTranscript.blocks.none { it === block } }.forEach { shellBlockViews.remove(it) }
        shellTranscript.blocks.forEach { block ->
            val view = shellBlockViews.getOrPut(block) { shellBlockView(block) }
            reflowShellBlock(view, block)
            addShellView(view)
        }
        if (inputFocused) shellInput?.requestFocus()
        else focusedBlock?.takeIf { it.parent === shellContent }?.getChildAt(0)?.requestFocus()
        if (::shellScroll.isInitialized) {
            if (follow) scrollShellToTail() else requestShellScroll({
                anchoredView?.takeIf { it.parent === shellContent }?.let { it.top + (anchorOffset ?: 0) } ?: anchor
            })
        }
    }

    private fun shellBlockView(block: AgentPresentationBlock): LinearLayout = LinearLayout(this).apply {
        orientation = LinearLayout.VERTICAL
        addView(shellText("", block.color).apply {
            setTextIsSelectable(true)
            val inspectAction = View.generateViewId()
            customSelectionActionModeCallback = object : ActionMode.Callback {
                override fun onCreateActionMode(mode: ActionMode, menu: Menu): Boolean {
                    menu.add(Menu.NONE, inspectAction, Menu.NONE, "Full result")
                    return true
                }
                override fun onPrepareActionMode(mode: ActionMode, menu: Menu) = false
                override fun onActionItemClicked(mode: ActionMode, item: MenuItem): Boolean {
                    if (item.itemId != inspectAction) return false
                    mode.finish()
                    val values = block.safeValueFile
                    if (values != null) showSafeValuePage(values, 0) else {
                        AlertDialog.Builder(this@MainActivity).setTitle("Full safe result")
                            .setView(ScrollView(this@MainActivity).apply {
                                addView(shellText(block.fullText, block.color).apply { setTextIsSelectable(true) })
                            }).setPositiveButton("Close", null).show()
                    }
                    return true
                }
                override fun onDestroyActionMode(mode: ActionMode) = Unit
            }
        })
    }

    private fun showSafeValuePage(values: AgentSafeValueFile, start: Int) {
        val labels = try { values.labels(start) } catch (failure: java.io.IOException) {
            AlertDialog.Builder(this).setTitle("Safe values unavailable").setMessage("The result was evicted or its private cache cannot be read.").setPositiveButton("Close", null).show()
            return
        }
        val dialog = AlertDialog.Builder(this).setTitle("Safe values ${start + 1}–${start + labels.size}/${values.valueCount}")
            .setItems(labels.toTypedArray()) { _, index -> showSafeValue(values, start + index) }
            .setNeutralButton("Close", null)
        if (start > 0) dialog.setNegativeButton("Previous") { _, _ -> showSafeValuePage(values, (start - 25).coerceAtLeast(0)) }
        if (start + labels.size < values.valueCount) dialog.setPositiveButton("Next") { _, _ -> showSafeValuePage(values, start + labels.size) }
        dialog.show()
    }

    private fun showSafeValue(values: AgentSafeValueFile, index: Int, chunkStart: Int = 0) {
        val value = try { values.value(index) } catch (failure: java.io.IOException) {
            AlertDialog.Builder(this).setTitle("Safe value unavailable").setMessage("The result was evicted or its private cache cannot be read.").setPositiveButton("Close", null).show()
            return
        }
        // Binder clipboard payloads are bounded. Normal identities fit in one chunk;
        // long opaque native references remain fully reachable in numbered chunks.
        var end = (chunkStart + 32768).coerceAtMost(value.length)
        if (end < value.length && end > chunkStart && Character.isHighSurrogate(value[end - 1])) end--
        val chunk = value.substring(chunkStart, end)
        val view = shellText(chunk, AgentLogStyle.TEXT_COLOR).apply { setTextIsSelectable(true) }
        val range = if (value.isEmpty()) "empty (0 characters)" else "${chunkStart + 1}–$end/${value.length}"
        val dialog = AlertDialog.Builder(this).setTitle("Full safe value ${index + 1}; $range")
            .setView(ScrollView(this).apply { addView(view) })
            .setNegativeButton("Values") { _, _ -> showSafeValuePage(values, (index / 25) * 25) }
            .setNeutralButton(if (chunkStart == 0 && end == value.length) "Copy full value" else "Copy this numbered chunk") { _, _ ->
                getSystemService(ClipboardManager::class.java).setPrimaryClip(ClipData.newPlainText("Dropcheck safe value", chunk))
            }
        if (end < value.length) dialog.setPositiveButton("Next chunk") { _, _ -> showSafeValue(values, index, end) }
        else dialog.setPositiveButton("Close", null)
        dialog.show()
    }

    private fun reflowShellBlock(view: LinearLayout, block: AgentPresentationBlock) {
        val textView = view.getChildAt(0) as TextView
        textView.textSize = block.baseSizeSp
        val layout = AgentNativeBlockLayout.render(block, shellContentWidthPx, textView.paint) ?: return
        if (textView.text.toString() == layout.text.toString() && textView.tag == shellContentWidthPx) return
        val selectionStart = textView.selectionStart
        val selectionEnd = textView.selectionEnd
        textView.text = layout.text
        textView.tag = shellContentWidthPx
        if (selectionStart >= 0 && selectionEnd >= 0) (textView.text as? android.text.Spannable)?.let {
            Selection.setSelection(it, selectionStart.coerceAtMost(it.length), selectionEnd.coerceAtMost(it.length))
        }
    }

    private fun showShellOutputSizeSettings() {
        val sizes = listOf(10f, 12f, 14f, 16f, 18f, 20f, 24f)
        AlertDialog.Builder(this).setTitle("Output base size (new blocks)")
            .setItems(sizes.map { "${it.toInt()}sp" }.toTypedArray()) { _, index ->
                val baseSize = sizes[index]
                val minima = sizes.filter { it <= baseSize }
                AlertDialog.Builder(this).setTitle("Minimum output size")
                    .setItems(minima.map { "${it.toInt()}sp" }.toTypedArray()) { _, minimum ->
                        shellOutputSizeSp = baseSize
                        shellMinimumSizeSp = minima[minimum]
                        renderShell()
                    }.show()
            }.show()
    }

    private fun shellText(text: CharSequence, color: Int): TextView {
        return TextView(this).apply {
            this.text = if (text.isEmpty()) SHELL_BLANK_LINE else text
            setTextColor(color)
            setBackgroundColor(Color.BLACK)
            typeface = Typeface.MONOSPACE
            textSize = SHELL_TEXT_SIZE_SP
            textDirection = View.TEXT_DIRECTION_LTR
            includeFontPadding = false
            minHeight = 0
            minimumHeight = 0
            setLineSpacing(0f, 1.0f)
            setPadding(0, 0, 0, 0)
            excludeFromContentCapture()
        }
    }

    private fun createShellInput(): EditText {
        val input = ShellInputEditText(this).apply {
            onEdit = { scrollShellToTail() }
            onHelp = { prefix -> appendShellLines(AgentShellParser.contextHelp(prefix)) }
            onComplete = { completeShellInput() }
            onRecall = { direction -> recallShellInput(direction) }
            onInterrupt = { if (shellBusy) shellOwner.cancel() else text.clear() }
            onEscape = { hideShellInputIme() }
            onSubmit = { submitShellInput(text.toString()) }
            isSaveEnabled = false
            hint = "Shell command"
            importantForAutofill = View.IMPORTANT_FOR_AUTOFILL_NO_EXCLUDE_DESCENDANTS
            setTextColor(AgentLogStyle.TEXT_COLOR)
            setHintTextColor(Color.DKGRAY)
            setBackgroundColor(Color.BLACK)
            typeface = Typeface.MONOSPACE
            textSize = SHELL_TEXT_SIZE_SP
            includeFontPadding = false
            minHeight = 0
            minimumHeight = 0
            setLineSpacing(0f, 1.0f)
            setPadding(0, 0, 0, 0)
            setSingleLine(true)
            inputType = InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS
            imeOptions = EditorInfo.IME_ACTION_GO or EditorInfo.IME_FLAG_NO_EXTRACT_UI or EditorInfo.IME_FLAG_NO_PERSONALIZED_LEARNING or EditorInfo.IME_FLAG_FORCE_ASCII
            imeHintLocales = LocaleList(Locale.ENGLISH)
            useBlockCursor()
            excludeFromContentCapture()
            setOnEditorActionListener { view, actionId, event ->
                if (event?.keyCode == KeyEvent.KEYCODE_ENTER) {
                    if (event.action == KeyEvent.ACTION_UP && !event.isCanceled) submitShellInput(view.text.toString())
                    true
                } else if (actionId == EditorInfo.IME_ACTION_GO || actionId == EditorInfo.IME_ACTION_DONE) {
                    submitShellInput(view.text.toString())
                    true
                } else {
                    false
                }
            }
        }
        shellInput = input
        return input
    }

    /** Extra keys stay above the system IME instead of scrolling away with output. */
    private fun shellKeyboard(): HorizontalScrollView {
        val input = checkNotNull(shellInput) as ShellInputEditText
        val keyWidth = (48 * 0.8f * resources.displayMetrics.density).roundToInt()
        val keyHeight = (48 * 0.78f * resources.displayMetrics.density).roundToInt()
        // Measured from the connected Gboard dark theme; Android exposes no cross-IME theme API.
        val keyColor = Color.rgb(49, 43, 42)
        val surfaceColor = Color.rgb(28, 25, 24)
        val textColor = Color.rgb(238, 227, 226)
        var keyClickSequence = 0
        fun press(code: Int, modifiers: Int = 0) {
            input.requestFocus()
            val time = android.os.SystemClock.uptimeMillis()
            for (action in listOf(KeyEvent.ACTION_DOWN, KeyEvent.ACTION_UP)) {
                input.dispatchKeyEvent(KeyEvent(time, time, action, code, 0, modifiers))
            }
        }
        fun insert(value: String) {
            input.requestFocus()
            input.clearComposingText()
            val from = minOf(input.selectionStart, input.selectionEnd).coerceAtLeast(0)
            val to = maxOf(input.selectionStart, input.selectionEnd).coerceAtLeast(from)
            input.text.replace(from, to, value)
            input.setSelection(from + value.length)
        }
        fun key(label: String, description: String, button: android.widget.Button = android.widget.Button(this), action: () -> Unit) = button.apply {
            text = label
            contentDescription = description
            tooltipText = description
            isAllCaps = false
            maxLines = 1
            typeface = Typeface.SANS_SERIF
            textSize = 16f
            letterSpacing = 0f
            setTextColor(textColor)
            backgroundTintList = null
            background = RippleDrawable(android.content.res.ColorStateList.valueOf(Color.argb(48, 238, 227, 226)), GradientDrawable().apply {
                setColor(keyColor)
                cornerRadius = dp(10).toFloat()
            }, null)
            stateListAnimator = null
            elevation = 0f
            minWidth = keyWidth; minimumWidth = keyWidth
            minHeight = keyHeight; minimumHeight = keyHeight
            setPadding(dp(6), 0, dp(6), 0)
            setOnClickListener { keyClickSequence++; action() }
        }
        fun shortcut(text: String) = key(text.trim(), "Insert ${text.trim()}") { insert(text) }
        fun command(text: String): android.widget.Button {
            var doubleTap = false
            var insertedDraft: String? = null
            var insertedCursor = -1
            var insertedSequence = -1
            val gestures = GestureDetector(this, object : GestureDetector.SimpleOnGestureListener() {
                override fun onDown(event: MotionEvent) = true
                override fun onDoubleTapEvent(event: MotionEvent): Boolean {
                    if (event.actionMasked == MotionEvent.ACTION_UP) doubleTap = true
                    return false
                }
            })
            val button = object : android.widget.Button(this) {
                override fun dispatchTouchEvent(event: MotionEvent): Boolean {
                    if (event.actionMasked == MotionEvent.ACTION_DOWN) doubleTap = false
                    gestures.onTouchEvent(event)
                    if (event.actionMasked == MotionEvent.ACTION_CANCEL ||
                        event.actionMasked == MotionEvent.ACTION_UP && event.eventTime - event.downTime >= ViewConfiguration.getLongPressTimeout()) {
                        doubleTap = false
                        insertedDraft = null
                    }
                    return super.dispatchTouchEvent(event)
                }
            }
            return key(text, "Insert $text; double tap to execute", button) {
                val execute = doubleTap && insertedSequence == keyClickSequence - 1 &&
                    input.text.toString() == insertedDraft && input.selectionStart == insertedCursor && input.selectionEnd == insertedCursor
                doubleTap = false
                insertedDraft = null
                if (execute) submitShellInput(input.text.toString()) else {
                    insert(text)
                    insertedDraft = input.text.toString()
                    insertedCursor = input.selectionStart
                    insertedSequence = keyClickSequence
                }
            }
        }
        // Expected workflow frequency, not telemetry. Only command chips support double-tap execution.
        val keys = listOf(
            key("Tab", "Complete command") { press(KeyEvent.KEYCODE_TAB) },
            key("↑", "Previous command") { press(KeyEvent.KEYCODE_DPAD_UP) },
            key("↓", "Next command") { press(KeyEvent.KEYCODE_DPAD_DOWN) },
            key("C-c", "Cancel running command; clear unsent input when idle") { press(KeyEvent.KEYCODE_C, KeyEvent.META_CTRL_ON) },
            key("?", "Context help") { input.requestFocus(); input.requestHelp() },
            command("sh wi status"),
            command("req wi scan"),
            command("sh wi status ip"),
            shortcut(" | match "),
            shortcut(" | count "),
            shortcut(" | display json "),
            key("\"", "Insert double quote") { insert("\"") },
            key("|", "Insert pipe") { insert("|") },
            command("sh permissions"),
        )
        return HorizontalScrollView(this).apply {
            isFillViewport = true
            layoutDirection = View.LAYOUT_DIRECTION_LTR
            setBackgroundColor(surfaceColor)
            setPadding(0, dp(4), 0, dp(4))
            excludeFromContentCapture()
            addView(LinearLayout(this@MainActivity).apply {
                orientation = LinearLayout.HORIZONTAL
                isBaselineAligned = false
                gravity = Gravity.CENTER_VERTICAL
                keys.forEach { addView(it, LinearLayout.LayoutParams(LinearLayout.LayoutParams.WRAP_CONTENT, LinearLayout.LayoutParams.WRAP_CONTENT).apply {
                    marginStart = dp(2); marginEnd = dp(2)
                }) }
            })
        }
    }

    private fun completeShellInput() {
        val input = shellInput ?: return
        val cursor = input.selectionStart.coerceAtLeast(0)
        val line = input.text.toString()
        val end = shellCompletionEnd(line, cursor) ?: return
        val choices = AgentShellParser.complete(line.take(cursor))
        val replacements = choices.filterNot { it.endsWith('>') }
        if (replacements.size == 1) {
            input.text.replace(0, end, replacements.single())
            input.setSelection(replacements.single().length)
        } else appendShellLines(AgentShellParser.contextHelp(line.take(cursor)))
    }

    private fun recallShellInput(direction: Int) {
        val input = shellInput ?: return
        if (shellBusy) return
        input.setText(shellSession.recall(input.text.toString(), direction))
        input.setSelection(input.text.length)
    }

    private fun submitShellInput(raw: String) {
        val line = raw.trim()
        if (line.isBlank()) {
            shellInput?.setText("")
            shellInput?.requestFocus()
            return
        }
        if (shellBusy) {
            shellInput?.requestFocus()
            return
        }
        shellInput?.setText("")
        shellSession.remember(raw)
        scrollShellToTail()
        appendShellBlock(AgentPresentationBlock.commandLine("$SHELL_PROMPT$raw", AgentLogStyle.TEXT_COLOR, SHELL_PROMPT.length), true)
        when (val command = AgentShellParser.parse(line)) {
            AgentShellCommand.Noop -> Unit
            is AgentShellCommand.Invalid -> {
                if (command.testProfile.isNotEmpty()) shellSession.recordTest(AgentShellResult(false, listOf(command.message)))
                appendShellLine(command.message, SHELL_ERROR_COLOR)
            }
            else -> {
                val app = applicationContext
                val logger = agentShellLogger()
                val executor by lazy { CommandExecutor(app, logger) }
                val adapter = AgentShellAdapter(shellSession) { executor.execute(it) }
                val inner = if (command is AgentShellCommand.Piped) command.command else command
                val name = when (inner) {
                    is AgentShellCommand.Execute -> inner.request.commandCase.name.lowercase().replace('_', '.')
                    is AgentShellCommand.Check -> "check"
                    is AgentShellCommand.Connect -> "wifi.connect"
                    is AgentShellCommand.ShowWifiEht -> "wifi.eht"
                    else -> "shell.local"
                }
                runShellLinesCommand(name) { adapter.execute(command) }
            }
        }
    }

    private fun shellStatus(): String = listOfNotNull(shellOwner.status(), TerminalLog.persistenceWarning).joinToString("; ")

    private fun runShellLinesCommand(name: String, action: () -> AgentShellResult) {
        if (!shellOwner.start(name, action)) return
        renderShell()
    }

    private fun agentShellLogger(): CommandLogger {
        val app = applicationContext
        return object : CommandLogger {
            override fun log(level: CommandLog.Level, message: String, scope: CommandLogScope) {
                val name = when (level) {
                    CommandLog.Level.LEVEL_DEBUG -> "DEBUG"
                    CommandLog.Level.LEVEL_WARN -> "WARN"
                    CommandLog.Level.LEVEL_ERROR -> "ERROR"
                    else -> "INFO"
                }
                TerminalLog.log(app, name, CommandTerminalLog.agentShell(scope, message))
            }
        }
    }

    private fun appendShellLine(text: CharSequence, color: Int = AgentLogStyle.TEXT_COLOR, focusInput: Boolean = true) {
        appendShellLines(listOf(text), color, focusInput)
    }

    private fun appendShellLines(
        lines: Iterable<CharSequence>,
        color: Int = AgentLogStyle.TEXT_COLOR,
        focusInput: Boolean = true,
    ) {
        appendShellBlock(AgentPresentationBlock.create(lines.map { AgentBlockPart.Text(it.toString()) }, color, shellOutputSizeSp, shellMinimumSizeSp), focusInput)
    }

    private fun appendShellBlock(block: AgentPresentationBlock, focusInput: Boolean) {
        val follow = shouldFollowShellTail()
        val styled = block.withStyle(shellOutputSizeSp, shellMinimumSizeSp)
        if (shellTranscript.fits(styled)) {
            shellTranscript.append(styled)
        } else {
            val values = try { AgentSafeValueFile.write(styled, cacheDir) } catch (failure: java.io.IOException) {
                shellTranscript.append(AgentPresentationBlock.create(listOf(
                    AgentBlockPart.Text("Result FAILED"),
                    AgentBlockPart.Field("Target", "self"),
                    AgentBlockPart.Field("Error", "Full safe result unavailable: ${failure.message}"),
                ), SHELL_ERROR_COLOR, shellOutputSizeSp, shellMinimumSizeSp))
                renderShell()
                return
            }
            val retained = styled.overflowSummary(values)
            shellTranscript.evictValueFileBlocks()
            shellTranscript.append(retained)
        }
        renderShell()
        if (follow && focusInput) shellInput?.requestFocus()
    }

    private fun seedShellTranscript() {
        if (shellTranscriptSeeded) return
        shellTranscriptSeeded = true
        shellTranscript.append(AgentPresentationBlock.create(AgentShellParser.help().map { AgentBlockPart.Text(it) }, AgentLogStyle.TEXT_COLOR))
    }

    private fun shouldFollowShellTail(): Boolean = !::shellScroll.isInitialized ||
        shellAutoScroll.shouldFollowTail(!shellScroll.canScrollVertically(1))

    private fun scrollShellToTail() {
        if (!::shellScroll.isInitialized) return
        shellAutoScroll.resumeFollowingTail()
        shellAutoScroll.markScrollToBottomPending()
        requestShellScroll({
            if (shellAutoScroll.isFollowingTail) (shellContent.height - shellScroll.height).coerceAtLeast(0) else shellScroll.scrollY
        }, replace = true)
    }

    /** Resolve one position after measurement; a tail request overrides stale render anchors. */
    private fun requestShellScroll(target: () -> Int, replace: Boolean = false) {
        if (shellScrollTarget != null) {
            if (replace) shellScrollTarget = target
            return
        }
        shellScrollTarget = target
        val observer = shellScroll.viewTreeObserver
        observer.addOnPreDrawListener(object : ViewTreeObserver.OnPreDrawListener {
            override fun onPreDraw(): Boolean {
                if (shellScroll.visibility == View.VISIBLE && (shellScroll.isLayoutRequested || shellContent.isLayoutRequested)) return false
                if (observer.isAlive) observer.removeOnPreDrawListener(this)
                else shellScroll.viewTreeObserver.takeIf { it.isAlive }?.removeOnPreDrawListener(this)
                val position = checkNotNull(shellScrollTarget)
                shellScrollTarget = null
                shellAutoScroll.finishScrollToBottomPending()
                shellScroll.scrollTo(0, position().coerceAtLeast(0))
                return true
            }
        })
        shellScroll.invalidate()
    }

    private fun updateShellContentPadding() {
        if (!::shellContent.isInitialized) return
        val follow = ::shellScroll.isInitialized && shellVisible && shouldFollowShellTail()
        val sidePadding = dp(SHELL_PANEL_PADDING_DP)
        shellContent.setPadding(
            sidePadding + shellSafeLeftInset,
            dp(SHELL_PANEL_TOP_PADDING_DP) + shellSafeTopInset,
            sidePadding + shellSafeRightInset,
            sidePadding,
        )
        if (::shellScroll.isInitialized) shellScroll.layoutParams = (shellScroll.layoutParams as FrameLayout.LayoutParams).apply {
            bottomMargin = shellBottomInset + shellInputPanelHeight
        }
        if (follow) scrollShellToTail()
    }

    private fun focusShellInput() {
        val input = shellInput ?: return
        scrollShellToTail()
        input.requestFocus()
        input.post {
            if (!shellVisible || shellInput !== input) return@post
            input.requestFocus()
            // The controller applies this when the window gains IME control.
            window.insetsController?.show(WindowInsets.Type.ime())
        }
    }

    private fun hideShellInputIme() {
        window.insetsController?.hide(WindowInsets.Type.ime())
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
        shellInputPanel.visibility = View.VISIBLE
        viewerToggle?.visibility = View.GONE
        renderShell()
        shellScroll.visibility = View.VISIBLE
        scroll.visibility = View.GONE
        scrollShellToTail()
        focusShellInput()
        resetIdleDimTimer()
    }

    private fun showInitialScreen(intent: Intent?) {
        if (intent?.action == ACTION_OPEN_LOG_VIEWER) {
            showViewer()
        } else {
            showShell()
        }
    }

    private fun showViewer() {
        if (!shellVisible) {
            if (autoScroll.isFollowingTail) requestScrollToBottom()
            resetIdleDimTimer()
            return
        }
        shellVisible = false
        shellInputPanel.visibility = View.GONE
        viewerToggle?.visibility = View.VISIBLE
        hideShellInputIme()
        shellInput?.clearFocus()
        root.requestFocus()
        shellScroll.visibility = View.GONE
        scroll.visibility = View.VISIBLE
        if (autoScroll.isFollowingTail) requestScrollToBottom()
        resetIdleDimTimer()
    }

    private fun View.excludeFromContentCapture() {
        importantForContentCapture = View.IMPORTANT_FOR_CONTENT_CAPTURE_NO_EXCLUDE_DESCENDANTS
    }

    private fun hideSystemBars() {
        window.attributes = window.attributes.apply {
            layoutInDisplayCutoutMode = WindowManager.LayoutParams.LAYOUT_IN_DISPLAY_CUTOUT_MODE_SHORT_EDGES
        }
        if (Build.VERSION.SDK_INT < 35) {
            @Suppress("DEPRECATION")
            window.setDecorFitsSystemWindows(false)
        }

        val decorView = window.decorView
        decorView.windowInsetsController?.let { controller ->
            controller.hide(WindowInsets.Type.systemBars())
            controller.systemBarsBehavior = WindowInsetsController.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE
        }
    }

    private data class ShellDisplayState(
        val history: AgentBlockHistory,
        val baseSizeSp: Float,
        val minimumSizeSp: Float,
        val scrollY: Int,
        val owner: ShellActionOwner,
        val session: AgentShellSession,
        val followTail: Boolean,
    )
}

/** A bounded tail is enough to catch up without rereading the whole log. */
internal fun terminalCatchUp(last: String?, snapshot: String): List<String> {
    if (last == null || snapshot.isEmpty()) return emptyList()
    val lines = snapshot.lineSequence().toList()
    val marker = lines.indexOfLast { it == last }
    return (if (marker < 0) listOf("[older stopped log lines omitted]") else emptyList()) + lines.drop(marker + 1)
}

private class ShellInputEditText(context: Context) : EditText(context) {
    var onEdit: (() -> Unit)? = null
    var onHelp: ((String) -> Unit)? = null
    var onComplete: (() -> Unit)? = null
    var onRecall: ((Int) -> Unit)? = null
    var onInterrupt: (() -> Unit)? = null
    var onEscape: (() -> Unit)? = null
    var onSubmit: (() -> Unit)? = null
    private var submitOnEnterUp = false

    fun requestHelp(): Boolean {
        if (selectionStart != selectionEnd || android.view.inputmethod.BaseInputConnection.getComposingSpanStart(text) >= 0) return false
        val prefix = text.toString().take(selectionStart.coerceAtLeast(0))
        if (!shellHelpPosition(prefix)) return false
        onHelp?.invoke(prefix) ?: return false
        return true
    }
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
            override fun commitText(value: CharSequence?, newCursorPosition: Int): Boolean {
                if ((value?.toString() == "?" || value?.toString() == "？") && requestHelp()) return true
                return super.commitText(value, newCursorPosition)
            }
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

    override fun onKeyPreIme(keyCode: Int, event: KeyEvent): Boolean {
        if (keyCode == KeyEvent.KEYCODE_C && event.hasModifiers(KeyEvent.META_CTRL_ON)) {
            if (event.action == KeyEvent.ACTION_DOWN && event.repeatCount == 0) onInterrupt?.invoke()
            return true
        }
        if (keyCode == KeyEvent.KEYCODE_ENTER) {
            // A composing IME owns the entire Enter gesture, including its key-up.
            if (event.action == KeyEvent.ACTION_DOWN && event.repeatCount == 0) {
                submitOnEnterUp = android.view.inputmethod.BaseInputConnection.getComposingSpanStart(text) < 0
            }
            if (submitOnEnterUp) {
                if (event.action == KeyEvent.ACTION_UP) {
                    submitOnEnterUp = false
                    if (!event.isCanceled) onSubmit?.invoke()
                }
                return true
            }
        }
        return super.onKeyPreIme(keyCode, event)
    }

    override fun onKeyDown(keyCode: Int, event: KeyEvent): Boolean {
        if ((event.unicodeChar == '?'.code || event.unicodeChar == '？'.code) && requestHelp()) return true
        when (keyCode) {
            KeyEvent.KEYCODE_TAB -> { onComplete?.invoke(); return true }
            KeyEvent.KEYCODE_DPAD_UP -> { onRecall?.invoke(-1); return true }
            KeyEvent.KEYCODE_DPAD_DOWN -> { onRecall?.invoke(1); return true }
            KeyEvent.KEYCODE_ESCAPE -> { onEscape?.invoke(); return true }
            KeyEvent.KEYCODE_C -> if (event.hasModifiers(KeyEvent.META_CTRL_ON)) {
                if (event.repeatCount == 0) onInterrupt?.invoke()
                return true
            }
            KeyEvent.KEYCODE_P -> if (event.hasModifiers(KeyEvent.META_CTRL_ON)) { onRecall?.invoke(-1); return true }
            KeyEvent.KEYCODE_N -> if (event.hasModifiers(KeyEvent.META_CTRL_ON)) { onRecall?.invoke(1); return true }
        }
        if (keyCode == KeyEvent.KEYCODE_DEL) return deleteBackwards()
        return super.onKeyDown(keyCode, event)
    }

    override fun onKeyUp(keyCode: Int, event: KeyEvent): Boolean {
        if (keyCode == KeyEvent.KEYCODE_DEL || keyCode == KeyEvent.KEYCODE_ESCAPE ||
            (keyCode == KeyEvent.KEYCODE_C && event.hasModifiers(KeyEvent.META_CTRL_ON))) return true
        return super.onKeyUp(keyCode, event)
    }

    override fun onFocusChanged(focused: Boolean, direction: Int, previouslyFocusedRect: android.graphics.Rect?) {
        super.onFocusChanged(focused, direction, previouslyFocusedRect)
        if (!focused) submitOnEnterUp = false
        restartCursorBlink()
    }

    override fun onSelectionChanged(selStart: Int, selEnd: Int) {
        super.onSelectionChanged(selStart, selEnd)
        if (hasFocus()) onEdit?.invoke()
        restartCursorBlink()
    }

    override fun onTextChanged(text: CharSequence?, start: Int, lengthBefore: Int, lengthAfter: Int) {
        super.onTextChanged(text, start, lengthBefore, lengthAfter)
        onEdit?.invoke()
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
