package io.dropcheck.agent

import android.content.Context
import android.os.Build
import io.dropcheck.agent.grpc.CommandLog
import io.dropcheck.agent.grpc.CommandResult
import io.dropcheck.agent.grpc.DeviceInfo
import io.dropcheck.agent.grpc.FestivalMeasurementStep
import io.dropcheck.agent.grpc.FestivalPlan
import io.dropcheck.agent.grpc.FestivalRunArchive
import io.dropcheck.agent.grpc.FestivalRunSummary
import io.dropcheck.agent.grpc.FestivalStep
import io.dropcheck.agent.grpc.RunCommand
import java.security.MessageDigest
import java.time.Instant
import java.util.UUID
import java.util.concurrent.Executors
import java.util.concurrent.Future
import java.util.concurrent.TimeUnit

/** Runs Dropcheck Festival measurement plans locally while the controller is absent. */
internal class FestivalStandaloneRunner(private val context: Context) {
    private val appContext = context.applicationContext
    private val executor = Executors.newSingleThreadExecutor()
    private val configStore = FestivalConfigStore(appContext)
    private val resultStore = FestivalResultStore(appContext)
    private var future: Future<*>? = null

    @Synchronized
    fun refresh() {
        val config = configStore.load()
        FestivalStateBroadcast.send(appContext, config.enabled)
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
        FestivalRuntimeState.running.set(false)
        FestivalRuntimeState.currentRunId.set("")
    }

    fun shutdown() {
        stop()
        executor.shutdownNow()
    }

    private fun loop() {
        while (!Thread.currentThread().isInterrupted) {
            val config = configStore.load()
            if (!config.enabled) break
            if (!config.hasPlan()) {
                FestivalRuntimeState.message.set("standalone enabled without a plan")
                sleepInterruptibly(1_000)
                continue
            }
            val archive = runPlan(config.plan, save = true)
            val cleanup = resultStore.enforce(config.retentionMs.toLong(), config.maxBytes.toLong())
            FestivalRuntimeState.message.set(cleanup.ifBlank { "last run ${archive.summary.status}" })
            sleepInterruptibly(config.intervalMs.toLong().coerceAtLeast(1_000))
        }
        FestivalRuntimeState.running.set(false)
        FestivalRuntimeState.currentRunId.set("")
    }

    fun runPlan(plan: FestivalPlan, save: Boolean): FestivalRunArchive {
        val runId = "${Instant.now().toEpochMilli()}-${UUID.randomUUID()}"
        val logs = mutableListOf<CommandLog>()
        val logger = archiveLogger(logs)
        val started = System.currentTimeMillis()
        FestivalRuntimeState.running.set(true)
        FestivalRuntimeState.currentRunId.set(runId)
        FestivalRuntimeState.message.set("running")
        TerminalLog.info(appContext, "Dropcheck Festival run start run_id=$runId plan=${plan.name.ifBlank { "-" }} networks=${plan.networksCount}")

        val steps = mutableListOf<FestivalMeasurementStep>()
        var failed = 0
        for ((networkIndex, network) in plan.networksList.withIndex()) {
            val networkName = network.name.ifBlank { network.connect.ssid.ifBlank { "network_${networkIndex + 1}" } }
            var stepIndex = 1
            fun runStep(name: String, command: RunCommand): Boolean {
                val measurement = executeStep(
                    logger = logger,
                    networkIndex = networkIndex + 1,
                    networkName = networkName,
                    stepIndex = stepIndex++,
                    stepName = name,
                    command = command,
                )
                steps += measurement
                val ok = measurement.hasResult() && measurement.result.status == CommandResult.Status.STATUS_OK && measurement.error.isBlank()
                if (!ok) failed += 1
                return ok
            }

            val connected = runStep("connect", RunCommand.newBuilder()
                .setLabel("festival connect $networkName")
                .setConnectWifi(network.connect)
                .build())
            val ready = connected && (!network.hasWaitConnected() || runStep("wait_connected", RunCommand.newBuilder()
                .setLabel("festival wait $networkName")
                .setWaitWifiConnected(network.waitConnected)
                .build()))
            if (ready) {
                for (check in plan.checksList + network.checksList) {
                    runStep(check.name.ifBlank { check.command.safeLabel() }, check.command)
                }
            }
            if (network.disconnectAfter) {
                runStep("disconnect", RunCommand.newBuilder()
                    .setLabel("festival disconnect $networkName")
                    .setDisconnectWifi(io.dropcheck.agent.grpc.DisconnectWifi.getDefaultInstance())
                    .build())
            }
            if (network.forgetAfter) {
                val target = network.connect.ssid.ifBlank { network.connect.bssid }
                runStep("forget", RunCommand.newBuilder()
                    .setLabel("festival forget $networkName")
                    .setForgetWifi(io.dropcheck.agent.grpc.ForgetWifi.newBuilder().setTarget(target))
                    .build())
            }
            if (network.pauseMs > 0) {
                sleepInterruptibly(network.pauseMs.toLong())
            }
        }

        val finished = System.currentTimeMillis()
        val summary = FestivalRunSummary.newBuilder()
            .setRunId(runId)
            .setPlanName(plan.name)
            .setPlanHash(planHash(plan))
            .setStartedUnixMs(started)
            .setFinishedUnixMs(finished)
            .setStatus(if (failed == 0) "ok" else "failed")
            .setNetworkCount(plan.networksCount)
            .setStepCount(steps.size)
            .setFailedStepCount(failed)
            .setSynced(false)
            .setMessage(if (failed == 0) "measurement completed" else "measurement completed with failed commands")
            .build()
        val archive = FestivalRunArchive.newBuilder()
            .setSummary(summary)
            .setDevice(deviceInfo())
            .setPlan(plan)
            .addAllSteps(steps)
            .addAllLogs(logs)
            .build()
        if (save) resultStore.save(archive)
        FestivalRuntimeState.running.set(false)
        FestivalRuntimeState.currentRunId.set("")
        FestivalRuntimeState.message.set(summary.message)
        TerminalLog.info(appContext, "Dropcheck Festival run end run_id=$runId status=${summary.status} steps=${summary.stepCount} failed=${summary.failedStepCount}")
        return archive
    }

    private fun executeStep(
        logger: CommandLogger,
        networkIndex: Int,
        networkName: String,
        stepIndex: Int,
        stepName: String,
        command: RunCommand,
    ): FestivalMeasurementStep {
        val started = System.currentTimeMillis()
        val builder = FestivalMeasurementStep.newBuilder()
            .setNetworkIndex(networkIndex)
            .setNetworkName(networkName)
            .setStepIndex(stepIndex)
            .setStepName(stepName)
            .setAttempt(1)
            .setStartedUnixMs(started)
            .setCommand(command)
        if (isStandaloneControlCommand(command)) {
            return builder
                .setFinishedUnixMs(System.currentTimeMillis())
                .setError("standalone control commands cannot be nested in a Festival plan")
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

    private fun archiveLogger(logs: MutableList<CommandLog>): CommandLogger {
        return object : CommandLogger {
            override fun log(level: CommandLog.Level, message: String) {
                logs += CommandLog.newBuilder()
                    .setLevel(level)
                    .setMessage(message)
                    .setUnixTimeMs(System.currentTimeMillis())
                    .build()
                TerminalLog.log(appContext, terminalLevelName(level), "festival $message")
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

    private fun planHash(plan: FestivalPlan): String {
        val digest = MessageDigest.getInstance("SHA-256").digest(plan.toByteArray())
        return digest.joinToString("") { "%02x".format(it) }.take(16)
    }

    private fun isStandaloneControlCommand(command: RunCommand): Boolean {
        return when (command.commandCase) {
            RunCommand.CommandCase.SET_FESTIVAL_CONFIG,
            RunCommand.CommandCase.GET_FESTIVAL_CONFIG,
            RunCommand.CommandCase.GET_FESTIVAL_STATUS,
            RunCommand.CommandCase.LIST_FESTIVAL_RUNS,
            RunCommand.CommandCase.GET_FESTIVAL_RUN,
            RunCommand.CommandCase.CLEAR_FESTIVAL_RUNS,
            RunCommand.CommandCase.RUN_FESTIVAL_ONCE -> true
            else -> false
        }
    }

    private fun terminalLevelName(level: CommandLog.Level): String = when (level) {
        CommandLog.Level.LEVEL_DEBUG -> "DEBUG"
        CommandLog.Level.LEVEL_WARN -> "WARN"
        CommandLog.Level.LEVEL_ERROR -> "ERROR"
        else -> "INFO"
    }
}
