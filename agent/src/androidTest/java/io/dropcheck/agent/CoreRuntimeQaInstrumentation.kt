package io.dropcheck.agent

import android.app.Activity
import android.app.Instrumentation
import android.content.Context
import android.content.ContextWrapper
import android.os.Bundle
import android.os.Looper
import java.io.File
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean

/**
 * Verifies that the shared Wi-Fi capture entry moves main-thread snapshots to
 * the logger executor. Observation is passive: no scan, connect, or permission
 * change, and synthetic lines go to an isolated cache store.
 */
class CoreRuntimeQaInstrumentation : Instrumentation() {
    override fun onCreate(arguments: Bundle?) {
        super.onCreate(arguments)
        start()
    }

    override fun onStart() {
        val report = Bundle()
        var code = Activity.RESULT_CANCELED
        var logger: WifiEventLogger? = null
        var listener: ((TerminalLogEntry) -> Unit)? = null
        try {
            val storage = File(targetContext.cacheDir, "core-runtime-qa-${System.nanoTime()}").apply { check(mkdir()) }
            // The isolated store is intentionally left behind: callbacks still
            // running on ConnectivityThread may append after stop() returns, so
            // the supervisor removes core-runtime-qa-* after this process exits.
            val isolated = object : ContextWrapper(targetContext) {
                override fun getApplicationContext(): Context = this
                override fun getExternalFilesDir(type: String?): File = storage
                override fun getFilesDir(): File = storage
                override fun getCacheDir(): File = storage
            }
            val marker = "qa-main-thread-${System.nanoTime()}"
            val delivered = CountDownLatch(1)
            val deliveredOnMain = AtomicBoolean(true)
            val snapshotDelivered = AtomicBoolean(false)
            val snapshotFailed = AtomicBoolean(false)
            val observer: (TerminalLogEntry) -> Unit = { entry ->
                if (entry.text.contains("reason=$marker")) {
                    if (entry.text.contains("event=wifi.snapshot.failed")) {
                        snapshotFailed.set(true)
                    } else if (entry.text.contains("event=wifi.snapshot.")) {
                        snapshotDelivered.set(true)
                        deliveredOnMain.set(Looper.myLooper() == Looper.getMainLooper())
                    }
                    delivered.countDown()
                }
            }
            TerminalLog.addListener(observer)
            listener = observer
            val active = WifiEventLogger(isolated)
            logger = active
            runOnMainSync {
                check(Looper.myLooper() == Looper.getMainLooper())
                active.start()
                active.capture(marker, emptyList())
            }
            check(delivered.await(10, TimeUnit.SECONDS)) { "reason marker was not logged" }
            check(!snapshotFailed.get()) { "snapshot failed for the reason marker" }
            check(snapshotDelivered.get()) { "successful snapshot event was not logged" }
            check(!deliveredOnMain.get()) { "snapshot ran on the main thread" }
            report.putString("stream", "PASS: main-thread capture delivered off-main reason=$marker; passive observation only.\n")
            code = Activity.RESULT_OK
        } catch (failure: Throwable) {
            report.putString("stream", "FAIL: ${failure.javaClass.simpleName}: ${failure.message}\n")
        } finally {
            val stopFailure = runCatching { logger?.stop() }.exceptionOrNull()
            listener?.let { TerminalLog.removeListener(it) }
            if (stopFailure != null) {
                report.putString("stream", report.getString("stream").orEmpty() +
                    "FAIL: logger.stop ${stopFailure.javaClass.simpleName}: ${stopFailure.message}\n")
                code = Activity.RESULT_CANCELED
            }
        }
        finish(code, report)
    }
}
