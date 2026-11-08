package io.dropcheck.agent

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class TerminalAutoScrollStateTest {
    @Test
    fun ignoresLayoutDrivenNonBottomWhileFollowingTail() {
        val state = TerminalAutoScrollState()

        state.onScrollChanged(atBottom = false)

        assertTrue(state.isFollowingTail)
        assertTrue(state.shouldFollowTail(atBottom = true))
    }

    @Test
    fun userScrollMoveStopsFollowingUntilBottomIsReached() {
        val state = TerminalAutoScrollState()

        state.markScrollToBottomPending()
        state.onUserScrollMove()
        state.finishScrollToBottomPending()

        assertFalse(state.shouldFollowTail(atBottom = false))

        state.onScrollChanged(atBottom = true)

        assertTrue(state.isFollowingTail)
        assertTrue(state.shouldFollowTail(atBottom = true))
    }

    @Test
    fun pendingScrollStillFollowsWhenUserHasNotScrolledAway() {
        val state = TerminalAutoScrollState()

        assertTrue(state.markScrollToBottomPending())

        assertTrue(state.shouldFollowTail(atBottom = false))

        state.finishScrollToBottomPending()

        assertTrue(state.isFollowingTail)
    }

    @Test
    fun explicitTailRequestResumesFromHistoryBeforeLayoutCatchesUp() {
        val state = TerminalAutoScrollState()
        state.onUserScrollEnd(atBottom = false)
        state.resumeFollowingTail()
        state.markScrollToBottomPending()

        assertTrue(state.shouldFollowTail(atBottom = false))
    }

    @Test
    fun userGestureWinsEvenWhileTailRequestIsStillPending() {
        val state = TerminalAutoScrollState()
        state.markScrollToBottomPending()
        state.onUserScrollMove()

        assertFalse(state.shouldFollowTail(atBottom = false))
        state.onUserScrollEnd(atBottom = false)
        state.finishScrollToBottomPending()
        assertFalse(state.shouldFollowTail(atBottom = false))
    }
}
