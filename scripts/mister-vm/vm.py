"""Headless MiSTer guest transport. All writable disks belong to caller."""
import os
from pathlib import Path
import selectors
import subprocess
import time


def drive(path):
    return str(Path(path).resolve()).replace(',', ',,')


class VM:
    def __init__(self, assets, disk, log, port=None, fixtures=None, provision=False):
        assets = Path(assets)
        self.log = Path(log).open('xb')
        self.buffer = b''
        self.selector = selectors.DefaultSelector()
        args = ['qemu-system-arm', '-machine', 'virt-10.2,highmem=off,gic-version=2',
                '-cpu', 'cortex-a15', '-smp', '2', '-m', '511', '-accel', 'tcg',
                '-kernel', str(assets / 'kernel-out/arch/arm/boot/zImage'),
                '-display', 'none', '-serial', 'stdio', '-monitor', 'none', '-no-reboot']
        if provision:
            args += ['-append', 'console=ttyAMA0,115200 root=/dev/vdc ro rootwait init=/bin/sh',
                     '-drive', f'if=none,id=root,file={drive(assets / "stock.img")},format=raw,readonly=on',
                     '-device', 'virtio-blk-device,drive=root', '-nic', 'none',
                     '-drive', f'if=none,id=payload,file={drive(fixtures)},format=raw,readonly=on',
                     '-device', 'virtio-blk-device,drive=payload',
                     '-drive', f'if=none,id=outer,file={drive(disk)},format=raw',
                     '-device', 'virtio-blk-device,drive=outer']
        else:
            root = '/dev/vdb1' if fixtures else '/dev/vda1'
            args += ['-append', f'console=ttyAMA0,115200 root={root} loop=linux/linux.img loop.max_part=8 ro rootwait',
                     '-drive', f'if=none,id=root,file={drive(disk)},format=qcow2',
                     '-device', 'virtio-blk-device,drive=root',
                     '-netdev', f'user,id=n,restrict=on,hostfwd=tcp:127.0.0.1:{port}-:7497',
                     '-device', 'virtio-net-device,netdev=n,mac=52:54:00:4d:53:01', '-rtc', 'base=utc']
            if fixtures:
                args += ['-drive', f'if=none,id=fixtures,file={drive(fixtures)},format=raw,readonly=on',
                         '-device', 'virtio-blk-device,drive=fixtures']
        try:
            self.process = subprocess.Popen(args, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
            self.selector.register(self.process.stdout, selectors.EVENT_READ)
        except BaseException:
            if hasattr(self, 'process'):
                self.close()
            else:
                self.log.close()
                self.selector.close()
            raise

    def wait(self, marker, timeout=120):
        until = time.monotonic() + timeout
        while marker not in self.buffer:
            if b'Kernel panic' in self.buffer:
                raise RuntimeError('Guest kernel panic: ' + self.buffer[-3000:].decode(errors='replace'))
            if time.monotonic() >= until:
                raise TimeoutError(f'Waiting for {marker!r}: {self.buffer[-2000:]!r}')
            for key, _ in self.selector.select(0.25):
                data = os.read(key.fd, 65536)
                if not data:
                    raise RuntimeError('QEMU exited: ' + self.buffer[-3000:].decode(errors='replace'))
                self.log.write(data)
                self.log.flush()
                self.buffer += data
        end = self.buffer.index(marker) + len(marker)
        result, self.buffer = self.buffer[:end], self.buffer[end:]
        return result.decode(errors='replace')

    def cmd(self, command, timeout=120):
        # Split marker prevents matching terminal echo rather than completion.
        line = command + "; rc=$?; printf '\\n%s%s%s\\n' __VM_ DONE_ $rc\n"
        self.process.stdin.write(line.encode())
        self.process.stdin.flush()
        result = self.wait(b'__VM_DONE_', timeout)
        status = self.wait(b'\n', 10).strip()
        print(result, flush=True)
        if status != '0':
            raise RuntimeError(f'Guest command exited {status}: {command}')
        return result

    def close(self):
        if self.log.closed:
            return
        try:
            if self.process.poll() is None:
                self.process.terminate()
            try:
                remaining, _ = self.process.communicate(timeout=10)
            except subprocess.TimeoutExpired:
                self.process.kill()
                remaining, _ = self.process.communicate()
            self.log.write(remaining or b'')
        finally:
            self.selector.close()
            self.log.close()
