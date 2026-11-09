package io.dropcheck.agent

import android.app.Activity
import android.app.Dialog
import android.app.Instrumentation
import android.content.Context
import android.content.ContextWrapper
import android.content.Intent
import android.graphics.Color
import android.os.Bundle
import android.os.Looper
import android.os.SystemClock
import android.view.View
import android.view.ViewGroup
import android.view.WindowManager
import android.widget.Button
import android.widget.FrameLayout
import android.widget.TextView
import java.io.File
import java.util.concurrent.atomic.AtomicInteger

/** Real recycled rows against isolated stores, never replacement of the user's saved log. */
class LogHistoryQaInstrumentation : Instrumentation() {
    override fun onCreate(arguments: Bundle?) { super.onCreate(arguments); start() }

    override fun onStart() {
        val report = Bundle()
        var code = Activity.RESULT_CANCELED
        var activity: MainActivity? = null
        var dialog: Dialog? = null
        var viewer: TerminalLogViewer? = null
        var directory: File? = null
        val metrics = mutableListOf<String>()
        var cases = 0
        var phase = "startup"
        try {
            val realFile = TerminalLog.file(targetContext)
            val realLength = realFile.length()
            val realInode = if (realFile.isFile) android.system.Os.stat(realFile.absolutePath).st_ino else null
            val realPrefix = if (realLength > 0) java.io.RandomAccessFile(realFile, "r").use { input ->
                ByteArray(minOf(realLength, 4096).toInt()).also(input::readFully)
            } else byteArrayOf()
            activity = startActivitySync(Intent(targetContext, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)) as MainActivity
            val main = checkNotNull(activity)
            onMain { main.setShowWhenLocked(true); main.setTurnScreenOn(true) }
            directory = File(targetContext.cacheDir, "log-history-qa-${System.nanoTime()}").apply { check(mkdir()) }
            val storage = checkNotNull(directory)
            val holder = FrameLayout(main).apply { setBackgroundColor(Color.BLACK) }
            onMain {
                dialog = Dialog(main).apply {
                    setContentView(holder)
                    @Suppress("DEPRECATION")
                    window!!.addFlags(WindowManager.LayoutParams.FLAG_SHOW_WHEN_LOCKED or WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
                    show()
                    window!!.setLayout(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT)
                    window!!.decorView.setBackgroundColor(Color.BLACK)
                }
            }
            await { dialog!!.window!!.decorView.hasWindowFocus() }
            fun stage(name: String) { phase = name; sendStatus(0, Bundle().apply { putString("phase", name) }) }
            fun fixture(name: String): Context {
                val folder = File(storage, name).apply { check(mkdir()) }
                return object : ContextWrapper(targetContext) {
                    override fun getApplicationContext(): Context = this
                    override fun getExternalFilesDir(type: String?): File {
                        check(Looper.myLooper() != Looper.getMainLooper()) { "History file access on the UI thread" }
                        return folder
                    }
                    override fun getFilesDir(): File = folder
                }
            }
            fun attach(context: Context): TerminalLogViewer {
                return onMain {
                    viewer?.close()
                    holder.removeAllViews()
                    TerminalLogViewer(context).also {
                        viewer = it
                        holder.addView(it, FrameLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT))
                        it.start(); it.show()
                    }
                }
            }
            fun last(view: TerminalLogViewer) = (view.list.adapter.getItem(view.list.adapter.count - 1) as TerminalLogEntry).text
            fun settled(view: TerminalLogViewer): Boolean = !view.list.isLayoutRequested &&
                TerminalLogViewer::class.java.getDeclaredField("appliedVersion").apply { isAccessible = true }.getLong(view) >= 0 &&
                listOf("loading", "positioning", "touching").none { name ->
                    TerminalLogViewer::class.java.getDeclaredField(name).apply { isAccessible = true }.getBoolean(view)
                } && TerminalLogViewer::class.java.getDeclaredField("scrollState").apply { isAccessible = true }.getInt(view) == 0
            fun ready(view: TerminalLogViewer, marker: String) = await {
                settled(view) && view.list.adapter.count > 0 && last(view).contains(marker) &&
                    descendants(view).filterIsInstance<TextView>().none { it.text.toString().startsWith("Loading saved") }
            }
            fun place(view: TerminalLogViewer, position: Int, top: Int = 0) {
                onMain { view.list.setSelectionFromTop(position, top) }
                await {
                    !view.list.isLayoutRequested && if (position == view.list.adapter.count - 1) {
                        view.list.lastVisiblePosition == position && !view.list.canScrollVertically(1)
                    } else view.list.firstVisiblePosition == position
                }
                pointer(view.list, android.view.MotionEvent.ACTION_DOWN)
                pointer(view.list, android.view.MotionEvent.ACTION_UP)
            }
            fun viewport(view: TerminalLogViewer) = onMain {
                view.list.adapter.getItemId(view.list.firstVisiblePosition) to view.list.getChildAt(0).top
            }
            var small: Context? = null
            for (size in listOf(2_000, 200_000)) {
                stage("history-$size")
                val isolated = fixture("rows-$size")
                if (small == null) small = isolated
                val file = TerminalLog.file(isolated)
                file.bufferedWriter().use { writer ->
                    repeat(size) { writer.append("INFO history-row-${it.toString().padStart(7, '0')} ${"payload ".repeat(20)}\n") }
                }
                TerminalLog.compactFileIfNeeded(file)
                val times = List(11) {
                    val start = System.nanoTime()
                    val page = TerminalLog.readPage({ file })
                    check(page.entries.size <= 256)
                    check(page.entries.sumOf { it.text.length } <= TerminalLogHistory.MAX_PAGE_CHARS)
                    System.nanoTime() - start
                }.sorted()
                val view = attach(isolated)
                ready(view, (size - 1).toString().padStart(7, '0'))
                await { view.list.childCount > 0 && !view.list.isLayoutRequested }
                onMain {
                    check(view.list.adapter.count <= 512)
                    check(descendants(view).none { it is Button }) { "Log paging buttons still occupy the viewport" }
                    check(view.list.childCount < view.list.adapter.count) { "All log rows were inflated" }
                    metrics += "history=$size rows=${view.list.adapter.count} attached=${view.list.childCount} medianPageUs=${times[5] / 1000}"
                }
                cases++
                if (size == 200_000) continue

                stage("bidirectional-pages")
                val original = onMain { (view.list.adapter.getItem(0) as TerminalLogEntry).position }
                place(view, 0)
                await { settled(view) && (view.list.adapter.getItem(0) as TerminalLogEntry).position != original }
                val firstOlder = onMain { (view.list.adapter.getItem(0) as TerminalLogEntry).position }
                place(view, 0)
                await { settled(view) && (view.list.adapter.getItem(0) as TerminalLogEntry).position != firstOlder }
                check(onMain { view.list.adapter.count } <= 512)
                place(view, onMain { view.list.adapter.count - 1 })
                ready(view, (size - 1).toString().padStart(7, '0'))
                cases++

                stage("history-anchor-and-live-burst")
                place(view, 80, -5)
                stage("history-gesture-start")
                await { settled(view) }
                pointer(view.list, android.view.MotionEvent.ACTION_DOWN, 0.35f)
                pointer(view.list, android.view.MotionEvent.ACTION_MOVE, 0.5f)
                waitForIdleSync()
                val anchor = viewport(view)
                TerminalLog.info(isolated, "while-reading-old-history")
                Thread.sleep(250)
                check(viewport(view) == anchor) { "Live arrival moved a held history gesture" }
                pointer(view.list, android.view.MotionEvent.ACTION_MOVE, 0.5f)
                Thread.sleep(60)
                pointer(view.list, android.view.MotionEvent.ACTION_MOVE, 0.5f)
                pointer(view.list, android.view.MotionEvent.ACTION_UP, 0.5f)
                stage("history-gesture-end")
                await { settled(view) }
                check(viewport(view) == anchor) { "Ending the gesture moved history" }
                place(view, onMain { view.list.adapter.count - 1 })
                stage("history-resume-tail")
                ready(view, "while-reading-old-history")
                val notifications = AtomicInteger()
                onMain { view.list.adapter.registerDataSetObserver(object : android.database.DataSetObserver() {
                    override fun onChanged() { notifications.incrementAndGet() }
                }) }
                val burstStart = System.nanoTime()
                repeat(500) { TerminalLog.info(isolated, "live-burst-$it") }
                ready(view, "live-burst-499")
                check(notifications.get() < 250) { "Live updates were not coalesced" }
                check(onMain { view.list.adapter.count } <= 512)
                metrics += "liveEvents=500 UIRefreshes=${notifications.get()} persistAndRefreshMs=${(System.nanoTime() - burstStart) / 1_000_000}"
                cases++

                stage("pressed-tail-does-not-follow")
                val previousTail = onMain { last(view) }
                pointer(view.list, android.view.MotionEvent.ACTION_DOWN)
                val pressed = viewport(view)
                TerminalLog.info(isolated, "tail-after-press")
                Thread.sleep(250)
                check(onMain { last(view) } == previousTail && viewport(view) == pressed)
                pointer(view.list, android.view.MotionEvent.ACTION_UP)
                ready(view, "tail-after-press")
                cases++

                stage("fling-defers-updates")
                onMain { view.list.fling(-4_000) }
                await { TerminalLogViewer::class.java.getDeclaredField("scrollState").apply { isAccessible = true }.getInt(view) == 2 }
                val beforeFling = notifications.get()
                TerminalLog.info(isolated, "arrived-during-fling")
                Thread.sleep(100)
                check(notifications.get() == beforeFling) { "An adapter update interrupted the fling" }
                onMain { view.list.fling(0) }
                await { settled(view) }
                place(view, onMain { view.list.adapter.count - 1 })
                ready(view, "arrived-during-fling")
                cases++

                stage("hidden-viewer")
                onMain { view.hide() }
                val unchanged = notifications.get()
                repeat(100) { TerminalLog.info(isolated, "hidden-$it") }
                Thread.sleep(250)
                check(notifications.get() == unchanged) { "Hidden viewer still updated its adapter" }
                onMain { view.show() }
                ready(view, "hidden-99")
                cases++

                stage("stopped-viewer-catches-up")
                onMain { view.stop() }
                TerminalLog.info(isolated, "arrived-while-stopped")
                onMain { view.start() }
                ready(view, "arrived-while-stopped")
                cases++
            }
            stage("empty-history-stays-idle")
            val emptyView = attach(fixture("empty"))
            await { settled(emptyView) && descendants(emptyView).filterIsInstance<TextView>().none { it.text.contains("Loading saved") } }
            val emptyChanges = AtomicInteger()
            onMain { emptyView.list.adapter.registerDataSetObserver(object : android.database.DataSetObserver() {
                override fun onChanged() { emptyChanges.incrementAndGet() }
            }) }
            Thread.sleep(300)
            check(emptyChanges.get() == 0) { "Empty history repeatedly refreshed without a new event" }
            cases++
            stage("storage-failure-live-delivery")
            val blocked = fixture("blocked")
            TerminalLog.file(blocked).mkdir()
            val failedView = attach(blocked)
            TerminalLog.warn(blocked, "live-with-storage-failure")
            await {
                failedView.list.adapter.count > 0 && last(failedView).contains("live-with-storage-failure") &&
                    descendants(failedView).filterIsInstance<TextView>().any { it.text.contains("Saved history unavailable") }
            }
            await { settled(failedView) }
            val failedChanges = AtomicInteger()
            onMain { failedView.list.adapter.registerDataSetObserver(object : android.database.DataSetObserver() {
                override fun onChanged() { failedChanges.incrementAndGet() }
            }) }
            Thread.sleep(300)
            check(failedChanges.get() == 0) { "Failed history repeatedly retried without a new event" }
            cases++
            // Recover only the isolated store state; do not touch the real log.
            onMain { failedView.stop() }
            TerminalLog.info(checkNotNull(small), "storage-recovery-fixture")
            TerminalLog.page(checkNotNull(small))
            check(TerminalLog.persistenceWarning == null)
            stage("capture")
            val captureView = attach(checkNotNull(small))
            ready(captureView, "storage-recovery-fixture")
            val frames = java.util.concurrent.CountDownLatch(1)
            onMain { android.view.Choreographer.getInstance().postFrameCallback {
                android.view.Choreographer.getInstance().postFrameCallback { frames.countDown() }
            } }
            check(frames.await(2, java.util.concurrent.TimeUnit.SECONDS))
            val image = checkNotNull(uiAutomation.takeScreenshot())
            try { File(targetContext.cacheDir, "log-history-qa.png").outputStream().use { image.compress(android.graphics.Bitmap.CompressFormat.PNG, 100, it) } }
            finally { image.recycle() }
            stage("main-activity-swipe-navigation")
            onMain { viewer?.close(); viewer = null; dialog!!.dismiss(); dialog = null }
            await { main.hasWindowFocus() }
            val actual = onMain { MainActivity::class.java.getDeclaredField("logViewer").apply { isAccessible = true }.get(main) as TerminalLogViewer }
            val shell = onMain { MainActivity::class.java.getDeclaredField("shellScroll").apply { isAccessible = true }.get(main) as android.widget.ScrollView }
            onMain {
                descendants(main.window.decorView).filterIsInstance<TextView>()
                    .single { it.contentDescription == "Switch to Log Viewer" }.performClick()
            }
            await { actual.visibility == View.VISIBLE && actual.list.childCount > 0 }
            swipe(actual.list, right = true)
            await { actual.visibility == View.GONE && shell.visibility == View.VISIBLE }
            cases++
            onMain { main.window.insetsController?.hide(android.view.WindowInsets.Type.ime()); shell.scrollTo(0, 0) }
            await { shell.scrollY == 0 && !shell.isLayoutRequested }
            val header = onMain { (shell.getChildAt(0) as ViewGroup).getChildAt(0) }
            swipe(header, right = false)
            await { actual.visibility == View.VISIBLE && shell.visibility == View.GONE }
            cases++
            if (realInode != null && realLength < TerminalLogHistory.SEGMENT_BYTES - 64 * 1024) {
                check(android.system.Os.stat(realFile.absolutePath).st_ino == realInode)
                check(realFile.length() >= realLength)
                val after = java.io.RandomAccessFile(realFile, "r").use { input -> ByteArray(realPrefix.size).also(input::readFully) }
                check(after.contentEquals(realPrefix)) { "Real saved log was replaced or truncated" }
                metrics += "realLogPreserved=true"
            }
            report.putString("stream", "PASS: in-app log history cases=$cases ${metrics.joinToString("; ")}; isolated synthetic files, no network mutation.\n")
            code = Activity.RESULT_OK
        } catch (failure: Throwable) {
            val state = viewer?.let { view -> onMain {
                listOf("loading", "positioning", "touching", "scrollState", "following", "hasOlder", "hasNewer", "appliedVersion", "generation")
                    .joinToString { name -> "$name=${TerminalLogViewer::class.java.getDeclaredField(name).apply { isAccessible = true }.get(view)}" } +
                    "; count=${view.list.adapter.count} first=${view.list.firstVisiblePosition} last=${view.list.lastVisiblePosition} down=${view.list.canScrollVertically(1)}"
            } }.orEmpty()
            report.putString("stream", "FAIL: history phase=$phase cases=$cases ${failure.javaClass.simpleName}: ${failure.message}; $state\n")
        } finally {
            onMain { viewer?.close(); dialog?.dismiss(); activity?.finish() }
            directory?.deleteRecursively()
        }
        finish(code, report)
    }

    private fun descendants(view: View): Sequence<View> = sequence {
        yield(view)
        if (view is ViewGroup) for (i in 0 until view.childCount) yieldAll(descendants(view.getChildAt(i)))
    }

    private fun swipe(view: View, right: Boolean) {
        val bounds = onMain {
            val point = IntArray(2).also(view::getLocationOnScreen)
            floatArrayOf(point[0].toFloat(), point[1].toFloat(), view.width.toFloat(), view.height.toFloat())
        }
        val down = SystemClock.uptimeMillis()
        for (step in 0..8) {
            val fraction = if (right) 0.2f + step * 0.075f else 0.8f - step * 0.075f
            val action = when (step) { 0 -> android.view.MotionEvent.ACTION_DOWN; 8 -> android.view.MotionEvent.ACTION_UP; else -> android.view.MotionEvent.ACTION_MOVE }
            val event = android.view.MotionEvent.obtain(down, SystemClock.uptimeMillis(), action, bounds[0] + bounds[2] * fraction, bounds[1] + bounds[3] * 0.5f, 0)
            try { check(uiAutomation.injectInputEvent(event, true)) } finally { event.recycle() }
            if (step < 8) Thread.sleep(16)
        }
    }

    private var downTime = 0L
    private fun pointer(view: View, action: Int, y: Float = 0.5f) {
        val point = onMain {
            val location = IntArray(2).also(view::getLocationOnScreen)
            floatArrayOf(location[0] + view.width * 0.5f, location[1] + view.height * y)
        }
        if (action == android.view.MotionEvent.ACTION_DOWN) downTime = SystemClock.uptimeMillis()
        val event = android.view.MotionEvent.obtain(downTime, SystemClock.uptimeMillis(), action, point[0], point[1], 0)
        try { check(uiAutomation.injectInputEvent(event, true)) } finally { event.recycle() }
    }

    private fun <T> onMain(action: () -> T): T {
        var result: Result<T>? = null
        runOnMainSync { result = runCatching(action) }
        return checkNotNull(result).getOrThrow()
    }

    private fun await(condition: () -> Boolean) {
        val deadline = SystemClock.elapsedRealtime() + 10_000
        while (SystemClock.elapsedRealtime() < deadline) {
            if (onMain(condition)) return
            Thread.sleep(25)
            waitForIdleSync()
        }
        error("Native history condition did not settle")
    }
}
