package io.dropcheck.agent

import java.util.concurrent.TimeUnit

internal data class ProcessRun(
    val finished: Boolean,
    val exitCode: Int,
    val output: String,
    val error: String,
)

internal fun collectProbeProcess(process: Process, timeoutMs: Int): ProcessRun {
    try {
        val finished = process.waitFor(timeoutMs.toLong(), TimeUnit.MILLISECONDS)
        if (!finished) reapProbeProcess(process)
        val output = runCatching { process.inputStream.bufferedReader().readText() }.getOrDefault("")
        return ProcessRun(finished, if (finished) process.exitValue() else -1, output,
            if (finished) "" else "process_timeout=${timeoutMs}ms")
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
                    process.outputStream.close()
                }
            }
        }
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
