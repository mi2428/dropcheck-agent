package io.dropcheck.agent

import android.app.Activity
import android.app.Application
import android.app.Instrumentation
import android.content.Intent
import android.os.Bundle
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference

/** Real Activity owner and completion listener; actions only wait on local latches. */
class ShellLifecycleInstrumentation : Instrumentation() {
    override fun onCreate(arguments: Bundle?) { super.onCreate(arguments); start() }

    override fun onStart() {
        val result = Bundle()
        val application = targetContext.applicationContext as Application
        var initial: MainActivity? = null
        val current = AtomicReference<MainActivity>()
        val recreated = CountDownLatch(1)
        val destroyed = CountDownLatch(1)
        val release = CountDownLatch(1)
        val finishRelease = CountDownLatch(1)
        val callbacks = object : Application.ActivityLifecycleCallbacks {
            override fun onActivityResumed(activity: Activity) {
                if (activity is MainActivity && activity !== initial) {
                    current.set(activity)
                    recreated.countDown()
                }
            }
            override fun onActivityDestroyed(activity: Activity) {
                if (activity === current.get()) destroyed.countDown()
            }
            override fun onActivityCreated(activity: Activity, state: Bundle?) = Unit
            override fun onActivityStarted(activity: Activity) = Unit
            override fun onActivityPaused(activity: Activity) = Unit
            override fun onActivityStopped(activity: Activity) = Unit
            override fun onActivitySaveInstanceState(activity: Activity, state: Bundle) = Unit
        }
        try {
            val first = startActivitySync(Intent(targetContext, MainActivity::class.java)
                .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_MULTIPLE_TASK)) as MainActivity
            initial = first
            current.set(first)
            waitForIdleSync()
            runOnMainSync { application.registerActivityLifecycleCallbacks(callbacks) }
            val owner = first.field<ShellActionOwner>("shellOwner")
            val started = CountDownLatch(1)
            val marker = "synthetic lifecycle retained result"
            runOnMainSync {
                first.startAction("synthetic.recreate") {
                    started.countDown()
                    check(release.await(10, TimeUnit.SECONDS)) { "synthetic action timed out" }
                    AgentShellResult(true, listOf(marker))
                }
            }
            await(started, "action did not start")
            check(owner.busy)
            runOnMainSync { first.recreate() }
            await(recreated, "Activity did not recreate")
            waitForIdleSync()
            val next = checkNotNull(current.get())
            check(next !== first)
            check(next.field<ShellActionOwner>("shellOwner") === owner) { "in-flight owner was replaced" }
            check(next.field<AgentBlockHistory>("shellTranscript") === first.field<AgentBlockHistory>("shellTranscript"))
            check(owner.busy) { "recreate canceled the in-flight action" }
            release.countDown()
            awaitIdle(owner)
            runOnMainSync {
                check(next.historyCount(marker) == 1) { "result was lost or delivered more than once" }
                check(next.viewCount(marker) == 1) { "new screen did not receive the result" }
                check(first.viewCount(marker) == 0) { "detached screen received the result" }
            }
            // A reattach must not deliver the already consumed result a second time.
            runOnMainSync {
                val listener = next.field<(AgentShellResult) -> Unit>("shellCompletionListener")
                owner.detach(listener)
                owner.attach(listener)
            }
            waitForIdleSync()
            runOnMainSync { check(next.historyCount(marker) == 1 && next.viewCount(marker) == 1) }

            val beforeFailure = next.field<AgentBlockHistory>("shellTranscript").blocks.size
            runOnMainSync { next.startAction("synthetic.exception") { throw IllegalStateException("synthetic failure") } }
            awaitIdle(owner)
            runOnMainSync {
                val history = next.field<AgentBlockHistory>("shellTranscript")
                check(history.blocks.size == beforeFailure + 1) { "exception result was not delivered" }
                check(history.blocks.last().fullText.contains("failed", ignoreCase = true))
            }
            val retry = "synthetic lifecycle retry result"
            runOnMainSync { next.startAction("synthetic.retry") { AgentShellResult(true, listOf(retry)) } }
            awaitIdle(owner)
            runOnMainSync { check(next.historyCount(retry) == 1 && next.viewCount(retry) == 1) }

            val finishStarted = CountDownLatch(1)
            val interrupted = CountDownLatch(1)
            runOnMainSync {
                next.startAction("synthetic.finish") {
                    finishStarted.countDown()
                    try {
                        check(finishRelease.await(10, TimeUnit.SECONDS)) { "finish did not interrupt action" }
                        AgentShellResult(true)
                    } catch (failure: InterruptedException) {
                        interrupted.countDown()
                        throw failure
                    }
                }
            }
            await(finishStarted, "finish action did not start")
            check(owner.busy)
            runOnMainSync { next.finish() }
            await(destroyed, "Activity did not finish")
            await(interrupted, "finish did not interrupt the owner action")
            result.putString("stream", "PASS: real MainActivity in-flight recreate retained owner; result delivered once to new screen; exception cleared busy and retry completed; finish interrupted action. Synthetic actions only; no command dispatch or network acquisition.\n")
            finish(Activity.RESULT_OK, result)
        } catch (failure: Throwable) {
            result.putString("stream", "FAIL: ${failure.javaClass.simpleName}: ${failure.message}\n")
            finish(Activity.RESULT_CANCELED, result)
        } finally {
            release.countDown()
            finishRelease.countDown()
            runOnMainSync {
                application.unregisterActivityLifecycleCallbacks(callbacks)
                current.get()?.let { if (!it.isDestroyed) it.finish() }
            }
        }
    }

    private fun await(latch: CountDownLatch, message: String) { check(latch.await(5, TimeUnit.SECONDS)) { message } }

    private fun awaitIdle(owner: ShellActionOwner) {
        for (attempt in 0 until 100) {
            waitForIdleSync()
            if (!owner.busy) return
            Thread.sleep(20)
        }
        error("owner remained busy")
    }

    private fun MainActivity.startAction(name: String, action: () -> AgentShellResult) {
        javaClass.getDeclaredMethod("runShellLinesCommand", String::class.java, Function0::class.java)
            .apply { isAccessible = true }.invoke(this, name, action)
    }

    private fun MainActivity.historyCount(marker: String): Int = field<AgentBlockHistory>("shellTranscript").blocks.count { marker in it.fullText }
    private fun MainActivity.viewCount(marker: String): Int = field<Map<AgentPresentationBlock, *>>("shellBlockViews").keys.count { marker in it.fullText }

    @Suppress("UNCHECKED_CAST")
    private fun <T> MainActivity.field(name: String): T = javaClass.getDeclaredField(name).apply { isAccessible = true }.get(this) as T
}
