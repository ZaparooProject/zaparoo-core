"""Deterministic synthetic guest fixtures; no copyrighted media or host mounts."""
from pathlib import Path
import shutil
import subprocess

SOURCE = Path(__file__).resolve().parent
CONFIG = '''error_reporting = false
debug_logging = true
[audio]
scan_feedback = false
[updates]
check = false
install = false
[readers]
auto_detect = false
[[readers.connect]]
driver = "file"
path = "/tmp/vm-reader.token"
'''
MAIN = '''#!/bin/sh
ln -sf /dev/null /dev/MrAudio
printf MENU > /tmp/CORENAME
'''
STARTUP = '''#!/bin/sh
printf 'VM_USER_STARTUP\\n'
'''
PREPARE = '''#!/bin/sh
set -eu
[ -b /dev/vda1 ]
mkdir /tmp/outer
mount -t exfat /dev/vda1 /tmp/outer
cp -r /tmp/payload/sd/. /tmp/outer/
sync
umount /tmp/outer
printf 'VM_DISK_PREPARED\\n'
'''


def text(path, content):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content)


def ext4(stage, image, size):
    with image.open('xb') as stream:
        stream.truncate(size)
    subprocess.run(['mke2fs', '-q', '-t', 'ext4', '-F', '-O', '^orphan_file,^metadata_csum_seed',
                    '-d', str(stage), str(image)], check=True, timeout=120)


def build(stage, image, binary, scenario):
    stage.mkdir(exist_ok=False)
    (stage / 'Scripts').mkdir()
    shutil.copyfile(binary, stage / 'Scripts/zaparoo.sh')
    text(stage / 'zaparoo/config.toml', CONFIG + f'scan_mode = "{"hold" if scenario == "launch" else "tap"}"\n')
    text(stage / 'MiSTer', MAIN)
    if scenario == 'launch':
        shutil.copyfile(SOURCE / 'main-sim.py', stage / 'main-sim.py')
        text(stage / 'games/SNES/VM Test & Game.sfc', 'Synthetic fixture; not a playable game.\n')
        text(stage / '_Console/SNES_20260905.rbf', 'Synthetic RBF; never program an FPGA.\n')
        text(stage / 'menu.rbf', 'Synthetic Menu.\n')
        text(stage / 'fixtures/missing.mgl', '''<mistergamedescription><rbf>_Console/SNES</rbf>
<file delay="2" type="f" index="0" path="../../../../../media/fat/games/SNES/Missing.sfc"/>
</mistergamedescription>''')
    ext4(stage, image, max(128 * 1024 * 1024, binary.stat().st_size * 2 + 32 * 1024 * 1024))
