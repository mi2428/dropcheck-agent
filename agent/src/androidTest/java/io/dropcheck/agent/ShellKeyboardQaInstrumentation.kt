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
import android.view.inputmethod.InputMethodManager
import android.view.inputmethod.EditorInfo
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

/** Attached-window QA with injected input, read-only snapshots and local-only actions. */
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
            "vendor-hierarchy" to ::vendorHierarchy,
            "repeat-command-guards" to ::repeatCommandGuards,
            "repeat-command" to ::repeatCommand,
            "tap-policy" to ::tapPolicy,
            "ime-visible-tap" to ::imeVisibleTap,
            "scrollback-ime" to ::scrollbackIme,
            "prompt" to ::prompt,
            "english-ime" to ::englishIme,
            "keybar-scroll" to ::keybarScroll,
            "ime-tail-follow" to ::imeTailFollow,
            "command-tail" to ::commandTail,
            "burst-tail" to ::burstTail,
            "async-tail" to ::asyncTail,
            "reflow-tail" to ::reflowTail,
            "gesture-tail" to ::gestureTail,
            "touch-editing" to ::touchEditing,
            "shortcut-input" to ::shortcutInput,
            "shortcut-double-tap" to ::shortcutDoubleTap,
            "shortcut-double-tap-guards" to ::shortcutDoubleTapGuards,
            "shortcut-double-tap-busy" to ::shortcutDoubleTapBusy,
            "result-context" to ::resultContext,
            "raw-enter-canceled" to ::rawEnterCanceled,
            "cancel-repeat" to ::cancelRepeat,
            "ctrl-c-key" to ::ctrlCKey,
            "escape-editing" to ::escapeEditing,
            "escape-interrupt" to ::escapeInterrupt,
            "escape-completion" to ::escapeCompletion,
        )
        val cases = allCases.filter { options.getString("case").let { selected -> selected == null || it.first in selected.split(',') } }
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
                sendStatus(0, Bundle().apply { putString("phase", name) })
                val result = runCatching {
                    val main = checkNotNull(current.get())
                    if (name != "startup-input") prepare(main)
                    test(main)
                }
                if (result.isFailure && options.getString("capture") == "true") runCatching { capture(checkNotNull(current.get()), "failure-$name") }
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
        lines += if (options.getString("ouiLive") == "true") "Explicit public IEEE database download/update; no fresh scans, connectivity changes, diagnostic probes or global settings changes."
            else "Synthetic data and read-only snapshots; no fresh scans, connectivity changes, external probes or global settings changes."
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

    private fun vendorHierarchy(main: MainActivity) {
        check(OuiDatabase.parseIEEE("Organization\n00-11-22 (hex) Café Labs\n001122 (base 16) Café\u00a0\u0085Labs\n".toByteArray(), 6)["001122"] == "Café Labs") { "native OUI whitespace normalization differs from JVM/Go" }
        val bundled = OuiDatabase(null)
        check(bundled.info().assignments > 50000 && bundled.vendor("e8:0a:b9:00:00:01").contains("Cisco")) { "native bundled OUI Java resource unavailable" }
        val adapter = AgentShellAdapter { error("OUI commands must not dispatch an Android acquisition") }
        if (options.getString("ouiLive") == "true") {
            val updated = adapter.execute(AgentShellParser.parse("request oui update"))
            check(updated.ok) { "official OUI update failed: ${updated.block?.fullText ?: updated.lines}" }
        }
        val info = adapter.execute(AgentShellParser.parse("show oui"))
        check(info.ok && info.block?.fullText?.contains("IEEE MA-L/MA-M/MA-S/IAB") == true) { "local OUI status unavailable" }
        check(OuiDatabase.current.info().assignments > 50000) { "bundled OUI resource missing or incomplete" }
        val scan = io.dropcheck.agent.grpc.WifiScan.newBuilder()
            .setConnections(io.dropcheck.agent.grpc.WifiScanConnections.newBuilder().setObservedAtUnixMs(1234).setComplete(true).addBssids("e8:0a:b9:00:00:01"))
            .addResults(io.dropcheck.agent.grpc.WifiScanResult.newBuilder().setSsid("hierarchy-qa").setBssid("e8:0a:b9:00:00:01").setRssiDbm(-30)
                .setWifiStandard("802.11be")
                .setSecurityDetails(io.dropcheck.agent.grpc.WifiSecurityDetails.newBuilder().setRsnPresent(true).setRsnVersion(1))
                .addInformationElements(io.dropcheck.agent.grpc.WifiInformationElement.newBuilder().setId(48).setByteCount(2).setBytesHex("0100"))
                .addObservationFields(io.dropcheck.agent.grpc.DiagnosticField.newBuilder().setKey("fixture.state").setValue("available")))
            .addResults(io.dropcheck.agent.grpc.WifiScanResult.newBuilder().setSsid("second-qa").setBssid("02:00:00:00:00:01").setRssiDbm(-40))
            .addResults(io.dropcheck.agent.grpc.WifiScanResult.newBuilder().setSsid("third-qa").setBssid("00:11:22:33:44:55").setWifiStandard("802.11be").setRssiDbm(-50)).build()
        val block = AgentPresentationBlock.create(AgentWifiScanRenderer.presentation(scan, AgentWifiScanContext(detail = true)), AgentLogStyle.TEXT_COLOR)
        check(block.fullText.contains("Cisco") && block.fullText.contains("local MAC")) { "scan lost vendor attribution or local-MAC rejection" }
        onMain { append(main, block) }
        settle(main)
        val text = onMain {
            val retained = main.field<AgentBlockHistory>("shellTranscript").blocks.last()
            main.field<Map<AgentPresentationBlock, LinearLayout>>("shellBlockViews").getValue(retained).getChildAt(0) as TextView
        }
        onMain {
            for (heading in listOf("[*] AP 1", "[ ] AP 2", "[+] AP 3", "  AP Capabilities", "  Information Elements", "  Fields")) {
                check(text.text.contains("\n\n$heading\n") && !text.text.contains("\n\n\n$heading\n")) { "native section needs exactly one blank separator: $heading" }
            }
            check(text.text.contains("AP display: shown=3 total=3")) { "native scan footer lost actual display count" }
            fun x(needle: String): Float {
                val index = text.text.indexOf(needle)
                check(index >= 0) { "native hierarchy label missing: $needle" }
                return text.layout.getPrimaryHorizontal(index)
            }
            check(x("Info") > x("[*] AP 1") + 1 && x("IE 1") > x("Info") + 1) { "native AP/section/field indentation collapsed" }
            val start = text.text.indexOf("[*] AP 1")
            val contextLine = (text.layout.getLineForOffset(start) - 6).coerceAtLeast(0)
            main.field<ScrollView>("shellScroll").scrollTo(0, text.top + (text.parent as View).top + text.layout.getLineTop(contextLine))
        }
        settle(main)
        if (options.getString("capture") == "true") capture(main, "vendor-hierarchy")
        val briefParts = AgentWifiScanRenderer.presentation(scan, AgentWifiScanContext(brief = true))
        check(briefParts.filterIsInstance<AgentBlockPart.Scan>().single().columns.map { it.label } ==
            listOf("SSID", "BSSID", "dBm", "BAND", "CH", "BW", "PHY", "SEC", "AGE(s)")) { "brief scan changed its original table columns" }
        val brief = AgentPresentationBlock.create(briefParts, AgentLogStyle.TEXT_COLOR)
        check(!brief.fullText.contains("VENDOR") && !brief.fullText.contains("Cisco")) { "brief scan expanded vendor details" }
        onMain { append(main, brief) }
        settle(main)
        onMain {
            val retained = main.field<AgentBlockHistory>("shellTranscript").blocks.last()
            val view = main.field<Map<AgentPresentationBlock, LinearLayout>>("shellBlockViews").getValue(retained)
            val body = view.getChildAt(0) as TextView
            if (main.resources.configuration.fontScale == 0.85f && main.field<View>("root").width >= 1080) {
                check(body.layout.getLineForOffset(body.text.indexOf("SSID")) == body.layout.getLineForOffset(body.text.indexOf("BSSID"))) { "brief scan stopped using the fitting native table" }
            }
            main.field<ScrollView>("shellScroll").scrollTo(0, view.top)
        }
        settle(main)
        if (options.getString("capture") == "true") capture(main, "scan-brief")
    }

    private fun tapPolicy(main: MainActivity) {
        val scroll = main.field<ScrollView>("shellScroll")
        val content = main.field<ViewGroup>("shellContent")
        val draft = "tap policy draft"
        onMain { input(main).apply { setText(draft); setSelection(2, 7) } }
        settle(main)
        fun hideIme() {
            onMain { main.window.insetsController?.hide(WindowInsets.Type.ime()) }
            await("tap policy could not hide IME") { onMain { !imeVisible(main) } }
            settle(main)
        }
        fun verify(busy: Boolean) {
            for (fraction in listOf(0.15f, 0.5f, 0.85f)) {
                hideIme()
                onMain { scroll.scrollTo(0, (content.height - scroll.height).coerceAtLeast(0) / 2) }
                settle(main)
                val before = onMain { Triple(scroll.scrollY, input(main).selectionStart, input(main).selectionEnd) }
                onMain { check(scroll.canScrollVertically(1)) { "tap policy must start in older history" } }
                val point = onMain {
                    val area = bounds(scroll)
                    // Inside the real safe area, just outside child text/control bounds.
                    bounds(content).left + content.paddingLeft - 2f to area.top + area.height() * fraction
                }
                injectTap(point)
                if (fraction >= 2f / 3f) await("lower-third tap did not open IME: busy=$busy") { onMain { imeVisible(main) } }
                settle(main)
                onMain {
                    if (fraction < 2f / 3f) check(!imeVisible(main) && scroll.scrollY == before.first) { "upper background tap moved history or opened IME: busy=$busy fraction=$fraction" }
                    check(input(main).text.toString() == draft && input(main).selectionStart == before.second && input(main).selectionEnd == before.third) { "background tap changed the draft or selection" }
                }
                if (fraction >= 2f / 3f) assertTail(main, "lower-third tap busy=$busy")
            }
            hideIme()
            onMain { scroll.scrollTo(0, (content.height - scroll.height).coerceAtLeast(0) / 2) }
            settle(main)
            val outputPoint = onMain {
                val area = bounds(scroll).apply { top += height() * 2 / 3 }
                main.field<Map<AgentPresentationBlock, LinearLayout>>("shellBlockViews").values.firstNotNullOfOrNull {
                    textPoint(it.getChildAt(0) as TextView, area)
                } ?: error("no visible lower-third output word")
            }
            injectTap(outputPoint)
            await("lower-third text tap did not open IME: busy=$busy") { onMain { imeVisible(main) } }
            settle(main)
            assertTail(main, "lower-third text tap busy=$busy")
            onMain { check(input(main).text.toString() == draft) { "text tap changed the draft" } }
            onMain { scroll.scrollTo(0, content.bottom) }
            settle(main)
            for (rightEdge in listOf(false, true)) {
                hideIme()
                val point = onMain {
                    val editor = bounds(input(main))
                    check(visibleBounds(input(main)) == editor) { "input tap target is not visible" }
                    (if (rightEdge) editor.right - 2f else editor.left + 2f) to editor.exactCenterY()
                }
                injectTap(point)
                await("input tap did not reopen IME: busy=$busy right=$rightEdge") { onMain { imeVisible(main) } }
                settle(main)
                onMain { check(input(main).hasFocus() && input(main).text.toString() == draft) { "input tap changed the draft or lost focus" } }
                assertTail(main, "input tap busy=$busy right=$rightEdge")
            }
            onMain { check(main.field<ShellActionOwner>("shellOwner").busy == busy) { "tap changed action state" } }
        }
        verify(false)
        withAction(main) { _, _ -> verify(true) }
        if (options.getString("capture") == "true") capture(main, "tap-policy")
    }

    private fun repeatCommand(main: MainActivity) {
        submitLocal(main, "sh version | count")
        val command = "  sh cli | count  "
        submitLocal(main, command)
        for (showIme in listOf(false, true)) for (word in listOf(false, true)) {
            val draft = if (!showIme && !word) "sh version | count" else "retained repeat draft"
            onMain { input(main).apply { setText(draft); setSelection(2, 7); requestFocus() } }
            prepareRepeatTap(main, showIme)
            val point = repeatPoint(main, word)
            sendStatus(0, Bundle().apply { putString("phase", "repeat-command ime=$showIme word=$word") })
            val before = onMain { main.field<AgentBlockHistory>("shellTranscript").blocks.count { it.fullText == "self> $command" } }
            injectTap(point)
            onMain { check(main.field<AgentBlockHistory>("shellTranscript").blocks.count { it.fullText == "self> $command" } == before) { "single upper tap repeated a command" } }
            Thread.sleep(70)
            injectTap(point)
            if (showIme && word) { Thread.sleep(70); injectTap(point) }
            await("upper double tap did not repeat exactly once") { onMain {
                !main.field<ShellActionOwner>("shellOwner").busy && main.field<AgentBlockHistory>("shellTranscript").blocks.count { it.fullText == "self> $command" } == before + 1
            } }
            settle(main)
            onMain {
                check(main.field<AgentBlockHistory>("shellTranscript").blocks.count { it.fullText == "self> $command" } == before + 1) { "upper double tap repeated more than once" }
                checkEditSelection(main, draft, 2, 7)
                check(imeVisible(main) == showIme) { "repeat changed IME visibility" }
            }
            assertTail(main, "repeated command")
        }
        if (options.getString("capture") == "true") capture(main, "repeat-command")
        val invalid = "invalid-repeat-qa"
        submitLocal(main, invalid)
        onMain { input(main).apply { setText("retained rejected-command draft"); setSelection(2, 7) } }
        prepareRepeatTap(main, false)
        val point = repeatPoint(main, false)
        injectTap(point); Thread.sleep(70); injectTap(point)
        settle(main)
        onMain {
            val blocks = main.field<AgentBlockHistory>("shellTranscript").blocks
            check(blocks.count { it.fullText == "self> $invalid" } == 2 && blocks.last().fullText.contains("unknown keyword")) { "rejected command did not repeat once" }
            checkEditSelection(main, "retained rejected-command draft", 2, 7)
            check(!imeVisible(main)) { "rejected command replay reopened IME" }
        }
        assertTail(main, "repeated rejected command")
        if (options.getString("capture") == "true") capture(main, "repeat-command-invalid")
    }

    private fun repeatCommandGuards(main: MainActivity) {
        val initial = onMain { main.field<AgentBlockHistory>("shellTranscript").blocks.last() }
        onMain { check(main.field<AgentBlockHistory>("shellTranscript").blocks.none { it.fullText.startsWith("self> ") }) { "empty-history repeat check requires a fresh Shell" } }
        onMain { input(main).apply { setText("retained guard draft"); setSelection(2, 7) } }
        for (showIme in listOf(false, true)) {
            prepareRepeatTap(main, showIme)
            val point = repeatPoint(main, false)
            injectTap(point); Thread.sleep(70); injectTap(point)
            settle(main)
            onMain {
                check(main.field<AgentBlockHistory>("shellTranscript").blocks.last() === initial) { "empty-history gesture submitted input" }
                checkEditSelection(main, "retained guard draft", 2, 7)
                check(imeVisible(main) == showIme) { "empty-history gesture changed IME" }
            }
        }
        submitLocal(main, "sh cli | count")
        onMain { input(main).apply { setText("retained guard draft"); setSelection(2, 7) } }
        val previous = onMain { main.field<AgentBlockHistory>("shellTranscript").blocks.last() }
        for (guard in listOf("slow", "middle", "long", "cancel")) {
            prepareRepeatTap(main, true)
            val point = repeatPoint(main, false, if (guard == "middle") 0.58f else 0.35f)
            injectTap(point)
            Thread.sleep(if (guard == "slow") ViewConfiguration.getDoubleTapTimeout().toLong() + 50 else 70)
            if (guard == "cancel") {
                val down = SystemClock.uptimeMillis()
                for (action in listOf(MotionEvent.ACTION_DOWN, MotionEvent.ACTION_CANCEL)) {
                    val event = MotionEvent.obtain(down, SystemClock.uptimeMillis(), action, point.first, point.second, 0).apply { source = InputDevice.SOURCE_TOUCHSCREEN }
                    try { check(uiAutomation.injectInputEvent(event, true)) } finally { event.recycle() }
                    Thread.sleep(30)
                }
            } else injectTap(point, if (guard == "long") ViewConfiguration.getLongPressTimeout().toLong() + 100 else 30)
            settle(main)
            onMain {
                check(main.field<AgentBlockHistory>("shellTranscript").blocks.last() === previous && !main.field<ShellActionOwner>("shellOwner").busy) { "$guard gesture repeated a command" }
                checkEditSelection(main, "retained guard draft", 2, 7)
            }
        }
        withAction(main) { _, _ ->
            prepareRepeatTap(main, true)
            val point = repeatPoint(main, true)
            injectTap(point); Thread.sleep(70); injectTap(point)
            settle(main)
            onMain {
                check(main.field<ShellActionOwner>("shellOwner").busy && main.field<AgentBlockHistory>("shellTranscript").blocks.last() === previous) { "busy repeat gesture dispatched a command" }
                checkEditSelection(main, "retained guard draft", 2, 7)
            }
        }
    }

    private fun prepareRepeatTap(main: MainActivity, showIme: Boolean) {
        onMain {
            if (showIme) main.window.insetsController?.show(WindowInsets.Type.ime()) else main.window.insetsController?.hide(WindowInsets.Type.ime())
        }
        await("repeat tap IME state did not settle") { onMain { imeVisible(main) == showIme } }
        settle(main)
        onMain { main.field<ScrollView>("shellScroll").apply { scrollTo(0, (getChildAt(0).height - height).coerceAtLeast(0) / 2) } }
        settle(main)
        Thread.sleep(ViewConfiguration.getDoubleTapTimeout().toLong() + 30)
    }

    private fun repeatPoint(main: MainActivity, word: Boolean, fraction: Float = 0.35f): Pair<Float, Float> = onMain {
        val scroll = main.field<ScrollView>("shellScroll")
        val content = main.field<ViewGroup>("shellContent")
        val area = bounds(scroll)
        if (word) {
            area.top += (area.height() * 0.15f).toInt()
            area.bottom = bounds(scroll).top + (scroll.height * 0.49f).toInt()
            main.field<Map<AgentPresentationBlock, LinearLayout>>("shellBlockViews").values.firstNotNullOfOrNull {
                textPoint(it.getChildAt(0) as TextView, area)
            } ?: error("no visible upper-half result word")
        } else bounds(content).left + content.paddingLeft - 2f to area.top + area.height() * fraction
    }

    private fun submitLocal(main: MainActivity, command: String) {
        check(command.trim() in listOf("sh version | count", "sh cli | count", "invalid-repeat-qa")) { "repeat test only submits local commands" }
        val previous = onMain {
            val last = main.field<AgentBlockHistory>("shellTranscript").blocks.last()
            input(main).apply {
                setText(command); setSelection(text.length); requestFocus()
                check(checkNotNull(onCreateInputConnection(EditorInfo())).performEditorAction(EditorInfo.IME_ACTION_GO))
            }
            last
        }
        await("local repeat-test command remained busy") { onMain {
            !main.field<ShellActionOwner>("shellOwner").busy && main.field<AgentBlockHistory>("shellTranscript").blocks.last() !== previous
        } }
        settle(main)
    }

    private fun checkEditSelection(main: MainActivity, expected: String, start: Int, end: Int) {
        check(input(main).let { it.text.toString() == expected && it.selectionStart == start && it.selectionEnd == end && it.hasFocus() }) { "repeat gesture changed draft, selection or focus" }
    }

    private fun prompt(main: MainActivity) {
        try {
            for (command in listOf("  sh cli  | count  ", "set cli default-passphrase 'qa-only password=visible'",
                "invalid-qa password=visible https://user:visible@example.test/?token=visible", "clear cli default-passphrase")) {
                val previous = onMain {
                    val last = main.field<AgentBlockHistory>("shellTranscript").blocks.last()
                    input(main).apply {
                        setText(command); setSelection(text.length); requestFocus()
                        check(checkNotNull(onCreateInputConnection(EditorInfo())).performEditorAction(EditorInfo.IME_ACTION_GO))
                    }
                    last
                }
                await("echo command remained busy") { onMain {
                    !main.field<ShellActionOwner>("shellOwner").busy && main.field<AgentBlockHistory>("shellTranscript").blocks.last() !== previous
                } }
                settle(main)
                onMain {
                    val block = main.field<AgentBlockHistory>("shellTranscript").blocks.single { it.fullText == "self> $command" }
                    val text = main.field<Map<AgentPresentationBlock, LinearLayout>>("shellBlockViews").getValue(block).getChildAt(0) as TextView
                    check(!text.text.contains("<redacted>") && !text.text.contains("<command submitted>")) { "displayed command was masked" }
                    checkPromptColor(text.text)
                    for (width in listOf(text.width, (text.paint.measureText("self>") / 2).toInt().coerceAtLeast(1))) {
                        checkPromptColor(checkNotNull(AgentNativeBlockLayout.render(block.withStyle(16f, 10f), width, text.paint)).text)
                    }
                    android.text.Selection.setSelection(text.text as android.text.Spannable, 1, 3)
                    main.javaClass.getDeclaredMethod("renderShell").apply { isAccessible = true }.invoke(main)
                    checkPromptColor(text.text)
                    check(text.selectionStart == 1 && text.selectionEnd == 3) { "prompt redraw lost selection" }
                    val ordinary = AgentPresentationBlock.create(listOf(AgentBlockPart.Text("self> ordinary result text")), AgentLogStyle.TEXT_COLOR)
                    val resultText = checkNotNull(AgentNativeBlockLayout.render(ordinary, text.width, text.paint)).text as android.text.Spanned
                    check(resultText.getSpans(0, resultText.length, android.text.style.ForegroundColorSpan::class.java).isEmpty()) { "ordinary output was mistaken for a prompt" }
                    check(input(main).text.isEmpty() && input(main).hasFocus()) { "command echo lost its live editor" }
                    val session = main.field<AgentShellSession>("shellSession")
                    check(session.recall("", -1) == command && session.recall(command, 1).isEmpty()) { "recall changed the original command input" }
                }
                assertTail(main, "verbatim command echo")
            }
            if (options.getString("capture") == "true") capture(main, "prompt")
        } finally {
            onMain { main.field<AgentShellSession>("shellSession").setPassphrase("") }
        }
    }

    private fun imeVisibleTap(main: MainActivity) {
        val scroll = main.field<ScrollView>("shellScroll")
        val content = main.field<ViewGroup>("shellContent")
        val draft = "visible IME tap draft"
        onMain {
            append(main, AgentPresentationBlock.create(List(100) { AgentBlockPart.Text("tap scroll QA row $it") }, AgentLogStyle.TEXT_COLOR))
            input(main).apply { setText(draft); setSelection(3, 8) }
        }
        settle(main)
        fun verify(busy: Boolean) {
            for (word in listOf(false, true)) for (fraction in listOf(0.15f, 0.5f, 0.85f)) {
                onMain { input(main).requestFocus(); main.window.insetsController?.show(WindowInsets.Type.ime()) }
                await("tap check requires visible IME") { onMain { imeVisible(main) } }
                settle(main)
                onMain { scroll.scrollTo(0, (content.height - scroll.height) / 2) }
                settle(main)
                val before = onMain { scroll.scrollY }
                val positions = mutableListOf<Int>()
                val observer = android.view.ViewTreeObserver.OnScrollChangedListener { positions += scroll.scrollY }
                val point = onMain {
                    val area = bounds(scroll)
                    if (word) {
                        val target = (area.top + area.height() * fraction).toInt()
                        area.top = target
                        area.bottom = minOf(area.bottom, target + input(main).lineHeight * 3)
                        main.field<Map<AgentPresentationBlock, LinearLayout>>("shellBlockViews").values.firstNotNullOfOrNull {
                            textPoint(it.getChildAt(0) as TextView, area)
                        } ?: error("no visible result word at $fraction")
                    } else bounds(content).left + content.paddingLeft - 2f to area.top + area.height() * fraction
                }
                onMain { scroll.viewTreeObserver.addOnScrollChangedListener(observer) }
                try {
                    injectTap(point)
                    settle(main)
                    repeat(12) {
                        onMain {
                            check(imeVisible(main) && input(main).hasFocus() && kotlin.math.abs(scroll.scrollY - before) <= 2 && positions.all { kotlin.math.abs(it - before) <= 2 }) {
                                "visible-IME tap scrolled: busy=$busy word=$word fraction=$fraction before=$before after=${scroll.scrollY} frames=${positions.distinct()} ime=${imeVisible(main)} inputFocus=${input(main).hasFocus()}"
                            }
                            check(input(main).text.toString() == draft) { "output tap changed input" }
                        }
                        Thread.sleep(50)
                    }
                } finally { onMain { scroll.viewTreeObserver.removeOnScrollChangedListener(observer) } }
            }
        }
        verify(false)
        withAction(main) { _, _ -> verify(true) }
        if (options.getString("capture") == "true") capture(main, "ime-visible-tap")
    }

    private fun checkPromptColor(value: CharSequence) {
        val styled = value as android.text.Spanned
        var promptChars = 0
        for (index in value.indices) {
            if (value[index] == '\n') continue
            val colors = styled.getSpans(index, index + 1, android.text.style.ForegroundColorSpan::class.java)
            if (promptChars < "self> ".length) {
                check(colors.singleOrNull()?.foregroundColor == android.graphics.Color.YELLOW) { "history prompt is not yellow at character $promptChars" }
                promptChars++
            } else check(colors.isEmpty()) { "command text inherited the prompt color" }
        }
        check(promptChars == "self> ".length) { "prompt characters missing" }
    }

    private fun scrollbackIme(main: MainActivity) {
        val scroll = main.field<ScrollView>("shellScroll")
        onMain {
            append(main, AgentPresentationBlock.create(List(160) { AgentBlockPart.Text("scrollback keyboard QA row $it") }, AgentLogStyle.TEXT_COLOR))
            input(main).apply { setText("retained scrollback draft"); setSelection(3, 8) }
        }
        settle(main)
        fun swipe(backward: Boolean) {
            val area = onMain { bounds(scroll) }
            val down = SystemClock.uptimeMillis()
            for (step in 0..8) {
                val action = when (step) { 0 -> MotionEvent.ACTION_DOWN; 8 -> MotionEvent.ACTION_UP; else -> MotionEvent.ACTION_MOVE }
                val fraction = if (backward) 0.15f + 0.65f * step / 8 else 0.8f - 0.65f * step / 8
                val event = MotionEvent.obtain(down, SystemClock.uptimeMillis(), action, area.exactCenterX(),
                    area.top + area.height() * fraction, 0).apply { source = InputDevice.SOURCE_TOUCHSCREEN }
                try { check(uiAutomation.injectInputEvent(event, true)) { "scrollback injection failed" } } finally { event.recycle() }
                Thread.sleep(if (step == 7) 250 else 40)
            }
        }
        fun verify(busy: Boolean) {
            for (fromTail in listOf(false, true)) {
                onMain { input(main).requestFocus(); main.window.insetsController?.show(WindowInsets.Type.ime()) }
                await("scrollback requires visible IME") { onMain { imeVisible(main) } }
                settle(main)
                onMain { scroll.scrollTo(0, if (fromTail) scroll.getChildAt(0).bottom else (scroll.getChildAt(0).height - scroll.height) / 2) }
                settle(main)
                val before = onMain { scroll.scrollY }
                swipe(backward = true)
                await("scrollback did not hide IME: busy=$busy tail=$fromTail") { onMain { !imeVisible(main) } }
                settle(main)
                onMain {
                    check(scroll.scrollY < before) { "scrollback did not move toward history" }
                    check(input(main).let { it.text.toString() == "retained scrollback draft" && it.selectionStart == 3 && it.selectionEnd == 8 && it.hasFocus() }) { "scrollback changed input/selection/focus" }
                    check(main.field<ShellActionOwner>("shellOwner").busy == busy) { "scrollback changed action state" }
                }
            }
            onMain { main.window.insetsController?.show(WindowInsets.Type.ime()) }
            await("forward scroll requires visible IME") { onMain { imeVisible(main) } }
            settle(main)
            onMain { scroll.scrollTo(0, (scroll.getChildAt(0).height - scroll.height) / 2) }
            settle(main)
            val before = onMain { scroll.scrollY }
            swipe(backward = false)
            settle(main)
            onMain { check(imeVisible(main) && scroll.scrollY > before) { "forward scroll hid IME or did not move" } }
        }
        verify(false)
        withAction(main) { _, _ -> verify(true) }
        if (options.getString("capture") == "true") capture(main, "scrollback-ime")
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

    private fun englishIme(main: MainActivity) {
        for (key in listOf("q", "a", "z")) tapImeKey(key)
        await("English IME did not commit literal ASCII text") { onMain { input(main).text.toString() == "qaz" } }
        onMain { checkEdit(main, "qaz", 3) }
        onMain { main.field<HorizontalScrollView>("shellKeyBar").scrollTo(0, 0) }
        settle(main)
        if (options.getString("capture") == "true") capture(main, "english-ime")
    }

    private fun keybarScroll(main: MainActivity) {
        val bar = main.field<HorizontalScrollView>("shellKeyBar")
        onMain {
            check(keys(main).size == 14 && bar.height <= keys(main).maxOf { it.height } + bar.paddingTop + bar.paddingBottom + 1) { "extra keys occupy more than one row" }
            bar.scrollTo(0, 0)
        }
        settle(main)
        val overflow = onMain { bar.canScrollHorizontally(1) }
        onMain {
            input(main).apply { setText("retained swipe draft"); setSelection(text.length) }
        }
        val history = onMain { main.field<AgentBlockHistory>("shellTranscript").blocks.size }
        val rect = onMain { bounds(bar) }
        val down = SystemClock.uptimeMillis()
        val events = listOf(MotionEvent.ACTION_DOWN to 0.85f, MotionEvent.ACTION_MOVE to 0.5f,
            MotionEvent.ACTION_MOVE to 0.15f, MotionEvent.ACTION_UP to 0.15f)
        for ((action, fraction) in events) {
            val event = MotionEvent.obtain(down, SystemClock.uptimeMillis(), action,
                rect.left + rect.width() * fraction, rect.exactCenterY(), 0).apply { source = InputDevice.SOURCE_TOUCHSCREEN }
            try { check(uiAutomation.injectInputEvent(event, true)) { "bar swipe injection failed" } } finally { event.recycle() }
            Thread.sleep(30)
        }
        if (overflow) await("real horizontal swipe did not scroll extra keys") { onMain { bar.scrollX > 0 } }
        else onMain { check(bar.scrollX == 0) { "fitting extra-key row scrolled unexpectedly" } }
        onMain {
            check(input(main).text.toString() == "retained swipe draft" && main.field<AgentBlockHistory>("shellTranscript").blocks.size == history) { "toolbar swipe activated a chip" }
            check(imeVisible(main)) { "horizontal extra-key swipe hid IME" }
        }
    }

    private fun imeTailFollow(main: MainActivity) {
        val scroll = main.field<ScrollView>("shellScroll")
        val append = main.javaClass.getDeclaredMethod("appendShellBlock", AgentPresentationBlock::class.java, Boolean::class.javaPrimitiveType!!)
            .apply { isAccessible = true }
        val marker = AgentPresentationBlock.create(listOf(AgentBlockPart.Text("synthetic IME tail marker")), AgentLogStyle.TEXT_COLOR)
        onMain { append.invoke(main, marker, false) }
        settle(main)
        for (following in listOf(true, false)) {
            hardwareKey(main, KeyEvent.KEYCODE_ESCAPE)
            await("Esc did not hide IME before tail check") { onMain { !imeVisible(main) } }
            settle(main)
            onMain { scroll.scrollTo(0, if (following) scroll.getChildAt(0).bottom else 0) }
            settle(main)
            val before = onMain { scroll.scrollY }
            if (following) {
                val editor = onMain { bounds(input(main)) }
                injectTap(editor.exactCenterX() to editor.exactCenterY())
            } else onMain { main.window.insetsController?.show(WindowInsets.Type.ime()) }
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

    private fun commandTail(main: MainActivity) {
        // No scrolling after submission: checking only busy/idle used to miss stale anchors.
        val commands = listOf("sh wi status", "show wifi scan", "sh wi status ip", "sh permissions",
            "sh wi status | count", "sh wi status | display json", "sh cli", "invalid-tail-qa")
        val imeStates = options.getString("ime")?.let { listOf(it.toBooleanStrict()) } ?: listOf(true, false)
        for (showIme in imeStates) {
            onMain { if (showIme) main.window.insetsController?.show(WindowInsets.Type.ime()) else main.window.insetsController?.hide(WindowInsets.Type.ime()) }
            await("command IME state did not settle") { onMain { imeVisible(main) == showIme } }
            settle(main)
            for (fromHistory in listOf(false, true)) for (command in if (fromHistory) commands.take(1) else commands) {
                sendStatus(0, Bundle().apply { putString("phase", "command-tail IME=$showIme history=$fromHistory command=$command") })
                val before = onMain {
                    val scroll = main.field<ScrollView>("shellScroll")
                    if (fromHistory) scroll.scrollTo(0, 0)
                    val previous = main.field<AgentBlockHistory>("shellTranscript").blocks.last()
                    input(main).apply {
                        setText(command); setSelection(text.length); requestFocus()
                        check(checkNotNull(onCreateInputConnection(EditorInfo())).performEditorAction(EditorInfo.IME_ACTION_GO))
                    }
                    previous
                }
                await("read-only command did not finish") {
                    onMain { !main.field<ShellActionOwner>("shellOwner").busy && main.field<AgentBlockHistory>("shellTranscript").blocks.last() !== before }
                }
                settle(main)
                if (options.getString("capture") == "true" && command == commands.first()) capture(main, "command-tail-$showIme-$fromHistory")
                assertTail(main, "$command IME=$showIme history=$fromHistory")
                onMain { check(input(main).hasFocus() && imeVisible(main) == showIme) { "command changed input focus/IME" } }
            }
        }
    }

    private fun burstTail(main: MainActivity) {
        var detached = 0
        val listener = object : View.OnAttachStateChangeListener {
            override fun onViewAttachedToWindow(view: View) = Unit
            override fun onViewDetachedFromWindow(view: View) { detached++ }
        }
        try {
            onMain {
                input(main).addOnAttachStateChangeListener(listener)
                check(checkNotNull(input(main).onCreateInputConnection(EditorInfo())).setComposingText("burst draft", 1))
                repeat(6) { append(main, AgentPresentationBlock.create(List(12) { AgentBlockPart.Text("burst $it tail QA") }, AgentLogStyle.TEXT_COLOR)) }
                repeat(3) { main.javaClass.getDeclaredMethod("renderShell").apply { isAccessible = true }.invoke(main) }
                check(android.view.inputmethod.BaseInputConnection.getComposingSpanStart(input(main).text) == 0) { "synchronous render cleared composition" }
            }
            settle(main)
            assertTail(main, "batched appends and repeated render")
            onMain {
                check(detached == 0 && input(main).text.toString() == "burst draft") { "render detached the inline editor or changed its draft" }
                input(main).clearComposingText()
            }
        } finally { onMain { input(main).removeOnAttachStateChangeListener(listener) } }
        val scroll = main.field<ScrollView>("shellScroll")
        onMain { scroll.scrollTo(0, scroll.getChildAt(0).height / 3) }
        settle(main)
        val before = onMain { scroll.scrollY }
        onMain { repeat(3) { append(main, AgentPresentationBlock.create(listOf(AgentBlockPart.Text("incoming history QA")), AgentLogStyle.TEXT_COLOR)) } }
        settle(main)
        onMain { check(scroll.scrollY == before) { "batched output moved history" } }
    }

    private fun asyncTail(main: MainActivity) {
        val block = AgentPresentationBlock.create(List(50) { AgentBlockPart.Text("async tail QA row $it") }, AgentLogStyle.TEXT_COLOR)
        val results = listOf(AgentShellResult(true, block.fullText.lines()), AgentShellResult(true, block = block),
            AgentShellResult(false), AgentShellResult(true))
        for ((kind, result) in results.withIndex()) for (history in listOf(false, true)) {
            withAction(main, result) { release, _ ->
                settle(main)
                val scroll = main.field<ScrollView>("shellScroll")
                onMain {
                    input(main).apply { setText("retained async draft"); setSelection(3, 8) }
                }
                settle(main)
                onMain { scroll.scrollTo(0, if (history) 0 else scroll.getChildAt(0).bottom) }
                settle(main)
                val before = onMain { scroll.scrollY }
                release.countDown()
                await("async result stayed busy") { !main.field<ShellActionOwner>("shellOwner").busy }
                settle(main)
                if (history) onMain { check(scroll.scrollY == before) { "async completion moved history" } }
                else assertTail(main, "async kind=$kind")
                onMain { check(input(main).let { it.text.toString() == "retained async draft" && it.hasFocus() && it.selectionStart == 3 && it.selectionEnd == 8 }) { "async result changed draft/selection/focus" } }
            }
        }
    }

    private fun reflowTail(main: MainActivity) {
        val scroll = main.field<ScrollView>("shellScroll")
        val original = onMain { scroll.layoutParams.width to scroll.layoutParams.height }
        try {
            onMain {
                append(main, AgentPresentationBlock.create(List(40) { AgentBlockPart.Field("Row $it", "long safe output value ".repeat(4)) }, AgentLogStyle.TEXT_COLOR))
                input(main).apply { setText("reflow draft"); setSelection(2) }
            }
            settle(main)
            assertTail(main, "before reflow")
            val fullWidth = onMain { scroll.width }
            for (width in listOf((fullWidth * 0.6).roundToInt(), fullWidth)) {
                onMain { scroll.layoutParams = scroll.layoutParams.apply { this.width = width } }
                await("output width did not change") { onMain { scroll.width == width } }
                settle(main)
                assertTail(main, "width=$width")
                onMain { checkEdit(main, "reflow draft", 2) }
            }
            val height = onMain { scroll.height }
            for (next in listOf(height * 3 / 4, height)) {
                onMain { scroll.layoutParams = scroll.layoutParams.apply { this.height = next } }
                await("output height did not change") { onMain { scroll.height == next } }
                settle(main)
                assertTail(main, "height=$next")
            }
            val status = main.field<TextView>("shellActionStatus")
            val minimum = onMain { status.minimumHeight }
            try {
                // Elapsed/status text can grow without appending or rebuilding the transcript.
                onMain { status.minimumHeight = minimum + 6 * status.lineHeight }
                settle(main)
                assertTail(main, "status-only content growth")
            } finally { onMain { status.minimumHeight = minimum } }
            settle(main)
            assertTail(main, "status-only content shrink")
            if (options.getString("capture") == "true") capture(main, "reflow-tail")
        } finally {
            onMain { scroll.layoutParams = scroll.layoutParams.apply { width = original.first; height = original.second } }
        }
    }

    private fun gestureTail(main: MainActivity) = withAction(main) { release, _ ->
        hardwareKey(main, KeyEvent.KEYCODE_ESCAPE)
        await("history swipe could not hide IME") { onMain { !imeVisible(main) } }
        settle(main)
        val scroll = main.field<ScrollView>("shellScroll")
        val rect = onMain { bounds(scroll).apply { bottom = minOf(bottom, bounds(main.field<View>("shellInputPanel")).top) } }
        val down = SystemClock.uptimeMillis()
        for (step in 0..8) {
            val action = when (step) { 0 -> MotionEvent.ACTION_DOWN; 8 -> MotionEvent.ACTION_UP; else -> MotionEvent.ACTION_MOVE }
            val y = rect.top + rect.height() * (0.2f + 0.6f * step / 8)
            val event = MotionEvent.obtain(down, SystemClock.uptimeMillis(), action, rect.exactCenterX(), y, 0).apply { source = InputDevice.SOURCE_TOUCHSCREEN }
            try { check(uiAutomation.injectInputEvent(event, true)) { "history swipe injection failed" } } finally { event.recycle() }
            Thread.sleep(if (step == 7) 250 else 40)
        }
        settle(main)
        val anchor = onMain {
            check(!imeVisible(main)) { "history swipe reopened IME" }
            check(scroll.canScrollVertically(1)) { "real swipe did not detach from tail" }
            val content = scroll.getChildAt(0) as ViewGroup
            val view = (0 until content.childCount).map { content.getChildAt(it) }.first { it.bottom > scroll.scrollY }
            view to scroll.scrollY - view.top
        }
        release.countDown()
        await("swipe result stayed busy") { !main.field<ShellActionOwner>("shellOwner").busy }
        settle(main)
        onMain { check(kotlin.math.abs(scroll.scrollY - anchor.first.top - anchor.second) <= 2) { "completion pulled reader away from swiped history" } }
        onMain {
            input(main).requestFocus()
            check(checkNotNull(input(main).onCreateInputConnection(EditorInfo())).commitText("s", 1))
        }
        settle(main)
        assertTail(main, "typing after reading history")
        // A new explicit command must resume following even after a real history gesture.
        onMain {
            input(main).apply {
                setText("show cli | count"); requestFocus()
                check(checkNotNull(onCreateInputConnection(EditorInfo())).performEditorAction(EditorInfo.IME_ACTION_GO))
            }
        }
        await("resume command stayed busy") { !main.field<ShellActionOwner>("shellOwner").busy }
        settle(main)
        assertTail(main, "command after real history gesture")
    }

    private fun append(main: MainActivity, block: AgentPresentationBlock) {
        main.javaClass.getDeclaredMethod("appendShellBlock", AgentPresentationBlock::class.java, Boolean::class.javaPrimitiveType!!)
            .apply { isAccessible = true }.invoke(main, block, false)
    }

    private fun assertTail(main: MainActivity, phase: String) = onMain {
        val scroll = main.field<ScrollView>("shellScroll")
        val content = scroll.getChildAt(0) as ViewGroup
        val remaining = (content.height - scroll.height - scroll.scrollY).coerceAtLeast(0)
        check(!scroll.canScrollVertically(1) && remaining <= 2) { "$phase stopped $remaining px before tail (y=${scroll.scrollY})" }
        check(bounds(content.getChildAt(content.childCount - 1)).bottom <= bounds(main.field<View>("shellInputPanel")).top + 1) { "$phase final output hidden behind input panel" }
        check(content.getChildAt(content.childCount - 1) === input(main) && visibleBounds(input(main)) == bounds(input(main))) { "$phase inline prompt is not visible at the tail" }
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
            check((bar.background as android.graphics.drawable.ColorDrawable).color == android.graphics.Color.rgb(28, 25, 24)) { "extra-key surface does not match the keyboard background" }
            val content = main.field<ViewGroup>("shellContent")
            check(editor.parent === content && content.getChildAt(content.childCount - 1) === editor) { "editor is not inline after output" }
            val naturalHeight = editor.layout.height + editor.compoundPaddingTop + editor.compoundPaddingBottom
            check(editor.height == naturalHeight && editor.layout.lineCount == 1 && editor.top == content.getChildAt(content.childCount - 2).bottom) { "inline prompt has a separate padded bar: height=${editor.height} natural=$naturalHeight gap=${editor.top - content.getChildAt(content.childCount - 2).bottom}" }
            check((main.field<ViewGroup>("shellInputPanel")).childCount == 1) { "fixed input bar remains above the chips" }
            check(bounds(main.field<View>("shellInputPanel")).top - bounds(main.field<View>("root")).top >= 2 * editor.lineHeight) { "input controls leave less than two text lines for results" }
        }
        val labels = mapOf(
            "Tab" to "Complete command", "↑" to "Previous command", "↓" to "Next command",
            "C-c" to "Cancel running command; clear unsent input when idle", "?" to "Context help",
            "sh wi st" to "Insert sh wi st; double tap to execute", "req wi scan br" to "Insert req wi scan br; double tap to execute",
            "sh wi st ip" to "Insert sh wi st ip; double tap to execute", "| match" to "Insert | match",
            "| count" to "Insert | count", "| display json" to "Insert | display json",
            "\"" to "Insert double quote", "|" to "Insert pipe",
            "sh permissions" to "Insert sh permissions; double tap to execute",
        )
        onMain { check(keys(main).map { it.text.toString() } == labels.keys.toList()) { "key labels/order changed" } }
        for ((label, description) in labels) {
            reveal(main, label)
            onMain {
                val button = key(main, label)
                val width = (48 * 0.8f * main.resources.displayMetrics.density).roundToInt()
                val height = (48 * 0.78f * main.resources.displayMetrics.density).roundToInt()
                check(button.width >= width && button.height >= height) { "key smaller than its compact target: $label" }
                if (main.resources.configuration.fontScale <= 1f) check(button.height == height) { "key did not use 0.78x height: $label" }
                val params = button.layoutParams as LinearLayout.LayoutParams
                check(params.width == ViewGroup.LayoutParams.WRAP_CONTENT && params.weight == 0f && button.maxLines == 1) { "chip is stretched or wraps its label: $label" }
                check(visibleBounds(button) == bounds(button)) { "key cannot be fully reached by horizontal scrolling: $label" }
                check(button.isEnabled && button.isClickable && button.contentDescription?.toString() == description) { "key has no enabled action/expected accessibility name: $label" }
                check(button.typeface == android.graphics.Typeface.SANS_SERIF && button.currentTextColor == android.graphics.Color.rgb(238, 227, 226) && button.letterSpacing == 0f) { "key typography does not match the keyboard: $label" }
                val background = button.background as android.graphics.drawable.RippleDrawable
                val shape = background.getDrawable(0) as android.graphics.drawable.GradientDrawable
                check(shape.color?.defaultColor == android.graphics.Color.rgb(49, 43, 42) && shape.cornerRadius == (10 * main.resources.displayMetrics.density).toInt().toFloat()) { "key color or rounding differs from the keyboard: $label" }
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
            hardwareKey(main, KeyEvent.KEYCODE_DPAD_LEFT)
            onMain { checkEdit(main, "A${cluster}B", 1) }
            hardwareKey(main, KeyEvent.KEYCODE_DPAD_RIGHT)
            onMain { checkEdit(main, "A${cluster}B", 1 + cluster.length) }
        }
        onMain { input(main).apply { setText("ab"); setSelection(0, 1) } }
        tap(main, "\"")
        onMain { checkEdit(main, "\"b", 1) }
    }

    private fun shortcutInput(main: MainActivity) {
        val before = onMain { main.field<AgentBlockHistory>("shellTranscript").blocks.size }
        for (command in listOf("sh wi st", "req wi scan br", "sh wi st ip", "sh permissions")) {
            onMain { input(main).apply { setText("draft"); setSelection(0, text.length) } }
            tap(main, command)
            onMain {
                checkEdit(main, command, command.length)
                check(AgentShellParser.parse(command) !is AgentShellCommand.Invalid) { "shortcut contains an invalid Dropcheck command" }
                val parsed = AgentShellParser.parse(command) as AgentShellCommand.Execute
                if (command == "req wi scan br") check(parsed.request.hasGetFreshWifiScan() && parsed.brief) { "scan shortcut did not request a brief fresh scan" }
                if (command == "sh wi st ip") check(parsed.request.hasGetIpStatus()) { "IP shortcut does not inspect IP status" }
            }
        }
        for (filter in listOf("| match", "| count", "| display json")) {
            val source = "sh wi status"
            val expected = "$source $filter "
            onMain { input(main).apply { setText(source); setSelection(text.length) } }
            tap(main, filter)
            onMain {
                checkEdit(main, expected, expected.length)
                check(AgentShellParser.parse(expected + if (filter == "| match") "wifi" else "") !is AgentShellCommand.Invalid) { "shortcut contains an invalid pipeline" }
            }
        }
        settle(main)
        onMain {
            check(main.field<AgentBlockHistory>("shellTranscript").blocks.size == before && !main.field<ShellActionOwner>("shellOwner").busy) { "shortcut executed instead of only editing the draft" }
        }
    }

    private fun ctrlCKey(main: MainActivity) = withAction(main) { _, interrupted ->
        onMain { input(main).apply { setText("retained next draft"); setSelection(text.length) } }
        tap(main, "C-c", ViewConfiguration.getLongPressTimeout().toLong() + 100)
        settle(main)
        onMain { check(key(main, "C-c").createAccessibilityNodeInfo().tooltipText?.toString() == "Cancel running command; clear unsent input when idle") { "C-c explanation is missing from its accessibility node" } }
        check(main.field<ShellActionOwner>("shellOwner").busy) { "long-press explanation canceled the command" }
        if (options.getString("capture") == "true") capture(main, "ctrl-c-tooltip")
        tap(main, "C-c")
        check(interrupted.await(3, TimeUnit.SECONDS)) { "Ctrl-C did not interrupt the active command" }
        await("Ctrl-C left the command busy") { !main.field<ShellActionOwner>("shellOwner").busy }
        settle(main)
        assertTail(main, "Ctrl-C result")
        onMain { checkEdit(main, "retained next draft", "retained next draft".length) }
    }

    private fun shortcutDoubleTap(main: MainActivity) {
        for ((index, command) in listOf("sh permissions", "sh wi st", "sh wi st ip").withIndex()) {
            val suffix = " | match '^double-tap-qa-$index$'"
            val expected = command + suffix
            onMain { input(main).apply { setText("replace-me$suffix"); setSelection(0, "replace-me".length) } }
            reveal(main, command)
            val point = chipPoint(main, command)
            val previous = onMain { main.field<AgentBlockHistory>("shellTranscript").blocks.last() }
            injectTap(point)
            onMain {
                checkEdit(main, expected, command.length)
                check(main.field<AgentBlockHistory>("shellTranscript").blocks.last() === previous) { "first tap executed a command" }
            }
            Thread.sleep(70)
            injectTap(point)
            if (index == 0) { Thread.sleep(70); injectTap(point) }
            await("double tap did not submit its command") { onMain {
                !main.field<ShellActionOwner>("shellOwner").busy && main.field<AgentBlockHistory>("shellTranscript").blocks.last() !== previous
            } }
            settle(main)
            onMain {
                check(main.field<AgentBlockHistory>("shellTranscript").blocks.count { it.fullText == "self> $expected" } == 1) { "double tap did not submit exactly once" }
                val draft = if (index == 0) command else ""
                checkEdit(main, draft, draft.length)
            }
            assertTail(main, "double-tap result")
        }
        if (options.getString("capture") == "true") capture(main, "shortcut-double-tap")
    }

    private fun shortcutDoubleTapGuards(main: MainActivity) {
        val command = "sh permissions"
        fun reset(): Pair<Float, Float> {
            onMain { input(main).apply { setText(""); requestFocus() } }
            reveal(main, command)
            return chipPoint(main, command)
        }
        val previous = onMain { main.field<AgentBlockHistory>("shellTranscript").blocks.last() }
        var point = reset()
        injectTap(point)
        Thread.sleep(ViewConfiguration.getDoubleTapTimeout().toLong() + 50)
        injectTap(point)
        onMain { check(input(main).text.toString() == command + command) { "two slow taps were treated as a double tap" } }

        point = reset()
        injectTap(point)
        onMain { input(main).text.append(" edited") }
        Thread.sleep(70)
        injectTap(point)
        onMain { check(input(main).text.toString() == "$command edited$command") { "double tap executed an edited draft" } }

        point = reset()
        injectTap(point)
        onMain { input(main).setSelection(0) }
        Thread.sleep(70)
        injectTap(point)
        onMain { check(input(main).text.toString() == command + command) { "double tap executed after cursor movement" } }

        point = reset()
        injectTap(point)
        Thread.sleep(70)
        injectTap(point, ViewConfiguration.getLongPressTimeout().toLong() + 100)
        Thread.sleep(70)
        injectTap(point)
        onMain { check(input(main).text.toString() == command + command) { "long press armed double-tap execution" } }

        point = reset()
        injectTap(point)
        Thread.sleep(70)
        val down = SystemClock.uptimeMillis()
        for (action in listOf(MotionEvent.ACTION_DOWN, MotionEvent.ACTION_CANCEL)) {
            val event = MotionEvent.obtain(down, SystemClock.uptimeMillis(), action, point.first, point.second, 0).apply { source = InputDevice.SOURCE_TOUCHSCREEN }
            try { check(uiAutomation.injectInputEvent(event, true)) } finally { event.recycle() }
            Thread.sleep(30)
        }
        onMain { check(input(main).text.toString() == command) { "canceled second tap changed the draft" } }
        settle(main)
        onMain { check(main.field<AgentBlockHistory>("shellTranscript").blocks.last() === previous && !main.field<ShellActionOwner>("shellOwner").busy) { "guarded gesture submitted a command" } }

        onMain { input(main).apply { setText("sh wi status"); setSelection(text.length) } }
        reveal(main, "| count")
        point = chipPoint(main, "| count")
        injectTap(point); Thread.sleep(70); injectTap(point)
        settle(main)
        onMain {
            check(input(main).text.toString() == "sh wi status | count  | count ") { "filter double tap stopped being insertion-only" }
            check(main.field<AgentBlockHistory>("shellTranscript").blocks.last() === previous) { "filter double tap submitted input" }
        }

        point = reset()
        injectTap(point)
        val afterHelp = onMain {
            key(main, "?").performClick()
            main.field<AgentBlockHistory>("shellTranscript").blocks.last()
        }
        Thread.sleep(70)
        injectTap(point)
        onMain {
            check(input(main).text.toString() == command + command && main.field<AgentBlockHistory>("shellTranscript").blocks.last() === afterHelp) { "intervening key did not cancel double-tap execution" }
            input(main).setText("")
            repeat(2) { key(main, command).performClick() }
            check(input(main).text.toString() == command + command && main.field<AgentBlockHistory>("shellTranscript").blocks.last() === afterHelp) { "accessibility clicks bypassed touch-only double-tap recognition" }
        }
    }

    private fun shortcutDoubleTapBusy(main: MainActivity) = withAction(main) { _, _ ->
        val previous = onMain { main.field<AgentBlockHistory>("shellTranscript").blocks.last() }
        for (command in listOf("sh wi st", "req wi scan br", "sh wi st ip", "sh permissions")) {
            onMain { input(main).apply { setText("draft"); setSelection(0, text.length) } }
            reveal(main, command)
            val point = chipPoint(main, command)
            injectTap(point); Thread.sleep(70); injectTap(point)
            onMain {
                checkEdit(main, command, command.length)
                check(main.field<ShellActionOwner>("shellOwner").busy && main.field<AgentBlockHistory>("shellTranscript").blocks.last() === previous) { "busy double tap dispatched another command" }
            }
        }
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
            if (overflow) {
                hardwareKey(main, KeyEvent.KEYCODE_ESCAPE)
                await("result inspection could not hide IME") { onMain { !imeVisible(main) } }
                settle(main)
            }
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
                    val visible = visibleBounds(text).apply { bottom = minOf(bottom, bounds(main.field<View>("shellInputPanel")).top) }
                    // A large-font landscape viewport may show only the end of the result.
                    textPoint(text, visible) ?: error("result has no visible inspection word")
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
        onMain { check(!imeVisible(main)) { "read-only result inspection reopened IME" } }
    }

    private fun findText(value: String, exact: Boolean = true): AccessibilityNodeInfo? = findNode { node ->
        val text = node.text?.toString().orEmpty()
        if (exact) text == value else text.contains(value)
    }

    private fun textPoint(text: TextView, visible: Rect): Pair<Float, Float>? {
        val origin = bounds(text)
        return (0 until text.layout.lineCount).asSequence().mapNotNull { line ->
            val start = text.layout.getLineStart(line)
            val end = text.layout.getLineEnd(line)
            val offset = (start until end).firstOrNull { !text.text[it].isWhitespace() } ?: return@mapNotNull null
            val x = origin.left + text.totalPaddingLeft + text.layout.getPrimaryHorizontal(offset) + text.paint.measureText(text.text.subSequence(offset, offset + 1).toString()) / 2
            val y = origin.top + text.totalPaddingTop + text.layout.getLineBaseline(line) - text.paint.textSize / 3
            (x to y).takeIf { visible.contains(x.toInt(), y.toInt()) }
        }.firstOrNull()
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
        hardwareKey(main, KeyEvent.KEYCODE_ESCAPE)
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
        hardwareKey(main, KeyEvent.KEYCODE_ESCAPE)
        await("Esc did not hide the real IME") { onMain { !imeVisible(main) } }
        hardwareKey(main, KeyEvent.KEYCODE_C, KeyEvent.META_CTRL_ON)
        check(interrupted.await(3, TimeUnit.SECONDS)) { "physical Ctrl-C after Esc did not interrupt the action" }
        await("physical cancellation remained busy") { !main.field<ShellActionOwner>("shellOwner").busy }
        onMain { checkEdit(main, "next draft", 10) }
    }

    private fun escapeCompletion(main: MainActivity) = withAction(main) { release, _ ->
        hardwareKey(main, KeyEvent.KEYCODE_ESCAPE)
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

    private fun withAction(main: MainActivity, result: AgentShellResult = AgentShellResult(true, listOf("synthetic keyboard QA result")), test: (CountDownLatch, CountDownLatch) -> Unit) {
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
                        result
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

    private fun tap(main: MainActivity, label: String, holdMs: Long = 30) {
        reveal(main, label)
        injectTap(chipPoint(main, label), holdMs)
        onMain { Unit }
    }

    private fun chipPoint(main: MainActivity, label: String): Pair<Float, Float> = onMain {
            check(main.hasWindowFocus()) { "refusing touch outside the focused Activity" }
            val rect = bounds(key(main, label))
            check(visibleBounds(key(main, label)) == rect && rect.width() > 0 && rect.height() > 0) { "touch key is clipped: $label" }
            check(!imeVisible(main) || rect.bottom <= imeTop(main) + 1) { "touch key is behind the IME: $label" }
            rect.exactCenterX() to rect.exactCenterY()
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
                val bar = main.field<HorizontalScrollView>("shellKeyBar")
                listOf(bounds(input(main)), bounds(bar), bar.scrollX, imeBottom(main), scroll.scrollY, scroll.getChildAt(0).height)
            }
            stable = if (sample == previous) stable + 1 else 0
            previous = sample
            stable >= 3
        }
    }

    private fun checkEdit(main: MainActivity, expected: String, cursor: Int) {
        check(input(main).let { it.text.toString() == expected && it.selectionStart == cursor && it.selectionEnd == cursor && it.hasFocus() }) { "touch/physical editing changed text, cursor, or focus unexpectedly" }
    }

    private fun input(main: MainActivity): EditText = main.field("shellInput")
    private fun imeVisible(main: MainActivity): Boolean = main.window.decorView.rootWindowInsets?.isVisible(WindowInsets.Type.ime()) == true
    private fun imeBottom(main: MainActivity): Int = checkNotNull(main.window.decorView.rootWindowInsets).getInsets(WindowInsets.Type.ime()).bottom
    private fun imeTop(main: MainActivity): Int = main.windowManager.currentWindowMetrics.bounds.bottom - imeBottom(main)
    private fun key(main: MainActivity, label: String): Button = keys(main).single { it.text.toString() == label }
    private fun keys(main: MainActivity): List<Button> {
        val row = main.field<HorizontalScrollView>("shellKeyBar").getChildAt(0) as ViewGroup
        return (0 until row.childCount).map { row.getChildAt(it) as Button }
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
