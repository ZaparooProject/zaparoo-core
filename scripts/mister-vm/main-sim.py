#!/usr/bin/env python3
"""One-system Main boundary substitute, not FPGA emulation or hardware parity.

Owns FIFO, CORENAME and RBFNAME only. Never writes ACTIVEGAME or Core databases.
"""
import json
import os
from pathlib import Path
import select
import time
import xml.etree.ElementTree as ET

ROOT = Path('/media/fat')


def resolve(command):
    if not command.startswith('load_core '):
        raise ValueError('unsupported command')
    argument = command[len('load_core '):]
    if any(ord(char) < 32 for char in argument):
        raise ValueError('control character or combined commands')
    path = Path(os.path.normpath(str(ROOT / argument)))
    if path == ROOT / 'menu.rbf':
        if not path.is_file():
            raise ValueError('menu fixture missing')
        return 'MENU', None
    if path.suffix.lower() != '.mgl' or path.stat().st_size > 16384:
        raise ValueError('unsupported or oversized launch file')
    data = path.read_text()
    if '<!DOCTYPE' in data or '<!ENTITY' in data:
        raise ValueError('XML declarations unsupported')
    document = ET.fromstring(data)
    if document.tag != 'mistergamedescription' or document.attrib:
        raise ValueError('unsupported MGL root')
    if [child.tag for child in document] != ['rbf', 'file']:
        raise ValueError('expected one SNES file')
    if document[0].text != '_Console/SNES' or document[0].attrib:
        raise ValueError('unsupported core')
    attributes = document[1].attrib
    if set(attributes) != {'delay', 'type', 'index', 'path'}:
        raise ValueError('unsupported file attributes')
    if (attributes['delay'], attributes['type'], attributes['index']) != ('2', 'f', '0'):
        raise ValueError('unsupported slot')
    game = Path(os.path.normpath(str(ROOT / 'games/SNES' / attributes['path'])))
    if game.parent != ROOT / 'games/SNES' or game.suffix != '.sfc' or not game.is_file():
        raise ValueError('missing or unsupported SNES game')
    cores = list((ROOT / '_Console').glob('SNES_*.rbf'))
    if len(cores) != 1 or not cores[0].is_file():
        raise ValueError('expected one SNES RBF')
    return 'SNES', str(game)


def event(kind, **data):
    with Path('/tmp/mister-sim-events.jsonl').open('a') as stream:
        stream.write(json.dumps(dict(kind=kind, time=time.time(), **data)) + '\n')


def publish(core, game=None):
    # Truncating writes, as in Main; no atomic-rename substitute for inotify.
    Path('/tmp/CORENAME').write_text(core)
    Path('/tmp/RBFNAME').write_text(core)
    Path('/tmp/mister-sim-status.json').write_text(json.dumps(dict(core=core, game=game)))
    event('state', core=core, game=game)


def main():
    Path('/tmp/mister-sim-events.jsonl').write_text('')
    fifo = Path('/dev/MiSTer_cmd')
    if fifo.exists():
        fifo.unlink()
    os.mkfifo(str(fifo), 0o666)
    fd = os.open(str(fifo), os.O_RDWR | os.O_NONBLOCK | os.O_CLOEXEC)
    poll = select.poll()
    poll.register(fd, select.POLLIN)
    publish('MENU')
    try:
        while True:
            poll.poll()
            raw = os.read(fd, 1023)
            if raw.endswith(b'\n'):
                raw = raw[:-1]
            try:
                command = raw.decode('utf-8')
                event('command', command=command)
                core, game = resolve(command)
                time.sleep(0.1)  # Synthetic delay, not calibrated FPGA loading.
                publish(core, game)
            except (ValueError, OSError, ET.ParseError) as exc:
                event('rejected', reason=str(exc))
    finally:
        os.close(fd)


if __name__ == '__main__':
    main()
