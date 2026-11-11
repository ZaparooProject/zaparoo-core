//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package rocknix

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esbase"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/jonboulle/clockwork"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const testProcRoot = "/proc"

type killed struct {
	pid int
	sig syscall.Signal
}

func newStopper(t *testing.T, procs map[int]string) (*stopper, *[]killed, *mocks.MockCommandExecutor) {
	t.Helper()
	fs := afero.NewMemMapFs()
	for pid, comm := range procs {
		dir := filepath.Join(testProcRoot, strconv.Itoa(pid))
		require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "comm"), []byte(comm+"\n"), 0o644))
		require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "cmdline"), []byte("/usr/bin/"+comm+"\x00"), 0o644))
	}
	var sent []killed
	cmd := &mocks.MockCommandExecutor{}
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
		cmd: cmd,
	}, &sent, cmd
}

func writeKillData(t *testing.T, fs afero.Fs, data string) {
	t.Helper()
	require.NoError(t, afero.WriteFile(fs, KillDataFile, []byte(data), 0o644))
}

func TestPlatform_Identity(t *testing.T) {
	t.Parallel()

	p := NewPlatform()
	assert.Equal(t, platformids.ROCKNIX, p.ID())
	settings := p.Settings()
	assert.Equal(t, "/storage/.local/share/zaparoo", settings.DataDir)
	assert.Equal(t, "/storage/.config/zaparoo", settings.ConfigDir)
	assert.Equal(t, "/storage/.local/share/zaparoo/logs", settings.LogDir)
	assert.False(t, p.ManagedByPackageManager())

	opts := Options()
	assert.Equal(t, []string{"/usr/config/emulationstation", "/storage/.config/emulationstation"}, opts.ESConfigDirs)
	assert.Equal(t, "/storage/roms", opts.DefaultROMRoot)
	assert.NotNil(t, opts.KillGame, "ROCKNIX's /emukill does nothing")
	assert.Nil(t, opts.Controls, "ROCKNIX has no hotkeygen")
}

func TestRootDirs_AlwaysIncludesROCKNIXROMs(t *testing.T) {
	t.Parallel()

	cfg, err := helpers.NewTestConfig(helpers.NewMemoryFS(), t.TempDir())
	require.NoError(t, err)
	assert.Contains(t, NewPlatform().RootDirs(cfg), "/storage/roms")
}

func TestExitGame_KillsTheNamedProcesses(t *testing.T) {
	t.Parallel()

	s, sent, _ := newStopper(t, map[int]string{
		100: "retroarch", 101: "retroarch32", 102: "emulationstation", 103: "mednafen",
	})
	writeKillData(t, s.fs, "retroarch retroarch32\n")

	require.NoError(t, s.exitGame(context.Background()))
	assert.ElementsMatch(t, []killed{{100, syscall.SIGTERM}, {101, syscall.SIGTERM}}, *sent)
}

func TestExitGame_MinusNineKillsAtOnce(t *testing.T) {
	t.Parallel()

	s, sent, _ := newStopper(t, map[int]string{100: "mednafen"})
	writeKillData(t, s.fs, "-9 mednafen")

	require.NoError(t, s.exitGame(context.Background()))
	assert.Equal(t, []killed{{100, syscall.SIGKILL}}, *sent)
}

func TestExitGame_DropsOtherLauncherOptions(t *testing.T) {
	t.Parallel()

	s, sent, _ := newStopper(t, map[int]string{100: "mednafen"})
	writeKillData(t, s.fs, "-q mednafen")

	require.NoError(t, s.exitGame(context.Background()))
	assert.Equal(t, []killed{{100, syscall.SIGTERM}}, *sent)
}

func TestExitGame_NeverKillsEmulationStation(t *testing.T) {
	t.Parallel()

	for _, data := range []string{"emulationstation", "retroarch emulationstation"} {
		s, sent, _ := newStopper(t, map[int]string{100: "retroarch", 102: "emulationstation"})
		writeKillData(t, s.fs, data)

		require.ErrorIs(t, s.exitGame(context.Background()), errIdle, data)
		assert.Empty(t, *sent, data)
	}
}

func TestExitGame_RejectsUnsafeNames(t *testing.T) {
	t.Parallel()

	s, sent, _ := newStopper(t, map[int]string{100: "retroarch"})
	writeKillData(t, s.fs, "../retroarch /usr/bin/retroarch")

	require.ErrorIs(t, s.exitGame(context.Background()), esbase.ErrNoProcessNames)
	assert.Empty(t, *sent)
}

func TestExitGame_NoKillData(t *testing.T) {
	t.Parallel()

	s, sent, _ := newStopper(t, nil)
	require.Error(t, s.exitGame(context.Background()))
	assert.Empty(t, *sent)
}

func TestKillGame_StopsGameScopes(t *testing.T) {
	t.Parallel()

	s, _, cmd := newStopper(t, nil)
	cmd.On("Run", mock.Anything, "systemctl", []string{"stop", "game-*.scope"}).Return(nil).Once()
	require.NoError(t, s.killGame())

	cmd.On("Run", mock.Anything, "systemctl", []string{"stop", "game-*.scope"}).Return(errors.New("boom")).Once()
	require.ErrorContains(t, s.killGame(), "game-*.scope")
	cmd.AssertExpectations(t)
}

func TestOptions_StopUsesTheKillData(t *testing.T) {
	t.Parallel()

	s, _, _ := newStopper(t, nil)
	opts := newOptions(s)
	require.NotNil(t, opts.ExitGame)
	require.NotNil(t, opts.KillGame)
	assert.Equal(t, time.Second, opts.KillRetryDelay)
}

func TestOptions_SharedAliasesAndNonGameSystems(t *testing.T) {
	t.Parallel()

	opts := newOptions(&stopper{})
	assert.Equal(t, esbase.ELECSystemAliases, opts.SystemAliases)
	assert.Equal(t, []string{"idtech", "imageviewer", "music", "tools"}, opts.NonGameSystems)
}
