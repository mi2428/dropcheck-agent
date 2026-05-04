package io.dropcheck.agent

/**
 * Tracks whether the terminal should follow newly appended log lines.
 *
 * Layout changes while new text is appended can briefly make the current scroll
 * position look away from the bottom. Only user scroll gestures or an explicit
 * non-bottom position while already detached should disable tail-following.
 */
internal class TerminalAutoScrollState {
    var isFollowingTail: Boolean = true
        private set
    var isScrollToBottomPending: Boolean = false
        private set

    private var userScrollActive: Boolean = false

    fun onUserScrollMove() {
        userScrollActive = true
        isFollowingTail = false
    }

    fun onUserScrollEnd(atBottom: Boolean) {
        userScrollActive = false
        isFollowingTail = atBottom
    }

    fun onScrollChanged(atBottom: Boolean) {
        if (userScrollActive || !isFollowingTail || atBottom) {
            isFollowingTail = atBottom
        }
    }

    fun shouldFollowTail(atBottom: Boolean): Boolean {
        if (!isFollowingTail) return false
        if (isScrollToBottomPending) return true
        if (!atBottom) {
            isFollowingTail = false
            return false
        }
        return true
    }

    fun resumeFollowingTail() {
        isFollowingTail = true
    }

    fun markScrollToBottomPending(): Boolean {
        if (isScrollToBottomPending) return false
        isScrollToBottomPending = true
        return true
    }

    fun finishScrollToBottomPending() {
        isScrollToBottomPending = false
    }
}
