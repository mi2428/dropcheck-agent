package io.dropcheck.agent

import android.app.Activity
import android.app.Application
import android.app.Instrumentation
import android.accessibilityservice.AccessibilityServiceInfo
import android.content.Intent
import android.content.pm.ActivityInfo
import android.content.res.Configuration
import android.graphics.Rect
import android.graphics.Bitmap
import android.os.Bundle
import android.os.SystemClock
import android.view.InputDevice
import android.view.KeyCharacterMap
import android.view.KeyEvent
import android.view.MotionEvent
import android.view.View
import android.view.ViewGroup
import android.view.ViewConfiguration
import android.view.WindowInsets
import android.view.accessibility.AccessibilityNodeInfo
import android.view.accessibility.AccessibilityWindowInfo
import android.view.inputmethod.BaseInputConnection
import android.view.inputmethod.InputMethodManager
import android.widget.Button
import android.widget.EditText
import android.widget.HorizontalScrollView
import android.widget.ScrollView
import android.widget.LinearLayout
import android.widget.TextView
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference
import java.io.File
import kotlin.math.roundToInt

/** Attached-window QA with injected input and local-only actions; no acquisition commands. */
class ShellKeyboardQaInstrumentation : Instrumentation() {
    private var options = Bundle()
    private val releases = mutableListOf<CountDownLatch>()

    override fun onCreate(arguments: Bundle?) {
        super.onCreate(arguments)
        options = arguments ?: Bundle()
        start()
    }

    override fun onStart() {
        val application = targetContext.applicationContext as Application
        val current = AtomicReference<MainActivity>()
        val activities = mutableListOf<MainActivity>()
        val lines = mutableListOf<String>()
        var failed = false
        var originalOrientation = ActivityInfo.SCREEN_ORIENTATION_UNSPECIFIED
        var originalWidth = ViewGroup.LayoutParams.MATCH_PARENT
        var originalHeight = ViewGroup.LayoutParams.MATCH_PARENT
        val accessibilityFlags = uiAutomation.serviceInfo.flags
        uiAutomation.serviceInfo = uiAutomation.serviceInfo.apply { flags = flags or AccessibilityServiceInfo.FLAG_RETRIEVE_INTERACTIVE_WINDOWS }
        val callbacks = object : Application.ActivityLifecycleCallbacks {
            override fun onActivityCreated(activity: Activity, state: Bundle?) { if (activity is MainActivity) activities += activity }
            override fun onActivityResumed(activity: Activity) { if (activity is MainActivity && activity in activities) current.set(activity) }
            override fun onActivityStarted(activity: Activity) = Unit
            override fun onActivityPaused(activity: Activity) = Unit
            override fun onActivityStopped(activity: Activity) = Unit
            override fun onActivitySaveInstanceState(activity: Activity, state: Bundle) = Unit
            override fun onActivityDestroyed(activity: Activity) = Unit
        }
        val allCases: List<Pair<String, (MainActivity) -> Unit>> = listOf(
            "startup-input" to ::startupInput,
            "geometry" to ::geometry,
            "ime-tail-follow" to ::imeTailFollow,
            "touch-editing" to ::touchEditing,
            "result-context" to ::resultContext,
            "ime-enter-confirmation" to ::imeEnterConfirmation,
            "raw-enter-canceled" to ::rawEnterCanceled,
            "cancel-repeat" to ::cancelRepeat,
            "busy-up-composition" to { main: MainActivity -> busyComposition(main, "↑") },
            "busy-cancel-composition" to { main: MainActivity -> busyComposition(main, "Ctrl-C") },
            "escape-editing" to ::escapeEditing,
            "escape-interrupt" to ::escapeInterrupt,
            "escape-completion" to ::escapeCompletion,
        )
        val cases = allCases.filter { options.getString("case").let { selected -> selected == null || selected == it.first } }
        try {
            check(cases.isNotEmpty()) { "unknown test case" }
            val orientation = when (options.getString("orientation")) {
                null -> null
                "portrait" -> ActivityInfo.SCREEN_ORIENTATION_PORTRAIT
                "landscape" -> ActivityInfo.SCREEN_ORIENTATION_LANDSCAPE
                else -> error("orientation must be portrait or landscape")
            }
            val widthDp = options.getString("widthDp")?.let { value ->
                checkNotNull(value.toIntOrNull()?.takeIf { it > 0 }) { "widthDp must be a positive integer" }
            }
            onMain {
                application.registerActivityLifecycleCallbacks(callbacks)
            }
            val first = startActivitySync(Intent(targetContext, MainActivity::class.java)
                .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_MULTIPLE_TASK)) as MainActivity
            current.compareAndSet(null, first)
            onMain {
                val main = checkNotNull(current.get())
                originalOrientation = main.requestedOrientation
                originalWidth = main.window.attributes.width
                originalHeight = main.window.attributes.height
                if (orientation != null) main.requestedOrientation = orientation
            }
            await("Activity orientation/window focus did not settle") {
                onMain {
                    current.get()?.let { main ->
                        val expected = if (orientation == ActivityInfo.SCREEN_ORIENTATION_LANDSCAPE) Configuration.ORIENTATION_LANDSCAPE else Configuration.ORIENTATION_PORTRAIT
                        !main.isDestroyed && main.hasWindowFocus() && (orientation == null || main.resources.configuration.orientation == expected)
                    } == true
                }
            }
            onMain {
                val main = checkNotNull(current.get())
                if (options.getString("rtl") == "true") main.field<View>("root").layoutDirection = View.LAYOUT_DIRECTION_RTL
                if (widthDp != null) {
                    val pixels = (widthDp.toDouble() * main.resources.displayMetrics.density).roundToInt()
                    check(pixels in 1..main.windowManager.currentWindowMetrics.bounds.width()) { "widthDp exceeds the current window" }
                    main.window.setLayout(pixels, ViewGroup.LayoutParams.MATCH_PARENT)
                }
            }
            if (widthDp != null) await("requested local window width was not applied") {
                onMain {
                    val main = checkNotNull(current.get())
                    main.field<View>("root").width == (widthDp.toDouble() * main.resources.displayMetrics.density).roundToInt()
                }
            }
            for ((name, test) in cases) {
                val result = runCatching {
                    val main = checkNotNull(current.get())
                    if (name != "startup-input") prepare(main)
                    test(main)
                }
                if (result.isFailure) failed = true
                lines += if (result.isSuccess) "PASS: $name: all assertions satisfied" else "FAIL: $name: ${reason(checkNotNull(result.exceptionOrNull()))}"
            }
            lines += onMain {
                val main = checkNotNull(current.get())
                "Environment: fontScale=${main.resources.configuration.fontScale} orientation=${main.resources.configuration.orientation} rootWidth=${main.field<View>("root").width}"
            }
        } catch (failure: Throwable) {
            failed = true
            if (cases.isEmpty()) lines += "FAIL: setup: ${reason(failure)}"
            for ((name, _) in cases.drop(lines.size)) lines += "FAIL: $name: setup: ${reason(failure)}"
        } finally {
            releases.forEach { it.countDown() }
            val cleanup = runCatching {
                onMain {
                    try {
                        activities.asReversed().filterNot { it.isDestroyed }.forEach { main ->
                            main.field<ShellActionOwner>("shellOwner").cancel()
                            main.window.insetsController?.hide(WindowInsets.Type.ime())
                            main.window.setLayout(originalWidth, originalHeight)
                            main.requestedOrientation = originalOrientation
                            main.finish()
                        }
                    } finally {
                        application.unregisterActivityLifecycleCallbacks(callbacks)
                    }
                }
            }
            if (cleanup.isFailure) {
                failed = true
                lines += "FAIL: cleanup: ${reason(checkNotNull(cleanup.exceptionOrNull()))}"
            }
            uiAutomation.serviceInfo = uiAutomation.serviceInfo.apply { flags = accessibilityFlags }
        }
        lines += "Synthetic data and local-only actions; no device acquisition or global settings changes."
        finish(if (failed) Activity.RESULT_CANCELED else Activity.RESULT_OK, Bundle().apply { putString("stream", lines.joinToString("\n", postfix = "\n")) })
    }

    private fun startupInput(main: MainActivity) {
        await("startup did not show the IME") { onMain { imeVisible(main) && imeBottom(main) > 0 } }
        settle(main)
        onMain {
            val editor = input(main)
            check(editor.hasFocus() && visibleBounds(editor) == bounds(editor)) { "startup command input is offscreen or unfocused" }
            check(bounds(editor).bottom <= bounds(main.field<View>("shellKeyBar")).top + 1) { "startup input overlaps the extra keys" }
        }
    }

    private fun prepare(main: MainActivity) {
        await("previous synthetic action remained busy") { !main.field<ShellActionOwner>("shellOwner").busy }
        onMain {
            check(main.hasWindowFocus()) { "Activity window is not focused" }
            input(main).apply { setText(""); requestFocus() }
            checkNotNull(main.getSystemService(InputMethodManager::class.java)).restartInput(input(main))
            main.window.insetsController?.show(WindowInsets.Type.ime())
            checkNotNull(main.getSystemService(InputMethodManager::class.java)) { "input method service unavailable" }.showSoftInput(input(main), 0)
        }
        await("docked IME did not become visible") { onMain { imeVisible(main) && imeBottom(main) > 0 } }
        onMain { main.field<ScrollView>("shellScroll").apply { scrollTo(0, getChildAt(0).bottom) } }
        settle(main)
    }

    private fun imeTailFollow(main: MainActivity) {
        val scroll = main.field<ScrollView>("shellScroll")
        val append = main.javaClass.getDeclaredMethod("appendShellBlock", AgentPresentationBlock::class.java, Boolean::class.javaPrimitiveType!!)
            .apply { isAccessible = true }
        val marker = AgentPresentationBlock.create(listOf(AgentBlockPart.Text("synthetic IME tail marker")), AgentLogStyle.TEXT_COLOR)
        onMain { append.invoke(main, marker, false) }
        settle(main)
        for (following in listOf(true, false)) {
            tap(main, "Esc")
            await("Esc did not hide IME before tail check") { onMain { !imeVisible(main) } }
            settle(main)
            onMain { scroll.scrollTo(0, if (following) scroll.getChildAt(0).bottom else 0) }
            settle(main)
            val before = onMain { scroll.scrollY }
            val editor = onMain { bounds(input(main)) }
            injectTap(editor.exactCenterX() to editor.exactCenterY())
            await("editor tap did not reopen IME") { onMain { imeVisible(main) } }
            settle(main)
            if (options.getString("capture") == "true") capture(main, if (following) "ime-tail" else "ime-history")
            onMain {
                if (following) {
                    check(!scroll.canScrollVertically(1)) { "IME reopening lost tail following" }
                    val views = main.field<Map<AgentPresentationBlock, LinearLayout>>("shellBlockViews")
                    val retained = views.keys.single { it.fullText == marker.fullText }
                    check(bounds(views.getValue(retained)).bottom <= bounds(main.field<View>("shellInputPanel")).top + 1) { "tail marker is hidden behind input controls" }
                } else check(scroll.scrollY == before) { "IME reopening moved the reader's history position" }
                val next = AgentPresentationBlock.create(listOf(AgentBlockPart.Text("synthetic incoming result")), AgentLogStyle.TEXT_COLOR)
                append.invoke(main, next, false)
            }
            settle(main)
            onMain {
                check(if (following) !scroll.canScrollVertically(1) else scroll.scrollY == before) { "result delivery changed tail/history following" }
            }
        }
    }

    private fun geometry(main: MainActivity) {
        onMain {
            val editor = input(main)
            val bar = main.field<HorizontalScrollView>("shellKeyBar")
            val editorRect = bounds(editor)
            val barRect = bounds(bar)
            check(imeVisible(main) && imeBottom(main) > 0) { "geometry requires a visible docked IME" }
            check(editorRect.bottom <= barRect.top + 1) { "input overlaps key bar: inputBottom=${editorRect.bottom}, barTop=${barRect.top}" }
            check(barRect.bottom <= imeTop(main) + 1) { "key bar overlaps IME: barBottom=${barRect.bottom}, imeTop=${imeTop(main)}" }
            check(visibleBounds(editor) == editorRect && editorRect.height() > 0) { "input is clipped or not visible" }
            check(visibleBounds(bar) == barRect) { "key bar is clipped" }
            check(bounds(main.field<View>("shellInputPanel")).top - bounds(main.field<View>("root")).top >= 2 * editor.lineHeight) { "input controls leave less than two text lines for results" }
        }
        val labels = mapOf(
            "Esc" to "Hide keyboard", "Tab" to "Complete command", "Ctrl-C" to "Cancel current command or clear input",
            "?" to "Context help", "↑" to "Previous command", "\"" to "Insert double quote",
            "/" to "Insert slash", "-" to "Insert hyphen", "|" to "Insert pipe",
            "←" to "Move cursor left", "↓" to "Next command", "→" to "Move cursor right",
        )
        onMain { check(keys(main).map { it.text.toString() } == labels.keys.toList()) { "key labels/order changed" } }
        for ((label, description) in labels) {
            reveal(main, label)
            onMain {
                val button = key(main, label)
                val minimum = (48 * main.resources.displayMetrics.density).toInt()
                check(button.width >= minimum && button.height >= minimum) { "key smaller than 48dp: $label" }
                check(visibleBounds(button) == bounds(button)) { "key cannot be fully reached by horizontal scrolling: $label" }
                check(button.isEnabled && button.isClickable && button.contentDescription?.toString() == description) { "key has no enabled action/expected accessibility name: $label" }
                val layout = checkNotNull(button.layout) { "key label has no layout: $label" }
                val availableWidth = button.width - button.compoundPaddingLeft - button.compoundPaddingRight
                check(layout.height <= button.height - button.compoundPaddingTop - button.compoundPaddingBottom) { "key label is vertically clipped: $label" }
                for (line in 0 until layout.lineCount) check(layout.getEllipsisCount(line) == 0 && layout.getLineWidth(line) <= availableWidth + 1) { "key label is clipped: $label" }
            }
        }
        if (options.getString("capture") == "true") capture(main, "geometry")
    }

    private fun touchEditing(main: MainActivity) {
        onMain { input(main).apply { setText("sho"); setSelection(3) } }
        tap(main, "Tab")
        onMain { checkEdit(main, "show", 4) }
        for (cluster in listOf("\uD83D\uDE00", "e\u0301", "\uD83D\uDC69\u200D\uD83D\uDD2C")) {
            onMain { input(main).apply { setText("A${cluster}B"); setSelection(1 + cluster.length) } }
            tap(main, "←")
            onMain { checkEdit(main, "A${cluster}B", 1) }
            tap(main, "→")
            onMain { checkEdit(main, "A${cluster}B", 1 + cluster.length) }
        }
        onMain { input(main).apply { setText("ab"); setSelection(0, 1) } }
        tap(main, "/")
        onMain { checkEdit(main, "/b", 1) }
    }

    private fun compose(main: MainActivity): List<Any> {
        onMain {
            input(main).apply {
                setText("next "); setSelection(5)
            }
            checkNotNull(main.getSystemService(InputMethodManager::class.java)).restartInput(input(main))
        }
        settle(main)
        tapImeKey(checkNotNull(options.getString("composingKey")) { "composingKey is required for real IME composition" })
        await("real IME did not create a composing range") { onMain { BaseInputConnection.getComposingSpanStart(input(main).text) >= 5 } }
        return onMain { editState(input(main)) }
    }

    private fun imeEnterConfirmation(main: MainActivity) {
        val composing = compose(main)
        val before = onMain { main.field<AgentBlockHistory>("shellTranscript").blocks.size }
        hardwareKey(main, KeyEvent.KEYCODE_ENTER)
        await("physical Enter did not confirm IME composition") { onMain { BaseInputConnection.getComposingSpanStart(input(main).text) < 0 } }
        onMain {
            check(input(main).text.toString() == composing[0] && main.field<AgentBlockHistory>("shellTranscript").blocks.size == before) { "physical Enter submitted an unconfirmed IME composition" }
        }
    }

    private fun busyComposition(main: MainActivity, label: String) = withAction(main) { _, interrupted ->
        val before = compose(main)
        tap(main, label)
        if (label == "Ctrl-C") {
            check(interrupted.await(3, TimeUnit.SECONDS)) { "Ctrl-C did not interrupt the synthetic action" }
            await("canceled action remained busy") { !main.field<ShellActionOwner>("shellOwner").busy }
        } else check(main.field<ShellActionOwner>("shellOwner").busy) { "history key ended the synthetic action" }
        onMain { check(editState(input(main)) == before) { "$label changed the next draft, selection, or composing span" } }
    }

    private fun rawEnterCanceled(main: MainActivity) {
        val before = onMain { main.field<AgentBlockHistory>("shellTranscript").blocks.size }
        onMain {
            val editor = input(main).apply { setText("synthetic canceled draft"); setSelection(text.length) }
            val time = SystemClock.uptimeMillis()
            editor.dispatchKeyEvent(KeyEvent(time, time, KeyEvent.ACTION_DOWN, KeyEvent.KEYCODE_ENTER, 0))
            editor.dispatchKeyEvent(KeyEvent(time, time, KeyEvent.ACTION_UP, KeyEvent.KEYCODE_ENTER, 0, 0,
                KeyCharacterMap.VIRTUAL_KEYBOARD, 0, KeyEvent.FLAG_CANCELED))
        }
        repeat(10) {
            onMain {
                check(input(main).text.toString() == "synthetic canceled draft" && main.field<AgentBlockHistory>("shellTranscript").blocks.size == before) { "canceled raw Enter submitted a command" }
            }
            Thread.sleep(50)
        }
        onMain {
            val editor = input(main)
            editor.dispatchKeyEvent(KeyEvent(KeyEvent.ACTION_DOWN, KeyEvent.KEYCODE_ENTER))
            editor.dispatchKeyEvent(KeyEvent(KeyEvent.ACTION_UP, KeyEvent.KEYCODE_ENTER))
            check(editor.text.isEmpty() && main.field<AgentBlockHistory>("shellTranscript").blocks.size == before + 2) { "normal raw Enter control did not submit exactly once" }
        }
    }

    private fun cancelRepeat(main: MainActivity) = withAction(main) { _, interrupted ->
        onMain { input(main).apply { setText("retained next draft"); setSelection(text.length) } }
        val down = SystemClock.uptimeMillis()
        fun event(action: Int, repeat: Int) = KeyEvent(down, SystemClock.uptimeMillis(), action, KeyEvent.KEYCODE_C,
            repeat, KeyEvent.META_CTRL_ON, KeyCharacterMap.VIRTUAL_KEYBOARD, 0, 0, InputDevice.SOURCE_KEYBOARD)
        sendHardwareEvent(main, event(KeyEvent.ACTION_DOWN, 0))
        check(interrupted.await(3, TimeUnit.SECONDS)) { "initial Ctrl-C did not interrupt" }
        await("initial cancellation remained busy") { !main.field<ShellActionOwner>("shellOwner").busy }
        sendHardwareEvent(main, event(KeyEvent.ACTION_DOWN, 1))
        sendHardwareEvent(main, event(KeyEvent.ACTION_UP, 0))
        onMain { check(input(main).text.toString() == "retained next draft") { "Ctrl-C repeat erased the next draft after cancellation" } }
    }

    private fun resultContext(main: MainActivity) {
        for (overflow in listOf(false, true)) {
            val source = AgentPresentationBlock.create(listOf(
                AgentBlockPart.Text("synthetic inspection result"),
                AgentBlockPart.Field("QA marker", "retained synthetic full value"),
                AgentBlockPart.Field("Source", "android"),
            ), AgentLogStyle.TEXT_COLOR)
            val file = if (overflow) AgentSafeValueFile.write(source, targetContext.cacheDir) else null
            var selectedText: TextView? = null
            try {
                onMain {
                    main.javaClass.getDeclaredMethod("appendShellBlock", AgentPresentationBlock::class.java, Boolean::class.javaPrimitiveType!!)
                        .apply { isAccessible = true }.invoke(main, file?.let(source::overflowSummary) ?: source, false)
                }
                waitForIdleSync()
                onMain { main.field<ScrollView>("shellScroll").apply { scrollTo(0, getChildAt(0).bottom) } }
                settle(main)
                val point = onMain {
                    val block = main.field<AgentBlockHistory>("shellTranscript").blocks.last()
                    val container = main.field<Map<AgentPresentationBlock, LinearLayout>>("shellBlockViews").getValue(block)
                    check(container.childCount == 1) { "permanent result control row remains" }
                    val text = container.getChildAt(0) as TextView
                    selectedText = text
                    check(text.isTextSelectable) { "native text selection was lost" }
                    val origin = bounds(text)
                    val visible = visibleBounds(text).apply { bottom = minOf(bottom, bounds(main.field<View>("shellInputPanel")).top) }
                    // A large-font landscape viewport may show only the end of the result.
                    (0 until text.layout.lineCount).asSequence().mapNotNull { line ->
                        val start = text.layout.getLineStart(line)
                        val end = text.layout.getLineEnd(line)
                        val offset = (start until end).firstOrNull { !text.text[it].isWhitespace() } ?: return@mapNotNull null
                        val x = origin.left + text.totalPaddingLeft + text.layout.getPrimaryHorizontal(offset) + text.paint.measureText(text.text.subSequence(offset, offset + 1).toString()) / 2
                        val y = origin.top + text.totalPaddingTop + text.layout.getLineBaseline(line) - text.paint.textSize / 3
                        (x to y).takeIf { visible.contains(x.toInt(), y.toInt()) }
                    }.firstOrNull() ?: error("result has no visible inspection word")
                }
                injectTap(point, ViewConfiguration.getLongPressTimeout().toLong() + 100)
                try {
                    await("selection context menu did not open") { findText("Full result") != null || findMore() != null }
                } catch (failure: IllegalStateException) {
                    if (options.getString("capture") == "true") capture(main, "context-failure")
                    val state = onMain { checkNotNull(selectedText).let { "clickable=${it.isClickable} longClickable=${it.isLongClickable} focused=${it.hasFocus()} selection=${it.selectionStart}:${it.selectionEnd}" } }
                    error("selection context menu did not open: $state")
                }
                if (findText("Full result") == null) {
                    tapNode(checkNotNull(findMore()))
                    await("Full result is not reachable from the selection menu") { findText("Full result") != null }
                }
                tapNode(checkNotNull(findText("Full result")))
                if (overflow) {
                    await("full-value index is unavailable") { findText("Safe values ", false) != null }
                    val label = checkNotNull(file).labels(0).single { it.contains("QA marker") }
                    val entry = checkNotNull(findText(label))
                    val tapped = Rect().also(entry::getBoundsInScreen)
                    tapNode(entry)
                    try {
                        await("retained full safe value is unavailable") { findText("retained synthetic full value") != null }
                    } catch (failure: IllegalStateException) {
                        if (options.getString("capture") == "true") capture(main, "value-failure")
                        val remaining = findText(label)?.let { Rect().also(it::getBoundsInScreen) }
                        error("retained full safe value is unavailable: tapped=$tapped remaining=$remaining index=${findText("Safe values ", false) != null} detail=${findText("Full safe value ", false) != null}")
                    }
                } else await("full result dialog is unavailable") { findText("Full safe result") != null }
            } finally {
                if (onMain { !main.hasWindowFocus() }) {
                    sendKeyDownUpSync(KeyEvent.KEYCODE_BACK)
                    await("inspection dialog did not close") { onMain { main.hasWindowFocus() } }
                }
                onMain { input(main).requestFocus() }
                file?.delete()
            }
        }
    }

    private fun findText(value: String, exact: Boolean = true): AccessibilityNodeInfo? = findNode { node ->
        val text = node.text?.toString().orEmpty()
        if (exact) text == value else text.contains(value)
    }

    private fun findMore(): AccessibilityNodeInfo? = findNode {
        it.viewIdResourceName?.endsWith("/overflow") == true || it.contentDescription?.toString() in listOf("More options", "その他のオプション")
    }

    private fun findNode(matches: (AccessibilityNodeInfo) -> Boolean): AccessibilityNodeInfo? {
        fun find(node: AccessibilityNodeInfo): AccessibilityNodeInfo? {
            if (node.packageName?.toString() in listOf("io.dropcheck.agent", "android", "com.android.systemui") && matches(node)) return node
            for (index in 0 until node.childCount) node.getChild(index)?.let { child -> find(child)?.let { return it } }
            return null
        }
        for (window in uiAutomation.windows) window.root?.let { find(it)?.let { return it } }
        return null
    }

    private fun capture(main: MainActivity, phase: String) {
        val area = onMain { bounds(main.field("root")) }
        val image = checkNotNull(uiAutomation.takeScreenshot()) { "screenshot unavailable" }
        try {
            val cropped = Bitmap.createBitmap(image, area.left, area.top, area.width(), area.height())
            try {
                val config = main.resources.configuration
                File(targetContext.cacheDir, "keyboard-qa-$phase-${config.orientation}-${config.fontScale}-${area.width()}.png").outputStream().use {
                    check(cropped.compress(Bitmap.CompressFormat.PNG, 100, it)) { "screenshot encoding failed" }
                }
            } finally { if (cropped !== image) cropped.recycle() }
        } finally { image.recycle() }
    }

    private fun escapeEditing(main: MainActivity) {
        onMain { input(main).apply { setText("draft"); setSelection(5) } }
        tap(main, "Esc")
        await("Esc did not hide the real IME") { onMain { !imeVisible(main) } }
        // Navigation/deletion are independent of the user's physical-key character map.
        hardwareKey(main, KeyEvent.KEYCODE_DPAD_LEFT)
        hardwareKey(main, KeyEvent.KEYCODE_DEL)
        await("physical editing/focus did not survive Esc") { onMain { input(main).let { it.hasFocus() && it.text.toString() == "drat" && it.selectionStart == 3 && it.selectionEnd == 3 } } }
        tap(main, "?")
        settle(main)
        onMain {
            check(!imeVisible(main)) { "help reopened the IME after Esc" }
            check(input(main).hasFocus()) { "help moved focus to ${main.currentFocus?.javaClass?.simpleName}" }
        }
        hardwareKey(main, KeyEvent.KEYCODE_C, KeyEvent.META_CTRL_ON)
        onMain {
            check(!imeVisible(main)) { "idle Ctrl-C reopened the IME after Esc" }
            check(input(main).text.isEmpty()) { "idle hardware Ctrl-C did not clear the draft; focus=${main.currentFocus?.javaClass?.simpleName}" }
        }
        hardwareKey(main, KeyEvent.KEYCODE_ENTER)
        repeat(5) {
            onMain {
                check(!imeVisible(main)) { "empty Enter reopened the IME after Esc" }
                check(input(main).hasFocus()) { "empty Enter moved focus to ${main.currentFocus?.javaClass?.simpleName}" }
            }
            Thread.sleep(50)
        }
        val before = onMain { main.field<AgentBlockHistory>("shellTranscript").blocks.size }
        onMain { input(main).apply { setText("show cli"); setSelection(text.length) } }
        hardwareKey(main, KeyEvent.KEYCODE_ENTER)
        await("local hardware Enter did not finish") { !main.field<ShellActionOwner>("shellOwner").busy }
        onMain {
            check(main.field<AgentBlockHistory>("shellTranscript").blocks.size == before + 2) { "hardware Enter submitted more than once or lost its local result" }
            check(input(main).text.isEmpty() && !imeVisible(main)) { "hardware Enter lost input state or reopened the IME" }
        }
    }

    private fun escapeInterrupt(main: MainActivity) = withAction(main) { _, interrupted ->
        onMain { input(main).apply { setText("next draft"); setSelection(10) } }
        tap(main, "Esc")
        await("Esc did not hide the real IME") { onMain { !imeVisible(main) } }
        hardwareKey(main, KeyEvent.KEYCODE_C, KeyEvent.META_CTRL_ON)
        check(interrupted.await(3, TimeUnit.SECONDS)) { "physical Ctrl-C after Esc did not interrupt the action" }
        await("physical cancellation remained busy") { !main.field<ShellActionOwner>("shellOwner").busy }
        onMain { checkEdit(main, "next draft", 10) }
    }

    private fun escapeCompletion(main: MainActivity) = withAction(main) { release, _ ->
        tap(main, "Esc")
        await("Esc did not hide the real IME") { onMain { !imeVisible(main) } }
        // fullScroll() can request focus and would mask the Esc regression.
        onMain { main.field<ScrollView>("shellScroll").apply { scrollTo(0, getChildAt(0).bottom) } }
        settle(main)
        onMain { check(!main.field<ScrollView>("shellScroll").canScrollVertically(1)) { "completion precondition: not following the transcript tail" } }
        release.countDown()
        await("completed action remained busy") { !main.field<ShellActionOwner>("shellOwner").busy }
        repeat(25) {
            onMain { check(!imeVisible(main)) { "late completion reopened the IME after Esc" } }
            Thread.sleep(50)
        }
    }

    private fun withAction(main: MainActivity, test: (CountDownLatch, CountDownLatch) -> Unit) {
        val release = CountDownLatch(1).also { releases += it }
        val started = CountDownLatch(1)
        val interrupted = CountDownLatch(1)
        val owner = main.field<ShellActionOwner>("shellOwner")
        try {
            onMain {
                check(!owner.busy) { "synthetic owner already busy" }
                val action: () -> AgentShellResult = {
                    started.countDown()
                    try {
                        check(release.await(10, TimeUnit.SECONDS)) { "synthetic action timed out" }
                        AgentShellResult(true, listOf("synthetic keyboard QA result"))
                    } catch (failure: InterruptedException) {
                        interrupted.countDown()
                        throw failure
                    }
                }
                main.javaClass.getDeclaredMethod("runShellLinesCommand", String::class.java, Function0::class.java)
                    .apply { isAccessible = true }.invoke(main, "synthetic.keyboard.qa", action)
            }
            check(started.await(3, TimeUnit.SECONDS)) { "synthetic action did not start" }
            test(release, interrupted)
        } finally {
            release.countDown()
            owner.cancel()
            await("synthetic action cleanup remained busy") { !owner.busy }
        }
    }

    private fun reveal(main: MainActivity, label: String) {
        onMain { key(main, label).apply { requestRectangleOnScreen(Rect(0, 0, width, height), true) } }
        settle(main)
    }

    private fun tap(main: MainActivity, label: String) {
        reveal(main, label)
        val point = onMain {
            check(main.hasWindowFocus()) { "refusing touch outside the focused Activity" }
            val rect = bounds(key(main, label))
            check(visibleBounds(key(main, label)) == rect && rect.width() > 0 && rect.height() > 0) { "touch key is clipped: $label" }
            check(!imeVisible(main) || rect.bottom <= imeTop(main) + 1) { "touch key is behind the IME: $label" }
            rect.exactCenterX() to rect.exactCenterY()
        }
        injectTap(point)
        onMain { Unit }
    }

    private fun tapImeKey(label: String) {
        val root = checkNotNull(uiAutomation.windows.firstOrNull { it.type == AccessibilityWindowInfo.TYPE_INPUT_METHOD }?.root) { "IME accessibility window unavailable" }
        fun find(node: AccessibilityNodeInfo): AccessibilityNodeInfo? {
            if (node.text?.toString() == label || node.contentDescription?.toString() == label) return node
            for (index in 0 until node.childCount) node.getChild(index)?.let { child -> find(child)?.let { return it } }
            return null
        }
        val node = checkNotNull(find(root)) { "requested composing key is not exposed by the active IME" }
        val rect = Rect().also(node::getBoundsInScreen)
        check(rect.width() > 0 && rect.height() > 0) { "IME key is not visible" }
        injectTap(rect.exactCenterX() to rect.exactCenterY())
    }

    private fun tapNode(node: AccessibilityNodeInfo) {
        var rect = Rect()
        var stable = 0
        await("context action did not become visible and stationary") {
            val current = Rect()
            val visible = node.refresh() && node.isVisibleToUser
            node.getBoundsInScreen(current)
            stable = if (visible && current == rect && !current.isEmpty) stable + 1 else 0
            rect = current
            stable >= 3
        }
        injectTap(rect.exactCenterX() to rect.exactCenterY())
        onMain { Unit }
    }

    private fun injectTap(point: Pair<Float, Float>, holdMs: Long = 30) {
        val down = SystemClock.uptimeMillis()
        for (action in listOf(MotionEvent.ACTION_DOWN, MotionEvent.ACTION_UP)) {
            val event = MotionEvent.obtain(down, SystemClock.uptimeMillis(), action, point.first, point.second, 0).apply { source = InputDevice.SOURCE_TOUCHSCREEN }
            try { check(uiAutomation.injectInputEvent(event, true)) { "touch injection failed" } } finally { event.recycle() }
            if (action == MotionEvent.ACTION_DOWN) Thread.sleep(holdMs)
        }
    }

    private fun hardwareKey(main: MainActivity, code: Int, modifiers: Int = 0) {
        val down = SystemClock.uptimeMillis()
        for (action in listOf(KeyEvent.ACTION_DOWN, KeyEvent.ACTION_UP)) {
            sendHardwareEvent(main, KeyEvent(down, SystemClock.uptimeMillis(), action, code, 0, modifiers,
                KeyCharacterMap.VIRTUAL_KEYBOARD, 0, 0, InputDevice.SOURCE_KEYBOARD))
        }
    }

    private fun sendHardwareEvent(main: MainActivity, event: KeyEvent) {
        onMain { check(main.hasWindowFocus()) { "refusing key injection outside the focused Activity" } }
        check(uiAutomation.injectInputEvent(event, true)) { "hardware-key injection failed" }
        onMain { Unit }
    }

    private fun settle(main: MainActivity) {
        var previous: List<Any>? = null
        var stable = 0
        await("keyboard/input layout did not settle") {
            val sample = onMain {
                val scroll = main.field<ScrollView>("shellScroll")
                listOf(bounds(input(main)), bounds(main.field<View>("shellKeyBar")), imeBottom(main), scroll.scrollY, scroll.getChildAt(0).height)
            }
            stable = if (sample == previous) stable + 1 else 0
            previous = sample
            stable >= 3
        }
    }

    private fun checkEdit(main: MainActivity, expected: String, cursor: Int) {
        check(input(main).let { it.text.toString() == expected && it.selectionStart == cursor && it.selectionEnd == cursor && it.hasFocus() }) { "touch/physical editing changed text, cursor, or focus unexpectedly" }
    }

    private fun editState(editor: EditText): List<Any> = listOf(editor.text.toString(), BaseInputConnection.getComposingSpanStart(editor.text), BaseInputConnection.getComposingSpanEnd(editor.text), editor.selectionStart, editor.selectionEnd)
    private fun input(main: MainActivity): EditText = main.field("shellInput")
    private fun imeVisible(main: MainActivity): Boolean = main.window.decorView.rootWindowInsets?.isVisible(WindowInsets.Type.ime()) == true
    private fun imeBottom(main: MainActivity): Int = checkNotNull(main.window.decorView.rootWindowInsets).getInsets(WindowInsets.Type.ime()).bottom
    private fun imeTop(main: MainActivity): Int = main.windowManager.currentWindowMetrics.bounds.bottom - imeBottom(main)
    private fun key(main: MainActivity, label: String): Button = keys(main).single { it.text.toString() == label }
    private fun keys(main: MainActivity): List<Button> {
        val rows = main.field<HorizontalScrollView>("shellKeyBar").getChildAt(0) as ViewGroup
        return (0 until rows.childCount).flatMap { index -> (rows.getChildAt(index) as ViewGroup).let { row -> (0 until row.childCount).map { row.getChildAt(it) as Button } } }
    }

    private fun bounds(view: View): Rect = IntArray(2).let { point -> view.getLocationOnScreen(point); Rect(point[0], point[1], point[0] + view.width, point[1] + view.height) }
    private fun visibleBounds(view: View): Rect {
        val rect = Rect()
        check(view.getLocalVisibleRect(rect)) { "view has no visible region" }
        val point = IntArray(2)
        view.getLocationOnScreen(point)
        return rect.apply { offset(point[0], point[1]) }
    }

    private fun await(message: String, condition: () -> Boolean) {
        val deadline = SystemClock.uptimeMillis() + 4_000
        do {
            if (condition()) return
            Thread.sleep(50)
        } while (SystemClock.uptimeMillis() < deadline)
        error(message)
    }

    private fun <T> onMain(action: () -> T): T {
        var result: Result<T>? = null
        runOnMainSync { result = runCatching(action) }
        return checkNotNull(result).getOrThrow()
    }

    private fun reason(failure: Throwable): String = "${failure.javaClass.simpleName}: ${failure.message.orEmpty().replace('\n', ' ').take(180)}"
    @Suppress("UNCHECKED_CAST")
    private fun <T> MainActivity.field(name: String): T = javaClass.getDeclaredField(name).apply { isAccessible = true }.get(this) as T
}
