#!/usr/bin/env python3
"""Drive the real tui TUI in a private PTY with synthetic runner results only."""

import errno
import fcntl
import json
import os
from pathlib import Path
import pty
import re
import select
import signal
import struct
import subprocess
import tempfile
import termios
import time


ROOT = Path(__file__).resolve().parents[1]
GO = os.environ.get("GO", "go")


def check_names():
    """Reject stale product names, retaining only documented negative/wire cases."""
    allowed = {
        "proto/dropcheck/v1/control.proto": r'^\s*reserved "(?:watch_wifi|wifi_watch)"',
        "agent/src/main/java/io/dropcheck/agent/GrpcSessionClient.kt": r'watches for silent controller periods',
        "controller/integration/e2e/cases_test.go": r'(?:strings.Contains|return fmt.Errorf).*wifi watch',
        "controller/internal/app/run_test.go": r'(?:removed :=|args :=).*"(?:dropcheck \[flags\] )?watch',
        "controller/internal/app/tui_entry_qa_test.go": r'(?:\{"watch"|strings.Contains\(b.String\(\), "watch")',
    }
    names = subprocess.check_output(["git", "ls-files", "-co", "--exclude-standard", "-z"], cwd=ROOT).decode().split("\0")
    for name in sorted(set(names) - {""}):
        source = ROOT / name
        if not source.is_file() or "__pycache__" in source.parts:
            continue
        assert "watch" not in name.lower(), f"stale source path: {name}"
        if "/controlpb/" in name or name == "tools/test_tui.py":
            continue  # Generated protocol descriptors retain reserved wire names.
        if source.suffix not in (".go", ".kt", ".proto", ".py", ".md", ".yml", ".yaml") and name != "Makefile":
            continue
        for number, line in enumerate(source.read_text().splitlines(), 1):
            if re.search("watch", line, re.I):
                assert name in allowed and re.search(allowed[name], line), f"stale product name: {name}:{number}"
    print("PASS: TUI source paths and names; only reserved protocol names, observer prose and negative tests are exempt")


def run():
    check_names()
    with tempfile.TemporaryDirectory(prefix="dropcheck-tui-pty-") as directory:
        binary = Path(directory) / "tui.test"
        subprocess.run(
            [GO, "test", "-c", "-o", str(binary), "./internal/tui"],
            cwd=ROOT / "controller",
            check=True,
            timeout=90,
        )
        master, slave = pty.openpty()
        read_fd, write_fd = os.pipe()
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 48, 160, 0, 0))
        environment = os.environ | {
            "TERM": "xterm-256color",
            "DROPCHECK_TUI_PTY_CHILD": "1",
            "DROPCHECK_TUI_QA_FD": str(write_fd),
        }
        process = subprocess.Popen(
            [str(binary), "-test.run=^TestTUIPTYChild$", "-test.timeout=30s"],
            cwd=ROOT / "controller",
            stdin=slave,
            stdout=slave,
            stderr=slave,
            env=environment,
            pass_fds=(write_fd,),
            start_new_session=True,
        )
        os.close(slave)
        os.close(write_fd)
        pending = bytearray()
        tail = bytearray()

        def read_message(timeout=3):
            deadline = time.monotonic() + timeout
            while time.monotonic() < deadline:
                if b"\n" in pending:
                    line, _, rest = pending.partition(b"\n")
                    pending[:] = rest
                    return json.loads(line)
                ready, _, _ = select.select([master, read_fd], [], [], deadline - time.monotonic())
                for descriptor in ready:
                    try:
                        data = os.read(descriptor, 65536)
                    except OSError as error:
                        if descriptor != master or error.errno != errno.EIO:
                            raise
                        data = b""
                    if descriptor == read_fd:
                        if not data:
                            raise AssertionError("checkpoint stream closed before completion")
                        pending.extend(data)
                    else:
                        tail.extend(data)
                        tail[:] = tail[-4096:]
                if process.poll() is not None and not pending:
                    raise AssertionError(f"tui child exited early: {process.returncode}; {bytes(tail)!r}")
            raise AssertionError(f"tui checkpoint timed out; {bytes(tail)!r}")

        def press(value):
            os.write(master, value)

        def checkpoint(predicate, description):
            deadline = time.monotonic() + 3
            last = None
            while time.monotonic() < deadline:
                press(b"\x1b[24~")  # F12 asks the test-only wrapper for current model state.
                last = read_message()
                if last.get("kind") == "checkpoint" and predicate(last):
                    return last
                time.sleep(0.02)
            compact = {key: value for key, value in (last or {}).items() if key not in ("frame", "inspector")}
            raise AssertionError(f"{description}: {compact}")

        try:
            assert read_message()["kind"] == "ready"
            checkpoint(lambda state: state["active"], "runtime did not bind")
            press(b"a")
            checkpoint(lambda state: state["menu"] and "Select control scope" in state["frame"], "scope picker")
            press(b"\x1b[B" * 4 + b"\r")
            checkpoint(lambda state: state["menu_scope"].get("kind") == "check", "check selection")
            press(b"\r")
            checkpoint(
                lambda state: not state["busy"] and state["applied"]
                and state["scope"].get("agent_id") == "qa-agent-a"
                and state["scope"].get("check_id") == "check/0",
                "scoped pause did not apply",
            )
            press(b"a" + b"\x1b[B" * 4 + b"\r\r")
            checkpoint(lambda state: not state["busy"] and state["applied"] and not state["menu"], "scoped resume")
            press(b"\t/ping\r\r")  # Select the seeded check, not a newer target summary.
            snapshot = checkpoint(lambda state: state["detail"] and "Passing Check Detail" in state["frame"], "inspector")
            press(b"\x1b[6~")
            snapshot = checkpoint(lambda state: state["offset"] > 0, "inspector did not scroll")
            checkpoint(
                lambda state: state["round"] > snapshot["round"]
                and state["inspector"] == snapshot["inspector"] and state["offset"] == snapshot["offset"],
                "new events displaced the snapshot",
            )
            fcntl.ioctl(master, termios.TIOCSWINSZ, struct.pack("HHHH", 24, 80, 0, 0))
            os.kill(process.pid, signal.SIGWINCH)
            checkpoint(lambda state: state["width"] == 80 and state["height"] == 24 and state["detail"], "resize")
            press(b"\x1a")  # Ctrl-Z is an explicit global pause/resume request.
            checkpoint(lambda state: state["run_paused"] and state["phase"] == "paused" and not state["busy"], "global pause")
            press(b"\x1b[27u")  # Unambiguous Escape; do not merge with the following F12.
            checkpoint(lambda state: not state["detail"] and state["run_paused"], "Esc resumed execution")
            press(b"\x1a")
            checkpoint(lambda state: not state["run_paused"] and not state["busy"], "global resume")
            press(b"\x0e\r")  # Ctrl-N selects a scope; Enter must not confirm a skip.
            checkpoint(lambda state: state["confirmation"] == "skip", "skip confirmation")
            press(b"\x1b[27u")
            checkpoint(lambda state: not state["menu"], "skip dismissal")
            press(b"\x03")
            assert read_message()["kind"] == "done"
            assert process.wait(timeout=3) == 0
            print("PASS: tui PTY scoped controls, confirmation, snapshot scrolling, live updates, resize, pause/resume and clean shutdown; synthetic runners only")
        finally:
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=3)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait(timeout=3)
            os.close(master)
            os.close(read_fd)


if __name__ == "__main__":
    run()
