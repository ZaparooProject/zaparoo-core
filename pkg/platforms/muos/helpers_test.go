//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later
//
// This file is part of Zaparoo Core.
//
// Zaparoo Core is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// Zaparoo Core is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.

package muos

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/jonboulle/clockwork"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

const (
	testProc   = "/proc"
	testSelf   = 1
	testROMs   = "/mnt/mmc/ROMS"
	testSDROMs = "/mnt/sdcard/ROMS"
)

const gbaManifest = `{
  "catalogue": "Nintendo Game Boy Advance",
  "lookup": 0,
  "default": "mgba",
  "governor": "balanced",
  "control": "gba",
  "friendly": ["gba", "gameboyadvance"],
  "libretro": {
    "armgba": {"core": "armgba_libretro.so", "require": {"arch": ["aarch64"]}},
    "gpsp": {"core": "gpsp_libretro.so"},
    "mgba": {"core": "mgba_libretro.so", "governor": "performance"}
  }
}`

const mdManifest = `{
  "catalogue": "Sega Mega Drive - Genesis",
  "lookup": "1",
  "friendly": ["md", "genesis", "megadrive"],
  "libretro": {
    "picodrive": {"core": "picodrive_libretro.so", "extra": 1},
    "genesis plus gx": {"core": "genesis_plus_gx_libretro.so"}
  }
}`

func writeFile(t *testing.T, fs afero.Fs, path, content string) {
	t.Helper()
	require.NoError(t, fs.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, afero.WriteFile(fs, path, []byte(content), 0o644))
}

func readFile(t *testing.T, fs afero.Fs, path string) string {
	t.Helper()
	data, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	return string(data)
}

// newTestFs returns a muOS file system with the stock GBA and Mega Drive
// manifests and an empty /proc.
func newTestFs(t *testing.T) afero.Fs {
	t.Helper()
	fs := afero.NewMemMapFs()
	core := "/opt/muos/share/info/manifest/core"
	writeFile(t, fs, filepath.Join(core, "Nintendo", "Nintendo Game Boy Advance.json"), gbaManifest)
	writeFile(t, fs, filepath.Join(core, "Sega", "Sega Mega Drive - Genesis.json"), mdManifest)
	require.NoError(t, fs.MkdirAll(testProc, 0o755))
	require.NoError(t, fs.MkdirAll(runDir, 0o755))
	require.NoError(t, fs.MkdirAll(testROMs, 0o755))
	return fs
}

func addProc(t *testing.T, fs afero.Fs, pid int, comm string, args ...string) {
	t.Helper()
	dir := filepath.Join(testProc, strconv.Itoa(pid))
	writeFile(t, fs, filepath.Join(dir, "comm"), comm+"\n")
	writeFile(t, fs, filepath.Join(dir, "stat"), strconv.Itoa(pid)+" ("+comm+") S 1 1")
	writeFile(t, fs, filepath.Join(dir, "cmdline"), strings.Join(args, "\x00")+"\x00")
}

func removeProc(fs afero.Fs, pid int) {
	_ = fs.RemoveAll(filepath.Join(testProc, strconv.Itoa(pid)))
}

// fakeSignals records signals and lets a test decide how each process
// reacts to them.
type fakeSignals struct {
	onSignal func(pid int, sig syscall.Signal)
	sent     []sentSignal
	mu       syncutil.Mutex
}

type sentSignal struct {
	pid int
	sig syscall.Signal
}

func (f *fakeSignals) send(pid int, sig syscall.Signal) error {
	f.mu.Lock()
	f.sent = append(f.sent, sentSignal{pid: pid, sig: sig})
	handler := f.onSignal
	f.mu.Unlock()
	if handler != nil {
		handler(pid, sig)
	}
	return nil
}

func (f *fakeSignals) signals() []sentSignal {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]sentSignal(nil), f.sent...)
}

// mediaRecorder stands in for the service's active media state.
type mediaRecorder struct {
	media   *models.ActiveMedia
	history []*models.ActiveMedia
	mu      syncutil.Mutex
}

func (m *mediaRecorder) get() *models.ActiveMedia {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.media
}

func (m *mediaRecorder) set(media *models.ActiveMedia) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.media = media
	m.history = append(m.history, media)
}

func newTestPlatform(t *testing.T, fs afero.Fs) (*Platform, *fakeSignals, *mediaRecorder) {
	t.Helper()
	sigs := &fakeSignals{}
	media := &mediaRecorder{}
	p := &Platform{
		fs:             fs,
		clock:          clockwork.NewFakeClock(),
		signal:         sigs.send,
		sleep:          func(time.Duration) {},
		procPath:       testProc,
		pid:            testSelf,
		arch:           "aarch64",
		activeMedia:    media.get,
		setActiveMedia: media.set,
	}
	return p, sigs, media
}

var errInjected = errors.New("injected failure")

// failFs fails one operation on one path, for the error branches a memory
// file system never takes. op is "open", "read", "write", "sync", "close",
// "rename", "remove" or "mkdir". The first skip matching calls succeed.
type failFs struct {
	afero.Fs
	op   string
	path string
	skip int
}

func (f *failFs) hit(op, name string) bool {
	if f.op != op || filepath.Clean(name) != f.path {
		return false
	}
	if f.skip > 0 {
		f.skip--
		return false
	}
	return true
}

func (f *failFs) wrap(file afero.File, name string) afero.File {
	if filepath.Clean(name) != f.path {
		return file
	}
	switch f.op {
	case "read", "write", "sync", "close":
		return &failFile{File: file, op: f.op}
	}
	return file
}

func (f *failFs) Open(name string) (afero.File, error) {
	if f.hit("open", name) {
		return nil, errInjected
	}
	file, err := f.Fs.Open(name)
	if err != nil {
		return nil, err //nolint:wrapcheck // test double passes errors through
	}
	return f.wrap(file, name), nil
}

func (f *failFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if f.hit("open", name) {
		return nil, errInjected
	}
	file, err := f.Fs.OpenFile(name, flag, perm)
	if err != nil {
		return nil, err //nolint:wrapcheck // test double passes errors through
	}
	return f.wrap(file, name), nil
}

func (f *failFs) MkdirAll(path string, perm os.FileMode) error {
	if f.hit("mkdir", path) {
		return errInjected
	}
	return f.Fs.MkdirAll(path, perm) //nolint:wrapcheck // test double passes errors through
}

func (f *failFs) Rename(oldname, newname string) error {
	if f.hit("rename", oldname) {
		return errInjected
	}
	return f.Fs.Rename(oldname, newname) //nolint:wrapcheck // test double passes errors through
}

func (f *failFs) Remove(name string) error {
	if f.hit("remove", name) {
		return errInjected
	}
	return f.Fs.Remove(name) //nolint:wrapcheck // test double passes errors through
}

type failFile struct {
	afero.File
	op string
}

func (f *failFile) Read(b []byte) (int, error) {
	if f.op == "read" {
		return 0, errInjected
	}
	return f.File.Read(b) //nolint:wrapcheck // test double passes errors through
}

func (f *failFile) Write(b []byte) (int, error) {
	if f.op == "write" {
		return 0, errInjected
	}
	return f.File.Write(b) //nolint:wrapcheck // test double passes errors through
}

func (f *failFile) WriteString(s string) (int, error) {
	if f.op == "write" {
		return 0, errInjected
	}
	return f.File.WriteString(s) //nolint:wrapcheck // test double passes errors through
}

func (f *failFile) Sync() error {
	if f.op == "sync" {
		return errInjected
	}
	return f.File.Sync() //nolint:wrapcheck // test double passes errors through
}

func (f *failFile) Close() error {
	err := f.File.Close()
	if f.op == "close" {
		return errInjected
	}
	return err //nolint:wrapcheck // test double passes errors through
}
