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
import android.text.Selection
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
import java.util.concurrent.Executors
import java.util.IdentityHashMap

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
private const val SHELL_PROMPT = "dropcheck# "
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
    private var shellInput: EditText? = null
    private var shellInputRowView: LinearLayout? = null
    private var shellImeBottomInset = 0
    private var shellSafeLeftInset = 0
    private var shellSafeTopInset = 0
    private var shellSafeRightInset = 0
    private var statusIconViews: List<ImageView> = emptyList()
    private var controllerHeartbeatConnected = false
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

    private val terminalLogListener: (String) -> Unit = { line ->
        runOnUiThread { append(line) }
    }
    private val statusListener: () -> Unit = {
        runOnUiThread { syncStatusIcons() }
    }
    private val shellExecutor = Executors.newSingleThreadExecutor()
    private var shellTranscript = AgentBlockHistory()
    private val shellBlockViews = IdentityHashMap<AgentPresentationBlock, LinearLayout>()
    private var shellContentWidthPx = 0
    private var shellOutputSizeSp = SHELL_TEXT_SIZE_SP
    private var shellMinimumSizeSp = 10f
    private var shellTranscriptSeeded = false

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        if (lastNonConfigurationInstance == null) AgentSafeValueFile.clearAbandoned(cacheDir)
        (lastNonConfigurationInstance as? ShellDisplayState)?.let {
            shellTranscript = it.history
            shellTranscriptSeeded = true
            shellOutputSizeSp = it.baseSizeSp
            shellMinimumSizeSp = it.minimumSizeSp
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
                if (event.actionMasked == MotionEvent.ACTION_UP && maybeHandleSwipe(event, SwipeDirection.RIGHT)) {
                    return@setOnTouchListener true
                }
                if (event.actionMasked == MotionEvent.ACTION_UP && isTapGesture(event)) {
                    view.performClick()
                }
                false
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
            val cutoutInsets = insets.getInsets(WindowInsets.Type.displayCutout())
            val systemBarInsets = insets.getInsets(WindowInsets.Type.systemBars())
            val safeLeft = maxOf(cutoutInsets.left, systemBarInsets.left)
            val safeTop = cutoutInsets.top
            val safeRight = maxOf(cutoutInsets.right, systemBarInsets.right)
            if (
                shellImeBottomInset != imeBottom ||
                shellSafeLeftInset != safeLeft ||
                shellSafeTopInset != safeTop ||
                shellSafeRightInset != safeRight
            ) {
                shellImeBottomInset = imeBottom
                shellSafeLeftInset = safeLeft
                shellSafeTopInset = safeTop
                shellSafeRightInset = safeRight
                updateShellContentPadding()
                if (shellVisible && shellInput?.hasFocus() == true && !shellScroll.canScrollVertically(1)) scrollShellToInput()
            }
            insets
        }
        setContentView(root)
        controllerHeartbeatConnected = ControllerSessionRuntimeState.heartbeatConnected()
        updateStatusIcons()
        resetIdleDimTimer()
        showInitialScreen(intent)
        (lastNonConfigurationInstance as? ShellDisplayState)?.let { state ->
            shellScroll.post { shellScroll.scrollTo(0, state.scrollY) }
        }
    }

    @Suppress("DEPRECATION")
    override fun onRetainNonConfigurationInstance(): Any = ShellDisplayState(
        shellTranscript, shellOutputSizeSp, shellMinimumSizeSp, if (::shellScroll.isInitialized) shellScroll.scrollY else 0,
    )

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        showInitialScreen(intent)
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
        appendLogLine(line)
    }

    private fun appendLogLine(line: String) {
        val displayLine = boundedLine(line)
        val terminalLine = terminalDisplayText(displayLine)
        logView.append(colored(displayLine, terminalLine))
        displayLineLengths.addLast(terminalLine.length)
        displayLogChars += terminalLine.length
        trimDisplayIfNeeded()
        requestScrollToBottom()
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
            addOnLayoutChangeListener { view, _, _, _, _, _, _, _, _ ->
                val width = (view.width - view.paddingLeft - view.paddingRight).coerceAtLeast(0)
                if (width != shellContentWidthPx) {
                    shellContentWidthPx = width
                    view.post { renderShell() }
                }
            }
        }
        updateShellContentPadding()
        renderShell()
        return ScrollView(this).apply {
            setBackgroundColor(Color.BLACK)
            isFillViewport = true
            addView(shellContent, FrameLayout.LayoutParams(
                FrameLayout.LayoutParams.MATCH_PARENT,
                FrameLayout.LayoutParams.WRAP_CONTENT,
            ))
            setOnTouchListener { view, event ->
                captureSwipeStart(event)
                if (event.actionMasked == MotionEvent.ACTION_UP) {
                    if (maybeHandleSwipe(event, SwipeDirection.LEFT)) {
                        return@setOnTouchListener true
                    }
                    if (isTapGesture(event)) {
                        view.performClick()
                        handleShellTap(event)
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
        val inputFocused = shellInput?.hasFocus() == true
        val inputSelectionStart = shellInput?.selectionStart ?: -1
        val inputSelectionEnd = shellInput?.selectionEnd ?: -1
        val focusedBlock = shellBlockViews.values.firstOrNull { (it.getChildAt(0) as TextView).hasFocus() }
        val anchor = if (::shellScroll.isInitialized) shellScroll.scrollY else 0
        val anchoredView = if (::shellScroll.isInitialized) shellTranscript.blocks.mapNotNull { shellBlockViews[it] }.firstOrNull { it.bottom > anchor } else null
        val anchorOffset = anchoredView?.let { anchor - it.top }
        shellContent.removeAllViews()
        fun addShellView(view: View) {
            shellContent.addView(view)
        }
        addShellView(shellText("Agent Shell", AgentLogStyle.TEXT_COLOR))
        val controllerState = if (ControllerSessionRuntimeState.heartbeatConnected()) "connected" else "idle"
        val defaults = AgentShellUseDefaultsStore(applicationContext).load()
        addShellView(shellText("surface=agent-shell controller=$controllerState ${AgentShellUsePolicy.statusText(defaults)}", AgentLogStyle.TEXT_COLOR))
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
        // Keep the same input View attached during acquisition and reflow.
        // submitShellInput already rejects concurrent execution.
        addShellView(ensureShellInputRow())
        if (inputFocused) {
            shellInput?.requestFocus()
            val length = shellInput?.text?.length ?: 0
            if (inputSelectionStart >= 0 && inputSelectionEnd >= 0) shellInput?.setSelection(inputSelectionStart.coerceAtMost(length), inputSelectionEnd.coerceAtMost(length))
        }
        focusedBlock?.takeIf { it.parent === shellContent }?.getChildAt(0)?.requestFocus()
        if (::shellScroll.isInitialized) shellScroll.post {
            val newAnchor = anchoredView?.takeIf { it.parent === shellContent }?.let { it.top + (anchorOffset ?: 0) } ?: anchor
            shellScroll.scrollTo(0, newAnchor.coerceAtLeast(0))
        }
    }

    private fun shellBlockView(block: AgentPresentationBlock): LinearLayout = LinearLayout(this).apply {
        orientation = LinearLayout.VERTICAL
        addView(shellText("", block.color).apply { setTextIsSelectable(true) })
        addView(android.widget.Button(this@MainActivity).apply {
            text = "Copy full result"
            textSize = SHELL_TEXT_SIZE_SP
            minHeight = dp(48)
            contentDescription = "Copy full safe result"
            if (block.safeValueFile != null) {
                text = "Inspect/copy full safe values"
                contentDescription = "Inspect and copy every full safe result value"
            }
            setOnClickListener {
                if (block.safeValueFile != null) showSafeValuePage(block.safeValueFile, 0) else {
                    getSystemService(ClipboardManager::class.java).setPrimaryClip(ClipData.newPlainText("Dropcheck result", block.fullText))
                }
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
            setLineSpacing(0f, 1.0f)
            setPadding(0, 0, 0, 0)
            setSingleLine(true)
            inputType = InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS
            imeOptions = EditorInfo.IME_ACTION_GO or EditorInfo.IME_FLAG_NO_EXTRACT_UI
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
            layoutParams = LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT,
                LinearLayout.LayoutParams.WRAP_CONTENT,
            )
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
        appendShellLine(shellCommandLine(redactAgentShellCommandLine(line)))
        when (val command = AgentShellParser.parse(line)) {
            AgentShellCommand.Noop -> Unit
            is AgentShellCommand.Help -> {
                appendShellLines(AgentShellParser.help(command.topic))
            }
            is AgentShellCommand.SetDefaultPassphrase -> {
                AgentShellUseDefaultsStore(applicationContext).setDefaultPassphrase(command.passphrase)
                appendShellLine(AgentShellUsePolicy.setDefaultPassphraseMessage(command.passphrase))
            }
            AgentShellCommand.ClearDefaultPassphrase -> {
                AgentShellUseDefaultsStore(applicationContext).setDefaultPassphrase("")
                appendShellLine(AgentShellUsePolicy.setDefaultPassphraseMessage(""))
            }
            AgentShellCommand.ShowVersion -> {
                appendShellLine("version ${BuildConfig.VERSION_NAME}")
            }
            is AgentShellCommand.Execute, is AgentShellCommand.ShowWifiEht, is AgentShellCommand.Use,
            is AgentShellCommand.Check, AgentShellCommand.ShowChecks, is AgentShellCommand.ShowCheckLast ->
                runShellLinesCommand {
                    AgentShellAdapter(applicationContext, agentShellLogger()).execute(command)
                }
            is AgentShellCommand.Invalid -> appendShellLine(command.message, SHELL_ERROR_COLOR)
        }
    }

    private fun runShellLinesCommand(action: () -> AgentShellResult) {
        shellBusy = true
        renderShell()
        shellExecutor.submit {
            val result = runCatching { action() }.getOrElse { AgentShellResult(false, listOf("command failed (execution error)")) }
            runOnUiThread {
                shellBusy = false
                if (result.block != null) appendShellBlock(result.block, true) else appendShellLines(
                    result.lines, if (result.ok) AgentLogStyle.TEXT_COLOR else SHELL_ERROR_COLOR,
                )
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
        val follow = !::shellScroll.isInitialized || !shellScroll.canScrollVertically(1)
        val styled = AgentPresentationBlock.create(block.parts, block.color, shellOutputSizeSp, shellMinimumSizeSp)
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
            val important = styled.parts.filterIndexed { index, part ->
                index == 0 || when (part) {
                    is AgentBlockPart.Text -> part.value.startsWith("Status:") || part.value.startsWith("Source:") || part.value.startsWith("Error:") || part.value.startsWith("Reason:")
                    is AgentBlockPart.Field -> part.label in listOf("Source", "Target", "Error", "Reason", "Note")
                    else -> false
                }
            }.take(64).map { part ->
                when (part) {
                    is AgentBlockPart.Text -> if (part.value.length > 2048) AgentBlockPart.Text("${part.value.take(2048)}\nDisplay note truncated; full safe value is in Inspect/copy full safe values") else part
                    is AgentBlockPart.Field -> if (part.value.length > 2048) AgentBlockPart.Field(part.label, "${part.value.take(2048)} (display note abbreviated; full safe value in Inspect/copy)") else part
                    else -> part
                }
            }
            val retained = AgentPresentationBlock.create(important + AgentBlockPart.Text(
                "shown=0; ${styled.fullText.length} characters exceed in-memory history. All ${values.valueCount} safe values remain in Inspect/copy full safe values; no identity was clipped.",
            ), styled.color, shellOutputSizeSp, shellMinimumSizeSp, safeValueFile = values)
            shellTranscript.evictValueFileBlocks()
            shellTranscript.append(retained)
        }
        renderShell()
        if (follow) {
            scrollShellToInput()
            if (focusInput) focusShellInput()
        }
    }

    private fun seedShellTranscript() {
        if (shellTranscriptSeeded) return
        shellTranscriptSeeded = true
        shellTranscript.append(AgentPresentationBlock.create(AgentShellParser.help().map { AgentBlockPart.Text(it) }, AgentLogStyle.TEXT_COLOR))
    }

    private fun scrollShellToInput() {
        shellScroll.post {
            val inputRow = shellInputRowView ?: return@post
            val fullViewportHeight = shellScroll.height - shellScroll.paddingTop - shellScroll.paddingBottom
            if (fullViewportHeight <= 0) return@post
            val visibleViewportHeight = (fullViewportHeight - shellImeBottomInset).coerceAtLeast(dp(48))
            val viewportTop = shellScroll.scrollY
            val viewportBottom = viewportTop + visibleViewportHeight
            val inputBottom = inputRow.bottom + dp(SHELL_PANEL_PADDING_DP)
            val targetScrollY = when {
                inputBottom > viewportBottom -> inputBottom - visibleViewportHeight
                inputRow.top < viewportTop -> inputRow.top
                else -> viewportTop
            }
            val maxScrollY = (shellContent.height - fullViewportHeight).coerceAtLeast(0)
            val clampedScrollY = targetScrollY.coerceIn(0, maxScrollY)
            if (clampedScrollY != viewportTop) {
                shellScroll.smoothScrollTo(0, clampedScrollY)
            }
        }
    }

    private fun updateShellContentPadding() {
        if (!::shellContent.isInitialized) return
        val sidePadding = dp(SHELL_PANEL_PADDING_DP)
        shellContent.setPadding(
            sidePadding + shellSafeLeftInset,
            dp(SHELL_PANEL_TOP_PADDING_DP) + shellSafeTopInset,
            sidePadding + shellSafeRightInset,
            sidePadding + shellImeBottomInset,
        )
    }

    private fun focusShellInput(forceIme: Boolean = false) {
        val input = shellInput ?: return
        input.requestFocus()
        input.post {
            if (!shellVisible || shellInput !== input) return@post
            input.requestFocus()
            if (forceIme) {
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
        window.insetsController?.hide(WindowInsets.Type.ime())
        input.clearFocus()
        root.requestFocus()
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

    private fun handleShellTap(event: MotionEvent) {
        if (!shellBusy && isShellKeyboardTapArea(event)) {
            focusShellInput(forceIme = true)
        }
    }

    private fun isShellKeyboardTapArea(event: MotionEvent): Boolean {
        val height = shellScroll.height.takeIf { it > 0 } ?: root.height
        return height <= 0 || event.y >= height * 2f / 3f
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

    private fun showInitialScreen(intent: Intent?) {
        if (intent?.action == ACTION_OPEN_LOG_VIEWER) {
            showViewer()
        } else {
            showShell()
        }
    }

    private fun showViewer() {
        if (!shellVisible) {
            requestScrollToBottom()
            resetIdleDimTimer()
            return
        }
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

    private data class ShellDisplayState(
        val history: AgentBlockHistory,
        val baseSizeSp: Float,
        val minimumSizeSp: Float,
        val scrollY: Int,
    )
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
