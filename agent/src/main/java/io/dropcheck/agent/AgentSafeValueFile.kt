package io.dropcheck.agent

import java.io.DataOutputStream
import java.io.File
import java.io.IOException
import java.io.RandomAccessFile

/** One current oversize result, sanitized label/value data only. No command or log archive. */
internal class AgentSafeValueFile private constructor(private val file: File, val valueCount: Int) {
    fun labels(start: Int, count: Int = 25): List<String> = read(start, count, values = false)
    fun value(index: Int): String = read(index, 1, values = true).single()

    private fun read(start: Int, count: Int, values: Boolean): List<String> {
        require(start >= 0 && start < valueCount && count > 0)
        return RandomAccessFile(file, "r").use { input ->
            if (input.readInt() != MAGIC) throw IOException("Invalid safe-value file")
            val result = mutableListOf<String>()
            repeat(valueCount) { index ->
                val labelSize = input.readInt()
                val valueSize = input.readInt()
                if (labelSize < 0 || valueSize < 0 || labelSize.toLong() + valueSize > input.length() - input.filePointer) throw IOException("Invalid safe-value record")
                if (index >= start && result.size < count) {
                    val label = ByteArray(labelSize).also(input::readFully).toString(Charsets.UTF_8)
                    if (values) result += ByteArray(valueSize).also(input::readFully).toString(Charsets.UTF_8)
                    else { result += label; input.seek(input.filePointer + valueSize) }
                } else input.seek(input.filePointer + labelSize + valueSize)
                if (result.size == count) return@use result
            }
            result
        }
    }

    fun delete() { check(!file.exists() || file.delete()) { "Cannot remove safe-value file" } }

    companion object {
        private const val MAGIC = 0x44433337
        const val MAX_BYTES = 32L * 1024 * 1024

        fun clearAbandoned(directory: File) {
            directory.listFiles { file -> file.name.startsWith("shell-safe-values-") && file.name.endsWith(".bin") }?.forEach { file ->
                check(file.delete()) { "Cannot remove abandoned safe-value file" }
            }
        }

        fun write(block: AgentPresentationBlock, directory: File): AgentSafeValueFile {
            val file = File.createTempFile("shell-safe-values-", ".bin", directory)
            var count = 0
            var size = 4L
            try {
                DataOutputStream(file.outputStream().buffered()).use { output ->
                    output.writeInt(MAGIC)
                    block.safeValues().forEach { (label, value) ->
                        val labelBytes = label.toByteArray(Charsets.UTF_8)
                        val valueBytes = value.toByteArray(Charsets.UTF_8)
                        size += 8L + labelBytes.size + valueBytes.size
                        if (size > MAX_BYTES) throw IOException("Safe-value result exceeds 32MiB quota; no value was silently clipped")
                        output.writeInt(labelBytes.size)
                        output.writeInt(valueBytes.size)
                        output.write(labelBytes)
                        output.write(valueBytes)
                        count++
                    }
                }
                require(count > 0) { "Empty oversize safe-value result" }
                return AgentSafeValueFile(file, count)
            } catch (failure: Throwable) {
                if (!file.delete()) failure.addSuppressed(IOException("Cannot remove incomplete safe-value file"))
                throw failure
            }
        }
    }
}
