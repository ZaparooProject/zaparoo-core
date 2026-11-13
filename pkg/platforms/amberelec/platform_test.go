//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package amberelec

import (
	"context"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esbase"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/jonboulle/clockwork"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testProcRoot = "/proc"

type killed struct {
	pid int
	sig syscall.Signal
}

func newStopper(t *testing.T, procs map[int]string) (*stopper, *[]killed) {
	t.Helper()
	fs := afero.NewMemMapFs()
	for pid, comm := range procs {
		dir := filepath.Join(testProcRoot, strconv.Itoa(pid))
		require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "comm"), []byte(comm+"\n"), 0o644))
		require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "cmdline"), []byte("/usr/bin/"+comm+"\x00"), 0o644))
	}
	var sent []killed
	return &stopper{
		fs: fs,
		killer: &esbase.ProcessKiller{
			FS:    fs,
			Clock: clockwork.NewFakeClock(),
			Signal: func(pid int, sig syscall.Signal) error {
				sent = append(sent, killed{pid: pid, sig: sig})
				return fs.RemoveAll(filepath.Join(testProcRoot, strconv.Itoa(pid)))
			},
			ProcPath: testProcRoot,
			Grace:    5 * time.Second,
			Poll:     time.Second,
			SelfPID:  2,
		},
	}, &sent
}

func writeJSListen(t *testing.T, fs afero.Fs, program string) {
	t.Helper()
	data := "[Generic]\nprogram=" + program + "\nbutton1=6\n"
	require.NoError(t, afero.WriteFile(fs, JSListenConfig, []byte(data), 0o644))
}

func TestPlatform_Identity(t *testing.T) {
	t.Parallel()

	p := NewPlatform()
	assert.Equal(t, platformids.AmberELEC, p.ID())
	settings := p.Settings()
	assert.Equal(t, "/storage/.local/share/zaparoo", settings.DataDir)
	assert.Equal(t, "/storage/.config/zaparoo", settings.ConfigDir)
	assert.Equal(t, "/storage/.local/share/zaparoo/logs", settings.LogDir)
	assert.False(t, p.ManagedByPackageManager())

	opts := Options()
	assert.Equal(t, []string{"/usr/config/emulationstation", "/storage/.config/emulationstation"}, opts.ESConfigDirs)
	assert.Equal(t, "/storage/roms", opts.DefaultROMRoot)
	assert.NotNil(t, opts.ExitGame)
	assert.NotNil(t, opts.KillGame, "AmberELEC's /emukill does nothing")
	assert.Nil(t, opts.Controls, "AmberELEC has no hotkeygen")
	assert.Equal(t, time.Second, opts.KillRetryDelay)
}

func TestRootDirs_AlwaysIncludesAmberELECROMs(t *testing.T) {
	t.Parallel()

	cfg, err := helpers.NewTestConfig(helpers.NewMemoryFS(), t.TempDir())
	require.NoError(t, err)
	assert.Contains(t, NewPlatform().RootDirs(cfg), "/storage/roms")
}

func TestExitGame_KillsTheJSListenProcess(t *testing.T) {
	t.Parallel()

	for _, program := range []string{`"/usr/bin/killall retroarch"`, `/usr/bin/killall retroarch`} {
		s, sent := newStopper(t, map[int]string{100: "retroarch", 101: "emulationstation"})
		writeJSListen(t, s.fs, program)

		require.NoError(t, s.exitGame(context.Background()), program)
		assert.Equal(t, []killed{{100, syscall.SIGTERM}}, *sent, program)
	}
}

func TestKillGame_RepeatsTheKill(t *testing.T) {
	t.Parallel()

	s, sent := newStopper(t, map[int]string{100: "mupen64plus"})
	writeJSListen(t, s.fs, `"/usr/bin/killall mupen64plus"`)

	require.NoError(t, s.killGame())
	assert.Equal(t, []killed{{100, syscall.SIGTERM}}, *sent)
}

func TestExitGame_RefusesAnythingButAPlainKillall(t *testing.T) {
	t.Parallel()

	for _, program := range []string{
		`"/usr/bin/reboot"`,
		`"/usr/bin/manage_mpv.sh quit"`,
		`"/usr/bin/killall -s KILL retroarch"`,
		`"sh -c killall retroarch"`,
		`""`,
	} {
		s, sent := newStopper(t, map[int]string{100: "retroarch"})
		writeJSListen(t, s.fs, program)

		require.Error(t, s.exitGame(context.Background()), program)
		assert.Empty(t, *sent, program)
	}
}

func TestExitGame_NeverKillsEmulationStation(t *testing.T) {
	t.Parallel()

	s, sent := newStopper(t, map[int]string{101: "emulationstation"})
	writeJSListen(t, s.fs, `"/usr/bin/killall emulationstation"`)

	require.ErrorIs(t, s.exitGame(context.Background()), esbase.ErrNoProcessNames)
	assert.Empty(t, *sent)
}

func TestExitGame_MissingConfig(t *testing.T) {
	t.Parallel()

	s, sent := newStopper(t, nil)
	require.Error(t, s.exitGame(context.Background()))
	require.NoError(t, afero.WriteFile(s.fs, JSListenConfig, []byte("[Generic]\nbutton1=6\n"), 0o644))
	require.Error(t, s.exitGame(context.Background()))
	assert.Empty(t, *sent)
}

// AmberELEC's modules and ports call "jslisten set" with "killall <name>" or
// "/usr/bin/killall <name>", and the helper prefixes its own killall.
func TestExitGame_DoubledKillall(t *testing.T) {
	t.Parallel()

	for _, program := range []string{
		`"/usr/bin/killall killall scummvm"`,
		`"/usr/bin/killall /usr/bin/killall OpenTyrian"`,
	} {
		s, sent := newStopper(t, map[int]string{100: "scummvm", 101: "OpenTyrian", 102: "killall"})
		writeJSListen(t, s.fs, program)

		require.NoError(t, s.exitGame(context.Background()), program)
		require.Len(t, *sent, 1, program)
		assert.NotEqual(t, 102, (*sent)[0].pid, program)
	}
}

func TestOptions_AmberELEC(t *testing.T) {
	t.Parallel()

	opts := Options()
	assert.Equal(t, esbase.ELECSystemAliases, opts.SystemAliases)
	assert.ElementsMatch(t, []string{"imageviewer", "tools"}, opts.NonGameSystems)
	assert.Equal(t, []string{SystemESConfigDir, UserESConfigDir}, opts.ESConfigDirs)
	assert.Equal(t, ROMRoot, opts.DefaultROMRoot)
	assert.NotNil(t, opts.ExitGame)
	assert.NotNil(t, opts.KillGame)
}
