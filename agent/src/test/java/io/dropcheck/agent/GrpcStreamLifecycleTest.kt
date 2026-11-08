package io.dropcheck.agent

import io.grpc.stub.StreamObserver
import java.util.Collections
import java.util.concurrent.Callable
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.TimeoutException
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class GrpcStreamLifecycleTest {
    private class RecordingObserver : StreamObserver<String> {
        val events = Collections.synchronizedList(mutableListOf<String>())
        @Volatile var writeActive = false
        @Volatile var completedDuringWrite = false
        var entered: CountDownLatch? = null
        var release: CountDownLatch? = null

        override fun onNext(value: String) {
            writeActive = true
            entered?.countDown()
            release?.await()
            events += "next:$value"
            writeActive = false
        }

        override fun onError(t: Throwable) {
            events += "error:${t.javaClass.simpleName}"
        }

        override fun onCompleted() {
            completedDuringWrite = writeActive
            events += "completed"
        }
    }

    @Test
    fun completionWaitsForInFlightWriteAndRunsOnce() {
        val guard = StreamWriteGuard()
        val observer = RecordingObserver().apply {
            entered = CountDownLatch(1)
            release = CountDownLatch(1)
        }
        val writer = Thread { guard.write { observer.onNext("first") } }
        writer.start()
        try {
            assertTrue(observer.entered!!.await(5, TimeUnit.SECONDS))

            val completer = Executors.newSingleThreadExecutor()
            try {
                val completion = completer.submit(Callable { guard.complete { observer.onCompleted() } })
                try {
                    completion.get(200, TimeUnit.MILLISECONDS)
                    throw AssertionError("completion ran while a write was in flight")
                } catch (expected: TimeoutException) {
                    // Expected: completion is waiting for the write.
                }

                observer.release!!.countDown()
                writer.join(5_000)
                assertTrue(completion.get(5, TimeUnit.SECONDS))
                assertEquals(listOf("next:first", "completed"), observer.events)
                assertFalse(observer.completedDuringWrite)
            } finally {
                completer.shutdownNow()
            }
        } finally {
            observer.release!!.countDown()
            writer.join(5_000)
        }
    }

    @Test
    fun writeAfterCompletionIsDropped() {
        val guard = StreamWriteGuard()
        val observer = RecordingObserver()

        assertTrue(guard.complete { observer.onCompleted() })
        assertFalse(guard.write { observer.onNext("late") })
        assertFalse(guard.complete { observer.onCompleted() })
        assertEquals(listOf("completed"), observer.events)
    }
}
