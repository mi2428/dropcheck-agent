package io.dropcheck.agent

import android.content.Context
import io.dropcheck.agent.grpc.FestivalClearResult
import io.dropcheck.agent.grpc.FestivalRunArchive
import io.dropcheck.agent.grpc.FestivalRunSummary
import io.dropcheck.agent.grpc.FestivalRuns
import java.io.File

/** File-backed archive store for standalone measurement results. */
internal class FestivalResultStore internal constructor(private val runsDir: File) {
	constructor(context: Context) : this(File(context.filesDir, "festival/runs"))

    data class Stats(
        val storedRuns: Int,
        val unsyncedRuns: Int,
        val storedBytes: Long,
        val last: FestivalRunSummary?,
    )

    @Synchronized
    fun save(archive: FestivalRunArchive) {
        runsDir.mkdirs()
        runFile(archive.summary.runId).writeBytes(archive.toByteArray())
    }

    @Synchronized
    fun list(includeSynced: Boolean, limit: Int): FestivalRuns {
        val archives = loadArchives()
        val filtered = archives
            .filter { includeSynced || !it.summary.synced }
            .sortedByDescending { it.summary.startedUnixMs }
            .let { if (limit > 0) it.take(limit) else it }
        return FestivalRuns.newBuilder()
            .addAllRuns(filtered.map { it.summary })
            .setTotalRuns(archives.size)
            .setUnsyncedRuns(archives.count { !it.summary.synced })
            .build()
    }

    @Synchronized
    fun load(runId: String): FestivalRunArchive? {
        val file = runFile(runId)
        if (!file.exists()) return null
        return runCatching { FestivalRunArchive.parseFrom(file.readBytes()) }.getOrNull()
    }

    @Synchronized
    fun markSynced(runId: String): FestivalRunArchive? {
        val archive = load(runId) ?: return null
        val updated = archive.toBuilder()
            .setSummary(archive.summary.toBuilder().setSynced(true))
            .build()
        save(updated)
        return updated
    }

    @Synchronized
    fun clear(syncedOnly: Boolean, all: Boolean): FestivalClearResult {
        var removedRuns = 0
        var removedBytes = 0L
        for (file in runFiles()) {
            val archive = runCatching { FestivalRunArchive.parseFrom(file.readBytes()) }.getOrNull()
            val remove = all || (syncedOnly && archive?.summary?.synced == true)
            if (remove) {
                removedBytes += file.length()
                if (file.delete()) removedRuns += 1
            }
        }
        return FestivalClearResult.newBuilder()
            .setRemovedRuns(removedRuns)
            .setRemovedBytes(removedBytes)
            .build()
    }

    @Synchronized
    fun stats(): Stats {
        val archives = loadArchives()
        return Stats(
            storedRuns = archives.size,
            unsyncedRuns = archives.count { !it.summary.synced },
            storedBytes = runFiles().sumOf { it.length() },
            last = archives.maxByOrNull { it.summary.finishedUnixMs }?.summary,
        )
    }

    @Synchronized
    fun enforce(retentionMs: Long, maxBytes: Long): String {
        val now = System.currentTimeMillis()
        var removed = 0
        for (archive in loadArchives()) {
            if (!archive.summary.synced) continue
            if (retentionMs > 0 && archive.summary.finishedUnixMs > 0 && now - archive.summary.finishedUnixMs > retentionMs) {
                if (runFile(archive.summary.runId).delete()) removed += 1
            }
        }
        if (maxBytes > 0) {
            val synced = loadArchives()
                .filter { it.summary.synced }
                .sortedBy { it.summary.finishedUnixMs }
            for (archive in synced) {
                if (runFiles().sumOf { it.length() } <= maxBytes) break
                if (runFile(archive.summary.runId).delete()) removed += 1
            }
        }
        val bytes = runFiles().sumOf { it.length() }
        return when {
            maxBytes > 0 && bytes > maxBytes -> "store over budget with unsynced runs: bytes=$bytes max=$maxBytes"
            removed > 0 -> "removed synced runs=$removed"
            else -> ""
        }
    }

    private fun loadArchives(): List<FestivalRunArchive> {
        return runFiles().mapNotNull { file ->
            runCatching { FestivalRunArchive.parseFrom(file.readBytes()) }.getOrNull()
        }
    }

    private fun runFiles(): List<File> {
        return runsDir.listFiles { file -> file.isFile && file.extension == "pb" }
            ?.toList()
            .orEmpty()
    }

    private fun runFile(runId: String): File {
        val safe = runId.replace(Regex("[^A-Za-z0-9._-]+"), "_").ifBlank { "unknown" }
        return File(runsDir, "$safe.pb")
    }
}
