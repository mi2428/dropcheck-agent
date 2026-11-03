#!/usr/bin/env python3
"""Exercise the production readline loop directly, without ADB or an agent."""
import fcntl
import json
import os
from pathlib import Path
import pty
import select
import signal
import struct
import subprocess
import tempfile
import termios
import time

ROOT = Path(__file__).resolve().parents[1]

def main():
    version = b'round2-local-version'
    with tempfile.TemporaryDirectory(prefix='dropcheck-round2-pty-') as directory:
        binary = Path(directory) / 'round2-app.test'
        subprocess.run([os.environ.get('GO', 'go'), 'test', '-p=1', '-c', '-ldflags', '-X dropcheck/controller/internal/version.Version=round2-local-version', '-o', str(binary), './internal/app'], cwd=ROOT / 'controller', check=True, timeout=60)
        secret = b'set default passphrase round2-synthetic-secret'
        cases = [
            ('arrows', secret + b'\x1b[A\x1b[B\r', 0, 1),
            ('control-p-n', secret + b'\x10\x0e\r', 0, 1),
            ('edited-recall', b'\x1b[A\x15' + secret + b'\x1b[A\x1b[B\r', 0, 1),
            ('submitted-secret', secret + b'\rclear default passphrase\r\x1b[A\x1b[B\r', 1, 1),
            ('search', secret + b'\x12\x07\r', 0, 1),
            ('interrupt', secret + b'\x03\x1b[A\x1b[B\r', 0, 1),
            ('safe-draft', b'show version\x1b[A\x1b[B\r', 0, 2),
            ('safe-recall', b'\x1b[A\r', 0, 2),
        ]
        for name, keys, updates, versions in cases:
            run_case(binary, version, name, keys, updates, versions)

def run_case(binary, version, name, keys, updates, versions):
    pid, master = pty.fork()
    if pid == 0:
        os.environ['TERM'] = 'xterm-256color'
        os.environ['DROPCHECK_SHELL_PTY'] = '1'
        os.execv(str(binary), [str(binary), '-test.run', '^TestPTYRound2NativeHistory$', '-test.v'])
    fcntl.ioctl(master, termios.TIOCSWINSZ, struct.pack('HHHH', 36, 100, 0, 0))
    transcript = bytearray()
    def receive(marker=None, seconds=4, after=0):
        if marker is not None and marker in transcript[after:]:
            return
        limit = time.monotonic() + seconds
        while time.monotonic() < limit:
            readable, _, _ = select.select([master], [], [], max(0, limit - time.monotonic()))
            if not readable:
                break
            try:
                block = os.read(master, 65536)
            except OSError:
                if marker is not None:
                    raise AssertionError('PTY closed before required prompt/output')
                return
            if not block:
                if marker is not None:
                    raise AssertionError('PTY closed before required prompt/output')
                return
            transcript.extend(block)
            if marker is not None and marker in transcript[after:]:
                return
        raise AssertionError('PTY did not reach the required output or exit before its deadline')
    reaped = False
    try:
        receive(b'dropcheck>')
        os.write(master, b'show version\r')
        receive(version)
        # Output precedes the next raw-mode prompt. Do not let the terminal's
        # cooked-mode Ctrl-R/Ctrl-C handling consume the keys under test.
        receive(b'dropcheck>', after=transcript.index(version) + len(version))
        os.write(master, keys + b'exit\r')
        receive(seconds=5)
        observed_updates = transcript.count(b'default passphrase updated (session only)')
        passed = b'--- PASS: TestPTYRound2NativeHistory' in transcript
        completed = passed or b'--- FAIL: TestPTYRound2NativeHistory' in transcript
        print(json.dumps({'case': name, 'real_readline': True, 'adb_and_agent_calls': 0, 'native_test_passed': passed, 'native_test_completed': completed}))
        assert b'WARNING: DATA RACE' not in transcript, bytes(transcript[-16000:])
        assert completed, 'PTY did not complete the native regression'
        assert observed_updates == updates, (name, observed_updates, bytes(transcript[-12000:]))
        assert transcript.count(version) == versions, 'Safe command history/draft was lost'
        assert passed, 'PTY did not complete the native regression successfully'
        _, status = os.waitpid(pid, 0)
        reaped = True
        assert os.waitstatus_to_exitcode(status) == 0, (name, status)
    finally:
        os.close(master)
        if not reaped:
            waited, _ = os.waitpid(pid, os.WNOHANG)
            if waited == 0:
                os.kill(pid, signal.SIGTERM)
                os.waitpid(pid, 0)

if __name__ == '__main__':
    main()
