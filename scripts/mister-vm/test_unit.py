#!/usr/bin/env python3
"""Fast tests; no QEMU, network, compiler or prepared assets required."""
import hashlib
import importlib.util
import io
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
from xml.sax.saxutils import escape

import setup

spec = importlib.util.spec_from_file_location('main_sim', Path(__file__).parent / 'main-sim.py')
sim = importlib.util.module_from_spec(spec)
spec.loader.exec_module(sim)


class SimulatorTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        old_root = sim.ROOT
        self.addCleanup(setattr, sim, 'ROOT', old_root)
        sim.ROOT = Path(self.temp.name)
        (sim.ROOT / '_Console').mkdir()
        (sim.ROOT / 'games/SNES').mkdir(parents=True)
        self.rbf = sim.ROOT / '_Console/SNES_20260905.rbf'
        self.rbf.write_text('fixture')
        (sim.ROOT / 'menu.rbf').write_text('fixture')
        self.game = sim.ROOT / 'games/SNES/VM Test & Game.sfc'
        self.game.write_text('fixture')
        self.mgl = sim.ROOT / 'test.mgl'
        self.mgl.write_text('<mistergamedescription><rbf>_Console/SNES</rbf><file delay="2" type="f" index="0" path="' + escape(str(self.game)) + '"/></mistergamedescription>')
        self.command = 'load_core ' + str(self.mgl)

    def test_menu(self):
        self.assertEqual(sim.resolve('load_core menu.rbf'), ('MENU', None))

    def test_snes_xml(self):
        self.assertEqual(sim.resolve(self.command), ('SNES', str(self.game)))

    def test_missing_game(self):
        self.game.unlink()
        with self.assertRaisesRegex(ValueError, 'missing'):
            sim.resolve(self.command)

    def test_missing_rbf(self):
        self.rbf.unlink()
        with self.assertRaisesRegex(ValueError, 'RBF'):
            sim.resolve(self.command)

    def test_wrong_slot(self):
        self.mgl.write_text(self.mgl.read_text().replace('index="0"', 'index="1"'))
        with self.assertRaisesRegex(ValueError, 'slot'):
            sim.resolve(self.command)

    def test_combined_command(self):
        with self.assertRaisesRegex(ValueError, 'combined'):
            sim.resolve(self.command + '\nload_core menu.rbf')

    def test_unknown_command(self):
        with self.assertRaisesRegex(ValueError, 'unsupported'):
            sim.resolve('unknown')

    def test_entities(self):
        self.mgl.write_text('<!DOCTYPE x [<!ENTITY y "z">]>' + self.mgl.read_text())
        with self.assertRaisesRegex(ValueError, 'XML declarations'):
            sim.resolve(self.command)


class FramingTests(unittest.TestCase):
    def test_single_command(self):
        self.assertEqual(sim.frames(b'load_core menu.rbf\n'), [b'load_core menu.rbf'])

    def test_coalesced_reads_stay_separate(self):
        # Two writes queued between reads must not become one rejected command.
        self.assertEqual(sim.frames(b'load_core a.mgl\nload_core menu.rbf\n'),
                         [b'load_core a.mgl', b'load_core menu.rbf'])

    def test_unterminated_command_is_a_frame(self):
        self.assertEqual(sim.frames(b'fb_cmd0 rgb32 0 1'), [b'fb_cmd0 rgb32 0 1'])

    def test_blank_writes_ignored(self):
        self.assertEqual(sim.frames(b'\n\n'), [])


class FetchTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.cache = Path(self.temp.name)
        self.data = b'pinned fixture'
        self.pin = dict(name='fixture.tar.gz', url='https://example.invalid/fixture', sha256=hashlib.sha256(self.data).hexdigest())

    def test_verified_download(self):
        with patch('urllib.request.urlopen', return_value=io.BytesIO(self.data)):
            target = setup.fetch(self.pin, self.cache)
        self.assertEqual(target.read_bytes(), self.data)
        self.assertFalse(target.with_name(target.name + '.part').exists())

    def test_valid_cache_never_downloads(self):
        (self.cache / self.pin['name']).write_bytes(self.data)
        with patch('urllib.request.urlopen', side_effect=AssertionError('Unexpected network')):
            setup.fetch(self.pin, self.cache)

    def test_invalid_cache_not_overwritten(self):
        target = self.cache / self.pin['name']
        target.write_bytes(b'wrong')
        with self.assertRaisesRegex(RuntimeError, 'Cached checksum'):
            setup.fetch(self.pin, self.cache)
        self.assertEqual(target.read_bytes(), b'wrong')

    def test_invalid_download_not_promoted(self):
        with patch('urllib.request.urlopen', return_value=io.BytesIO(b'wrong')):
            with self.assertRaisesRegex(RuntimeError, 'Download checksum'):
                setup.fetch(self.pin, self.cache)
        self.assertFalse((self.cache / self.pin['name']).exists())


if __name__ == '__main__':
    unittest.main()
