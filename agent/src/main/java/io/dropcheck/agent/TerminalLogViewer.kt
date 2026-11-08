package io.dropcheck.agent

import android.content.Context
import android.graphics.Color
import android.graphics.Typeface
import android.graphics.text.LineBreaker
import android.os.Handler
import android.os.Looper
import android.view.Gravity
import android.view.MotionEvent
import android.view.View
import android.view.ViewGroup
import android.view.ViewTreeObserver
import android.widget.AbsListView
import android.widget.BaseAdapter
import android.widget.FrameLayout
import android.widget.ListView
import android.widget.TextView
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicLong

/** Two bounded pages; paging and tail updates are applied only between user gestures. */
internal class TerminalLogViewer(context: Context) : FrameLayout(context) {
    private data class Row(val entry: TerminalLogEntry, val id: Long)
    private val rows = ArrayList<Row>()
    private val status = TextView(context)
    private val main = Handler(Looper.getMainLooper())
    private val executor = Executors.newSingleThreadScheduledExecutor { Thread(it, "log-history-page").apply { isDaemon = true } }
    private val reads = WidgetUpdateQueue("log-history-page", 0, 100, executor)
    private val version = AtomicLong()
    private val refreshPosted = AtomicBoolean()
    private val unsavedId = AtomicLong(-1)
    private val unsaved = ArrayDeque<Row>()
    private var unsavedChars = 0
    private var omittedUnsaved = 0L
    @Volatile private var started = false
    @Volatile private var shown = false
    private var touching = false
    private var scrollState = AbsListView.OnScrollListener.SCROLL_STATE_IDLE
    private var following = true
    private var loading = false
    private var positioning = false
    private var layoutCallback = false
    private var generation = 0L
    private var appliedVersion = -1L
    private var hasOlder = false
    private var hasNewer = false
    private var storageWarning: String? = null
    private val interacting get() = touching || scrollState != AbsListView.OnScrollListener.SCROLL_STATE_IDLE
    val list = TerminalLogListView(context).apply {
        onTouchChanged = { down ->
            touching = down
            if (down) { following = false; invalidateReads() } else main.post { settle() }
        }
    }
    private val adapter = object : BaseAdapter() {
        override fun getCount() = rows.size
        override fun getItem(position: Int) = rows[position].entry
        override fun getItemId(position: Int) = rows[position].id
        override fun hasStableIds() = true
        override fun getView(position: Int, recycled: View?, parent: ViewGroup): View {
            val entry = rows[position].entry
            return ((recycled as? TextView) ?: TextView(context).apply {
                typeface = Typeface.MONOSPACE
                textSize = 8f
                includeFontPadding = false
                setLineSpacing(0f, 1.05f)
                breakStrategy = LineBreaker.BREAK_STRATEGY_SIMPLE
                hyphenationFrequency = android.text.Layout.HYPHENATION_FREQUENCY_NONE
                importantForContentCapture = View.IMPORTANT_FOR_CONTENT_CAPTURE_NO_EXCLUDE_DESCENDANTS
            }).apply {
                setTextColor(AgentLogStyle.colorForLine(entry.text))
                text = terminalDisplayText(TerminalDisplayPolicy.boundedLine(entry.text, false))
            }
        }
    }
    private val refresh = Runnable { refreshPosted.set(false); settle() }
    private val listener: (TerminalLogEntry) -> Unit = { entry ->
        if (started) {
            if (entry.position == null) synchronized(unsaved) {
                unsaved.addLast(Row(entry, unsavedId.getAndDecrement()))
                unsavedChars += entry.text.length
                while (unsaved.size > 256 || unsavedChars > 256 * 1024) {
                    unsavedChars -= unsaved.removeFirst().entry.text.length
                    omittedUnsaved++
                }
            }
            version.incrementAndGet()
            scheduleRefresh()
        }
    }

    init {
        setBackgroundColor(Color.BLACK)
        setPadding(0, (48 * resources.displayMetrics.density).toInt(), 0, 0)
        list.adapter = adapter
        list.divider = null
        list.isStackFromBottom = true
        list.setOnScrollListener(object : AbsListView.OnScrollListener {
            override fun onScrollStateChanged(view: AbsListView, state: Int) {
                scrollState = state
                if (state != AbsListView.OnScrollListener.SCROLL_STATE_IDLE) {
                    following = false
                    invalidateReads()
                } else main.post { settle() }
            }
            override fun onScroll(view: AbsListView, first: Int, visible: Int, total: Int) = Unit
        })
        addView(list, LayoutParams(LayoutParams.MATCH_PARENT, LayoutParams.MATCH_PARENT))
        status.setTextColor(AgentLogStyle.TEXT_COLOR)
        status.setBackgroundColor(Color.BLACK)
        status.textSize = 10f
        status.visibility = GONE
        // Warnings overlay the list, so appearing/disappearing cannot shift its viewport.
        addView(status, LayoutParams(LayoutParams.MATCH_PARENT, LayoutParams.WRAP_CONTENT, Gravity.BOTTOM))
    }

    fun start() {
        if (started) return
        started = true
        appliedVersion = -1
        TerminalLog.addListener(listener)
        if (shown) afterLayout()
    }
    fun stop() { started = false; TerminalLog.removeListener(listener); invalidateReads() }
    fun close() { stop(); executor.shutdownNow() }
    fun show() { shown = true; appliedVersion = -1; visibility = VISIBLE; afterLayout() }
    fun hide() { shown = false; visibility = GONE; invalidateReads() }

    private fun invalidateReads() {
        generation++
        reads.cancel()
        loading = false
        main.removeCallbacks(refresh)
        refreshPosted.set(false)
    }
    private fun scheduleRefresh() {
        if (shown && started && refreshPosted.compareAndSet(false, true)) main.postDelayed(refresh, 100)
    }
    private fun settle() {
        if (!started || !shown || interacting || loading || positioning) return
        following = rows.isEmpty() || (!hasNewer && !list.canScrollVertically(1))
        when {
            following && version.get() != appliedVersion -> load()
            !following && hasOlder && list.firstVisiblePosition <= 8 -> load(before = rows.first { it.entry.position != null }.entry.position)
            !following && hasNewer && list.lastVisiblePosition >= rows.lastIndex - 8 -> load(after = rows.last { it.entry.position != null }.entry.position)
        }
    }
    private fun afterLayout() {
        if (layoutCallback) return
        layoutCallback = true
        val observer = list.viewTreeObserver
        observer.addOnPreDrawListener(object : ViewTreeObserver.OnPreDrawListener {
            override fun onPreDraw(): Boolean {
                if (observer.isAlive) observer.removeOnPreDrawListener(this)
                else list.viewTreeObserver.removeOnPreDrawListener(this)
                layoutCallback = false
                positioning = false
                settle()
                return true
            }
        })
        list.invalidate()
    }
    private fun load(before: TerminalLogPosition? = null, after: TerminalLogPosition? = null) {
        if (!started || !shown || interacting) return
        val token = ++generation
        val requestedVersion = version.get()
        val tail = before == null && after == null
        loading = true
        if (rows.isEmpty()) warning(context.getString(R.string.log_history_loading))
        reads.request {
            val result = try { Result.success(TerminalLog.page(context, before, after)) }
            catch (error: java.io.IOException) { Result.failure(error) }
            catch (error: SecurityException) { Result.failure(error) }
            catch (error: Throwable) { main.post { throw error }; return@request }
            val notice = TerminalLog.persistenceWarning
            main.post {
                if (token != generation || !started || !shown || interacting) return@post
                loading = false
                storageWarning = notice
                val anchor = rows.getOrNull(list.firstVisiblePosition)?.id
                val top = list.getChildAt(0)?.top ?: 0
                result.fold(onSuccess = { page ->
                    val incoming = page.entries.map { entry ->
                        val position = checkNotNull(entry.position)
                        Row(entry, position.generation * TerminalLogHistory.SEGMENT_BYTES + position.offset)
                    }
                    if (tail) {
                        rows.clear(); rows.addAll(incoming)
                        synchronized(unsaved) { rows.addAll(unsaved) }
                        hasOlder = page.hasOlder; hasNewer = page.hasNewer
                    } else if (before != null) {
                        rows.addAll(0, incoming); hasOlder = page.hasOlder
                    } else {
                        rows.addAll(incoming); hasNewer = page.hasNewer
                    }
                    var chars = rows.sumOf { it.entry.text.length }
                    while (rows.size > 512 || chars > 512 * 1024) {
                        val removeFirst = before == null
                        val removed = if (removeFirst) rows.removeAt(0) else rows.removeAt(rows.lastIndex)
                        check(tail || removed.id != anchor) { "Paging evicted the visible anchor" }
                        chars -= removed.entry.text.length
                        if (removeFirst) hasOlder = true else hasNewer = true
                    }
                    positioning = true
                    adapter.notifyDataSetChanged()
                    if (tail && following) list.setSelection(rows.lastIndex.coerceAtLeast(0))
                    else if (anchor != null) {
                        val index = rows.indexOfFirst { it.id == anchor }
                        check(index >= 0) { "Paging lost the visible anchor" }
                        list.setSelectionFromTop(index, top)
                    }
                    appliedVersion = requestedVersion
                    warning(storageWarning)
                    afterLayout()
                }, onFailure = { failure ->
                    if (failure is java.io.InterruptedIOException) return@post
                    // Retry on a new event or explicit reopen, not every empty/error layout.
                    appliedVersion = requestedVersion
                    if (following) {
                        synchronized(unsaved) { rows.removeAll { it.entry.position == null }; rows.addAll(unsaved) }
                        var chars = rows.sumOf { it.entry.text.length }
                        while (rows.size > 512 || chars > 512 * 1024) chars -= rows.removeAt(0).entry.text.length
                        positioning = true
                        adapter.notifyDataSetChanged()
                        list.setSelection(rows.lastIndex.coerceAtLeast(0))
                        afterLayout()
                    }
                    warning(context.getString(R.string.log_history_unavailable, failure.javaClass.simpleName))
                })
            }
        }
    }
    private fun warning(message: String?) {
        val lost = synchronized(unsaved) { omittedUnsaved }
        val overflow = if (lost > 0) resources.getQuantityString(R.plurals.log_history_unsaved_evicted, lost.coerceAtMost(Int.MAX_VALUE.toLong()).toInt(), lost) else null
        val text = listOfNotNull(message, overflow).joinToString("\n")
        status.text = text
        status.visibility = if (text.isEmpty()) GONE else VISIBLE
    }
}

/** Preserve native accessibility while observing the full touch, including a stationary press. */
internal class TerminalLogListView(context: Context) : ListView(context) {
    var onTouchChanged: ((Boolean) -> Unit)? = null
    override fun performClick(): Boolean = super.performClick()
    override fun dispatchTouchEvent(event: MotionEvent): Boolean {
        if (event.actionMasked == MotionEvent.ACTION_DOWN) onTouchChanged?.invoke(true)
        val handled = super.dispatchTouchEvent(event)
        if (event.actionMasked == MotionEvent.ACTION_UP || event.actionMasked == MotionEvent.ACTION_CANCEL) onTouchChanged?.invoke(false)
        return handled
    }
}
