package io.dropcheck.agent

import android.annotation.SuppressLint
import android.content.Context
import android.net.wifi.WifiManager
import android.os.Build
import io.dropcheck.agent.grpc.CommandLog
import io.dropcheck.agent.grpc.CommandResult
import io.dropcheck.agent.grpc.ConnectWifi
import io.dropcheck.agent.grpc.DeviceInfo
import io.dropcheck.agent.grpc.DnsRecordType
import io.dropcheck.agent.grpc.HttpCheck
import io.dropcheck.agent.grpc.NetworkSelector
import io.dropcheck.agent.grpc.Ping
import io.dropcheck.agent.grpc.ResolveDns
import io.dropcheck.agent.grpc.RunCommand
import io.dropcheck.agent.grpc.StandaloneFesta
import io.dropcheck.agent.grpc.StandaloneMeasurementStep
import io.dropcheck.agent.grpc.StandaloneRunArchive
import io.dropcheck.agent.grpc.StandaloneRunSummary
import io.dropcheck.agent.grpc.StandaloneWifiGroup
import io.dropcheck.agent.grpc.WaitWifiConnected
import java.security.MessageDigest
import java.time.Instant
import java.util.UUID
import java.util.concurrent.Executors
import java.util.concurrent.Future
import java.util.concurrent.TimeUnit

/** Runs persisted standalone connectivity festas while the controller is absent. */
internal class StandaloneRunner(private val context: Context) {
    private val appContext = context.applicationContext
    private val executor = Executors.newSingleThreadExecutor()
    private val configStore = StandaloneConfigStore(appContext)
    private val resultStore = StandaloneResultStore(appContext)
    private val lastRuns = mutableMapOf<String, Long>()
    private var future: Future<*>? = null

    @Synchronized
    fun refresh() {
        val config = configStore.load()
        if (!config.enabled) {
            stop()
            return
        }
        if (future?.isDone == false) return
        future = executor.submit { loop() }
    }

    @Synchronized
    fun stop() {
        future?.cancel(true)
        future = null
        StandaloneRuntimeState.running.set(false)
        StandaloneRuntimeState.currentRunId.set("")
        AgentStatusBroadcast.send(appContext)
    }

    fun shutdown() {
        stop()
        executor.shutdownNow()
    }

    private fun loop() {
        while (!Thread.currentThread().isInterrupted) {
            val config = configStore.load()
            if (!config.enabled) break
            val enabledFestas = config.festasList.filter { it.enabled }
            if (enabledFestas.isEmpty()) {
                StandaloneRuntimeState.message.set("standalone enabled without enabled festas")
                sleepInterruptibly(1_000)
                continue
            }
            val now = System.currentTimeMillis()
            var ran = false
            for (festa in enabledFestas) {
                val intervalMs = festa.intervalMs.takeIf { it > 0 }?.toLong() ?: DEFAULT_FESTA_INTERVAL_MS
                val last = lastRuns[festa.name] ?: 0L
                if (last == 0L || now - last >= intervalMs) {
                    val archive = runFesta(festa, save = true)
                    lastRuns[festa.name] = System.currentTimeMillis()
                    val cleanup = resultStore.enforce(retentionMs(config.retentionMs), maxBytes(config.maxBytes))
                    StandaloneRuntimeState.message.set(cleanup.ifBlank { "last run ${archive.summary.status}" })
                    ran = true
                }
            }
            sleepInterruptibly(if (ran) 250 else 1_000)
        }
        StandaloneRuntimeState.running.set(false)
        StandaloneRuntimeState.currentRunId.set("")
    }

    fun runOnce(festaName: String, save: Boolean): StandaloneRunArchive {
        val config = configStore.load()
        val candidates = if (festaName.isBlank()) {
            config.festasList.filter { it.enabled }
        } else {
            config.festasList.filter { it.name == festaName }
        }
        if (candidates.isEmpty()) {
            return failedArchive(festaName.ifBlank { "unspecified" }, "standalone festa not found or not enabled")
        }
        if (festaName.isBlank() && candidates.size > 1) {
            return failedArchive("multiple", "multiple enabled festas; specify festa <name>")
        }
        return runFesta(candidates.first(), save)
    }

    fun runFesta(festa: StandaloneFesta, save: Boolean): StandaloneRunArchive {
        val runId = "${Instant.now().toEpochMilli()}-${UUID.randomUUID()}"
        val logs = mutableListOf<CommandLog>()
        val logger = archiveLogger(logs)
        val started = System.currentTimeMillis()
        StandaloneRuntimeState.running.set(true)
        StandaloneRuntimeState.currentRunId.set(runId)
        StandaloneRuntimeState.message.set("running")
        AgentStatusBroadcast.send(appContext)
        TerminalLog.info(appContext, "standalone run start run_id=$runId festa=${festa.name.ifBlank { "-" }} wifi_groups=${festa.wifiGroupsCount}")

        val steps = mutableListOf<StandaloneMeasurementStep>()
        var failed = 0
        for ((groupIndex, group) in festa.wifiGroupsList.withIndex()) {
            var stepIndex = 1
            val resolvedEssid = resolveEssid(group)
            fun runStep(name: String, command: RunCommand): Boolean {
                val measurement = executeStep(
                    logger = logger,
                    wifiGroupIndex = groupIndex + 1,
                    wifiGroupName = group.name.ifBlank { "wifi_group_${groupIndex + 1}" },
                    stepIndex = stepIndex++,
                    stepName = name,
                    command = command,
                )
                steps += measurement
                val ok = measurement.hasResult() && measurement.result.status == CommandResult.Status.STATUS_OK && measurement.error.isBlank()
                if (!ok) failed += 1
                return ok
            }

            if (resolvedEssid.isBlank()) {
                steps += failedStep(groupIndex + 1, group.name, stepIndex++, "resolve", "wifi-group requires essid or scan-visible bssid")
                failed += 1
                continue
            }
            val connected = runStep("connect", connectCommand(group, resolvedEssid))
            val ready = connected && runStep("wait_connected", waitCommand(group, resolvedEssid))
            if (ready) {
                runChecks(festa, groupIndex + 1, group.name, resolvedEssid, steps, logger) { failed += 1 }
            }
        }

        val finished = System.currentTimeMillis()
        val summary = StandaloneRunSummary.newBuilder()
            .setRunId(runId)
            .setFestaName(festa.name)
            .setConfigHash(configHash(festa))
            .setStartedUnixMs(started)
            .setFinishedUnixMs(finished)
            .setStatus(if (failed == 0) "ok" else "failed")
            .setWifiGroupCount(festa.wifiGroupsCount)
            .setStepCount(steps.size)
            .setFailedStepCount(failed)
            .setSynced(false)
            .setMessage(if (failed == 0) "connectivity completed" else "connectivity completed with failed checks")
            .build()
        val archive = StandaloneRunArchive.newBuilder()
            .setSummary(summary)
            .setDevice(deviceInfo())
            .setFesta(festa)
            .addAllSteps(steps)
            .addAllLogs(logs)
            .build()
        if (save) resultStore.save(archive)
        StandaloneRuntimeState.running.set(false)
        StandaloneRuntimeState.currentRunId.set("")
        StandaloneRuntimeState.message.set(summary.message)
        AgentStatusBroadcast.send(appContext)
        TerminalLog.info(appContext, "standalone run end run_id=$runId status=${summary.status} steps=${summary.stepCount} failed=${summary.failedStepCount}")
        return archive
    }

    private fun runChecks(
        festa: StandaloneFesta,
        wifiGroupIndex: Int,
        wifiGroupName: String,
        essid: String,
        steps: MutableList<StandaloneMeasurementStep>,
        logger: CommandLogger,
        onFailure: () -> Unit,
    ) {
        var stepIndex = steps.count { it.wifiGroupIndex == wifiGroupIndex } + 1
        fun runCheck(name: String, command: RunCommand) {
            val measurement = executeStep(logger, wifiGroupIndex, wifiGroupName, stepIndex++, name, command)
            steps += measurement
            val ok = measurement.hasResult() && measurement.result.status == CommandResult.Status.STATUS_OK && measurement.error.isBlank()
            if (!ok) onFailure()
        }
        val selector = NetworkSelector.newBuilder().setSsid(essid)
        val checks = festa.checks
        if (checks.dns.enabled) {
            val qtypes = checks.dns.qtypesList.takeIf { it.isNotEmpty() } ?: listOf(DnsRecordType.DNS_RECORD_TYPE_A)
            runCheck("dns", RunCommand.newBuilder()
                .setLabel("standalone dns ${checks.dns.name}")
                .setResolveDns(ResolveDns.newBuilder()
                    .setName(checks.dns.name)
                    .addAllQtypes(qtypes)
                    .setTimeoutMs(checks.dns.timeoutMs.takeIf { it > 0 } ?: DEFAULT_CHECK_TIMEOUT_MS)
                    .setSelector(selector))
                .build())
        }
        if (checks.ping.enabled) {
            runCheck("ping", RunCommand.newBuilder()
                .setLabel("standalone ping ${checks.ping.host}")
                .setPing(Ping.newBuilder()
                    .setHost(checks.ping.host)
                    .setCount(checks.ping.count.takeIf { it > 0 } ?: 3)
                    .setTimeoutMs(checks.ping.timeoutMs.takeIf { it > 0 } ?: DEFAULT_CHECK_TIMEOUT_MS)
                    .setSizeBytes(checks.ping.sizeBytes)
                    .setSelector(selector))
                .build())
        }
        if (checks.http.enabled) {
            runCheck("http", RunCommand.newBuilder()
                .setLabel("standalone http ${checks.http.url}")
                .setHttpCheck(HttpCheck.newBuilder()
                    .setUrl(checks.http.url)
                    .setExpectedStatus(checks.http.expectedStatus.takeIf { it > 0 } ?: 204)
                    .setTimeoutMs(checks.http.timeoutMs.takeIf { it > 0 } ?: DEFAULT_CHECK_TIMEOUT_MS)
                    .setSelector(selector))
                .build())
        }
    }

    private fun executeStep(
        logger: CommandLogger,
        wifiGroupIndex: Int,
        wifiGroupName: String,
        stepIndex: Int,
        stepName: String,
        command: RunCommand,
    ): StandaloneMeasurementStep {
        val started = System.currentTimeMillis()
        val builder = StandaloneMeasurementStep.newBuilder()
            .setWifiGroupIndex(wifiGroupIndex)
            .setWifiGroupName(wifiGroupName)
            .setStepIndex(stepIndex)
            .setStepName(stepName)
            .setAttempt(1)
            .setStartedUnixMs(started)
            .setCommand(command)
        if (isStandaloneControlCommand(command)) {
            return builder
                .setFinishedUnixMs(System.currentTimeMillis())
                .setError("standalone control commands cannot be nested in a standalone run")
                .build()
        }
        return try {
            val result = CommandExecutor(appContext, logger).execute(command)
            builder
                .setFinishedUnixMs(System.currentTimeMillis())
                .setResult(result)
                .build()
        } catch (e: InterruptedException) {
            Thread.currentThread().interrupt()
            builder
                .setFinishedUnixMs(System.currentTimeMillis())
                .setError("interrupted")
                .build()
        } catch (e: Exception) {
            builder
                .setFinishedUnixMs(System.currentTimeMillis())
                .setError(e.toString())
                .build()
        }
    }

    private fun failedStep(index: Int, groupName: String, stepIndex: Int, stepName: String, error: String): StandaloneMeasurementStep {
        val now = System.currentTimeMillis()
        return StandaloneMeasurementStep.newBuilder()
            .setWifiGroupIndex(index)
            .setWifiGroupName(groupName)
            .setStepIndex(stepIndex)
            .setStepName(stepName)
            .setAttempt(1)
            .setStartedUnixMs(now)
            .setFinishedUnixMs(now)
            .setError(error)
            .build()
    }

    private fun connectCommand(group: StandaloneWifiGroup, essid: String): RunCommand {
        return RunCommand.newBuilder()
            .setLabel("standalone connect ${group.name.ifBlank { essid }}")
            .setConnectWifi(ConnectWifi.newBuilder()
                .setSsid(essid)
                .setPassphrase(group.passphrase)
                .setSecurity(group.security)
                .setBssid(group.bssid)
                .setBand(group.band)
                .setTimeoutMs(group.timeoutMs.takeIf { it > 0 } ?: DEFAULT_WIFI_TIMEOUT_MS))
            .build()
    }

    private fun waitCommand(group: StandaloneWifiGroup, essid: String): RunCommand {
        return RunCommand.newBuilder()
            .setLabel("standalone wait ${group.name.ifBlank { essid }}")
            .setWaitWifiConnected(WaitWifiConnected.newBuilder()
                .setSsid(essid)
                .setBssid(group.bssid)
                .setSecurity(group.security)
                .setBand(group.band)
                .setRequireIp(group.requireIp)
                .setRequireValidated(group.requireValidated)
                .setTimeoutMs(group.timeoutMs.takeIf { it > 0 } ?: DEFAULT_WIFI_TIMEOUT_MS))
            .build()
    }

    @Suppress("DEPRECATION")
    @SuppressLint("MissingPermission")
    private fun resolveEssid(group: StandaloneWifiGroup): String {
        if (group.essid.isNotBlank()) return group.essid
        if (group.bssid.isBlank()) return ""
        val wifi = appContext.getSystemService(WifiManager::class.java)
        return runCatching {
            wifi.scanResults.orEmpty()
                .firstOrNull { it.BSSID.equals(group.bssid, ignoreCase = true) }
                ?.SSID
                .orEmpty()
        }.getOrDefault("")
    }

    private fun failedArchive(festaName: String, message: String): StandaloneRunArchive {
        val now = System.currentTimeMillis()
        val runId = "${now}-${UUID.randomUUID()}"
        val summary = StandaloneRunSummary.newBuilder()
            .setRunId(runId)
            .setFestaName(festaName)
            .setStartedUnixMs(now)
            .setFinishedUnixMs(now)
            .setStatus("failed")
            .setFailedStepCount(1)
            .setMessage(message)
            .build()
        return StandaloneRunArchive.newBuilder()
            .setSummary(summary)
            .setDevice(deviceInfo())
            .build()
    }

    private fun archiveLogger(logs: MutableList<CommandLog>): CommandLogger {
        return object : CommandLogger {
            override fun log(level: CommandLog.Level, message: String, scope: CommandLogScope) {
                logs += CommandLog.newBuilder()
                    .setLevel(level)
                    .setMessage(message)
                    .setUnixTimeMs(System.currentTimeMillis())
                    .build()
                TerminalLog.log(appContext, terminalLevelName(level), CommandTerminalLog.standalone(scope, message))
            }
        }
    }

    private fun sleepInterruptibly(ms: Long) {
        if (ms <= 0) return
        TimeUnit.MILLISECONDS.sleep(ms)
    }

    private fun deviceInfo(): DeviceInfo {
        return DeviceInfo.newBuilder()
            .setManufacturer(Build.MANUFACTURER)
            .setModel(Build.MODEL)
            .setDevice(Build.DEVICE)
            .setSdk(Build.VERSION.SDK_INT)
            .setRelease(Build.VERSION.RELEASE)
            .build()
    }

    private fun configHash(festa: StandaloneFesta): String {
        val digest = MessageDigest.getInstance("SHA-256").digest(festa.toByteArray())
        return digest.joinToString("") { "%02x".format(it) }.take(16)
    }

    private fun isStandaloneControlCommand(command: RunCommand): Boolean {
        return when (command.commandCase) {
            RunCommand.CommandCase.EDIT_STANDALONE_CONFIG,
            RunCommand.CommandCase.GET_STANDALONE_CONFIG,
            RunCommand.CommandCase.GET_STANDALONE_STATUS,
            RunCommand.CommandCase.LIST_STANDALONE_RUNS,
            RunCommand.CommandCase.GET_STANDALONE_RUN,
            RunCommand.CommandCase.CLEAR_STANDALONE_RUNS,
            RunCommand.CommandCase.RUN_STANDALONE_ONCE -> true
            else -> false
        }
    }

    private fun retentionMs(value: Int): Long = value.takeIf { it > 0 }?.toLong() ?: DEFAULT_RETENTION_MS
    private fun maxBytes(value: Long): Long = value.takeIf { it > 0 } ?: DEFAULT_MAX_BYTES

    private fun terminalLevelName(level: CommandLog.Level): String = when (level) {
        CommandLog.Level.LEVEL_DEBUG -> "DEBUG"
        CommandLog.Level.LEVEL_WARN -> "WARN"
        CommandLog.Level.LEVEL_ERROR -> "ERROR"
        else -> "INFO"
    }

    private companion object {
        const val DEFAULT_FESTA_INTERVAL_MS = 30_000L
        const val DEFAULT_WIFI_TIMEOUT_MS = 35_000
        const val DEFAULT_CHECK_TIMEOUT_MS = 10_000
        const val DEFAULT_RETENTION_MS = 7L * 24L * 60L * 60L * 1_000L
        const val DEFAULT_MAX_BYTES = 512L * 1024L * 1024L
    }
}
