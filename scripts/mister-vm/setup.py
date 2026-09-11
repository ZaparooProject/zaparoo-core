#!/usr/bin/env python3
"""Download pinned assets, build virtual-board kernel, provision file-only SD."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import signal
import struct
import subprocess
import tarfile
import time
import urllib.request
import zipfile

import fixtures
from vm import VM

SOURCE = Path(__file__).resolve().parent
DEFAULT = SOURCE.parents[1] / '_scratch/mister-vm-assets'
PINS = json.loads((SOURCE / 'pins.json').read_text())


def digest(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def fetch(pin, cache):
    cache.mkdir(parents=True, exist_ok=True)
    target = cache / pin['name']
    if not target.exists():
        # Failed partials are never cleaned up: the file may belong to a
        # concurrent download, and an owned one is evidence for diagnosis.
        partial = target.with_name(target.name + '.part')
        if partial.exists():
            raise RuntimeError('Partial download in progress or left by a failed run: ' + str(partial))
        with partial.open('xb') as out, urllib.request.urlopen(pin['url'], timeout=30) as response:
            start, total = time.monotonic(), 0
            while data := response.read(1024 * 1024):
                total += len(data)
                if total > 512 * 1024 * 1024 or time.monotonic() - start > 600:
                    raise RuntimeError('Download exceeded size/time bound')
                out.write(data)
        if digest(partial) != pin['sha256']:
            raise RuntimeError('Download checksum mismatch: ' + pin['name'])
        partial.rename(target)
    if digest(target) != pin['sha256']:
        raise RuntimeError('Cached checksum mismatch: ' + str(target))
    return target


def release(assets, cache):
    pin = PINS['release']
    archive = fetch(pin, cache)
    out = assets / 'releases' / pin['version']
    record = out / 'release.json'
    binary = out / 'zaparoo.sh'
    if record.exists():
        old = json.loads(record.read_text())
        if old['archive_sha256'] != pin['sha256'] or digest(binary) != old['binary_sha256']:
            raise RuntimeError('Existing release assets differ; choose a fresh asset directory')
        print(binary)
        return
    out.mkdir(parents=True, exist_ok=False)
    with zipfile.ZipFile(archive) as zipped:
        members = [x for x in zipped.infolist() if Path(x.filename).name == 'zaparoo.sh' and not x.is_dir()]
        if len(members) != 1 or members[0].file_size > 200 * 1024 * 1024:
            raise RuntimeError('Expected exactly one bounded zaparoo.sh member')
        with zipped.open(members[0]) as src, binary.open('xb') as dst:
            shutil.copyfileobj(src, dst)
    with binary.open('rb') as stream:
        if stream.read(6) != b'\x7fELF\x01\x01':
            raise RuntimeError('Published member is not an ELF32 little-endian binary')
    record.write_text(json.dumps(dict(version=pin['version'], url=pin['url'], archive_sha256=pin['sha256'], binary_sha256=digest(binary)), indent=2) + '\n')
    print(binary)


def build_kernel(assets, cache, compiler, host_include, host_bin, jobs):
    archive = fetch(PINS['kernel'], cache)
    extracted = assets / 'source'
    extracted.mkdir()
    with tarfile.open(archive) as tar:
        # Pin verification precedes extraction. Data filter also confines
        # symlinks/paths and rejects device nodes from the source archive.
        tar.extractall(extracted, filter='data')
    src = extracted / PINS['kernel']['prefix']
    out = assets / 'kernel-out'
    out.mkdir()
    args = ['make', '-C', str(src), f'O={out}', 'ARCH=arm', f'CROSS_COMPILE={compiler}']
    if host_include:
        args.append(f'HOSTCFLAGS=-O2 -I{host_include}')
    env = dict(os.environ, SOURCE_DATE_EPOCH='1636588800', KBUILD_BUILD_TIMESTAMP='Thu Nov 11 00:00:00 UTC 2021',
               KBUILD_BUILD_USER='vm-spike', KBUILD_BUILD_HOST='mister-virt', KBUILD_BUILD_VERSION='1')
    if host_bin:
        env['PATH'] = str(host_bin) + os.pathsep + env['PATH']
    with (assets / 'kernel-build.log').open('x') as log:
        def run(command):
            subprocess.run(command, env=env, stdout=log, stderr=subprocess.STDOUT, check=True, timeout=1800)
        run(args + ['MiSTer_defconfig'])
        cmd = [str(src / 'scripts/config'), '--file', str(out / '.config')]
        for name in ('ARCH_VIRT', 'SERIAL_AMBA_PL011', 'SERIAL_AMBA_PL011_CONSOLE', 'VIRTIO_MMIO', 'VIRTIO_BLK', 'VIRTIO_NET', 'RTC_DRV_PL031'):
            cmd += ['--enable', name]
        for name in ('ARCH_INTEL_SOCFPGA', 'FB_MISTER', 'SND_MISTER_AUDIO', 'GCC_PLUGINS', 'LOCALVERSION_AUTO'):
            cmd += ['--disable', name]
        run(cmd + ['--set-str', 'LOCALVERSION', '-mister-virt-spike'])
        run(args + ['olddefconfig'])
        run(args + [f'-j{jobs}', 'zImage'])
    diff = subprocess.run([str(src / 'scripts/diffconfig'), str(src / 'arch/arm/configs/MiSTer_defconfig'), str(out / '.config')], capture_output=True, text=True, check=True, timeout=30)
    (assets / 'kernel-config-delta.txt').write_text(diff.stdout)


def sparse(path, size):
    with path.open('xb') as out:
        out.truncate(size)


def prepare_image(assets, cache):
    archive = fetch(PINS['userspace'], cache)
    stock = assets / 'stock.img'
    with stock.open('xb') as out:
        subprocess.run(['7z', 'e', '-so', str(archive), PINS['userspace']['member']], stdout=out, stderr=subprocess.PIPE, check=True, timeout=180)
    if digest(stock) != PINS['userspace']['image_sha256']:
        raise RuntimeError('Stock rootfs checksum mismatch')
    stage = assets / 'provision-staging'
    inner = stage / 'sd/linux/linux.img'
    inner.parent.mkdir(parents=True)
    shutil.copyfile(stock, inner)

    def debug(command, path=inner):
        return subprocess.run(['debugfs', '-R', command, str(path)], capture_output=True, text=True, check=True, timeout=30).stdout

    original = debug('cat /etc/inittab', stock)
    lines = [x for x in original.splitlines() if not any(word in x for word in ('/sbin/agetty', '/sbin/gpm', '/bin/loadkeys', '/bin/setfont'))]
    lines.append('ttyAMA0::respawn:/bin/sh')
    inittab = assets / 'guest-inittab'
    inittab.write_text('\n'.join(lines) + '\n')
    commands = assets / 'debugfs.commands'
    commands.write_text('\n'.join(['rm /etc/inittab', f'write {json.dumps(str(inittab))} /etc/inittab',
                                  'set_inode_field /etc/inittab mode 0100644', 'rm /etc/init.d/S50sshd',
                                  'rm /etc/init.d/S50proftpd', 'rm /etc/init.d/S91smb']) + '\n')
    result = subprocess.run(['debugfs', '-w', '-f', str(commands), str(inner)], check=True, capture_output=True, text=True, timeout=60)
    (assets / 'rootfs-build.log').write_text(result.stdout + result.stderr)
    if debug('cat /etc/inittab') != inittab.read_text():
        raise RuntimeError('Offline init edit did not apply')
    for path in ('/etc/init.d/S50sshd', '/etc/init.d/S50proftpd', '/etc/init.d/S91smb'):
        if 'Inode:' in debug('stat ' + path):
            raise RuntimeError('Offline service disable failed: ' + path)
    fixtures.text(stage / 'sd/MiSTer', fixtures.MAIN)
    fixtures.text(stage / 'sd/linux/user-startup.sh', fixtures.STARTUP)
    fixtures.text(stage / 'prepare.sh', fixtures.PREPARE)
    (stage / 'sd/config').mkdir()
    payload = assets / 'payload.ext4'
    fixtures.ext4(stage, payload, 768 * 1024 * 1024)
    size, start = 2 * 1024 * 1024 * 1024, 2048
    sectors = size // 512 - start
    partition, outer = assets / 'outer.exfat', assets / 'sd.raw'
    sparse(partition, sectors * 512)
    subprocess.run(['mkfs.exfat', '-L', 'MISTERVM', str(partition)], check=True, capture_output=True, timeout=60)
    sparse(outer, size)
    mbr = bytearray(512)
    mbr[446:462] = struct.pack('<B3sB3sII', 0, b'\x00\x02\x00', 7, b'\xfe\xff\xff', start, sectors)
    mbr[510:512] = b'\x55\xaa'
    with outer.open('r+b') as dst, partition.open('rb') as src:
        dst.write(mbr)
        offset = start * 512
        while block := src.read(1024 * 1024):
            if block.strip(b'\0'):
                dst.seek(offset)
                dst.write(block)
            offset += len(block)
    vm = VM(assets, outer, assets / 'provision.log', fixtures=payload, provision=True)
    try:
        vm.wait(b'# ')
        result = vm.cmd('mount -t proc proc /proc; mount -t sysfs sysfs /sys; mount -t tmpfs tmpfs /tmp; mkdir /tmp/payload; mount -o ro /dev/vdb /tmp/payload; sh /tmp/payload/prepare.sh', timeout=240)
        if 'VM_DISK_PREPARED' not in result:
            raise RuntimeError('Guest did not confirm provisioning')
    finally:
        vm.close()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--assets', type=Path, default=DEFAULT)
    parser.add_argument('--download-cache', type=Path, default=DEFAULT.parent / 'mister-vm-downloads')
    parser.add_argument('--cross-compile', default='arm-linux-gnueabihf-')
    parser.add_argument('--host-include', type=Path)
    parser.add_argument('--host-bin', type=Path)
    parser.add_argument('--jobs', type=int, default=8)
    action = parser.add_mutually_exclusive_group()
    action.add_argument('--release-only', action='store_true')
    action.add_argument('--download-only', action='store_true', help='Populate verified kernel/userspace cache without building')
    args = parser.parse_args()
    assets, cache = args.assets.resolve(), args.download_cache.resolve()
    if args.release_only:
        release(assets, cache)
        return
    if args.download_only:
        for name in ('kernel', 'userspace'):
            print(fetch(PINS[name], cache))
        return
    if not 1 <= args.jobs <= 64:
        parser.error('--jobs must be between 1 and 64')
    ready = assets / 'ready.json'
    if ready.exists():
        old = json.loads(ready.read_text())
        if old['pins'] != PINS or old['base_sha256'] != digest(assets / 'sd.raw') or old['kernel_sha256'] != digest(assets / 'kernel-out/arch/arm/boot/zImage'):
            raise RuntimeError('Existing assets differ; choose a fresh asset directory')
        print('Already ready:', assets)
        return
    if assets.exists() and any(p.name != 'releases' for p in assets.iterdir()):
        raise RuntimeError('Incomplete/existing assets; preserve evidence and choose a fresh --assets path')
    if '/' in args.cross_compile:
        args.cross_compile = str(Path(args.cross_compile).resolve())
    if args.host_include:
        args.host_include = args.host_include.resolve()
    if args.host_bin:
        args.host_bin = args.host_bin.resolve()
    for command in ('make', 'gcc', 'flex', 'bison', 'debugfs', 'mke2fs', 'mkfs.exfat', '7z', 'qemu-system-arm', args.cross_compile + 'gcc'):
        if not shutil.which(command):
            raise RuntimeError('Install required build tool first: ' + command)
    assets.mkdir(parents=True, exist_ok=True)
    lock = assets / 'setup.lock'
    with lock.open('x') as stream:
        stream.write('setup in progress\n')
    try:
        compiler = subprocess.run([args.cross_compile + 'gcc', '--version'], capture_output=True, text=True, check=True, timeout=10).stdout.splitlines()[0]
        build_kernel(assets, cache, args.cross_compile, args.host_include, args.host_bin, args.jobs)
        prepare_image(assets, cache)
        ready.write_text(json.dumps(dict(pins=PINS, compiler=compiler, base_sha256=digest(assets / 'sd.raw'),
                                         kernel_sha256=digest(assets / 'kernel-out/arch/arm/boot/zImage')), indent=2) + '\n')
        print('Ready:', assets)
    except BaseException:
        (assets / 'FAILED').write_text('Setup incomplete; preserve logs and choose a fresh asset directory.\n')
        raise
    finally:
        lock.unlink(missing_ok=True)


def interrupted(_signal, _frame):
    raise KeyboardInterrupt('Setup interrupted')


if __name__ == '__main__':
    signal.signal(signal.SIGTERM, interrupted)
    main()
