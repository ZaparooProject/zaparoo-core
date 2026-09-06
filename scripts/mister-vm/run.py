#!/usr/bin/env python3
"""Run a disposable MiSTer VM scenario using local prepared assets."""
import argparse
import contextlib
import hashlib
import json
from pathlib import Path
import shutil
import signal
import socket
import subprocess
import time
import traceback
import uuid

import fixtures
from scenarios import Harness, launch, service

SOURCE = Path(__file__).resolve().parent
DEFAULT_ASSETS = SOURCE.parents[1] / '_scratch/mister-vm-assets'


class Deadline(BaseException):
    pass


def timed_out(_signal, _frame):
    raise Deadline('Whole-run deadline exceeded')


def interrupted(_signal, _frame):
    raise KeyboardInterrupt('Run interrupted')


def digest(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def validate_binary(path):
    if not path.is_file():
        raise ValueError('Expected regular ARM ELF binary')
    with path.open('rb') as stream:
        header = stream.read(20)
    if len(header) != 20 or header[:6] != b'\x7fELF\x01\x01' or header[18:20] != b'\x28\x00':
        raise ValueError('Expected little-endian ELF32 ARM, not an archive or shell script')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--assets', type=Path, default=DEFAULT_ASSETS)
    parser.add_argument('--binary', type=Path, required=True)
    parser.add_argument('--scenario', choices=('launch', 'service'), default='launch')
    parser.add_argument('--expect-version')
    parser.add_argument('--timeout', type=int, default=360)
    parser.add_argument('--keep-disk', action='store_true')
    args = parser.parse_args()
    if not __debug__:
        parser.error('Assertions required; do not use optimized Python')
    if not 1 <= args.timeout <= 3600:
        parser.error('--timeout must be between 1 and 3600 seconds')
    assets, binary = args.assets.resolve(), args.binary.resolve()
    if not assets.is_dir() or (assets / 'setup.lock').exists() or (assets / 'FAILED').exists():
        parser.error('Asset directory absent or setup in progress; run setup.py first')
    runs = assets / 'runs'
    if runs.is_symlink():
        parser.error('Refusing symlinked runs directory')
    runs.mkdir(exist_ok=True)
    output = runs / (time.strftime('%Y%m%dT%H%M%S') + '-' + uuid.uuid4().hex[:12])
    output.mkdir()
    state = dict(passed=False, output=str(output), scenario=args.scenario, binary=str(binary), started=time.time())
    stage, image, disk = output / 'staging', output / 'fixtures.ext4', output / 'disk.qcow2'
    base, kernel = assets / 'sd.raw', assets / 'kernel-out/arch/arm/boot/zImage'
    reservation, harness = socket.socket(), None
    old_alarm = signal.signal(signal.SIGALRM, timed_out)
    old_term = signal.signal(signal.SIGTERM, interrupted)
    signal.alarm(args.timeout)
    try:
        validate_binary(binary)
        for command in ('qemu-system-arm', 'qemu-img', 'mke2fs'):
            if not shutil.which(command):
                raise RuntimeError('Required executable missing: ' + command)
        state['base_sha256'], state['kernel_sha256'] = digest(base), digest(kernel)
        state['binary_sha256'] = digest(binary)
        state['sources'] = {p.name: digest(p) for p in SOURCE.iterdir() if p.suffix in ('.py', '.json', '.txt')}
        state['qemu'] = subprocess.run(['qemu-system-arm', '--version'], check=True, capture_output=True, text=True, timeout=10).stdout.splitlines()[0]
        reservation.bind(('127.0.0.1', 0))
        state['port'] = reservation.getsockname()[1]
        fixtures.build(stage, image, binary, args.scenario)
        if digest(stage / 'Scripts/zaparoo.sh') != state['binary_sha256']:
            raise RuntimeError('Input binary changed during copy')
        subprocess.run(['qemu-img', 'create', '-f', 'qcow2', '-F', 'raw', '-b', str(base), str(disk)], check=True, capture_output=True, timeout=20)
        (output / 'run.json').write_text(json.dumps(state, indent=2) + '\n')
        harness = Harness(assets, output, state['port'], state['binary_sha256'], args.expect_version)
        # A competing bind after release makes QEMU fail before API access.
        reservation.close()
        with (output / 'scenario.log').open('x') as log, contextlib.redirect_stdout(log), contextlib.redirect_stderr(log):
            try:
                {'launch': launch, 'service': service}[args.scenario](harness)
                harness.save(True)
            finally:
                harness.close()
        state['passed'] = True
    except BaseException as exc:
        state['error'] = type(exc).__name__ + ': ' + str(exc)
        (output / 'failure.txt').write_text(traceback.format_exc())
        if harness:
            harness.save(False)
    finally:
        signal.alarm(0)
        signal.signal(signal.SIGTERM, signal.SIG_IGN)
        reservation.close()
        try:
            if harness:
                harness.close()
                state['processes'] = harness.processes
            if 'base_sha256' in state:
                state['base_unchanged'] = digest(base) == state['base_sha256']
                state['kernel_unchanged'] = digest(kernel) == state['kernel_sha256']
                if not state['base_unchanged'] or not state['kernel_unchanged']:
                    raise RuntimeError('Base image or kernel changed during run')
            if stage.exists():
                shutil.rmtree(stage)
            if not args.keep_disk:
                disk.unlink(missing_ok=True)
                image.unlink(missing_ok=True)
            state['disk_retained'] = disk.exists()
        except Exception as exc:
            state['passed'] = False
            state['cleanup_error'] = str(exc)
        if not (output / 'results.json').exists():
            (output / 'results.json').write_text(json.dumps(dict(passed=False, checks=[])) + '\n')
        state['finished'] = time.time()
        (output / 'run.json').write_text(json.dumps(state, indent=2) + '\n')
        signal.signal(signal.SIGALRM, old_alarm)
        signal.signal(signal.SIGTERM, old_term)
    print(('PASS: ' if state['passed'] else 'FAIL: ') + str(output))
    if 'error' in state:
        print(state['error'])
    return 0 if state['passed'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
