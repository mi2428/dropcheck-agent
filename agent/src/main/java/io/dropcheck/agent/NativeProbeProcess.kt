package io.dropcheck.agent

import java.io.ByteArrayOutputStream
import java.io.IOException
import java.util.concurrent.TimeUnit

internal const val MAX_NATIVE_PROBE_OUTPUT_BYTES = 1_048_576

internal data class ProcessRun(
    val finished: Boolean,
    val exitCode: Int,
    val output: String,
    val error: String,
)

internal fun collectProbeProcess(process: Process, timeoutMs: Int): ProcessRun {
    val output = ByteArrayOutputStream()
    var exceededLimit = false
    var readFailure: IOException? = null
    val reader = Thread({
        try {
            val buffer = ByteArray(8192)
            while (true) {
                val count = process.inputStream.read(buffer)
                if (count < 0) break
                val captured = minOf(count, MAX_NATIVE_PROBE_OUTPUT_BYTES - output.size())
                output.write(buffer, 0, captured)
                if (captured < count) exceededLimit = true
            }
        } catch (e: IOException) {
            readFailure = e
        }
    }, "native-probe-output").apply { isDaemon = true }
    try {
        reader.start()
        val finished = process.waitFor(timeoutMs.toLong(), TimeUnit.MILLISECONDS)
        if (!finished) reapProbeProcess(process)
        reader.join(500)
        check(!reader.isAlive) { "native probe output reader did not finish" }
        // Never feed a truncated measurement to ping/trace/PMTU parsers.
        check(!exceededLimit) { "native probe output exceeded $MAX_NATIVE_PROBE_OUTPUT_BYTES bytes" }
        if (finished) readFailure?.let { throw it }
        val error = if (finished) "" else "process_timeout=${timeoutMs}ms" +
            (readFailure?.let { "; output_read=${it.javaClass.simpleName}:${it.message.orEmpty()}" } ?: "")
        return ProcessRun(finished, if (finished) process.exitValue() else -1,
            output.toString(Charsets.UTF_8.name()), error)
    } catch (e: InterruptedException) {
        Thread.currentThread().interrupt()
        throw e
    } finally {
        try {
            reapProbeProcess(process)
        } finally {
            try {
                process.inputStream.close()
            } finally {
                try {
                    process.errorStream.close()
                } finally {
                    try {
                        process.outputStream.close()
                    } finally {
                        joinProbeReader(reader)
                    }
                }
            }
        }
    }
}

private fun joinProbeReader(reader: Thread) {
    var interrupted = Thread.interrupted()
    try {
        val deadline = System.nanoTime() + TimeUnit.MILLISECONDS.toNanos(500)
        while (reader.isAlive) {
            val remainingMs = TimeUnit.NANOSECONDS.toMillis(deadline - System.nanoTime())
            check(remainingMs > 0) { "native probe output reader did not terminate" }
            try {
                reader.join(remainingMs)
            } catch (e: InterruptedException) {
                interrupted = true
            }
        }
    } finally {
        if (interrupted) Thread.currentThread().interrupt()
    }
}

private fun reapProbeProcess(process: Process) {
    var interrupted = Thread.interrupted()
    try {
        if (process.isAlive) process.destroyForcibly()
        val deadline = System.nanoTime() + TimeUnit.MILLISECONDS.toNanos(500)
        while (true) {
            try {
                check(process.waitFor((deadline - System.nanoTime()).coerceAtLeast(0), TimeUnit.NANOSECONDS)) {
                    "native probe process did not terminate"
                }
                return
            } catch (e: InterruptedException) {
                interrupted = true
                check(System.nanoTime() < deadline) { "native probe process cleanup interrupted" }
            }
        }
    } finally {
        if (interrupted) Thread.currentThread().interrupt()
    }
}
