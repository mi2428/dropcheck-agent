package io.dropcheck.agent

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class CommandTerminalLogTest {
    @Test
    fun appendsCommandContextToStructuredMessages() {
        val line = CommandTerminalLog.controller(
            commandId = "abc123",
            scope = CommandLogScope.EXEC,
            message = StructuredLog.format(
                "probe.exec",
                listOf(
                    "probe" to "ping",
                    "command_line" to "/system/bin/ping -c 3 1.1.1.1",
                ),
            ),
        )

        assertEquals(
            "event=probe.exec probe=ping command_line=\"/system/bin/ping -c 3 1.1.1.1\" command_id=abc123 scope=exec",
            line,
        )
    }

    @Test
    fun wrapsPlainMessagesAsLogfmt() {
        val line = CommandTerminalLog.controller(
            commandId = "abc123",
            scope = CommandLogScope.COMMAND,
            message = "wifi status requested",
        )

        assertEquals("event=command.log command_id=abc123 scope=command msg=\"wifi status requested\"", line)
    }

    @Test
    fun noLongerUsesBracketPrefix() {
        val line = CommandTerminalLog.controller("abc123", CommandLogScope.EXEC, "ping start")

        assertFalse(line.contains("command["))
        assertFalse(line.contains("exec["))
        assertTrue(line.contains("command_id=abc123"))
        assertTrue(line.contains("scope=exec"))
    }
}
