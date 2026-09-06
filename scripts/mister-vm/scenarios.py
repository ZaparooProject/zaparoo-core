"""Public API scenarios against real daemon; no direct database/state mutation."""
import json
import shlex
import time

from websockets.sync.client import connect
from websockets.exceptions import WebSocketException
from vm import VM

GAME = '/media/fat/games/SNES/VM Test & Game.sfc'
MISSING = '/media/fat/games/SNES/Missing.sfc'
TOKEN = '**delay:1'


def wait(read, predicate, timeout=90):
    until = time.monotonic() + timeout
    last = None
    while time.monotonic() < until:
        try:
            last = read()
            if predicate(last):
                return last
        except (OSError, ValueError, RuntimeError, WebSocketException) as exc:
            last = str(exc)
        time.sleep(0.2)
    raise TimeoutError(str(last))


class Harness:
    def __init__(self, assets, output, port, binary_hash, expected_version):
        self.assets, self.output, self.port = assets, output, port
        self.binary_hash, self.expected_version = binary_hash, expected_version
        self.vm, self.socket = None, None
        self.checks, self.notifications, self.processes = [], [], []
        self.request_id = 0

    def record(self, name, evidence):
        self.checks.append(dict(check=name, evidence=evidence))
        print(name, json.dumps(evidence), flush=True)
        self.save(False)
        return evidence

    def save(self, passed):
        (self.output / 'results.json').write_text(json.dumps(dict(passed=passed, checks=self.checks), indent=2) + '\n')
        (self.output / 'notifications.json').write_text(json.dumps(self.notifications, indent=2) + '\n')

    def disconnect(self):
        if self.socket is not None:
            try:
                self.socket.close()
            finally:
                self.socket = None

    def close(self):
        try:
            self.disconnect()
        finally:
            if self.vm is not None:
                try:
                    self.vm.close()
                finally:
                    self.processes.append(dict(pid=self.vm.process.pid, exit_code=self.vm.process.returncode))
                    self.vm = None

    def rpc(self, method, params=None):
        if self.socket is None:
            self.socket = connect(f'ws://127.0.0.1:{self.port}/api/v0.1', open_timeout=3, close_timeout=3, proxy=None)
        self.request_id += 1
        try:
            self.socket.send(json.dumps(dict(jsonrpc='2.0', id=self.request_id, method=method, params=params)))
            while True:
                data = json.loads(self.socket.recv(timeout=3))
                if data.get('id') == self.request_id:
                    if 'error' in data:
                        raise RuntimeError(data['error'])
                    return data['result']
                self.notifications.append(data)
        except (OSError, WebSocketException):
            self.disconnect()
            raise

    def boot(self, install=True):
        self.vm = VM(self.assets, self.output / 'disk.qcow2', self.output / f'boot-{len(self.processes)}.log',
                     port=self.port, fixtures=self.output / 'fixtures.ext4')
        self.vm.wait(b'# ')
        if install:
            self.vm.cmd('mkdir /tmp/fixtures; mount -o ro /dev/vda /tmp/fixtures && cp -r /tmp/fixtures/. /media/fat/ && umount /tmp/fixtures')
        self.vm.cmd('i=0; until ip -4 addr show eth0 | grep -q "inet 10.0.2.15/"; do i=$((i+1)); [ "$i" -lt 45 ] || break; sleep 1; done; ip -4 addr show eth0 | grep "inet 10.0.2.15/"')
        self.vm.cmd('test -c /dev/uinput && test "$(grep -c ^processor /proc/cpuinfo)" = 2 && grep " / ext4 ro" /proc/mounts && grep " /media/fat exfat rw" /proc/mounts && grep " /tmp tmpfs " /proc/mounts && grep " /run tmpfs " /proc/mounts')
        value = self.vm.cmd('sha256sum /media/fat/Scripts/zaparoo.sh')
        assert self.binary_hash + '  /media/fat/Scripts/zaparoo.sh' in value
        self.record('selected_binary_installed', self.binary_hash)

    def start(self):
        self.vm.cmd('/media/fat/Scripts/zaparoo.sh -service start')
        version = wait(lambda: self.rpc('version'), lambda r: r.get('platform') == 'mister')
        self.record('api_ready', version)
        if self.expected_version is not None:
            assert version['version'] == self.expected_version, f'Expected {self.expected_version}, got {version["version"]}'
        assert self.rpc('health') == {'status': 'ok'}
        readers = wait(lambda: self.rpc('readers'), lambda r: any(x.get('driver') == 'file' and x.get('connected') for x in r.get('readers', [])))
        self.record('reader_connected', readers)
        return next(x for x in readers['readers'] if x['driver'] == 'file')

    def scan(self, text):
        self.vm.cmd('printf %s ' + shlex.quote(text) + ' > /tmp/vm-reader.token')

    def guest(self, expression):
        code = 'import json; print("VM_JSON:"+json.dumps(' + expression + '))'
        result = self.vm.cmd('python3 -c ' + shlex.quote(code))
        return json.loads(next(x[8:] for x in result.splitlines() if x.startswith('VM_JSON:')))

    def state(self):
        return self.guest('json.load(open("/tmp/mister-sim-status.json"))')

    def events(self):
        return self.guest('[json.loads(x) for x in open("/tmp/mister-sim-events.jsonl")]')


def launch(h):
    h.boot()
    h.vm.cmd('python3 /media/fat/main-sim.py >/tmp/mister-sim.stderr 2>&1 & sim=$!; echo "$sim"')
    wait(h.state, lambda r: r['core'] == 'MENU')
    assert h.start()['scanMode'] == 'hold'
    h.rpc('media.generate', {'systems': ['SNES']})
    h.record('indexed_fixture', wait(lambda: h.rpc('media.search', {'systems': ['SNES']}), lambda r: any(x['path'] == GAME for x in r['results']), 180))
    h.scan(GAME)
    h.record('simulator_snes', wait(h.state, lambda r: r['core'] == 'SNES' and r['game'] == GAME))
    h.record('active_media', wait(lambda: h.rpc('media'), lambda r: any(x['mediaPath'] == GAME and x['systemId'] == 'SNES' for x in r['active'])))
    h.record('token_success', wait(lambda: h.rpc('tokens.history'), lambda r: any(x['text'] == GAME and x['success'] for x in r['entries'])))
    mgl = h.guest('open("/media/fat/.LASTLAUNCH.mgl").read()')
    assert '&amp;' in mgl
    h.record('generated_mgl', mgl)
    session = wait(lambda: h.rpc('media.history'), lambda r: any(x['mediaPath'] == GAME for x in r['entries']))['entries'][0]
    h.scan('')
    h.record('removal_menu', wait(h.state, lambda r: r['core'] == 'MENU'))
    h.record('media_cleared', wait(lambda: h.rpc('media'), lambda r: not r['active']))
    h.record('session_closed', wait(lambda: h.rpc('media.history'), lambda r: any(x['mediaPath'] == GAME and x['startedAt'] == session['startedAt'] and x.get('endedAt') for x in r['entries'])))
    wait(lambda: h.rpc('tokens'), lambda r: not r['active'])
    h.vm.cmd("printf %s 'load_core /media/fat/fixtures/missing.mgl' > /dev/MiSTer_cmd")
    wait(h.events, lambda r: r[-1]['kind'] == 'rejected')
    assert h.state()['core'] == 'MENU' and not h.rpc('media')['active']
    h.scan(MISSING)
    h.record('trusted_path_history', wait(lambda: h.rpc('tokens.history'), lambda r: any(x['text'] == MISSING and x['success'] for x in r['entries'])))
    # Owner-confirmed contract: submission is not verified loading. Never add
    # ZIP inspection to Core's hot path to satisfy this synthetic observer.
    assert h.state()['core'] == 'MENU'
    assert any(x['mediaPath'] == MISSING for x in h.rpc('media')['active'])
    events = h.events()
    assert events[-2]['command'] == 'load_core /media/fat/.LASTLAUNCH.mgl'
    assert events[-1]['kind'] == 'rejected'
    h.record('fifo_events', events)
    h.scan('')
    wait(lambda: h.rpc('tokens'), lambda r: not r['active'])
    h.vm.cmd('/media/fat/Scripts/zaparoo.sh -service stop && sync')


def service(h):
    h.boot()
    reader = h.start()
    identity = reader['readerId']
    h.vm.cmd('test -s /media/fat/zaparoo/user.db && test -s /media/fat/zaparoo/media.db')
    before = h.rpc('tokens.history')['entries']
    h.scan(TOKEN)
    h.record('reader_scan', wait(lambda: h.rpc('tokens'), lambda r: any(x['text'] == TOKEN for x in r['active'])))
    history = wait(lambda: h.rpc('tokens.history'), lambda r: any(x['text'] == TOKEN and x['success'] and x not in before for x in r['entries']))
    entry = next(x for x in history['entries'] if x['text'] == TOKEN and x not in before)
    h.record('new_history_entry', entry)
    h.scan('')
    h.record('reader_removal', wait(lambda: h.rpc('tokens'), lambda r: not r['active']))
    h.vm.cmd('/media/fat/Scripts/zaparoo.sh -service restart')
    h.disconnect()
    wait(lambda: h.rpc('version'), lambda r: r['platform'] == 'mister')
    h.record('restart_identity', wait(lambda: h.rpc('readers'), lambda r: any(x.get('readerId') == identity and x['connected'] for x in r['readers'])))
    h.record('restart_history', wait(lambda: h.rpc('tokens.history'), lambda r: entry in r['entries']))
    h.vm.cmd('/media/fat/Scripts/zaparoo.sh -service stop && sync')
    h.disconnect()
    try:
        h.rpc('health')
    except (OSError, WebSocketException):
        h.record('api_stopped', True)
    else:
        raise AssertionError('API responds after service stop')
    h.close()
    h.boot(install=False)
    assert h.start()['readerId'] == identity
    h.record('cold_boot_history', wait(lambda: h.rpc('tokens.history'), lambda r: entry in r['entries']))
    tokens = h.rpc('tokens')
    assert not tokens.get('active') and not tokens.get('last')
    h.record('tmpfs_reset', tokens)
    h.vm.cmd('test ! -s /tmp/vm-reader.token && /media/fat/Scripts/zaparoo.sh -service stop && sync')
