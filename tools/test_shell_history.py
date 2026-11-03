#!/usr/bin/env python3
"""Exercise the production readline loop directly, without ADB or an agent."""
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

def main():
    version = b'round2-local-version'
    with tempfile.TemporaryDirectory(prefix='dropcheck-round2-pty-') as directory:
        binary = Path(directory) / 'round2-app.test'
        subprocess.run([os.environ.get('GO', 'go'), 'test', '-p=1', '-race', '-c', '-ldflags', '-X dropcheck/controller/internal/version.Version=round2-local-version', '-o', str(binary), './internal/app'], cwd=ROOT / 'controller', check=True, timeout=60)
        secret = b'set cli default-passphrase round2-synthetic-secret'
        cases = [
            ('arrows', secret + b'\x1b[A\x1b[B\r', 0, 1),
            ('control-p-n', secret + b'\x10\x0e\r', 0, 1),
            ('edited-recall', b'\x1b[A\x15' + secret + b'\x1b[A\x1b[B\r', 0, 1),
            ('submitted-secret', secret + b'\rclear cli default-passphrase\r\x1b[A\x1b[B\r', 1, 1),
            ('search', secret + b'\x12\x07\r', 0, 1),
            ('interrupt', secret + b'\x03\x1b[A\x1b[B\r', 0, 1),
            ('safe-draft', b'show version\x1b[A\x1b[B\r', 0, 2),
            ('safe-recall', b'\x1b[A\r', 0, 2),
        ]
        for name, keys, updates, versions in cases:
            run_case(binary, version, name, keys, updates, versions)
        run_case(binary, version, 'running-cancel', b'', 0, 2, cancel=True)

def run_case(binary, version, name, keys, updates, versions, cancel=False):
    test_name = 'TestPTYJunosRunningCancel' if cancel else 'TestPTYRound2NativeHistory'
    # An empty prompt, not a redisplay of the prompt prefix while editing.
    prompt = (b'R5CT12345>' if cancel else b'dropcheck>') + b'  \x08'
    pid, master = pty.fork()
    if pid == 0:
        os.environ['TERM'] = 'xterm-256color'
        os.environ['DROPCHECK_SHELL_PTY'] = '1'
        os.execv(str(binary), [str(binary), '-test.run', f'^{test_name}$', '-test.v', '-test.timeout', '4s'])
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
                    raise AssertionError(('PTY closed before required prompt/output', bytes(transcript[-4000:])))
                return
            if not block:
                if marker is not None:
                    raise AssertionError(('PTY closed before required prompt/output', bytes(transcript[-4000:])))
                return
            transcript.extend(block)
            if marker is not None and marker in transcript[after:]:
                return
        raise AssertionError(('PTY did not reach the required output or exit before its deadline', bytes(transcript[-4000:])))
    reaped = False
    def settle_prompt():
        # Search/history can redraw an empty buffer before Enter is processed.
        # Drain that burst and require the final empty prompt, not its prefix.
        deadline = time.monotonic() + 3
        while time.monotonic() < deadline:
            if not select.select([master], [], [], 0.05)[0]:
                if transcript.endswith(prompt):
                    return
                continue
            block = os.read(master, 65536)
            assert block, 'PTY closed before the next input boundary'
            transcript.extend(block)
        raise AssertionError('PTY did not settle at an empty input boundary')
    try:
        receive(prompt)
        os.write(master, b'show version\r')
        receive(version)
        # Output precedes the next raw-mode prompt. Do not let the terminal's
        # cooked-mode Ctrl-R/Ctrl-C handling consume the keys under test.
        receive(prompt, after=transcript.index(version) + len(version))
        settle_prompt()
        if cancel:
            log = Path(re.search(rb'PTY_CANCEL_LOG=([^\r\n]+)', transcript).group(1).decode())
            os.write(master, b'show adb timeout 10000\r')
            receive(b'ADB 1/10')
            deadline = time.monotonic() + 3
            while not log.exists() and time.monotonic() < deadline:
                time.sleep(0.01)
            assert log.exists(), 'The fake acquisition did not start'
            os.write(master, b'\x03')
            receive(b'ADB diagnostics FAILED')
            receive(prompt, after=transcript.index(b'ADB diagnostics FAILED'))
            settle_prompt()
            keys = b'show version\r'
        # Do not submit another command while the Shell is busy. Its execution
        # boundary deliberately discards typeahead, including local commands.
        events = re.findall(rb'.*?[\r\x03]', keys, re.DOTALL)
        assert b''.join(events) == keys
        for event in events:
            before = len(transcript)
            os.write(master, event)
            receive(b'\r\n', after=before)
            submitted = transcript.index(b'\r\n', before) + 2
            receive(prompt, after=submitted)
            settle_prompt()
        os.write(master, b'exit\r')
        receive(seconds=5)
        observed_updates = transcript.count(b'default passphrase updated (session only)')
        passed = f'--- PASS: {test_name}'.encode() in transcript
        completed = passed or f'--- FAIL: {test_name}'.encode() in transcript
        print(json.dumps({'case': name, 'real_readline': True, 'adb_and_agent_calls': 0, 'native_test_passed': passed, 'native_test_completed': completed}))
        assert b'WARNING: DATA RACE' not in transcript, bytes(transcript[-16000:])
        assert completed, ('PTY did not complete the native regression', bytes(transcript[-16000:]))
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
