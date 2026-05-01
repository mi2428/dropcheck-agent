package io.dropcheck.agent

import io.dropcheck.agent.grpc.CommandLog

/**
 * Command-scoped log sink.
 *
 * Implementations send each line both to the on-device terminal log and back
 * over the active gRPC command stream.
 */
interface CommandLogger {
    /** Emits a command-scoped line with the protocol log level preserved. */
    fun log(level: CommandLog.Level, message: String)

    /** Convenience wrappers keep call sites readable while preserving gRPC levels. */
    fun debug(message: String) = log(CommandLog.Level.LEVEL_DEBUG, message)
    fun info(message: String) = log(CommandLog.Level.LEVEL_INFO, message)
    fun warn(message: String) = log(CommandLog.Level.LEVEL_WARN, message)
    fun error(message: String) = log(CommandLog.Level.LEVEL_ERROR, message)
}
