#!/usr/bin/env python3
"""Explicit VM tests using setup.py assets, local binary and pinned release."""
import argparse
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import time
import unittest
from unittest.mock import patch

import run

SOURCE = Path(__file__).resolve().parent
ASSETS = SOURCE.parents[1] / '_scratch/mister-vm-assets'
BINARY = SOURCE.parents[1] / '_build/mister_arm/zaparoo.sh'


class IntegrationTests(unittest.TestCase):
    def command(self, binary=None, extra=()):
        return [sys.executable, str(SOURCE / 'run.py'), '--assets', str(ASSETS), '--binary', str(binary or BINARY), *extra]

    def evidence(self, process, expected):
        self.assertEqual(process.returncode, expected, process.stdout + (process.stderr or ''))
        line = next(x for x in process.stdout.splitlines() if x.startswith(('PASS: ', 'FAIL: ')))
        output = Path(line.split(': ', 1)[1])
        self.assertEqual(output.parent, ASSETS / 'runs')
        state = json.loads((output / 'run.json').read_text())
        self.assertEqual(state['passed'], expected == 0)
        self.assertNotIn('cleanup_error', state)
        self.assertFalse((output / 'staging').exists())
        self.assertTrue((output / 'results.json').exists())
        if 'base_sha256' in state:
            self.assertTrue(state['base_unchanged'])
            self.assertTrue(state['kernel_unchanged'])
        for item in state.get('processes', []):
            self.assertIsNotNone(item['exit_code'])
            with self.assertRaises(ProcessLookupError):
                os.kill(item['pid'], 0)
        if 'port' in state:
            with socket.socket() as listener:
                listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
                listener.bind(('127.0.0.1', state['port']))
                listener.listen(1)
        if not state.get('disk_retained'):
            self.assertFalse((output / 'disk.qcow2').exists())
            self.assertFalse((output / 'fixtures.ext4').exists())
        print(line, flush=True)
        return output, state

    def test_parallel_launch(self):
        release = ASSETS / 'releases/2.17.2/zaparoo.sh'
        commands = [self.command(release, ('--expect-version', '2.17.2', '--keep-disk')), self.command()]
        children = [subprocess.Popen(cmd, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True) for cmd in commands]
        results = []
        try:
            for child in children:
                stdout, _ = child.communicate(timeout=420)
                results.append(self.evidence(subprocess.CompletedProcess(child.args, child.returncode, stdout, ''), 0))
        finally:
            for child in children:
                if child.poll() is None:
                    child.terminate()
                    child.communicate(timeout=30)
        self.assertNotEqual(results[0][0], results[1][0])
        self.assertNotEqual(results[0][1]['port'], results[1][1]['port'])
        self.assertTrue(results[0][1]['disk_retained'])
        for output, state in results:
            checks = json.loads((output / 'results.json').read_text())['checks']
            self.assertEqual(next(x['evidence'] for x in checks if x['check'] == 'selected_binary_installed'), state['binary_sha256'])

    def test_service_release(self):
        result = subprocess.run(self.command(ASSETS / 'releases/2.17.2/zaparoo.sh', ('--scenario', 'service', '--expect-version', '2.17.2')), capture_output=True, text=True, timeout=420)
        output, state = self.evidence(result, 0)
        self.assertEqual(len(state['processes']), 2)
        checks = json.loads((output / 'results.json').read_text())['checks']
        self.assertIn('cold_boot_history', [x['check'] for x in checks])

    def test_system_exit_propagates(self):
        # Framework exits must survive the runner boundary; only its explicit
        # deadline and user interruption signals become failed-run reports.
        with (
            patch.object(sys, 'argv', self.command()[1:]),
            patch.object(run, 'digest', return_value='fixture-hash'),
            patch.object(run.shutil, 'which', return_value='/fixture/tool'),
            patch.object(run.subprocess, 'run', return_value=subprocess.CompletedProcess([], 0, 'QEMU fixture\n')),
            patch.object(run.fixtures, 'build', side_effect=SystemExit(23)),
        ):
            with self.assertRaises(SystemExit) as raised:
                run.main()
        self.assertEqual(raised.exception.code, 23)

    def test_version_failure(self):
        result = subprocess.run(self.command(extra=('--expect-version', 'intentional-mismatch')), capture_output=True, text=True, timeout=180)
        output, state = self.evidence(result, 1)
        self.assertIn('Expected intentional-mismatch', state['error'])
        self.assertTrue((output / 'failure.txt').exists())

    def test_deadline(self):
        result = subprocess.run(self.command(extra=('--timeout', '12')), capture_output=True, text=True, timeout=90)
        _, state = self.evidence(result, 1)
        self.assertIn('Deadline', state['error'])

    def test_bad_binary(self):
        result = subprocess.run(self.command(SOURCE / 'fixtures.py'), capture_output=True, text=True, timeout=30)
        _, state = self.evidence(result, 1)
        self.assertIn('ELF32', state['error'])

    def test_termination(self):
        before = set((ASSETS / 'runs').iterdir())
        child = subprocess.Popen(self.command(), stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
        try:
            ready, end = False, time.monotonic() + 60
            while time.monotonic() < end and child.poll() is None:
                for path in set((ASSETS / 'runs').iterdir()) - before:
                    log = path / 'boot-0.log'
                    if log.exists() and b'__VM_DONE_' in log.read_bytes():
                        ready = True
                        break
                if ready:
                    break
                time.sleep(0.1)
            self.assertTrue(ready)
            child.terminate()
            stdout, _ = child.communicate(timeout=30)
            _, state = self.evidence(subprocess.CompletedProcess(child.args, child.returncode, stdout, ''), 1)
            self.assertIn('KeyboardInterrupt', state['error'])
        finally:
            if child.poll() is None:
                child.terminate()
                child.communicate(timeout=30)


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--assets', type=Path, default=ASSETS)
    parser.add_argument('--binary', type=Path, default=BINARY)
    args, rest = parser.parse_known_args()
    ASSETS, BINARY = args.assets.resolve(), args.binary.resolve()
    unittest.main(argv=[sys.argv[0], *rest])
