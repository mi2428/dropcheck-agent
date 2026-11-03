#!/usr/bin/env python3
"""Run the retained synthetic QA regressions. No devices or network probes."""
import argparse
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[3]


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('target', choices=['go', 'android'])
    args = parser.parse_args()
    if args.target == 'go':
        command = ['go', 'test', '-p=1', '-count=1', '-run', 'TestAdversarialQA', '-v', './internal/app', './internal/command', './internal/render']
        cwd = ROOT / 'controller'
    else:
        command = ['./gradlew', ':agent:testDebugUnitTest', '--tests', 'io.dropcheck.agent.AdversarialShellQaTest']
        cwd = ROOT
    raise SystemExit(subprocess.run(command, cwd=cwd, timeout=120).returncode)


if __name__ == '__main__':
    main()
