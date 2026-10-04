#!/usr/bin/env python3
"""Run a compiled tui.test child in a real PTY, never discover real devices.

From controller/: go test -c -o "$TMPDIR/tui.test" ./internal/tui
python3 internal/tui/testdata/pty_smoke.py "$TMPDIR/tui.test"
The caller provides a temporary binary path; this script persists no output.
"""

import argparse
import errno
import fcntl
import os
import pty
import select
import signal
import struct
import subprocess
import termios
import time


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("binary")
    args = parser.parse_args()
    master, slave = pty.openpty()
    original = termios.tcgetattr(slave)

    def resize(rows, columns):
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", rows, columns, 0, 0))

    def child_session():
        os.setsid()
        fcntl.ioctl(slave, termios.TIOCSCTTY, 0)

    resize(32, 120)
    # Do not inherit lab credential/device/session environment variables.
    env = {"TERM": "xterm-256color", "LANG": "en_US.UTF-8", "NO_COLOR": "1", "DROPCHECK_TUI_PTY_CHILD": "1"}
    process = subprocess.Popen(
        [os.path.abspath(args.binary), "-test.run=^TestWorkflowPTYChild$", "-test.v"],
        stdin=slave, stdout=slave, stderr=slave, env=env, preexec_fn=child_session,
    )
    captured = bytearray()

    def wait_for(marker, timeout=12):
        deadline = time.monotonic() + timeout
        start = len(captured)
        while time.monotonic() < deadline:
            readable, _, _ = select.select([master], [], [], 0.1)
            if readable:
                try:
                    data = os.read(master, 65536)
                except OSError as error:
                    if error.errno == errno.EIO:
                        break
                    raise
                captured.extend(data)
                if b"\x1b[6n" in data:
                    os.write(master, b"\x1b[1;1R")
                if marker in captured[start:]:
                    return
                if len(captured) > 1024 * 1024:
                    raise AssertionError("PTY output exceeded bounded smoke budget")
            if process.poll() is not None:
                break
        raise AssertionError("PTY did not reach the expected keyboard transition: " + marker.decode())

    def send(keys):
        os.write(master, keys)

    try:
        wait_for(b"Select Agents")
        # Exercise actual target/check/agent selection keys (toggle off/on)
        # before previewing the single synthetic scenario.
        send(b"\t  \t  \t  \r")
        wait_for(b"Preview")
        resize(12, 60)
        os.kill(process.pid, signal.SIGWINCH)
        send(b"\r")
        wait_for(b"Review 1/1")
        resize(32, 120)
        os.kill(process.pid, signal.SIGWINCH)
        send(b"f")
        wait_for(b"Preview")
        send(b"\r")
        wait_for(b"Review 2/2")
        send(b"e")
        wait_for(b"Select Agents")
        send(b"l\r")
        wait_for(b"Preview")
        send(b"\r")
        wait_for(b"running")
        send(b"q")
        wait_for(b"Review 3/3")
        send(b"q")
        wait_for(b"--- PASS: TestWorkflowPTYChild")
        assert process.wait(timeout=8) == 0, "PTY child failed its fake execution assertions"
        # Drain the final renderer/test output after process exit, including the
        # alternate-screen leave sequence. Never write captured bytes to disk.
        while select.select([master], [], [], 0.1)[0]:
            try:
                data = os.read(master, 65536)
            except OSError as error:
                if error.errno == errno.EIO:
                    break
                raise
            if not data:
                break
            captured.extend(data)
        assert termios.tcgetattr(slave) == original, "PTY termios was not restored"
        assert b"\x1b[?1049l" in captured or b"\x1b[?1047l" in captured, "alternate screen was not restored"
        print("PTY PASS: selection/preview, finite-review-rerun, loop-cancel-cleanup, resize, exit, termios+alternate-screen restoration")
    finally:
        if process.poll() is None:
            os.killpg(process.pid, signal.SIGTERM)
            process.wait(timeout=5)
        os.close(master)
        os.close(slave)


if __name__ == "__main__":
    main()
