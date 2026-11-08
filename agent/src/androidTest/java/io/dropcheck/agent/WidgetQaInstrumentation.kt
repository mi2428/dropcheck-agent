package io.dropcheck.agent

import android.app.Activity
import android.app.Dialog
import android.app.Instrumentation
import android.appwidget.AppWidgetHostView
import android.appwidget.AppWidgetManager
import android.content.ComponentName
import android.content.Context
import android.content.ContextWrapper
import android.content.Intent
import android.graphics.Color
import android.os.Bundle
import android.os.SystemClock
import android.view.Gravity
import android.view.View
import android.view.ViewGroup
import android.view.WindowInsets
import android.widget.FrameLayout
import android.widget.TextView
import java.io.File

/** Attached framework widget host; isolated synthetic logs, no launcher or permission changes. */
class WidgetQaInstrumentation : Instrumentation() {
    private var options = Bundle()
    private var phase = "startup"

    override fun onCreate(arguments: Bundle?) { super.onCreate(arguments); options = arguments ?: Bundle(); start() }

    override fun onStart() {
        val result = Bundle()
        var activity: MainActivity? = null
        var dialog: Dialog? = null
        var directory: File? = null
        var cases = 0
        var largestParcel = 0
        var code = Activity.RESULT_CANCELED
        try {
            activity = startActivitySync(Intent(targetContext, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)) as MainActivity
            val main = checkNotNull(activity)
            runOnMainSync { main.setShowWhenLocked(true); main.setTurnScreenOn(true) }
            waitForIdleSync()
            directory = File(targetContext.cacheDir, "widget-qa-isolated-${System.nanoTime()}").apply { check(mkdir()) }
            val storage = checkNotNull(directory)
            val isolated = object : ContextWrapper(targetContext) {
                override fun getApplicationContext(): Context = this
                override fun getExternalFilesDir(type: String?): File = storage
                override fun getFilesDir(): File = storage
            }
            val info = AppWidgetManager.getInstance(targetContext).installedProviders.single {
                it.provider == ComponentName(targetContext, AgentLogWidgetProvider::class.java)
            }
            lateinit var host: AppWidgetHostView
            lateinit var content: FrameLayout
            fun dp(value: Int) = (value * main.resources.displayMetrics.density).toInt()
            val width = options.getString("widthDp")?.toInt() ?: 320
            val height = options.getString("heightDp")?.toInt() ?: 180
            runOnMainSync {
                host = AppWidgetHostView(main).apply { setAppWidget(0x4c4f47, info) }
                content = FrameLayout(main).apply {
                    setBackgroundColor(Color.BLACK)
                    addView(host, FrameLayout.LayoutParams(dp(width), dp(height), Gravity.TOP or Gravity.CENTER_HORIZONTAL).apply { topMargin = dp(32) })
                }
                dialog = Dialog(main).apply {
                    setContentView(content)
                    @Suppress("DEPRECATION")
                    window!!.addFlags(android.view.WindowManager.LayoutParams.FLAG_SHOW_WHEN_LOCKED or android.view.WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
                    show()
                    window!!.setLayout(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT)
                    window!!.decorView.setBackgroundColor(Color.BLACK)
                    window!!.insetsController?.hide(WindowInsets.Type.ime())
                }
            }
            waitForIdleSync()
            val focusDeadline = SystemClock.elapsedRealtime() + 3000
            while (!onMain { dialog!!.window!!.decorView.hasWindowFocus() } && SystemClock.elapsedRealtime() < focusDeadline) {
                Thread.sleep(25)
                waitForIdleSync()
            }
            check(onMain { dialog!!.window!!.decorView.hasWindowFocus() }) { "widget host window is obscured by ${uiAutomation.rootInActiveWindow?.packageName}/${uiAutomation.rootInActiveWindow?.className}" }
            var fixtureContext: Context = isolated
            var file = TerminalLog.file(fixtureContext)
            fun render() {
                val views = AgentLogWidgetProvider.remoteViews(fixtureContext, 0x4c4f47)
                val parcel = android.os.Parcel.obtain()
                try {
                    views.writeToParcel(parcel, 0)
                    largestParcel = maxOf(largestParcel, parcel.dataSize())
                    check(parcel.dataSize() < 128 * 1024) { "Latest-log RemoteViews exceeds its payload budget" }
                } finally { parcel.recycle() }
                runOnMainSync { host.updateAppWidget(views) }
                waitForIdleSync()
            }
            fun verify(marker: String, decorated: Boolean = true) {
                val displayedMarker = if (decorated) terminalDisplayText(marker) else marker
                val deadline = SystemClock.elapsedRealtime() + 3000
                while (SystemClock.elapsedRealtime() < deadline) {
                    if (onMain { visible(host, displayedMarker) }) return
                    Thread.sleep(25)
                    waitForIdleSync()
                }
                sendStatus(0, Bundle().apply { putString("geometry", onMain {
                    val tree = descendants(host).joinToString(",") { "${it.javaClass.simpleName}#${it.id}" }
                    tree + "; " + descendants(host).filterIsInstance<TextView>().joinToString("; ") { text ->
                        val p = IntArray(2).also { text.getLocationOnScreen(it) }
                        val global = android.graphics.Rect().also { text.getGlobalVisibleRect(it) }
                        val local = android.graphics.Rect().also { text.getLocalVisibleRect(it) }
                        val line = text.layout.getLineForOffset(text.text.indexOf(displayedMarker).coerceAtLeast(0))
                        "chars=${text.text.length} marker=${text.text.indexOf(displayedMarker)} position=${p.toList()} global=$global local=$local scrollY=${text.scrollY} height=${text.height} padding=${text.totalPaddingTop}/${text.totalPaddingBottom} lineTop=${text.layout.getLineTop(line)} lineBottom=${text.layout.getLineBottom(line)}"
                    }
                }) })
                error("latest log marker is outside the widget viewport: $phase")
            }
            fun stage(name: String) { phase = name; sendStatus(0, Bundle().apply { putString("phase", name) }) }

            stage("large-history-latest-tail")
            file.bufferedWriter().use { output ->
                repeat(200_000) { output.append("INFO widget fixture row $it\n") }
                output.append("INFO WIDGET_QA_TAIL_INITIAL\n")
            }
            render()
            verify("WIDGET_QA_TAIL_INITIAL")
            cases++

            stage("rolling-tail")
            file.appendText((0 until 300).joinToString("\n") { "WARN widget burst row $it" } + "\nERROR WIDGET_QA_TAIL_ROLLING\n")
            render()
            verify("WIDGET_QA_TAIL_ROLLING")
            cases++

            stage("maximum-text-payload")
            file.appendText(List(100) { "INFO " + "x".repeat(7_940) }.joinToString("\n") + "\nINFO WIDGET_QA_BOUNDED_TAIL\n")
            render()
            verify("WIDGET_QA_BOUNDED_TAIL")
            check(onMain { host.findViewById<TextView>(R.id.agentLogWidgetText).text.length } <= AgentLogWidgetLines.MAX_DISPLAY_CHARS)
            cases++

            stage("wrapped-last-row")
            file.appendText("INFO " + "wrapped text ".repeat(100) + " WIDGET_QA_TAIL_WRAPPED\n")
            render()
            verify("WIDGET_QA_TAIL_WRAPPED")
            cases++

            stage("resized-tail")
            runOnMainSync { host.layoutParams = (host.layoutParams as FrameLayout.LayoutParams).apply { this.width = dp(180); this.height = dp(96) } }
            waitForIdleSync()
            render()
            verify("WIDGET_QA_TAIL_WRAPPED")
            cases++

            stage("empty-and-replaced-tail")
            val empty = File(storage, "empty-store").apply { check(mkdir()) }
            fixtureContext = object : ContextWrapper(targetContext) {
                override fun getApplicationContext(): Context = this
                override fun getExternalFilesDir(type: String?): File = empty
            }
            file = TerminalLog.file(fixtureContext)
            render()
            verify(main.getString(R.string.agent_log_widget_placeholder), decorated = false)
            file.writeText("INFO WIDGET_QA_TAIL_REPLACED\n")
            render()
            verify("WIDGET_QA_TAIL_REPLACED")
            cases++

            stage("storage-fault-and-recovery")
            val healthy = fixtureContext
            val blocked = File(storage, "blocked-store").apply { check(mkdir()) }
            fixtureContext = object : ContextWrapper(targetContext) {
                override fun getApplicationContext(): Context = this
                override fun getExternalFilesDir(type: String?): File = blocked
            }
            check(TerminalLog.file(fixtureContext).mkdir())
            render()
            verify("Saved history unavailable")
            fixtureContext = healthy
            render()
            verify("WIDGET_QA_TAIL_REPLACED")
            cases++

            if (options.getString("capture") == "true") capture("log-tail")
            stage("log-viewer-tap")
            check(onMain { host.findViewById<View>(R.id.agentLogWidgetRoot).performClick() })
            await("Log widget did not route its tap to the viewer") { main.intent.action == ACTION_OPEN_LOG_VIEWER }
            cases++
            stage("clock-fields-and-resize")
            val clockInfo = AppWidgetManager.getInstance(targetContext).installedProviders.single {
                it.provider == ComponentName(targetContext, AgentClockWidgetProvider::class.java)
            }
            runOnMainSync { host.setAppWidget(0x434c4b, clockInfo) }
            var smallerTimeSize = 0f
            for ((clockWidth, clockHeight) in listOf(180 to 96, 320 to 180)) {
                val views = AgentClockWidgetProvider.remoteViews(targetContext)
                runOnMainSync {
                    host.layoutParams = (host.layoutParams as FrameLayout.LayoutParams).apply {
                        this.width = dp(clockWidth); this.height = dp(clockHeight)
                    }
                    host.updateAppWidget(views)
                }
                waitForIdleSync()
                await("Clock layout did not settle") {
                    host.width == dp(clockWidth) && host.height == dp(clockHeight) && !host.isLayoutRequested
                }
                onMain {
                    val date = host.findViewById<android.widget.TextClock>(R.id.agentClockDate)
                    val time = host.findViewById<android.widget.TextClock>(R.id.agentClockTime)
                    val now = System.currentTimeMillis()
                    check(date.text == android.text.format.DateFormat.format(date.format24Hour, now))
                    val format = if (android.text.format.DateFormat.is24HourFormat(main)) time.format24Hour else time.format12Hour
                    check(time.text == android.text.format.DateFormat.format(format, now))
                    check(visible(host, time.text.toString())) { "Clock time is clipped" }
                    check(visible(host, date.text.toString())) { "Clock date is clipped" }
                    for (id in listOf(R.id.agentClockDate, R.id.agentClockTime, R.id.agentClockWifiInfo, R.id.agentClockIpInfo, R.id.agentClockLocationInfo)) {
                        val text = host.findViewById<TextView>(id)
                        check(text.text.isNotEmpty() && text.getLocalVisibleRect(android.graphics.Rect())) { "Clock field $id not visible" }
                    }
                    check(host.findViewById<TextView>(R.id.agentClockIpInfo).text.startsWith("IPv4 "))
                    check(host.findViewById<TextView>(R.id.agentClockLocationInfo).text.startsWith("GPS/GNSS "))
                    if (smallerTimeSize == 0f) smallerTimeSize = time.textSize else check(time.textSize > smallerTimeSize)
                    check(host.findViewById<View>(R.id.agentClockRoot).hasOnClickListeners())
                }
                cases++
            }
            if (options.getString("capture") == "true") capture("clock")
            if (options.getString("lifecycle") in setOf("true", "log")) {
                stage("log-widget-manager-delivery")
                cases += verifyLogDelivery(main, content, host)
            }
            if (options.getString("lifecycle") == "true") {
                stage("clock-binding-service-lifecycle")
                cases += verifyClockLifecycle(main, content)
            }
            result.putString("stream", "PASS: attached RemoteViews/AppWidgetHostView cases=$cases; largestRemoteViewsBytes=$largestParcel; widthDp=$width heightDp=$height fontScale=${main.resources.configuration.fontScale}; isolated synthetic log, launcher/settings/network unchanged; lifecycle=${options.getString("lifecycle") ?: "none"}.\n")
            code = Activity.RESULT_OK
        } catch (failure: Throwable) {
            if (options.getString("capture") == "true") runCatching { capture("failure-$phase") }
            result.putString("stream", "FAIL: widget stage=$phase cases=$cases ${failure.javaClass.simpleName}: ${failure.message}\n")
        } finally {
            runOnMainSync { dialog?.dismiss(); activity?.finish() }
            directory?.deleteRecursively()
        }
        finish(code, result)
    }

    private fun descendants(view: View): Sequence<View> = sequence {
        yield(view)
        if (view is ViewGroup) for (index in 0 until view.childCount) yieldAll(descendants(view.getChildAt(index)))
    }

    private fun visible(root: View, marker: String): Boolean = descendants(root).filterIsInstance<TextView>().any { text ->
        val start = text.text.indexOf(marker)
        val layout = text.layout
        val rect = android.graphics.Rect()
        if (start < 0 || layout == null || !text.getLocalVisibleRect(rect)) false else {
            val top = text.totalPaddingTop + layout.getLineTop(layout.getLineForOffset(start))
            val end = text.totalPaddingTop + layout.getLineBottom(layout.getLineForOffset(start + marker.length - 1))
            val first = layout.getLineForOffset(start)
            val last = layout.getLineForOffset(start + marker.length - 1)
            top >= rect.top - 1 && end <= rect.bottom + 1 && (first..last).all {
                text.totalPaddingLeft + layout.getLineLeft(it) >= rect.left - 1 &&
                    text.totalPaddingLeft + layout.getLineRight(it) <= rect.right + 1
            }
        }
    }

    private fun <T> onMain(action: () -> T): T {
        var value: Result<T>? = null
        runOnMainSync { value = runCatching(action) }
        return checkNotNull(value).getOrThrow()
    }

    private fun await(message: String, condition: () -> Boolean) {
        val deadline = SystemClock.elapsedRealtime() + 10_000
        while (SystemClock.elapsedRealtime() < deadline) {
            if (onMain(condition)) return
            Thread.sleep(25)
            waitForIdleSync()
        }
        error(message)
    }

    private fun verifyLogDelivery(activity: Activity, content: FrameLayout, original: AppWidgetHostView): Int {
        val manager = AppWidgetManager.getInstance(targetContext)
        val component = ComponentName(targetContext, AgentLogWidgetProvider::class.java)
        val existing = manager.getAppWidgetIds(component).toSet()
        val owner = android.appwidget.AppWidgetHost(targetContext, 0x4c4f4753)
        check(owner.appWidgetIds.isEmpty())
        var id: Int? = null
        uiAutomation.adoptShellPermissionIdentity(android.Manifest.permission.BIND_APPWIDGET)
        try {
            owner.startListening()
            val widgetId = owner.allocateAppWidgetId()
            id = widgetId
            check(manager.bindAppWidgetIdIfAllowed(widgetId, component))
            lateinit var view: AppWidgetHostView
            onMain {
                view = owner.createView(activity, widgetId, checkNotNull(manager.getAppWidgetInfo(widgetId)))
                content.removeAllViews()
                content.addView(view, FrameLayout.LayoutParams(640, 360))
            }
            val saved = TerminalLog.page(targetContext, maxLines = 1).entries.last().text
            await("AppWidgetManager did not deliver the latest saved log") { visible(view, terminalDisplayText(saved)) }
            if (AgentClockWidgetProvider.hasClockWidgets(targetContext)) {
                var previousDelivery = 0L
                repeat(2) { cycle ->
                    val arrived = java.util.concurrent.CountDownLatch(1)
                    val expected = java.util.concurrent.atomic.AtomicReference<String>()
                    val listener: (TerminalLogEntry) -> Unit = { entry ->
                        if ("event=service.start" in entry.text && AgentService.ACTION_WIDGET_START in entry.text) {
                            expected.set(entry.text); arrived.countDown()
                        }
                    }
                    val before = onMain { view.findViewById<TextView>(R.id.agentLogWidgetText).text.toString() }
                    TerminalLog.addListener(listener)
                    try {
                        // Only normal diagnostics from the user's existing Clock lifetime append.
                        AgentService.requestWidgetUpdates(targetContext)
                        check(arrived.await(3, java.util.concurrent.TimeUnit.SECONDS))
                        if (cycle > 0) {
                            Thread.sleep(250)
                            check(onMain { view.findViewById<TextView>(R.id.agentLogWidgetText).text.toString() } == before) { "Widget bypassed batch interval" }
                        }
                        await("AppWidgetManager did not deliver the new log batch") { visible(view, terminalDisplayText(checkNotNull(expected.get()))) }
                        val now = SystemClock.elapsedRealtime()
                        if (cycle > 0) check(now - previousDelivery >= AgentLogWidgetProvider.MIN_UPDATE_INTERVAL_MS - 250)
                        previousDelivery = now
                    } finally { TerminalLog.removeListener(listener) }
                }
                onMain { activity.intent.action = null; check(view.findViewById<View>(R.id.agentLogWidgetRoot).performClick()) }
                await("Delivered widget did not open Log Viewer") { activity.intent.action == ACTION_OPEN_LOG_VIEWER }
                return 4
            }
            return 1
        } finally {
            try {
                id?.let(owner::deleteAppWidgetId)
                owner.stopListening(); owner.deleteHost()
                onMain { content.removeAllViews(); content.addView(original) }
                check(manager.getAppWidgetIds(component).toSet() == existing) { "User Log widget IDs changed" }
            } finally { uiAutomation.dropShellPermissionIdentity() }
        }
    }

    @Suppress("DEPRECATION")
    private fun verifyClockLifecycle(activity: Activity, content: FrameLayout): Int {
        val manager = AppWidgetManager.getInstance(targetContext)
        val component = ComponentName(targetContext, AgentClockWidgetProvider::class.java)
        val services = targetContext.getSystemService(android.app.ActivityManager::class.java)
        fun service() = services.getRunningServices(Int.MAX_VALUE).singleOrNull {
            it.service == ComponentName(targetContext, AgentService::class.java)
        }
        check(manager.getAppWidgetIds(component).isEmpty()) { "Existing Clock widgets must not be disturbed" }
        check(service() == null) { "Existing agent service must not be disturbed" }
        val host = android.appwidget.AppWidgetHost(targetContext, 0x44514341)
        check(host.appWidgetIds.isEmpty()) { "QA host ID is already in use" }
        val owned = mutableListOf<Int>()
        val stopped = java.util.concurrent.CountDownLatch(1)
        val listener: (TerminalLogEntry) -> Unit = {
            if ("service.start" in it.text && AgentService.ACTION_WIDGET_STOP in it.text) stopped.countDown()
        }
        uiAutomation.adoptShellPermissionIdentity(android.Manifest.permission.BIND_APPWIDGET)
        try {
            host.startListening()
            repeat(2) { cycle ->
                val id = host.allocateAppWidgetId().also { owned.add(it) }
                check(manager.bindAppWidgetIdIfAllowed(id, component)) { "Scoped QA binding denied" }
                val info = checkNotNull(manager.getAppWidgetInfo(id))
                lateinit var view: AppWidgetHostView
                runOnMainSync {
                    view = host.createView(activity, id, info)
                    content.removeAllViews()
                    content.addView(view, FrameLayout.LayoutParams(640, 360))
                }
                await("Clock binding did not start its foreground service") { service()?.foreground == true }
                await("Clock provider did not publish RemoteViews through AppWidgetManager") {
                    view.findViewById<TextView>(R.id.agentClockIpInfo)?.text?.startsWith("IPv4 ") == true
                }
                java.net.ServerSocket(0, 1, java.net.InetAddress.getByName("127.0.0.1")).use { endpoint ->
                    endpoint.soTimeout = 3000
                    // The first removal overlaps a local held stream; no commands or external traffic.
                    val connection = if (cycle == 0) {
                        targetContext.startForegroundService(Intent(targetContext, AgentService::class.java)
                            .setAction(AgentService.ACTION_GRPC_SESSION)
                            .putExtra(AgentService.EXTRA_GRPC_PORT, endpoint.localPort)
                            .putExtra(AgentService.EXTRA_GRPC_TOKEN, "widget-lifecycle-fixture"))
                        endpoint.accept()
                    } else null
                    try {
                        TerminalLog.addListener(listener)
                        runOnMainSync { content.removeView(view) }
                        host.deleteAppWidgetId(id)
                        owned.remove(id)
                        if (cycle == 0) {
                            check(stopped.await(3, java.util.concurrent.TimeUnit.SECONDS))
                            check(onMain { service()?.foreground == true }) { "Widget removal stopped an active controller session" }
                        }
                    } finally { TerminalLog.removeListener(listener); connection?.close() }
                }
                await("Last Clock removal did not stop its foreground service") { service() == null }
                check(manager.getAppWidgetIds(component).isEmpty())
            }
            return 2
        } finally {
            try {
                owned.forEach { host.deleteAppWidgetId(it) }
                host.stopListening()
                host.deleteHost()
                targetContext.stopService(Intent(targetContext, AgentService::class.java))
            } finally { uiAutomation.dropShellPermissionIdentity() }
        }
    }

    private fun capture(name: String) {
        val drawn = java.util.concurrent.CountDownLatch(1)
        runOnMainSync {
            // Two frame callbacks ensure layout changes have reached the displayed frame.
            android.view.Choreographer.getInstance().postFrameCallback {
                android.view.Choreographer.getInstance().postFrameCallback { drawn.countDown() }
            }
        }
        check(drawn.await(2, java.util.concurrent.TimeUnit.SECONDS))
        val bitmap = checkNotNull(uiAutomation.takeScreenshot())
        try { File(targetContext.cacheDir, "widget-qa-$name.png").outputStream().use { check(bitmap.compress(android.graphics.Bitmap.CompressFormat.PNG, 100, it)) } }
        finally { bitmap.recycle() }
    }
}
