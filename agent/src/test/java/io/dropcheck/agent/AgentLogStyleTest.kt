package io.dropcheck.agent

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class AgentLogStyleTest {
    @Test
    fun colorsExecScopedLines() {
        assertEquals(
            AgentLogStyle.ACTIVE_PROBE_COLOR,
            AgentLogStyle.colorForLine("2026-05-03T00:00:00Z INFO  event=probe.exec probe=ping command_line=\"/system/bin/ping -c 3 1.1.1.1\" command_id=id scope=exec\n"),
        )
        assertEquals(AgentLogStyle.ACTIVE_PROBE_COLOR, AgentLogStyle.colorForLine("INFO  event=probe.exec probe=dns scope=exec"))
    }

    @Test
    fun keepsWarningAndErrorPriority() {
        assertEquals(
            AgentLogStyle.ERROR_COLOR,
            AgentLogStyle.colorForLine("2026-05-03T00:00:00Z ERROR event=command.executor.end status=STATUS_FAILED scope=command"),
        )
    }

    @Test
    fun leavesFailedCommandSummariesAtDefaultColor() {
        assertEquals(
            AgentLogStyle.TEXT_COLOR,
            AgentLogStyle.colorForLine("DEBUG event=command.executor.end command_case=PING status=STATUS_FAILED command_id=abc scope=command"),
        )
        assertEquals(
            AgentLogStyle.WARN_COLOR,
            AgentLogStyle.colorForLine("WARN  event=probe.exec probe=ping scope=exec"),
        )
    }

    @Test
    fun classifiesOnlyExecScope() {
        assertTrue(AgentLogStyle.isExecLine("INFO  event=probe.exec probe=download scope=exec url=https://example.com"))
        assertTrue(AgentLogStyle.isExecLine("DEBUG event=probe.exec probe=traceroute scope=exec"))
        assertFalse(AgentLogStyle.isExecLine("INFO  event=controller.request command_case=PING host=1.1.1.1"))
        assertFalse(AgentLogStyle.isExecLine("INFO  event=command.log msg=\"scope=exec appears inside message\" scope=command"))
        assertFalse(AgentLogStyle.isExecLine("INFO  event=command.log msg=\"foo scope=exec bar\" scope=command"))
    }
}
