//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package recalbox

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxbase"
	"github.com/jonboulle/clockwork"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

const testTemplateSystems = `<?xml version="1.0"?>
<systemList>
  <defaults command="python /usr/lib/python3.11/site-packages/configgen/emulatorlauncher.pyc"/>
  <system uuid="u-snes" name="snes" fullname="Super Nintendo Entertainment System">
    <descriptor path="%ROOT%/snes" extensions=".smc .sfc .ZIP .7z" theme="snes"/>
  </system>
  <system uuid="u-segacd" name="segacd" fullname="Mega CD">
    <descriptor path="%ROOT%/segacd" extensions=".cue .chd"/>
  </system>
  <system uuid="u-2048" name="2048" fullname="2048">
    <descriptor path="%ROOT%/ports/2048" extensions=".game"/>
  </system>
  <system uuid="u-outrun" name="outrun" fullname="OutRun">
    <descriptor path="%ROOT%/ports/OutRun" extensions=".zip"/>
  </system>
  <system uuid="u-240p" name="240ptestsuite" fullname="240p Test Suite">
    <descriptor path="%ROOT%/240ptestsuite" extensions=".k7"/>
  </system>
  <system uuid="u-fav" name="favorites" fullname="Favorites">
    <descriptor path="%ROOT%/favorites" extensions=".zip"/>
  </system>
  <system uuid="u-bad" name="nes" fullname="Escaping NES">
    <descriptor path="%ROOT%/../etc" extensions=".nes"/>
  </system>
</systemList>
`

const testUserSystems = `<?xml version="1.0"?>
<systemList>
  <system uuid="u-snes" name="snes" fullname="Super Nintendo (user)">
    <descriptor path="%ROOT%/snes" extensions=".sfc"/>
  </system>
  <system uuid="u-mame" name="mame" fullname="MAME">
    <descriptor path="/media/usb/arcade" extensions=""/>
  </system>
</systemList>
`

type mediaRecorder struct {
	current *models.ActiveMedia
	sets    chan *models.ActiveMedia
	mu      syncutil.Mutex
}

func newMediaRecorder() *mediaRecorder {
	return &mediaRecorder{sets: make(chan *models.ActiveMedia, 16)}
}

func (r *mediaRecorder) get() *models.ActiveMedia {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.current
}

func (r *mediaRecorder) set(m *models.ActiveMedia) {
	r.mu.Lock()
	r.current = m
	r.mu.Unlock()
	r.sets <- m
}

type signalRecord struct {
	pid int
	sig syscall.Signal
}

func newTestFs(t *testing.T) afero.Fs {
	t.Helper()
	fs := afero.NewMemMapFs()
	writeFile(t, fs, templateSystemList, testTemplateSystems)
	require.NoError(t, fs.MkdirAll(romsRoot, 0o755))
	return fs
}

func writeFile(t *testing.T, fs afero.Fs, path, content string) {
	t.Helper()
	require.NoError(t, fs.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, afero.WriteFile(fs, path, []byte(content), 0o644))
}

// addProc adds a fake /proc entry.
func addProc(t *testing.T, fs afero.Fs, pid, ppid int, comm string, args ...string) {
	t.Helper()
	dir := filepath.Join(procRoot, strconv.Itoa(pid))
	writeFile(t, fs, filepath.Join(dir, "stat"),
		strconv.Itoa(pid)+" ("+comm+") S "+strconv.Itoa(ppid)+" 1 1 0 -1")
	var cmdline strings.Builder
	for _, a := range args {
		cmdline.WriteString(a + "\x00")
	}
	writeFile(t, fs, filepath.Join(dir, "cmdline"), cmdline.String())
}

func removeProc(fs afero.Fs, pid int) {
	_ = fs.RemoveAll(filepath.Join(procRoot, strconv.Itoa(pid)))
}

// addRunningGame adds ES, configgen and an emulator below it.
func addRunningGame(t *testing.T, fs afero.Fs) {
	t.Helper()
	addProc(t, fs, 10, 1, "emulationstatio", "/usr/bin/emulationstation")
	addProc(t, fs, 20, 10, "sh", "sh", "-c", "python emulatorlauncher.pyc -system snes")
	addProc(t, fs, 30, 20, "python", "python",
		"/usr/lib/python3.11/site-packages/configgen/emulatorlauncher.pyc", "-system", "snes")
	addProc(t, fs, 40, 30, "retroarch", "retroarch", "-L", "snes9x_libretro.so", "/recalbox/share/roms/snes/a.sfc")
}

func newTestPlatform(t *testing.T, fs afero.Fs) (*Platform, *mediaRecorder, *clockwork.FakeClock) {
	t.Helper()
	clock := clockwork.NewFakeClock()
	rec := newMediaRecorder()
	p := &Platform{
		Base:           linuxbase.NewBase(platformids.Recalbox),
		fs:             fs,
		clock:          clock,
		wake:           make(chan struct{}, 1),
		changed:        make(chan struct{}),
		commandAddr:    "127.0.0.1:1",
		procDir:        procRoot,
		activeMedia:    rec.get,
		setActiveMedia: rec.set,
	}
	p.signal = func(pid int, sig syscall.Signal) error {
		t.Errorf("unexpected signal %s to %d", sig, pid)
		return nil
	}
	require.NoError(t, p.Base.StartPost(context.Background(), nil, nil, rec.get, rec.set, nil, nil))
	return p, rec, clock
}

func snesMedia() *models.ActiveMedia {
	return models.NewActiveMedia(systemdefs.SystemSNES, "SNES", snesGame, "Game", "SNES")
}

func stateText(action, systemID, game, path string) string {
	return "Action=" + action + "\nActionData=\nSystem=Some System\nSystemId=" + systemID +
		"\nGame=" + game + "\nGamePath=" + path + "\nImagePath=\nState=playing\n"
}
