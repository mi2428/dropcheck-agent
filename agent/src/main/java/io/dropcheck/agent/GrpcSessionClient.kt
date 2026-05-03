package io.dropcheck.agent

import android.os.Build
import io.dropcheck.agent.grpc.AgentFrame
import io.dropcheck.agent.grpc.AgentHeartbeat
import io.dropcheck.agent.grpc.AgentHello
import io.dropcheck.agent.grpc.CommandAccepted
import io.dropcheck.agent.grpc.CommandError
import io.dropcheck.agent.grpc.CommandLog
import io.dropcheck.agent.grpc.CommandResult
import io.dropcheck.agent.grpc.ControllerFrame
import io.dropcheck.agent.grpc.DeviceInfo
import io.dropcheck.agent.grpc.DropcheckControlGrpc
import io.dropcheck.agent.grpc.RunCommand
import io.grpc.ManagedChannelBuilder
import io.grpc.stub.StreamObserver
import java.util.UUID
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.Future
import java.util.concurrent.FutureTask
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicLong

/**
 * Owns the bidi gRPC session between the Android agent and the controller.
 *
 * It deliberately does not interpret commands beyond lifecycle concerns
 * (acceptance, cancellation, ordering, heartbeat, and error reporting).
 */
class GrpcSessionClient(
    private val service: AgentService,
    private val host: String,
    private val port: Int,
    private val token: String,
    private val agentId: String,
    private val adbSerial: String,
    private val transport: String = "adb-reverse",
) {
    private val sessionId = UUID.randomUUID().toString()
    private val done = CountDownLatch(1)
    private val seq = AtomicLong(1)
    private val sendLock = Any()
    private val commandExecutor = Executors.newSingleThreadExecutor()
    private val heartbeatExecutor = Executors.newScheduledThreadPool(2)
    private val active = ConcurrentHashMap<String, Future<*>>()
    private val lastControllerFrameMs = AtomicLong(0)
    private val lastControllerTimeoutLogMs = AtomicLong(0)
    private val controllerTimedOut = AtomicBoolean(false)

    /**
     * Opens the bidi stream, sends hello, and blocks until the stream ends.
     *
     * Shutdown always cancels local command work before completing the request
     * stream so the controller does not receive late frames from old commands.
     */
    fun run() {
        TerminalLog.debugEvent(service, "grpc.session.open", listOf(
            "host" to host,
            "port" to port,
            "transport" to transport,
            "local_session_id" to sessionId,
            "agent_id" to agentId,
            "adb_serial" to adbSerial,
            "thread" to Thread.currentThread().name,
        ))
        val channel = ManagedChannelBuilder
            .forAddress(host, port)
            .usePlaintext()
            .keepAliveTime(1, TimeUnit.SECONDS)
            .keepAliveTimeout(1, TimeUnit.SECONDS)
            .keepAliveWithoutCalls(true)
            .build()
        TerminalLog.debug(service, "grpc channel built authority=${channel.authority()} session=$sessionId")
        ControllerLinkRuntimeState.markConnecting("$host:$port", transport)
        AgentStatusBroadcast.send(service)
        val stub = DropcheckControlGrpc.newStub(channel)
        val requestsRef = arrayOfNulls<StreamObserver<AgentFrame>>(1)

        val responses = object : StreamObserver<ControllerFrame> {
            override fun onNext(value: ControllerFrame) {
                markControllerFrameSeen(value)
                if (value.bodyCase != ControllerFrame.BodyCase.HEARTBEAT) {
                    TerminalLog.debugEvent(service, "grpc.rx", listOf(
                        "local_session_id" to sessionId,
                        "direction" to "controller_to_agent",
                    ) + value.logFields())
                }
                when (value.bodyCase) {
                    ControllerFrame.BodyCase.RUN_COMMAND -> startCommand(value, requestsRef[0])
                    ControllerFrame.BodyCase.CANCEL_COMMAND -> cancelCommand(value)
                    ControllerFrame.BodyCase.HEARTBEAT -> Unit
                    ControllerFrame.BodyCase.BODY_NOT_SET -> TerminalLog.warn(service, "controller frame without body")
                }
            }

            override fun onError(t: Throwable) {
                TerminalLog.warn(service, "controller connection lost session=$sessionId", t)
                done.countDown()
            }

            override fun onCompleted() {
                TerminalLog.warn(service, "controller stream completed; connection closed session=$sessionId")
                done.countDown()
            }
        }

        val requests = stub.session(responses)
        requestsRef[0] = requests
        try {
            val hello = helloFrame()
            TerminalLog.debugEvent(service, "grpc.hello.prepare", listOf(
                "local_session_id" to sessionId,
            ) + hello.logFields())
            send(requests, hello)
            ControllerLinkRuntimeState.markConnected("$host:$port", transport)
            AgentStatusBroadcast.send(service)
            TerminalLog.info(service, "grpc hello sent session=$sessionId agent_id=$agentId adb_serial=$adbSerial host=$host port=$port")
            startHeartbeat(requests)
            done.await()
        } catch (e: InterruptedException) {
            Thread.currentThread().interrupt()
        } catch (e: Exception) {
            TerminalLog.error(service, "grpc session failed", e)
            sendError(requests, "", "grpc session failed", e.toString())
        } finally {
            TerminalLog.debug(service, "grpc session shutting down active_commands=${active.size}")
            for (future in active.values) {
                future.cancel(true)
            }
            active.clear()
            commandExecutor.shutdownNow()
            heartbeatExecutor.shutdownNow()
            ControllerLinkRuntimeState.markDisconnected("gRPC session ended")
            AgentStatusBroadcast.send(service)
            runCatching { requests.onCompleted() }
                .onSuccess { TerminalLog.debug(service, "grpc request stream completed session=$sessionId") }
                .onFailure { TerminalLog.warn(service, "grpc request stream completion failed session=$sessionId error=$it") }
            channel.shutdown()
            val terminated = channel.awaitTermination(2, TimeUnit.SECONDS)
            TerminalLog.debug(service, "grpc channel shutdown session=$sessionId terminated=$terminated")
        }
    }

    /**
     * Accepts a controller command and schedules it on the single command worker.
     *
     * Only one task may own a command ID at a time; duplicate IDs are rejected
     * before any local side effect runs.
     */
    private fun startCommand(frame: ControllerFrame, requests: StreamObserver<AgentFrame>?) {
        if (requests == null) return
        val commandId = frame.commandId.ifBlank { UUID.randomUUID().toString() }
        val command = frame.runCommand
        TerminalLog.debugEvent(service, "command.received", listOf(
            "local_session_id" to sessionId,
            "command_id" to commandId,
            "active_before" to active.size,
        ) + command.logFields())
        TerminalLog.infoEvent(service, "controller.request", listOf(
            "local_session_id" to sessionId,
            "command_id" to commandId,
        ) + command.logFields())
        val task = FutureTask<Unit> {
            val logger = object : CommandLogger {
                override fun log(level: CommandLog.Level, message: String, scope: CommandLogScope) {
                    TerminalLog.log(service, terminalLevel(level), CommandTerminalLog.controller(commandId, scope, message))
                    send(requests, AgentFrame.newBuilder()
                        .setSeq(seq.getAndIncrement())
                        .setSessionId(sessionId)
                        .setCommandId(commandId)
                        .setLog(CommandLog.newBuilder()
                            .setLevel(level)
                            .setMessage(message)
                            .setUnixTimeMs(System.currentTimeMillis())
                            .build())
                        .build())
                }
            }

            try {
                logger.debugEvent("command.dispatch.begin", listOf(
                    "thread" to Thread.currentThread().name,
                ) + command.logFields())
                val startedAt = System.nanoTime()
                val result = CommandExecutor(service, logger).execute(command)
                logger.debugEvent("command.dispatch.end", listOf(
                    "dispatch_elapsed_ms" to elapsedMs(startedAt),
                ) + result.logFields())
                TerminalLog.infoEvent(service, "controller.response.result", listOf(
                    "local_session_id" to sessionId,
                    "command_id" to commandId,
                ) + result.logFields())
                send(requests, AgentFrame.newBuilder()
                    .setSeq(seq.getAndIncrement())
                    .setSessionId(sessionId)
                    .setCommandId(commandId)
                    .setResult(result)
                    .build())
                if (command.commandCase == RunCommand.CommandCase.RECONNECT_CONTROLLER && result.status == CommandResult.Status.STATUS_OK) {
                    TerminalLog.info(service, "controller reconnect requested; closing current stream session=$sessionId")
                    done.countDown()
                }
            } catch (e: InterruptedException) {
                Thread.currentThread().interrupt()
                TerminalLog.warnEvent(service, "command.interrupted", listOf(
                    "local_session_id" to sessionId,
                    "command_id" to commandId,
                ))
                val result = CommandResult.newBuilder()
                    .setStatus(CommandResult.Status.STATUS_CANCELED)
                    .setMessage("command canceled")
                    .build()
                TerminalLog.infoEvent(service, "controller.response.result", listOf(
                    "local_session_id" to sessionId,
                    "command_id" to commandId,
                    "interrupted" to true,
                ) + result.logFields())
                send(requests, AgentFrame.newBuilder()
                    .setSeq(seq.getAndIncrement())
                    .setSessionId(sessionId)
                    .setCommandId(commandId)
                    .setResult(result)
                    .build())
            } catch (e: Exception) {
                TerminalLog.error(service, StructuredLog.format(
                    "command.failed",
                    listOf(
                        "local_session_id" to sessionId,
                        "command_id" to commandId,
                    ),
                ), e)
                sendError(requests, commandId, "command failed", e.toString())
            } finally {
                active.remove(commandId)
            }
        }

        if (active.putIfAbsent(commandId, task) != null) {
            TerminalLog.warnEvent(service, "command.duplicate", listOf(
                "local_session_id" to sessionId,
                "command_id" to commandId,
            ) + command.logFields())
            sendError(requests, commandId, "duplicate command_id", commandId)
            return
        }

        TerminalLog.debugEvent(service, "command.accepted.prepare", listOf(
            "local_session_id" to sessionId,
            "command_id" to commandId,
            "active_after" to active.size,
        ) + command.logFields())
        send(requests, AgentFrame.newBuilder()
            .setSeq(seq.getAndIncrement())
            .setSessionId(sessionId)
            .setCommandId(commandId)
            .setAccepted(CommandAccepted.newBuilder()
                .setCommandName(commandSummary(command))
                .build())
            .build())

        TerminalLog.debugEvent(service, "command.accepted", listOf(
            "local_session_id" to sessionId,
            "command_id" to commandId,
            "active_after" to active.size,
        ) + command.logFields())
        commandExecutor.execute(task)
    }

    /**
     * Sends agent heartbeats and watches for silent controller periods.
     *
     * The timeout is diagnostic only; gRPC itself remains responsible for
     * connection teardown.
     */
    private fun startHeartbeat(requests: StreamObserver<AgentFrame>) {
        lastControllerFrameMs.set(System.currentTimeMillis())
        TerminalLog.debug(service, "agent heartbeat scheduler start session=$sessionId interval_ms=1000 timeout_ms=$CONTROLLER_HEARTBEAT_TIMEOUT_MS")
        heartbeatExecutor.scheduleWithFixedDelay({
            val now = System.currentTimeMillis()
            send(requests, AgentFrame.newBuilder()
                .setSeq(seq.getAndIncrement())
                .setSessionId(sessionId)
                .setHeartbeat(AgentHeartbeat.newBuilder()
                    .setUnixTimeMs(now)
                    .build())
                .build())
        }, 1, 1, TimeUnit.SECONDS)
        heartbeatExecutor.scheduleWithFixedDelay({
            checkControllerHeartbeatDeadline()
        }, 1, 1, TimeUnit.SECONDS)
    }

    private fun markControllerFrameSeen(frame: ControllerFrame) {
        val now = System.currentTimeMillis()
        val previous = lastControllerFrameMs.getAndSet(now)
        val gap = if (previous == 0L) 0L else now - previous
        if (controllerTimedOut.getAndSet(false)) {
            ControllerLinkRuntimeState.markHeartbeatRecovered()
            AgentStatusBroadcast.send(service)
            TerminalLog.warn(service, "controller connection recovered session=$sessionId seq=${frame.seq} body=${frame.bodyCase} gap_ms=$gap")
        }
    }

    private fun checkControllerHeartbeatDeadline() {
        val lastSeen = lastControllerFrameMs.get()
        if (lastSeen == 0L) return
        val now = System.currentTimeMillis()
        val silentMs = now - lastSeen
        if (silentMs < CONTROLLER_HEARTBEAT_TIMEOUT_MS) return

        val lastLog = lastControllerTimeoutLogMs.get()
        if (lastLog != 0L && now - lastLog < CONTROLLER_HEARTBEAT_WARN_INTERVAL_MS) return
        if (!lastControllerTimeoutLogMs.compareAndSet(lastLog, now)) return

        controllerTimedOut.set(true)
        ControllerLinkRuntimeState.markHeartbeatTimedOut("controller heartbeat timeout")
        AgentStatusBroadcast.send(service)
        TerminalLog.warn(
            service,
            "controller heartbeat timeout session=$sessionId silent_ms=$silentMs timeout_ms=$CONTROLLER_HEARTBEAT_TIMEOUT_MS last_seen_unix_time_ms=$lastSeen; USB cable, adb reverse, or PC controller may be disconnected",
        )
    }

    private fun cancelCommand(frame: ControllerFrame) {
        val commandId = frame.commandId
        val future = active.remove(commandId)
        if (future != null) {
            TerminalLog.warnEvent(service, "command.cancel", listOf(
                "local_session_id" to sessionId,
                "command_id" to commandId,
                "reason" to frame.cancelCommand.reason,
            ))
            future.cancel(true)
        } else {
            TerminalLog.debugEvent(service, "command.cancel.ignored", listOf(
                "local_session_id" to sessionId,
                "command_id" to commandId,
                "reason" to "no_active_command",
                "request_reason" to frame.cancelCommand.reason,
            ))
        }
    }

    /** Builds the session hello frame, including auth token and local capability list. */
    private fun helloFrame(): AgentFrame {
        val hello = AgentHello.newBuilder()
            .setToken(token)
            .setPackageName(service.packageName)
            .setAppVersion("0.1.0")
            .setControllerAgentId(agentId)
            .setAdbSerial(adbSerial)
            .setDevice(DeviceInfo.newBuilder()
                .setManufacturer(Build.MANUFACTURER)
                .setModel(Build.MODEL)
                .setDevice(Build.DEVICE)
                .setSdk(Build.VERSION.SDK_INT)
                .setRelease(Build.VERSION.RELEASE)
                .build())
            .addAllCapabilities(AgentCommandRegistry.capabilities)
            .build()
        return AgentFrame.newBuilder()
            .setSeq(seq.getAndIncrement())
            .setSessionId(sessionId)
            .setHello(hello)
            .build()
    }

    private fun commandSummary(command: RunCommand): String {
        return command.safeLabel()
    }

    private fun elapsedMs(startedAt: Long): Long {
        return TimeUnit.NANOSECONDS.toMillis(System.nanoTime() - startedAt)
    }

    private fun sendError(
        requests: StreamObserver<AgentFrame>,
        commandId: String,
        message: String,
        detail: String,
    ) {
        TerminalLog.debugEvent(service, "controller.response.error.prepare", listOf(
            "local_session_id" to sessionId,
            "command_id" to commandId,
            "message" to message,
            "detail_len" to detail.length,
            "detail" to StructuredLog.preview(detail, 800),
        ))
        TerminalLog.warnEvent(service, "controller.response.error", listOf(
            "local_session_id" to sessionId,
            "command_id" to commandId,
            "message" to message,
            "detail_len" to detail.length,
            "detail" to StructuredLog.preview(detail, 800),
        ))
        send(requests, AgentFrame.newBuilder()
            .setSeq(seq.getAndIncrement())
            .setSessionId(sessionId)
            .setCommandId(commandId)
            .setError(CommandError.newBuilder()
                .setMessage(message)
                .setDetail(detail)
                .build())
            .build())
    }

    /**
     * Serializes outgoing stream writes.
     *
     * gRPC StreamObserver is not guaranteed to be thread-safe; command logs,
     * command results, and heartbeats can originate from different executors.
     */
    private fun send(requests: StreamObserver<AgentFrame>, frame: AgentFrame) {
        synchronized(sendLock) {
            if (frame.bodyCase != AgentFrame.BodyCase.HEARTBEAT) {
                TerminalLog.debugEvent(service, "grpc.tx", listOf(
                    "local_session_id" to sessionId,
                    "direction" to "agent_to_controller",
                ) + frame.logFields())
            }
            runCatching { requests.onNext(frame) }
                .onFailure {
                    TerminalLog.warn(service, "controller connection lost while sending frame session=$sessionId seq=${frame.seq} body=${frame.bodyCase}", it)
                    done.countDown()
                }
        }
    }

    private fun terminalLevel(level: CommandLog.Level): String = when (level) {
        CommandLog.Level.LEVEL_DEBUG -> "DEBUG"
        CommandLog.Level.LEVEL_WARN -> "WARN"
        CommandLog.Level.LEVEL_ERROR -> "ERROR"
        else -> "INFO"
    }

    companion object {
        private const val CONTROLLER_HEARTBEAT_TIMEOUT_MS = 3_000L
        private const val CONTROLLER_HEARTBEAT_WARN_INTERVAL_MS = 3_000L
    }
}
