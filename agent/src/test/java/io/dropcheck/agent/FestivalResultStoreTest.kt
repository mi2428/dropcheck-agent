package io.dropcheck.agent

import io.dropcheck.agent.grpc.FestivalRunArchive
import io.dropcheck.agent.grpc.FestivalRunSummary
import java.nio.file.Files
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class FestivalResultStoreTest {
    @Test
    fun storesListsMarksAndClearsRuns() {
        val dir = Files.createTempDirectory("festival-runs").toFile()
        try {
            val store = FestivalResultStore(dir)
            store.save(archive("run-1", started = 100, synced = false))
            store.save(archive("run-2", started = 200, synced = true))

            val unsynced = store.list(includeSynced = false, limit = 0)
            assertEquals(listOf("run-1"), unsynced.runsList.map { it.runId })
            assertEquals(2, unsynced.totalRuns)
            assertEquals(1, unsynced.unsyncedRuns)

            val marked = store.markSynced("run-1")
            assertTrue(marked!!.summary.synced)
            assertTrue(store.list(includeSynced = true, limit = 0).runsList.all { it.synced })

            val cleared = store.clear(syncedOnly = true, all = false)
            assertEquals(2, cleared.removedRuns)
            assertEquals(0, store.stats().storedRuns)
        } finally {
            dir.deleteRecursively()
        }
    }

    @Test
    fun neverDeletesUnsyncedRunsForStoreBudget() {
        val dir = Files.createTempDirectory("festival-runs").toFile()
        try {
            val store = FestivalResultStore(dir)
            store.save(archive("run-1", started = 100, synced = false))

            val message = store.enforce(retentionMs = 0, maxBytes = 1)

            assertTrue(message.contains("unsynced"))
            assertFalse(store.list(includeSynced = false, limit = 0).runsList.isEmpty())
        } finally {
            dir.deleteRecursively()
        }
    }

    private fun archive(runId: String, started: Long, synced: Boolean): FestivalRunArchive {
        return FestivalRunArchive.newBuilder()
            .setSummary(FestivalRunSummary.newBuilder()
                .setRunId(runId)
                .setPlanName("lab")
                .setStartedUnixMs(started)
                .setFinishedUnixMs(started + 10)
                .setStatus("ok")
                .setSynced(synced))
            .build()
    }
}
